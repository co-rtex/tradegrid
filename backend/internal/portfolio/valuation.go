package portfolio

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Errors reported by Value, in addition to ErrInvalidLot, ErrInvalidPrice, and
// ErrOverflow. Callers should compare with errors.Is. Every one of them is a
// server bug and maps to INTERNAL ("API: GET /api/v1/portfolio" in
// docs/portfolio.md).
var (
	ErrMissingPrice    = errors.New("portfolio: missing reference price")
	ErrInvalidSnapshot = errors.New("portfolio: invalid account snapshot")
)

// Balance is cash in cents or a position in shares, as in GET
// /api/v1/accounts. Reserved is held by resting orders and is still owned;
// Available is Total minus Reserved. Its fields match accounts.Balance, so a
// ledger balance converts with portfolio.Balance(b).
type Balance struct {
	Total     int64 `json:"total"`
	Reserved  int64 `json:"reserved"`
	Available int64 `json:"available"`
}

// Holding is one symbol's input to Value: the share balance from the ledger
// and the Lot tracked beside it. Lot.Quantity must equal Shares.Total.
type Holding struct {
	Symbol string
	Shares Balance
	Lot    Lot
}

// AccountSnapshot is one account's input to Value, copied from the exchange's
// state under its serialized boundary. Holdings may be in any order but must
// not repeat a symbol; every holding is valued, including zero holdings.
type AccountSnapshot struct {
	UserID       string
	DisplayName  string
	AsOfSequence int64
	Cash         Balance
	Holdings     []Holding
}

// Valuation is one account's portfolio. Its JSON form is the response of GET
// /api/v1/portfolio in docs/portfolio.md.
type Valuation struct {
	UserID       string              `json:"userId"`
	DisplayName  string              `json:"displayName"`
	AsOfSequence int64               `json:"asOfSequence"`
	Cash         Balance             `json:"cash"`
	Positions    []PositionValuation `json:"positions"`
	Totals       Totals              `json:"totals"`
}

// PositionValuation is one symbol's row in a Valuation. MarketValue is
// Total × ReferencePrice and UnrealizedPnL is MarketValue - CostBasis. Reserved
// shares are part of Total and so of MarketValue.
type PositionValuation struct {
	Symbol string `json:"symbol"`
	Balance
	CostBasis      int64 `json:"costBasis"`
	ReferencePrice int64 `json:"referencePrice"`
	MarketValue    int64 `json:"marketValue"`
	UnrealizedPnL  int64 `json:"unrealizedPnl"`
	RealizedPnL    int64 `json:"realizedPnl"`
}

// Totals sums a Valuation's positions. Equity is cash total plus MarketValue:
// reserved cash and shares are already inside those totals and are never
// added or subtracted again.
type Totals struct {
	CostBasis     int64 `json:"costBasis"`
	MarketValue   int64 `json:"marketValue"`
	UnrealizedPnL int64 `json:"unrealizedPnl"`
	RealizedPnL   int64 `json:"realizedPnl"`
	Equity        int64 `json:"equity"`
}

// Value returns the portfolio of the account in s, valuing each holding at
// its reference price in prices (symbol to cents). Positions are sorted by
// symbol.
//
// prices must cover exactly the tradable symbols, and s must hold a row for
// each of them, zero holdings included (docs/portfolio.md lists every
// tradable symbol). A holding without a price, or with a price that is not
// positive, is an error; it is never valued at zero. A priced symbol with no
// holding is an error too, so a missing row cannot silently drop its
// realized P&L from the totals. So is an inconsistent snapshot, an
// invalid Lot, or any amount outside [-MaxAmount, MaxAmount]. On error Value
// returns the zero Valuation. It never changes s or prices, and the result
// shares no memory with them.
func Value(s AccountSnapshot, prices map[string]int64) (Valuation, error) {
	if s.AsOfSequence < 0 {
		return Valuation{}, fmt.Errorf("%w: asOfSequence %d", ErrInvalidSnapshot, s.AsOfSequence)
	}
	if err := s.Cash.validate("cash"); err != nil {
		return Valuation{}, err
	}
	positions := make([]PositionValuation, 0, len(s.Holdings))
	for _, h := range s.Holdings {
		p, err := valuePosition(h, prices)
		if err != nil {
			return Valuation{}, err
		}
		positions = append(positions, p)
	}
	slices.SortFunc(positions, func(a, b PositionValuation) int { return cmp.Compare(a.Symbol, b.Symbol) })
	for i := 1; i < len(positions); i++ {
		if positions[i].Symbol == positions[i-1].Symbol {
			return Valuation{}, fmt.Errorf("%w: duplicate symbol %q", ErrInvalidSnapshot, positions[i].Symbol)
		}
	}
	// Every holding has a price and symbols are unique, so equal counts mean
	// every priced symbol has a holding.
	if len(positions) != len(prices) {
		var missing []string
		for symbol := range prices {
			if _, found := slices.BinarySearchFunc(positions, symbol, func(p PositionValuation, s string) int {
				return cmp.Compare(p.Symbol, s)
			}); !found {
				missing = append(missing, symbol)
			}
		}
		slices.Sort(missing)
		return Valuation{}, fmt.Errorf("%w: no holding for priced symbol(s) %s",
			ErrInvalidSnapshot, strings.Join(missing, ", "))
	}
	totals, err := sumTotals(s.Cash.Total, positions)
	if err != nil {
		return Valuation{}, err
	}
	return Valuation{
		UserID:       s.UserID,
		DisplayName:  s.DisplayName,
		AsOfSequence: s.AsOfSequence,
		Cash:         s.Cash,
		Positions:    positions,
		Totals:       totals,
	}, nil
}

// valuePosition validates h and values it at its reference price.
func valuePosition(h Holding, prices map[string]int64) (PositionValuation, error) {
	if h.Symbol == "" {
		return PositionValuation{}, fmt.Errorf("%w: holding with an empty symbol", ErrInvalidSnapshot)
	}
	if err := h.Shares.validate(h.Symbol + " shares"); err != nil {
		return PositionValuation{}, err
	}
	if err := h.Lot.Validate(); err != nil {
		return PositionValuation{}, fmt.Errorf("%w (symbol %s)", err, h.Symbol)
	}
	if h.Lot.Quantity != h.Shares.Total {
		return PositionValuation{}, fmt.Errorf("%w: %s lot quantity %d, shares total %d",
			ErrInvalidSnapshot, h.Symbol, h.Lot.Quantity, h.Shares.Total)
	}
	price, ok := prices[h.Symbol]
	if !ok {
		return PositionValuation{}, fmt.Errorf("%w: %q", ErrMissingPrice, h.Symbol)
	}
	if price <= 0 {
		return PositionValuation{}, fmt.Errorf("%w: %s reference price %d", ErrInvalidPrice, h.Symbol, price)
	}
	if price > MaxAmount {
		return PositionValuation{}, fmt.Errorf("%w: %s reference price %d", ErrOverflow, h.Symbol, price)
	}
	var marketValue int64
	if h.Shares.Total > 0 {
		v, err := mul(h.Shares.Total, price)
		if err != nil {
			return PositionValuation{}, fmt.Errorf("%w (%s market value)", err, h.Symbol)
		}
		marketValue = v
	}
	return PositionValuation{
		Symbol:         h.Symbol,
		Balance:        h.Shares,
		CostBasis:      h.Lot.CostBasis,
		ReferencePrice: price,
		MarketValue:    marketValue,
		// Both terms are within [0, MaxAmount], so the difference is within
		// ±MaxAmount and cannot wrap.
		UnrealizedPnL: marketValue - h.Lot.CostBasis,
		RealizedPnL:   h.Lot.RealizedPnL,
	}, nil
}

// sumTotals adds up positions and the equity, checking every sum.
func sumTotals(cashTotal int64, positions []PositionValuation) (Totals, error) {
	var t Totals
	var err error
	for _, p := range positions {
		if t.CostBasis, err = add(t.CostBasis, p.CostBasis); err != nil {
			return Totals{}, fmt.Errorf("%w (total cost basis)", err)
		}
		if t.MarketValue, err = add(t.MarketValue, p.MarketValue); err != nil {
			return Totals{}, fmt.Errorf("%w (total market value)", err)
		}
		if t.UnrealizedPnL, err = add(t.UnrealizedPnL, p.UnrealizedPnL); err != nil {
			return Totals{}, fmt.Errorf("%w (total unrealized P&L)", err)
		}
		if t.RealizedPnL, err = add(t.RealizedPnL, p.RealizedPnL); err != nil {
			return Totals{}, fmt.Errorf("%w (total realized P&L)", err)
		}
	}
	// Reserved cash is part of cashTotal: it is not subtracted or added again.
	if t.Equity, err = add(cashTotal, t.MarketValue); err != nil {
		return Totals{}, fmt.Errorf("%w (equity)", err)
	}
	return t, nil
}

// validate reports whether b is a consistent balance within [0, MaxAmount].
// name identifies b in the error.
func (b Balance) validate(name string) error {
	switch {
	case b.Total < 0 || b.Total > MaxAmount:
		return fmt.Errorf("%w: %s total %d", ErrInvalidSnapshot, name, b.Total)
	case b.Reserved < 0 || b.Reserved > b.Total:
		return fmt.Errorf("%w: %s reserved %d of total %d", ErrInvalidSnapshot, name, b.Reserved, b.Total)
	case b.Available != b.Total-b.Reserved:
		return fmt.Errorf("%w: %s available %d, want %d", ErrInvalidSnapshot, name, b.Available, b.Total-b.Reserved)
	}
	return nil
}
