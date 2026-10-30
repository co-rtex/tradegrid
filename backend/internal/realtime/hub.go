// Package realtime fans committed exchange changes out to in-process
// subscribers, following docs/realtime.md. It knows nothing about HTTP or
// WebSockets: the stream endpoint (#73) subscribes here and turns each Event
// into an exchange.changed message.
//
// Events are invalidation notices, not data. A subscriber that misses one
// re-fetches the REST snapshots, so the hub drops a subscriber whose queue is
// full instead of ever blocking the publisher.
package realtime

import (
	"errors"
	"fmt"
	"slices"
	"sync"
)

// DefaultQueueSize is the per-subscriber queue length from docs/realtime.md.
const DefaultQueueSize = 64

var (
	// ErrSlowConsumer means the subscriber's queue was full when an event was
	// published, so the hub dropped it (close code 1013 in the contract).
	ErrSlowConsumer = errors.New("realtime: subscriber queue full")
	// ErrClosed means the hub was closed (server shutdown, close code 1001).
	ErrClosed = errors.New("realtime: hub closed")
	// ErrOutOfOrder means Publish got a sequence that is not larger than the
	// last one published. The event is discarded.
	ErrOutOfOrder = errors.New("realtime: event sequence out of order")
)

// Event is one committed submission. Sequence is the accepted order's
// acceptance sequence; the other fields are the exchange.changed data.
// Every subscriber receives its own copy, so changing one has no effect on
// the others.
type Event struct {
	Sequence        int64    `json:"-"`
	Symbol          string   `json:"symbol"`
	OrderID         string   `json:"orderId"`
	UserID          string   `json:"userId"`
	ExecutionCount  int      `json:"executionCount"`
	AffectedUserIDs []string `json:"affectedUserIds"`
}

// Hub is a set of subscribers with bounded queues. Create one with New; it is
// safe for concurrent use. Publish never blocks.
type Hub struct {
	mu     sync.Mutex
	size   int
	seq    int64 // last published sequence
	subs   map[*Subscription]struct{}
	closed bool
}

// New returns a hub whose subscribers each queue at most size events. A size
// of zero or less means DefaultQueueSize.
func New(size int) *Hub {
	if size <= 0 {
		size = DefaultQueueSize
	}
	return &Hub{size: size, subs: make(map[*Subscription]struct{})}
}

// Subscription is one subscriber's queue. Read events from Events until the
// channel is closed, then call Err to learn why.
type Subscription struct {
	hub *Hub
	ch  chan Event
	err error // set under hub.mu when ch is closed
}

// Subscribe registers a subscriber and returns it with the last published
// sequence. The two are read together, so the subscriber receives every
// event with a larger sequence and none with a smaller one: this is the
// hello asOfSequence. Subscribing to a closed hub returns a subscription
// that is already closed with ErrClosed.
func (h *Hub) Subscribe() (*Subscription, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := &Subscription{hub: h, ch: make(chan Event, h.size)}
	if h.closed {
		h.drop(s, ErrClosed)
		return s, h.seq
	}
	h.subs[s] = struct{}{}
	return s, h.seq
}

// Publish queues e for every subscriber without blocking. A subscriber whose
// queue is full is dropped with ErrSlowConsumer; the others still receive
// e. Sequences must increase: an event whose sequence is not larger than the
// last published one is discarded with ErrOutOfOrder. Publishing to a closed
// hub does nothing.
func (h *Hub) Publish(e Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}
	if e.Sequence <= h.seq {
		return fmt.Errorf("%w: got %d after %d", ErrOutOfOrder, e.Sequence, h.seq)
	}
	h.seq = e.Sequence
	for s := range h.subs {
		c := e
		c.AffectedUserIDs = slices.Clone(e.AffectedUserIDs)
		select {
		case s.ch <- c:
		default:
			delete(h.subs, s)
			h.drop(s, ErrSlowConsumer)
		}
	}
	return nil
}

// Sequence returns the last published sequence, for heartbeats.
func (h *Hub) Sequence() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// Len returns the number of active subscribers.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Close drops every subscriber with ErrClosed. Later calls do nothing.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	h.closed = true
	for s := range h.subs {
		h.drop(s, ErrClosed)
	}
	clear(h.subs)
}

// drop closes s's channel and records why. The caller holds h.mu and has
// already removed s from h.subs. Events already queued stay readable.
func (h *Hub) drop(s *Subscription, err error) {
	s.err = err
	close(s.ch)
}

// Events returns the subscriber's queue. It is closed when the subscription
// ends, after any events that were already queued.
func (s *Subscription) Events() <-chan Event { return s.ch }

// Err returns nil while the subscription is active or after Unsubscribe,
// ErrSlowConsumer if the hub dropped it, or ErrClosed if the hub closed.
func (s *Subscription) Err() error {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	return s.err
}

// Unsubscribe removes the subscriber and closes its channel. It is safe to
// call more than once and after the hub dropped or closed it.
func (s *Subscription) Unsubscribe() {
	h := s.hub
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	h.drop(s, nil)
}
