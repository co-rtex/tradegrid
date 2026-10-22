package accounts

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
	"github.com/co-rtex/tradegrid/backend/internal/risk"
)

const (
	buy  = orders.SideBuy
	sell = orders.SideSell
)

var testTime = time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)

func newOrder(t *testing.T, id, userID, symbol string, side orders.Side, price, qty int64) orders.Order {
	t.Helper()
	o, err := orders.New(orders.Params{
		ID: id, UserID: userID, Symbol: symbol, Side: side, Type: orders.TypeLimit,
		Price: price, Quantity: qty, CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("orders.New(%s): %v", id, err)
	}
	return o
}

func mustReserve(t *testing.T, l *Ledger, o orders.Order) {
	t.Helper()
	if err := l.Reserve(o); err != nil {
		t.Fatalf("Reserve(%s): %v", o, err)
	}
}

// record builds the execution of qty shares at price between buy and sell.
func record(id string, b, s orders.Order, price, qty int64) executions.Record {
	return executions.Record{
		ID: id, Sequence: 1, ExecutedAt: testTime, Symbol: b.Symbol, Price: price, Quantity: qty,
		BuyOrderID: b.ID, SellOrderID: s.ID, BuyerUserID: b.UserID, SellerUserID: s.UserID,
	}
}

func byID(os ...orders.Order) map[string]orders.Order {
	m := make(map[string]orders.Order, len(os))
	for _, o := range os {
		m[o.ID] = o
	}
	return m
}

func account(t *testing.T, l *Ledger, userID string) Snapshot {
	t.Helper()
	for _, a := range l.Snapshot() {
		if a.UserID == userID {
			return a
		}
	}
	t.Fatalf("no account %q", userID)
	return Snapshot{}
}

func position(t *testing.T, l *Ledger, userID, symbol string) Balance {
	t.Helper()
	for _, p := range account(t, l, userID).Positions {
		if p.Symbol == symbol {
			return p.Balance
		}
	}
	t.Fatalf("%s has no %s position", userID, symbol)
	return Balance{}
}

func bal(total, reserved int64) Balance {
	return Balance{Total: total, Reserved: reserved, Available: total - reserved}
}

// checkConserved fails unless the ledger still holds exactly the seeded cash
// and shares in total and every balance is internally consistent.
func checkConserved(t *testing.T, l *Ledger) {
	t.Helper()
	var cash int64
	shares := map[string]int64{}
	for _, a := range l.Snapshot() {
		cash += a.Cash.Total
		for _, b := range append([]Position{{Symbol: "", Balance: a.Cash}}, a.Positions...) {
			if b.Reserved < 0 || b.Available < 0 || b.Total != b.Reserved+b.Available {
				t.Errorf("%s %q balance inconsistent: %+v", a.UserID, b.Symbol, b.Balance)
			}
		}
		for _, p := range a.Positions {
			shares[p.Symbol] += p.Total
		}
	}
	n := int64(len(demoAccounts))
	if cash != n*DemoCash {
		t.Errorf("total cash %d, want %d", cash, n*DemoCash)
	}
	for _, symbol := range demoSymbols {
		if shares[symbol] != n*DemoShares {
			t.Errorf("total %s shares %d, want %d", symbol, shares[symbol], n*DemoShares)
		}
	}
}

func TestSeedMatchesContract(t *testing.T) {
	seeded := func(userID, name string) Snapshot {
		return Snapshot{
			UserID: userID, DisplayName: name, Cash: bal(10000000, 0),
			Positions: []Position{{"AAPL", bal(100, 0)}, {"MSFT", bal(100, 0)}},
		}
	}
	want := []Snapshot{seeded("alice", "Alice (demo)"), seeded("bob", "Bob (demo)")}
	if got := Seed().Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Seed().Snapshot() =\n%+v\nwant\n%+v", got, want)
	}
	for _, id := range []string{"alice", "bob"} {
		if !Seed().Has(id) {
			t.Errorf("Has(%q) = false", id)
		}
	}
	for _, id := range []string{"", "carol", "Alice"} {
		if Seed().Has(id) {
			t.Errorf("Has(%q) = true", id)
		}
	}
}

func TestSeedReturnsIndependentLedgers(t *testing.T) {
	first := Seed()
	mustReserve(t, first, newOrder(t, "ord-1", "alice", "AAPL", buy, 19000, 10))
	mustReserve(t, first, newOrder(t, "ord-2", "bob", "MSFT", sell, 40000, 5))
	second := Seed()
	if got := account(t, second, "alice").Cash; got != bal(DemoCash, 0) {
		t.Errorf("a new Seed() saw an earlier ledger's reservation: %+v", got)
	}
	if got := position(t, second, "bob", "MSFT"); got != bal(DemoShares, 0) {
		t.Errorf("a new Seed() saw an earlier ledger's reservation: %+v", got)
	}
}

func TestReserveRecordsExactReservations(t *testing.T) {
	l := Seed()
	mustReserve(t, l, newOrder(t, "ord-1", "bob", "AAPL", buy, 19050, 25))
	mustReserve(t, l, newOrder(t, "ord-2", "alice", "AAPL", sell, 19000, 10))

	if got, want := account(t, l, "bob").Cash, bal(10000000, 476250); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	if got, want := position(t, l, "alice", "AAPL"), bal(100, 10); got != want {
		t.Errorf("alice AAPL = %+v, want %+v", got, want)
	}
	if got, want := account(t, l, "alice").Cash, bal(10000000, 0); got != want {
		t.Errorf("SELL changed alice cash: %+v", got)
	}
	if got, want := position(t, l, "bob", "AAPL"), bal(100, 0); got != want {
		t.Errorf("BUY changed bob AAPL: %+v", got)
	}
	checkConserved(t, l)
}

func TestReserveRejectsWithoutChange(t *testing.T) {
	market := newOrder(t, "ord-m", "bob", "AAPL", buy, 1, 1)
	market.Type, market.Price = orders.TypeMarket, 0
	partly := newOrder(t, "ord-p", "bob", "AAPL", buy, 100, 10)
	if err := partly.Fill(4, testTime); err != nil {
		t.Fatal(err)
	}
	invalid := newOrder(t, "ord-i", "bob", "AAPL", buy, 100, 10)
	invalid.ID = ""

	tests := []struct {
		name  string
		order orders.Order
		want  error
	}{
		{"fixture 04: 600 at $200 with $100,000", newOrder(t, "ord-1", "bob", "AAPL", buy, 20000, 600), risk.ErrInsufficientCash},
		{"buy one cent more than cash", newOrder(t, "ord-1", "bob", "AAPL", buy, 10000001, 1), risk.ErrInsufficientCash},
		{"sell one share more than held", newOrder(t, "ord-1", "alice", "MSFT", sell, 100, 101), risk.ErrInsufficientShares},
		{"sell a symbol not held", newOrder(t, "ord-1", "alice", "TSLA", sell, 100, 1), risk.ErrInsufficientShares},
		{"unknown user", newOrder(t, "ord-1", "carol", "AAPL", buy, 100, 1), ErrUnknownUser},
		{"user id is case sensitive", newOrder(t, "ord-1", "Alice", "AAPL", buy, 100, 1), ErrUnknownUser},
		{"notional overflows int64", newOrder(t, "ord-1", "bob", "AAPL", buy, math.MaxInt64, 2), risk.ErrLimitExceeded},
		{"notional exceeds MaxAggregate", newOrder(t, "ord-1", "bob", "AAPL", buy, risk.MaxAggregate, 2), risk.ErrLimitExceeded},
		{"market order", market, ErrInvalidOrder},
		{"order with fills", partly, ErrInvalidOrder},
		{"structurally invalid order", invalid, ErrInvalidOrder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Seed()
			before := l.Clone()
			err := l.Reserve(tt.order)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Reserve = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(l, before) {
				t.Errorf("rejected Reserve changed the ledger:\n%+v\nwas\n%+v", l.Snapshot(), before.Snapshot())
			}
		})
	}
}

func TestReserveExactlyAvailable(t *testing.T) {
	l := Seed()
	mustReserve(t, l, newOrder(t, "ord-1", "alice", "AAPL", buy, 10000000, 1))
	mustReserve(t, l, newOrder(t, "ord-2", "alice", "AAPL", sell, 1, 100))
	if got := account(t, l, "alice").Cash; got != bal(10000000, 10000000) {
		t.Errorf("alice cash = %+v, want fully reserved", got)
	}
	if got := position(t, l, "alice", "AAPL"); got != bal(100, 100) {
		t.Errorf("alice AAPL = %+v, want fully reserved", got)
	}
	if err := l.Reserve(newOrder(t, "ord-3", "alice", "MSFT", buy, 1, 1)); !errors.Is(err, risk.ErrInsufficientCash) {
		t.Errorf("BUY with no available cash = %v, want ErrInsufficientCash", err)
	}
	if err := l.Reserve(newOrder(t, "ord-4", "alice", "AAPL", sell, 1, 1)); !errors.Is(err, risk.ErrInsufficientShares) {
		t.Errorf("SELL with no available shares = %v, want ErrInsufficientShares", err)
	}
}

// Outstanding orders hold their reservations, so later orders can only use
// what is still available even though totals are unchanged.
func TestReserveAcrossOutstandingOrders(t *testing.T) {
	l := Seed()
	// Two BUYs of $40,000 each fit in $100,000; a third does not, but $20,000 does.
	mustReserve(t, l, newOrder(t, "ord-1", "alice", "AAPL", buy, 400000, 10))
	mustReserve(t, l, newOrder(t, "ord-2", "alice", "MSFT", buy, 400000, 10))
	if err := l.Reserve(newOrder(t, "ord-3", "alice", "AAPL", buy, 400000, 10)); !errors.Is(err, risk.ErrInsufficientCash) {
		t.Fatalf("third BUY = %v, want ErrInsufficientCash", err)
	}
	mustReserve(t, l, newOrder(t, "ord-4", "alice", "AAPL", buy, 200000, 10))
	if got := account(t, l, "alice").Cash; got != bal(10000000, 10000000) {
		t.Errorf("alice cash = %+v, want fully reserved", got)
	}

	// Sixty plus forty shares exhaust AAPL; MSFT and the other account are unaffected.
	mustReserve(t, l, newOrder(t, "ord-5", "bob", "AAPL", sell, 19000, 60))
	mustReserve(t, l, newOrder(t, "ord-6", "bob", "AAPL", sell, 19500, 40))
	if err := l.Reserve(newOrder(t, "ord-7", "bob", "AAPL", sell, 19000, 1)); !errors.Is(err, risk.ErrInsufficientShares) {
		t.Fatalf("oversell = %v, want ErrInsufficientShares", err)
	}
	mustReserve(t, l, newOrder(t, "ord-8", "bob", "MSFT", sell, 40000, 100))
	mustReserve(t, l, newOrder(t, "ord-9", "alice", "AAPL", sell, 19000, 100))
	checkConserved(t, l)
}

func TestSettleFullFillWithPriceImprovement(t *testing.T) {
	// docs/fixtures/02-full-fill.json
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19100, 10)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if got := account(t, l, "bob").Cash; got != bal(10000000, 191000) {
		t.Fatalf("bob cash before = %+v", got)
	}

	if err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	// Bob reserved $1,910.00 but paid $1,900.00; the $10.00 difference is available again.
	if got, want := account(t, l, "bob").Cash, bal(9810000, 0); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	if got, want := account(t, l, "alice").Cash, bal(10190000, 0); got != want {
		t.Errorf("alice cash = %+v, want %+v", got, want)
	}
	if got, want := position(t, l, "bob", "AAPL"), bal(110, 0); got != want {
		t.Errorf("bob AAPL = %+v, want %+v", got, want)
	}
	if got, want := position(t, l, "alice", "AAPL"), bal(90, 0); got != want {
		t.Errorf("alice AAPL = %+v, want %+v", got, want)
	}
	checkConserved(t, l)
}

func TestSettlePartialBuyRetainsReservation(t *testing.T) {
	// docs/fixtures/03-partial-fill.json
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19050, 25)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	// 15 unfilled shares at $190.50 keep $2,857.50 reserved.
	if got, want := account(t, l, "bob").Cash, bal(9810000, 285750); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	if got, want := account(t, l, "alice").Cash, bal(10190000, 0); got != want {
		t.Errorf("alice cash = %+v, want %+v", got, want)
	}
	checkConserved(t, l)
}

func TestSettlePartialSellRetainsShareReservation(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 30)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19000, 10)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if got, want := position(t, l, "alice", "AAPL"), bal(90, 20); got != want {
		t.Errorf("alice AAPL = %+v, want %+v", got, want)
	}
	if got, want := account(t, l, "bob").Cash, bal(9810000, 0); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	checkConserved(t, l)
}

// A BUY that sweeps two price levels pays each level's price and releases its
// whole reservation at its own limit.
func TestSettleSweepAcrossLevels(t *testing.T) {
	l := Seed()
	s1 := newOrder(t, "ord-1", "alice", "AAPL", sell, 18900, 5)
	s2 := newOrder(t, "ord-2", "alice", "AAPL", sell, 19000, 5)
	b := newOrder(t, "ord-3", "bob", "AAPL", buy, 19100, 10)
	for _, o := range []orders.Order{s1, s2, b} {
		mustReserve(t, l, o)
	}
	recs := []executions.Record{record("exe-1", b, s1, 18900, 5), record("exe-2", b, s2, 19000, 5)}
	if err := l.Settle(recs, byID(s1, s2, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if got, want := account(t, l, "bob").Cash, bal(10000000-94500-95000, 0); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	if got, want := account(t, l, "alice").Cash, bal(10000000+94500+95000, 0); got != want {
		t.Errorf("alice cash = %+v, want %+v", got, want)
	}
	if got, want := position(t, l, "alice", "AAPL"), bal(90, 0); got != want {
		t.Errorf("alice AAPL = %+v, want %+v", got, want)
	}
	checkConserved(t, l)
}

// Same-account trades are allowed: totals net to zero and both reservations
// are released (docs/api.md, "Same-account trades").
func TestSettleSameAccount(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "alice", "AAPL", buy, 19100, 10)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if got := account(t, l, "alice"); got.Cash != bal(10000000, 191000) || got.Positions[0].Balance != bal(100, 10) {
		t.Fatalf("alice before = %+v", got)
	}
	if err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if got, want := l.Snapshot(), Seed().Snapshot(); !reflect.DeepEqual(got, want) {
		t.Errorf("after a same-account trade =\n%+v\nwant the seeded ledger\n%+v", got, want)
	}
}

// A same-account partial fill releases only the matched quantity's
// reservations; the rest stays reserved for the resting remainder.
func TestSettleSameAccountPartial(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "bob", "MSFT", sell, 40000, 30)
	b := newOrder(t, "ord-2", "bob", "MSFT", buy, 41000, 10)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if err := l.Settle([]executions.Record{record("exe-1", b, s, 40000, 10)}, byID(s, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if got, want := account(t, l, "bob").Cash, bal(10000000, 0); got != want {
		t.Errorf("bob cash = %+v, want %+v", got, want)
	}
	if got, want := position(t, l, "bob", "MSFT"), bal(100, 20); got != want {
		t.Errorf("bob MSFT = %+v, want %+v", got, want)
	}
}

func TestSettleEmptyBatch(t *testing.T) {
	l := Seed()
	before := l.Clone()
	if err := l.Settle(nil, nil); err != nil {
		t.Fatalf("Settle(nil) = %v", err)
	}
	if !reflect.DeepEqual(l, before) {
		t.Error("empty Settle changed the ledger")
	}
}

// Every failing batch leaves the ledger unchanged, including when earlier
// records in the batch were valid.
func TestSettleRejectsWithoutChange(t *testing.T) {
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19100, 10)
	s2 := newOrder(t, "ord-3", "alice", "AAPL", sell, 19000, 5)
	wrongSide := newOrder(t, "ord-2", "bob", "AAPL", sell, 19100, 10)
	wrongSymbol := newOrder(t, "ord-2", "bob", "MSFT", buy, 19100, 10)
	stranger := newOrder(t, "ord-2", "carol", "AAPL", buy, 19100, 10)
	good := record("exe-1", b, s, 19000, 5)

	withBuyer := func(r executions.Record, user string) executions.Record { r.BuyerUserID = user; return r }
	withPrice := func(r executions.Record, p int64) executions.Record { r.Price = p; return r }

	tests := []struct {
		name      string
		records   []executions.Record
		canonical map[string]orders.Order
		want      error
	}{
		{"missing buy order", []executions.Record{good}, byID(s), ErrInconsistent},
		{"missing sell order", []executions.Record{good}, byID(b), ErrInconsistent},
		{"buyer is not the buy order's account", []executions.Record{withBuyer(good, "alice")}, byID(s, b), ErrInconsistent},
		{"buy order is a SELL", []executions.Record{good}, byID(s, wrongSide), ErrInconsistent},
		{"order symbol differs", []executions.Record{good}, byID(s, wrongSymbol), ErrInconsistent},
		{"unknown counterparty", []executions.Record{withBuyer(good, "carol")}, byID(s, stranger), ErrUnknownUser},
		{"price above buy limit", []executions.Record{withPrice(good, 19200)}, byID(s, b), risk.ErrInvalidInput},
		{"second record invalid", []executions.Record{good, withBuyer(record("exe-2", b, s, 19000, 5), "alice")}, byID(s, b), ErrInconsistent},
		// s2 was never reserved, so the batch releases 15 reserved shares
		// where only 10 exist.
		{"releases more than reserved", []executions.Record{good, record("exe-2", b, s2, 19000, 10)}, byID(s, s2, b), risk.ErrInconsistent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Seed()
			mustReserve(t, l, s)
			mustReserve(t, l, b)
			before := l.Clone()
			err := l.Settle(tt.records, tt.canonical)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Settle = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(l, before) {
				t.Errorf("failed Settle changed the ledger:\n%+v\nwas\n%+v", l.Snapshot(), before.Snapshot())
			}
		})
	}
}

// A settlement that would push a total past MaxAggregate is rejected with
// risk.ErrLimitExceeded and changes nothing.
func TestSettleOverflowRejected(t *testing.T) {
	l := Seed()
	alice := l.balances["alice"]
	alice.Cash.Total = risk.MaxAggregate - 1000
	l.balances["alice"] = alice

	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 1)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19000, 1)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	before := l.Clone()
	err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 1)}, byID(s, b))
	if !errors.Is(err, risk.ErrLimitExceeded) {
		t.Fatalf("Settle = %v, want ErrLimitExceeded", err)
	}
	if !reflect.DeepEqual(l, before) {
		t.Error("overflowing Settle changed the ledger")
	}

	// The same trade between the accounts' own orders nets to zero and fits.
	b2 := newOrder(t, "ord-3", "alice", "AAPL", buy, 19000, 1)
	mustReserve(t, l, b2)
	if err := l.Settle([]executions.Record{record("exe-2", b2, s, 19000, 1)}, byID(s, b2)); err != nil {
		t.Fatalf("same-account Settle at the bound: %v", err)
	}
	if got := account(t, l, "alice").Cash; got != bal(risk.MaxAggregate-1000, 0) {
		t.Errorf("alice cash = %+v", got)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	published := Seed()
	want := published.Snapshot()

	candidate := published.Clone()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19100, 10)
	mustReserve(t, candidate, s)
	mustReserve(t, candidate, b)
	if err := candidate.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
		t.Fatal(err)
	}
	if got := published.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Errorf("changing a clone changed the original:\n%+v", got)
	}
	if reflect.DeepEqual(candidate.Snapshot(), want) {
		t.Error("clone did not change")
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	l := Seed()
	snap := l.Snapshot()
	snap[0].Cash.Total = 1
	snap[0].Positions[0].Reserved = 99
	snap[1].Positions = snap[1].Positions[:0]
	if got := l.Snapshot(); !reflect.DeepEqual(got, Seed().Snapshot()) {
		t.Errorf("changing a snapshot changed the ledger: %+v", got)
	}
}

func TestSnapshotListsZeroHoldings(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "MSFT", sell, 40000, 100)
	b := newOrder(t, "ord-2", "bob", "MSFT", buy, 40000, 100)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if err := l.Settle([]executions.Record{record("exe-1", b, s, 40000, 100)}, byID(s, b)); err != nil {
		t.Fatal(err)
	}
	got := account(t, l, "alice").Positions
	want := []Position{{"AAPL", bal(100, 0)}, {"MSFT", bal(0, 0)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("alice positions = %+v, want %+v", got, want)
	}
}

func TestReconcile(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 30)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19050, 25)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	if err := l.Reconcile([]orders.Order{s, b}); err != nil {
		t.Fatalf("Reconcile after reserving: %v", err)
	}
	if err := l.Reconcile([]orders.Order{s}); !errors.Is(err, ErrInconsistent) {
		t.Errorf("Reconcile missing a BUY = %v, want ErrInconsistent", err)
	}
	if err := l.Reconcile([]orders.Order{b}); !errors.Is(err, ErrInconsistent) {
		t.Errorf("Reconcile missing a SELL = %v, want ErrInconsistent", err)
	}

	if err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 25)}, byID(s, b)); err != nil {
		t.Fatal(err)
	}
	for _, o := range []*orders.Order{&s, &b} {
		if err := o.Fill(25, testTime); err != nil {
			t.Fatal(err)
		}
	}
	// b is FILLED and needs nothing; s has 5 shares left.
	if err := l.Reconcile([]orders.Order{s, b}); err != nil {
		t.Fatalf("Reconcile after the fill: %v", err)
	}

	extra := newOrder(t, "ord-3", "carol", "AAPL", buy, 1, 1)
	if err := l.Reconcile([]orders.Order{s, b, extra}); !errors.Is(err, ErrUnknownUser) {
		t.Errorf("Reconcile with an unknown account = %v, want ErrUnknownUser", err)
	}
	huge := newOrder(t, "ord-4", "bob", "AAPL", buy, math.MaxInt64, 2)
	if err := l.Reconcile([]orders.Order{s, huge}); !errors.Is(err, ErrInconsistent) {
		t.Errorf("Reconcile with an overflowing order = %v, want ErrInconsistent", err)
	}
	sellOther := newOrder(t, "ord-5", "bob", "TSLA", sell, 1, 1)
	if err := l.Reconcile([]orders.Order{s, sellOther}); !errors.Is(err, ErrInconsistent) {
		t.Errorf("Reconcile with an unreserved symbol = %v, want ErrInconsistent", err)
	}
}

func TestMulAdd(t *testing.T) {
	tests := []struct {
		sum, a, b int64
		want      int64
		ok        bool
	}{
		{0, 0, 5, 0, true},
		{10, 3, 4, 22, true},
		{0, risk.MaxAggregate, 1, risk.MaxAggregate, true},
		{1, risk.MaxAggregate, 1, 0, false},
		{0, math.MaxInt64, 2, 0, false},
		{0, 1 << 32, 1 << 32, 0, false},
		{-1, 1, 1, 0, false},
		{0, -1, 1, 0, false},
		{0, 1, math.MinInt64, 0, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.sum, tt.a, tt.b), func(t *testing.T) {
			got, ok := mulAdd(tt.sum, tt.a, tt.b)
			if got != tt.want || ok != tt.ok {
				t.Errorf("mulAdd = %d, %v, want %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
