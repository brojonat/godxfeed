package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/brojonat/godxfeed/service"
	"github.com/golang-jwt/jwt"
)

func createBearerToken(email string, expiresAt time.Time) (string, error) {
	sc := jwt.StandardClaims{
		ExpiresAt: expiresAt.Unix(),
	}
	c := authJWTClaims{
		StandardClaims: sc,
		Email:          email,
	}
	return generateAccessToken(c)
}

// handleIssueAuthToken returns an auth token for the http server
func handleIssueToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, ok := r.Context().Value(ctxKeyEmail).(string)
		if !ok {
			writeInternalError(s, w, fmt.Errorf("missing context key for basic auth email"))
			return
		}
		token, err := createBearerToken(email, time.Now().Add(2*7*24*time.Hour))
		if err != nil {
			writeInternalError(s, w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct {
			Token string `json:"token"`
		}{Token: token})
	}
}

func handleRefreshToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct {
			Token string `json:"token"`
		}{Token: token})
	}
}

// handleNATSCallout returns simply returns the token that was passed
// in the Authroization header. This handler should be wrapped in a
// handler that checks the token and returns a 401 if it's invalid,
// so we know this token is valid; we simply need to return a 200
// response to the caller (which is the NATS server).
func handleNATSCallout(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct {
			Token string `json:"token"`
		}{Token: token})
	}
}

func handleTestBearerToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

func handleNewStreamerToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		td, err := s.NewStreamerToken(r.Context())
		if err != nil {
			writeInternalError(s, w, err)
			return
		}
		writeJSONResponse(w, td, http.StatusOK)
	}
}
