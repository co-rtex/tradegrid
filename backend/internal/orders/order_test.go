package orders

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var (
	t0 = time.Date(2026, time.October, 11, 14, 30, 0, 0, time.UTC)
	t1 = t0.Add(time.Minute)
	t2 = t0.Add(2 * time.Minute)
)

// validParams returns parameters for a well-formed limit order that each test
// can adjust.
func validParams() Params {
	return Params{
		ID:        "O-1",
		UserID:    "U-1",
		Symbol:    "AAPL",
		Side:      SideBuy,
		Type:      TypeLimit,
		Price:     19900,
		Quantity:  10,
		CreatedAt: t0,
	}
}

func mustNew(t *testing.T, p Params) Order {
	t.Helper()
	o, err := New(p)
	if err != nil {
		t.Fatalf("New(%+v) returned error: %v", p, err)
	}
	return o
}

func TestNewSetsDerivedFields(t *testing.T) {
	o := mustNew(t, validParams())

	if o.Remaining != o.Quantity {
		t.Errorf("Remaining = %d, want %d", o.Remaining, o.Quantity)
	}
	if o.Status != StatusNew {
		t.Errorf("Status = %s, want %s", o.Status, StatusNew)
	}
	if !o.UpdatedAt.Equal(t0) {
		t.Errorf("UpdatedAt = %s, want %s", o.UpdatedAt, t0)
	}
	if got := o.Filled(); got != 0 {
		t.Errorf("Filled() = %d, want 0", got)
	}
	if !o.Open() || o.Terminal() {
		t.Errorf("Open() = %t, Terminal() = %t, want true, false", o.Open(), o.Terminal())
	}
}

func TestNewRejectsBadParams(t *testing.T) {
	cases := []struct {
		name  string
		mutte func(*Params)
		want  error
	}{
		{"missing id", func(p *Params) { p.ID = "" }, ErrMissingID},
		{"missing user", func(p *Params) { p.UserID = "" }, ErrMissingUserID},
		{"missing symbol", func(p *Params) { p.Symbol = "" }, ErrMissingSymbol},
		{"unspecified side", func(p *Params) { p.Side = SideUnspecified }, ErrInvalidSide},
		{"out of range side", func(p *Params) { p.Side = Side(99) }, ErrInvalidSide},
		{"unspecified type", func(p *Params) { p.Type = TypeUnspecified }, ErrInvalidType},
		{"zero quantity", func(p *Params) { p.Quantity = 0 }, ErrInvalidQuantity},
		{"negative quantity", func(p *Params) { p.Quantity = -5 }, ErrInvalidQuantity},
		{"limit without price", func(p *Params) { p.Price = 0 }, ErrInvalidPrice},
		{"negative price", func(p *Params) { p.Price = -1 }, ErrInvalidPrice},
		{"market with price", func(p *Params) { p.Type = TypeMarket }, ErrInvalidPrice},
		{"missing timestamp", func(p *Params) { p.CreatedAt = time.Time{} }, ErrMissingTime},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validParams()
			tc.mutte(&p)

			got, err := New(p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("New() error = %v, want %v", err, tc.want)
			}
			if got != (Order{}) {
				t.Errorf("New() returned %v on error, want zero Order", got)
			}
		})
	}
}

func TestNewAcceptsMarketOrder(t *testing.T) {
	p := validParams()
	p.Type = TypeMarket
	p.Price = 0

	o := mustNew(t, p)
	if o.Price != 0 {
		t.Errorf("Price = %d, want 0", o.Price)
	}
}

func TestFillPartialThenFull(t *testing.T) {
	o := mustNew(t, validParams())

	if err := o.Fill(4, t1); err != nil {
		t.Fatalf("first Fill returned error: %v", err)
	}
	if o.Remaining != 6 || o.Filled() != 4 {
		t.Errorf("after partial fill: remaining %d, filled %d; want 6, 4", o.Remaining, o.Filled())
	}
	if o.Status != StatusPartiallyFilled {
		t.Errorf("Status = %s, want %s", o.Status, StatusPartiallyFilled)
	}
	if !o.UpdatedAt.Equal(t1) {
		t.Errorf("UpdatedAt = %s, want %s", o.UpdatedAt, t1)
	}
	if o.Quantity != 10 {
		t.Errorf("Quantity changed to %d; original quantity must not change", o.Quantity)
	}

	if err := o.Fill(6, t2); err != nil {
		t.Fatalf("second Fill returned error: %v", err)
	}
	if o.Remaining != 0 || o.Status != StatusFilled {
		t.Errorf("after full fill: remaining %d, status %s; want 0, %s", o.Remaining, o.Status, StatusFilled)
	}
	if o.Open() || !o.Terminal() {
		t.Errorf("Open() = %t, Terminal() = %t, want false, true", o.Open(), o.Terminal())
	}
	if err := o.Validate(); err != nil {
		t.Errorf("Validate() after full fill returned error: %v", err)
	}
}

func TestFillRejected(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Order)
		qty   int64
		at    time.Time
		want  error
	}{
		{"zero quantity", func(*Order) {}, 0, t1, ErrInvalidQuantity},
		{"negative quantity", func(*Order) {}, -1, t1, ErrInvalidQuantity},
		{"more than remaining", func(*Order) {}, 11, t1, ErrFillTooLarge},
		{"missing timestamp", func(*Order) {}, 1, time.Time{}, ErrMissingTime},
		{"already filled", func(o *Order) { _ = o.Fill(10, t1) }, 1, t2, ErrNotOpen},
		{"canceled", func(o *Order) { _ = o.Cancel(t1) }, 1, t2, ErrNotOpen},
		{"rejected", func(o *Order) { _ = o.Reject(t1) }, 1, t2, ErrNotOpen},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustNew(t, validParams())
			tc.setup(&o)
			before := o

			if err := o.Fill(tc.qty, tc.at); !errors.Is(err, tc.want) {
				t.Fatalf("Fill() error = %v, want %v", err, tc.want)
			}
			if o != before {
				t.Errorf("order changed on a rejected fill:\n got %+v\nwant %+v", o, before)
			}
		})
	}
}

func TestCancelKeepsFills(t *testing.T) {
	o := mustNew(t, validParams())
	if err := o.Fill(3, t1); err != nil {
		t.Fatalf("Fill returned error: %v", err)
	}

	if err := o.Cancel(t2); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if o.Status != StatusCanceled {
		t.Errorf("Status = %s, want %s", o.Status, StatusCanceled)
	}
	if o.Filled() != 3 || o.Remaining != 7 {
		t.Errorf("filled %d, remaining %d; want 3, 7", o.Filled(), o.Remaining)
	}
	if !o.UpdatedAt.Equal(t2) {
		t.Errorf("UpdatedAt = %s, want %s", o.UpdatedAt, t2)
	}

	if err := o.Cancel(t2); !errors.Is(err, ErrNotOpen) {
		t.Errorf("second Cancel error = %v, want %v", err, ErrNotOpen)
	}
}

func TestRejectOnlyBeforeTrading(t *testing.T) {
	o := mustNew(t, validParams())
	if err := o.Reject(t1); err != nil {
		t.Fatalf("Reject returned error: %v", err)
	}
	if o.Status != StatusRejected || o.Remaining != o.Quantity {
		t.Errorf("after Reject: status %s, remaining %d; want %s, %d",
			o.Status, o.Remaining, StatusRejected, o.Quantity)
	}

	traded := mustNew(t, validParams())
	if err := traded.Fill(1, t1); err != nil {
		t.Fatalf("Fill returned error: %v", err)
	}
	if err := traded.Reject(t2); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Reject on a partially filled order = %v, want %v", err, ErrNotOpen)
	}
}

func TestValidateCatchesImpossibleStates(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Order)
		want error
	}{
		{"remaining above quantity", func(o *Order) { o.Remaining = 11 }, ErrInvalidRemain},
		{"negative remaining", func(o *Order) { o.Remaining = -1 }, ErrInvalidRemain},
		{"new with fills", func(o *Order) { o.Remaining = 5 }, ErrInvalidStatus},
		{"filled with remainder", func(o *Order) { o.Status = StatusFilled; o.Remaining = 5 }, ErrInvalidStatus},
		{"partial with nothing filled", func(o *Order) { o.Status = StatusPartiallyFilled }, ErrInvalidStatus},
		{"partial fully filled", func(o *Order) { o.Status = StatusPartiallyFilled; o.Remaining = 0 }, ErrInvalidStatus},
		{"rejected with fills", func(o *Order) { o.Status = StatusRejected; o.Remaining = 5 }, ErrInvalidStatus},
		{"unknown status", func(o *Order) { o.Status = Status(99) }, ErrInvalidStatus},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustNew(t, validParams())
			tc.mut(&o)
			if err := o.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestZeroOrderIsInvalid(t *testing.T) {
	var o Order
	if err := o.Validate(); err == nil {
		t.Fatal("Validate() on the zero Order returned nil, want an error")
	}
	if o.Side.Valid() || o.Type.Valid() || o.Status.Valid() {
		t.Error("zero values of Side, Type, and Status must not be valid members")
	}
}

func TestEnumStringsAndParsing(t *testing.T) {
	sides := map[Side]string{SideBuy: "BUY", SideSell: "SELL", SideUnspecified: "UNSPECIFIED"}
	for s, want := range sides {
		if got := s.String(); got != want {
			t.Errorf("Side(%d).String() = %q, want %q", uint8(s), got, want)
		}
		parsed, err := ParseSide(want)
		if s.Valid() && (err != nil || parsed != s) {
			t.Errorf("ParseSide(%q) = %v, %v; want %v, nil", want, parsed, err, s)
		}
	}
	if _, err := ParseSide("buy"); !errors.Is(err, ErrInvalidSide) {
		t.Errorf("ParseSide(\"buy\") error = %v, want %v", err, ErrInvalidSide)
	}

	types := map[Type]string{TypeLimit: "LIMIT", TypeMarket: "MARKET", TypeUnspecified: "UNSPECIFIED"}
	for ty, want := range types {
		if got := ty.String(); got != want {
			t.Errorf("Type(%d).String() = %q, want %q", uint8(ty), got, want)
		}
		parsed, err := ParseType(want)
		if ty.Valid() && (err != nil || parsed != ty) {
			t.Errorf("ParseType(%q) = %v, %v; want %v, nil", want, parsed, err, ty)
		}
	}
	if _, err := ParseType("STOP"); !errors.Is(err, ErrInvalidType) {
		t.Errorf("ParseType(\"STOP\") error = %v, want %v", err, ErrInvalidType)
	}

	statuses := map[Status]string{
		StatusNew:             "NEW",
		StatusPartiallyFilled: "PARTIALLY_FILLED",
		StatusFilled:          "FILLED",
		StatusCanceled:        "CANCELED",
		StatusRejected:        "REJECTED",
		StatusUnspecified:     "UNSPECIFIED",
	}
	for st, want := range statuses {
		if got := st.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", uint8(st), got, want)
		}
		parsed, err := ParseStatus(want)
		if st.Valid() && (err != nil || parsed != st) {
			t.Errorf("ParseStatus(%q) = %v, %v; want %v, nil", want, parsed, err, st)
		}
	}
	if _, err := ParseStatus("EXPIRED"); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("ParseStatus(\"EXPIRED\") error = %v, want %v", err, ErrInvalidStatus)
	}
}

func TestSideOpposite(t *testing.T) {
	cases := map[Side]Side{
		SideBuy:         SideSell,
		SideSell:        SideBuy,
		SideUnspecified: SideUnspecified,
	}
	for in, want := range cases {
		if got := in.Opposite(); got != want {
			t.Errorf("Side(%s).Opposite() = %s, want %s", in, got, want)
		}
	}
}

func TestStatusOpenAndTerminal(t *testing.T) {
	open := []Status{StatusNew, StatusPartiallyFilled}
	terminal := []Status{StatusFilled, StatusCanceled, StatusRejected}

	for _, s := range open {
		if !s.Open() || s.Terminal() {
			t.Errorf("%s: Open() = %t, Terminal() = %t; want true, false", s, s.Open(), s.Terminal())
		}
	}
	for _, s := range terminal {
		if s.Open() || !s.Terminal() {
			t.Errorf("%s: Open() = %t, Terminal() = %t; want false, true", s, s.Open(), s.Terminal())
		}
	}
	if StatusUnspecified.Open() || StatusUnspecified.Terminal() {
		t.Error("StatusUnspecified must be neither open nor terminal")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	o := mustNew(t, validParams())
	if err := o.Fill(4, t1); err != nil {
		t.Fatalf("Fill returned error: %v", err)
	}

	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map returned error: %v", err)
	}
	for field, want := range map[string]string{
		"side": "BUY", "type": "LIMIT", "status": "PARTIALLY_FILLED",
	} {
		if raw[field] != want {
			t.Errorf("json field %q = %v, want %q", field, raw[field], want)
		}
	}

	var back Order
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal into Order returned error: %v", err)
	}
	if !back.CreatedAt.Equal(o.CreatedAt) || !back.UpdatedAt.Equal(o.UpdatedAt) {
		t.Errorf("timestamps did not round-trip: got %s/%s, want %s/%s",
			back.CreatedAt, back.UpdatedAt, o.CreatedAt, o.UpdatedAt)
	}
	back.CreatedAt, back.UpdatedAt = o.CreatedAt, o.UpdatedAt
	if back != o {
		t.Errorf("round trip changed the order:\n got %+v\nwant %+v", back, o)
	}
}

func TestJSONRejectsUnknownEnumValues(t *testing.T) {
	cases := map[string]error{
		`{"side":"HOLD"}`:       ErrInvalidSide,
		`{"type":"STOP"}`:       ErrInvalidType,
		`{"status":"PENDING"}`:  ErrInvalidStatus,
		`{"side":"buy"}`:        ErrInvalidSide,
		`{"status":"Canceled"}`: ErrInvalidStatus,
	}
	for input, want := range cases {
		var o Order
		if err := json.Unmarshal([]byte(input), &o); !errors.Is(err, want) {
			t.Errorf("Unmarshal(%s) error = %v, want %v", input, err, want)
		}
	}
}

func TestMarshalRejectsInvalidEnumValues(t *testing.T) {
	var o Order
	if _, err := json.Marshal(o); !errors.Is(err, ErrInvalidSide) {
		t.Errorf("Marshal(zero Order) error = %v, want %v", err, ErrInvalidSide)
	}
}

func TestOrderIsCopiedByValue(t *testing.T) {
	original := mustNew(t, validParams())
	copied := original

	if err := copied.Fill(5, t1); err != nil {
		t.Fatalf("Fill returned error: %v", err)
	}
	if original.Remaining != 10 || original.Status != StatusNew {
		t.Errorf("mutating a copy changed the original: %+v", original)
	}
}

func TestString(t *testing.T) {
	o := mustNew(t, validParams())
	want := "O-1 BUY AAPL 10/10 @ 19900 [NEW]"
	if got := o.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
