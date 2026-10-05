package matching

// restingOrder is an order waiting on the book with its unfilled quantity.
type restingOrder struct {
	id        string
	price     int64
	remaining int64
}

// sideBook holds one side of one symbol's book, sorted best price first and
// FIFO within a price.
type sideBook struct {
	orders []*restingOrder
}

// insert places o behind every resting order with an equal or better price.
func (b *sideBook) insert(o *restingOrder, better func(a, b int64) bool) {
	i := len(b.orders)
	for j, r := range b.orders {
		if better(o.price, r.price) {
			i = j
			break
		}
	}
	b.orders = append(b.orders, nil)
	copy(b.orders[i+1:], b.orders[i:])
	b.orders[i] = o
}

func (b *sideBook) snapshot() []BookEntry {
	entries := make([]BookEntry, 0, len(b.orders))
	for _, o := range b.orders {
		entries = append(entries, BookEntry{ID: o.id, Price: o.price, Remaining: o.remaining})
	}
	return entries
}

type symbolBook struct {
	bids sideBook
	asks sideBook
}

// Book is the set of order books for all symbols. Orders must be submitted
// sequentially; submission order is acceptance order.
type Book struct {
	symbols map[string]*symbolBook
}

// NewBook returns an empty book.
func NewBook() *Book {
	return &Book{symbols: make(map[string]*symbolBook)}
}

func (bk *Book) symbol(s string) *symbolBook {
	sb, ok := bk.symbols[s]
	if !ok {
		sb = &symbolBook{}
		bk.symbols[s] = sb
	}
	return sb
}

func higher(a, b int64) bool { return a > b }
func lower(a, b int64) bool  { return a < b }

// Submit matches an accepted order against the opposite side of its symbol's
// book and rests any remainder. It assumes the order is already valid.
func (bk *Book) Submit(o Order) Result {
	sb := bk.symbol(o.Symbol)

	opposite, own := &sb.asks, &sb.bids
	ownBetter := higher
	marketable := func(resting int64) bool { return o.Price >= resting }
	if o.Side == Sell {
		opposite, own = &sb.bids, &sb.asks
		ownBetter = lower
		marketable = func(resting int64) bool { return o.Price <= resting }
	}

	res := Result{Remaining: o.Quantity}
	for res.Remaining > 0 && len(opposite.orders) > 0 {
		best := opposite.orders[0]
		if !marketable(best.price) {
			break
		}
		qty := min(res.Remaining, best.remaining)
		res.Executions = append(res.Executions, Execution{
			IncomingID: o.ID,
			RestingID:  best.id,
			Symbol:     o.Symbol,
			Price:      best.price,
			Quantity:   qty,
		})
		res.Remaining -= qty
		best.remaining -= qty
		if best.remaining == 0 {
			opposite.orders = opposite.orders[1:]
		}
	}

	if res.Remaining > 0 {
		own.insert(&restingOrder{id: o.ID, price: o.Price, remaining: res.Remaining}, ownBetter)
	}
	return res
}

// Bids returns the symbol's resting buy orders, highest price first and FIFO
// within a price.
func (bk *Book) Bids(symbol string) []BookEntry {
	sb, ok := bk.symbols[symbol]
	if !ok {
		return []BookEntry{}
	}
	return sb.bids.snapshot()
}

// Asks returns the symbol's resting sell orders, lowest price first and FIFO
// within a price.
func (bk *Book) Asks(symbol string) []BookEntry {
	sb, ok := bk.symbols[symbol]
	if !ok {
		return []BookEntry{}
	}
	return sb.asks.snapshot()
}
