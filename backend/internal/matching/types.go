// Package matching implements the TradeGrid limit-order matching engine
// specified in docs/matching-engine.md.
package matching

import "github.com/co-rtex/tradegrid/backend/internal/orders"

// Side is the direction of an order. It is the shared orders.Side, so the
// engine and the Order domain model cannot disagree about sides.
type Side = orders.Side

const (
	Buy  = orders.SideBuy
	Sell = orders.SideSell
)

// Order is the engine's view of an accepted limit order: only the fields the
// matching spec needs. Build one from the shared domain model with FromOrder.
//
// Prices are integer cents and quantities are whole units, as specified in
// docs/order-model.md.
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
	// Err is the validation error for a rejected order. Rejected orders produce
	// no executions or remaining quantity and do not change the book.
	// Check Err before interpreting Remaining: zero also occurs on a full fill.
	Err error
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
