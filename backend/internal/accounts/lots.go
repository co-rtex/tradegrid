package accounts

import (
	"fmt"
	"maps"
	"slices"

	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/portfolio"
)

// Cost basis and realized P&L (docs/portfolio.md). Every account keeps one
// portfolio.Lot per symbol beside its share balance. The lot's Quantity is
// always the balance's Total; settlement changes both in the same step, and
// any disagreement is ErrInconsistent.

// lotBook holds every account's lots by user ID, then symbol. Like the
// balances, it is replaced rather than changed in place, so a published
// ledger's lots can be read concurrently.
type lotBook map[string]map[string]portfolio.Lot

func (b lotBook) clone() lotBook {
	out := make(lotBook, len(b))
	for id, lots := range b {
		out[id] = maps.Clone(lots)
	}
	return out
}

// applyLots returns the lots after records, applied in order: for each
// execution the buyer's lot gains the shares at the execution price and the
// seller's lot gives them up under average cost. A same-account execution
// changes neither lot, as docs/portfolio.md requires. Only the accounts the
// records touch are copied; b is never changed.
func (b lotBook) applyLots(records []executions.Record) (lotBook, error) {
	out := maps.Clone(b)
	copied := map[string]bool{}
	lotsOf := func(userID string) (map[string]portfolio.Lot, error) {
		lots, ok := out[userID]
		if !ok {
			return nil, fmt.Errorf("%w: no cost basis for %q", ErrInconsistent, userID)
		}
		if !copied[userID] {
			lots = maps.Clone(lots)
			out[userID] = lots
			copied[userID] = true
		}
		return lots, nil
	}

	for _, r := range records {
		if r.BuyerUserID == r.SellerUserID {
			continue
		}
		buyer, err := lotsOf(r.BuyerUserID)
		if err != nil {
			return nil, err
		}
		seller, err := lotsOf(r.SellerUserID)
		if err != nil {
			return nil, err
		}
		bought, err := portfolio.ApplyBuy(buyer[r.Symbol], r.Quantity, r.Price)
		if err != nil {
			return nil, fmt.Errorf("%w: execution %s: buyer %s: %w", ErrInconsistent, r.ID, r.BuyerUserID, err)
		}
		sold, err := portfolio.ApplySell(seller[r.Symbol], r.Quantity, r.Price)
		if err != nil {
			return nil, fmt.Errorf("%w: execution %s: seller %s: %w", ErrInconsistent, r.ID, r.SellerUserID, err)
		}
		buyer[r.Symbol] = bought
		seller[r.Symbol] = sold
	}
	return out, nil
}

// checkLots verifies that every lot of the given accounts is valid and holds
// exactly the account's share total, and that every position has a lot.
func (l *Ledger) checkLots(userIDs []string) error {
	for _, id := range userIDs {
		a, lots := l.balances[id], l.lots[id]
		symbols := slices.Sorted(maps.Keys(a.Positions))
		for symbol := range lots {
			if _, held := a.Positions[symbol]; !held {
				symbols = append(symbols, symbol)
			}
		}
		for _, symbol := range symbols {
			lot, ok := lots[symbol]
			if !ok {
				return fmt.Errorf("%w: %s has no cost basis for %s", ErrInconsistent, id, symbol)
			}
			if err := lot.Validate(); err != nil {
				return fmt.Errorf("%w: %s %s: %w", ErrInconsistent, id, symbol, err)
			}
			if total := a.Positions[symbol].Total; lot.Quantity != total {
				return fmt.Errorf("%w: %s %s cost basis covers %d shares, account holds %d",
					ErrInconsistent, id, symbol, lot.Quantity, total)
			}
		}
	}
	return nil
}

// Holding is one symbol of an account in a portfolio snapshot: the share
// balance from Snapshot plus the cost-basis lot for those shares.
type Holding struct {
	Symbol string
	Shares Balance
	Lot    portfolio.Lot
}

// PortfolioSnapshot is a copied view of one account for portfolio valuation
// (#65, #69). Holdings lists every symbol the account has a position in,
// sorted by symbol and including zero holdings, like Snapshot.Positions.
type PortfolioSnapshot struct {
	UserID      string
	DisplayName string
	Cash        Balance
	Holdings    []Holding
}

// Portfolio returns a copy of one account's cash, positions, and cost-basis
// lots, or ErrUnknownUser. The result shares no memory with l.
func (l *Ledger) Portfolio(userID string) (PortfolioSnapshot, error) {
	a, ok := l.balances[userID]
	if !ok {
		return PortfolioSnapshot{}, fmt.Errorf("%w: %q", ErrUnknownUser, userID)
	}
	lots := l.lots[userID]
	holdings := make([]Holding, 0, len(a.Positions))
	for _, symbol := range slices.Sorted(maps.Keys(a.Positions)) {
		holdings = append(holdings, Holding{Symbol: symbol, Shares: balance(a.Positions[symbol]), Lot: lots[symbol]})
	}
	return PortfolioSnapshot{
		UserID:      userID,
		DisplayName: l.names[userID],
		Cash:        balance(a.Cash),
		Holdings:    holdings,
	}, nil
}
