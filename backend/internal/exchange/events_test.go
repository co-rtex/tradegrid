package exchange

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/realtime"
)

func newHubService(size int) (*Service, *realtime.Hub) {
	hub := realtime.New(size)
	s := newTestService()
	s.cfg.Events = hub
	return s, hub
}

// next returns the next queued event, or fails if none arrives.
func next(t *testing.T, sub *realtime.Subscription) realtime.Event {
	t.Helper()
	select {
	case e, ok := <-sub.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", sub.Err())
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	return realtime.Event{}
}

func noEvent(t *testing.T, sub *realtime.Subscription) {
	t.Helper()
	select {
	case e := <-sub.Events():
		t.Fatalf("unexpected event %+v", e)
	default:
	}
}

func TestCommittedSubmissionsPublishEvents(t *testing.T) {
	s, hub := newHubService(0)
	sub, seq := hub.Subscribe()
	if seq != 0 {
		t.Fatalf("hello sequence = %d, want 0", seq)
	}

	// The two samples in docs/realtime.md (fixture 03-partial-fill).
	mustSubmit(t, s, limit("alice", "SELL", 10, 18500))
	mustSubmit(t, s, limit("bob", "BUY", 4, 18500))
	for _, want := range []realtime.Event{
		{Sequence: 1, Symbol: "AAPL", OrderID: "ord-1", UserID: "alice", ExecutionCount: 0, AffectedUserIDs: []string{"alice"}},
		{Sequence: 2, Symbol: "AAPL", OrderID: "ord-2", UserID: "bob", ExecutionCount: 1, AffectedUserIDs: []string{"alice", "bob"}},
	} {
		if got := next(t, sub); !reflect.DeepEqual(got, want) {
			t.Errorf("event = %+v, want %+v", got, want)
		}
	}
	if hub.Sequence() != 2 {
		t.Errorf("hub sequence = %d, want 2", hub.Sequence())
	}
}

func TestSameAccountTradeAffectsOneUser(t *testing.T) {
	s, hub := newHubService(0)
	sub, _ := hub.Subscribe()
	mustSubmit(t, s, limit("alice", "SELL", 3, 18500))
	mustSubmit(t, s, limit("alice", "BUY", 3, 18500))
	next(t, sub)
	if e := next(t, sub); e.ExecutionCount != 1 || !reflect.DeepEqual(e.AffectedUserIDs, []string{"alice"}) {
		t.Errorf("event = %+v, want one execution affecting only alice", e)
	}
}

func TestRejectedSubmissionsPublishNothing(t *testing.T) {
	s, hub := newHubService(0)
	sub, _ := hub.Subscribe()
	mustSubmit(t, s, limit("alice", "SELL", 100, 18500)) // reserves all of alice's AAPL
	next(t, sub)

	for name, submit := range map[string]func() error{
		"unknown user":       func() error { _, err := s.Submit(limit("carol", "BUY", 1, 100)); return err },
		"missing field":      func() error { _, err := s.Submit(SubmitRequest{UserID: "alice"}); return err },
		"invalid price":      func() error { _, err := s.Submit(limit("alice", "BUY", 1, 0)); return err },
		"insufficient cash":  func() error { _, err := s.Submit(limit("bob", "BUY", 1000, 100000)); return err },
		"insufficient share": func() error { _, err := s.Submit(limit("alice", "SELL", 1, 18500)); return err },
	} {
		if err := submit(); err == nil {
			t.Errorf("%s: Submit succeeded", name)
		}
	}
	noEvent(t, sub)
	if hub.Sequence() != 1 {
		t.Errorf("hub sequence = %d, want 1", hub.Sequence())
	}
}

func TestInternalFailurePublishesNoEvent(t *testing.T) {
	hub := realtime.New(0)
	s := New(Config{OrderID: func(int64) string { return "same" }, Events: hub})
	sub, _ := hub.Subscribe()
	mustSubmit(t, s, limit("bob", "BUY", 1, 100))
	next(t, sub)
	// The second order reuses the ID, so the exchange rolls it back.
	if _, err := s.Submit(limit("bob", "BUY", 1, 100)); err == nil {
		t.Fatal("duplicate order ID accepted")
	}
	noEvent(t, sub)
}

// TestEventFollowsCommit checks that a subscriber reading the exchange as soon
// as it receives an event sees the committed state.
func TestEventFollowsCommit(t *testing.T) {
	s, hub := newHubService(0)
	sub, _ := hub.Subscribe()
	const n = 20
	done := make(chan error, 1)
	go func() {
		for range n {
			e := <-sub.Events()
			book, err := s.OrderBook(e.Symbol, 0)
			if err != nil {
				done <- err
				return
			}
			orders := must(s.Orders(e.UserID, MaxLimit))
			found := false
			for _, o := range orders {
				found = found || o.ID == e.OrderID
			}
			if book.AsOfSequence < e.Sequence || !found {
				done <- errors.New("event visible before its submission was committed")
				return
			}
		}
		done <- nil
	}()
	for i := range n {
		mustSubmit(t, s, limit("alice", "BUY", 1, int64(100+i)))
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStalledSubscriberDoesNotBlockTrading(t *testing.T) {
	s, hub := newHubService(1)
	stalled, _ := hub.Subscribe() // never read
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := range 10 {
			mustSubmit(t, s, limit("bob", "BUY", 1, int64(100+i)))
		}
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("submissions stalled behind a subscriber that never reads")
	}
	if !errors.Is(stalled.Err(), realtime.ErrSlowConsumer) || hub.Len() != 0 {
		t.Errorf("stalled subscriber Err = %v, Len = %d; want it dropped", stalled.Err(), hub.Len())
	}
}

// TestConcurrentSubmissionsPublishInAcceptanceOrder runs with -race in CI.
// Every accepted submission produces exactly one event, in sequence order,
// and a subscriber joining mid-trading sees every sequence after the one
// Subscribe returned.
func TestConcurrentSubmissionsPublishInAcceptanceOrder(t *testing.T) {
	const workers, each = 8, 25
	s, hub := newHubService(workers * each) // room for every event
	sub, _ := hub.Subscribe()

	var trading, joining sync.WaitGroup
	tradingDone := make(chan struct{})
	for w := range workers {
		trading.Add(1)
		go func() {
			defer trading.Done()
			user, side := "alice", "SELL"
			if w%2 == 0 {
				user, side = "bob", "BUY"
			}
			for i := range each {
				// Errors are risk rejections, which must publish nothing.
				_, _ = s.Submit(limit(user, side, 1, int64(18490+i%20)))
			}
		}()
		joining.Add(1)
		go func() {
			defer joining.Done()
			late, seq := hub.Subscribe()
			<-tradingDone
			late.Unsubscribe()
			for e := range late.Events() {
				seq++
				if e.Sequence != seq {
					t.Errorf("late subscriber got %d, want %d", e.Sequence, seq)
					return
				}
			}
			if seq != s.orderSeq {
				t.Errorf("late subscriber stopped at %d, want %d", seq, s.orderSeq)
			}
		}()
	}
	trading.Wait()
	close(tradingDone)
	joining.Wait()

	sub.Unsubscribe()
	got := []int64{}
	for e := range sub.Events() {
		got = append(got, e.Sequence)
	}
	if int64(len(got)) != s.orderSeq || s.orderSeq == 0 {
		t.Fatalf("got %d events, want %d accepted submissions", len(got), s.orderSeq)
	}
	for i, seq := range got {
		if seq != int64(i+1) {
			t.Fatalf("event %d has sequence %d, want %d", i, seq, i+1)
		}
	}
	if hub.Len() != 0 {
		t.Errorf("Len = %d after every subscriber left", hub.Len())
	}
}
