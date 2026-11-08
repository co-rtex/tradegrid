package exchange

import (
	"errors"
	"math"
	"testing"

	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

func TestSubmissionBoundary(t *testing.T) {
	// Check inclusive endpoints, values just outside them, and int64 extremes.
	for _, tc := range []struct {
		name            string
		price, quantity int64
		code            Code
		cause           error
	}{
		{"minimum", 1, 1, "", nil},
		{"maximum", MaxPrice, MaxQuantity, "", nil},
		{"zero price", 0, 1, CodeInvalidPrice, orders.ErrInvalidPrice},
		{"negative price", math.MinInt64, 1, CodeInvalidPrice, orders.ErrInvalidPrice},
		{"price above limit", MaxPrice + 1, 1, CodeInvalidPrice, nil},
		{"maximum int price", math.MaxInt64, 1, CodeInvalidPrice, nil},
		{"zero quantity", 1, 0, CodeInvalidQuantity, orders.ErrInvalidQuantity},
		{"negative quantity", 1, math.MinInt64, CodeInvalidQuantity, orders.ErrInvalidQuantity},
		{"quantity above limit", 1, MaxQuantity + 1, CodeInvalidQuantity, nil},
		{"maximum int quantity", 1, math.MaxInt64, CodeInvalidQuantity, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := checkRequest(limit("bob", "BUY", tc.quantity, tc.price))
			if tc.code == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var boundaryErr *Error
			if !errors.As(err, &boundaryErr) || boundaryErr.Code != tc.code {
				t.Fatalf("error = %v, want code %s", err, tc.code)
			}
			// Nonpositive values must retain the original order-model error.
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("error = %v, want underlying %v", err, tc.cause)
			}
		})
	}
}

func TestSymbolQueryValidation(t *testing.T) {
	s := newTestService()
	for _, symbol := range []string{"aapl", "GOOG", " "} {
		for _, err := range []error{func() error { _, e := s.OrderBook(symbol, 0); return e }(), func() error { _, e := s.Executions(symbol, "", 0); return e }()} {
			var boundaryErr *Error
			if !errors.As(err, &boundaryErr) || boundaryErr.Code != CodeUnknownSymbol || boundaryErr.Field != "symbol" {
				t.Errorf("symbol %q: error = %v, want UNKNOWN_SYMBOL", symbol, err)
			}
		}
	}
	for _, symbol := range []string{"AAPL", "MSFT"} {
		if _, err := s.OrderBook(symbol, 0); err != nil {
			t.Errorf("OrderBook(%q): %v", symbol, err)
		}
		if _, err := s.Executions(symbol, "", 0); err != nil {
			t.Errorf("Executions(%q): %v", symbol, err)
		}
	}
}

func TestAcceptedBoundaryOrders(t *testing.T) {
	s := newTestService()
	req := limit("bob", "BUY", 1, 1)
	result, err := s.Submit(req)
	if err != nil {
		t.Fatalf("Submit(%+v): %v", req, err)
	}
	book, err := s.OrderBook("AAPL", 0)
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Price != 1 || book.Bids[0].Quantity != 1 || result.Order.Remaining != 1 {
		t.Fatalf("Submit(%+v) result = %+v, book = %+v, err = %v", req, result, book, err)
	}

	// The largest valid request passes validation; a demo account cannot
	// afford it, so risk rejects it rather than a field rule.
	req = limit("bob", "BUY", MaxQuantity, MaxPrice)
	req.Symbol = "MSFT"
	if _, err := checkRequest(req); err != nil {
		t.Fatalf("checkRequest(%+v): %v", req, err)
	}
	var e *Error
	if _, err := s.Submit(req); !errors.As(err, &e) || e.Code != CodeInsufficientCash {
		t.Fatalf("Submit(%+v): err = %v, want INSUFFICIENT_CASH", req, err)
	}
}
