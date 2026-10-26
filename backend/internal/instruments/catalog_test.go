package instruments

import (
	"reflect"
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
