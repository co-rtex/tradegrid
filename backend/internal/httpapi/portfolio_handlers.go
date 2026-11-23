package httpapi

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
	"github.com/co-rtex/tradegrid/backend/internal/instruments"
	"github.com/co-rtex/tradegrid/backend/internal/portfolio"
)

// GET /api/v1/portfolio?userId=... (docs/portfolio.md). The handler reads one
// copied account view from the exchange, taken under its lock, and values it
// with the pure portfolio calculator at the catalog's fixed reference prices.
// It computes nothing itself and changes nothing.
func handlePortfolio(svc *exchange.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, &exchange.Error{Code: exchange.CodeInvalidQuery,
				Err: fmt.Errorf("malformed query string: %w", err)})
			return
		}
		view, err := svc.Portfolio(q.Get("userId"))
		if err != nil {
			writeError(w, err)
			return
		}
		valuation, err := portfolio.Value(toValuationInput(view), referencePrices())
		if err != nil {
			// A missing reference price, an overflow, or a broken invariant
			// is a server bug: INTERNAL, logged, and nothing changes.
			writeError(w, &exchange.Error{Code: exchange.CodeInternal, Err: err})
			return
		}
		writeJSON(w, http.StatusOK, valuation)
	}
}

// toValuationInput converts the exchange's copied view into the calculator's
// input. Both sides use the same balance fields, so this is a field copy.
func toValuationInput(v exchange.Portfolio) portfolio.AccountSnapshot {
	holdings := make([]portfolio.Holding, 0, len(v.Holdings))
	for _, h := range v.Holdings {
		holdings = append(holdings, portfolio.Holding{
			Symbol: h.Symbol,
			Shares: portfolio.Balance(h.Shares),
			Lot:    h.Lot,
		})
	}
	return portfolio.AccountSnapshot{
		UserID:       v.UserID,
		DisplayName:  v.DisplayName,
		AsOfSequence: v.AsOfSequence,
		Cash:         portfolio.Balance(v.Cash),
		Holdings:     holdings,
	}
}

// referencePrices returns the catalog's simulated reference prices by symbol.
func referencePrices() map[string]int64 {
	list := instruments.ReferencePrices()
	prices := make(map[string]int64, len(list))
	for _, p := range list {
		prices[p.Symbol] = p.Price
	}
	return prices
}
