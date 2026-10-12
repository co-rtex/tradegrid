package matching

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestValidateQuantity(t *testing.T) {
	tests := []struct {
		name     string
		quantity int64
		wantErr  bool
	}{
		{"zero", 0, true},
		{"negative", -1, true},
		{"minimum int64", math.MinInt64, true},
		{"one", 1, false},
		{"positive", 100, false},
		{"maximum int64", math.MaxInt64, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := Order{Quantity: tt.quantity}
			before := o
			err := o.ValidateQuantity()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidQuantity) {
					t.Fatalf("ValidateQuantity() = %v, want ErrInvalidQuantity", err)
				}
				if err.Error() != "order quantity must be greater than zero" {
					t.Errorf("unexpected validation message: %q", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidateQuantity() = %v, want nil", err)
			}
			if o != before {
				t.Error("validation changed the order")
			}
		})
	}
}

func TestSubmitRejectsInvalidQuantity(t *testing.T) {
	for _, side := range []Side{Buy, Sell} {
		for _, quantity := range []int64{0, -1, math.MinInt64} {
			for _, symbol := range []string{"AAPL", "NEW"} {
				o := Order{ID: "invalid", Symbol: symbol, Side: side, Price: 100, Quantity: quantity}
				bk := NewBook()
				bk.Submit(Order{ID: "bid", Symbol: "AAPL", Side: Buy, Price: 100, Quantity: 5})
				bk.Submit(Order{ID: "ask", Symbol: "AAPL", Side: Sell, Price: 101, Quantity: 5})
				bids, asks := bk.Bids("AAPL"), bk.Asks("AAPL")
				got := bk.Submit(o)
				if !errors.Is(got.Err, ErrInvalidQuantity) || len(got.Executions) != 0 || got.Remaining != 0 {
					t.Fatalf("Submit(%+v) = %+v, want rejection with no fills or remainder", o, got)
				}
				if !reflect.DeepEqual(bk.Bids("AAPL"), bids) || !reflect.DeepEqual(bk.Asks("AAPL"), asks) || len(bk.symbols) != 1 {
					t.Fatalf("Submit(%+v) changed the book", o)
				}
			}
		}
	}
}

func TestSubmitAcceptsPositiveQuantity(t *testing.T) {
	for _, side := range []Side{Buy, Sell} {
		for _, quantity := range []int64{1, 100, math.MaxInt64} {
			bk := NewBook()
			o := Order{ID: "valid", Symbol: "AAPL", Side: side, Price: 100, Quantity: quantity}
			got := bk.Submit(o)
			if got.Err != nil || len(got.Executions) != 0 || got.Remaining != quantity {
				t.Fatalf("Submit(%+v) = %+v, want accepted resting order", o, got)
			}
			entries := bk.Bids(o.Symbol)
			if side == Sell {
				entries = bk.Asks(o.Symbol)
			}
			want := []BookEntry{{ID: o.ID, Price: o.Price, Remaining: quantity}}
			if !reflect.DeepEqual(entries, want) {
				t.Fatalf("book entries = %+v, want %+v", entries, want)
			}
		}
	}
}
