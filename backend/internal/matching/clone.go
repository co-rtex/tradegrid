package matching

// Clone returns a deep copy of bk. Submitting to the copy never changes bk,
// so a caller can match on a candidate book and keep or discard it as a whole.
func (bk *Book) Clone() *Book {
	c := &Book{symbols: make(map[string]*symbolBook, len(bk.symbols))}
	for s, sb := range bk.symbols {
		c.symbols[s] = &symbolBook{bids: sb.bids.clone(), asks: sb.asks.clone()}
	}
	return c
}

func (b sideBook) clone() sideBook {
	orders := make([]*restingOrder, len(b.orders))
	for i, o := range b.orders {
		cp := *o
		orders[i] = &cp
	}
	return sideBook{orders: orders}
}
