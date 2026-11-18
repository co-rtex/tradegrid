package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// post sends body to POST /api/v1/orders with the given Content-Type ("" to
// send none) and returns the status, headers, and decoded JSON body.
func post(t *testing.T, base, contentType, body string) (*http.Response, any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/orders", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("Content-Type"); got != jsonContentType {
		t.Errorf("Content-Type = %q, want %q", got, jsonContentType)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("body %q is not JSON: %v", raw, err)
	}
	return resp, v
}

func postJSON(t *testing.T, base, body string) (*http.Response, any) {
	t.Helper()
	return post(t, base, "application/json", body)
}

// orderBody builds a request body from the fixture-style fields.
func orderBody(userID, symbol, side string, price, quantity int64) string {
	return fmt.Sprintf(`{"userId":%q,"symbol":%q,"side":%q,"type":"LIMIT","price":%d,"quantity":%d}`,
		userID, symbol, side, price, quantity)
}

// exchangeState reads everything a client can observe, so a test can check
// that a rejected request changed nothing.
func exchangeState(t *testing.T, base string) map[string]any {
	t.Helper()
	state := map[string]any{}
	for _, path := range []string{
		"/api/v1/accounts",
		"/api/v1/orders?userId=alice", "/api/v1/orders?userId=bob",
		"/api/v1/orderbook?symbol=AAPL", "/api/v1/orderbook?symbol=MSFT",
		"/api/v1/executions?symbol=AAPL", "/api/v1/executions?symbol=MSFT",
	} {
		status, v := getJSON(t, base+path)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d", path, status)
		}
		state[path] = v
	}
	return state
}

// TestSubmitFixtures replays every shared fixture through POST and checks
// the exact response body, then every read the fixture lists. Rejections
// compare status, code, and field; messages are for people and may change.
func TestSubmitFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "docs", "fixtures", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	type step struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	var replayed int
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fx struct {
			Setup    []step `json:"setup"`
			Request  *step  `json:"request"`
			Response struct {
				Status int `json:"status"`
				Body   any `json:"body"`
			} `json:"response"`
			After map[string]json.RawMessage `json:"after"`
		}
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if fx.Request == nil || fx.Request.Method != http.MethodPost {
			continue
		}
		replayed++
		t.Run(filepath.Base(path), func(t *testing.T) {
			srv := newExchangeServer(t, exchange.New(fixtureConfig()))
			for i, s := range fx.Setup {
				if s.Method != http.MethodPost || s.Path != "/api/v1/orders" {
					t.Fatalf("setup[%d]: unexpected %s %s", i, s.Method, s.Path)
				}
				if resp, v := postJSON(t, srv.URL, string(s.Body)); resp.StatusCode != http.StatusCreated {
					t.Fatalf("setup[%d]: status %d, body %v", i, resp.StatusCode, v)
				}
			}

			resp, got := postJSON(t, srv.URL, string(fx.Request.Body))
			if resp.StatusCode != fx.Response.Status {
				t.Fatalf("status %d, want %d; body %v", resp.StatusCode, fx.Response.Status, got)
			}
			if resp.StatusCode == http.StatusCreated {
				if !reflect.DeepEqual(got, fx.Response.Body) {
					gotJSON, _ := json.MarshalIndent(got, "", "  ")
					wantJSON, _ := json.MarshalIndent(fx.Response.Body, "", "  ")
					t.Errorf("response:\n got %s\nwant %s", gotJSON, wantJSON)
				}
			} else {
				want := fx.Response.Body.(map[string]any)["error"].(map[string]any)
				field, _ := want["field"].(string)
				checkError(t, got, want["code"].(string), field)
			}

			for key, wantRaw := range fx.After {
				path, ok := strings.CutPrefix(key, "GET ")
				if !ok {
					t.Fatalf("unexpected read %q", key)
				}
				var want any
				if err := json.Unmarshal(wantRaw, &want); err != nil {
					t.Fatal(err)
				}
				if _, got := getJSON(t, srv.URL+path); !reflect.DeepEqual(got, want) {
					gotJSON, _ := json.MarshalIndent(got, "", "  ")
					t.Errorf("%s:\n got %s\nwant %s", key, gotJSON, wantRaw)
				}
			}
		})
	}
	if replayed < 4 {
		t.Fatalf("replayed %d POST fixtures, want at least 4", replayed)
	}
}

// A resting order is 201 with an empty executions array, never null, and
// the response carries the server-owned metadata.
func TestSubmitRestingOrder(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(fixtureConfig()))

	resp, v := postJSON(t, srv.URL, orderBody("alice", "MSFT", "BUY", 41000, 3))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, body %v", resp.StatusCode, v)
	}
	body := v.(map[string]any)
	if ex, ok := body["executions"].([]any); !ok || len(ex) != 0 {
		t.Errorf("executions = %#v, want []", body["executions"])
	}
	order := body["order"].(map[string]any)
	want := map[string]any{
		"id": "ord-1", "userId": "alice", "symbol": "MSFT", "side": "BUY", "type": "LIMIT",
		"price": 41000.0, "quantity": 3.0, "remaining": 3.0, "status": "NEW", "sequence": 1.0,
		"createdAt": "2026-10-20T15:00:01Z", "updatedAt": "2026-10-20T15:00:01Z",
	}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v\nwant %v", order, want)
	}
}

// One submission crossing several resting orders returns every fill in
// matching order (price, then time), each at the resting order's price.
func TestSubmitMultipleFillsInMatchingOrder(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(fixtureConfig()))
	for _, b := range []string{
		orderBody("alice", "AAPL", "SELL", 19100, 2), // ord-1
		orderBody("alice", "AAPL", "SELL", 19000, 3), // ord-2, best price
		orderBody("bob", "AAPL", "SELL", 19000, 4),   // ord-3, same price, later
	} {
		if resp, v := postJSON(t, srv.URL, b); resp.StatusCode != http.StatusCreated {
			t.Fatalf("setup: status %d, body %v", resp.StatusCode, v)
		}
	}

	resp, v := postJSON(t, srv.URL, orderBody("bob", "AAPL", "BUY", 19200, 8))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, body %v", resp.StatusCode, v)
	}
	body := v.(map[string]any)
	order := body["order"].(map[string]any)
	if order["status"] != "FILLED" || order["remaining"] != 0.0 {
		t.Errorf("order = %v, want FILLED with 0 remaining", order)
	}
	var fills []string
	for _, e := range body["executions"].([]any) {
		e := e.(map[string]any)
		fills = append(fills, fmt.Sprintf("%s %v@%v", e["sellOrderId"], e["quantity"], e["price"]))
	}
	want := []string{"ord-2 3@19000", "ord-3 4@19000", "ord-1 1@19100"}
	if !reflect.DeepEqual(fills, want) {
		t.Errorf("fills = %v, want %v", fills, want)
	}
}

// Every request the server rejects gets the contracted status, code, and
// field, and leaves the exchange exactly as it was.
func TestSubmitRejections(t *testing.T) {
	valid := orderBody("bob", "AAPL", "BUY", 19050, 25)
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
		field       string
	}{
		// Content type (415) comes before anything about the body.
		{"no content type", "", valid, 415, "UNSUPPORTED_MEDIA_TYPE", ""},
		{"text/plain", "text/plain", valid, 415, "UNSUPPORTED_MEDIA_TYPE", ""},
		{"form encoded", "application/x-www-form-urlencoded", valid, 415, "UNSUPPORTED_MEDIA_TYPE", ""},
		{"json suffix type", "application/merge-patch+json", valid, 415, "UNSUPPORTED_MEDIA_TYPE", ""},
		{"non-UTF-8 charset", "application/json; charset=latin1", valid, 415, "UNSUPPORTED_MEDIA_TYPE", ""},
		{"unparsable content type", "application/json; =", valid, 415, "UNSUPPORTED_MEDIA_TYPE", ""},
		{"415 before malformed body", "text/plain", "{", 415, "UNSUPPORTED_MEDIA_TYPE", ""},

		// Size (413).
		{"body over 4096 bytes", "application/json", valid + strings.Repeat(" ", 4097-len(valid)), 413, "BODY_TOO_LARGE", ""},
		{"413 before malformed body", "application/json", "{" + strings.Repeat("x", 5000), 413, "BODY_TOO_LARGE", ""},

		// Structure (400 MALFORMED_JSON).
		{"empty body", "application/json", "", 400, "MALFORMED_JSON", ""},
		{"whitespace body", "application/json", "  \n", 400, "MALFORMED_JSON", ""},
		{"truncated object", "application/json", `{"userId":"bob"`, 400, "MALFORMED_JSON", ""},
		{"invalid JSON", "application/json", `{userId: "bob"}`, 400, "MALFORMED_JSON", ""},
		{"array", "application/json", "[" + valid + "]", 400, "MALFORMED_JSON", ""},
		{"null", "application/json", "null", 400, "MALFORMED_JSON", ""},
		{"string", "application/json", `"order"`, 400, "MALFORMED_JSON", ""},
		{"trailing object", "application/json", valid + valid, 400, "MALFORMED_JSON", ""},
		{"trailing garbage", "application/json", valid + " x", 400, "MALFORMED_JSON", ""},
		{"trailing comma", "application/json", strings.TrimSuffix(valid, "}") + ",}", 400, "MALFORMED_JSON", ""},
		{"duplicate field", "application/json", `{"userId":"bob","userId":"alice","symbol":"AAPL","side":"BUY","type":"LIMIT","price":1,"quantity":1}`, 400, "MALFORMED_JSON", "userId"},

		// Wrong JSON types (400 MALFORMED_JSON) with the field named.
		{"price as string", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":"19050","quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"fractional price", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":190.5,"quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"integral float price", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050.0,"quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"exponent price", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":1.905e4,"quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"integer exponent quantity", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050,"quantity":1e1}`, 400, "MALFORMED_JSON", "quantity"},
		{"price beyond int64", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":9223372036854775808,"quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"null price", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":null,"quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"boolean quantity", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050,"quantity":true}`, 400, "MALFORMED_JSON", "quantity"},
		{"numeric userId", "application/json", `{"userId":7,"symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050,"quantity":25}`, 400, "MALFORMED_JSON", "userId"},
		{"null symbol", "application/json", `{"userId":"bob","symbol":null,"side":"BUY","type":"LIMIT","price":19050,"quantity":25}`, 400, "MALFORMED_JSON", "symbol"},
		{"object side", "application/json", `{"userId":"bob","symbol":"AAPL","side":{"v":"BUY"},"type":"LIMIT","price":19050,"quantity":25}`, 400, "MALFORMED_JSON", "side"},
		{"array type", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":["LIMIT"],"price":19050,"quantity":25}`, 400, "MALFORMED_JSON", "type"},

		// Unknown and server-owned fields (400 UNKNOWN_FIELD), matched exactly.
		{"unknown field", "application/json", strings.TrimSuffix(valid, "}") + `,"note":"hi"}`, 400, "UNKNOWN_FIELD", "note"},
		{"server-owned id", "application/json", strings.TrimSuffix(valid, "}") + `,"id":"ord-99"}`, 400, "UNKNOWN_FIELD", "id"},
		{"server-owned status", "application/json", strings.TrimSuffix(valid, "}") + `,"status":"FILLED"}`, 400, "UNKNOWN_FIELD", "status"},
		{"server-owned remaining", "application/json", strings.TrimSuffix(valid, "}") + `,"remaining":0}`, 400, "UNKNOWN_FIELD", "remaining"},
		{"server-owned sequence", "application/json", strings.TrimSuffix(valid, "}") + `,"sequence":1}`, 400, "UNKNOWN_FIELD", "sequence"},
		{"server-owned createdAt", "application/json", strings.TrimSuffix(valid, "}") + `,"createdAt":"2026-10-20T15:00:00Z"}`, 400, "UNKNOWN_FIELD", "createdAt"},
		{"server-owned updatedAt", "application/json", strings.TrimSuffix(valid, "}") + `,"updatedAt":"2026-10-20T15:00:00Z"}`, 400, "UNKNOWN_FIELD", "updatedAt"},
		{"wrong-case field name", "application/json", `{"UserId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050,"quantity":25}`, 400, "UNKNOWN_FIELD", "UserId"},
		{"first unknown field is reported", "application/json", `{"zeta":1,"userId":"bob","alpha":2}`, 400, "UNKNOWN_FIELD", "zeta"},
		{"malformed before unknown", "application/json", `{"note":"hi","userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":"x","quantity":25}`, 400, "MALFORMED_JSON", "price"},
		{"decoding before unknown user", "application/json", `{"userId":"carol","note":1}`, 400, "UNKNOWN_FIELD", "note"},

		// User (404) before field validation.
		{"unknown user", "application/json", orderBody("carol", "AAPL", "BUY", 19050, 25), 404, "UNKNOWN_USER", "userId"},
		{"user ids are case-sensitive", "application/json", orderBody("Bob", "AAPL", "BUY", 19050, 25), 404, "UNKNOWN_USER", "userId"},
		{"unknown user before missing price", "application/json", `{"userId":"carol","symbol":"AAPL","side":"BUY","type":"LIMIT","quantity":25}`, 404, "UNKNOWN_USER", "userId"},
		{"unknown user before bad symbol", "application/json", orderBody("carol", "aapl", "BUY", 0, 0), 404, "UNKNOWN_USER", "userId"},

		// Field validation (422) in table order.
		{"empty object", "application/json", `{}`, 422, "MISSING_FIELD", "userId"},
		{"missing userId", "application/json", `{"symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050,"quantity":25}`, 422, "MISSING_FIELD", "userId"},
		{"empty userId", "application/json", orderBody("", "AAPL", "BUY", 19050, 25), 422, "MISSING_FIELD", "userId"},
		{"missing symbol", "application/json", `{"userId":"bob","side":"BUY","type":"LIMIT","price":19050,"quantity":25}`, 422, "MISSING_FIELD", "symbol"},
		{"empty side", "application/json", orderBody("bob", "AAPL", "", 19050, 25), 422, "MISSING_FIELD", "side"},
		{"missing type", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","price":19050,"quantity":25}`, 422, "MISSING_FIELD", "type"},
		{"missing price", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","quantity":25}`, 422, "MISSING_FIELD", "price"},
		{"missing quantity", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":19050}`, 422, "MISSING_FIELD", "quantity"},
		{"missing string before missing price", "application/json", `{"userId":"bob","side":"BUY","type":"LIMIT","quantity":25}`, 422, "MISSING_FIELD", "symbol"},
		{"missing quantity before unknown symbol", "application/json", `{"userId":"bob","symbol":"ZZZ","side":"BUY","type":"LIMIT","price":19050}`, 422, "MISSING_FIELD", "quantity"},
		{"missing price before invalid quantity", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","quantity":0}`, 422, "MISSING_FIELD", "price"},
		{"lowercase symbol", "application/json", orderBody("bob", "aapl", "BUY", 19050, 25), 422, "UNKNOWN_SYMBOL", "symbol"},
		{"unknown symbol", "application/json", orderBody("bob", "TSLA", "BUY", 19050, 25), 422, "UNKNOWN_SYMBOL", "symbol"},
		{"lowercase side", "application/json", orderBody("bob", "AAPL", "buy", 19050, 25), 422, "INVALID_SIDE", "side"},
		{"MARKET order", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"MARKET","price":19050,"quantity":25}`, 422, "UNSUPPORTED_ORDER_TYPE", "type"},
		{"unknown order type", "application/json", `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"STOP","price":19050,"quantity":25}`, 422, "UNSUPPORTED_ORDER_TYPE", "type"},
		{"zero price", "application/json", orderBody("bob", "AAPL", "BUY", 0, 25), 422, "INVALID_PRICE", "price"},
		{"negative price", "application/json", orderBody("bob", "AAPL", "BUY", -1, 25), 422, "INVALID_PRICE", "price"},
		{"price above bound", "application/json", orderBody("bob", "AAPL", "BUY", 100_000_001, 1), 422, "INVALID_PRICE", "price"},
		{"price before quantity", "application/json", orderBody("bob", "AAPL", "BUY", 0, 0), 422, "INVALID_PRICE", "price"},
		{"zero quantity", "application/json", orderBody("bob", "AAPL", "BUY", 19050, 0), 422, "INVALID_QUANTITY", "quantity"},
		{"negative quantity", "application/json", orderBody("bob", "AAPL", "SELL", 19050, -5), 422, "INVALID_QUANTITY", "quantity"},
		{"quantity above bound", "application/json", orderBody("bob", "AAPL", "SELL", 1, 1_000_001), 422, "INVALID_QUANTITY", "quantity"},

		// Risk (422), last.
		{"insufficient cash", "application/json", orderBody("bob", "AAPL", "BUY", 20000, 600), 422, "INSUFFICIENT_CASH", ""},
		{"insufficient shares", "application/json", orderBody("alice", "MSFT", "SELL", 42000, 101), 422, "INSUFFICIENT_SHARES", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := exchange.New(fixtureConfig())
			srv := newExchangeServer(t, svc)
			// One accepted order first, so "nothing changed" also covers an
			// existing resting order and its reservation.
			if resp, v := postJSON(t, srv.URL, orderBody("alice", "AAPL", "SELL", 19100, 5)); resp.StatusCode != http.StatusCreated {
				t.Fatalf("setup: status %d, body %v", resp.StatusCode, v)
			}
			before := exchangeState(t, srv.URL)

			resp, got := post(t, srv.URL, tt.contentType, tt.body)
			if resp.StatusCode != tt.status {
				t.Errorf("status %d, want %d; body %v", resp.StatusCode, tt.status, got)
			}
			checkError(t, got, tt.code, tt.field)

			if after := exchangeState(t, srv.URL); !reflect.DeepEqual(after, before) {
				t.Errorf("rejected request changed the exchange:\nbefore %v\nafter  %v", before, after)
			}
			// The next accepted order gets the next sequence, so the
			// rejection consumed no order ID or sequence number.
			resp, v := postJSON(t, srv.URL, orderBody("bob", "MSFT", "BUY", 100, 1))
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("follow-up: status %d, body %v", resp.StatusCode, v)
			}
			if o := v.(map[string]any)["order"].(map[string]any); o["id"] != "ord-2" || o["sequence"] != 2.0 {
				t.Errorf("follow-up order = %v %v, want ord-2 sequence 2", o["id"], o["sequence"])
			}
		})
	}
}

// Requests the contract allows are accepted: a body of exactly 4096 bytes,
// a UTF-8 charset parameter in any case, media-type case, whitespace between
// tokens, and fields in any order.
func TestSubmitAcceptsValidVariants(t *testing.T) {
	valid := orderBody("bob", "AAPL", "BUY", 19050, 25)
	tests := []struct {
		name, contentType, body string
	}{
		{"exactly 4096 bytes", "application/json", valid + strings.Repeat(" ", 4096-len(valid))},
		{"charset utf-8", "application/json; charset=utf-8", valid},
		{"charset UTF-8", "application/json; charset=UTF-8", valid},
		{"media type case", "Application/JSON", valid},
		{"whitespace", "application/json", "\n { \"userId\" : \"bob\" ,\t\"symbol\":\"AAPL\", \"side\":\"BUY\",\"type\":\"LIMIT\",\"price\": 19050 ,\"quantity\":25 } \n"},
		{"field order", "application/json", `{"quantity":25,"price":19050,"type":"LIMIT","side":"BUY","symbol":"AAPL","userId":"bob"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.body) > maxRequestBody {
				t.Fatalf("test body is %d bytes", len(tt.body))
			}
			srv := newExchangeServer(t, exchange.New(fixtureConfig()))
			resp, v := post(t, srv.URL, tt.contentType, tt.body)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("status %d, body %v", resp.StatusCode, v)
			}
			o := v.(map[string]any)["order"].(map[string]any)
			if o["userId"] != "bob" || o["price"] != 19050.0 || o["quantity"] != 25.0 {
				t.Errorf("order = %v", o)
			}
		})
	}
}

// The orders route serves GET (and HEAD) reads and POST submissions; every
// other method is METHOD_NOT_ALLOWED with all three in Allow.
func TestOrdersRouteMethods(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			resp, body := do(t, method, srv.URL+"/api/v1/orders")
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("status %d, want 405", resp.StatusCode)
			}
			if got := resp.Header.Get("Allow"); got != "GET, HEAD, POST" {
				t.Errorf("Allow = %q, want %q", got, "GET, HEAD, POST")
			}
			var v any
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatalf("body %q is not JSON: %v", body, err)
			}
			checkError(t, v, "METHOD_NOT_ALLOWED", "")
		})
	}
	if status, _ := getJSON(t, srv.URL+"/api/v1/orders?userId=alice"); status != http.StatusOK {
		t.Errorf("GET status %d, want 200", status)
	}
}

// Concurrent submissions through HTTP are serialized: every accepted order
// gets a distinct, gap-free sequence, and cash and shares are conserved
// across the two accounts with every reservation matching the open orders.
func TestSubmitConcurrent(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)

	const n = 60
	var wg sync.WaitGroup
	seqs := make(chan float64, n)
	errs := make(chan string, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user, side := "alice", "SELL"
			if i%2 == 1 {
				user, side = "bob", "BUY"
			}
			body := orderBody(user, "AAPL", side, int64(18990+i%5*5), int64(1+i%3))
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/orders", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err.Error()
				return
			}
			defer resp.Body.Close()
			var v struct {
				Order struct {
					Sequence float64 `json:"sequence"`
				} `json:"order"`
			}
			if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&v) != nil {
				errs <- fmt.Sprintf("status %d", resp.StatusCode)
				return
			}
			seqs <- v.Order.Sequence
		}()
	}
	wg.Wait()
	close(seqs)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	seen := map[float64]bool{}
	for s := range seqs {
		if seen[s] || s < 1 || s > n {
			t.Errorf("sequence %v duplicated or out of range", s)
		}
		seen[s] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct sequences, want %d", len(seen), n)
	}

	var cash, shares, cashReserved, sharesReserved int64
	for _, a := range svc.Accounts() {
		cash += a.Cash.Total
		cashReserved += a.Cash.Reserved
		for _, p := range a.Positions {
			if p.Symbol == "AAPL" {
				shares += p.Total
				sharesReserved += p.Reserved
			}
		}
	}
	if cash != 20_000_000 || shares != 200 {
		t.Errorf("totals: cash %d, AAPL %d; want 20000000 and 200", cash, shares)
	}
	var wantCash, wantShares int64
	for _, user := range []string{"alice", "bob"} {
		orders, err := svc.Orders(user, exchange.MaxLimit)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range orders {
			if o.Side.String() == "BUY" {
				wantCash += o.Price * o.Remaining
			} else {
				wantShares += o.Remaining
			}
		}
	}
	if cashReserved != wantCash || sharesReserved != wantShares {
		t.Errorf("reserved cash %d shares %d, open orders need %d and %d",
			cashReserved, sharesReserved, wantCash, wantShares)
	}
}
