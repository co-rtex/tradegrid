package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// docExample is the GET /api/v1/portfolio response in docs/portfolio.md:
// Alice after worked example 1.
const docExample = `{
  "userId": "alice",
  "displayName": "Alice (demo)",
  "asOfSequence": 2,
  "cash": { "total": 10185000, "reserved": 0, "available": 10185000 },
  "positions": [
    {
      "symbol": "AAPL", "total": 90, "reserved": 0, "available": 90,
      "costBasis": 1710000, "referencePrice": 19000, "marketValue": 1710000,
      "unrealizedPnl": 0, "realizedPnl": -5000
    },
    {
      "symbol": "MSFT", "total": 100, "reserved": 0, "available": 100,
      "costBasis": 4200000, "referencePrice": 42000, "marketValue": 4200000,
      "unrealizedPnl": 0, "realizedPnl": 0
    }
  ],
  "totals": {
    "costBasis": 5910000, "marketValue": 5910000, "unrealizedPnl": 0,
    "realizedPnl": -5000, "equity": 16095000
  }
}`

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPortfolioMatchesContractExample(t *testing.T) {
	svc := exchange.New(fixtureConfig())
	srv := newExchangeServer(t, svc)
	submit(t, svc, "alice", "AAPL", "SELL", 18500, 10)
	submit(t, svc, "bob", "AAPL", "BUY", 18500, 10)

	status, got := getJSON(t, srv.URL+"/api/v1/portfolio?userId=alice")
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, got)
	}
	if want := decode(t, docExample); !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		t.Errorf("portfolio:\n got %s\nwant %s", gotJSON, docExample)
	}

	// Bob's side of the same trade: 110 AAPL with basis 2085000.
	_, bob := getJSON(t, srv.URL+"/api/v1/portfolio?userId=bob")
	totals := bob.(map[string]any)["totals"].(map[string]any)
	if totals["unrealizedPnl"] != 5000.0 || totals["realizedPnl"] != 0.0 || totals["equity"] != 16105000.0 {
		t.Errorf("bob totals = %v", totals)
	}
}

// A fresh account is valued at its seeded basis: zero P&L and equity
// 16100000, with every symbol listed.
func TestPortfolioSeed(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))
	_, v := getJSON(t, srv.URL+"/api/v1/portfolio?userId=bob")
	body := v.(map[string]any)
	if body["asOfSequence"] != 0.0 || len(body["positions"].([]any)) != 2 {
		t.Errorf("body = %v", body)
	}
	want := map[string]any{"costBasis": 6100000.0, "marketValue": 6100000.0, "unrealizedPnl": 0.0, "realizedPnl": 0.0, "equity": 16100000.0}
	if got := body["totals"]; !reflect.DeepEqual(got, want) {
		t.Errorf("totals = %v, want %v", got, want)
	}
}

// A closed position still appears, with zero holdings and its realized P&L,
// and no field is null.
func TestPortfolioClosedPosition(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)
	submit(t, svc, "alice", "MSFT", "SELL", 41500, 60)
	submit(t, svc, "alice", "MSFT", "SELL", 42500, 40)
	submit(t, svc, "bob", "MSFT", "BUY", 43000, 100)

	resp, raw := do(t, http.MethodGet, srv.URL+"/api/v1/portfolio?userId=alice")
	if resp.StatusCode != http.StatusOK || strings.Contains(string(raw), "null") {
		t.Fatalf("status %d, body %s", resp.StatusCode, raw)
	}
	msft := decode(t, string(raw)).(map[string]any)["positions"].([]any)[1]
	want := map[string]any{"symbol": "MSFT", "total": 0.0, "reserved": 0.0, "available": 0.0, "costBasis": 0.0,
		"referencePrice": 42000.0, "marketValue": 0.0, "unrealizedPnl": 0.0, "realizedPnl": -10000.0}
	if !reflect.DeepEqual(msft, want) {
		t.Errorf("MSFT = %v, want %v", msft, want)
	}
}

func TestPortfolioErrors(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))
	for _, tt := range []struct {
		query       string
		status      int
		code, field string
	}{
		{"", 400, "INVALID_QUERY", "userId"},
		{"?userId=", 400, "INVALID_QUERY", "userId"},
		{"?userId=%zz", 400, "INVALID_QUERY", ""},
		{"?userId=carol", 404, "UNKNOWN_USER", "userId"},
		{"?userId=Alice", 404, "UNKNOWN_USER", "userId"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			status, v := getJSON(t, srv.URL+"/api/v1/portfolio"+tt.query)
			if status != tt.status {
				t.Errorf("status %d, want %d", status, tt.status)
			}
			checkError(t, v, tt.code, tt.field)
		})
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		resp, _ := do(t, method, srv.URL+"/api/v1/portfolio?userId=alice")
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: status %d Allow %q", method, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}
}

// Reading a portfolio changes nothing.
func TestPortfolioReadChangesNothing(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)
	submit(t, svc, "alice", "AAPL", "SELL", 19100, 5)
	_, before := getJSON(t, srv.URL+"/api/v1/accounts")
	_, first := getJSON(t, srv.URL+"/api/v1/portfolio?userId=alice")
	_, second := getJSON(t, srv.URL+"/api/v1/portfolio?userId=alice")
	_, after := getJSON(t, srv.URL+"/api/v1/accounts")
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(first, second) {
		t.Errorf("portfolio read changed state")
	}
	// Reserved shares stay in total and market value.
	aapl := first.(map[string]any)["positions"].([]any)[0].(map[string]any)
	if aapl["reserved"] != 5.0 || aapl["total"] != 100.0 || aapl["marketValue"] != 1900000.0 {
		t.Errorf("AAPL = %v", aapl)
	}
}
