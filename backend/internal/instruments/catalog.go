// Package instruments provides the fixed tradable instruments for the demo and
// their simulated reference prices.
//
// Reference prices are constants from docs/portfolio.md, used only to value
// positions. They are not market data: nothing fetches or updates them, trades
// do not move them, and they are never an execution price, which is always the
// resting order's price.
package instruments

import (
	"errors"
	"fmt"
)

// ErrUnknownSymbol reports a lookup for a symbol outside the catalog. Callers
// should compare with errors.Is.
var ErrUnknownSymbol = errors.New("instruments: unknown symbol")

// Instrument describes a tradable symbol in the API catalog.
type Instrument struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

// ReferencePrice is one symbol's simulated reference price in integer cents.
type ReferencePrice struct {
	Symbol string
	Price  int64
}

// listing is one catalog row. Every symbol must have a reference price, so a
// supported symbol can never be valued at zero.
type listing struct {
	Instrument
	referencePrice int64
}

// Keep entries sorted by symbol: List supplies the API's stable display order
// and Seed uses the same catalog when creating account positions. Changing a
// reference price changes every example in docs/portfolio.md, so it needs a
// pull request to that contract.
var catalog = [...]listing{
	{Instrument{Symbol: "AAPL", Name: "Apple Inc."}, 19_000},
	{Instrument{Symbol: "MSFT", Name: "Microsoft Corporation"}, 42_000},
}

// List returns the catalog in symbol order. The returned slice is independent.
func List() []Instrument {
	out := make([]Instrument, len(catalog))
	for i, l := range catalog {
		out[i] = l.Instrument
	}
	return out
}

// Supported matches symbols exactly; case and whitespace are not normalized.
func Supported(symbol string) bool {
	for _, l := range catalog {
		if l.Symbol == symbol {
			return true
		}
	}
	return false
}

// ReferencePrices returns every symbol's simulated reference price in symbol
// order. The returned slice is independent.
func ReferencePrices() []ReferencePrice {
	out := make([]ReferencePrice, len(catalog))
	for i, l := range catalog {
		out[i] = ReferencePrice{Symbol: l.Symbol, Price: l.referencePrice}
	}
	return out
}

// LookupReferencePrice returns symbol's simulated reference price in integer
// cents. Symbols match exactly, as in Supported. An unknown symbol returns
// ErrUnknownSymbol and no price.
func LookupReferencePrice(symbol string) (int64, error) {
	for _, l := range catalog {
		if l.Symbol == symbol {
			return l.referencePrice, nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownSymbol, symbol)
}
