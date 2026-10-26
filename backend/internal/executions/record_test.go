package executions

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/matching"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

var t0 = time.Date(2026, 10, 20, 14, 30, 0, 0, time.UTC)

func usd(dollars int64) int64 { return dollars * 100 }

func order(t *testing.T, id, user string, side orders.Side, qty, dollars int64) orders.Order {
	t.Helper()
	o, err := orders.New(orders.Params{
		ID: id, UserID: user, Symbol: "AAPL", Side: side, Type: orders.TypeLimit,
		Price: usd(dollars), Quantity: qty, CreatedAt: t0,
	})
	if err != nil {
		t.Fatalf("orders.New(%s): %v", id, err)
	}
	return o
}

// match rests each order in turn, then submits incoming, and returns the
// incoming order's fills plus the resting orders by ID.
func match(t *testing.T, rest []orders.Order, incoming orders.Order) ([]matching.Execution, map[string]orders.Order) {
	t.Helper()
	bk := matching.NewBook()
	byID := make(map[string]orders.Order, len(rest))
	for _, o := range append(slices.Clone(rest), incoming) {
		in, err := matching.FromOrder(o)
		if err != nil {
			t.Fatalf("FromOrder(%s): %v", o.ID, err)
		}
		res := bk.Submit(in)
		if res.Err != nil {
			t.Fatalf("Submit(%s): %v", o.ID, res.Err)
		}
		if o.ID == incoming.ID {
			return res.Executions, byID
		}
		if len(res.Executions) != 0 {
			t.Fatalf("resting order %s traded while building the book", o.ID)
		}
		byID[o.ID] = o
	}
	panic("unreachable")
}

func metadata(n int) []Metadata {
	meta := make([]Metadata, n)
	for i := range meta {
		meta[i] = Metadata{
			ID:         "E" + string(rune('1'+i)),
			Sequence:   int64(10 + i),
			ExecutedAt: t0.Add(time.Duration(i) * time.Second),
		}
	}
	return meta
}

func rec(id string, seq int64, sec int, qty, dollars int64, buyID, sellID, buyer, seller string) Record {
	return Record{
		ID: id, Sequence: seq, ExecutedAt: t0.Add(time.Duration(sec) * time.Second),
		Symbol: "AAPL", Price: usd(dollars), Quantity: qty,
		BuyOrderID: buyID, SellOrderID: sellID, BuyerUserID: buyer, SellerUserID: seller,
	}
}

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		rest     func(t *testing.T) []orders.Order
		incoming func(t *testing.T) orders.Order
		want     []Record
	}{
		{
			name: "incoming buy across two price levels",
			rest: func(t *testing.T) []orders.Order {
				return []orders.Order{
					order(t, "S1", "alice", orders.SideSell, 3, 100),
					order(t, "S2", "bob", orders.SideSell, 4, 101),
				}
			},
			incoming: func(t *testing.T) orders.Order { return order(t, "B1", "carol", orders.SideBuy, 5, 101) },
			want: []Record{
				rec("E1", 10, 0, 3, 100, "B1", "S1", "carol", "alice"),
				rec("E2", 11, 1, 2, 101, "B1", "S2", "carol", "bob"),
			},
		},
		{
			name: "incoming sell across two price levels",
			rest: func(t *testing.T) []orders.Order {
				return []orders.Order{
					order(t, "B1", "alice", orders.SideBuy, 2, 100),
					order(t, "B2", "bob", orders.SideBuy, 2, 102),
				}
			},
			incoming: func(t *testing.T) orders.Order { return order(t, "S1", "carol", orders.SideSell, 3, 100) },
			want: []Record{
				rec("E1", 10, 0, 2, 102, "B2", "S1", "bob", "carol"),
				rec("E2", 11, 1, 1, 100, "B1", "S1", "alice", "carol"),
			},
		},
		{
			name: "same user on both sides",
			rest: func(t *testing.T) []orders.Order {
				return []orders.Order{order(t, "S1", "alice", orders.SideSell, 5, 100)}
			},
			incoming: func(t *testing.T) orders.Order { return order(t, "B1", "alice", orders.SideBuy, 5, 100) },
			want:     []Record{rec("E1", 10, 0, 5, 100, "B1", "S1", "alice", "alice")},
		},
		{
			name: "no fills",
			rest: func(t *testing.T) []orders.Order {
				return []orders.Order{order(t, "S1", "alice", orders.SideSell, 5, 101)}
			},
			incoming: func(t *testing.T) orders.Order { return order(t, "B1", "bob", orders.SideBuy, 5, 100) },
			want:     []Record{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			incoming := tt.incoming(t)
			fills, resting := match(t, tt.rest(t), incoming)
			meta := metadata(len(fills))

			got, err := Map(incoming, resting, fills, meta)
			if err != nil {
				t.Fatalf("Map: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("records:\n got  %+v\n want %+v", got, tt.want)
			}

			// Deterministic: the same inputs give the same records.
			again, err := Map(incoming, resting, fills, meta)
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatalf("second Map differs: %+v, %v", again, err)
			}
		})
	}
}

func TestMapConvertsTimestampsToUTC(t *testing.T) {
	incoming := order(t, "B1", "bob", orders.SideBuy, 1, 100)
	fills, resting := match(t, []orders.Order{order(t, "S1", "alice", orders.SideSell, 1, 100)}, incoming)
	chicago := time.FixedZone("CDT", -5*60*60)
	meta := []Metadata{{ID: "E1", Sequence: 1, ExecutedAt: t0.In(chicago)}}

	got, err := Map(incoming, resting, fills, meta)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if loc := got[0].ExecutedAt.Location(); loc != time.UTC || !got[0].ExecutedAt.Equal(t0) {
		t.Fatalf("ExecutedAt = %v (%v), want %v UTC", got[0].ExecutedAt, loc, t0)
	}
}

func TestMapRejects(t *testing.T) {
	// The base scenario fills B1 against S1 and S2. Each case breaks one input.
	type inputs struct {
		incoming orders.Order
		resting  map[string]orders.Order
		fills    []matching.Execution
		meta     []Metadata
	}
	base := func(t *testing.T) inputs {
		incoming := order(t, "B1", "carol", orders.SideBuy, 5, 101)
		fills, resting := match(t, []orders.Order{
			order(t, "S1", "alice", orders.SideSell, 3, 100),
			order(t, "S2", "bob", orders.SideSell, 4, 101),
		}, incoming)
		return inputs{incoming, resting, fills, metadata(len(fills))}
	}
	tests := []struct {
		name    string
		break_  func(t *testing.T, in *inputs)
		wantErr error
	}{
		{"missing resting order", func(t *testing.T, in *inputs) { delete(in.resting, "S2") }, ErrMissingOrder},
		{"resting order on the same side", func(t *testing.T, in *inputs) {
			in.resting["S2"] = order(t, "S2", "bob", orders.SideBuy, 4, 101)
		}, ErrMismatch},
		{"resting order for another symbol", func(t *testing.T, in *inputs) {
			o := in.resting["S2"]
			o.Symbol = "MSFT"
			in.resting["S2"] = o
		}, ErrMismatch},
		{"fill symbol differs", func(t *testing.T, in *inputs) { in.fills[1].Symbol = "MSFT" }, ErrMismatch},
		{"fill for another incoming order", func(t *testing.T, in *inputs) { in.fills[0].IncomingID = "B9" }, ErrMismatch},
		{"invalid incoming order", func(t *testing.T, in *inputs) { in.incoming.UserID = "" }, ErrMismatch},
		{"price not the resting price", func(t *testing.T, in *inputs) { in.fills[0].Price = usd(101) }, ErrInvalidFill},
		{"price above buy limit", func(t *testing.T, in *inputs) {
			o := in.resting["S2"]
			o.Price = usd(102)
			in.resting["S2"] = o
			in.fills[1].Price = usd(102)
		}, ErrInvalidFill},
		{"zero quantity", func(t *testing.T, in *inputs) { in.fills[1].Quantity = 0 }, ErrInvalidFill},
		{"quantity above order size", func(t *testing.T, in *inputs) { in.fills[0].Quantity = 6 }, ErrInvalidFill},
		{"metadata count mismatch", func(t *testing.T, in *inputs) { in.meta = in.meta[:1] }, ErrMetadata},
		{"missing execution id", func(t *testing.T, in *inputs) { in.meta[1].ID = "" }, ErrMetadata},
		{"duplicate execution id", func(t *testing.T, in *inputs) { in.meta[1].ID = in.meta[0].ID }, ErrMetadata},
		{"sequence not increasing", func(t *testing.T, in *inputs) { in.meta[1].Sequence = in.meta[0].Sequence }, ErrMetadata},
		{"nonpositive sequence", func(t *testing.T, in *inputs) { in.meta[0].Sequence = 0 }, ErrMetadata},
		{"missing timestamp", func(t *testing.T, in *inputs) { in.meta[1].ExecutedAt = time.Time{} }, ErrMetadata},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base(t)
			tt.break_(t, &in)
			incoming, resting, fills := in.incoming, maps.Clone(in.resting), slices.Clone(in.fills)

			got, err := Map(in.incoming, in.resting, in.fills, in.meta)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("got %+v, want no records", got)
			}
			// Map must not change what it was given.
			if in.incoming != incoming || !maps.Equal(in.resting, resting) || !slices.Equal(in.fills, fills) {
				t.Fatalf("Map modified its inputs")
			}
		})
	}
}
