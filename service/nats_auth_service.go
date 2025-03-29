package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/nats-io/nkeys"
)

type AuthService struct {
	issuerKeyPair nkeys.KeyPair

	handler AuthHandler
}

type AuthHandler func(req *jwt.AuthorizationRequestClaims) (*jwt.UserClaims, error)

func NewAuthService(issuer nkeys.KeyPair, handler AuthHandler) *AuthService {
	return &AuthService{
		issuerKeyPair: issuer,
		handler:       handler,
	}
}

func (a *AuthService) Handle(r micro.Request) {
	rc, err := jwt.DecodeAuthorizationRequestClaims(string(r.Data()))
	if err != nil {
		log.Println("Error", err)
		r.Error("500", err.Error(), nil)
	}

	userNkey := rc.UserNkey
	serverId := rc.Server.ID

	claims, err := a.handler(rc)
	if err != nil {
		a.Respond(r, userNkey, serverId, "", err)
		return
	}

	token, err := ValidateAndSign(claims, a.issuerKeyPair)
	a.Respond(r, userNkey, serverId, token, err)
}

func ValidateAndSign(claims *jwt.UserClaims, kp nkeys.KeyPair) (string, error) {
	// Validate the claims.
	vr := jwt.CreateValidationResults()
	claims.Validate(vr)
	if len(vr.Errors()) > 0 {
		return "", errors.Join(vr.Errors()...)
	}

	// Sign it with the issuer key since this is non-operator mode.
	return claims.Encode(kp)
}

func (a *AuthService) Respond(req micro.Request, userNKey, serverId, userJWT string, err error) {
	rc := jwt.NewAuthorizationResponseClaims(userNKey)
	rc.Audience = serverId
	rc.Jwt = userJWT
	if err != nil {
		rc.Error = err.Error()
	}

	token, err := rc.Encode(a.issuerKeyPair)
	if err != nil {
		log.Println("error encoding response jwt:", err)
	}

	req.Respond([]byte(token))
}

func SetupNatsWithAuthCallout(
	ctx context.Context,
	natsURL string,
	natsAuthUser string,
	natsAuthPassword string,
	natsGodxfeedUser string,
	natsGodxfeedPassword string,
	natsNkeySeed string,
	authCalloutEndpoint string,
) (*nats.Conn, error) {
	// setup a nats connection for the auth service
	authNC, err := nats.Connect(
		natsURL,
		nats.UserInfo(
			natsAuthUser,
			natsAuthPassword,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("could not connect to nats server (auth): %w", err)
	}

	// setup a nats connection for the app service
	appNC, err := nats.Connect(
		natsURL,
		nats.UserInfo(
			natsGodxfeedUser,
			natsGodxfeedPassword,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("could not connect to nats server (app): %w", err)
	}

	// setup the nats key pair for auth service
	kp, err := nkeys.FromSeed([]byte(natsNkeySeed))
	if err != nil {
		return nil, fmt.Errorf("could not setup nats server: %w", err)
	}

	// implement the auth service with a callout to the auth service
	auth := NewAuthService(kp, func(req *jwt.AuthorizationRequestClaims) (*jwt.UserClaims, error) {

		claims := jwt.NewUserClaims(req.UserNkey)
		claims.Audience = "godxfeed"

		// verify token (this is simply a request to localhost)
		token := req.ConnectOptions.Token

		// make a call to /nats-auth-callout
		calloutReq, err := http.NewRequest(
			http.MethodGet,
			authCalloutEndpoint,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("could not create auth callout request: %w", err)
		}
		calloutReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		resp, err := http.DefaultClient.Do(calloutReq)
		if err != nil {
			return nil, fmt.Errorf("could not make auth callout request: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("auth callout request failed with status code %d", resp.StatusCode)
		}

		// the JWT is good; extract the user's email
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid token format")
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("could not decode token payload: %w", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, fmt.Errorf("could not parse token claims: %w", err)
		}
		email, ok := parsed["email"].(string)
		if !ok {
			return nil, fmt.Errorf("email not found in token claims")
		}

		// Assign Permissions (these users should only be able to read from the godxfeed topic)
		claims.Name = email
		claims.Permissions = jwt.Permissions{
			Pub: jwt.Permission{
				Allow: jwt.StringList{},
			},
			Sub: jwt.Permission{
				Allow: jwt.StringList{"godxfeed.*"},
			},
		}
		return claims, nil
	})

	// add the auth service to the auth nats connection
	_, err = micro.AddService(authNC, micro.Config{
		Name:        "auth",
		Version:     "0.0.1",
		Description: "handle authentication for JWT",
		Endpoint: &micro.EndpointConfig{
			Subject: "$SYS.REQ.USER.AUTH",
			Handler: auth,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("could not add service: %w", err)
	}
	return appNC, nil
}
