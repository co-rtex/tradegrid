package matching

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

func newDomainOrder(t *testing.T, mutate func(*orders.Params)) orders.Order {
	t.Helper()
	p := orders.Params{
		ID:        "o1",
		UserID:    "u1",
		Symbol:    "AAPL",
		Side:      orders.SideBuy,
		Type:      orders.TypeLimit,
		Price:     usd(100),
		Quantity:  10,
		CreatedAt: time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC),
	}
	if mutate != nil {
		mutate(&p)
	}
	o, err := orders.New(p)
	if err != nil {
		t.Fatalf("orders.New: %v", err)
	}
	return o
}

func TestFromOrder(t *testing.T) {
	for _, side := range []Side{Buy, Sell} {
		t.Run(side.String(), func(t *testing.T) {
			o := newDomainOrder(t, func(p *orders.Params) { p.Side = side })
			got, err := FromOrder(o)
			if err != nil {
				t.Fatalf("FromOrder: %v", err)
			}
			want := Order{ID: "o1", Symbol: "AAPL", Side: side, Price: usd(100), Quantity: 10}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestFromOrderRejects(t *testing.T) {
	at := time.Date(2026, 10, 13, 9, 31, 0, 0, time.UTC)
	tests := []struct {
		name    string
		order   func(t *testing.T) orders.Order
		wantErr error
	}{
		{"market order", func(t *testing.T) orders.Order {
			return newDomainOrder(t, func(p *orders.Params) { p.Type = orders.TypeMarket; p.Price = 0 })
		}, ErrUnsupportedOrder},
		{"partially filled", func(t *testing.T) orders.Order {
			o := newDomainOrder(t, nil)
			if err := o.Fill(4, at); err != nil {
				t.Fatal(err)
			}
			return o
		}, ErrUnsupportedOrder},
		{"canceled", func(t *testing.T) orders.Order {
			o := newDomainOrder(t, nil)
			if err := o.Cancel(at); err != nil {
				t.Fatal(err)
			}
			return o
		}, ErrUnsupportedOrder},
		{"rejected", func(t *testing.T) orders.Order {
			o := newDomainOrder(t, nil)
			if err := o.Reject(at); err != nil {
				t.Fatal(err)
			}
			return o
		}, ErrUnsupportedOrder},
		{"invalid domain order", func(t *testing.T) orders.Order {
			o := newDomainOrder(t, nil)
			o.Side = orders.SideUnspecified
			return o
		}, orders.ErrInvalidSide},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromOrder(tt.order(t))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != (Order{}) {
				t.Fatalf("got %+v, want zero Order", got)
			}
		})
	}
}

// Time priority follows acceptance order, not timestamps or IDs (spec
// "Use equal timestamps with a known acceptance order"). Three asks share a
// timestamp and are accepted out of ID order; a fourth is accepted last with
// an earlier timestamp and must still trade last.
func TestEqualTimestampsFollowAcceptanceOrder(t *testing.T) {
	same := time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC)
	earlier := same.Add(-time.Minute)
	asks := []struct {
		id string
		at time.Time
	}{{"S3", same}, {"S1", same}, {"S2", same}, {"S0", earlier}}

	bk := NewBook()
	for _, a := range asks {
		o := newDomainOrder(t, func(p *orders.Params) {
			p.ID, p.Side, p.Quantity, p.CreatedAt = a.id, orders.SideSell, 2, a.at
		})
		in, err := FromOrder(o)
		if err != nil {
			t.Fatalf("FromOrder(%s): %v", a.id, err)
		}
		bk.Submit(in)
	}

	got := bk.Submit(buy("B1", 7, 100))

	want := []Execution{
		exec("B1", "S3", 2, 100),
		exec("B1", "S1", 2, 100),
		exec("B1", "S2", 2, 100),
		exec("B1", "S0", 1, 100),
	}
	if !slices.Equal(got.Executions, want) {
		t.Errorf("executions:\n got  %v\n want %v", got.Executions, want)
	}
	if want := []BookEntry{entry("S0", 1, 100)}; !slices.Equal(bk.Asks(sym), want) {
		t.Errorf("asks:\n got  %v\n want %v", bk.Asks(sym), want)
	}
}
