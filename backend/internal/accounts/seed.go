package accounts

import (
	"github.com/co-rtex/tradegrid/backend/internal/instruments"
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
func Seed() *Ledger {
	l := &Ledger{
		userIDs:  make([]string, 0, len(demoAccounts)),
		names:    make(map[string]string, len(demoAccounts)),
		balances: make(map[string]risk.Account, len(demoAccounts)),
	}
	for _, d := range demoAccounts {
		catalog := instruments.List()
		positions := make(map[string]risk.Balance, len(catalog))
		for _, instrument := range catalog {
			positions[instrument.Symbol] = risk.Balance{Total: DemoShares}
		}
		l.userIDs = append(l.userIDs, d.userID)
		l.names[d.userID] = d.displayName
		l.balances[d.userID] = risk.Account{
			Cash:      risk.Balance{Total: DemoCash},
			Positions: positions,
		}
	}
	return l
}
