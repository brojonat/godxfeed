package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/brojonat/godxfeed/http/api"
	"github.com/brojonat/godxfeed/service"
	"github.com/brojonat/server-tools/stools"
	"github.com/golang-jwt/jwt"
	"github.com/gorilla/handlers"
)

type contextKey int

var ctxKeyJWT contextKey = 1
var ctxKeyEmail contextKey = 2

func getSecretKey() string {
	return os.Getenv("SERVER_SECRET_KEY")
}

type authJWTClaims struct {
	jwt.StandardClaims
	Email string `json:"email"`
}

func generateAccessToken(claims authJWTClaims) (string, error) {
	t := jwt.New(jwt.SigningMethodHS256)
	t.Claims = claims
	return t.SignedString([]byte(getSecretKey()))
}

func basicAuthorizerCtxSetEmail(gsk func() string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("WWW-Authenticate", `Basic realm="godxfeed"`)
		email, pwd, ok := r.BasicAuth()
		if !ok {
			return false
		}
		if email == "" {
			return false
		}
		if pwd != gsk() {
			return false
		}
		ctx := context.WithValue(r.Context(), ctxKeyEmail, email)
		*r = *r.WithContext(ctx)
		return true
	}
}

func bearerAuthorizerCtxSetToken(gsk func() string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		var claims authJWTClaims
		ts := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ts == "" {
			return false
		}
		kf := func(token *jwt.Token) (interface{}, error) {
			return []byte(gsk()), nil
		}
		token, err := jwt.ParseWithClaims(ts, &claims, kf)
		if err != nil || !token.Valid {
			return false
		}
		ctx := context.WithValue(r.Context(), ctxKeyJWT, token.Claims)
		*r = *r.WithContext(ctx)
		return true
	}
}

// Iterates over the supplied authorizers and if at least one passes, then the
// next handler is called, otherwise an unauthorized response is written.
func atLeastOneAuth(authorizers ...func(http.ResponseWriter, *http.Request) bool) stools.HandlerAdapter {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for _, a := range authorizers {
				if !a(w, r) {
					continue
				}
				next(w, r)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(api.DefaultJSONResponse{Error: "unauthorized"})
		}
	}
}

// Convenience middleware that applies commonly used middleware to the wrapped
// handler. This will make the handler gracefully handle panics, sets the
// content type to application/json, limits the body size that clients can send,
// wraps the handler with the usual CORS settings.
func apiMode(s service.Service, maxBytes int64, headers, methods, origins []string) stools.HandlerAdapter {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next = makeGraceful(s)(next)
			next = setMaxBytesReader(maxBytes)(next)
			next = setContentType("application/json")(next)
			handlers.CORS(
				handlers.AllowedHeaders(headers),
				handlers.AllowedMethods(methods),
				handlers.AllowedOrigins(origins),
			)(next).ServeHTTP(w, r)
		}
	}
}

func makeGraceful(s service.Service) stools.HandlerAdapter {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				err := recover()
				if err != nil {
					s.Log(0, "recovered from panic")
					switch v := err.(type) {
					case error:
						writeInternalError(s, w, v)
					case string:
						writeInternalError(s, w, fmt.Errorf(v))
					default:
						writeInternalError(s, w, fmt.Errorf("recovered but unexpected type from recover()"))
					}
				}
			}()
			next.ServeHTTP(w, r)
		}
	}
}

func setMaxBytesReader(mb int64) stools.HandlerAdapter {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, mb)
			next(w, r)
		}
	}
}

func setContentType(content string) stools.HandlerAdapter {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", content)
			next(w, r)
		}
	}
}
