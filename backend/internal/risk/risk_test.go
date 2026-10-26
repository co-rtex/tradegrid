package risk

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/co-rtex/tradegrid/backend/internal/matching"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

const (
	buy  = orders.SideBuy
	sell = orders.SideSell
)

// seeded returns the demo accounts from docs/api.md.
func seeded() map[string]Account {
	account := func() Account {
		return Account{
			Cash:      Balance{Total: 10000000},
			Positions: map[string]Balance{"AAPL": {Total: 100}, "MSFT": {Total: 100}},
		}
	}
	return map[string]Account{"alice": account(), "bob": account()}
}

func clone(accounts map[string]Account) map[string]Account {
	out := make(map[string]Account, len(accounts))
	for id, a := range accounts {
		out[id] = a.Clone()
	}
	return out
}

func mustPreflight(t *testing.T, a Account, side orders.Side, symbol string, price, qty int64) Account {
	t.Helper()
	out, err := Preflight(a, side, symbol, price, qty)
	if err != nil {
		t.Fatalf("Preflight(%s %d %s @ %d): %v", side, qty, symbol, price, err)
	}
	return out
}

func mustSettle(t *testing.T, accounts map[string]Account, fills ...Fill) map[string]Account {
	t.Helper()
	out, err := Settle(accounts, fills)
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	return out
}

func TestPreflightReservesExactAmounts(t *testing.T) {
	a := seeded()["bob"]
	got := mustPreflight(t, a, buy, "AAPL", 19050, 25)
	if got.Cash != (Balance{Total: 10000000, Reserved: 476250}) {
		t.Errorf("BUY cash = %+v, want reserved 476250", got.Cash)
	}
	if !reflect.DeepEqual(got.Positions, a.Positions) {
		t.Errorf("BUY changed positions: %+v", got.Positions)
	}

	got = mustPreflight(t, a, sell, "MSFT", 50000, 40)
	if got.Positions["MSFT"] != (Balance{Total: 100, Reserved: 40}) {
		t.Errorf("SELL MSFT = %+v, want reserved 40", got.Positions["MSFT"])
	}
	if got.Cash != a.Cash || got.Positions["AAPL"] != a.Positions["AAPL"] {
		t.Errorf("SELL changed other balances: %+v", got)
	}
}

func TestPreflightBoundaries(t *testing.T) {
	withCash := func(total, reserved int64) Account {
		a := seeded()["bob"]
		a.Cash = Balance{Total: total, Reserved: reserved}
		return a
	}
	withAAPL := func(total, reserved int64) Account {
		a := seeded()["bob"]
		a.Positions["AAPL"] = Balance{Total: total, Reserved: reserved}
		return a
	}

	tests := []struct {
		name    string
		account Account
		side    orders.Side
		symbol  string
		price   int64
		qty     int64
		want    error
	}{
		{"buy exactly available", withCash(1000, 0), buy, "AAPL", 100, 10, nil},
		{"buy one cent short", withCash(999, 0), buy, "AAPL", 100, 10, ErrInsufficientCash},
		{"buy total suffices but reserved does not", withCash(10000, 9001), buy, "AAPL", 100, 10, ErrInsufficientCash},
		{"buy exactly remaining available", withCash(10000, 9000), buy, "AAPL", 100, 10, nil},
		{"fixture 04: 600 at $200 with $100,000", seeded()["bob"], buy, "AAPL", 20000, 600, ErrInsufficientCash},
		{"sell all shares", withAAPL(100, 0), sell, "AAPL", 1, 100, nil},
		{"sell one share too many", withAAPL(100, 0), sell, "AAPL", 1, 101, ErrInsufficientShares},
		{"sell total suffices but reserved does not", withAAPL(100, 99), sell, "AAPL", 1, 2, ErrInsufficientShares},
		{"sell symbol not held", seeded()["bob"], sell, "TSLA", 1, 1, ErrInsufficientShares},
		{"buy notional overflows int64", seeded()["bob"], buy, "AAPL", math.MaxInt64, 2, ErrLimitExceeded},
		{"buy notional exceeds MaxAggregate", seeded()["bob"], buy, "AAPL", MaxAggregate, 2, ErrLimitExceeded},
		{"zero price", seeded()["bob"], buy, "AAPL", 0, 1, ErrInvalidInput},
		{"negative quantity", seeded()["bob"], sell, "AAPL", 100, -1, ErrInvalidInput},
		{"min int64 quantity", seeded()["bob"], sell, "AAPL", 100, math.MinInt64, ErrInvalidInput},
		{"unspecified side", seeded()["bob"], orders.SideUnspecified, "AAPL", 100, 1, ErrInvalidInput},
		{"missing symbol", seeded()["bob"], buy, "", 100, 1, ErrInvalidInput},
		{"reserved above total", withCash(10, 11), buy, "AAPL", 1, 1, ErrInvalidInput},
		{"negative position", withAAPL(-1, 0), buy, "AAPL", 1, 1, ErrInvalidInput},
		{"total above MaxAggregate", withCash(MaxAggregate+1, 0), buy, "AAPL", 1, 1, ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.account.Clone()
			got, err := Preflight(tt.account, tt.side, tt.symbol, tt.price, tt.qty)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err != nil && !reflect.DeepEqual(got, Account{}) {
				t.Errorf("failed Preflight returned %+v, want zero Account", got)
			}
			if err == nil {
				if err := got.Validate(); err != nil {
					t.Errorf("result invalid: %v", err)
				}
			}
			if !reflect.DeepEqual(tt.account, before) {
				t.Errorf("Preflight mutated its input: %+v, was %+v", tt.account, before)
			}
		})
	}
}

func TestPreflightOutstandingReservations(t *testing.T) {
	bob := seeded()["bob"]
	// Two BUYs commit $60,000 + $40,000, all of Bob's cash.
	bob = mustPreflight(t, bob, buy, "AAPL", 20000, 300)
	bob = mustPreflight(t, bob, buy, "MSFT", 40000, 100)
	if bob.Cash.Available() != 0 {
		t.Fatalf("available cash = %d, want 0", bob.Cash.Available())
	}
	if _, err := Preflight(bob, buy, "AAPL", 1, 1); !errors.Is(err, ErrInsufficientCash) {
		t.Errorf("third BUY err = %v, want ErrInsufficientCash", err)
	}
	// Cash reservations do not block selling shares.
	bob = mustPreflight(t, bob, sell, "AAPL", 1, 60)
	bob = mustPreflight(t, bob, sell, "AAPL", 1, 40)
	if _, err := Preflight(bob, sell, "AAPL", 1, 1); !errors.Is(err, ErrInsufficientShares) {
		t.Errorf("selling reserved shares err = %v, want ErrInsufficientShares", err)
	}
	if got := bob.Positions["AAPL"]; got != (Balance{Total: 100, Reserved: 100}) {
		t.Errorf("AAPL = %+v, want fully reserved", got)
	}
}

func TestPreflightDoesNotAliasPositions(t *testing.T) {
	a := seeded()["alice"]
	got := mustPreflight(t, a, sell, "AAPL", 19000, 10)
	got.Positions["MSFT"] = Balance{Total: 1}
	if a.Positions["AAPL"].Reserved != 0 || a.Positions["MSFT"].Total != 100 {
		t.Fatalf("result shares Positions with input: %+v", a.Positions)
	}
}

func TestSettleFullFillWithPriceImprovement(t *testing.T) {
	accounts := seeded()
	accounts["alice"] = mustPreflight(t, accounts["alice"], sell, "AAPL", 19000, 10)
	accounts["bob"] = mustPreflight(t, accounts["bob"], buy, "AAPL", 19100, 10)
	before := clone(accounts)

	got := mustSettle(t, accounts, Fill{BuyerID: "bob", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19100, Price: 19000, Quantity: 10})

	want := seeded()
	want["bob"] = Account{Cash: Balance{Total: 9810000}, Positions: map[string]Balance{"AAPL": {Total: 110}, "MSFT": {Total: 100}}}
	want["alice"] = Account{Cash: Balance{Total: 10190000}, Positions: map[string]Balance{"AAPL": {Total: 90}, "MSFT": {Total: 100}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Settle =\n%+v\nwant\n%+v", got, want)
	}
	if !reflect.DeepEqual(accounts, before) {
		t.Errorf("Settle mutated its input")
	}
}

func TestSettlePartialFillRetainsReservation(t *testing.T) {
	accounts := seeded()
	accounts["alice"] = mustPreflight(t, accounts["alice"], sell, "AAPL", 19000, 10)
	accounts["bob"] = mustPreflight(t, accounts["bob"], buy, "AAPL", 19050, 25)
	accounts["bob"] = mustPreflight(t, accounts["bob"], buy, "MSFT", 20000, 5) // unrelated open order

	got := mustSettle(t, accounts, Fill{BuyerID: "bob", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19050, Price: 19000, Quantity: 10})

	// 15 AAPL rest at $190.50 (285750) plus 5 MSFT at $200.00 (100000).
	if want := (Balance{Total: 9810000, Reserved: 285750 + 100000}); got["bob"].Cash != want {
		t.Errorf("bob cash = %+v, want %+v", got["bob"].Cash, want)
	}
	if got["bob"].Positions["AAPL"] != (Balance{Total: 110}) {
		t.Errorf("bob AAPL = %+v", got["bob"].Positions["AAPL"])
	}
}

func TestSettlePartialSellRetainsShareReservation(t *testing.T) {
	accounts := seeded()
	accounts["alice"] = mustPreflight(t, accounts["alice"], sell, "AAPL", 19000, 30)
	accounts["bob"] = mustPreflight(t, accounts["bob"], buy, "AAPL", 19000, 12)

	got := mustSettle(t, accounts, Fill{BuyerID: "bob", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19000, Price: 19000, Quantity: 12})

	if want := (Balance{Total: 88, Reserved: 18}); got["alice"].Positions["AAPL"] != want {
		t.Errorf("alice AAPL = %+v, want %+v", got["alice"].Positions["AAPL"], want)
	}
	if want := (Balance{Total: 10000000 + 12*19000}); got["alice"].Cash != want {
		t.Errorf("alice cash = %+v, want %+v", got["alice"].Cash, want)
	}
}

func TestSettleSweepReleasesAtBuyLimit(t *testing.T) {
	// Bob's BUY 30 @ $192 sweeps Alice's asks at $190 (10) and $191 (20).
	accounts := seeded()
	accounts["alice"] = mustPreflight(t, accounts["alice"], sell, "AAPL", 19000, 10)
	accounts["alice"] = mustPreflight(t, accounts["alice"], sell, "AAPL", 19100, 20)
	accounts["bob"] = mustPreflight(t, accounts["bob"], buy, "AAPL", 19200, 30)

	got := mustSettle(t, accounts,
		Fill{BuyerID: "bob", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19200, Price: 19000, Quantity: 10},
		Fill{BuyerID: "bob", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19200, Price: 19100, Quantity: 20},
	)

	paid := int64(10*19000 + 20*19100)
	if want := (Balance{Total: 10000000 - paid}); got["bob"].Cash != want {
		t.Errorf("bob cash = %+v, want %+v (improvement released)", got["bob"].Cash, want)
	}
	if want := (Balance{Total: 10000000 + paid}); got["alice"].Cash != want {
		t.Errorf("alice cash = %+v, want %+v", got["alice"].Cash, want)
	}
	if got["alice"].Positions["AAPL"] != (Balance{Total: 70}) || got["bob"].Positions["AAPL"] != (Balance{Total: 130}) {
		t.Errorf("positions alice %+v, bob %+v", got["alice"].Positions["AAPL"], got["bob"].Positions["AAPL"])
	}
}

func TestSettleSameAccountNetting(t *testing.T) {
	accounts := seeded()
	alice := mustPreflight(t, accounts["alice"], sell, "AAPL", 19000, 5)
	alice = mustPreflight(t, alice, buy, "AAPL", 19100, 5)
	accounts["alice"] = alice

	got := mustSettle(t, accounts, Fill{BuyerID: "alice", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19100, Price: 19000, Quantity: 5})

	if !reflect.DeepEqual(got, seeded()) {
		t.Errorf("self fill should net totals and release both reservations, got %+v", got["alice"])
	}
}

func TestSettleSameAccountNettingAtBound(t *testing.T) {
	// Applying the buyer leg first would push the share total past
	// MaxAggregate; netting the deltas first leaves it unchanged.
	accounts := map[string]Account{"max": {
		Cash:      Balance{Total: 1000, Reserved: 100},
		Positions: map[string]Balance{"AAPL": {Total: MaxAggregate, Reserved: 1}},
	}}
	got := mustSettle(t, accounts, Fill{BuyerID: "max", SellerID: "max", Symbol: "AAPL", BuyLimit: 100, Price: 100, Quantity: 1})
	want := Account{Cash: Balance{Total: 1000}, Positions: map[string]Balance{"AAPL": {Total: MaxAggregate}}}
	if !reflect.DeepEqual(got["max"], want) {
		t.Errorf("got %+v, want %+v", got["max"], want)
	}
}

func TestSettleCombinesBuyerAndSellerRolesInOneBatch(t *testing.T) {
	// Carol sells AAPL to Bob and buys MSFT from him in the same batch.
	accounts := seeded()
	accounts["carol"] = Account{Cash: Balance{Total: 50000}, Positions: map[string]Balance{"AAPL": {Total: 3}}}
	accounts["carol"] = mustPreflight(t, accounts["carol"], sell, "AAPL", 10000, 3)
	accounts["carol"] = mustPreflight(t, accounts["carol"], buy, "MSFT", 20000, 2)
	accounts["bob"] = mustPreflight(t, accounts["bob"], buy, "AAPL", 10000, 3)
	accounts["bob"] = mustPreflight(t, accounts["bob"], sell, "MSFT", 20000, 2)

	got := mustSettle(t, accounts,
		Fill{BuyerID: "bob", SellerID: "carol", Symbol: "AAPL", BuyLimit: 10000, Price: 10000, Quantity: 3},
		Fill{BuyerID: "carol", SellerID: "bob", Symbol: "MSFT", BuyLimit: 20000, Price: 20000, Quantity: 2},
	)
	wantCarol := Account{Cash: Balance{Total: 50000 + 30000 - 40000}, Positions: map[string]Balance{"AAPL": {}, "MSFT": {Total: 2}}}
	if !reflect.DeepEqual(got["carol"], wantCarol) {
		t.Errorf("carol = %+v, want %+v", got["carol"], wantCarol)
	}
	if !reflect.DeepEqual(got["alice"], seeded()["alice"]) {
		t.Errorf("uninvolved account changed: %+v", got["alice"])
	}
}

func TestSettleEmptyBatchCopies(t *testing.T) {
	accounts := seeded()
	got := mustSettle(t, accounts)
	if !reflect.DeepEqual(got, accounts) {
		t.Fatalf("empty batch changed accounts")
	}
	got["alice"].Positions["AAPL"] = Balance{}
	if accounts["alice"].Positions["AAPL"].Total != 100 {
		t.Fatalf("result aliases input Positions")
	}
}

func TestSettleRejectsWithoutMutation(t *testing.T) {
	reserved := func() map[string]Account {
		a := seeded()
		a["alice"] = mustPreflightNoT(a["alice"], sell, "AAPL", 19000, 10)
		a["bob"] = mustPreflightNoT(a["bob"], buy, "AAPL", 19100, 10)
		return a
	}
	good := Fill{BuyerID: "bob", SellerID: "alice", Symbol: "AAPL", BuyLimit: 19100, Price: 19000, Quantity: 10}
	with := func(edit func(*Fill)) Fill { f := good; edit(&f); return f }

	richSeller := func() map[string]Account {
		a := reserved()
		a["alice"] = Account{Cash: Balance{Total: MaxAggregate - 100}, Positions: map[string]Balance{"AAPL": {Total: 10, Reserved: 10}}}
		return a
	}
	fullBuyer := func() map[string]Account {
		a := reserved()
		bob := a["bob"]
		bob.Positions["AAPL"] = Balance{Total: MaxAggregate - 5}
		a["bob"] = bob
		return a
	}

	tests := []struct {
		name     string
		accounts map[string]Account
		fills    []Fill
		want     error
	}{
		{"unknown buyer", reserved(), []Fill{with(func(f *Fill) { f.BuyerID = "carol" })}, ErrUnknownAccount},
		{"unknown seller", reserved(), []Fill{with(func(f *Fill) { f.SellerID = "carol" })}, ErrUnknownAccount},
		{"missing symbol", reserved(), []Fill{with(func(f *Fill) { f.Symbol = "" })}, ErrInvalidInput},
		{"zero quantity", reserved(), []Fill{with(func(f *Fill) { f.Quantity = 0 })}, ErrInvalidInput},
		{"zero price", reserved(), []Fill{with(func(f *Fill) { f.Price = 0 })}, ErrInvalidInput},
		{"price above buy limit", reserved(), []Fill{with(func(f *Fill) { f.Price = 19101 })}, ErrInvalidInput},
		{"nothing reserved", seeded(), []Fill{good}, ErrInconsistent},
		{"fill larger than reservation", reserved(), []Fill{with(func(f *Fill) { f.Quantity = 11 })}, ErrInconsistent},
		{"second fill overdraws reservation", reserved(), []Fill{good, good}, ErrInconsistent},
		{"buy limit higher than reserved", reserved(), []Fill{with(func(f *Fill) { f.BuyLimit = 19200 })}, ErrInconsistent},
		{"wrong symbol reserved", reserved(), []Fill{with(func(f *Fill) { f.Symbol = "MSFT" })}, ErrInconsistent},
		{"seller cash past bound", richSeller(), []Fill{good}, ErrLimitExceeded},
		{"buyer shares past bound", fullBuyer(), []Fill{good}, ErrLimitExceeded},
		{"payment overflows", reserved(), []Fill{with(func(f *Fill) { f.Price, f.BuyLimit = MaxAggregate, MaxAggregate })}, ErrLimitExceeded},
		{"later fill invalid", reserved(), []Fill{good, with(func(f *Fill) { f.Quantity = -1 })}, ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := clone(tt.accounts)
			got, err := Settle(tt.accounts, tt.fills)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if got != nil {
				t.Errorf("failed Settle returned %+v, want nil", got)
			}
			if !reflect.DeepEqual(tt.accounts, before) {
				t.Errorf("failed Settle mutated its input")
			}
		})
	}
}

func mustPreflightNoT(a Account, side orders.Side, symbol string, price, qty int64) Account {
	out, err := Preflight(a, side, symbol, price, qty)
	if err != nil {
		panic(err)
	}
	return out
}

// TestFixtures replays docs/fixtures through Preflight and Settle and compares
// the resulting ledger with each fixture's GET /api/v1/accounts.
func TestFixtures(t *testing.T) {
	type wireBalance struct{ Total, Reserved, Available int64 }
	type wireAccount struct {
		UserID    string
		Cash      wireBalance
		Positions []struct {
			Symbol string
			wireBalance
		}
	}
	type submission struct {
		Body struct {
			UserID, Symbol, Side string
			Price, Quantity      int64
		}
	}
	type fixture struct {
		Setup    []submission
		Request  *submission
		Response struct {
			Status int
			Body   struct {
				Executions []struct {
					Price, Quantity           int64
					BuyOrderID, SellOrderID   string
					BuyerUserID, SellerUserID string
				}
				Error struct{ Code string }
			}
		}
		After map[string]json.RawMessage
	}
	ledger := func(t *testing.T, raw json.RawMessage) map[string]Account {
		t.Helper()
		var body struct{ Accounts []wireAccount }
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		out := map[string]Account{}
		for _, a := range body.Accounts {
			acct := Account{Cash: Balance{a.Cash.Total, a.Cash.Reserved}, Positions: map[string]Balance{}}
			if a.Cash.Available != acct.Cash.Available() {
				t.Fatalf("fixture %s cash available %d inconsistent", a.UserID, a.Cash.Available)
			}
			for _, p := range a.Positions {
				acct.Positions[p.Symbol] = Balance{p.Total, p.Reserved}
				if p.Available != p.Total-p.Reserved {
					t.Fatalf("fixture %s %s available inconsistent", a.UserID, p.Symbol)
				}
			}
			out[a.UserID] = acct
		}
		return out
	}
	load := func(t *testing.T, name string) fixture {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		var f fixture
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatal(err)
		}
		return f
	}

	seed := ledger(t, load(t, "00-seed.json").After["GET /api/v1/accounts"])
	if !reflect.DeepEqual(seed, seeded()) {
		t.Fatalf("00-seed.json accounts = %+v, want docs/api.md demo accounts", seed)
	}

	for _, name := range []string{"01-no-fill.json", "02-full-fill.json", "03-partial-fill.json", "04-risk-rejection.json"} {
		t.Run(name, func(t *testing.T) {
			f := load(t, name)
			accounts := clone(seed)
			limits := map[string]int64{} // order ID -> limit price
			submit := func(s submission) error {
				side, err := orders.ParseSide(s.Body.Side)
				if err != nil {
					t.Fatal(err)
				}
				next, err := Preflight(accounts[s.Body.UserID], side, s.Body.Symbol, s.Body.Price, s.Body.Quantity)
				if err != nil {
					return err
				}
				accounts[s.Body.UserID] = next
				limits[fmt.Sprintf("ord-%d", len(limits)+1)] = s.Body.Price
				return nil
			}
			for _, s := range f.Setup {
				if err := submit(s); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			err := submit(*f.Request)
			switch code := f.Response.Body.Error.Code; code {
			case "":
				if err != nil {
					t.Fatalf("request rejected: %v", err)
				}
			case "INSUFFICIENT_CASH":
				if !errors.Is(err, ErrInsufficientCash) {
					t.Fatalf("err = %v, want ErrInsufficientCash", err)
				}
			default:
				t.Fatalf("fixture uses unhandled code %s", code)
			}

			var fills []Fill
			for _, e := range f.Response.Body.Executions {
				fills = append(fills, Fill{
					BuyerID: e.BuyerUserID, SellerID: e.SellerUserID, Symbol: f.Request.Body.Symbol,
					BuyLimit: limits[e.BuyOrderID], Price: e.Price, Quantity: e.Quantity,
				})
			}
			got := mustSettle(t, accounts, fills...)
			if want := ledger(t, f.After["GET /api/v1/accounts"]); !reflect.DeepEqual(got, want) {
				t.Errorf("accounts =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// TestSimulationInvariants drives the real matching engine with a fixed
// pseudo-random order stream and checks, after every submission, that cash and
// shares are conserved and that every reservation equals what the account's
// open orders still need.
func TestSimulationInvariants(t *testing.T) {
	type open struct {
		user  string
		side  orders.Side
		price int64
		left  int64
	}
	users := []string{"alice", "bob", "carol"}
	symbols := []string{"AAPL", "MSFT"}
	accounts := seeded()
	accounts["carol"] = Account{Cash: Balance{Total: 500000}, Positions: map[string]Balance{"AAPL": {Total: 10}}}
	totals := func(a map[string]Account) (cash int64, shares map[string]int64) {
		shares = map[string]int64{}
		for _, acct := range a {
			cash += acct.Cash.Total
			for s, b := range acct.Positions {
				shares[s] += b.Total
			}
		}
		return cash, shares
	}
	wantCash, wantShares := totals(accounts)

	rng := rand.New(rand.NewPCG(34, 2026))
	book := matching.NewBook()
	live := map[string]*open{}
	symbolOf := map[string]string{}
	accepted, rejected, filled := 0, 0, 0

	for i := range 2000 {
		user := users[rng.IntN(len(users))]
		symbol := symbols[rng.IntN(len(symbols))]
		side := buy
		if rng.IntN(2) == 0 {
			side = sell
		}
		price := 18000 + rng.Int64N(21)*100
		qty := 1 + rng.Int64N(40)

		next, err := Preflight(accounts[user], side, symbol, price, qty)
		if err != nil {
			if !errors.Is(err, ErrInsufficientCash) && !errors.Is(err, ErrInsufficientShares) {
				t.Fatalf("step %d: unexpected Preflight error %v", i, err)
			}
			rejected++
			continue
		}
		accepted++
		candidate := clone(accounts)
		candidate[user] = next

		id := fmt.Sprintf("ord-%d", i)
		res := book.Submit(matching.Order{ID: id, Symbol: symbol, Side: side, Price: price, Quantity: qty})
		if res.Err != nil {
			t.Fatalf("step %d: Submit: %v", i, res.Err)
		}
		incoming := &open{user: user, side: side, price: price, left: qty}
		var fills []Fill
		for _, e := range res.Executions {
			r := live[e.RestingID]
			b, s := incoming, r
			if side == sell {
				b, s = r, incoming
			}
			fills = append(fills, Fill{BuyerID: b.user, SellerID: s.user, Symbol: symbol, BuyLimit: b.price, Price: e.Price, Quantity: e.Quantity})
			r.left -= e.Quantity
			incoming.left -= e.Quantity
			if r.left == 0 {
				delete(live, e.RestingID)
			}
			filled++
		}
		if incoming.left > 0 {
			live[id] = incoming
			symbolOf[id] = symbol
		}

		settled, err := Settle(candidate, fills)
		if err != nil {
			t.Fatalf("step %d: Settle: %v", i, err)
		}
		accounts = settled

		cash, shares := totals(accounts)
		if cash != wantCash || !reflect.DeepEqual(shares, wantShares) {
			t.Fatalf("step %d: totals cash %d shares %v, want %d %v", i, cash, shares, wantCash, wantShares)
		}
		wantReserved := map[string]Account{}
		for _, u := range users {
			wantReserved[u] = Account{Positions: map[string]Balance{}}
		}
		for oid, o := range live {
			a := wantReserved[o.user]
			if o.side == buy {
				a.Cash.Reserved += o.price * o.left
			} else {
				p := a.Positions[symbolOf[oid]]
				p.Reserved += o.left
				a.Positions[symbolOf[oid]] = p
			}
			wantReserved[o.user] = a
		}
		for _, u := range users {
			got := accounts[u]
			if err := got.Validate(); err != nil {
				t.Fatalf("step %d: %s invalid: %v", i, u, err)
			}
			if got.Cash.Reserved != wantReserved[u].Cash.Reserved {
				t.Fatalf("step %d: %s cash reserved %d, open orders need %d", i, u, got.Cash.Reserved, wantReserved[u].Cash.Reserved)
			}
			for _, s := range symbols {
				if got.Positions[s].Reserved != wantReserved[u].Positions[s].Reserved {
					t.Fatalf("step %d: %s %s reserved %d, open orders need %d", i, u, s, got.Positions[s].Reserved, wantReserved[u].Positions[s].Reserved)
				}
			}
		}
	}
	if accepted == 0 || rejected == 0 || filled == 0 {
		t.Fatalf("simulation too narrow: accepted %d, rejected %d, fills %d", accepted, rejected, filled)
	}
}
