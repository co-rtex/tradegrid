// Package exchange joins the order model, matching engine, and execution
// mapper behind one serialized service. It owns the book, the canonical
// orders, and the execution history; nothing else changes them.
//
// Every submission is built on a candidate copy and published only after
// every step succeeds, so a failure at any point leaves the published state
// exactly as it was. One mutex covers submissions and reads, so the order in
// which submissions take the lock is their acceptance order.
//
// The service is internal: HTTP (#36, #37) calls it, and #35 adds accounts at
// the join marked in candidate.
package exchange

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/executions"
	"github.com/co-rtex/tradegrid/backend/internal/instruments"
	"github.com/co-rtex/tradegrid/backend/internal/matching"
	"github.com/co-rtex/tradegrid/backend/internal/orders"
)

// Read limits from docs/api.md. A zero limit or depth means the default.
const (
	DefaultLimit = 50
	MaxLimit     = 200
	DefaultDepth = 20
	MaxDepth     = 100
)

// Config supplies the service's clock and ID sources. Tests inject
// deterministic ones; zero fields get the defaults from New.
type Config struct {
	// Now returns the time of a submission. It is called once per submission
	// and converted to UTC.
	Now func() time.Time
	// OrderID and ExecutionID turn a sequence number into an ID.
	OrderID     func(seq int64) string
	ExecutionID func(seq int64) string
}

// SubmitRequest carries only client-owned fields. Side and Type are wire
// strings such as "BUY" and "LIMIT".
type SubmitRequest struct {
	UserID   string
	Symbol   string
	Side     string
	Type     string
	Price    int64
	Quantity int64
}

// Order is an accepted order with its acceptance sequence.
type Order struct {
	orders.Order
	Sequence int64 `json:"sequence"`
}

// SubmitResult is the incoming order after matching and the executions it
// produced, in matching order.
type SubmitResult struct {
	Order      Order               `json:"order"`
	Executions []executions.Record `json:"executions"`
}

// Level is the resting quantity at one price.
type Level struct {
	Price      int64 `json:"price"`
	Quantity   int64 `json:"quantity"`
	OrderCount int   `json:"orderCount"`
}

// BookSnapshot is one symbol's book aggregated by price: bids highest first,
// asks lowest first. AsOfSequence is the last accepted order's sequence.
type BookSnapshot struct {
	Symbol       string  `json:"symbol"`
	AsOfSequence int64   `json:"asOfSequence"`
	Bids         []Level `json:"bids"`
	Asks         []Level `json:"asks"`
}

// Service is the in-memory exchange. Create one with New; it is safe for
// concurrent use.
type Service struct {
	mu       sync.Mutex
	cfg      Config
	book     *matching.Book
	orders   map[string]Order
	history  []executions.Record
	orderSeq int64
	execSeq  int64
}

// New returns an empty exchange.
func New(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.OrderID == nil {
		cfg.OrderID = func(seq int64) string { return fmt.Sprintf("ord-%d", seq) }
	}
	if cfg.ExecutionID == nil {
		cfg.ExecutionID = func(seq int64) string { return fmt.Sprintf("exe-%d", seq) }
	}
	return &Service{cfg: cfg, book: matching.NewBook(), orders: make(map[string]Order)}
}

// candidate is the state one submission builds before it is published.
//
// #35 joins here: it adds a cloned accounts.Ledger, calls Reserve in reserve
// and Settle in settle, and publishes the ledger with the rest.
type candidate struct {
	book    *matching.Book
	changed map[string]Order // incoming and every resting order it filled
	records []executions.Record
}

func (c *candidate) reserve(orders.Order) error { return nil }

func (c *candidate) settle() error { return nil }

// Submit accepts one LIMIT order, matches it, and publishes the new book,
// orders, and executions together. A rejected request returns an *Error with
// a request code and publishes nothing; any later failure returns CodeInternal
// and also publishes nothing.
func (s *Service) Submit(req SubmitRequest) (SubmitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	side, err := checkRequest(req)
	if err != nil {
		return SubmitResult{}, err
	}
	seq := s.orderSeq + 1
	now := s.cfg.Now().UTC()
	o, err := orders.New(orders.Params{
		ID: s.cfg.OrderID(seq), UserID: req.UserID, Symbol: req.Symbol, Side: side,
		Type: orders.TypeLimit, Price: req.Price, Quantity: req.Quantity, CreatedAt: now,
	})
	if err != nil {
		return SubmitResult{}, internal(err)
	}
	if _, dup := s.orders[o.ID]; dup {
		return SubmitResult{}, internal(fmt.Errorf("duplicate order id %q", o.ID))
	}

	c := &candidate{changed: map[string]Order{o.ID: {Order: o, Sequence: seq}}}
	if err := c.reserve(o); err != nil {
		return SubmitResult{}, err
	}
	in, err := matching.FromOrder(o)
	if err != nil {
		return SubmitResult{}, internal(err)
	}
	c.book = s.book.Clone()
	res := c.book.Submit(in)
	if res.Err != nil {
		if errors.Is(res.Err, matching.ErrInvalidQuantity) {
			return SubmitResult{}, newError(CodeInvalidQuantity, "quantity", res.Err)
		}
		return SubmitResult{}, internal(res.Err)
	}

	resting := make(map[string]orders.Order, len(res.Executions))
	meta := make([]executions.Metadata, len(res.Executions))
	for i, f := range res.Executions {
		if r, ok := s.orders[f.RestingID]; ok {
			resting[f.RestingID] = r.Order
		}
		n := s.execSeq + int64(i) + 1
		meta[i] = executions.Metadata{ID: s.cfg.ExecutionID(n), Sequence: n, ExecutedAt: now}
	}
	c.records, err = executions.Map(o, resting, res.Executions, meta)
	if err != nil {
		return SubmitResult{}, internal(err)
	}
	for _, f := range res.Executions {
		for _, id := range []string{f.IncomingID, f.RestingID} {
			ord, ok := c.changed[id]
			if !ok {
				ord = s.orders[id]
			}
			if err := ord.Fill(f.Quantity, now); err != nil {
				return SubmitResult{}, internal(fmt.Errorf("fill %s: %w", id, err))
			}
			c.changed[id] = ord
		}
	}
	if got := c.changed[o.ID].Remaining; got != res.Remaining {
		return SubmitResult{}, internal(fmt.Errorf("order %s has %d remaining, matcher left %d", o.ID, got, res.Remaining))
	}
	if err := c.settle(); err != nil {
		return SubmitResult{}, err
	}

	// Publish. Nothing below can fail.
	s.book = c.book
	for id, ord := range c.changed {
		s.orders[id] = ord
	}
	s.history = append(slices.Clip(s.history), c.records...)
	s.orderSeq = seq
	s.execSeq += int64(len(c.records))
	return SubmitResult{Order: c.changed[o.ID], Executions: slices.Clone(c.records)}, nil
}

// checkRequest applies request rules in the order docs/api.md lists them.
func checkRequest(req SubmitRequest) (orders.Side, error) {
	for _, f := range []struct{ name, value string }{
		{"userId", req.UserID}, {"symbol", req.Symbol}, {"side", req.Side}, {"type", req.Type},
	} {
		if f.value == "" {
			return 0, newError(CodeMissingField, f.name, fmt.Errorf("%s is required", f.name))
		}
	}
	if !instruments.Supported(req.Symbol) {
		return 0, newError(CodeUnknownSymbol, "symbol", fmt.Errorf("unsupported symbol %q", req.Symbol))
	}
	side, err := orders.ParseSide(req.Side)
	if err != nil {
		return 0, newError(CodeInvalidSide, "side", err)
	}
	if typ, err := orders.ParseType(req.Type); err != nil || typ != orders.TypeLimit {
		return 0, newError(CodeUnsupportedOrderType, "type", fmt.Errorf("only LIMIT orders are supported, got %q", req.Type))
	}
	// Check price first as required by the API contract. A valid placeholder
	// quantity lets the domain model perform its existing price validation.
	params := orders.Params{ID: "validation", UserID: req.UserID, Symbol: req.Symbol,
		Side: side, Type: orders.TypeLimit, Price: req.Price, Quantity: 1, CreatedAt: time.Unix(1, 0).UTC()}
	if _, err := orders.New(params); err != nil {
		return 0, newError(CodeInvalidPrice, "price", err)
	}
	if req.Price > MaxPrice {
		return 0, newError(CodeInvalidPrice, "price", fmt.Errorf("price must be between 1 and %d cents", MaxPrice))
	}
	params.Quantity = req.Quantity
	if _, err := orders.New(params); err != nil {
		return 0, newError(CodeInvalidQuantity, "quantity", err)
	}
	if req.Quantity > MaxQuantity {
		return 0, newError(CodeInvalidQuantity, "quantity", fmt.Errorf("quantity must be between 1 and %d shares", MaxQuantity))
	}
	return side, nil
}

func internal(err error) *Error { return newError(CodeInternal, "", err) }

// Orders returns a user's accepted orders in every status, newest first.
func (s *Service) Orders(userID string, limit int) ([]Order, error) {
	if userID == "" {
		return nil, newError(CodeInvalidQuery, "userId", errors.New("userId is required"))
	}
	limit, err := bound(limit, DefaultLimit, MaxLimit, "limit")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []Order{}
	for _, o := range s.orders {
		if o.UserID == userID {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b Order) int { return cmp.Compare(b.Sequence, a.Sequence) })
	return out[:min(len(out), limit)], nil
}

// Executions returns a symbol's executions, newest first. A non-empty userID
// keeps only executions where that user is buyer or seller.
func (s *Service) Executions(symbol, userID string, limit int) ([]executions.Record, error) {
	if symbol == "" {
		return nil, newError(CodeInvalidQuery, "symbol", errors.New("symbol is required"))
	}
	if !instruments.Supported(symbol) {
		return nil, newError(CodeUnknownSymbol, "symbol", fmt.Errorf("unsupported symbol %q", symbol))
	}
	limit, err := bound(limit, DefaultLimit, MaxLimit, "limit")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []executions.Record{}
	for i := len(s.history) - 1; i >= 0 && len(out) < limit; i-- {
		r := s.history[i]
		if r.Symbol == symbol && (userID == "" || r.BuyerUserID == userID || r.SellerUserID == userID) {
			out = append(out, r)
		}
	}
	return out, nil
}

// OrderBook returns a symbol's resting quantity aggregated by price, at most
// depth levels per side.
func (s *Service) OrderBook(symbol string, depth int) (BookSnapshot, error) {
	if symbol == "" {
		return BookSnapshot{}, newError(CodeInvalidQuery, "symbol", errors.New("symbol is required"))
	}
	if !instruments.Supported(symbol) {
		return BookSnapshot{}, newError(CodeUnknownSymbol, "symbol", fmt.Errorf("unsupported symbol %q", symbol))
	}
	depth, err := bound(depth, DefaultDepth, MaxDepth, "depth")
	if err != nil {
		return BookSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return BookSnapshot{
		Symbol:       symbol,
		AsOfSequence: s.orderSeq,
		Bids:         levels(s.book.Bids(symbol), depth),
		Asks:         levels(s.book.Asks(symbol), depth),
	}, nil
}

// levels aggregates entries that are already sorted best price first.
func levels(entries []matching.BookEntry, depth int) []Level {
	out := []Level{}
	for _, e := range entries {
		if n := len(out); n > 0 && out[n-1].Price == e.Price {
			out[n-1].Quantity += e.Remaining
			out[n-1].OrderCount++
			continue
		}
		if len(out) == depth {
			break
		}
		out = append(out, Level{Price: e.Price, Quantity: e.Remaining, OrderCount: 1})
	}
	return out
}

func bound(v, def, hi int, field string) (int, error) {
	switch {
	case v == 0:
		return def, nil
	case v < 0 || v > hi:
		return 0, newError(CodeInvalidQuery, field, fmt.Errorf("%s must be between 1 and %d, got %d", field, hi, v))
	}
	return v, nil
}
