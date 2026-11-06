// Package httpapi is the backend's HTTP layer. It turns requests into calls on
// the rest of the backend and writes JSON responses. cmd/server owns
// configuration and the server lifecycle; this package owns routes.
//
// Routes use Go's method-aware ServeMux patterns such as "GET /health". The
// mux answers a known path with an unregistered method with 405 Method Not
// Allowed and an Allow header listing the registered methods, and answers an
// unknown path with 404 Not Found.
package httpapi

import (
	"net/http"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// NewRouter returns the handler for every backend route. The API routes read
// from and write to svc, the in-memory exchange; /health does not use it.
// Tests serve the router with httptest over a fresh exchange.New, so no
// database is needed.
//
// The Sprint 2 API routes (#36, #37) are registered here as they are added,
// each group in its own handler file.
func NewRouter(svc *exchange.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	// Read endpoints (#37), in read_handlers.go.
	mux.HandleFunc("GET /api/v1/symbols", handleSymbols)
	mux.HandleFunc("GET /api/v1/accounts", handleAccounts(svc))
	mux.HandleFunc("GET /api/v1/orders", handleOrders(svc))
	mux.HandleFunc("GET /api/v1/orderbook", handleOrderBook(svc))
	mux.HandleFunc("GET /api/v1/executions", handleExecutions(svc))
	return mux
}
