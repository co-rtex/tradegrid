// Package executions turns matching-engine fills into canonical execution
// records that settlement, history, and the UI can read without knowing which
// side was incoming and which was resting.
//
// The mapper is pure: it reads no clock, generates no IDs, touches no
// database, and never changes the matcher's output or the orders it is given.
// Callers inject IDs, sequences, and timestamps through Metadata.
package executions

import (
	"errors"
	"fmt"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/matching"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

// Record is one trade between a buyer and a seller.
type Record struct {
	// ID uniquely identifies the execution.
	ID string `json:"id"`
	// Sequence orders executions across the exchange. It strictly increases in
	// matcher order.
	Sequence int64 `json:"sequence"`
	// ExecutedAt is when the trade happened, in UTC.
	ExecutedAt time.Time `json:"executedAt"`
	// Symbol is the instrument traded.
	Symbol string `json:"symbol"`
	// Price is the execution price in whole cents: the resting order's price.
	Price int64 `json:"price"`
	// Quantity is the number of whole units traded.
	Quantity int64 `json:"quantity"`
	// BuyOrderID and SellOrderID identify the two orders that traded.
	BuyOrderID  string `json:"buyOrderId"`
	SellOrderID string `json:"sellOrderId"`
	// BuyerUserID and SellerUserID identify the two accounts. They may be the
	// same account when a user's orders match each other.
	BuyerUserID  string `json:"buyerUserId"`
	SellerUserID string `json:"sellerUserId"`
}

// Metadata is the caller-generated identity of one execution record.
type Metadata struct {
	ID         string
	Sequence   int64
	ExecutedAt time.Time
}

// Errors reported by Map. Callers should compare with errors.Is.
var (
	ErrMetadata     = errors.New("executions: invalid metadata")
	ErrMissingOrder = errors.New("executions: missing order")
	ErrMismatch     = errors.New("executions: incompatible orders")
	ErrInvalidFill  = errors.New("executions: invalid fill")
)

// Map converts the fills produced by one matching.Book.Submit call into
// execution records, one per fill and in the same order.
//
// incoming is the canonical order that was submitted. resting holds the
// canonical resting orders by ID; it must contain every RestingID in fills.
// meta supplies one Metadata per fill. Map returns no records unless every
// fill maps cleanly.
func Map(incoming orders.Order, resting map[string]orders.Order, fills []matching.Execution, meta []Metadata) ([]Record, error) {
	if len(meta) != len(fills) {
		return nil, fmt.Errorf("%w: %d entries for %d fills", ErrMetadata, len(meta), len(fills))
	}
	if len(fills) == 0 {
		return []Record{}, nil
	}
	if err := incoming.Validate(); err != nil {
		return nil, fmt.Errorf("%w: incoming %s: %w", ErrMismatch, incoming.ID, err)
	}

	records := make([]Record, 0, len(fills))
	seen := make(map[string]bool, len(meta))
	for i, f := range fills {
		m := meta[i]
		switch {
		case m.ID == "" || seen[m.ID]:
			return nil, fmt.Errorf("%w: fill %d: missing or duplicate id %q", ErrMetadata, i, m.ID)
		case m.Sequence <= 0 || i > 0 && m.Sequence <= meta[i-1].Sequence:
			return nil, fmt.Errorf("%w: fill %d: sequence %d must be positive and increasing", ErrMetadata, i, m.Sequence)
		case m.ExecutedAt.IsZero():
			return nil, fmt.Errorf("%w: fill %d: missing timestamp", ErrMetadata, i)
		}
		seen[m.ID] = true

		r, err := mapFill(incoming, resting, f)
		if err != nil {
			return nil, fmt.Errorf("fill %d: %w", i, err)
		}
		r.ID, r.Sequence, r.ExecutedAt = m.ID, m.Sequence, m.ExecutedAt.UTC()
		records = append(records, r)
	}
	return records, nil
}

// mapFill assigns buyer and seller for one fill and checks that the fill is
// consistent with both orders.
func mapFill(incoming orders.Order, resting map[string]orders.Order, f matching.Execution) (Record, error) {
	if f.IncomingID != incoming.ID {
		return Record{}, fmt.Errorf("%w: incoming id %q, want %q", ErrMismatch, f.IncomingID, incoming.ID)
	}
	r, ok := resting[f.RestingID]
	if !ok {
		return Record{}, fmt.Errorf("%w: resting %q", ErrMissingOrder, f.RestingID)
	}
	if err := r.Validate(); err != nil {
		return Record{}, fmt.Errorf("%w: resting %s: %w", ErrMismatch, r.ID, err)
	}
	switch {
	case f.Symbol != incoming.Symbol || r.Symbol != incoming.Symbol:
		return Record{}, fmt.Errorf("%w: symbols %s, %s, fill %s", ErrMismatch, incoming.Symbol, r.Symbol, f.Symbol)
	case r.Side != incoming.Side.Opposite():
		return Record{}, fmt.Errorf("%w: both orders are %s", ErrMismatch, r.Side)
	case incoming.Type != orders.TypeLimit || r.Type != orders.TypeLimit:
		return Record{}, fmt.Errorf("%w: only limit orders trade", ErrMismatch)
	case f.Quantity <= 0 || f.Quantity > incoming.Quantity || f.Quantity > r.Quantity:
		return Record{}, fmt.Errorf("%w: quantity %d", ErrInvalidFill, f.Quantity)
	case f.Price != r.Price:
		return Record{}, fmt.Errorf("%w: price %d, resting limit %d", ErrInvalidFill, f.Price, r.Price)
	}

	buy, sell := incoming, r
	if incoming.Side == orders.SideSell {
		buy, sell = r, incoming
	}
	if f.Price > buy.Price || f.Price < sell.Price {
		return Record{}, fmt.Errorf("%w: price %d outside limits buy %d, sell %d", ErrInvalidFill, f.Price, buy.Price, sell.Price)
	}
	return Record{
		Symbol:       f.Symbol,
		Price:        f.Price,
		Quantity:     f.Quantity,
		BuyOrderID:   buy.ID,
		SellOrderID:  sell.ID,
		BuyerUserID:  buy.UserID,
		SellerUserID: sell.UserID,
	}, nil
}
