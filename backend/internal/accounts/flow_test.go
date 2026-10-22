package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/matching"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
	"github.com/co-rtex/tradegrid/backend/internal/risk"
)

// flow drives the ledger together with the real matching book, order
// lifecycle, and execution mapper, the way the exchange service (#33) is
// specified to: one lock serializes submissions, the ledger and order map are
// staged on copies and published together, and the published ledger is never
// changed after it is published.
//
// It is a test fixture for this package, not the exchange service. The book
// has no copy helper yet, so the flow reserves before touching the book and
// treats any later failure as a bug in the test.
type flow struct {
	mu      sync.Mutex
	book    *matching.Book
	orders  map[string]orders.Order
	history []executions.Record
	ledger  *Ledger
	calls   int64
	nextOrd int64
	nextExe int64
}

func newFlow() *flow {
	return &flow{book: matching.NewBook(), orders: map[string]orders.Order{}, ledger: Seed()}
}

// errBug marks a failure after the book changed, which a correct ledger never
// produces.
var errBug = errors.New("flow: failure after matching")

// submit accepts, matches, and settles one LIMIT order. IDs and timestamps
// follow the fixtures: ord-N, exe-N, and one second per submission from
// 2026-10-20T15:00:00Z.
func (f *flow) submit(userID, symbol string, side orders.Side, price, qty int64) (orders.Order, []executions.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	at := testTime.Add(time.Duration(f.calls) * time.Second)
	o, err := orders.New(orders.Params{
		ID: fmt.Sprintf("ord-%d", f.nextOrd+1), UserID: userID, Symbol: symbol, Side: side,
		Type: orders.TypeLimit, Price: price, Quantity: qty, CreatedAt: at,
	})
	if err != nil {
		return orders.Order{}, nil, err
	}
	ledger := f.ledger.Clone()
	if err := ledger.Reserve(o); err != nil {
		return orders.Order{}, nil, err
	}
	mo, err := matching.FromOrder(o)
	if err != nil {
		return orders.Order{}, nil, err
	}
	res := f.book.Submit(mo)
	if res.Err != nil {
		return orders.Order{}, nil, res.Err
	}

	meta := make([]executions.Metadata, len(res.Executions))
	for i := range meta {
		n := f.nextExe + int64(i) + 1
		meta[i] = executions.Metadata{ID: fmt.Sprintf("exe-%d", n), Sequence: n, ExecutedAt: at}
	}
	records, err := executions.Map(o, f.orders, res.Executions, meta)
	if err != nil {
		return orders.Order{}, nil, fmt.Errorf("%w: %w", errBug, err)
	}
	canonical := maps.Clone(f.orders)
	canonical[o.ID] = o
	for _, e := range res.Executions {
		for _, id := range []string{e.IncomingID, e.RestingID} {
			c := canonical[id]
			if err := c.Fill(e.Quantity, at); err != nil {
				return orders.Order{}, nil, fmt.Errorf("%w: %w", errBug, err)
			}
			canonical[id] = c
		}
	}
	if err := ledger.Settle(records, canonical); err != nil {
		return orders.Order{}, nil, fmt.Errorf("%w: %w", errBug, err)
	}

	f.orders = canonical
	f.history = append(f.history, records...)
	f.ledger = ledger
	f.nextOrd++
	f.nextExe += int64(len(records))
	return canonical[o.ID], records, nil
}

// published returns the current ledger. It is never changed again, so callers
// may read it without the lock.
func (f *flow) published() *Ledger {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ledger
}

// check verifies, under the lock, that the ledger, orders, executions, and
// book agree and that cash and shares are conserved.
func (f *flow) check(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	checkConserved(t, f.ledger)
	all := slices.Collect(maps.Values(f.orders))
	if err := f.ledger.Reconcile(all); err != nil {
		t.Errorf("reservations disagree with open orders: %v", err)
	}

	filled := map[string]int64{}
	for i, r := range f.history {
		if r.Sequence != int64(i)+1 {
			t.Errorf("execution %d has sequence %d", i, r.Sequence)
		}
		filled[r.BuyOrderID] += r.Quantity
		filled[r.SellOrderID] += r.Quantity
	}
	for id, o := range f.orders {
		if err := o.Validate(); err != nil {
			t.Errorf("order %s invalid: %v", id, err)
		}
		if o.Filled() != filled[id] {
			t.Errorf("order %s filled %d, executions say %d", o, o.Filled(), filled[id])
		}
	}

	resting := map[string]matching.BookEntry{}
	for _, symbol := range demoSymbols {
		bids, asks := f.book.Bids(symbol), f.book.Asks(symbol)
		if len(bids) > 0 && len(asks) > 0 && bids[0].Price >= asks[0].Price {
			t.Errorf("%s book is crossed: bid %d, ask %d", symbol, bids[0].Price, asks[0].Price)
		}
		for _, e := range append(bids, asks...) {
			resting[e.ID] = e
		}
	}
	for id, o := range f.orders {
		e, onBook := resting[id]
		switch {
		case o.Open() != onBook:
			t.Errorf("order %s open = %v, on book = %v", o, o.Open(), onBook)
		case onBook && (e.Remaining != o.Remaining || e.Price != o.Price):
			t.Errorf("order %s disagrees with book entry %+v", o, e)
		}
	}
}

// TestFixtures replays docs/fixtures and compares the accounts, the incoming
// order, its executions, the AAPL book, and each account's orders with the
// fixture's expected results.
func TestFixtures(t *testing.T) {
	type request struct {
		UserID   string `json:"userId"`
		Symbol   string `json:"symbol"`
		Side     string `json:"side"`
		Price    int64  `json:"price"`
		Quantity int64  `json:"quantity"`
	}
	type level struct {
		Price      int64 `json:"price"`
		Quantity   int64 `json:"quantity"`
		OrderCount int   `json:"orderCount"`
	}
	type fixture struct {
		Setup   []struct{ Body request } `json:"setup"`
		Request struct{ Body request }   `json:"request"`
		Resp    struct {
			Status int `json:"status"`
			Body   struct {
				Order      *orders.Order       `json:"order"`
				Executions []executions.Record `json:"executions"`
				Error      struct{ Code string }
			} `json:"body"`
		} `json:"response"`
		After struct {
			Accounts struct{ Accounts []Snapshot } `json:"GET /api/v1/accounts"`
			Book     struct {
				Bids []level `json:"bids"`
				Asks []level `json:"asks"`
			} `json:"GET /api/v1/orderbook?symbol=AAPL"`
			AliceOrders struct{ Orders []orders.Order }          `json:"GET /api/v1/orders?userId=alice"`
			BobOrders   struct{ Orders []orders.Order }          `json:"GET /api/v1/orders?userId=bob"`
			Executions  struct{ Executions []executions.Record } `json:"GET /api/v1/executions?symbol=AAPL"`
		} `json:"after"`
	}
	load := func(t *testing.T, name string) fixture {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		var f fixture
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		return f
	}
	submit := func(f *flow, r request) (orders.Order, []executions.Record, error) {
		side, err := orders.ParseSide(r.Side)
		if err != nil {
			return orders.Order{}, nil, err
		}
		return f.submit(r.UserID, r.Symbol, side, r.Price, r.Quantity)
	}
	levels := func(entries []matching.BookEntry) []level {
		out := []level{}
		for _, e := range entries {
			if n := len(out); n > 0 && out[n-1].Price == e.Price {
				out[n-1].Quantity += e.Remaining
				out[n-1].OrderCount++
				continue
			}
			out = append(out, level{Price: e.Price, Quantity: e.Remaining, OrderCount: 1})
		}
		return out
	}
	ordersOf := func(f *flow, userID string) []orders.Order {
		out := []orders.Order{}
		for _, o := range f.orders {
			if o.UserID == userID {
				out = append(out, o)
			}
		}
		// Newest first. IDs are ord-N with single-digit N in the fixtures.
		slices.SortFunc(out, func(a, b orders.Order) int { return strings.Compare(b.ID, a.ID) })
		return out
	}

	t.Run("00-seed.json", func(t *testing.T) {
		want := load(t, "00-seed.json").After.Accounts.Accounts
		if got := Seed().Snapshot(); !reflect.DeepEqual(got, want) {
			t.Errorf("seed accounts =\n%+v\nwant\n%+v", got, want)
		}
	})

	codes := map[string]error{
		"INSUFFICIENT_CASH":   risk.ErrInsufficientCash,
		"INSUFFICIENT_SHARES": risk.ErrInsufficientShares,
		"LIMIT_EXCEEDED":      risk.ErrLimitExceeded,
		"UNKNOWN_USER":        ErrUnknownUser,
	}
	for _, name := range []string{"01-no-fill.json", "02-full-fill.json", "03-partial-fill.json", "04-risk-rejection.json"} {
		t.Run(name, func(t *testing.T) {
			fx := load(t, name)
			f := newFlow()
			for _, s := range fx.Setup {
				if _, _, err := submit(f, s.Body); err != nil {
					t.Fatalf("setup %+v: %v", s.Body, err)
				}
			}
			before := f.published().Snapshot()
			order, records, err := submit(f, fx.Request.Body)

			if fx.Resp.Status == 201 {
				if err != nil {
					t.Fatalf("submit: %v", err)
				}
				if !reflect.DeepEqual(order, *fx.Resp.Body.Order) {
					t.Errorf("order =\n%+v\nwant\n%+v", order, *fx.Resp.Body.Order)
				}
				if !reflect.DeepEqual(records, fx.Resp.Body.Executions) {
					t.Errorf("executions =\n%+v\nwant\n%+v", records, fx.Resp.Body.Executions)
				}
			} else {
				want, ok := codes[fx.Resp.Body.Error.Code]
				if !ok {
					t.Fatalf("fixture uses unhandled code %s", fx.Resp.Body.Error.Code)
				}
				if !errors.Is(err, want) {
					t.Fatalf("submit = %v, want %v", err, want)
				}
				if got := f.published().Snapshot(); !reflect.DeepEqual(got, before) {
					t.Errorf("rejected order changed accounts:\n%+v", got)
				}
			}

			if got, want := f.published().Snapshot(), fx.After.Accounts.Accounts; !reflect.DeepEqual(got, want) {
				t.Errorf("accounts =\n%+v\nwant\n%+v", got, want)
			}
			if got, want := levels(f.book.Bids("AAPL")), fx.After.Book.Bids; !reflect.DeepEqual(got, want) {
				t.Errorf("bids = %+v, want %+v", got, want)
			}
			if got, want := levels(f.book.Asks("AAPL")), fx.After.Book.Asks; !reflect.DeepEqual(got, want) {
				t.Errorf("asks = %+v, want %+v", got, want)
			}
			if got, want := ordersOf(f, "alice"), fx.After.AliceOrders.Orders; !reflect.DeepEqual(got, want) {
				t.Errorf("alice orders =\n%+v\nwant\n%+v", got, want)
			}
			if got, want := ordersOf(f, "bob"), fx.After.BobOrders.Orders; !reflect.DeepEqual(got, want) {
				t.Errorf("bob orders =\n%+v\nwant\n%+v", got, want)
			}
			history := append([]executions.Record{}, f.history...)
			slices.Reverse(history)
			if want := fx.After.Executions.Executions; !reflect.DeepEqual(history, want) {
				t.Errorf("executions =\n%+v\nwant\n%+v", history, want)
			}
			f.check(t)
		})
	}
}

// TestFlowSameAccountTrade checks that a self-match is recorded as a normal
// execution and leaves the account exactly as it was.
func TestFlowSameAccountTrade(t *testing.T) {
	f := newFlow()
	if _, _, err := f.submit("alice", "AAPL", sell, 19000, 10); err != nil {
		t.Fatal(err)
	}
	o, records, err := f.submit("alice", "AAPL", buy, 19100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != orders.StatusFilled || len(records) != 1 {
		t.Fatalf("order %s, %d executions", o, len(records))
	}
	if r := records[0]; r.BuyerUserID != "alice" || r.SellerUserID != "alice" || r.Price != 19000 {
		t.Errorf("execution = %+v", r)
	}
	if got, want := f.published().Snapshot(), Seed().Snapshot(); !reflect.DeepEqual(got, want) {
		t.Errorf("accounts after a self-match =\n%+v\nwant the seed\n%+v", got, want)
	}
	f.check(t)
}

// TestFlowMultiLevelPartialFills sweeps several resting orders from both
// accounts, leaving one resting order partly filled with its reservation
// retained.
func TestFlowMultiLevelPartialFills(t *testing.T) {
	f := newFlow()
	for _, s := range []struct {
		user  string
		price int64
		qty   int64
	}{{"alice", 19000, 10}, {"bob", 19000, 20}, {"alice", 19100, 30}} {
		if _, _, err := f.submit(s.user, "AAPL", sell, s.price, s.qty); err != nil {
			t.Fatal(err)
		}
	}
	// Bob buys 45 at $191.50: 10 from alice and 20 from himself at $190.00,
	// then 15 from alice at $191.00.
	o, records, err := f.submit("bob", "AAPL", buy, 19150, 45)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != orders.StatusFilled || len(records) != 3 {
		t.Fatalf("order %s, %d executions", o, len(records))
	}
	wantSellers := []string{"ord-1", "ord-2", "ord-3"}
	for i, r := range records {
		if r.SellOrderID != wantSellers[i] {
			t.Errorf("execution %d sold by %s, want %s (price-time priority)", i, r.SellOrderID, wantSellers[i])
		}
	}
	// Bob reserved 45 × 19150 and paid 10×19000 to alice, 20×19000 to himself,
	// and 15×19100 to alice.
	if got, want := account(t, f.published(), "bob").Cash, bal(10000000-190000-286500, 0); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	if got, want := position(t, f.published(), "bob", "AAPL"), bal(125, 0); got != want {
		t.Errorf("bob AAPL = %+v, want %+v", got, want)
	}
	if got, want := account(t, f.published(), "alice").Cash, bal(10000000+190000+286500, 0); got != want {
		t.Errorf("alice cash = %+v, want %+v", got, want)
	}
	// ord-3 rests with 15 of 30 left, so 15 shares stay reserved.
	if got, want := position(t, f.published(), "alice", "AAPL"), bal(75, 15); got != want {
		t.Errorf("alice AAPL = %+v, want %+v", got, want)
	}
	if got := f.orders["ord-3"]; got.Status != orders.StatusPartiallyFilled || got.Remaining != 15 {
		t.Errorf("ord-3 = %s", got)
	}
	f.check(t)
}

// TestFlowRejectionsLeaveStateUnchanged checks that every kind of rejected
// submission leaves the book, orders, executions, and accounts as they were.
func TestFlowRejectionsLeaveStateUnchanged(t *testing.T) {
	f := newFlow()
	if _, _, err := f.submit("alice", "AAPL", sell, 19000, 60); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.submit("bob", "AAPL", buy, 18000, 500); err != nil {
		t.Fatal(err)
	}
	type state struct {
		accounts []Snapshot
		orders   map[string]orders.Order
		history  []executions.Record
		bids     []matching.BookEntry
		asks     []matching.BookEntry
	}
	capture := func() state {
		return state{f.published().Snapshot(), maps.Clone(f.orders), slices.Clone(f.history), f.book.Bids("AAPL"), f.book.Asks("AAPL")}
	}
	before := capture()

	tests := []struct {
		name  string
		user  string
		side  orders.Side
		price int64
		qty   int64
		want  error
	}{
		{"overspend after an outstanding BUY", "bob", buy, 19000, 60, risk.ErrInsufficientCash},
		{"oversell after an outstanding SELL", "alice", sell, 19000, 41, risk.ErrInsufficientShares},
		{"unknown user", "carol", buy, 19000, 1, ErrUnknownUser},
		{"notional past the aggregate bound", "bob", buy, risk.MaxAggregate, 2, risk.ErrLimitExceeded},
		{"zero quantity", "bob", buy, 19000, 0, orders.ErrInvalidQuantity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := f.submit(tt.user, "AAPL", tt.side, tt.price, tt.qty)
			if !errors.Is(err, tt.want) {
				t.Fatalf("submit = %v, want %v", err, tt.want)
			}
			if got := capture(); !reflect.DeepEqual(got, before) {
				t.Errorf("rejected submission changed state:\n%+v\nwas\n%+v", got, before)
			}
		})
	}
	// Exactly what is left still fits.
	if _, _, err := f.submit("alice", "AAPL", sell, 19000, 40); err != nil {
		t.Errorf("selling the remaining 40 shares: %v", err)
	}
	f.check(t)
}

// TestFlowInjectedFailureRollsBack fails the last step of a submission after
// a candidate ledger has been reserved and settled, and checks that nothing
// is published.
func TestFlowInjectedFailureRollsBack(t *testing.T) {
	f := newFlow()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19100, 10)
	before := f.published().Snapshot()

	inject := errors.New("injected failure")
	stage := func() error {
		candidate := f.published().Clone()
		for _, o := range []orders.Order{s, b} {
			if err := candidate.Reserve(o); err != nil {
				return err
			}
		}
		if err := candidate.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
			return err
		}
		return inject // fails before publication
	}
	if err := stage(); !errors.Is(err, inject) {
		t.Fatalf("stage = %v", err)
	}
	if got := f.published().Snapshot(); !reflect.DeepEqual(got, before) {
		t.Errorf("failed submission was published:\n%+v", got)
	}
}

// TestFlowSimulation runs a deterministic random sequence of submissions from
// both accounts and checks every invariant after each one.
func TestFlowSimulation(t *testing.T) {
	f := newFlow()
	rng := rand.New(rand.NewPCG(35, 2026))
	accepted, rejected, fills := 0, 0, 0
	for step := range 1500 {
		user := []string{"alice", "bob"}[rng.IntN(2)]
		symbol := demoSymbols[rng.IntN(len(demoSymbols))]
		side := []orders.Side{buy, sell}[rng.IntN(2)]
		price := 18000 + rng.Int64N(2001)
		qty := 1 + rng.Int64N(40)
		_, records, err := f.submit(user, symbol, side, price, qty)
		switch {
		case errors.Is(err, errBug):
			t.Fatalf("step %d: %v", step, err)
		case errors.Is(err, risk.ErrInsufficientCash), errors.Is(err, risk.ErrInsufficientShares):
			rejected++
		case err != nil:
			t.Fatalf("step %d: unexpected error %v", step, err)
		default:
			accepted++
			fills += len(records)
		}
		f.check(t)
		if t.Failed() {
			t.Fatalf("invariants broken at step %d", step)
		}
	}
	if accepted == 0 || rejected == 0 || fills == 0 {
		t.Errorf("simulation too narrow: %d accepted, %d rejected, %d fills", accepted, rejected, fills)
	}
}

// TestConcurrentOverspendAndOversell races many orders that each fit alone
// but not together. Exactly as many are accepted as the balances allow.
func TestConcurrentOverspendAndOversell(t *testing.T) {
	f := newFlow()
	var wg sync.WaitGroup
	results := make(chan error, 40)
	for range 20 {
		wg.Go(func() {
			// $30,000 each: three fit in $100,000.
			_, _, err := f.submit("alice", "MSFT", buy, 300000, 10)
			results <- err
		})
		wg.Go(func() {
			// 30 shares each: three fit in 100.
			_, _, err := f.submit("bob", "AAPL", sell, 50000, 30)
			results <- err
		})
	}
	wg.Wait()
	close(results)

	ok, cash, shares := 0, 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, risk.ErrInsufficientCash):
			cash++
		case errors.Is(err, risk.ErrInsufficientShares):
			shares++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 6 || cash != 17 || shares != 17 {
		t.Errorf("accepted %d, cash rejections %d, share rejections %d; want 6, 17, 17", ok, cash, shares)
	}
	if got := account(t, f.published(), "alice").Cash; got != bal(10000000, 9000000) {
		t.Errorf("alice cash = %+v", got)
	}
	if got := position(t, f.published(), "bob", "AAPL"); got != bal(100, 90) {
		t.Errorf("bob AAPL = %+v", got)
	}
	f.check(t)
}

// TestConcurrentTradingIsConsistent runs crossing submissions from several
// goroutines while readers take snapshots without the lock. Every snapshot
// must be conserved, and the final state must satisfy every invariant.
func TestConcurrentTradingIsConsistent(t *testing.T) {
	f := newFlow()
	var writers, readers sync.WaitGroup
	done := make(chan struct{})
	bugs := make(chan error, 8)

	for w := range 8 {
		writers.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 35))
			for range 150 {
				user := []string{"alice", "bob"}[rng.IntN(2)]
				side := []orders.Side{buy, sell}[rng.IntN(2)]
				_, _, err := f.submit(user, demoSymbols[rng.IntN(2)], side, 18900+rng.Int64N(201), 1+rng.Int64N(20))
				if err != nil && !errors.Is(err, risk.ErrInsufficientCash) && !errors.Is(err, risk.ErrInsufficientShares) {
					bugs <- err
					return
				}
			}
		})
	}
	snapshots := 0
	var mu sync.Mutex
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				l := f.published()
				checkConserved(t, l)
				mu.Lock()
				snapshots++
				mu.Unlock()
			}
		})
	}
	writers.Wait()
	close(done)
	readers.Wait()
	close(bugs)
	for err := range bugs {
		t.Errorf("writer: %v", err)
	}
	if snapshots == 0 {
		t.Error("readers took no snapshots")
	}
	if len(f.history) == 0 {
		t.Error("no trades happened")
	}
	f.check(t)
}
