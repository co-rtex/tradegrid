// Package httpapi is the backend's HTTP layer. It turns requests into calls on
// the rest of the backend and writes JSON responses. cmd/server owns
// configuration and the server lifecycle; this package owns routes.
//
// Routes use Go's method-aware ServeMux patterns such as "GET /health". For
// non-API paths the mux answers a known path with an unregistered method with
// 405 Method Not Allowed and an Allow header, and an unknown path with 404 Not
// Found, both as plain text. Under /api/ the same cases get the JSON error
// body from docs/api.md instead (NOT_FOUND and METHOD_NOT_ALLOWED), because
// API clients parse every error response as JSON.
package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// NewRouter returns the handler for every backend route. The API routes read
// from and write to svc, the in-memory exchange; /health does not use it.
// Tests serve the router with httptest over a fresh exchange.New, so no
// database is needed.
//
// The Sprint 2 API routes (#36, #37) are registered here as they are added,
// each group in its own handler file. Register every API path with
// handleAPI, never with mux.HandleFunc directly: the /api/ catch-all below
// would otherwise answer its unsupported methods with 404 instead of 405.
//
// Options add optional routes: WithStream adds GET /api/v1/stream (#73).
func NewRouter(svc *exchange.Service, opts ...Option) http.Handler {
	var cfg routerConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	// Read endpoints (#37) are in read_handlers.go; order submission (#36)
	// is in order_handlers.go.
	handleAPI(mux, "/api/v1/symbols", methods{http.MethodGet: handleSymbols})
	handleAPI(mux, "/api/v1/accounts", methods{http.MethodGet: handleAccounts(svc)})
	handleAPI(mux, "/api/v1/orders", methods{
		http.MethodGet:  handleOrders(svc),
		http.MethodPost: handleSubmitOrder(svc),
	})
	handleAPI(mux, "/api/v1/orderbook", methods{http.MethodGet: handleOrderBook(svc)})
	handleAPI(mux, "/api/v1/executions", methods{http.MethodGet: handleExecutions(svc)})

	// Portfolio read (#69), in portfolio_handlers.go.
	handleAPI(mux, "/api/v1/portfolio", methods{http.MethodGet: handlePortfolio(svc)})

	// The real-time stream (#73), in stream_handler.go. It answers every
	// method itself so that its 405 lists exactly "Allow: GET", as
	// docs/realtime.md requires.
	if cfg.stream != nil {
		mux.Handle(streamPath, cfg.stream)
	}

	// Any other path under /api/, with any method, is NOT_FOUND. "/api" is
	// listed too so the mux does not redirect it to "/api/".
	mux.HandleFunc("/api", handleAPINotFound)
	mux.HandleFunc("/api/", handleAPINotFound)
	return mux
}

// Option configures optional routes for NewRouter.
type Option func(*routerConfig)

type routerConfig struct {
	stream *Stream
}

// WithStream serves s at GET /api/v1/stream. Without it the path is
// NOT_FOUND like any other unknown API path.
func WithStream(s *Stream) Option {
	return func(c *routerConfig) { c.stream = s }
}

// methods maps an HTTP method to the handler for one API path.
type methods map[string]http.HandlerFunc

// handleAPI registers one handler per method for an API path, plus a
// method-less pattern for the same path that answers every other method
// with METHOD_NOT_ALLOWED. ServeMux prefers "GET /path" over "/path" for a
// GET request, so the fallback only sees unsupported methods. A GET route
// also serves HEAD, as it does in ServeMux.
func handleAPI(mux *http.ServeMux, path string, ms methods) {
	allowed := make([]string, 0, len(ms)+1)
	for method, h := range ms {
		mux.HandleFunc(method+" "+path, h)
		allowed = append(allowed, method)
		if method == http.MethodGet {
			allowed = append(allowed, http.MethodHead)
		}
	}
	slices.Sort(allowed)
	allow := strings.Join(allowed, ", ")

	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeErrorBody(w, http.StatusMethodNotAllowed, codeMethodNotAllowed,
			fmt.Sprintf("%s is not allowed on %s; allowed: %s", r.Method, path, allow), "")
	})
}

func handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeErrorBody(w, http.StatusNotFound, codeNotFound, fmt.Sprintf("no route for %s", r.URL.Path), "")
}
