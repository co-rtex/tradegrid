// Package instruments provides the fixed tradable instruments for the demo.
package instruments

// Instrument describes a tradable symbol in the API catalog.
type Instrument struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

// Keep entries sorted by symbol: List supplies the API's stable display order
// and Seed uses the same catalog when creating account positions.
var catalog = [...]Instrument{
	{Symbol: "AAPL", Name: "Apple Inc."},
	{Symbol: "MSFT", Name: "Microsoft Corporation"},
}

// List returns the catalog in symbol order. The returned slice is independent.
func List() []Instrument {
	// Copy the array into a new slice so callers cannot alter future results.
	return append([]Instrument(nil), catalog[:]...)
}

// Supported matches symbols exactly; case and whitespace are not normalized.
func Supported(symbol string) bool {
	for _, instrument := range catalog {
		if instrument.Symbol == symbol {
			return true
		}
	}
	return false
}
