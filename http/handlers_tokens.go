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

// handleIssueAuthToken returns an auth token for the http server
func handleIssueToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, ok := r.Context().Value(ctxKeyEmail).(string)
		if !ok {
			writeInternalError(s, w, fmt.Errorf("missing context key for basic auth email"))
			return
		}
		sc := jwt.StandardClaims{
			ExpiresAt: time.Now().Add(2 * 7 * 24 * time.Hour).Unix(),
		}
		c := authJWTClaims{
			StandardClaims: sc,
			Email:          email,
		}
		token, _ := generateAccessToken(c)
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

func handleTestSessionToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := r.URL.Query().Get("session-token")
		if st == "" {
			writeBadRequestError(w, fmt.Errorf("must supply session-token"))
			return
		}
		twr, err := s.TestSessionToken(st)
		writeServiceResponse(s, w, twr, err)
	}
}

func handleNewSessionToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query()["username"][0]
		p := r.URL.Query()["password"][0]
		twr, err := s.NewSessionToken(u, p)
		writeServiceResponse(s, w, twr, err)
	}
}

func handleNewStreamerToken(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := r.URL.Query().Get("session-token")
		if st == "" {
			writeBadRequestError(w, fmt.Errorf("must supply session-token"))
			return
		}
		twr, err := s.NewStreamerToken()
		writeServiceResponse(s, w, twr, err)
	}
}
