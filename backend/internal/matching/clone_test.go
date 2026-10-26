package matching

import (
	"slices"
	"testing"
)

func TestCloneIsIndependent(t *testing.T) {
	bk := NewBook()
	bk.Submit(sell("S1", 4, 100))
	bk.Submit(sell("S2", 6, 101))
	bk.Submit(buy("B1", 3, 99))
	wantAsks, wantBids := bk.Asks(sym), bk.Bids(sym)

	c := bk.Clone()
	if !slices.Equal(c.Asks(sym), wantAsks) || !slices.Equal(c.Bids(sym), wantBids) {
		t.Fatalf("clone differs from original:\n asks %v\n bids %v", c.Asks(sym), c.Bids(sym))
	}

	// Partly fill S1's level and add a new symbol on the clone only.
	c.Submit(buy("B2", 5, 101))
	c.Submit(Order{ID: "M1", Symbol: "MSFT", Side: Sell, Price: usd(50), Quantity: 1})

	if !slices.Equal(bk.Asks(sym), wantAsks) || !slices.Equal(bk.Bids(sym), wantBids) {
		t.Errorf("original changed:\n asks %v\n bids %v", bk.Asks(sym), bk.Bids(sym))
	}
	if got := bk.Asks("MSFT"); len(got) != 0 {
		t.Errorf("original gained MSFT asks: %v", got)
	}
	if want := []BookEntry{entry("S2", 5, 101)}; !slices.Equal(c.Asks(sym), want) {
		t.Errorf("clone asks:\n got  %v\n want %v", c.Asks(sym), want)
	}
}
