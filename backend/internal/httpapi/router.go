// Package httpapi is the backend's HTTP layer. It turns requests into calls on
// the rest of the backend and writes JSON responses. cmd/server owns
// configuration and the server lifecycle; this package owns routes.
//
// Routes use Go's method-aware ServeMux patterns such as "GET /health". The
// mux answers a known path with an unregistered method with 405 Method Not
// Allowed and an Allow header listing the registered methods, and answers an
// unknown path with 404 Not Found.
package httpapi

import "net/http"

// NewRouter returns the handler for every backend route. It has no
// dependencies, so it can be served or tested with httptest without a
// database or an exchange.
//
// The Sprint 2 API routes (#36, #37) are registered here as they are added,
// each in its own handler file.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	return mux
}
