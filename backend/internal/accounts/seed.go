package accounts

import (
	"fmt"

	"github.com/co-rtex/tradegrid/backend/internal/instruments"
	"github.com/co-rtex/tradegrid/backend/internal/portfolio"
	"github.com/co-rtex/tradegrid/backend/internal/risk"
)

// Demo account values from docs/api.md ("Demo Accounts and Symbols").
const (
	// DemoCash is each demo account's starting cash in cents ($100,000.00).
	DemoCash int64 = 10_000_000
	// DemoShares is each demo account's starting position in every symbol.
	DemoShares int64 = 100
)

// demoAccounts lists the contracted demo accounts, sorted by user ID.
var demoAccounts = []struct {
	userID      string
	displayName string
}{
	{"alice", "Alice (demo)"},
	{"bob", "Bob (demo)"},
}

// Seed returns a new ledger holding exactly the contracted demo accounts:
// DemoCash cash and DemoShares of every demo symbol each, with nothing
// reserved. Every call returns an independent ledger, so a fresh exchange
// instance always starts from the same state.
//
// Each seeded position's cost basis is DemoShares × its reference price, with
// no realized P&L (docs/portfolio.md, "Seeded Positions"), so a fresh account
// shows zero P&L. The basis is set explicitly here and never inferred: a
// granted share is not a share bought for $0.
func Seed() *Ledger {
	l := &Ledger{
		userIDs:  make([]string, 0, len(demoAccounts)),
		names:    make(map[string]string, len(demoAccounts)),
		balances: make(map[string]risk.Account, len(demoAccounts)),
		lots:     make(lotBook, len(demoAccounts)),
	}
	// Seed positions from the same catalog used by exchange validation.
	catalog := instruments.List()
	for _, d := range demoAccounts {
		positions := make(map[string]risk.Balance, len(catalog))
		lots := make(map[string]portfolio.Lot, len(catalog))
		for _, instrument := range catalog {
			positions[instrument.Symbol] = risk.Balance{Total: DemoShares}
			lots[instrument.Symbol] = seededLot(instrument.Symbol)
		}
		l.lots[d.userID] = lots
		l.userIDs = append(l.userIDs, d.userID)
		l.names[d.userID] = d.displayName
		l.balances[d.userID] = risk.Account{
			Cash:      risk.Balance{Total: DemoCash},
			Positions: positions,
		}
	}
	return l
}

// seededLot is the starting lot for DemoShares of symbol. Every catalog
// symbol has a reference price (enforced by the instruments tests), so a
// failure here is a broken catalog, found the first time any exchange is
// created.
func seededLot(symbol string) portfolio.Lot {
	price, err := instruments.LookupReferencePrice(symbol)
	if err != nil {
		panic(fmt.Sprintf("accounts: seed %s: %v", symbol, err))
	}
	lot, err := portfolio.ApplyBuy(portfolio.Lot{}, DemoShares, price)
	if err != nil {
		panic(fmt.Sprintf("accounts: seed %s cost basis: %v", symbol, err))
	}
	return lot
}
