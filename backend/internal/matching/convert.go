package matching

import (
	"errors"
	"fmt"

	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

// ErrUnsupportedOrder indicates a valid domain order the engine cannot accept,
// such as a market order. Callers can use errors.Is to identify it.
var ErrUnsupportedOrder = errors.New("matching: unsupported order")

// FromOrder converts a newly accepted domain order into the engine's Order.
//
// The order must pass orders.Order.Validate and be a NEW limit order with no
// fills. Market orders are rejected because the matching spec covers limit
// orders only.
func FromOrder(o orders.Order) (Order, error) {
	if err := o.Validate(); err != nil {
		return Order{}, err
	}
	switch {
	case o.Type != orders.TypeLimit:
		return Order{}, fmt.Errorf("%w: type %s", ErrUnsupportedOrder, o.Type)
	case o.Status != orders.StatusNew:
		return Order{}, fmt.Errorf("%w: status %s", ErrUnsupportedOrder, o.Status)
	}
	return Order{
		ID:       o.ID,
		Symbol:   o.Symbol,
		Side:     o.Side,
		Price:    o.Price,
		Quantity: o.Quantity,
	}, nil
}
