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
	"testing"
	"time"

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
	return state{book, alice, bob, hist}
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

// TestFixtures replays the contract fixtures that do not need accounts (#35)
// and compares responses and reads with the documented JSON.
func TestFixtures(t *testing.T) {
	for _, name := range []string{"01-no-fill.json", "02-full-fill.json", "03-partial-fill.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "fixtures", name))
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Setup    []struct{ Body SubmitRequest }
				Request  struct{ Body SubmitRequest }
				Response struct {
					Status int
					Body   any
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
			res := mustSubmit(t, s, fx.Request.Body)
			if got := asJSON(t, res); !reflect.DeepEqual(got, fx.Response.Body) {
				t.Errorf("response:\n got  %v\n want %v", got, fx.Response.Body)
			}

			book, _ := s.OrderBook("AAPL", 0)
			alice, _ := s.Orders("alice", 0)
			bob, _ := s.Orders("bob", 0)
			execs, _ := s.Executions("AAPL", "", 0)
			for path, got := range map[string]any{
				"GET /api/v1/orderbook?symbol=AAPL":  book,
				"GET /api/v1/orders?userId=alice":    map[string]any{"orders": alice},
				"GET /api/v1/orders?userId=bob":      map[string]any{"orders": bob},
				"GET /api/v1/executions?symbol=AAPL": map[string]any{"executions": execs},
			} {
				if g := asJSON(t, got); !reflect.DeepEqual(g, fx.After[path]) {
					t.Errorf("%s:\n got  %v\n want %v", path, g, fx.After[path])
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
}

// Concurrent submissions and reads are race-clean (run with -race), and the
// final state is consistent: sequences are dense, every order's fills match
// the execution history, and the book holds exactly the open remainders.
func TestConcurrentSubmitsAndReads(t *testing.T) {
	s := New(Config{})
	const workers, perWorker = 8, 60
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(w), 1))
			for range perWorker {
				side := []string{"BUY", "SELL"}[r.IntN(2)]
				user := []string{"alice", "bob"}[r.IntN(2)]
				if _, err := s.Submit(limit(user, side, int64(1+r.IntN(5)), int64(18990+r.IntN(21)))); err != nil {
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
	s.mu.Unlock()
	if len(all) != workers*perWorker {
		t.Fatalf("%d orders, want %d", len(all), workers*perWorker)
	}
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
