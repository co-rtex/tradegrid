// Package accounts holds the demo accounts' cash and share ledger and joins
// it to order acceptance and settlement, as specified in docs/api.md
// ("Demo Accounts and Symbols" and "Reservations and Settlement").
//
// The arithmetic lives in package risk; this package owns the account set,
// looks accounts up by user ID, turns orders and execution records into risk
// inputs, and exports copied snapshots.
//
// A Ledger is a plain value with no lock of its own. The exchange service
// owns the published ledger under its serialized boundary and changes it by
// copy-on-write: Clone the published ledger, apply Reserve and Settle to the
// clone together with the rest of the submission, and publish the clone only
// when everything succeeded. Reserve and Settle change their receiver only
// when they succeed, and a published ledger that is never changed again may
// be read concurrently.
package accounts

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
	"github.com/co-rtex/tradegrid/backend/internal/risk"
)

// Errors reported by this package. Callers should compare with errors.Is.
// Errors from package risk, such as risk.ErrInsufficientCash, are wrapped and
// remain visible to errors.Is.
//
// ErrUnknownUser corresponds to the UNKNOWN_USER API code when it comes from
// Reserve. ErrInvalidOrder and ErrInconsistent describe caller bugs and map
// to INTERNAL.
var (
	ErrUnknownUser  = errors.New("accounts: unknown user")
	ErrInvalidOrder = errors.New("accounts: order cannot be reserved")
	ErrInconsistent = errors.New("accounts: ledger inconsistent with orders or executions")
)

// Ledger is the set of demo accounts with their cash and share balances.
// Create one with Seed; the zero value has no accounts.
type Ledger struct {
	// userIDs is sorted and, like names, never changes after Seed, so clones
	// share both.
	userIDs  []string
	names    map[string]string
	balances map[string]risk.Account
}

// Clone returns an independent copy of l. Changes to the copy never affect l.
func (l *Ledger) Clone() *Ledger {
	balances := make(map[string]risk.Account, len(l.balances))
	for id, a := range l.balances {
		balances[id] = a.Clone()
	}
	return &Ledger{userIDs: l.userIDs, names: l.names, balances: balances}
}

// Has reports whether userID is a demo account.
func (l *Ledger) Has(userID string) bool {
	_, ok := l.balances[userID]
	return ok
}

// Reserve runs risk.Preflight for a newly accepted order and records its
// reservation: price × quantity cash for a BUY, quantity shares for a SELL.
//
// o must be a valid NEW limit order with no fills. Reserve reports
// ErrUnknownUser for an account it does not hold, and wraps
// risk.ErrInsufficientCash, risk.ErrInsufficientShares, or
// risk.ErrLimitExceeded when the account cannot afford the order. On error l
// is unchanged.
func (l *Ledger) Reserve(o orders.Order) error {
	if err := o.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidOrder, err)
	}
	switch {
	case o.Type != orders.TypeLimit:
		return fmt.Errorf("%w: type %s", ErrInvalidOrder, o.Type)
	case o.Status != orders.StatusNew:
		return fmt.Errorf("%w: status %s", ErrInvalidOrder, o.Status)
	}
	account, ok := l.balances[o.UserID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownUser, o.UserID)
	}
	reserved, err := risk.Preflight(account, o.Side, o.Symbol, o.Price, o.Quantity)
	if err != nil {
		return fmt.Errorf("order %s: %w", o.ID, err)
	}
	l.balances[o.UserID] = reserved
	return nil
}

// Settle applies every execution record from one submission with
// risk.Settle: shares move from seller to buyer, cash moves from buyer to
// seller at the execution price, the buyer's cash reservation is released at
// the buy order's limit, and the seller's share reservation is released.
// Same-account records net to zero totals and release both reservations.
//
// canonical holds the orders by ID; it must contain both orders of every
// record. Settle reports ErrInconsistent when a record disagrees with its
// orders, and wraps risk errors, including risk.ErrLimitExceeded when a total
// would pass its bound. On error l is unchanged.
func (l *Ledger) Settle(records []executions.Record, canonical map[string]orders.Order) error {
	if len(records) == 0 {
		return nil
	}
	fills := make([]risk.Fill, 0, len(records))
	for _, r := range records {
		buy, err := counterparty(canonical, r.BuyOrderID, orders.SideBuy, r.BuyerUserID, r)
		if err != nil {
			return err
		}
		if _, err := counterparty(canonical, r.SellOrderID, orders.SideSell, r.SellerUserID, r); err != nil {
			return err
		}
		for _, id := range []string{r.BuyerUserID, r.SellerUserID} {
			if !l.Has(id) {
				return fmt.Errorf("%w: execution %s: %w: %q", ErrInconsistent, r.ID, ErrUnknownUser, id)
			}
		}
		fills = append(fills, risk.Fill{
			BuyerID:  r.BuyerUserID,
			SellerID: r.SellerUserID,
			Symbol:   r.Symbol,
			BuyLimit: buy.Price,
			Price:    r.Price,
			Quantity: r.Quantity,
		})
	}
	settled, err := risk.Settle(l.balances, fills)
	if err != nil {
		return err
	}
	l.balances = settled
	return nil
}

// counterparty returns the canonical order with the given ID after checking
// that it is the side, account, and symbol the record says it is.
func counterparty(canonical map[string]orders.Order, id string, side orders.Side, userID string, r executions.Record) (orders.Order, error) {
	o, ok := canonical[id]
	switch {
	case !ok:
		return orders.Order{}, fmt.Errorf("%w: execution %s: missing %s order %q", ErrInconsistent, r.ID, side, id)
	case o.Side != side || o.UserID != userID || o.Symbol != r.Symbol || o.Type != orders.TypeLimit:
		return orders.Order{}, fmt.Errorf("%w: execution %s: %s order %s does not match the record", ErrInconsistent, r.ID, side, o)
	}
	return o, nil
}

// Reconcile checks that every reservation in l equals what the open orders
// still need: for each account, reserved cash is the sum of price ×
// remaining over its open BUY orders, and reserved shares of a symbol are the
// sum of remaining over its open SELL orders. Orders that are not open are
// ignored. It reports ErrInconsistent on any difference.
//
// Reconcile is meant for tests and debug assertions; it is linear in the
// number of orders.
func (l *Ledger) Reconcile(all []orders.Order) error {
	type need struct {
		cash   int64
		shares map[string]int64
	}
	needs := make(map[string]*need, len(l.balances))
	for id := range l.balances {
		needs[id] = &need{shares: map[string]int64{}}
	}
	for _, o := range all {
		if !o.Open() {
			continue
		}
		n, ok := needs[o.UserID]
		if !ok {
			return fmt.Errorf("%w: open order %s: %w: %q", ErrInconsistent, o.ID, ErrUnknownUser, o.UserID)
		}
		var sum int64
		switch o.Side {
		case orders.SideBuy:
			sum, ok = mulAdd(n.cash, o.Price, o.Remaining)
			n.cash = sum
		case orders.SideSell:
			sum, ok = mulAdd(n.shares[o.Symbol], 1, o.Remaining)
			n.shares[o.Symbol] = sum
		default:
			ok = false
		}
		if !ok {
			return fmt.Errorf("%w: open order %s cannot be reserved", ErrInconsistent, o)
		}
	}

	for _, id := range l.userIDs {
		a, n := l.balances[id], needs[id]
		if a.Cash.Reserved != n.cash {
			return fmt.Errorf("%w: %s reserved cash %d, open orders need %d", ErrInconsistent, id, a.Cash.Reserved, n.cash)
		}
		symbols := slices.Sorted(maps.Keys(a.Positions))
		for symbol := range n.shares {
			if _, held := a.Positions[symbol]; !held {
				symbols = append(symbols, symbol)
			}
		}
		for _, symbol := range symbols {
			if got, want := a.Positions[symbol].Reserved, n.shares[symbol]; got != want {
				return fmt.Errorf("%w: %s reserved %s shares %d, open orders need %d", ErrInconsistent, id, symbol, got, want)
			}
		}
	}
	return nil
}

// mulAdd returns sum + a×b for nonnegative sum, a, and b, and false when the
// result would exceed risk.MaxAggregate or an operand is out of range.
func mulAdd(sum, a, b int64) (int64, bool) {
	if sum < 0 || a < 0 || b < 0 || a != 0 && b > math.MaxInt64/a {
		return 0, false
	}
	p := a * b
	if p > risk.MaxAggregate || sum > risk.MaxAggregate-p {
		return 0, false
	}
	return sum + p, true
}

// Balance is one asset of one account in a snapshot: cash in cents or a
// position in shares. Available is Total minus Reserved.
type Balance struct {
	Total     int64 `json:"total"`
	Reserved  int64 `json:"reserved"`
	Available int64 `json:"available"`
}

// Position is one symbol's balance in a snapshot.
type Position struct {
	Symbol string `json:"symbol"`
	Balance
}

// Snapshot is a copied view of one account. Its JSON form is the Account
// resource in docs/api.md.
type Snapshot struct {
	UserID      string     `json:"userId"`
	DisplayName string     `json:"displayName"`
	Cash        Balance    `json:"cash"`
	Positions   []Position `json:"positions"`
}

func balance(b risk.Balance) Balance {
	return Balance{Total: b.Total, Reserved: b.Reserved, Available: b.Available()}
}

// Snapshot returns a copy of every account, sorted by user ID, with
// positions sorted by symbol and including zero holdings. The result shares
// no memory with l.
func (l *Ledger) Snapshot() []Snapshot {
	out := make([]Snapshot, 0, len(l.userIDs))
	for _, id := range l.userIDs {
		a := l.balances[id]
		positions := make([]Position, 0, len(a.Positions))
		for _, symbol := range slices.Sorted(maps.Keys(a.Positions)) {
			positions = append(positions, Position{Symbol: symbol, Balance: balance(a.Positions[symbol])})
		}
		out = append(out, Snapshot{
			UserID:      id,
			DisplayName: l.names[id],
			Cash:        balance(a.Cash),
			Positions:   positions,
		})
	}
	return out
}
