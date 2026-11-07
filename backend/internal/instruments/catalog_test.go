package instruments

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	want := []Instrument{{Symbol: "AAPL", Name: "Apple Inc."}, {Symbol: "MSFT", Name: "Microsoft Corporation"}}
	got := List()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog = %#v, want %#v", got, want)
	}
	// A caller may modify its copy without changing the shared catalog.
	got[0].Symbol = "CHANGED"
	if !reflect.DeepEqual(List(), want) {
		t.Fatal("caller modified the catalog")
	}
	for _, tc := range []struct {
		symbol string
		want   bool
	}{
		{"AAPL", true}, {"MSFT", true}, {"", false}, {"aapl", false},
		{" AAPL", false}, {"AAPL ", false}, {"GOOG", false},
	} {
		t.Run(tc.symbol, func(t *testing.T) {
			if got := Supported(tc.symbol); got != tc.want {
				t.Errorf("Supported(%q) = %v, want %v", tc.symbol, got, tc.want)
			}
		})
	}
}

func TestLookupReferencePrice(t *testing.T) {
	for _, tc := range []struct {
		symbol string
		want   int64
		err    error
	}{
		{symbol: "AAPL", want: 19_000},
		{symbol: "MSFT", want: 42_000},
		{symbol: "", err: ErrUnknownSymbol},
		{symbol: "aapl", err: ErrUnknownSymbol},
		{symbol: " AAPL", err: ErrUnknownSymbol},
		{symbol: "AAPL ", err: ErrUnknownSymbol},
		{symbol: "GOOG", err: ErrUnknownSymbol},
	} {
		t.Run(tc.symbol, func(t *testing.T) {
			got, err := LookupReferencePrice(tc.symbol)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Errorf("price = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReferencePricesAreOrderedAndCopied(t *testing.T) {
	want := []ReferencePrice{{Symbol: "AAPL", Price: 19_000}, {Symbol: "MSFT", Price: 42_000}}
	got := ReferencePrices()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReferencePrices() = %#v, want %#v", got, want)
	}
	// A caller may modify its copy without changing the shared catalog.
	got[0].Price = 1
	got[1].Symbol = "CHANGED"
	if !reflect.DeepEqual(ReferencePrices(), want) {
		t.Fatal("caller modified the reference prices")
	}
	if price, err := LookupReferencePrice("AAPL"); err != nil || price != 19_000 {
		t.Fatalf("lookup after caller edit = %d, %v", price, err)
	}
}

// Every catalog symbol has a usable price: the contract treats a supported
// symbol without one as a server bug, never as a value of zero.
func TestEveryCatalogSymbolHasAReferencePrice(t *testing.T) {
	prices := ReferencePrices()
	instruments := List()
	if len(prices) != len(instruments) {
		t.Fatalf("%d prices for %d instruments", len(prices), len(instruments))
	}
	for i, instrument := range instruments {
		if prices[i].Symbol != instrument.Symbol {
			t.Errorf("price %d is for %q, want %q", i, prices[i].Symbol, instrument.Symbol)
		}
		// The same bounds as an order price in docs/api.md.
		if prices[i].Price < 1 || prices[i].Price > 100_000_000 {
			t.Errorf("%s reference price %d is out of range", instrument.Symbol, prices[i].Price)
		}
	}
}

// The prices are defined by docs/portfolio.md, so the code and the contract
// must change together.
func TestReferencePricesMatchPortfolioContract(t *testing.T) {
	contract, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "portfolio.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ReferencePrices() {
		row := fmt.Sprintf("| `%s` | `%d` (", p.Symbol, p.Price)
		if !strings.Contains(string(contract), row) {
			t.Errorf("docs/portfolio.md has no reference-price row starting %q", row)
		}
	}
}

// The catalog's JSON is GET /api/v1/symbols: reference prices must not leak
// into that contracted shape.
func TestInstrumentJSONOmitsReferencePrice(t *testing.T) {
	got, err := json.Marshal(List()[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"symbol":"AAPL","name":"Apple Inc."}`; string(got) != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}
