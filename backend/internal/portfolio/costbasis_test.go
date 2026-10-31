package portfolio

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

// Seeded lots from docs/portfolio.md: 100 shares at the reference price.
var (
	seedAAPL = Lot{Quantity: 100, CostBasis: 1900000}
	seedMSFT = Lot{Quantity: 100, CostBasis: 4200000}
)

func buy(t *testing.T, l Lot, q, p int64) Lot {
	t.Helper()
	out, err := ApplyBuy(l, q, p)
	if err != nil {
		t.Fatalf("ApplyBuy(%+v, %d, %d): %v", l, q, p, err)
	}
	return out
}

func sell(t *testing.T, l Lot, q, p int64) Lot {
	t.Helper()
	out, err := ApplySell(l, q, p)
	if err != nil {
		t.Fatalf("ApplySell(%+v, %d, %d): %v", l, q, p, err)
	}
	return out
}

func want(t *testing.T, step string, got, want Lot) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %+v, want %+v", step, got, want)
	}
}

// TestWorkedExamples replays the examples in docs/portfolio.md and checks
// every cent.
func TestWorkedExamples(t *testing.T) {
	alice, bob := seedAAPL, seedAAPL

	// 1. Basic BUY and SELL: 10 at 18500.
	alice, bob = sell(t, alice, 10, 18500), buy(t, bob, 10, 18500)
	want(t, "example 1 alice", alice, Lot{90, 1710000, -5000})
	want(t, "example 1 bob", bob, Lot{110, 2085000, 0})

	// 2. Price improvement: Bob's limit is 19500 but he pays the resting 18800.
	alice, bob = sell(t, alice, 5, 18800), buy(t, bob, 5, 18800)
	want(t, "example 2 alice", alice, Lot{85, 1615000, -6000})
	want(t, "example 2 bob", bob, Lot{115, 2179000, 0})

	// 3. Partial fill with rounding: floor(2179000 × 3 / 115) = 56843.
	bob, alice = sell(t, bob, 3, 19300), buy(t, alice, 3, 19300)
	want(t, "example 3 alice", alice, Lot{88, 1672900, -6000})
	want(t, "example 3 bob", bob, Lot{112, 2122157, 1057})

	// 4. Two fills close Alice's MSFT position.
	aliceMSFT, bobMSFT := seedMSFT, seedMSFT
	aliceMSFT, bobMSFT = sell(t, aliceMSFT, 60, 41500), buy(t, bobMSFT, 60, 41500)
	want(t, "example 4 fill 1 alice", aliceMSFT, Lot{40, 1680000, -30000})
	aliceMSFT, bobMSFT = sell(t, aliceMSFT, 40, 42500), buy(t, bobMSFT, 40, 42500)
	want(t, "example 4 alice", aliceMSFT, Lot{0, 0, -10000})
	want(t, "example 4 bob", bobMSFT, Lot{200, 8390000, 0})

	// Reconciliation: realized + unrealized at the reference prices.
	pnl := func(l Lot, ref int64) int64 { return l.RealizedPnL + l.Quantity*ref - l.CostBasis }
	if a, b := pnl(alice, 19000)+pnl(aliceMSFT, 42000), pnl(bob, 19000)+pnl(bobMSFT, 42000); a != -16900 || b != 16900 {
		t.Errorf("total P&L = %d and %d, want -16900 and 16900", a, b)
	}
}

func TestRoundingExample(t *testing.T) {
	l := Lot{Quantity: 3, CostBasis: 1000}
	for i, basis := range []int64{667, 334, 0} {
		l = sell(t, l, 1, 400)
		if l.CostBasis != basis {
			t.Fatalf("after sale %d: basis %d, want %d", i+1, l.CostBasis, basis)
		}
	}
	// Proceeds 1200 minus the full 1000 basis.
	want(t, "closed", l, Lot{0, 0, 200})
}

func TestRepeatedPartialSellsAllocateFullBasis(t *testing.T) {
	// Basis 1000003 over 7 shares divides unevenly at every step.
	for _, sizes := range [][]int64{{1, 1, 1, 1, 1, 1, 1}, {3, 3, 1}, {2, 5}, {6, 1}, {7}} {
		l := Lot{Quantity: 7, CostBasis: 1000003}
		var proceeds int64
		for _, q := range sizes {
			l = sell(t, l, q, 150000)
			proceeds += 150000 * q
			if err := l.Validate(); err != nil {
				t.Fatalf("sizes %v: %v", sizes, err)
			}
		}
		if l.Quantity != 0 || l.CostBasis != 0 || l.RealizedPnL != proceeds-1000003 {
			t.Errorf("sizes %v: closed lot %+v, want realized %d", sizes, l, proceeds-1000003)
		}
	}
}

func TestNegativeRealizedPnL(t *testing.T) {
	l := sell(t, seedAAPL, 100, 1)
	want(t, "sold everything at one cent", l, Lot{0, 0, 100 - 1900000})
	l = buy(t, l, 2, 500)
	want(t, "rebuy keeps realized", l, Lot{2, 1000, -1899900})
}

func TestAllocateUses128Bits(t *testing.T) {
	// basis × sold overflows int64 although the result fits.
	basis, held := MaxAmount, MaxAmount
	for _, sold := range []int64{1, 1 << 40, MaxAmount - 1} {
		if got := Allocate(basis, held, sold); got != sold {
			t.Errorf("Allocate(%d, %d, %d) = %d, want %d", basis, held, sold, got, sold)
		}
	}
	if got := Allocate(MaxAmount, 3, 2); got != MaxAmount/3*2 {
		t.Errorf("Allocate(max, 3, 2) = %d, want %d", got, MaxAmount/3*2)
	}
	l := sell(t, Lot{Quantity: MaxAmount, CostBasis: MaxAmount}, 1<<40, 1)
	want(t, "huge sell", l, Lot{MaxAmount - 1<<40, MaxAmount - 1<<40, 0})
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(Lot, int64, int64) (Lot, error)
		lot   Lot
		q, p  int64
		err   error
	}{
		{"buy zero quantity", ApplyBuy, seedAAPL, 0, 100, ErrInvalidQuantity},
		{"buy negative quantity", ApplyBuy, seedAAPL, -1, 100, ErrInvalidQuantity},
		{"buy zero price", ApplyBuy, seedAAPL, 1, 0, ErrInvalidPrice},
		{"buy negative price", ApplyBuy, seedAAPL, 1, math.MinInt64, ErrInvalidPrice},
		{"sell zero quantity", ApplySell, seedAAPL, 0, 100, ErrInvalidQuantity},
		{"sell zero price", ApplySell, seedAAPL, 1, 0, ErrInvalidPrice},
		{"sell more than held", ApplySell, seedAAPL, 101, 100, ErrInsufficientShares},
		{"sell from empty", ApplySell, Lot{}, 1, 100, ErrInsufficientShares},
		{"cost overflows int64", ApplyBuy, Lot{}, math.MaxInt64, 2, ErrOverflow},
		{"cost above max", ApplyBuy, Lot{}, MaxAmount, 2, ErrOverflow},
		{"basis above max", ApplyBuy, Lot{Quantity: 1, CostBasis: MaxAmount}, 1, 1, ErrOverflow},
		{"quantity above max", ApplyBuy, Lot{Quantity: MaxAmount, CostBasis: MaxAmount}, 1, 1, ErrOverflow},
		{"proceeds above max", ApplySell, Lot{Quantity: 2, CostBasis: 2}, 2, MaxAmount, ErrOverflow},
		{"realized above max", ApplySell, Lot{Quantity: 1, CostBasis: 1, RealizedPnL: MaxAmount}, 1, 2, ErrOverflow},
		{"realized below min", ApplySell, Lot{Quantity: 1, CostBasis: 2, RealizedPnL: -MaxAmount}, 1, 1, ErrOverflow},
		{"negative quantity lot", ApplyBuy, Lot{Quantity: -1}, 1, 1, ErrInvalidLot},
		{"negative basis lot", ApplyBuy, Lot{CostBasis: -1}, 1, 1, ErrInvalidLot},
		{"basis without shares", ApplySell, Lot{CostBasis: 5}, 1, 1, ErrInvalidLot},
		{"basis below one cent a share", ApplySell, Lot{Quantity: 5, CostBasis: 4}, 1, 1, ErrInvalidLot},
		{"basis above max lot", ApplyBuy, Lot{Quantity: 1, CostBasis: MaxAmount + 1}, 1, 1, ErrInvalidLot},
		{"realized out of range lot", ApplyBuy, Lot{RealizedPnL: math.MinInt64}, 1, 1, ErrInvalidLot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.lot
			got, err := tc.apply(in, tc.q, tc.p)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if got != (Lot{}) || in != tc.lot {
				t.Errorf("on error got %+v and input %+v, want zero Lot and unchanged input", got, in)
			}
		})
	}
}

// TestRandomFills checks the contract invariants over random fill sequences:
// the lot stays valid, money is conserved, and closing the position always
// allocates exactly what was paid.
func TestRandomFills(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		l := seedAAPL
		paid, received := seedAAPL.CostBasis, int64(0)
		for range 50 {
			q := 1 + r.Int64N(30)
			p := 1 + r.Int64N(100000)
			if l.Quantity > 0 && r.IntN(2) == 0 {
				q = min(q, l.Quantity)
				l = sell(t, l, q, p)
				received += q * p
			} else {
				l = buy(t, l, q, p)
				paid += q * p
			}
			if err := l.Validate(); err != nil {
				t.Fatal(err)
			}
			// Basis still held plus basis already realized equals what was paid.
			if l.CostBasis+(received-l.RealizedPnL) != paid {
				t.Fatalf("basis %d + allocated %d != paid %d", l.CostBasis, received-l.RealizedPnL, paid)
			}
		}
		if l.Quantity == 0 {
			continue
		}
		l = sell(t, l, l.Quantity, 1+r.Int64N(100000))
		if l.CostBasis != 0 {
			t.Fatalf("closed lot kept basis %d", l.CostBasis)
		}
	}
}
