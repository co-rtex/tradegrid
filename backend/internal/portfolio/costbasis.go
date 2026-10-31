// Package portfolio implements the average-cost accounting specified in
// docs/portfolio.md ("Average-Cost Accounting").
//
// The package is pure: it holds no state and never mutates the values it is
// given. ApplyBuy and ApplySell return a new Lot for one fill, which the
// exchange (#68) applies on its candidate ledger together with the rest of a
// submission.
//
// Every amount is an exact integer: money in US cents, quantities in whole
// shares. All arithmetic is checked, and the one division (allocating basis
// to sold shares) uses a 128-bit intermediate so it cannot overflow.
//
// Scope: valuation, reference prices, and totals belong to #65 and #63;
// same-account executions are skipped by the caller, since they change no
// basis or P&L.
package portfolio

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
)

// MaxAmount is the largest value any quantity, cost basis, or cash amount may
// take, and the largest magnitude of realized P&L: 2^53 - 1, the largest
// integer that is exact in both Go int64 and JavaScript Number. It equals
// risk.MaxAggregate.
const MaxAmount int64 = 1<<53 - 1

// Errors reported by this package. Callers should compare with errors.Is.
// During settlement every one of them is a bug and maps to INTERNAL.
var (
	ErrInvalidLot         = errors.New("portfolio: invalid lot")
	ErrInvalidQuantity    = errors.New("portfolio: quantity must be positive")
	ErrInvalidPrice       = errors.New("portfolio: price must be positive")
	ErrInsufficientShares = errors.New("portfolio: selling more shares than held")
	ErrOverflow           = errors.New("portfolio: amount out of range")
)

// Lot is one account's holding in one symbol: the shares held, what they
// cost under average cost, and the P&L realized by selling since the server
// started. The zero value is an empty position.
type Lot struct {
	Quantity    int64 `json:"quantity"`
	CostBasis   int64 `json:"costBasis"`
	RealizedPnL int64 `json:"realizedPnl"`
}

// Validate reports whether l can be produced by this package: quantity and
// basis within [0, MaxAmount], no basis without shares, at least one cent of
// basis per share held, and realized P&L within ±MaxAmount.
func (l Lot) Validate() error {
	switch {
	case l.Quantity < 0 || l.Quantity > MaxAmount:
		return fmt.Errorf("%w: quantity %d", ErrInvalidLot, l.Quantity)
	case l.CostBasis < 0 || l.CostBasis > MaxAmount:
		return fmt.Errorf("%w: cost basis %d", ErrInvalidLot, l.CostBasis)
	case l.Quantity == 0 && l.CostBasis != 0:
		return fmt.Errorf("%w: cost basis %d with no shares", ErrInvalidLot, l.CostBasis)
	case l.CostBasis < l.Quantity:
		// Every share cost at least one cent, and rounding down never takes
		// a remaining share below one cent.
		return fmt.Errorf("%w: cost basis %d for %d shares", ErrInvalidLot, l.CostBasis, l.Quantity)
	case l.RealizedPnL < -MaxAmount || l.RealizedPnL > MaxAmount:
		return fmt.Errorf("%w: realized P&L %d", ErrInvalidLot, l.RealizedPnL)
	}
	return nil
}

// ApplyBuy returns l after buying quantity shares at execution price price:
// the basis grows by price × quantity. price must be the execution price, not
// the buy order's limit. On error it returns the zero Lot.
func ApplyBuy(l Lot, quantity, price int64) (Lot, error) {
	if err := checkFill(l, quantity, price); err != nil {
		return Lot{}, err
	}
	cost, err := mul(price, quantity)
	if err != nil {
		return Lot{}, err
	}
	q, err := add(l.Quantity, quantity)
	if err != nil {
		return Lot{}, err
	}
	basis, err := add(l.CostBasis, cost)
	if err != nil {
		return Lot{}, err
	}
	return Lot{Quantity: q, CostBasis: basis, RealizedPnL: l.RealizedPnL}, nil
}

// ApplySell returns l after selling quantity shares at execution price price.
// It removes Allocate(l.CostBasis, l.Quantity, quantity) from the basis and
// realizes price × quantity minus that amount. On error it returns the zero
// Lot.
func ApplySell(l Lot, quantity, price int64) (Lot, error) {
	if err := checkFill(l, quantity, price); err != nil {
		return Lot{}, err
	}
	if quantity > l.Quantity {
		return Lot{}, fmt.Errorf("%w: selling %d of %d", ErrInsufficientShares, quantity, l.Quantity)
	}
	proceeds, err := mul(price, quantity)
	if err != nil {
		return Lot{}, err
	}
	allocated := Allocate(l.CostBasis, l.Quantity, quantity)
	realized, err := add(l.RealizedPnL, proceeds-allocated)
	if err != nil {
		return Lot{}, err
	}
	return Lot{Quantity: l.Quantity - quantity, CostBasis: l.CostBasis - allocated, RealizedPnL: realized}, nil
}

// Allocate returns the part of basis that sold of held shares carry under
// average cost: all of it when sold == held, otherwise
// floor(basis × sold / held). It requires 0 <= basis, 0 < sold <= held, which
// ApplySell checks; the product is computed in 128 bits.
func Allocate(basis, held, sold int64) int64 {
	if sold == held {
		return basis
	}
	hi, lo := bits.Mul64(uint64(basis), uint64(sold))
	// sold < held, so the quotient is below basis and hi < held: Div64
	// cannot overflow.
	quo, _ := bits.Div64(hi, lo, uint64(held))
	return int64(quo)
}

// checkFill validates the lot and the fill's quantity and price.
func checkFill(l Lot, quantity, price int64) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if quantity <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidQuantity, quantity)
	}
	if price <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidPrice, price)
	}
	return nil
}

// add returns a + b, or ErrOverflow if the sum leaves [-MaxAmount, MaxAmount].
// Callers pass values already within that range, so a + b cannot wrap.
func add(a, b int64) (int64, error) {
	s := a + b
	if s < -MaxAmount || s > MaxAmount {
		return 0, fmt.Errorf("%w: %d + %d", ErrOverflow, a, b)
	}
	return s, nil
}

// mul returns a × b for positive a and b, or ErrOverflow if the product
// exceeds MaxAmount.
func mul(a, b int64) (int64, error) {
	if a > math.MaxInt64/b || a*b > MaxAmount {
		return 0, fmt.Errorf("%w: %d × %d", ErrOverflow, a, b)
	}
	return a * b, nil
}
