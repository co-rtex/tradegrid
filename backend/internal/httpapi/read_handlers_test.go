package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/accounts"
	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// newExchangeServer serves the real router over svc, so a test can change
// the exchange directly and read it back through HTTP.
func newExchangeServer(t *testing.T, svc *exchange.Service) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRouter(svc))
	t.Cleanup(srv.Close)
	return srv
}

// fixtureConfig is the deterministic ID and clock source the fixtures use:
// ord-N, exe-N, and one second per submission from 2026-10-20T15:00:00Z.
func fixtureConfig() exchange.Config {
	var mu sync.Mutex
	next := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	return exchange.Config{Now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		next = next.Add(time.Second)
		return next
	}}
}

// getJSON sends a GET and decodes the body into a generic JSON value, so
// tests compare exactly what a client would see.
func getJSON(t *testing.T, url string) (int, any) {
	t.Helper()
	resp, body := do(t, http.MethodGet, url)
	if got := resp.Header.Get("Content-Type"); got != jsonContentType {
		t.Errorf("GET %s: Content-Type = %q, want %q", url, got, jsonContentType)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("GET %s: body %q is not JSON: %v", url, body, err)
	}
	return resp.StatusCode, v
}

func submit(t *testing.T, svc *exchange.Service, userID, symbol, side string, price, quantity int64) exchange.SubmitResult {
	t.Helper()
	res, err := svc.Submit(exchange.SubmitRequest{
		UserID: userID, Symbol: symbol, Side: side, Type: "LIMIT", Price: price, Quantity: quantity,
	})
	if err != nil {
		t.Fatalf("submit %s %s %d %s @ %d: %v", userID, side, quantity, symbol, price, err)
	}
	return res
}

// fixture is the part of a docs/fixtures scenario these tests replay.
type fixture struct {
	Setup   []fixtureRequest           `json:"setup"`
	Request *fixtureRequest            `json:"request"`
	After   map[string]json.RawMessage `json:"after"`
	// Response is the expected outcome of Request: 201, or an error code.
	Response struct {
		Status int `json:"status"`
		Body   struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"body"`
	} `json:"response"`
}

type fixtureRequest struct {
	Body struct {
		UserID   string `json:"userId"`
		Symbol   string `json:"symbol"`
		Side     string `json:"side"`
		Type     string `json:"type"`
		Price    int64  `json:"price"`
		Quantity int64  `json:"quantity"`
	} `json:"body"`
}

func (r fixtureRequest) submit(svc *exchange.Service) error {
	b := r.Body
	_, err := svc.Submit(exchange.SubmitRequest{
		UserID: b.UserID, Symbol: b.Symbol, Side: b.Side, Type: b.Type, Price: b.Price, Quantity: b.Quantity,
	})
	return err
}

// TestReadFixtures replays every shared fixture and checks each GET it lists
// returns exactly the expected body. Submissions go straight to the service
// because POST /api/v1/orders belongs to #36; the risk-rejection fixture's
// request is expected to fail and must leave the read state unchanged.
func TestReadFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "docs", "fixtures", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fx fixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			svc := exchange.New(fixtureConfig())
			srv := newExchangeServer(t, svc)

			for i, req := range fx.Setup {
				if err := req.submit(svc); err != nil {
					t.Fatalf("setup[%d]: %v", i, err)
				}
			}
			if fx.Request != nil {
				// #36 checks the POST response body; here only acceptance or
				// the rejection code must match, so the reads below start
				// from the state the fixture describes.
				err := fx.Request.submit(svc)
				var e *exchange.Error
				switch want := fx.Response.Body.Error.Code; {
				case fx.Response.Status == http.StatusCreated && err != nil:
					t.Fatalf("request rejected: %v", err)
				case fx.Response.Status != http.StatusCreated && !errors.As(err, &e):
					t.Fatalf("request accepted (err %v), want %d %s", err, fx.Response.Status, want)
				case e != nil && string(e.Code) != want:
					t.Fatalf("request rejected with %s, want %s", e.Code, want)
				}
			}
			if len(fx.After) == 0 {
				t.Fatal("fixture lists no reads")
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
				status, got := getJSON(t, srv.URL+path)
				if status != http.StatusOK {
					t.Errorf("%s: status %d, body %v", key, status, got)
					continue
				}
				if !reflect.DeepEqual(got, want) {
					gotJSON, _ := json.MarshalIndent(got, "", "  ")
					t.Errorf("%s:\n got %s\nwant %s", key, gotJSON, wantRaw)
				}
			}
		})
	}
}

// Every read of a fresh exchange is a valid empty result, never null.
func TestReadEmptyCollectionsAreArrays(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))

	for path, field := range map[string]string{
		"/api/v1/orders?userId=bob":             "orders",
		"/api/v1/executions?symbol=MSFT":        "executions",
		"/api/v1/orderbook?symbol=MSFT":         "bids",
		"/api/v1/orderbook?symbol=MSFT&depth=1": "asks",
	} {
		_, body := do(t, http.MethodGet, srv.URL+path)
		if want := `"` + field + `":[]`; !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %s, want it to contain %s", path, body, want)
		}
	}
}

// Resting remainders at one price combine into one level; fully filled
// orders leave the book but stay in order history.
func TestReadOrderBookAggregation(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)

	submit(t, svc, "alice", "AAPL", "BUY", 18900, 5)  // ord-1
	submit(t, svc, "bob", "AAPL", "BUY", 19000, 10)   // ord-2
	submit(t, svc, "alice", "AAPL", "BUY", 19000, 7)  // ord-3
	submit(t, svc, "bob", "AAPL", "SELL", 19200, 4)   // ord-4
	submit(t, svc, "alice", "AAPL", "SELL", 19100, 3) // ord-5
	submit(t, svc, "alice", "AAPL", "SELL", 19100, 6) // ord-6
	// Bob sells 12 at 19000: fills all of ord-2 (time priority) and 2 of ord-3.
	submit(t, svc, "bob", "AAPL", "SELL", 19000, 12) // ord-7

	_, got := getJSON(t, srv.URL+"/api/v1/orderbook?symbol=AAPL")
	want := map[string]any{
		"symbol":       "AAPL",
		"asOfSequence": 7.0,
		"bids": []any{
			map[string]any{"price": 19000.0, "quantity": 5.0, "orderCount": 1.0},
			map[string]any{"price": 18900.0, "quantity": 5.0, "orderCount": 1.0},
		},
		"asks": []any{
			map[string]any{"price": 19100.0, "quantity": 9.0, "orderCount": 2.0},
			map[string]any{"price": 19200.0, "quantity": 4.0, "orderCount": 1.0},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("orderbook = %v\nwant %v", got, want)
	}

	// depth limits levels per side, keeping the best prices.
	_, got = getJSON(t, srv.URL+"/api/v1/orderbook?symbol=AAPL&depth=1")
	book := got.(map[string]any)
	if bids := book["bids"].([]any); len(bids) != 1 || bids[0].(map[string]any)["price"] != 19000.0 {
		t.Errorf("depth=1 bids = %v, want only 19000", bids)
	}
	if asks := book["asks"].([]any); len(asks) != 1 || asks[0].(map[string]any)["price"] != 19100.0 {
		t.Errorf("depth=1 asks = %v, want only 19100", asks)
	}

	// ord-2 is gone from the book but listed in Bob's history as FILLED.
	_, got = getJSON(t, srv.URL+"/api/v1/orders?userId=bob")
	var ids, statuses []string
	for _, o := range got.(map[string]any)["orders"].([]any) {
		ids = append(ids, o.(map[string]any)["id"].(string))
		statuses = append(statuses, o.(map[string]any)["status"].(string))
	}
	if want := []string{"ord-7", "ord-4", "ord-2"}; !slices.Equal(ids, want) {
		t.Errorf("bob's orders = %v, want %v (newest first)", ids, want)
	}
	if want := []string{"FILLED", "NEW", "FILLED"}; !slices.Equal(statuses, want) {
		t.Errorf("bob's statuses = %v, want %v", statuses, want)
	}
}

// userId and symbol filters, newest-first order, and limit on executions.
func TestReadExecutionFiltersAndLimits(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)

	submit(t, svc, "alice", "AAPL", "SELL", 19000, 1)
	submit(t, svc, "bob", "AAPL", "BUY", 19000, 1) // exe-1: bob buys from alice
	submit(t, svc, "alice", "AAPL", "SELL", 19000, 1)
	submit(t, svc, "alice", "AAPL", "BUY", 19000, 1) // exe-2: alice with herself
	submit(t, svc, "bob", "MSFT", "SELL", 40000, 1)
	submit(t, svc, "alice", "MSFT", "BUY", 40000, 1) // exe-3: MSFT

	execIDs := func(path string) []string {
		t.Helper()
		status, got := getJSON(t, srv.URL+path)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d, body %v", path, status, got)
		}
		ids := []string{}
		for _, e := range got.(map[string]any)["executions"].([]any) {
			ids = append(ids, e.(map[string]any)["id"].(string))
		}
		return ids
	}
	for path, want := range map[string][]string{
		"/api/v1/executions?symbol=AAPL":              {"exe-2", "exe-1"},
		"/api/v1/executions?symbol=AAPL&userId=bob":   {"exe-1"},
		"/api/v1/executions?symbol=AAPL&userId=alice": {"exe-2", "exe-1"},
		"/api/v1/executions?symbol=MSFT&userId=bob":   {"exe-3"},
		"/api/v1/executions?symbol=AAPL&limit=1":      {"exe-2"},
	} {
		if got := execIDs(path); !slices.Equal(got, want) {
			t.Errorf("GET %s = %v, want %v", path, got, want)
		}
	}

	_, got := getJSON(t, srv.URL+"/api/v1/orders?userId=alice&limit=2")
	if n := len(got.(map[string]any)["orders"].([]any)); n != 2 {
		t.Errorf("orders with limit=2 returned %d", n)
	}
}

func TestReadErrors(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))

	tests := []struct {
		path   string
		status int
		code   string
		field  string
	}{
		{"/api/v1/orders", 400, "INVALID_QUERY", "userId"},
		{"/api/v1/orders?userId=", 400, "INVALID_QUERY", "userId"},
		{"/api/v1/orders?userId=carol", 404, "UNKNOWN_USER", "userId"},
		{"/api/v1/orders?userId=alice&limit=0", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?userId=alice&limit=201", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?userId=alice&limit=-1", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?userId=alice&limit=ten", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?userId=alice&limit=", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?userId=alice&limit=1.5", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?userId=alice&limit=99999999999999999999", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orderbook", 400, "INVALID_QUERY", "symbol"},
		{"/api/v1/orderbook?symbol=aapl", 422, "UNKNOWN_SYMBOL", "symbol"},
		{"/api/v1/orderbook?symbol=GOOG", 422, "UNKNOWN_SYMBOL", "symbol"},
		{"/api/v1/orderbook?symbol=AAPL&depth=101", 400, "INVALID_QUERY", "depth"},
		{"/api/v1/orderbook?symbol=AAPL&depth=0", 400, "INVALID_QUERY", "depth"},
		{"/api/v1/executions", 400, "INVALID_QUERY", "symbol"},
		{"/api/v1/executions?symbol=msft", 422, "UNKNOWN_SYMBOL", "symbol"},
		{"/api/v1/executions?symbol=AAPL&userId=carol", 404, "UNKNOWN_USER", "userId"},
		{"/api/v1/executions?symbol=AAPL&limit=500", 400, "INVALID_QUERY", "limit"},

		// A query string that cannot be decoded is INVALID_QUERY, not ignored.
		// No single parameter is at fault, so field is omitted.
		{"/api/v1/orders?userId=alice&limit=%zz", 400, "INVALID_QUERY", ""},
		{"/api/v1/orderbook?symbol=AAPL&depth=%zz", 400, "INVALID_QUERY", ""},
		{"/api/v1/executions?symbol=AAPL&%zz", 400, "INVALID_QUERY", ""},

		// When several rules fail, docs/api.md's order decides: missing
		// parameters and bounds (400), then the user (404), then the symbol (422).
		{"/api/v1/orders?userId=carol&limit=abc", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orders?limit=abc", 400, "INVALID_QUERY", "userId"},
		{"/api/v1/orderbook?depth=0", 400, "INVALID_QUERY", "symbol"},
		{"/api/v1/executions?userId=carol&limit=abc", 400, "INVALID_QUERY", "symbol"},
		{"/api/v1/orders?userId=carol&limit=500", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/orderbook?symbol=GOOG&depth=500", 400, "INVALID_QUERY", "depth"},
		{"/api/v1/executions?userId=carol", 400, "INVALID_QUERY", "symbol"},
		{"/api/v1/executions?symbol=GOOG&limit=500", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/executions?symbol=GOOG&userId=carol&limit=500", 400, "INVALID_QUERY", "limit"},
		{"/api/v1/executions?symbol=GOOG&userId=carol", 404, "UNKNOWN_USER", "userId"},
		{"/api/v1/executions?symbol=GOOG&userId=bob", 422, "UNKNOWN_SYMBOL", "symbol"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			status, got := getJSON(t, srv.URL+tt.path)
			if status != tt.status {
				t.Errorf("status = %d, want %d; body %v", status, tt.status, got)
			}
			checkError(t, got, tt.code, tt.field)
		})
	}
}

// checkError checks that body is the contracted error object with the given
// code and field ("" means field is absent) and a non-empty message.
func checkError(t *testing.T, body any, code, field string) {
	t.Helper()
	e, _ := body.(map[string]any)["error"].(map[string]any)
	gotField, hasField := e["field"]
	if e["code"] != code || (field == "" && hasField) || (field != "" && gotField != field) {
		t.Errorf("error = %v, want code %s field %q", e, code, field)
	}
	if msg, _ := e["message"].(string); msg == "" {
		t.Errorf("error has no message: %v", e)
	}
}

// Unsupported methods on read routes get METHOD_NOT_ALLOWED as JSON, with
// the same Allow header ServeMux would send; HEAD is served like GET.
func TestReadRoutesAreGetOnly(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))

	// /api/v1/orders also accepts POST; TestOrdersRouteMethods covers it.
	paths := []string{
		"/api/v1/symbols", "/api/v1/accounts",
		"/api/v1/orderbook", "/api/v1/executions",
	}
	for _, path := range paths {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			t.Run(method+" "+path, func(t *testing.T) {
				resp, body := do(t, method, srv.URL+path)
				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Errorf("status %d, want 405", resp.StatusCode)
				}
				if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
					t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
				}
				if got := resp.Header.Get("Content-Type"); got != jsonContentType {
					t.Errorf("Content-Type = %q, want %q", got, jsonContentType)
				}
				var v any
				if err := json.Unmarshal(body, &v); err != nil {
					t.Fatalf("body %q is not JSON: %v", body, err)
				}
				checkError(t, v, "METHOD_NOT_ALLOWED", "")
			})
		}
		t.Run("HEAD "+path, func(t *testing.T) {
			resp, body := do(t, http.MethodHead, srv.URL+path+"?userId=alice&symbol=AAPL")
			if resp.StatusCode != http.StatusOK || len(body) != 0 {
				t.Errorf("status %d with %d body bytes, want 200 and no body", resp.StatusCode, len(body))
			}
		})
	}
}

// Unknown paths under /api/ get NOT_FOUND as JSON for every method. Paths
// outside /api/ keep ServeMux's plain-text 404 (TestUnknownPathIsNotFound).
func TestUnknownAPIPathIsJSONNotFound(t *testing.T) {
	srv := newExchangeServer(t, exchange.New(exchange.Config{}))

	for _, path := range []string{
		"/api", "/api/", "/api/v1", "/api/v1/", "/api/v1/nope",
		"/api/v1/symbols/", "/api/v1/orderbook/AAPL", "/api/v2/symbols",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(method+" "+path, func(t *testing.T) {
				resp, body := do(t, method, srv.URL+path)
				if resp.StatusCode != http.StatusNotFound {
					t.Errorf("status %d, want 404; body %q", resp.StatusCode, body)
				}
				var v any
				if err := json.Unmarshal(body, &v); err != nil {
					t.Fatalf("body %q is not JSON: %v", body, err)
				}
				checkError(t, v, "NOT_FOUND", "")
			})
		}
	}
}

// Snapshots are copies: changing what the service returned never changes
// what the next read sees.
func TestReadSnapshotsAreCopies(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)
	submit(t, svc, "alice", "AAPL", "SELL", 19000, 10)
	submit(t, svc, "bob", "AAPL", "BUY", 19000, 4)

	_, before := getJSON(t, srv.URL+"/api/v1/accounts")
	_, ordersBefore := getJSON(t, srv.URL+"/api/v1/orders?userId=alice")
	_, execsBefore := getJSON(t, srv.URL+"/api/v1/executions?symbol=AAPL")

	accts := svc.Accounts()
	accts[0].Cash.Total = -1
	accts[0].Positions[0].Total = -1
	ords, _ := svc.Orders("alice", 0)
	ords[0].Remaining = -1
	execs, _ := svc.Executions("AAPL", "", 0)
	execs[0].Quantity = -1

	for path, want := range map[string]any{
		"/api/v1/accounts":               before,
		"/api/v1/orders?userId=alice":    ordersBefore,
		"/api/v1/executions?symbol=AAPL": execsBefore,
	} {
		if _, got := getJSON(t, srv.URL+path); !reflect.DeepEqual(got, want) {
			t.Errorf("GET %s changed after editing a returned snapshot", path)
		}
	}
}

// TestReadsConcurrentWithSubmissions runs reads against a stream of crossing
// submissions. Under go test -race it checks for data races; on every read
// it checks that the snapshot is coherent: no money or shares appear or
// vanish, balances add up, and the book is sorted and not crossed.
func TestReadsConcurrentWithSubmissions(t *testing.T) {
	svc := exchange.New(exchange.Config{})
	srv := newExchangeServer(t, svc)

	const submissions = 200
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		users := []string{"alice", "bob"}
		sides := []string{"BUY", "SELL"}
		for i := range submissions {
			// Prices around 100.00 so buys and sells keep crossing. Rejections
			// for insufficient funds are fine; only accepted state is read.
			_, _ = svc.Submit(exchange.SubmitRequest{
				UserID: users[i%2], Symbol: "AAPL", Side: sides[(i/2)%2], Type: "LIMIT",
				Price: int64(9_990 + i%5*5), Quantity: int64(1 + i%3),
			})
		}
	}()

	errs := make(chan error, 64)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if err := checkCoherent(srv.URL); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	// Guard against a stream that never trades, which would make the
	// coherence checks above trivially true.
	if execs, err := svc.Executions("AAPL", "", exchange.MaxLimit); err != nil || len(execs) < 10 {
		t.Errorf("submissions produced %d executions (err %v), want at least 10", len(execs), err)
	}
}

func checkCoherent(base string) error {
	var accts struct {
		Accounts []accounts.Snapshot `json:"accounts"`
	}
	if err := fetch(base+"/api/v1/accounts", &accts); err != nil {
		return err
	}
	var cash int64
	shares := map[string]int64{}
	for _, a := range accts.Accounts {
		bals := []accounts.Balance{a.Cash}
		for _, p := range a.Positions {
			shares[p.Symbol] += p.Total
			bals = append(bals, p.Balance)
		}
		for _, b := range bals {
			if b.Reserved < 0 || b.Reserved > b.Total || b.Available != b.Total-b.Reserved {
				return fmt.Errorf("account %s has inconsistent balance %+v", a.UserID, b)
			}
		}
		cash += a.Cash.Total
	}
	// Trades only move cash and shares between the two accounts.
	if want := 2 * accounts.DemoCash; cash != want {
		return fmt.Errorf("total cash = %d, want %d", cash, want)
	}
	for symbol, n := range shares {
		if want := 2 * accounts.DemoShares; n != want {
			return fmt.Errorf("total %s shares = %d, want %d", symbol, n, want)
		}
	}

	var book exchange.BookSnapshot
	if err := fetch(base+"/api/v1/orderbook?symbol=AAPL&depth=100", &book); err != nil {
		return err
	}
	for i := 1; i < len(book.Bids); i++ {
		if book.Bids[i].Price >= book.Bids[i-1].Price {
			return fmt.Errorf("bids not strictly descending: %+v", book.Bids)
		}
	}
	for i := 1; i < len(book.Asks); i++ {
		if book.Asks[i].Price <= book.Asks[i-1].Price {
			return fmt.Errorf("asks not strictly ascending: %+v", book.Asks)
		}
	}
	if len(book.Bids) > 0 && len(book.Asks) > 0 && book.Bids[0].Price >= book.Asks[0].Price {
		return fmt.Errorf("book is crossed: best bid %d, best ask %d", book.Bids[0].Price, book.Asks[0].Price)
	}
	return nil
}

// fetch is a goroutine-safe GET that decodes a 200 JSON body into v. It
// returns errors instead of calling t.Fatal, which only the test goroutine
// may call.
func fetch(url string, v any) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
