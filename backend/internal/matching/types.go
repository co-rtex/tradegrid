// Package matching implements the TradeGrid limit-order matching engine
// specified in docs/matching-engine.md.
package matching

// Side is the direction of an order.
type Side int

const (
	Buy Side = iota + 1
	Sell
)

func (s Side) String() string {
	switch s {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

// Order is an accepted limit order handed to the matching engine.
//
// TODO(#6): Replace or align with the shared Order domain model once issue #6
// is merged. Only the fields the matching spec needs are defined here.
//
// TODO: Numeric representation is out of scope for the matching spec. Prices
// are integer cents and quantities are whole shares until a decision is made.
type Order struct {
	ID       string
	Symbol   string
	Side     Side
	Price    int64 // limit price in cents
	Quantity int64 // original quantity
}

// Execution is one match between the incoming order and one resting order.
// It always executes at the resting order's price.
type Execution struct {
	IncomingID string
	RestingID  string
	Symbol     string
	Price      int64
	Quantity   int64
}

// Result describes what happened when one incoming order was processed.
type Result struct {
	// Executions in the order they occurred.
	Executions []Execution
	// Remaining is the incoming order's unfilled quantity. When positive, that
	// quantity now rests on the book.
	Remaining int64
}

// BookEntry is one resting order as seen in a book snapshot.
type BookEntry struct {
	ID        string
	Price     int64
	Remaining int64
}
