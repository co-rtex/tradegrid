package accounts

import (
	"errors"
	"reflect"
	"testing"

	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/portfolio"
)

func lotOf(t *testing.T, l *Ledger, userID, symbol string) portfolio.Lot {
	t.Helper()
	p, err := l.Portfolio(userID)
	if err != nil {
		t.Fatalf("Portfolio(%s): %v", userID, err)
	}
	for _, h := range p.Holdings {
		if h.Symbol == symbol {
			return h.Lot
		}
	}
	t.Fatalf("%s has no %s holding", userID, symbol)
	return portfolio.Lot{}
}

// Seeded shares carry a basis of quantity × reference price and no realized
// P&L (docs/portfolio.md, "Seeded Positions").
func TestSeedCostBasis(t *testing.T) {
	l := Seed()
	want := map[string]portfolio.Lot{
		"AAPL": {Quantity: 100, CostBasis: 1_900_000},
		"MSFT": {Quantity: 100, CostBasis: 4_200_000},
	}
	for _, user := range []string{"alice", "bob"} {
		p, err := l.Portfolio(user)
		if err != nil {
			t.Fatal(err)
		}
		if p.UserID != user || p.Cash != bal(DemoCash, 0) || len(p.Holdings) != len(want) {
			t.Fatalf("Portfolio(%s) = %+v", user, p)
		}
		for _, h := range p.Holdings {
			if h.Lot != want[h.Symbol] || h.Shares != bal(DemoShares, 0) {
				t.Errorf("%s %s = %+v, want lot %+v and 100 shares", user, h.Symbol, h, want[h.Symbol])
			}
		}
	}
	if err := l.Reconcile(nil); err != nil {
		t.Errorf("Reconcile(seed) = %v", err)
	}
}

// Settlement updates both sides' lots at the execution price, not the buy
// limit, and in record order.
func TestSettleUpdatesCostBasis(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 18500, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19500, 10)
	mustReserve(t, l, s)
	mustReserve(t, l, b)

	// Two fills of one submission, at the resting price 18500.
	records := []executions.Record{record("exe-1", b, s, 18500, 4), record("exe-2", b, s, 18500, 6)}
	if err := l.Settle(records, byID(s, b)); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	// Alice: allocate floor(1900000×4/100) = 76000, then floor(1824000×6/96) = 114000.
	if got, want := lotOf(t, l, "alice", "AAPL"), (portfolio.Lot{Quantity: 90, CostBasis: 1_710_000, RealizedPnL: -5000}); got != want {
		t.Errorf("alice AAPL lot = %+v, want %+v", got, want)
	}
	// Bob pays 185000 for the shares, not his 195000 limit.
	if got, want := lotOf(t, l, "bob", "AAPL"), (portfolio.Lot{Quantity: 110, CostBasis: 2_085_000}); got != want {
		t.Errorf("bob AAPL lot = %+v, want %+v", got, want)
	}
	if got := lotOf(t, l, "bob", "MSFT"); got != (portfolio.Lot{Quantity: 100, CostBasis: 4_200_000}) {
		t.Errorf("bob MSFT lot changed: %+v", got)
	}
	if err := l.Reconcile(nil); err != nil {
		t.Errorf("Reconcile = %v", err)
	}
}

// A same-account execution leaves the lot exactly as it was.
func TestSettleSameAccountKeepsCostBasis(t *testing.T) {
	l := Seed()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19500, 3)
	b := newOrder(t, "ord-2", "alice", "AAPL", buy, 19500, 3)
	mustReserve(t, l, s)
	mustReserve(t, l, b)
	before := lotOf(t, l, "alice", "AAPL")
	if err := l.Settle([]executions.Record{record("exe-1", b, s, 19500, 3)}, byID(s, b)); err != nil {
		t.Fatal(err)
	}
	if got := lotOf(t, l, "alice", "AAPL"); got != before {
		t.Errorf("lot = %+v, want unchanged %+v", got, before)
	}
}

// A cost-basis failure during Settle rolls back the cash and share changes
// too: on error the ledger is exactly as it was.
func TestSettleCostBasisFailureChangesNothing(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(l *Ledger)
	}{
		{"invalid seller lot", func(l *Ledger) {
			l.lots["alice"]["AAPL"] = portfolio.Lot{Quantity: 100, CostBasis: 10} // below one cent per share
		}},
		{"lot disagrees with shares", func(l *Ledger) {
			l.lots["bob"]["AAPL"] = portfolio.Lot{Quantity: 50, CostBasis: 950_000}
		}},
		{"missing lots", func(l *Ledger) { delete(l.lots, "bob") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Seed()
			s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
			b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19000, 10)
			mustReserve(t, l, s)
			mustReserve(t, l, b)
			tt.corrupt(l)
			before := l.Clone()

			err := l.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b))
			if !errors.Is(err, ErrInconsistent) {
				t.Fatalf("Settle = %v, want ErrInconsistent", err)
			}
			if !reflect.DeepEqual(l, before) {
				t.Errorf("failed Settle changed the ledger")
			}
		})
	}
}

// Clones share no lots: settling a clone leaves the original's basis alone.
func TestCloneCopiesCostBasis(t *testing.T) {
	l := Seed()
	c := l.Clone()
	s := newOrder(t, "ord-1", "alice", "AAPL", sell, 19000, 10)
	b := newOrder(t, "ord-2", "bob", "AAPL", buy, 19000, 10)
	mustReserve(t, c, s)
	mustReserve(t, c, b)
	if err := c.Settle([]executions.Record{record("exe-1", b, s, 19000, 10)}, byID(s, b)); err != nil {
		t.Fatal(err)
	}
	if got := lotOf(t, l, "alice", "AAPL"); got != (portfolio.Lot{Quantity: 100, CostBasis: 1_900_000}) {
		t.Errorf("original lot changed: %+v", got)
	}
	// A fresh clone's maps are its own too, before any Settle replaces them.
	fresh := l.Clone()
	fresh.lots["bob"]["MSFT"] = portfolio.Lot{}
	delete(fresh.lots, "alice")
	if got := lotOf(t, l, "bob", "MSFT"); got.Quantity != 100 {
		t.Errorf("clone's lot map is shared with the original")
	}
	if _, ok := l.lots["alice"]; !ok {
		t.Errorf("clone's account map is shared with the original")
	}
}

// Portfolio returns a copy and rejects unknown users.
func TestPortfolioSnapshot(t *testing.T) {
	l := Seed()
	p, err := l.Portfolio("bob")
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != "Bob (demo)" || p.Holdings[0].Symbol != "AAPL" || p.Holdings[1].Symbol != "MSFT" {
		t.Errorf("Portfolio(bob) = %+v", p)
	}
	p.Holdings[0].Lot.CostBasis = 1
	if got := lotOf(t, l, "bob", "AAPL"); got.CostBasis != 1_900_000 {
		t.Errorf("caller change reached the ledger: %+v", got)
	}
	if _, err := l.Portfolio("carol"); !errors.Is(err, ErrUnknownUser) {
		t.Errorf("Portfolio(carol) = %v, want ErrUnknownUser", err)
	}
}

// Reconcile reports a lot that no longer matches the share balance.
func TestReconcileChecksCostBasis(t *testing.T) {
	l := Seed()
	l.lots["alice"]["MSFT"] = portfolio.Lot{Quantity: 99, CostBasis: 4_158_000}
	if err := l.Reconcile(nil); !errors.Is(err, ErrInconsistent) {
		t.Errorf("Reconcile = %v, want ErrInconsistent", err)
	}
}
