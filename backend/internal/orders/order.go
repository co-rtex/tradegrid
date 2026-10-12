// Package orders defines the TradeGrid Order domain model: the central object
// that flows through validation, risk, matching, execution, and persistence.
//
// The model is deliberately storage-free. It holds no database tags, no SQL,
// and no driver imports, so the same type can be used by the HTTP layer, the
// matching engine, and any future persistence layer without one of them
// dictating the others' shape. See docs/order-model.md for the prose
// specification.
//
// Side, Type, and Status are closed enumerations rather than strings. Their
// zero values are the explicit "unspecified" members, so an order that was
// never populated fails Validate instead of silently defaulting to a real
// side or a real status.
//
// Scope: this package enforces structural invariants only — required
// identifiers are present, enum values are members of their enumeration,
// quantities are positive, and remaining quantity never leaves the range
// [0, quantity]. Trading rules such as permitted price increments, lot sizes,
// maximum order size, and tradable symbols belong to the validation and risk
// modules (issues #3, #7, and #8).
package orders

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Side is the direction of an order. The zero value, SideUnspecified, is not
// a valid side.
type Side uint8

const (
	// SideUnspecified is the zero value and never describes a real order.
	SideUnspecified Side = iota
	// SideBuy is a request to purchase the symbol.
	SideBuy
	// SideSell is a request to sell the symbol.
	SideSell
)

// String returns the wire form of the side, such as "BUY".
func (s Side) String() string {
	switch s {
	case SideBuy:
		return "BUY"
	case SideSell:
		return "SELL"
	default:
		return "UNSPECIFIED"
	}
}

// Valid reports whether s is a member of the Side enumeration.
func (s Side) Valid() bool { return s == SideBuy || s == SideSell }

// Opposite returns the side an order must have to trade against s. It returns
// SideUnspecified when s is not a valid side.
func (s Side) Opposite() Side {
	switch s {
	case SideBuy:
		return SideSell
	case SideSell:
		return SideBuy
	default:
		return SideUnspecified
	}
}

// ParseSide converts a wire value such as "BUY" into a Side. Input is matched
// exactly; callers that accept user input should normalize case first.
func ParseSide(s string) (Side, error) {
	switch s {
	case "BUY":
		return SideBuy, nil
	case "SELL":
		return SideSell, nil
	default:
		return SideUnspecified, fmt.Errorf("%w: %q", ErrInvalidSide, s)
	}
}

// MarshalJSON encodes the side as its wire string.
func (s Side) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSide, uint8(s))
	}
	return json.Marshal(s.String())
}

// UnmarshalJSON decodes a wire string such as "BUY" into a Side.
func (s *Side) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseSide(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// Type is the execution instruction attached to an order. The zero value,
// TypeUnspecified, is not a valid type.
type Type uint8

const (
	// TypeUnspecified is the zero value and never describes a real order.
	TypeUnspecified Type = iota
	// TypeLimit trades only at Price or better.
	TypeLimit
	// TypeMarket trades at whatever price the book offers. The first matching
	// engine accepts limit orders only (see docs/matching-engine.md); the
	// member exists so the model does not have to change when market orders
	// are specified.
	TypeMarket
)

// String returns the wire form of the type, such as "LIMIT".
func (t Type) String() string {
	switch t {
	case TypeLimit:
		return "LIMIT"
	case TypeMarket:
		return "MARKET"
	default:
		return "UNSPECIFIED"
	}
}

// Valid reports whether t is a member of the Type enumeration.
func (t Type) Valid() bool { return t == TypeLimit || t == TypeMarket }

// ParseType converts a wire value such as "LIMIT" into a Type.
func ParseType(s string) (Type, error) {
	switch s {
	case "LIMIT":
		return TypeLimit, nil
	case "MARKET":
		return TypeMarket, nil
	default:
		return TypeUnspecified, fmt.Errorf("%w: %q", ErrInvalidType, s)
	}
}

// MarshalJSON encodes the type as its wire string.
func (t Type) MarshalJSON() ([]byte, error) {
	if !t.Valid() {
		return nil, fmt.Errorf("%w: %d", ErrInvalidType, uint8(t))
	}
	return json.Marshal(t.String())
}

// UnmarshalJSON decodes a wire string such as "LIMIT" into a Type.
func (t *Type) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseType(raw)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// Status is the lifecycle state of an order. The zero value,
// StatusUnspecified, is not a valid status.
//
// The permitted transitions are:
//
//	New             -> PartiallyFilled | Filled | Canceled | Rejected
//	PartiallyFilled -> PartiallyFilled | Filled | Canceled
//	Filled          -> (terminal)
//	Canceled        -> (terminal)
//	Rejected        -> (terminal)
type Status uint8

const (
	// StatusUnspecified is the zero value and never describes a real order.
	StatusUnspecified Status = iota
	// StatusNew is an accepted order that has not traded yet. It is either
	// resting on the book or about to be matched.
	StatusNew
	// StatusPartiallyFilled is an order with at least one fill and positive
	// remaining quantity.
	StatusPartiallyFilled
	// StatusFilled is an order whose entire quantity has traded.
	StatusFilled
	// StatusCanceled is an order withdrawn before its remaining quantity
	// traded. Any quantity already filled stays filled.
	StatusCanceled
	// StatusRejected is an order refused by validation or risk. A rejected
	// order never trades and never rests on the book.
	StatusRejected
)

// String returns the wire form of the status, such as "PARTIALLY_FILLED".
func (s Status) String() string {
	switch s {
	case StatusNew:
		return "NEW"
	case StatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case StatusFilled:
		return "FILLED"
	case StatusCanceled:
		return "CANCELED"
	case StatusRejected:
		return "REJECTED"
	default:
		return "UNSPECIFIED"
	}
}

// Valid reports whether s is a member of the Status enumeration.
func (s Status) Valid() bool {
	switch s {
	case StatusNew, StatusPartiallyFilled, StatusFilled, StatusCanceled, StatusRejected:
		return true
	default:
		return false
	}
}

// Terminal reports whether s is an end state that no transition leaves.
func (s Status) Terminal() bool {
	switch s {
	case StatusFilled, StatusCanceled, StatusRejected:
		return true
	default:
		return false
	}
}

// Open reports whether s is a working state whose remaining quantity can
// still trade.
func (s Status) Open() bool {
	return s == StatusNew || s == StatusPartiallyFilled
}

// ParseStatus converts a wire value such as "NEW" into a Status.
func ParseStatus(s string) (Status, error) {
	switch s {
	case "NEW":
		return StatusNew, nil
	case "PARTIALLY_FILLED":
		return StatusPartiallyFilled, nil
	case "FILLED":
		return StatusFilled, nil
	case "CANCELED":
		return StatusCanceled, nil
	case "REJECTED":
		return StatusRejected, nil
	default:
		return StatusUnspecified, fmt.Errorf("%w: %q", ErrInvalidStatus, s)
	}
}

// MarshalJSON encodes the status as its wire string.
func (s Status) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("%w: %d", ErrInvalidStatus, uint8(s))
	}
	return json.Marshal(s.String())
}

// UnmarshalJSON decodes a wire string such as "NEW" into a Status.
func (s *Status) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseStatus(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// Errors reported by this package. Callers should compare with errors.Is
// rather than matching on message text.
var (
	ErrInvalidSide     = errors.New("orders: invalid side")
	ErrInvalidType     = errors.New("orders: invalid type")
	ErrInvalidStatus   = errors.New("orders: invalid status")
	ErrMissingID       = errors.New("orders: missing order id")
	ErrMissingUserID   = errors.New("orders: missing user id")
	ErrMissingSymbol   = errors.New("orders: missing symbol")
	ErrInvalidQuantity = errors.New("orders: quantity must be positive")
	ErrInvalidPrice    = errors.New("orders: invalid price")
	ErrInvalidRemain   = errors.New("orders: remaining quantity out of range")
	ErrMissingTime     = errors.New("orders: missing timestamp")
	ErrNotOpen         = errors.New("orders: order is not open")
	ErrFillTooLarge    = errors.New("orders: fill exceeds remaining quantity")
	ErrAlreadyFilled   = errors.New("orders: order has already traded")
)

// Order is one customer instruction to buy or sell a symbol.
//
// Quantity is the quantity requested when the order was accepted and never
// changes. Remaining is the part of Quantity that has not traded yet; fills
// reduce it and Filled reports the difference. Price is a whole number of
// cents, matching the matching engine's representation, which keeps money out
// of binary floating point. Market orders carry a Price of zero.
//
// An Order is a plain value. Copying one copies the whole order, and the
// mutating methods take a pointer receiver so the caller decides what is
// shared.
type Order struct {
	// ID uniquely identifies the order across the exchange.
	ID string `json:"id"`
	// UserID identifies the account that submitted the order.
	UserID string `json:"userId"`
	// Symbol is the instrument being traded, such as "AAPL".
	Symbol string `json:"symbol"`
	// Side is BUY or SELL.
	Side Side `json:"side"`
	// Type is LIMIT or MARKET.
	Type Type `json:"type"`
	// Price is the limit price in whole cents, or 0 for a market order.
	Price int64 `json:"price"`
	// Quantity is the original quantity in whole units. It never changes.
	Quantity int64 `json:"quantity"`
	// Remaining is the quantity that has not traded yet, in [0, Quantity].
	Remaining int64 `json:"remaining"`
	// Status is the lifecycle state.
	Status Status `json:"status"`
	// CreatedAt is when the order was accepted.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the order last changed. It equals CreatedAt until the
	// first fill or cancellation.
	UpdatedAt time.Time `json:"updatedAt"`
}

// Params carries the fields a caller supplies when accepting a new order.
// Remaining, Status, and UpdatedAt are derived by New rather than supplied.
type Params struct {
	ID        string
	UserID    string
	Symbol    string
	Side      Side
	Type      Type
	Price     int64
	Quantity  int64
	CreatedAt time.Time
}

// New returns a newly accepted order with Remaining equal to Quantity and
// Status StatusNew. It reports an error when the parameters cannot describe a
// real order.
//
// CreatedAt is supplied by the caller rather than read from the clock so that
// tests and replays are deterministic.
func New(p Params) (Order, error) {
	o := Order{
		ID:        p.ID,
		UserID:    p.UserID,
		Symbol:    p.Symbol,
		Side:      p.Side,
		Type:      p.Type,
		Price:     p.Price,
		Quantity:  p.Quantity,
		Remaining: p.Quantity,
		Status:    StatusNew,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.CreatedAt,
	}
	if err := o.Validate(); err != nil {
		return Order{}, err
	}
	return o, nil
}

// Validate reports whether the order satisfies the model's structural
// invariants. It does not apply trading rules such as permitted price
// increments or maximum order size; those belong to the validation and risk
// modules.
func (o Order) Validate() error {
	switch {
	case o.ID == "":
		return ErrMissingID
	case o.UserID == "":
		return ErrMissingUserID
	case o.Symbol == "":
		return ErrMissingSymbol
	case !o.Side.Valid():
		return fmt.Errorf("%w: %d", ErrInvalidSide, uint8(o.Side))
	case !o.Type.Valid():
		return fmt.Errorf("%w: %d", ErrInvalidType, uint8(o.Type))
	case !o.Status.Valid():
		return fmt.Errorf("%w: %d", ErrInvalidStatus, uint8(o.Status))
	case o.Quantity <= 0:
		return fmt.Errorf("%w: %d", ErrInvalidQuantity, o.Quantity)
	case o.Remaining < 0 || o.Remaining > o.Quantity:
		return fmt.Errorf("%w: %d of %d", ErrInvalidRemain, o.Remaining, o.Quantity)
	case o.CreatedAt.IsZero() || o.UpdatedAt.IsZero():
		return ErrMissingTime
	}
	if err := o.validatePrice(); err != nil {
		return err
	}
	return o.validateStatusAgainstQuantities()
}

// validatePrice enforces the one price rule the model itself depends on: a
// limit order needs a positive limit, and a market order has no limit to
// carry. Tick size and price bands are the risk module's concern.
func (o Order) validatePrice() error {
	if o.Type == TypeLimit && o.Price <= 0 {
		return fmt.Errorf("%w: limit order needs a positive price, got %d", ErrInvalidPrice, o.Price)
	}
	if o.Type == TypeMarket && o.Price != 0 {
		return fmt.Errorf("%w: market order must have price 0, got %d", ErrInvalidPrice, o.Price)
	}
	return nil
}

// validateStatusAgainstQuantities rejects combinations the lifecycle can
// never produce, such as a FILLED order with quantity left to trade.
func (o Order) validateStatusAgainstQuantities() error {
	switch o.Status {
	case StatusNew, StatusRejected:
		if o.Remaining != o.Quantity {
			return fmt.Errorf("%w: status %s requires no fills, got %d of %d remaining",
				ErrInvalidStatus, o.Status, o.Remaining, o.Quantity)
		}
	case StatusPartiallyFilled:
		if o.Remaining <= 0 || o.Remaining >= o.Quantity {
			return fmt.Errorf("%w: status %s requires a partial fill, got %d of %d remaining",
				ErrInvalidStatus, o.Status, o.Remaining, o.Quantity)
		}
	case StatusFilled:
		if o.Remaining != 0 {
			return fmt.Errorf("%w: status %s requires no remaining quantity, got %d",
				ErrInvalidStatus, o.Status, o.Remaining)
		}
	}
	return nil
}

// Filled returns the quantity that has traded so far.
func (o Order) Filled() int64 { return o.Quantity - o.Remaining }

// Open reports whether the order can still trade.
func (o Order) Open() bool { return o.Status.Open() }

// Terminal reports whether the order has reached an end state.
func (o Order) Terminal() bool { return o.Status.Terminal() }

// Fill records that qty units of the order traded at the given time. It
// reduces Remaining and moves Status to PARTIALLY_FILLED or FILLED. The order
// is left untouched when the fill is not allowed.
//
// Fill records quantity only. The execution price belongs to the Execution
// record the matching engine produces, not to the order.
func (o *Order) Fill(qty int64, at time.Time) error {
	switch {
	case !o.Open():
		return fmt.Errorf("%w: status %s", ErrNotOpen, o.Status)
	case qty <= 0:
		return fmt.Errorf("%w: fill quantity %d", ErrInvalidQuantity, qty)
	case qty > o.Remaining:
		return fmt.Errorf("%w: %d requested, %d remaining", ErrFillTooLarge, qty, o.Remaining)
	case at.IsZero():
		return ErrMissingTime
	}
	o.Remaining -= qty
	if o.Remaining == 0 {
		o.Status = StatusFilled
	} else {
		o.Status = StatusPartiallyFilled
	}
	o.UpdatedAt = at
	return nil
}

// Cancel withdraws the order's remaining quantity. Quantity already filled is
// unaffected, so a partially filled order keeps its fills and stops working.
func (o *Order) Cancel(at time.Time) error {
	if !o.Open() {
		return fmt.Errorf("%w: status %s", ErrNotOpen, o.Status)
	}
	if at.IsZero() {
		return ErrMissingTime
	}
	o.Status = StatusCanceled
	o.UpdatedAt = at
	return nil
}

// Reject refuses an order that has not traded. Validation and risk call it
// instead of letting the order reach the book.
func (o *Order) Reject(at time.Time) error {
	switch {
	case o.Status != StatusNew:
		return fmt.Errorf("%w: status %s", ErrNotOpen, o.Status)
	case o.Remaining != o.Quantity:
		return fmt.Errorf("%w: %d of %d already traded", ErrAlreadyFilled, o.Filled(), o.Quantity)
	case at.IsZero():
		return ErrMissingTime
	}
	o.Status = StatusRejected
	o.UpdatedAt = at
	return nil
}

// String returns a compact human-readable summary for logs and test failures.
func (o Order) String() string {
	return fmt.Sprintf("%s %s %s %d/%d @ %d [%s]",
		o.ID, o.Side, o.Symbol, o.Remaining, o.Quantity, o.Price, o.Status)
}
