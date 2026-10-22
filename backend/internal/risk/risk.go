// Package risk implements the cash and share reservation and settlement
// arithmetic specified in docs/api.md ("Reservations and Settlement").
//
// The package is pure: it holds no state, reads no clock, and never mutates
// the values it is given. Preflight and Settle return candidate account values
// that the exchange service (#35) publishes only after the rest of a
// submission succeeds, so a rejected or failed submission leaves the published
// ledger untouched.
//
// Every amount is an exact integer: cash in US cents, positions in whole
// shares. All arithmetic is checked, and any total or reservation that would
// leave [0, MaxAggregate] is rejected rather than wrapped or clamped.
//
// Scope: price and quantity bounds, symbol catalogs, and account lookup belong
// to validation (#31) and the exchange (#35). Cancellation releases, deposits,
// cost basis, and P&L are later milestones.
package risk

import (
	"errors"
	"fmt"
	"maps"
	"math"

	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

// MaxAggregate is the largest value any account total, reservation, or
// computed notional may take: 2^53 - 1, the largest integer that is exact in
// both Go int64 and JavaScript Number.
const MaxAggregate int64 = 1<<53 - 1

// Errors reported by this package. Callers should compare with errors.Is.
// ErrInsufficientCash, ErrInsufficientShares, and ErrLimitExceeded correspond
// to the INSUFFICIENT_CASH, INSUFFICIENT_SHARES, and LIMIT_EXCEEDED API codes.
// ErrInvalidInput, ErrInconsistent, and ErrUnknownAccount describe caller bugs
// rather than client mistakes and map to INTERNAL.
var (
	ErrInvalidInput       = errors.New("risk: invalid input")
	ErrInsufficientCash   = errors.New("risk: insufficient available cash")
	ErrInsufficientShares = errors.New("risk: insufficient available shares")
	ErrLimitExceeded      = errors.New("risk: aggregate limit exceeded")
	ErrInconsistent       = errors.New("risk: settlement inconsistent with reservations")
	ErrUnknownAccount     = errors.New("risk: unknown account")
)

// Balance is one asset of one account: cash in cents or a position in shares.
// Reserved is the part of Total committed to open orders.
type Balance struct {
	Total    int64
	Reserved int64
}

// Available returns the part of Total that new orders may commit.
func (b Balance) Available() int64 { return b.Total - b.Reserved }

func (b Balance) valid() bool {
	return b.Total >= 0 && b.Total <= MaxAggregate && b.Reserved >= 0 && b.Reserved <= b.Total
}

// Account is the ledger of one account. Positions is keyed by symbol; a
// missing symbol is a zero balance.
type Account struct {
	Cash      Balance
	Positions map[string]Balance
}

// Clone returns a deep copy of a, so the copy's Positions can be changed
// without affecting a.
func (a Account) Clone() Account {
	positions := maps.Clone(a.Positions)
	if positions == nil {
		positions = map[string]Balance{}
	}
	return Account{Cash: a.Cash, Positions: positions}
}

// Validate reports whether every balance in a lies in [0, MaxAggregate] with
// a reservation no larger than its total.
func (a Account) Validate() error {
	if !a.Cash.valid() {
		return fmt.Errorf("%w: cash %+v", ErrInvalidInput, a.Cash)
	}
	for symbol, b := range a.Positions {
		if symbol == "" || !b.valid() {
			return fmt.Errorf("%w: position %q %+v", ErrInvalidInput, symbol, b)
		}
	}
	return nil
}

// Preflight checks that a can afford a new limit order and returns a copy of
// a with the order's reservation added. A BUY reserves price × quantity cash;
// a SELL reserves quantity shares of symbol. There is no short selling, so a
// SELL needs shares already held and not reserved by other orders.
//
// a is never modified. On error the returned Account is the zero value.
func Preflight(a Account, side orders.Side, symbol string, price, quantity int64) (Account, error) {
	switch {
	case !side.Valid():
		return Account{}, fmt.Errorf("%w: side %s", ErrInvalidInput, side)
	case symbol == "":
		return Account{}, fmt.Errorf("%w: missing symbol", ErrInvalidInput)
	case price <= 0 || quantity <= 0:
		return Account{}, fmt.Errorf("%w: price %d, quantity %d", ErrInvalidInput, price, quantity)
	}
	if err := a.Validate(); err != nil {
		return Account{}, err
	}

	out := a.Clone()
	if side == orders.SideBuy {
		notional, err := mul(price, quantity)
		if err != nil {
			return Account{}, err
		}
		if available := out.Cash.Available(); available < notional {
			return Account{}, fmt.Errorf("%w: available cash %d is less than %d", ErrInsufficientCash, available, notional)
		}
		// Available >= notional implies Reserved+notional <= Total <= MaxAggregate.
		out.Cash.Reserved += notional
		return out, nil
	}

	held := out.Positions[symbol]
	if available := held.Available(); available < quantity {
		return Account{}, fmt.Errorf("%w: available %s shares %d is less than %d", ErrInsufficientShares, symbol, available, quantity)
	}
	held.Reserved += quantity
	out.Positions[symbol] = held
	return out, nil
}

// Fill is one execution to settle, in the terms settlement needs.
type Fill struct {
	BuyerID  string
	SellerID string
	Symbol   string
	// BuyLimit is the buy order's limit price in cents. The buyer's cash
	// reservation for the filled quantity was made at this price.
	BuyLimit int64
	// Price is the execution price in cents: the resting order's price, never
	// above BuyLimit.
	Price    int64
	Quantity int64
}

// delta is the net change to one account across a batch of fills. Its
// Balance fields hold changes, not values.
type delta struct {
	cash   Balance
	shares map[string]*Balance
}

func (d *delta) position(symbol string) *Balance {
	b, ok := d.shares[symbol]
	if !ok {
		b = &Balance{}
		d.shares[symbol] = b
	}
	return b
}

// Settle applies a batch of fills, typically every fill from one submission,
// and returns a new account map. For a fill of q shares at price p against a
// buy limit L:
//
//	buyer:  cash total -p×q, cash reserved -L×q, shares total +q
//	seller: cash total +p×q, shares total -q, shares reserved -q
//
// Deltas are combined per account before they are applied, so a same-account
// fill nets cash and share totals to zero while still releasing both
// reservations, and a batch's result does not depend on intermediate states.
//
// accounts is never modified; the result holds a copy of every account. On
// error the result is nil. Settlement that would release more than was
// reserved or leave a negative balance reports ErrInconsistent; a total
// pushed past MaxAggregate reports ErrLimitExceeded.
func Settle(accounts map[string]Account, fills []Fill) (map[string]Account, error) {
	deltas := make(map[string]*delta)
	get := func(id string) *delta {
		d, ok := deltas[id]
		if !ok {
			d = &delta{shares: map[string]*Balance{}}
			deltas[id] = d
		}
		return d
	}

	for i, f := range fills {
		if err := checkFill(accounts, f); err != nil {
			return nil, fmt.Errorf("fill %d: %w", i, err)
		}
		payment, err := mul(f.Price, f.Quantity)
		if err != nil {
			return nil, fmt.Errorf("fill %d: %w", i, err)
		}
		released, err := mul(f.BuyLimit, f.Quantity)
		if err != nil {
			return nil, fmt.Errorf("fill %d: %w", i, err)
		}

		bump := func(v *int64, n int64) {
			if err == nil {
				*v, err = add(*v, n)
			}
		}
		buyer, seller := get(f.BuyerID), get(f.SellerID)
		bump(&buyer.cash.Total, -payment)
		bump(&buyer.cash.Reserved, -released)
		bump(&buyer.position(f.Symbol).Total, f.Quantity)
		bump(&seller.cash.Total, payment)
		bump(&seller.position(f.Symbol).Total, -f.Quantity)
		bump(&seller.position(f.Symbol).Reserved, -f.Quantity)
		if err != nil {
			return nil, fmt.Errorf("fill %d: %w", i, err)
		}
	}

	out := make(map[string]Account, len(accounts))
	for id, a := range accounts {
		out[id] = a.Clone()
	}
	for id, d := range deltas {
		a, err := apply(out[id], d)
		if err != nil {
			return nil, fmt.Errorf("account %q: %w", id, err)
		}
		out[id] = a
	}
	return out, nil
}

func checkFill(accounts map[string]Account, f Fill) error {
	switch {
	case f.BuyerID == "" || f.SellerID == "" || f.Symbol == "":
		return fmt.Errorf("%w: missing buyer, seller, or symbol", ErrInvalidInput)
	case f.Quantity <= 0 || f.Price <= 0:
		return fmt.Errorf("%w: price %d, quantity %d", ErrInvalidInput, f.Price, f.Quantity)
	case f.Price > f.BuyLimit:
		return fmt.Errorf("%w: price %d above buy limit %d", ErrInvalidInput, f.Price, f.BuyLimit)
	}
	for _, id := range []string{f.BuyerID, f.SellerID} {
		a, ok := accounts[id]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownAccount, id)
		}
		if err := a.Validate(); err != nil {
			return fmt.Errorf("account %q: %w", id, err)
		}
	}
	return nil
}

// apply adds d to a, whose Positions map the caller owns, and checks the
// result.
func apply(a Account, d *delta) (Account, error) {
	cash, err := applyBalance(a.Cash, d.cash)
	if err != nil {
		return Account{}, fmt.Errorf("cash: %w", err)
	}
	a.Cash = cash
	for symbol, sd := range d.shares {
		held, err := applyBalance(a.Positions[symbol], *sd)
		if err != nil {
			return Account{}, fmt.Errorf("%s shares: %w", symbol, err)
		}
		a.Positions[symbol] = held
	}
	return a, nil
}

func applyBalance(b, d Balance) (Balance, error) {
	total, err := add(b.Total, d.Total)
	if err != nil {
		return Balance{}, err
	}
	reserved, err := add(b.Reserved, d.Reserved)
	if err != nil {
		return Balance{}, err
	}
	switch {
	case total > MaxAggregate:
		return Balance{}, fmt.Errorf("%w: total %d", ErrLimitExceeded, total)
	case total < 0 || reserved < 0 || reserved > total:
		return Balance{}, fmt.Errorf("%w: total %d, reserved %d", ErrInconsistent, total, reserved)
	}
	return Balance{Total: total, Reserved: reserved}, nil
}

// add returns a+b, or ErrLimitExceeded when the sum overflows int64.
func add(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, fmt.Errorf("%w: %d + %d overflows", ErrLimitExceeded, a, b)
	}
	return a + b, nil
}

// mul returns a×b for positive a and b, or ErrLimitExceeded when the product
// exceeds MaxAggregate.
func mul(a, b int64) (int64, error) {
	if a > MaxAggregate/b {
		return 0, fmt.Errorf("%w: %d × %d exceeds %d", ErrLimitExceeded, a, b, MaxAggregate)
	}
	return a * b, nil
}
