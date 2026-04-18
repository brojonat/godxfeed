package http

import (
	"encoding/json"
	"net/http"
)

// handlerAdapter wraps an http.HandlerFunc with middleware. Kept in-tree
// (used to come from github.com/brojonat/server-tools/stools) so we don't
// have to drag in that package's transitive deps on ethereum, temporal,
// and grpc.
type handlerAdapter func(http.HandlerFunc) http.HandlerFunc

// adaptHandler applies the given adapters to h. Adapters are wrapped in
// reverse order, so they execute outer-to-inner in the order given.
func adaptHandler(h http.HandlerFunc, opts ...handlerAdapter) http.HandlerFunc {
	for i := range opts {
		opt := opts[len(opts)-1-i]
		h = opt(h)
	}
	return h
}

// handlePing is the standard boot-check handler.
func handlePing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "pong"})
	}
}
