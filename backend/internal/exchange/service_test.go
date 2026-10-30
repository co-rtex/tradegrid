package exchange

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/accounts"
	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

var t0 = time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)

// newTestService returns a service whose clock starts at t0 and advances one
// second per submission, matching docs/fixtures. The clock is only called
// under the service lock.
func newTestService() *Service {
	n := 0
	return New(Config{Now: func() time.Time {
		n++
		return t0.Add(time.Duration(n) * time.Second)
	}})
}

func limit(user, side string, qty, cents int64) SubmitRequest {
	return SubmitRequest{UserID: user, Symbol: "AAPL", Side: side, Type: "LIMIT", Price: cents, Quantity: qty}
}

func mustSubmit(t *testing.T, s *Service, req SubmitRequest) SubmitResult {
	t.Helper()
	res, err := s.Submit(req)
	if err != nil {
		t.Fatalf("Submit(%+v): %v", req, err)
	}
	return res
}

// state is everything a reader can see, for before/after comparisons.
type state struct {
	Book       BookSnapshot
	Alice, Bob []Order
	History    []executions.Record
	Accounts   []accounts.Snapshot
}

func snapshot(t *testing.T, s *Service) state {
	t.Helper()
	book, err1 := s.OrderBook("AAPL", MaxDepth)
	alice, err2 := s.Orders("alice", MaxLimit)
	bob, err3 := s.Orders("bob", MaxLimit)
	hist, err4 := s.Executions("AAPL", "", MaxLimit)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return state{book, alice, bob, hist, s.Accounts()}
}

// asJSON round-trips v through JSON so it compares equal to decoded fixtures.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// TestFixtures replays every contract fixture and compares the submit
// response (or rejection code) and the reads with the documented JSON. The
// symbols read is the instruments catalog, which the service does not serve.
func TestFixtures(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("..", "..", "..", "docs", "fixtures", "*.json"))
	if err != nil || len(names) != 5 {
		t.Fatalf("fixtures = %v, %v; want 5 files", names, err)
	}
	for _, path := range names {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Setup    []struct{ Body SubmitRequest }
				Request  *struct{ Body SubmitRequest }
				Response struct {
					Status int
					Body   json.RawMessage
				}
				After map[string]any
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}

			s := newTestService()
			for _, r := range fx.Setup {
				mustSubmit(t, s, r.Body)
			}
			if fx.Request != nil {
				res, err := s.Submit(fx.Request.Body)
				if fx.Response.Status == 201 {
					var want any
					json.Unmarshal(fx.Response.Body, &want)
					if err != nil {
						t.Fatalf("Submit: %v", err)
					}
					if got := asJSON(t, res); !reflect.DeepEqual(got, want) {
						t.Errorf("response:\n got  %v\n want %v", got, want)
					}
				} else {
					var want struct{ Error struct{ Code Code } }
					json.Unmarshal(fx.Response.Body, &want)
					var e *Error
					if !errors.As(err, &e) || e.Code != want.Error.Code {
						t.Errorf("err = %v, want code %s", err, want.Error.Code)
					}
				}
			}

			book, _ := s.OrderBook("AAPL", 0)
			alice, _ := s.Orders("alice", 0)
			bob, _ := s.Orders("bob", 0)
			execs, _ := s.Executions("AAPL", "", 0)
			reads := map[string]any{
				"GET /api/v1/accounts":               map[string]any{"accounts": s.Accounts()},
				"GET /api/v1/orderbook?symbol=AAPL":  book,
				"GET /api/v1/orders?userId=alice":    map[string]any{"orders": alice},
				"GET /api/v1/orders?userId=bob":      map[string]any{"orders": bob},
				"GET /api/v1/executions?symbol=AAPL": map[string]any{"executions": execs},
			}
			for path, want := range fx.After {
				got, ok := reads[path]
				if !ok {
					if path != "GET /api/v1/symbols" {
						t.Errorf("fixture read %q is not checked", path)
					}
					continue
				}
				if g := asJSON(t, got); !reflect.DeepEqual(g, want) {
					t.Errorf("%s:\n got  %v\n want %v", path, g, want)
				}
			}
		})
	}
}

// A partial fill of a resting order updates it, and a later order trades
// with its remainder before newer orders at the same price.
func TestSubmitUpdatesRestingOrders(t *testing.T) {
	s := newTestService()
	mustSubmit(t, s, limit("alice", "SELL", 10, 19000)) // ord-1
	mustSubmit(t, s, limit("alice", "SELL", 5, 19000))  // ord-2

	res := mustSubmit(t, s, limit("bob", "BUY", 4, 19000)) // ord-3
	if res.Order.Status != orders.StatusFilled || len(res.Executions) != 1 || res.Executions[0].SellOrderID != "ord-1" {
		t.Fatalf("first buy: %+v", res)
	}
	alice, _ := s.Orders("alice", 0)
	got := map[string]orders.Status{}
	for _, o := range alice {
		got[o.ID] = o.Status
	}
	want := map[string]orders.Status{"ord-1": orders.StatusPartiallyFilled, "ord-2": orders.StatusNew}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alice statuses = %v, want %v", got, want)
	}

	res = mustSubmit(t, s, limit("bob", "BUY", 8, 19000)) // ord-4
	var fills []string
	for _, e := range res.Executions {
		fills = append(fills, fmt.Sprintf("%s:%d", e.SellOrderID, e.Quantity))
	}
	if want := []string{"ord-1:6", "ord-2:2"}; !slices.Equal(fills, want) {
		t.Fatalf("fills = %v, want %v", fills, want)
	}
	book, _ := s.OrderBook("AAPL", 0)
	if want := []Level{{Price: 19000, Quantity: 3, OrderCount: 1}}; !slices.Equal(book.Asks, want) {
		t.Fatalf("asks = %v, want %v", book.Asks, want)
	}
}

// IDs are unique, sequences increase, and FIFO follows acceptance order even
// when every order has the same timestamp.
func TestSequencesAndFIFOWithEqualTimestamps(t *testing.T) {
	s := New(Config{Now: func() time.Time { return t0 }})
	for i := range 3 {
		res := mustSubmit(t, s, limit("alice", "SELL", 1, 19000))
		if want := int64(i + 1); res.Order.Sequence != want || res.Order.ID != fmt.Sprintf("ord-%d", want) {
			t.Fatalf("order %d: id %s sequence %d", i, res.Order.ID, res.Order.Sequence)
		}
	}
	res := mustSubmit(t, s, limit("bob", "BUY", 3, 19000))
	for i, e := range res.Executions {
		if want := fmt.Sprintf("ord-%d", i+1); e.SellOrderID != want || e.Sequence != int64(i+1) || e.ID != fmt.Sprintf("exe-%d", i+1) {
			t.Errorf("execution %d = %+v, want sell %s sequence %d", i, e, want, i+1)
		}
	}
}

func TestRejectionsPublishNothing(t *testing.T) {
	tests := []struct {
		name  string
		req   SubmitRequest
		code  Code
		field string
	}{
		{"unknown user", limit("carol", "BUY", 1, 19000), CodeUnknownUser, "userId"},
		{"unknown user before field errors", SubmitRequest{UserID: "carol", Symbol: "aapl", Side: "BUY", Type: "LIMIT"}, CodeUnknownUser, "userId"},
		{"insufficient cash", limit("bob", "BUY", 600, 20000), CodeInsufficientCash, ""},
		{"insufficient shares", limit("bob", "SELL", 101, 19000), CodeInsufficientShares, ""},
		{"shares reserved by a resting order", limit("alice", "SELL", 96, 19500), CodeInsufficientShares, ""},
		{"missing user", SubmitRequest{Symbol: "AAPL", Side: "BUY", Type: "LIMIT", Price: 1, Quantity: 1}, CodeMissingField, "userId"},
		{"missing symbol", SubmitRequest{UserID: "bob", Side: "BUY", Type: "LIMIT", Price: 1, Quantity: 1}, CodeMissingField, "symbol"},
		{"missing side", SubmitRequest{UserID: "bob", Symbol: "AAPL", Type: "LIMIT", Price: 1, Quantity: 1}, CodeMissingField, "side"},
		{"missing type", SubmitRequest{UserID: "bob", Symbol: "AAPL", Side: "BUY", Price: 1, Quantity: 1}, CodeMissingField, "type"},
		{"unknown symbol", SubmitRequest{UserID: "bob", Symbol: "GOOG", Side: "BUY", Type: "LIMIT", Price: 1, Quantity: 1}, CodeUnknownSymbol, "symbol"},
		{"lowercase symbol", SubmitRequest{UserID: "bob", Symbol: "aapl", Side: "BUY", Type: "LIMIT", Price: 1, Quantity: 1}, CodeUnknownSymbol, "symbol"},
		{"symbol before invalid side", SubmitRequest{UserID: "bob", Symbol: "nope", Side: "buy", Type: "LIMIT", Price: 1, Quantity: 1}, CodeUnknownSymbol, "symbol"},
		{"lowercase side", SubmitRequest{UserID: "bob", Symbol: "AAPL", Side: "buy", Type: "LIMIT", Price: 1, Quantity: 1}, CodeInvalidSide, "side"},
		{"market order", SubmitRequest{UserID: "bob", Symbol: "AAPL", Side: "BUY", Type: "MARKET", Price: 1, Quantity: 1}, CodeUnsupportedOrderType, "type"},
		{"market order without limit price", SubmitRequest{UserID: "bob", Symbol: "AAPL", Side: "BUY", Type: "MARKET", Price: 0, Quantity: 1}, CodeUnsupportedOrderType, "type"},
		{"unknown type", SubmitRequest{UserID: "bob", Symbol: "AAPL", Side: "BUY", Type: "STOP", Price: 1, Quantity: 1}, CodeUnsupportedOrderType, "type"},
		{"zero price", limit("bob", "BUY", 1, 0), CodeInvalidPrice, "price"},
		{"price checked before quantity", limit("bob", "BUY", -1, -1), CodeInvalidPrice, "price"},
		{"negative quantity", limit("bob", "BUY", -1, 19000), CodeInvalidQuantity, "quantity"},
		{"price above maximum", limit("bob", "BUY", 1, MaxPrice+1), CodeInvalidPrice, "price"},
		{"quantity above maximum", limit("bob", "BUY", MaxQuantity+1, 1), CodeInvalidQuantity, "quantity"},
		{"price bound before invalid quantity", limit("bob", "BUY", 0, MaxPrice+1), CodeInvalidPrice, "price"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestService()
			mustSubmit(t, s, limit("alice", "SELL", 5, 19000))
			before := snapshot(t, s)

			_, err := s.Submit(tt.req)
			var e *Error
			if !errors.As(err, &e) || e.Code != tt.code || e.Field != tt.field {
				t.Fatalf("err = %v, want code %s field %s", err, tt.code, tt.field)
			}
			if after := snapshot(t, s); !reflect.DeepEqual(after, before) {
				t.Fatalf("state changed:\n before %+v\n after  %+v", before, after)
			}
			// The rejected request used no sequence number.
			if res := mustSubmit(t, s, limit("bob", "BUY", 1, 18000)); res.Order.Sequence != 2 {
				t.Fatalf("next sequence = %d, want 2", res.Order.Sequence)
			}
		})
	}
}

// A failure after the candidate book has matched publishes nothing, and the
// service keeps working afterwards.
func TestInternalFailurePublishesNothing(t *testing.T) {
	failNext := false
	s := New(Config{
		Now: func() time.Time { return t0 },
		ExecutionID: func(seq int64) string {
			if failNext && seq == 2 {
				failNext = false
				return "" // rejected by executions.Map
			}
			return fmt.Sprintf("exe-%d", seq)
		},
	})
	mustSubmit(t, s, limit("alice", "SELL", 2, 19000))
	mustSubmit(t, s, limit("alice", "SELL", 2, 19100))
	before := snapshot(t, s)

	failNext = true
	_, err := s.Submit(limit("bob", "BUY", 3, 19100)) // two fills; the second ID fails
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInternal {
		t.Fatalf("err = %v, want INTERNAL", err)
	}
	if after := snapshot(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("state changed:\n before %+v\n after  %+v", before, after)
	}

	res := mustSubmit(t, s, limit("bob", "BUY", 3, 19100))
	if res.Order.ID != "ord-3" || res.Order.Sequence != 3 || len(res.Executions) != 2 || res.Executions[0].ID != "exe-1" {
		t.Fatalf("retry = %+v", res)
	}
}

func TestDuplicateOrderIDPublishesNothing(t *testing.T) {
	s := New(Config{OrderID: func(int64) string { return "same" }})
	mustSubmit(t, s, limit("alice", "SELL", 2, 19000))
	before := snapshot(t, s)

	_, err := s.Submit(limit("bob", "BUY", 2, 19000))
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInternal {
		t.Fatalf("err = %v, want INTERNAL", err)
	}
	if after := snapshot(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("state changed")
	}
}

func TestSnapshotsAreCopies(t *testing.T) {
	s := newTestService()
	mustSubmit(t, s, limit("alice", "SELL", 5, 19000))
	res := mustSubmit(t, s, limit("bob", "BUY", 2, 19000))
	before := snapshot(t, s)

	res.Executions[0].Quantity = 99
	res.Order.Remaining = 99
	got := snapshot(t, s)
	got.Book.Asks[0].Quantity = 99
	got.Alice[0].Status = orders.StatusCanceled
	got.History[0].Quantity = 99

	if after := snapshot(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("caller changes reached the service:\n before %+v\n after  %+v", before, after)
	}
}

func TestReads(t *testing.T) {
	s := newTestService()
	mustSubmit(t, s, limit("alice", "BUY", 1, 18900))
	mustSubmit(t, s, limit("bob", "BUY", 2, 18900))
	mustSubmit(t, s, limit("alice", "BUY", 3, 18800))
	mustSubmit(t, s, limit("bob", "SELL", 4, 19100))
	mustSubmit(t, s, limit("alice", "SELL", 1, 18900)) // fills against alice's 18900 bid

	book, err := s.OrderBook("AAPL", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := BookSnapshot{Symbol: "AAPL", AsOfSequence: 5,
		Bids: []Level{{Price: 18900, Quantity: 2, OrderCount: 1}},
		Asks: []Level{{Price: 19100, Quantity: 4, OrderCount: 1}}}
	if !reflect.DeepEqual(book, want) {
		t.Errorf("book depth 1 = %+v, want %+v", book, want)
	}

	alice, _ := s.Orders("alice", 2)
	if len(alice) != 2 || alice[0].Sequence != 5 || alice[1].Sequence != 3 {
		t.Errorf("alice newest two = %+v", alice)
	}
	bobExecs, _ := s.Executions("AAPL", "bob", 0)
	if len(bobExecs) != 0 {
		t.Errorf("bob executions = %+v, want none", bobExecs)
	}
	if empty, _ := s.Executions("MSFT", "", 0); empty == nil || len(empty) != 0 {
		t.Errorf("MSFT executions = %#v, want empty non-nil slice", empty)
	}
	if empty, _ := s.OrderBook("MSFT", 0); empty.Bids == nil || empty.Asks == nil {
		t.Errorf("MSFT book = %#v, want empty non-nil sides", empty)
	}

	for name, call := range map[string]func() error{
		"orders without user":  func() error { _, err := s.Orders("", 0); return err },
		"limit too large":      func() error { _, err := s.Orders("alice", MaxLimit+1); return err },
		"negative limit":       func() error { _, err := s.Executions("AAPL", "", -1); return err },
		"executions no symbol": func() error { _, err := s.Executions("", "", 0); return err },
		"book without symbol":  func() error { _, err := s.OrderBook("", 0); return err },
		"depth too large":      func() error { _, err := s.OrderBook("AAPL", MaxDepth+1); return err },
	} {
		var e *Error
		if err := call(); !errors.As(err, &e) || e.Code != CodeInvalidQuery {
			t.Errorf("%s: err = %v, want INVALID_QUERY", name, err)
		}
	}
	for name, call := range map[string]func() error{
		"orders for unknown user":     func() error { _, err := s.Orders("carol", 0); return err },
		"executions for unknown user": func() error { _, err := s.Executions("AAPL", "carol", 0); return err },
	} {
		var e *Error
		if err := call(); !errors.As(err, &e) || e.Code != CodeUnknownUser || e.Field != "userId" {
			t.Errorf("%s: err = %v, want UNKNOWN_USER", name, err)
		}
	}
}

// Concurrent submissions and reads are race-clean (run with -race), and the
// final state is consistent: sequences are dense, every order's fills match
// the execution history, and the book holds exactly the open remainders.
func TestConcurrentSubmitsAndReads(t *testing.T) {
	s := New(Config{})
	const workers, perWorker = 8, 60
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for w := range workers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(w), 1))
			for range perWorker {
				side := []string{"BUY", "SELL"}[r.IntN(2)]
				user := []string{"alice", "bob"}[r.IntN(2)]
				_, err := s.Submit(limit(user, side, int64(1+r.IntN(5)), int64(18990+r.IntN(21))))
				var e *Error
				switch {
				case err == nil:
					accepted.Add(1)
				case errors.As(err, &e) && (e.Code == CodeInsufficientCash || e.Code == CodeInsufficientShares):
					// Expected: an account ran out of available cash or shares.
				default:
					t.Error(err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range perWorker {
				s.OrderBook("AAPL", 0)
				s.Orders("alice", 0)
				s.Executions("AAPL", "", 0)
				s.Accounts()
			}
		}()
	}
	wg.Wait()

	// Read everything directly: the public reads cap results at MaxLimit.
	s.mu.Lock()
	var all []Order
	for _, o := range s.orders {
		all = append(all, o)
	}
	history := slices.Clone(s.history)
	ledger := s.ledger
	s.mu.Unlock()
	if int64(len(all)) != accepted.Load() {
		t.Fatalf("%d orders stored, %d accepted", len(all), accepted.Load())
	}
	canonical := make([]orders.Order, len(all))
	for i, o := range all {
		canonical[i] = o.Order
	}
	if err := ledger.Reconcile(canonical); err != nil {
		t.Errorf("reservations disagree with open orders: %v", err)
	}
	checkConserved(t, s.Accounts())
	seqs := map[int64]bool{}
	filled := map[string]int64{}
	var restingBids, restingAsks int64
	for _, o := range all {
		seqs[o.Sequence] = true
		if o.Side == orders.SideBuy {
			restingBids += o.Remaining
		} else {
			restingAsks += o.Remaining
		}
	}
	for i := range int64(len(all)) {
		if !seqs[i+1] {
			t.Fatalf("missing order sequence %d", i+1)
		}
	}

	for i, e := range history {
		if e.Sequence != int64(i+1) {
			t.Fatalf("execution %d has sequence %d", i, e.Sequence)
		}
		filled[e.BuyOrderID] += e.Quantity
		filled[e.SellOrderID] += e.Quantity
	}
	for _, o := range all {
		if o.Filled() != filled[o.ID] {
			t.Errorf("order %s filled %d, executions say %d", o.ID, o.Filled(), filled[o.ID])
		}
	}

	book := must(s.OrderBook("AAPL", MaxDepth))
	var bookBids, bookAsks int64
	for _, l := range book.Bids {
		bookBids += l.Quantity
	}
	for _, l := range book.Asks {
		bookAsks += l.Quantity
	}
	if bookBids != restingBids || bookAsks != restingAsks {
		t.Errorf("book holds %d/%d, open orders %d/%d", bookBids, bookAsks, restingBids, restingAsks)
	}
	if len(book.Bids) > 0 && len(book.Asks) > 0 && book.Bids[0].Price >= book.Asks[0].Price {
		t.Errorf("book is crossed: %+v", book)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// checkConserved verifies that trading only moves cash and shares between
// accounts: totals equal the seeded totals, and no balance is negative.
func checkConserved(t *testing.T, snaps []accounts.Snapshot) {
	t.Helper()
	var cash int64
	shares := map[string]int64{}
	for _, a := range snaps {
		cash += a.Cash.Total
		if a.Cash.Available < 0 || a.Cash.Reserved < 0 {
			t.Errorf("%s cash %+v", a.UserID, a.Cash)
		}
		for _, p := range a.Positions {
			shares[p.Symbol] += p.Total
			if p.Available < 0 || p.Reserved < 0 {
				t.Errorf("%s %s %+v", a.UserID, p.Symbol, p.Balance)
			}
		}
	}
	n := int64(len(snaps))
	if cash != n*accounts.DemoCash {
		t.Errorf("total cash %d, want %d", cash, n*accounts.DemoCash)
	}
	for sym, total := range shares {
		if total != n*accounts.DemoShares {
			t.Errorf("total %s shares %d, want %d", sym, total, n*accounts.DemoShares)
		}
	}
}

func accountOf(t *testing.T, s *Service, userID string) accounts.Snapshot {
	t.Helper()
	for _, a := range s.Accounts() {
		if a.UserID == userID {
			return a
		}
	}
	t.Fatalf("no account %q", userID)
	return accounts.Snapshot{}
}

func position(a accounts.Snapshot, symbol string) accounts.Balance {
	for _, p := range a.Positions {
		if p.Symbol == symbol {
			return p.Balance
		}
	}
	return accounts.Balance{}
}

// A user's orders may trade with each other: the execution is recorded,
// totals do not change, and both reservations are released.
func TestSameAccountTradeNets(t *testing.T) {
	s := newTestService()
	mustSubmit(t, s, limit("alice", "SELL", 5, 19000))
	res := mustSubmit(t, s, limit("alice", "BUY", 5, 19100))

	if len(res.Executions) != 1 || res.Executions[0].BuyerUserID != "alice" || res.Executions[0].SellerUserID != "alice" {
		t.Fatalf("executions = %+v", res.Executions)
	}
	a := accountOf(t, s, "alice")
	if want := (accounts.Balance{Total: accounts.DemoCash, Available: accounts.DemoCash}); a.Cash != want {
		t.Errorf("cash = %+v, want %+v", a.Cash, want)
	}
	if got, want := position(a, "AAPL"), (accounts.Balance{Total: 100, Available: 100}); got != want {
		t.Errorf("AAPL = %+v, want %+v", got, want)
	}
}

// Resting orders keep their reservations, so later orders cannot spend the
// same cash or sell the same shares.
func TestReservationsBlockDoubleSpending(t *testing.T) {
	s := newTestService()
	// $2,000.00 × 50 reserves all of bob's $100,000.00.
	mustSubmit(t, s, limit("bob", "BUY", 50, 200000))
	if c := accountOf(t, s, "bob").Cash; c.Reserved != accounts.DemoCash || c.Available != 0 {
		t.Fatalf("bob cash = %+v", c)
	}
	var e *Error
	if _, err := s.Submit(limit("bob", "BUY", 1, 1)); !errors.As(err, &e) || e.Code != CodeInsufficientCash {
		t.Fatalf("second buy: err = %v, want INSUFFICIENT_CASH", err)
	}

	mustSubmit(t, s, limit("alice", "SELL", 60, 300000))
	mustSubmit(t, s, limit("alice", "SELL", 40, 300000))
	if _, err := s.Submit(limit("alice", "SELL", 1, 300000)); !errors.As(err, &e) || e.Code != CodeInsufficientShares {
		t.Fatalf("third sell: err = %v, want INSUFFICIENT_SHARES", err)
	}
	if p := position(accountOf(t, s, "alice"), "AAPL"); p.Reserved != 100 || p.Available != 0 {
		t.Fatalf("alice AAPL = %+v", p)
	}
}

// Concurrent orders that together need more than an account has: exactly
// the affordable number are accepted.
func TestConcurrentOverspendAndOversell(t *testing.T) {
	s := New(Config{})
	const tries = 20
	var wg sync.WaitGroup
	var buys, sells atomic.Int64
	for range tries {
		wg.Add(2)
		go func() {
			defer wg.Done()
			// $10,000.00 each: bob can afford 10.
			if _, err := s.Submit(limit("bob", "BUY", 1, 1000000)); err == nil {
				buys.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			// 10 shares each, priced above every bid: alice can sell 10 times.
			if _, err := s.Submit(limit("alice", "SELL", 10, 2000000)); err == nil {
				sells.Add(1)
			}
		}()
	}
	wg.Wait()
	if buys.Load() != 10 || sells.Load() != 10 {
		t.Fatalf("accepted %d buys and %d sells, want 10 and 10", buys.Load(), sells.Load())
	}
	if c := accountOf(t, s, "bob").Cash; c.Available != 0 || c.Reserved != accounts.DemoCash {
		t.Errorf("bob cash = %+v", c)
	}
	if p := position(accountOf(t, s, "alice"), "AAPL"); p.Available != 0 || p.Reserved != 100 {
		t.Errorf("alice AAPL = %+v", p)
	}
	checkConserved(t, s.Accounts())
}
