package matching

import "errors"

// ErrInvalidQuantity indicates that an order's original quantity is not positive.
var ErrInvalidQuantity = errors.New("order quantity must be greater than zero")

// ValidateQuantity checks the original quantity before an order is accepted.
// It does not validate other order fields or the remaining quantity after fills.
func (o Order) ValidateQuantity() error {
	if o.Quantity <= 0 {
		return ErrInvalidQuantity
	}
	return nil
}
