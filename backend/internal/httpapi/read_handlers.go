package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/co-rtex/tradegrid/backend/internal/accounts"
	"github.com/co-rtex/tradegrid/backend/internal/exchange"
	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/instruments"
)

// Read endpoints from docs/api.md. Each handler makes one service call, so
// every response is one coherent snapshot taken between submissions. The
// service returns copies and non-nil slices, so empty collections encode as
// [] and nothing here can reach the exchange's internal state.

type symbolsResponse struct {
	Symbols []instruments.Instrument `json:"symbols"`
}

type accountsResponse struct {
	Accounts []accounts.Snapshot `json:"accounts"`
}

type ordersResponse struct {
	Orders []exchange.Order `json:"orders"`
}

type executionsResponse struct {
	Executions []executions.Record `json:"executions"`
}

// GET /api/v1/symbols
func handleSymbols(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, symbolsResponse{Symbols: instruments.List()})
}

// GET /api/v1/accounts
func handleAccounts(svc *exchange.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, accountsResponse{Accounts: svc.Accounts()})
	}
}

// GET /api/v1/orders?userId=...&limit=...
func handleOrders(svc *exchange.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, limit, err := query(r, "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		out, err := svc.Orders(q.Get("userId"), limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ordersResponse{Orders: out})
	}
}

// GET /api/v1/orderbook?symbol=...&depth=...
func handleOrderBook(svc *exchange.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, depth, err := query(r, "depth")
		if err != nil {
			writeError(w, err)
			return
		}
		snap, err := svc.OrderBook(q.Get("symbol"), depth)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snap)
	}
}

// GET /api/v1/executions?symbol=...&userId=...&limit=...
func handleExecutions(svc *exchange.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, limit, err := query(r, "limit")
		if err != nil {
			writeError(w, err)
			return
		}
		out, err := svc.Executions(q.Get("symbol"), q.Get("userId"), limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, executionsResponse{Executions: out})
	}
}

// query parses the request's query string strictly and reads the optional
// integer parameter intName with intQuery. r.URL.Query would silently drop a
// pair it cannot decode, so "limit=%zz" would become the default limit; here
// it is INVALID_QUERY. The service then checks, in docs/api.md order,
// required parameters and bounds (400), the user (404), and the symbol (422).
func query(r *http.Request, intName string) (url.Values, int, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, 0, &exchange.Error{Code: exchange.CodeInvalidQuery,
			Err: fmt.Errorf("malformed query string: %w", err)}
	}
	n, err := intQuery(q, intName)
	return q, n, err
}

// intQuery parses an optional positive integer query parameter. An absent
// parameter returns 0, which the service treats as its default; the service
// also enforces the upper bound. A present parameter must be a decimal
// integer of at least 1, so "limit=", "limit=0", and "limit=abc" are all
// INVALID_QUERY rather than silently becoming the default.
func intQuery(q url.Values, name string) (int, error) {
	if !q.Has(name) {
		return 0, nil
	}
	v, err := strconv.Atoi(q.Get(name))
	if err != nil || v < 1 {
		return 0, &exchange.Error{Code: exchange.CodeInvalidQuery, Field: name,
			Err: fmt.Errorf("%s must be a positive integer, got %q", name, q.Get(name))}
	}
	return v, nil
}
