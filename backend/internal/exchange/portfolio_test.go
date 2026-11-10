package exchange

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/instruments"
	"github.com/co-rtex/tradegrid/backend/internal/portfolio"
)

func order(user, symbol, side string, qty, cents int64) SubmitRequest {
	return SubmitRequest{UserID: user, Symbol: symbol, Side: side, Type: "LIMIT", Price: cents, Quantity: qty}
}

func mustPortfolio(t *testing.T, s *Service, user string) Portfolio {
	t.Helper()
	p, err := s.Portfolio(user)
	if err != nil {
		t.Fatalf("Portfolio(%s): %v", user, err)
	}
	return p
}

// lots returns an account's lot per symbol.
func lots(t *testing.T, s *Service, user string) map[string]portfolio.Lot {
	t.Helper()
	out := map[string]portfolio.Lot{}
	for _, h := range mustPortfolio(t, s, user).Holdings {
		out[h.Symbol] = h.Lot
	}
	return out
}

func lot(qty, basis, realized int64) portfolio.Lot {
	return portfolio.Lot{Quantity: qty, CostBasis: basis, RealizedPnL: realized}
}

// checkPortfolioInvariants checks docs/portfolio.md's invariants on the
// published state: every lot is valid and covers exactly the account's
// shares, each account's equity minus the seeded equity equals its realized
// plus unrealized P&L, and across accounts every symbol's basis minus
// realized P&L is the seeded basis, so trading only moves value between
// accounts.
func checkPortfolioInvariants(t *testing.T, s *Service) {
	t.Helper()
	const seededEquity = 16_100_000
	basisLessRealized := map[string]int64{}
	var combinedEquity int64
	for _, user := range []string{"alice", "bob"} {
		p := mustPortfolio(t, s, user)
		equity, pnl := p.Cash.Total, int64(0)
		for _, h := range p.Holdings {
			if err := h.Lot.Validate(); err != nil || h.Lot.Quantity != h.Shares.Total {
				t.Fatalf("%s %s: lot %+v for shares %+v (%v)", user, h.Symbol, h.Lot, h.Shares, err)
			}
			ref, err := instruments.LookupReferencePrice(h.Symbol)
			if err != nil {
				t.Fatal(err)
			}
			mv := h.Shares.Total * ref
			equity += mv
			pnl += h.Lot.RealizedPnL + mv - h.Lot.CostBasis
			basisLessRealized[h.Symbol] += h.Lot.CostBasis - h.Lot.RealizedPnL
		}
		if equity-seededEquity != pnl {
			t.Errorf("%s: equity %d - %d != realized + unrealized %d", user, equity, seededEquity, pnl)
		}
		combinedEquity += equity
	}
	if combinedEquity != 2*seededEquity {
		t.Errorf("combined equity %d, want %d", combinedEquity, 2*seededEquity)
	}
	for symbol, got := range basisLessRealized {
		ref, _ := instruments.LookupReferencePrice(symbol)
		if want := 200 * ref; got != want {
			t.Errorf("%s: combined basis - realized = %d, want %d", symbol, got, want)
		}
	}
}

func TestPortfolioSeed(t *testing.T) {
	s := newTestService()
	for _, user := range []string{"alice", "bob"} {
		p := mustPortfolio(t, s, user)
		if p.AsOfSequence != 0 || p.UserID != user || p.Cash.Total != 10_000_000 {
			t.Errorf("Portfolio(%s) = %+v", user, p)
		}
		want := map[string]portfolio.Lot{"AAPL": lot(100, 1_900_000, 0), "MSFT": lot(100, 4_200_000, 0)}
		if got := lots(t, s, user); !reflect.DeepEqual(got, want) {
			t.Errorf("%s lots = %+v, want %+v", user, got, want)
		}
	}
	checkPortfolioInvariants(t, s)
}

// TestPortfolioWorkedExamples replays docs/portfolio.md's worked examples in
// order on one exchange and checks every documented basis, realized P&L,
// share, and cash value after each step.
func TestPortfolioWorkedExamples(t *testing.T) {
	s := newTestService()
	type want struct {
		cash        int64
		aapl, msft  portfolio.Lot
		aaplReserve int64
	}
	check := func(step string, alice, bob want) {
		t.Helper()
		for user, w := range map[string]want{"alice": alice, "bob": bob} {
			p := mustPortfolio(t, s, user)
			got := lots(t, s, user)
			if p.Cash.Total != w.cash || got["AAPL"] != w.aapl || got["MSFT"] != w.msft {
				t.Errorf("%s %s: cash %d AAPL %+v MSFT %+v\nwant cash %d AAPL %+v MSFT %+v",
					step, user, p.Cash.Total, got["AAPL"], got["MSFT"], w.cash, w.aapl, w.msft)
			}
			if r := p.Holdings[0].Shares.Reserved; p.Holdings[0].Symbol != "AAPL" || r != w.aaplReserve {
				t.Errorf("%s %s: AAPL reserved %d, want %d", step, user, r, w.aaplReserve)
			}
		}
		checkPortfolioInvariants(t, s)
	}
	seedMSFT := lot(100, 4_200_000, 0)

	// 1. Basic BUY and SELL.
	mustSubmit(t, s, order("alice", "AAPL", "SELL", 10, 18500))
	mustSubmit(t, s, order("bob", "AAPL", "BUY", 10, 18500))
	check("example 1",
		want{cash: 10_185_000, aapl: lot(90, 1_710_000, -5000), msft: seedMSFT},
		want{cash: 9_815_000, aapl: lot(110, 2_085_000, 0), msft: seedMSFT})

	// 2. Price improvement: Bob's basis grows by the 94000 he paid, not his
	// 97500 limit.
	mustSubmit(t, s, order("alice", "AAPL", "SELL", 5, 18800))
	mustSubmit(t, s, order("bob", "AAPL", "BUY", 5, 19500))
	check("example 2",
		want{cash: 10_279_000, aapl: lot(85, 1_615_000, -6000), msft: seedMSFT},
		want{cash: 9_721_000, aapl: lot(115, 2_179_000, 0), msft: seedMSFT})

	// 3. Partial fill with rounding: floor(2179000 × 3 / 115) = 56843. Bob's
	// 4 unfilled shares stay reserved and inside his basis.
	mustSubmit(t, s, order("bob", "AAPL", "SELL", 7, 19300))
	mustSubmit(t, s, order("alice", "AAPL", "BUY", 3, 19300))
	check("example 3",
		want{cash: 10_221_100, aapl: lot(88, 1_672_900, -6000), msft: seedMSFT},
		want{cash: 9_778_900, aapl: lot(112, 2_122_157, 1057), msft: seedMSFT, aaplReserve: 4})

	// 4. Multiple fills in one submission that close Alice's MSFT position;
	// the closing fill allocates all remaining basis.
	mustSubmit(t, s, order("alice", "MSFT", "SELL", 60, 41500))
	mustSubmit(t, s, order("alice", "MSFT", "SELL", 40, 42500))
	res := mustSubmit(t, s, order("bob", "MSFT", "BUY", 100, 43000))
	if len(res.Executions) != 2 {
		t.Fatalf("example 4: %d fills, want 2", len(res.Executions))
	}
	check("example 4",
		want{cash: 14_411_100, aapl: lot(88, 1_672_900, -6000), msft: lot(0, 0, -10000)},
		want{cash: 5_588_900, aapl: lot(112, 2_122_157, 1057), msft: lot(200, 8_390_000, 0), aaplReserve: 4})

	// 5. Same-account trade: nothing changes.
	mustSubmit(t, s, order("alice", "AAPL", "SELL", 2, 19000))
	res = mustSubmit(t, s, order("alice", "AAPL", "BUY", 2, 19000))
	if len(res.Executions) != 1 || res.Executions[0].BuyerUserID != res.Executions[0].SellerUserID {
		t.Fatalf("example 5 did not self-match: %+v", res.Executions)
	}
	check("example 5",
		want{cash: 14_411_100, aapl: lot(88, 1_672_900, -6000), msft: lot(0, 0, -10000)},
		want{cash: 5_588_900, aapl: lot(112, 2_122_157, 1057), msft: lot(200, 8_390_000, 0), aaplReserve: 4})

	if p := mustPortfolio(t, s, "alice"); p.AsOfSequence != 11 {
		t.Errorf("AsOfSequence = %d, want 11", p.AsOfSequence)
	}
}

// A resting order changes no basis; rejected and rolled-back submissions
// change no basis or P&L.
func TestPortfolioUnchangedWithoutCommittedFills(t *testing.T) {
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
	before := map[string]map[string]portfolio.Lot{"alice": lots(t, s, "alice"), "bob": lots(t, s, "bob")}
	unchanged := func(step string) {
		t.Helper()
		for user, want := range before {
			if got := lots(t, s, user); !reflect.DeepEqual(got, want) {
				t.Errorf("%s changed %s's lots: %+v, want %+v", step, user, got, want)
			}
		}
	}

	mustSubmit(t, s, order("alice", "AAPL", "SELL", 2, 19000))
	mustSubmit(t, s, order("alice", "AAPL", "SELL", 2, 19100))
	unchanged("resting orders")

	if _, err := s.Submit(order("bob", "AAPL", "BUY", 600, 20000)); err == nil {
		t.Fatal("oversized BUY accepted")
	}
	unchanged("risk rejection")

	failNext = true
	var e *Error
	if _, err := s.Submit(order("bob", "AAPL", "BUY", 3, 19100)); !errors.As(err, &e) || e.Code != CodeInternal {
		t.Fatalf("err = %v, want INTERNAL", err)
	}
	unchanged("rolled-back submission")
	checkPortfolioInvariants(t, s)

	// The same submission then commits once, with both fills applied once.
	mustSubmit(t, s, order("bob", "AAPL", "BUY", 3, 19100))
	// Alice sells 2 @ 19000 then 1 @ 19100: allocate 38000 then 19000.
	if got, want := lots(t, s, "alice")["AAPL"], lot(97, 1_843_000, 100); got != want {
		t.Errorf("alice AAPL = %+v, want %+v", got, want)
	}
	if got, want := lots(t, s, "bob")["AAPL"], lot(103, 1_957_100, 0); got != want {
		t.Errorf("bob AAPL = %+v, want %+v", got, want)
	}
}

func TestPortfolioErrors(t *testing.T) {
	s := newTestService()
	for user, code := range map[string]Code{"": CodeInvalidQuery, "carol": CodeUnknownUser, "Alice": CodeUnknownUser} {
		_, err := s.Portfolio(user)
		var e *Error
		if !errors.As(err, &e) || e.Code != code || e.Field != "userId" {
			t.Errorf("Portfolio(%q) = %v, want %s on userId", user, err, code)
		}
	}
}

func TestPortfolioIsACopy(t *testing.T) {
	s := newTestService()
	p := mustPortfolio(t, s, "alice")
	p.Holdings[0].Lot.CostBasis = 1
	p.Holdings[1].Shares.Total = 0
	if got := mustPortfolio(t, s, "alice"); got.Holdings[0].Lot.CostBasis != 1_900_000 || got.Holdings[1].Shares.Total != 100 {
		t.Errorf("caller change reached the service: %+v", got)
	}
}

// Random trading, including same-account matches and partial fills, keeps
// every portfolio invariant after every submission.
func TestPortfolioInvariantsUnderRandomTrading(t *testing.T) {
	s := newTestService()
	rng := rand.New(rand.NewPCG(68, 1))
	users, symbols, sides := []string{"alice", "bob"}, []string{"AAPL", "MSFT"}, []string{"BUY", "SELL"}
	base := map[string]int64{"AAPL": 19000, "MSFT": 42000}
	var accepted int
	for range 400 {
		symbol := symbols[rng.IntN(2)]
		req := order(users[rng.IntN(2)], symbol, sides[rng.IntN(2)], 1+rng.Int64N(9), base[symbol]-300+rng.Int64N(601))
		if _, err := s.Submit(req); err == nil {
			accepted++
		}
		checkPortfolioInvariants(t, s)
		if t.Failed() {
			t.Fatalf("after %+v", req)
		}
	}
	if accepted < 300 {
		t.Fatalf("only %d of 400 submissions accepted", accepted)
	}
}

// Portfolio reads taken while submissions run always see one committed
// state: lots match shares, and the sequence never goes backwards.
func TestPortfolioConcurrentWithSubmissions(t *testing.T) {
	s := newTestService()
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(w), 2))
			for range 100 {
				user, side := "alice", "SELL"
				if rng.IntN(2) == 0 {
					user, side = "bob", "BUY"
				}
				_, _ = s.Submit(order(user, "AAPL", side, 1+rng.Int64N(3), 18900+rng.Int64N(201)))
			}
		}()
	}
	errs := make(chan error, 2)
	for _, user := range []string{"alice", "bob"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var last int64
			for range 200 {
				p, err := s.Portfolio(user)
				if err != nil {
					errs <- err
					return
				}
				if p.AsOfSequence < last {
					errs <- fmt.Errorf("%s: sequence went back from %d to %d", user, last, p.AsOfSequence)
					return
				}
				last = p.AsOfSequence
				for _, h := range p.Holdings {
					if h.Lot.Quantity != h.Shares.Total || h.Lot.Validate() != nil {
						errs <- fmt.Errorf("%s %s: lot %+v for shares %+v", user, h.Symbol, h.Lot, h.Shares)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	checkPortfolioInvariants(t, s)
}
