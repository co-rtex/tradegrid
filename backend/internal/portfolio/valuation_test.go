package portfolio

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Reference prices and seeded equity from docs/portfolio.md.
var refPrices = map[string]int64{"AAPL": 19000, "MSFT": 42000}

const seedEquity = 16100000

func cash(total, reserved int64) Balance {
	return Balance{Total: total, Reserved: reserved, Available: total - reserved}
}

// holding returns a consistent Holding with total shares, reserved of them
// held by resting SELL orders.
func holding(symbol string, total, reserved, basis, realized int64) Holding {
	return Holding{
		Symbol: symbol,
		Shares: cash(total, reserved),
		Lot:    Lot{Quantity: total, CostBasis: basis, RealizedPnL: realized},
	}
}

func account(userID string, seq int64, c Balance, holdings ...Holding) AccountSnapshot {
	names := map[string]string{"alice": "Alice (demo)", "bob": "Bob (demo)"}
	return AccountSnapshot{UserID: userID, DisplayName: names[userID], AsOfSequence: seq, Cash: c, Holdings: holdings}
}

func seed(userID string) AccountSnapshot {
	return account(userID, 0, cash(10000000, 0),
		holding("AAPL", 100, 0, 1900000, 0),
		holding("MSFT", 100, 0, 4200000, 0))
}

func value(t *testing.T, s AccountSnapshot, prices map[string]int64) Valuation {
	t.Helper()
	v, err := Value(s, prices)
	if err != nil {
		t.Fatalf("Value(%+v): %v", s, err)
	}
	checkInvariants(t, v, prices)
	return v
}

// checkInvariants checks the definitions in docs/portfolio.md against v,
// recomputing each value independently.
func checkInvariants(t *testing.T, v Valuation, prices map[string]int64) {
	t.Helper()
	if v.Positions == nil {
		t.Error("positions is nil, want an empty or filled slice")
	}
	var sum Totals
	for i, p := range v.Positions {
		if i > 0 && v.Positions[i-1].Symbol >= p.Symbol {
			t.Errorf("positions not strictly sorted: %q before %q", v.Positions[i-1].Symbol, p.Symbol)
		}
		if p.Available != p.Total-p.Reserved {
			t.Errorf("%s: available %d != total %d - reserved %d", p.Symbol, p.Available, p.Total, p.Reserved)
		}
		if p.ReferencePrice != prices[p.Symbol] || p.ReferencePrice <= 0 {
			t.Errorf("%s: reference price %d, want %d", p.Symbol, p.ReferencePrice, prices[p.Symbol])
		}
		if p.MarketValue != p.Total*p.ReferencePrice {
			t.Errorf("%s: market value %d != %d × %d", p.Symbol, p.MarketValue, p.Total, p.ReferencePrice)
		}
		if p.UnrealizedPnL != p.MarketValue-p.CostBasis {
			t.Errorf("%s: unrealized %d != %d - %d", p.Symbol, p.UnrealizedPnL, p.MarketValue, p.CostBasis)
		}
		if p.Total == 0 && (p.CostBasis != 0 || p.MarketValue != 0 || p.UnrealizedPnL != 0) {
			t.Errorf("%s: empty position %+v has basis, value, or unrealized P&L", p.Symbol, p)
		}
		sum.CostBasis += p.CostBasis
		sum.MarketValue += p.MarketValue
		sum.UnrealizedPnL += p.UnrealizedPnL
		sum.RealizedPnL += p.RealizedPnL
	}
	sum.Equity = v.Cash.Total + sum.MarketValue
	if v.Totals != sum {
		t.Errorf("totals %+v, want sums %+v", v.Totals, sum)
	}
	if v.Totals.UnrealizedPnL != v.Totals.MarketValue-v.Totals.CostBasis {
		t.Errorf("total unrealized %d != market value %d - basis %d", v.Totals.UnrealizedPnL, v.Totals.MarketValue, v.Totals.CostBasis)
	}
}

func TestSeedValuation(t *testing.T) {
	for _, tc := range []struct{ userID, name string }{{"alice", "Alice (demo)"}, {"bob", "Bob (demo)"}} {
		t.Run(tc.userID, func(t *testing.T) {
			got := value(t, seed(tc.userID), refPrices)
			want := Valuation{
				UserID:      tc.userID,
				DisplayName: tc.name,
				Cash:        Balance{Total: 10000000, Available: 10000000},
				Positions: []PositionValuation{
					{Symbol: "AAPL", Balance: Balance{Total: 100, Available: 100}, CostBasis: 1900000, ReferencePrice: 19000, MarketValue: 1900000},
					{Symbol: "MSFT", Balance: Balance{Total: 100, Available: 100}, CostBasis: 4200000, ReferencePrice: 42000, MarketValue: 4200000},
				},
				Totals: Totals{CostBasis: 6100000, MarketValue: 6100000, Equity: seedEquity},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

// TestDocumentedResponse reproduces the GET /api/v1/portfolio example in
// docs/portfolio.md byte for byte: Alice after example 1.
func TestDocumentedResponse(t *testing.T) {
	const doc = `{
  "userId": "alice",
  "displayName": "Alice (demo)",
  "asOfSequence": 2,
  "cash": { "total": 10185000, "reserved": 0, "available": 10185000 },
  "positions": [
    {
      "symbol": "AAPL",
      "total": 90,
      "reserved": 0,
      "available": 90,
      "costBasis": 1710000,
      "referencePrice": 19000,
      "marketValue": 1710000,
      "unrealizedPnl": 0,
      "realizedPnl": -5000
    },
    {
      "symbol": "MSFT",
      "total": 100,
      "reserved": 0,
      "available": 100,
      "costBasis": 4200000,
      "referencePrice": 42000,
      "marketValue": 4200000,
      "unrealizedPnl": 0,
      "realizedPnl": 0
    }
  ],
  "totals": {
    "costBasis": 5910000,
    "marketValue": 5910000,
    "unrealizedPnl": 0,
    "realizedPnl": -5000,
    "equity": 16095000
  }
}`
	s := account("alice", 2, cash(10185000, 0),
		holding("AAPL", 90, 0, 1710000, -5000),
		holding("MSFT", 100, 0, 4200000, 0))
	got, err := json.Marshal(value(t, s, refPrices))
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, []byte(doc)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("JSON\n got %s\nwant %s", got, want.Bytes())
	}
}

// TestValuationWorkedExamples values both accounts after each worked example in
// docs/portfolio.md and checks every documented cent and the cross-account
// invariants.
func TestValuationWorkedExamples(t *testing.T) {
	type side struct {
		snapshot   AccountSnapshot
		unrealized map[string]int64
		equity     int64
	}
	// Bob's SELL 7 AAPL from example 3 keeps 4 shares reserved from then on.
	steps := []struct {
		name       string
		alice, bob side
	}{
		{"seed", side{seed("alice"), map[string]int64{"AAPL": 0, "MSFT": 0}, 16100000},
			side{seed("bob"), map[string]int64{"AAPL": 0, "MSFT": 0}, 16100000}},
		{"1. basic buy and sell",
			side{account("alice", 2, cash(10185000, 0), holding("AAPL", 90, 0, 1710000, -5000), holding("MSFT", 100, 0, 4200000, 0)),
				map[string]int64{"AAPL": 0, "MSFT": 0}, 16095000},
			side{account("bob", 2, cash(9815000, 0), holding("AAPL", 110, 0, 2085000, 0), holding("MSFT", 100, 0, 4200000, 0)),
				map[string]int64{"AAPL": 5000, "MSFT": 0}, 16105000}},
		{"2. price improvement",
			side{account("alice", 4, cash(10279000, 0), holding("AAPL", 85, 0, 1615000, -6000), holding("MSFT", 100, 0, 4200000, 0)),
				map[string]int64{"AAPL": 0, "MSFT": 0}, 16094000},
			side{account("bob", 4, cash(9721000, 0), holding("AAPL", 115, 0, 2179000, 0), holding("MSFT", 100, 0, 4200000, 0)),
				map[string]int64{"AAPL": 6000, "MSFT": 0}, 16106000}},
		{"3. partial fill with rounding",
			side{account("alice", 6, cash(10221100, 0), holding("AAPL", 88, 0, 1672900, -6000), holding("MSFT", 100, 0, 4200000, 0)),
				map[string]int64{"AAPL": -900, "MSFT": 0}, 16093100},
			side{account("bob", 6, cash(9778900, 0), holding("AAPL", 112, 4, 2122157, 1057), holding("MSFT", 100, 0, 4200000, 0)),
				map[string]int64{"AAPL": 5843, "MSFT": 0}, 16106900}},
		{"4. multiple fills close a position",
			side{account("alice", 9, cash(14411100, 0), holding("AAPL", 88, 0, 1672900, -6000), holding("MSFT", 0, 0, 0, -10000)),
				map[string]int64{"AAPL": -900, "MSFT": 0}, 16083100},
			side{account("bob", 9, cash(5588900, 0), holding("AAPL", 112, 4, 2122157, 1057), holding("MSFT", 200, 0, 8390000, 0)),
				map[string]int64{"AAPL": 5843, "MSFT": 10000}, 16116900}},
		{"5. same-account trade and reconciliation",
			side{account("alice", 11, cash(14411100, 0), holding("AAPL", 88, 0, 1672900, -6000), holding("MSFT", 0, 0, 0, -10000)),
				map[string]int64{"AAPL": -900, "MSFT": 0}, 16083100},
			side{account("bob", 11, cash(5588900, 0), holding("AAPL", 112, 4, 2122157, 1057), holding("MSFT", 200, 0, 8390000, 0)),
				map[string]int64{"AAPL": 5843, "MSFT": 10000}, 16116900}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			var combinedEquity, combinedPnL int64
			shares := map[string]int64{}
			for _, sd := range []side{step.alice, step.bob} {
				v := value(t, sd.snapshot, refPrices)
				if v.Totals.Equity != sd.equity {
					t.Errorf("%s equity %d, want %d", v.UserID, v.Totals.Equity, sd.equity)
				}
				if len(v.Positions) != len(sd.unrealized) {
					t.Fatalf("%s has %d positions, want %d", v.UserID, len(v.Positions), len(sd.unrealized))
				}
				for _, p := range v.Positions {
					if want, ok := sd.unrealized[p.Symbol]; !ok || p.UnrealizedPnL != want {
						t.Errorf("%s %s unrealized %d, want %d", v.UserID, p.Symbol, p.UnrealizedPnL, want)
					}
					shares[p.Symbol] += p.Total
				}
				// Invariant 3: equity moves only by P&L.
				pnl := v.Totals.RealizedPnL + v.Totals.UnrealizedPnL
				if v.Totals.Equity-seedEquity != pnl {
					t.Errorf("%s equity - seed = %d, P&L = %d", v.UserID, v.Totals.Equity-seedEquity, pnl)
				}
				combinedEquity += v.Totals.Equity
				combinedPnL += pnl
			}
			// Invariant 4: trading only moves value between the accounts.
			if combinedEquity != 2*seedEquity || combinedPnL != 0 {
				t.Errorf("combined equity %d and P&L %d, want %d and 0", combinedEquity, combinedPnL, 2*seedEquity)
			}
			if shares["AAPL"] != 200 || shares["MSFT"] != 200 {
				t.Errorf("share totals %v, want 200 each", shares)
			}
		})
	}

	// Reconciliation table: realized + unrealized.
	alice := value(t, steps[5].alice.snapshot, refPrices)
	bob := value(t, steps[5].bob.snapshot, refPrices)
	wantAlice := Totals{CostBasis: 1672900, MarketValue: 1672000, UnrealizedPnL: -900, RealizedPnL: -16000, Equity: 16083100}
	wantBob := Totals{CostBasis: 10512157, MarketValue: 10528000, UnrealizedPnL: 15843, RealizedPnL: 1057, Equity: 16116900}
	if alice.Totals != wantAlice || bob.Totals != wantBob {
		t.Errorf("reconciliation totals\n alice %+v, want %+v\n bob   %+v, want %+v", alice.Totals, wantAlice, bob.Totals, wantBob)
	}
}

// TestReservationsCountedOnce checks that resting orders change no equity:
// reserved cash stays in cash.total and reserved shares stay in market value.
func TestReservationsCountedOnce(t *testing.T) {
	// Alice rests BUY 10 AAPL @ 18000 (reserves 180000 cash) and SELL 5 MSFT
	// (reserves 5 shares).
	s := account("alice", 2, cash(10000000, 180000),
		holding("AAPL", 100, 0, 1900000, 0),
		holding("MSFT", 100, 5, 4200000, 0))
	got := value(t, s, refPrices)
	base := value(t, seed("alice"), refPrices)

	if got.Cash != (Balance{Total: 10000000, Reserved: 180000, Available: 9820000}) {
		t.Errorf("cash %+v, want the input cash", got.Cash)
	}
	msft := got.Positions[1]
	if msft.Balance != (Balance{Total: 100, Reserved: 5, Available: 95}) || msft.MarketValue != 4200000 {
		t.Errorf("MSFT %+v, want 100 shares (5 reserved) worth 4200000", msft)
	}
	if got.Totals != base.Totals {
		t.Errorf("totals with resting orders %+v, want unchanged %+v", got.Totals, base.Totals)
	}
	if got.Totals.Equity != seedEquity {
		t.Errorf("equity %d, want %d", got.Totals.Equity, seedEquity)
	}
}

func TestNegativeUnrealizedPnL(t *testing.T) {
	// Bought 50 AAPL at 25000 and 10 MSFT at 50000: both below cost.
	s := account("bob", 1, cash(0, 0),
		holding("AAPL", 50, 0, 1250000, 0),
		holding("MSFT", 10, 0, 500000, -42))
	got := value(t, s, refPrices)
	want := Totals{CostBasis: 1750000, MarketValue: 950000 + 420000, UnrealizedPnL: -300000 - 80000, RealizedPnL: -42, Equity: 1370000}
	if got.Totals != want {
		t.Errorf("totals %+v, want %+v", got.Totals, want)
	}
	if got.Positions[0].UnrealizedPnL != -300000 || got.Positions[1].UnrealizedPnL != -80000 {
		t.Errorf("unrealized %d and %d, want -300000 and -80000", got.Positions[0].UnrealizedPnL, got.Positions[1].UnrealizedPnL)
	}
}

func TestEmptyAndZeroPositions(t *testing.T) {
	t.Run("no holdings in an empty catalog", func(t *testing.T) {
		got := value(t, account("alice", 0, cash(500, 100)), map[string]int64{})
		if len(got.Positions) != 0 || got.Totals != (Totals{Equity: 500}) {
			t.Errorf("got %+v, want no positions and equity equal to cash total", got)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte(`"positions":[]`)) {
			t.Errorf("JSON %s, want an empty positions array, never null", b)
		}
	})
	t.Run("zero holdings keep realized P&L", func(t *testing.T) {
		s := account("alice", 0, cash(0, 0), holding("AAPL", 0, 0, 0, 0), holding("MSFT", 0, 0, 0, -10000))
		got := value(t, s, refPrices)
		want := []PositionValuation{
			{Symbol: "AAPL", ReferencePrice: 19000},
			{Symbol: "MSFT", ReferencePrice: 42000, RealizedPnL: -10000},
		}
		if !reflect.DeepEqual(got.Positions, want) || got.Totals != (Totals{RealizedPnL: -10000}) {
			t.Errorf("got %+v, want %+v and only realized P&L in totals", got, want)
		}
	})
	t.Run("zero value snapshot", func(t *testing.T) {
		got := value(t, AccountSnapshot{}, nil)
		if !reflect.DeepEqual(got, Valuation{Positions: []PositionValuation{}}) {
			t.Errorf("got %+v, want an empty valuation", got)
		}
	})
}

func TestReferencePriceErrors(t *testing.T) {
	alice := seed("alice")
	closed := account("alice", 0, cash(0, 0), holding("MSFT", 0, 0, 0, -10000))
	for _, tc := range []struct {
		name   string
		s      AccountSnapshot
		prices map[string]int64
		err    error
	}{
		{"nil prices", alice, nil, ErrMissingPrice},
		{"empty prices", alice, map[string]int64{}, ErrMissingPrice},
		{"missing MSFT", alice, map[string]int64{"AAPL": 19000}, ErrMissingPrice},
		{"missing AAPL", alice, map[string]int64{"MSFT": 42000}, ErrMissingPrice},
		{"symbols match exactly", alice, map[string]int64{"aapl": 19000, "MSFT": 42000}, ErrMissingPrice},
		{"missing for a zero holding", closed, map[string]int64{"AAPL": 19000}, ErrMissingPrice},
		{"zero price", alice, map[string]int64{"AAPL": 0, "MSFT": 42000}, ErrInvalidPrice},
		{"zero price for a zero holding", closed, map[string]int64{"MSFT": 0}, ErrInvalidPrice},
		{"negative price", alice, map[string]int64{"AAPL": 19000, "MSFT": math.MinInt64}, ErrInvalidPrice},
		{"price above max", alice, map[string]int64{"AAPL": MaxAmount + 1, "MSFT": 42000}, ErrOverflow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Value(tc.s, tc.prices)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, Valuation{}) {
				t.Errorf("on error got %+v, want the zero Valuation", got)
			}
		})
	}
}

// A priced (tradable) symbol without a holding row is an error, not an
// omitted position: the contract lists every tradable symbol, and a dropped
// row would also drop its realized P&L from the totals.
func TestPricedSymbolWithoutHolding(t *testing.T) {
	prices := map[string]int64{"AAPL": 19000, "GOOG": 1, "MSFT": 42000, "ZZZ": 5}
	got, err := Value(seed("alice"), prices)
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("error = %v, want ErrInvalidSnapshot", err)
	}
	if !strings.Contains(err.Error(), "GOOG, ZZZ") {
		t.Errorf("error %q should name the missing symbols in order", err)
	}
	if !reflect.DeepEqual(got, Valuation{}) {
		t.Errorf("got %+v, want the zero Valuation", got)
	}

	s := seed("alice")
	s.Holdings = s.Holdings[:1]
	if _, err := Value(s, refPrices); !errors.Is(err, ErrInvalidSnapshot) {
		t.Errorf("snapshot missing a catalog symbol: error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestEmptySymbolRejected(t *testing.T) {
	s := AccountSnapshot{Cash: cash(0, 0), Holdings: []Holding{holding("", 0, 0, 0, 0)}}
	if _, err := Value(s, map[string]int64{"": 1}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Errorf("error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestOverflow(t *testing.T) {
	h := holding
	for _, tc := range []struct {
		name     string
		cash     int64
		holdings []Holding
		prices   map[string]int64
		equity   int64 // checked when err is nil
		err      error
	}{
		{"market value at max", 0, []Holding{h("A", MaxAmount, 0, MaxAmount, 0)}, map[string]int64{"A": 1}, MaxAmount, nil},
		{"price at max", 0, []Holding{h("A", 1, 0, 1, 0)}, map[string]int64{"A": MaxAmount}, MaxAmount, nil},
		{"market value above max", 0, []Holding{h("A", MaxAmount, 0, MaxAmount, 0)}, map[string]int64{"A": 2}, 0, ErrOverflow},
		// 2^32 × 2^32 wraps int64 to exactly 0.
		{"market value wraps to zero", 0, []Holding{h("A", 1<<32, 0, 1<<32, 0)}, map[string]int64{"A": 1 << 32}, 0, ErrOverflow},
		{"market value wraps", 0, []Holding{h("A", MaxAmount, 0, MaxAmount, 0)}, map[string]int64{"A": MaxAmount}, 0, ErrOverflow},
		{"equity at max", MaxAmount - 1, []Holding{h("A", 1, 0, 1, 0)}, map[string]int64{"A": 1}, MaxAmount, nil},
		{"equity above max", 1, []Holding{h("A", MaxAmount, 0, MaxAmount, 0)}, map[string]int64{"A": 1}, 0, ErrOverflow},
		{"total market value above max", 0, []Holding{h("A", 1<<52, 0, 1<<52, 0), h("B", 1<<52, 0, 1<<52, 0)},
			map[string]int64{"A": 1, "B": 1}, 0, ErrOverflow},
		{"total cost basis above max", 0, []Holding{h("A", 1, 0, 1<<52, 0), h("B", 1, 0, 1<<52, 0)},
			map[string]int64{"A": 1, "B": 1}, 0, ErrOverflow},
		{"total realized at max", 0, []Holding{h("A", 0, 0, 0, MaxAmount), h("B", 0, 0, 0, 0)},
			map[string]int64{"A": 1, "B": 1}, 0, nil},
		{"total realized at min", 0, []Holding{h("A", 0, 0, 0, -MaxAmount), h("B", 0, 0, 0, 0)},
			map[string]int64{"A": 1, "B": 1}, 0, nil},
		{"total realized above max", 0, []Holding{h("A", 0, 0, 0, MaxAmount), h("B", 0, 0, 0, 1)},
			map[string]int64{"A": 1, "B": 1}, 0, ErrOverflow},
		{"total realized below min", 0, []Holding{h("A", 0, 0, 0, -MaxAmount), h("B", 0, 0, 0, -1)},
			map[string]int64{"A": 1, "B": 1}, 0, ErrOverflow},
		{"unrealized at max", 0, []Holding{h("A", 1, 0, 1, 0)}, map[string]int64{"A": MaxAmount}, MaxAmount, nil},
		{"large unrealized loss", 0, []Holding{h("A", 1, 0, MaxAmount, 0)}, map[string]int64{"A": 1}, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := AccountSnapshot{Cash: cash(tc.cash, 0), Holdings: tc.holdings}
			got, err := Value(s, tc.prices)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if tc.err != nil {
				if !reflect.DeepEqual(got, Valuation{}) {
					t.Errorf("on error got %+v, want the zero Valuation", got)
				}
				return
			}
			checkInvariants(t, got, tc.prices)
			if got.Totals.Equity != tc.equity {
				t.Errorf("equity %d, want %d", got.Totals.Equity, tc.equity)
			}
		})
	}
}

func TestInvalidSnapshot(t *testing.T) {
	ok := holding("AAPL", 100, 0, 1900000, 0)
	with := func(f func(*AccountSnapshot)) AccountSnapshot {
		s := seed("alice")
		f(&s)
		return s
	}
	for _, tc := range []struct {
		name string
		s    AccountSnapshot
		err  error
	}{
		{"negative sequence", with(func(s *AccountSnapshot) { s.AsOfSequence = -1 }), ErrInvalidSnapshot},
		{"negative cash", with(func(s *AccountSnapshot) { s.Cash = cash(-1, 0) }), ErrInvalidSnapshot},
		{"cash above max", with(func(s *AccountSnapshot) { s.Cash = cash(MaxAmount+1, 0) }), ErrInvalidSnapshot},
		{"negative reserved cash", with(func(s *AccountSnapshot) { s.Cash = cash(10, -1) }), ErrInvalidSnapshot},
		{"reserved cash above total", with(func(s *AccountSnapshot) { s.Cash = cash(10, 11) }), ErrInvalidSnapshot},
		{"available cash mismatch", with(func(s *AccountSnapshot) { s.Cash.Available-- }), ErrInvalidSnapshot},
		{"available cash ignores reserved", with(func(s *AccountSnapshot) {
			s.Cash = Balance{Total: 10, Reserved: 4, Available: 10}
		}), ErrInvalidSnapshot},
		{"reserved shares above total", with(func(s *AccountSnapshot) { s.Holdings[0].Shares = cash(100, 101) }), ErrInvalidSnapshot},
		{"available shares mismatch", with(func(s *AccountSnapshot) { s.Holdings[1].Shares.Available = 99 }), ErrInvalidSnapshot},
		{"negative shares", with(func(s *AccountSnapshot) { s.Holdings[0] = holding("AAPL", -1, 0, 0, 0) }), ErrInvalidSnapshot},
		{"lot quantity mismatch", with(func(s *AccountSnapshot) { s.Holdings[0].Lot.Quantity = 99 }), ErrInvalidSnapshot},
		{"duplicate symbol", with(func(s *AccountSnapshot) { s.Holdings = append(s.Holdings, ok) }), ErrInvalidSnapshot},
		{"basis below one cent a share", with(func(s *AccountSnapshot) { s.Holdings[0].Lot.CostBasis = 99 }), ErrInvalidLot},
		{"basis without shares", with(func(s *AccountSnapshot) { s.Holdings[0] = holding("AAPL", 0, 0, 5, 0) }), ErrInvalidLot},
		{"negative basis", with(func(s *AccountSnapshot) { s.Holdings[0] = holding("AAPL", 0, 0, -1, 0) }), ErrInvalidLot},
		{"realized out of range", with(func(s *AccountSnapshot) { s.Holdings[1].Lot.RealizedPnL = MaxAmount + 1 }), ErrInvalidLot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Value(tc.s, refPrices)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, Valuation{}) {
				t.Errorf("on error got %+v, want the zero Valuation", got)
			}
		})
	}
}

// TestInputsNotMutated passes holdings out of order and checks that neither
// the snapshot nor the prices change, on success or on error, and that the
// result shares no memory with them.
func TestInputsNotMutated(t *testing.T) {
	in := account("bob", 6, cash(9778900, 1000),
		holding("MSFT", 100, 7, 4200000, 0),
		holding("AAPL", 112, 4, 2122157, 1057))
	prices := maps.Clone(refPrices)
	wantIn := in
	wantIn.Holdings = slices.Clone(in.Holdings)
	wantPrices := maps.Clone(prices)

	got, err := Value(in, prices)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, wantIn) || !maps.Equal(prices, wantPrices) {
		t.Fatalf("inputs changed: %+v, %v", in, prices)
	}
	if got.Positions[0].Symbol != "AAPL" || in.Holdings[0].Symbol != "MSFT" {
		t.Errorf("output order %q first and input %q first, want AAPL and MSFT", got.Positions[0].Symbol, in.Holdings[0].Symbol)
	}

	// Changing the inputs afterwards does not change the result, and changing
	// the result does not change the inputs.
	saved := got
	saved.Positions = slices.Clone(got.Positions)
	in.Holdings[1].Lot.CostBasis = 1
	in.Cash.Total = 0
	prices["AAPL"] = 1
	if !reflect.DeepEqual(got, saved) {
		t.Errorf("result changed with its inputs: %+v", got)
	}
	in.Holdings[1].Lot.CostBasis, in.Cash.Total, prices["AAPL"] = 2122157, 9778900, 19000
	got.Positions[0].Total = 0
	got.Cash.Total = 0
	if !reflect.DeepEqual(in, wantIn) || !maps.Equal(prices, wantPrices) {
		t.Errorf("inputs changed with the result: %+v, %v", in, prices)
	}

	// Error paths leave the inputs alone too.
	missing := map[string]int64{"MSFT": 42000}
	if _, err := Value(in, missing); !errors.Is(err, ErrMissingPrice) {
		t.Fatalf("error = %v, want ErrMissingPrice", err)
	}
	if !reflect.DeepEqual(in, wantIn) || !maps.Equal(missing, map[string]int64{"MSFT": 42000}) {
		t.Errorf("inputs changed on error: %+v, %v", in, missing)
	}
}

// TestDeterministicOrder values every ordering of four holdings and expects
// the same symbol-sorted result each time.
func TestDeterministicOrder(t *testing.T) {
	prices := map[string]int64{"AAPL": 19000, "AMZN": 18500, "GOOG": 17000, "MSFT": 42000}
	holdings := []Holding{
		holding("MSFT", 200, 0, 8390000, 0),
		holding("GOOG", 3, 1, 50000, 7),
		holding("AAPL", 88, 0, 1672900, -6000),
		holding("AMZN", 0, 0, 0, -10000),
	}
	var first Valuation
	n := 0
	permute(holdings, 0, func(order []Holding) {
		got := value(t, account("alice", 1, cash(100, 0), slices.Clone(order)...), prices)
		if n == 0 {
			first = got
		} else if !reflect.DeepEqual(got, first) {
			t.Fatalf("order %v gave %+v, want %+v", symbols(order), got, first)
		}
		n++
	})
	if n != 24 {
		t.Fatalf("checked %d orderings, want 24", n)
	}
	if got := symbolsOf(first.Positions); !slices.Equal(got, []string{"AAPL", "AMZN", "GOOG", "MSFT"}) {
		t.Errorf("positions %v, want sorted by symbol", got)
	}
}

func permute(h []Holding, k int, f func([]Holding)) {
	if k == len(h) {
		f(h)
		return
	}
	for i := k; i < len(h); i++ {
		h[k], h[i] = h[i], h[k]
		permute(h, k+1, f)
		h[k], h[i] = h[i], h[k]
	}
}

func symbols(h []Holding) []string {
	out := make([]string, len(h))
	for i := range h {
		out[i] = h[i].Symbol
	}
	return out
}

func symbolsOf(p []PositionValuation) []string {
	out := make([]string, len(p))
	for i := range p {
		out[i] = p[i].Symbol
	}
	return out
}

// TestValueAfterFills chains the cost-basis calculator into Value: the lots
// ApplyBuy and ApplySell produce for example 1 value as documented.
func TestValueAfterFills(t *testing.T) {
	aliceAAPL, err := ApplySell(seedAAPL, 10, 18500)
	if err != nil {
		t.Fatal(err)
	}
	bobAAPL, err := ApplyBuy(seedAAPL, 10, 18500)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(userID string, c int64, l Lot) AccountSnapshot {
		return account(userID, 2, cash(c, 0),
			Holding{Symbol: "AAPL", Shares: cash(l.Quantity, 0), Lot: l},
			Holding{Symbol: "MSFT", Shares: cash(100, 0), Lot: seedMSFT})
	}
	alice := value(t, snap("alice", 10185000, aliceAAPL), refPrices)
	bob := value(t, snap("bob", 9815000, bobAAPL), refPrices)
	if alice.Totals.Equity != 16095000 || bob.Totals.Equity != 16105000 {
		t.Errorf("equity %d and %d, want 16095000 and 16105000", alice.Totals.Equity, bob.Totals.Equity)
	}
}
