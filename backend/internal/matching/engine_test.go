package matching

import (
	"reflect"
	"slices"
	"testing"
)

// Test cases follow the worked examples in docs/matching-engine.md. Prices are
// written in whole dollars and converted to cents.

const sym = "AAPL"

func usd(dollars int64) int64 { return dollars * 100 }

func buy(id string, qty, dollars int64) Order {
	return Order{ID: id, Symbol: sym, Side: Buy, Price: usd(dollars), Quantity: qty}
}

func sell(id string, qty, dollars int64) Order {
	return Order{ID: id, Symbol: sym, Side: Sell, Price: usd(dollars), Quantity: qty}
}

func exec(incoming, resting string, qty, dollars int64) Execution {
	return Execution{IncomingID: incoming, RestingID: resting, Symbol: sym, Price: usd(dollars), Quantity: qty}
}

func entry(id string, remaining, dollars int64) BookEntry {
	return BookEntry{ID: id, Price: usd(dollars), Remaining: remaining}
}

type matchCase struct {
	name string
	// resting orders are submitted in this order (their acceptance order) to
	// build the starting book. They must not trade with each other.
	resting       []Order
	incoming      Order
	wantExecs     []Execution
	wantRemaining int64
	// Expected final book: bids highest first, asks lowest first, FIFO within
	// a price.
	wantBids []BookEntry
	wantAsks []BookEntry
}

var matchCases = []matchCase{
	// Examples 1-7 from the spec.
	{
		name:          "example 1: no match, buy below best ask rests",
		resting:       []Order{sell("S1", 5, 101)},
		incoming:      buy("B1", 4, 100),
		wantExecs:     nil,
		wantRemaining: 4,
		wantBids:      []BookEntry{entry("B1", 4, 100)},
		wantAsks:      []BookEntry{entry("S1", 5, 101)},
	},
	{
		name:          "example 2: simple full fill at resting price",
		resting:       []Order{sell("S1", 10, 199)},
		incoming:      buy("B1", 10, 200),
		wantExecs:     []Execution{exec("B1", "S1", 10, 199)},
		wantRemaining: 0,
		wantBids:      nil,
		wantAsks:      nil,
	},
	{
		name:          "example 3: partial fill, incoming remainder rests",
		resting:       []Order{sell("S1", 4, 100)},
		incoming:      buy("B1", 10, 101),
		wantExecs:     []Execution{exec("B1", "S1", 4, 100)},
		wantRemaining: 6,
		wantBids:      []BookEntry{entry("B1", 6, 101)},
		wantAsks:      nil,
	},
	{
		name:     "example 4: FIFO at the same price",
		resting:  []Order{sell("S1", 4, 100), sell("S2", 6, 100), sell("S3", 2, 100)},
		incoming: buy("B1", 7, 100),
		wantExecs: []Execution{
			exec("B1", "S1", 4, 100),
			exec("B1", "S2", 3, 100),
		},
		wantRemaining: 0,
		wantBids:      nil,
		wantAsks:      []BookEntry{entry("S2", 3, 100), entry("S3", 2, 100)},
	},
	{
		name:     "example 5: price beats time across bid levels",
		resting:  []Order{buy("B1", 2, 100), buy("B2", 3, 101), buy("B3", 4, 102)},
		incoming: sell("S1", 9, 100),
		wantExecs: []Execution{
			exec("S1", "B3", 4, 102),
			exec("S1", "B2", 3, 101),
			exec("S1", "B1", 2, 100),
		},
		wantRemaining: 0,
		wantBids:      nil,
		wantAsks:      nil,
	},
	{
		name:     "example 6: partial walk across ask levels",
		resting:  []Order{sell("S1", 8, 100), sell("S2", 3, 99)},
		incoming: buy("B1", 7, 101),
		wantExecs: []Execution{
			exec("B1", "S2", 3, 99),
			exec("B1", "S1", 4, 100),
		},
		wantRemaining: 0,
		wantBids:      nil,
		wantAsks:      []BookEntry{entry("S1", 4, 100)},
	},
	{
		name:     "example 7: stop at the non-marketable boundary",
		resting:  []Order{buy("B1", 3, 102), buy("B2", 4, 101), buy("B3", 5, 100)},
		incoming: sell("S1", 10, 101),
		wantExecs: []Execution{
			exec("S1", "B1", 3, 102),
			exec("S1", "B2", 4, 101),
		},
		wantRemaining: 3,
		wantBids:      []BookEntry{entry("B3", 5, 100)},
		wantAsks:      []BookEntry{entry("S1", 3, 101)},
	},

	// Mirrored cases: the same rules with BUY and SELL swapped.
	{
		name:          "mirror of 1: sell above best bid rests",
		resting:       []Order{buy("B1", 5, 99)},
		incoming:      sell("S1", 4, 100),
		wantExecs:     nil,
		wantRemaining: 4,
		wantBids:      []BookEntry{entry("B1", 5, 99)},
		wantAsks:      []BookEntry{entry("S1", 4, 100)},
	},
	{
		name:          "mirror of 3: partial sell fill at resting bid price",
		resting:       []Order{buy("B1", 4, 100)},
		incoming:      sell("S1", 10, 99),
		wantExecs:     []Execution{exec("S1", "B1", 4, 100)},
		wantRemaining: 6,
		wantBids:      nil,
		wantAsks:      []BookEntry{entry("S1", 6, 99)},
	},
	{
		name:     "mirror of 4: FIFO among bids at the same price",
		resting:  []Order{buy("B1", 4, 100), buy("B2", 6, 100), buy("B3", 2, 100)},
		incoming: sell("S1", 7, 100),
		wantExecs: []Execution{
			exec("S1", "B1", 4, 100),
			exec("S1", "B2", 3, 100),
		},
		wantRemaining: 0,
		wantBids:      []BookEntry{entry("B2", 3, 100), entry("B3", 2, 100)},
		wantAsks:      nil,
	},
	{
		name:     "mirror of 7: buy stops at the non-marketable ask",
		resting:  []Order{sell("S1", 3, 98), sell("S2", 4, 99), sell("S3", 5, 100)},
		incoming: buy("B1", 10, 99),
		wantExecs: []Execution{
			exec("B1", "S1", 3, 98),
			exec("B1", "S2", 4, 99),
		},
		wantRemaining: 3,
		wantBids:      []BookEntry{entry("B1", 3, 99)},
		wantAsks:      []BookEntry{entry("S3", 5, 100)},
	},

	// Edge cases suggested by the spec.
	{
		name:          "buy into an empty book rests",
		incoming:      buy("B1", 5, 100),
		wantExecs:     nil,
		wantRemaining: 5,
		wantBids:      []BookEntry{entry("B1", 5, 100)},
		wantAsks:      nil,
	},
	{
		name:          "sell into an empty book rests",
		incoming:      sell("S1", 5, 100),
		wantExecs:     nil,
		wantRemaining: 5,
		wantBids:      nil,
		wantAsks:      []BookEntry{entry("S1", 5, 100)},
	},
	{
		name:          "resting order larger than incoming keeps its remainder",
		resting:       []Order{sell("S1", 10, 100)},
		incoming:      buy("B1", 3, 100),
		wantExecs:     []Execution{exec("B1", "S1", 3, 100)},
		wantRemaining: 0,
		wantBids:      nil,
		wantAsks:      []BookEntry{entry("S1", 7, 100)},
	},
	{
		name:     "same price trades are not aggregated into one execution",
		resting:  []Order{sell("S1", 1, 100), sell("S2", 1, 100)},
		incoming: buy("B1", 2, 100),
		wantExecs: []Execution{
			exec("B1", "S1", 1, 100),
			exec("B1", "S2", 1, 100),
		},
		wantRemaining: 0,
		wantBids:      nil,
		wantAsks:      nil,
	},
	{
		name:          "remainder rests behind earlier orders at its price",
		resting:       []Order{buy("B1", 5, 100), sell("S1", 2, 101)},
		incoming:      buy("B2", 3, 101),
		wantExecs:     []Execution{exec("B2", "S1", 2, 101)},
		wantRemaining: 1,
		wantBids:      []BookEntry{entry("B2", 1, 101), entry("B1", 5, 100)},
		wantAsks:      nil,
	},
	{
		name:          "remainder at an existing price joins the back of the queue",
		resting:       []Order{buy("B1", 5, 100), buy("B2", 2, 99)},
		incoming:      buy("B3", 3, 100),
		wantExecs:     nil,
		wantRemaining: 3,
		wantBids:      []BookEntry{entry("B1", 5, 100), entry("B3", 3, 100), entry("B2", 2, 99)},
		wantAsks:      nil,
	},
}

// run builds the starting book, submits the incoming order and returns the
// result along with the final book.
func run(t *testing.T, tc matchCase) (*Book, Result) {
	t.Helper()
	bk := NewBook()
	for _, o := range tc.resting {
		if res := bk.Submit(o); len(res.Executions) != 0 {
			t.Fatalf("starting order %s traded while building the book: %v", o.ID, res.Executions)
		}
	}
	return bk, bk.Submit(tc.incoming)
}

func TestSubmit(t *testing.T) {
	for _, tc := range matchCases {
		t.Run(tc.name, func(t *testing.T) {
			bk, got := run(t, tc)

			if !slices.Equal(got.Executions, tc.wantExecs) {
				t.Errorf("executions:\n got  %v\n want %v", got.Executions, tc.wantExecs)
			}
			if got.Remaining != tc.wantRemaining {
				t.Errorf("incoming remaining = %d, want %d", got.Remaining, tc.wantRemaining)
			}
			if bids := bk.Bids(sym); !slices.Equal(bids, tc.wantBids) {
				t.Errorf("bids:\n got  %v\n want %v", bids, tc.wantBids)
			}
			if asks := bk.Asks(sym); !slices.Equal(asks, tc.wantAsks) {
				t.Errorf("asks:\n got  %v\n want %v", asks, tc.wantAsks)
			}

			checkInvariants(t, tc.incoming, got, bk)
		})
	}
}

// checkInvariants verifies spec invariants that must hold after every input.
func checkInvariants(t *testing.T, incoming Order, got Result, bk *Book) {
	t.Helper()

	// Execution quantity, conservation and resting price (invariants 4-7).
	var filled int64
	for _, e := range got.Executions {
		if e.Quantity <= 0 {
			t.Errorf("execution %v has non-positive quantity", e)
		}
		if e.Symbol != incoming.Symbol {
			t.Errorf("execution %v has symbol %q, want %q", e, e.Symbol, incoming.Symbol)
		}
		if incoming.Side == Buy && e.Price > incoming.Price ||
			incoming.Side == Sell && e.Price < incoming.Price {
			t.Errorf("execution %v is outside the incoming limit %d", e, incoming.Price)
		}
		filled += e.Quantity
	}
	if got.Remaining < 0 || filled+got.Remaining != incoming.Quantity {
		t.Errorf("filled %d + remaining %d != original %d", filled, got.Remaining, incoming.Quantity)
	}

	// Completed book (invariant 12): positive quantities, sorted sides and no
	// crossed prices.
	bids, asks := bk.Bids(incoming.Symbol), bk.Asks(incoming.Symbol)
	for _, e := range append(slices.Clone(bids), asks...) {
		if e.Remaining <= 0 {
			t.Errorf("book entry %v has non-positive remaining quantity", e)
		}
	}
	for i := 1; i < len(bids); i++ {
		if bids[i].Price > bids[i-1].Price {
			t.Errorf("bids not sorted highest first: %v", bids)
		}
	}
	for i := 1; i < len(asks); i++ {
		if asks[i].Price < asks[i-1].Price {
			t.Errorf("asks not sorted lowest first: %v", asks)
		}
	}
	if len(bids) > 0 && len(asks) > 0 && bids[0].Price >= asks[0].Price {
		t.Errorf("book is crossed: best bid %d >= best ask %d", bids[0].Price, asks[0].Price)
	}
}

// A partially filled resting order keeps its place ahead of later orders at
// the same price when the next input arrives (spec invariant 9).
func TestPartialFillKeepsPriorityForNextInput(t *testing.T) {
	bk := NewBook()
	for _, o := range []Order{sell("S1", 4, 100), sell("S2", 6, 100), sell("S3", 2, 100), buy("B1", 7, 100)} {
		bk.Submit(o)
	}

	got := bk.Submit(buy("B2", 4, 100))

	wantExecs := []Execution{exec("B2", "S2", 3, 100), exec("B2", "S3", 1, 100)}
	if !slices.Equal(got.Executions, wantExecs) {
		t.Errorf("executions:\n got  %v\n want %v", got.Executions, wantExecs)
	}
	if want := []BookEntry{entry("S3", 1, 100)}; !slices.Equal(bk.Asks(sym), want) {
		t.Errorf("asks:\n got  %v\n want %v", bk.Asks(sym), want)
	}
}

// Orders only match against the same symbol (spec invariant 1).
func TestSymbolsAreIsolated(t *testing.T) {
	bk := NewBook()
	bk.Submit(Order{ID: "S1", Symbol: "MSFT", Side: Sell, Price: usd(100), Quantity: 5})

	got := bk.Submit(buy("B1", 5, 101))

	if len(got.Executions) != 0 {
		t.Errorf("AAPL buy traded against MSFT: %v", got.Executions)
	}
	if want := []BookEntry{entry("S1", 5, 100)}; !slices.Equal(bk.Asks("MSFT"), want) {
		t.Errorf("MSFT asks:\n got  %v\n want %v", bk.Asks("MSFT"), want)
	}
	if want := []BookEntry{entry("B1", 5, 101)}; !slices.Equal(bk.Bids(sym), want) {
		t.Errorf("AAPL bids:\n got  %v\n want %v", bk.Bids(sym), want)
	}
}

// The same starting book and inputs always produce the same executions and
// final book (spec invariant 14).
func TestDeterminism(t *testing.T) {
	for _, tc := range matchCases {
		t.Run(tc.name, func(t *testing.T) {
			bk1, res1 := run(t, tc)
			bk2, res2 := run(t, tc)
			if !reflect.DeepEqual(res1, res2) {
				t.Errorf("results differ between runs:\n %v\n %v", res1, res2)
			}
			if !slices.Equal(bk1.Bids(sym), bk2.Bids(sym)) || !slices.Equal(bk1.Asks(sym), bk2.Asks(sym)) {
				t.Errorf("final books differ between runs")
			}
		})
	}
}
