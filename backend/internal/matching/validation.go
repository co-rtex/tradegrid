package matching

import "errors"

// ErrInvalidQuantity indicates that an order's original quantity is not positive.
// Callers can use errors.Is(err, ErrInvalidQuantity) to identify this rejection.
var ErrInvalidQuantity = errors.New("order quantity must be greater than zero")

// ValidateQuantity checks the original quantity before an order is accepted.
// It does not validate other order fields or the remaining quantity after fills.
func (o Order) ValidateQuantity() error {
	// An order must request at least one share. Compare directly so even the
	// smallest int64 is rejected without negation or arithmetic overflow.
	if o.Quantity <= 0 {
		return ErrInvalidQuantity
	}
	// nil means the quantity is valid; other fields need their own validation.
	return nil
}
