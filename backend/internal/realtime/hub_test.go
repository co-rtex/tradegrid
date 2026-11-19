package realtime

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func event(seq int64) Event {
	return Event{Sequence: seq, Symbol: "AAPL", OrderID: "ord", UserID: "alice", AffectedUserIDs: []string{"alice", "bob"}}
}

// drain reads every queued event from a subscription that is already closed.
func drain(t *testing.T, s *Subscription) []int64 {
	t.Helper()
	var seqs []int64
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return seqs
			}
			seqs = append(seqs, e.Sequence)
		case <-time.After(time.Second):
			t.Fatal("subscription channel was not closed")
		}
	}
}

func TestSubscribersReceiveEventsInOrder(t *testing.T) {
	h := New(0)
	a, seqA := h.Subscribe()
	if err := h.Publish(event(1)); err != nil {
		t.Fatal(err)
	}
	b, seqB := h.Subscribe()
	for _, seq := range []int64{2, 3} {
		if err := h.Publish(event(seq)); err != nil {
			t.Fatal(err)
		}
	}
	if seqA != 0 || seqB != 1 || h.Sequence() != 3 {
		t.Fatalf("subscribe sequences = %d, %d; hub sequence = %d", seqA, seqB, h.Sequence())
	}
	a.Unsubscribe()
	b.Unsubscribe()
	// Each subscriber sees exactly the events after its subscribe sequence.
	if got := drain(t, a); !slices.Equal(got, []int64{1, 2, 3}) {
		t.Errorf("a got %v", got)
	}
	if got := drain(t, b); !slices.Equal(got, []int64{2, 3}) {
		t.Errorf("b got %v", got)
	}
	if a.Err() != nil || b.Err() != nil {
		t.Errorf("Err after Unsubscribe = %v, %v; want nil", a.Err(), b.Err())
	}
}

func TestOutOfOrderPublishIsDiscarded(t *testing.T) {
	h := New(0)
	s, _ := h.Subscribe()
	if err := h.Publish(event(2)); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int64{2, 1, 0} {
		if err := h.Publish(event(seq)); !errors.Is(err, ErrOutOfOrder) {
			t.Errorf("Publish(%d) = %v, want ErrOutOfOrder", seq, err)
		}
	}
	s.Unsubscribe()
	if got := drain(t, s); !slices.Equal(got, []int64{2}) || h.Sequence() != 2 {
		t.Errorf("got %v, hub sequence %d", got, h.Sequence())
	}
}

func TestEventsAreCopies(t *testing.T) {
	h := New(0)
	a, _ := h.Subscribe()
	b, _ := h.Subscribe()
	e := event(1)
	if err := h.Publish(e); err != nil {
		t.Fatal(err)
	}
	e.AffectedUserIDs[0] = "publisher"
	got := <-a.Events()
	got.AffectedUserIDs[0] = "a"
	if other := <-b.Events(); !slices.Equal(other.AffectedUserIDs, []string{"alice", "bob"}) {
		t.Errorf("b got %v, want an unshared copy", other.AffectedUserIDs)
	}
}

func TestSlowConsumerIsDroppedWithoutBlocking(t *testing.T) {
	h := New(2)
	slow, _ := h.Subscribe()
	fast, _ := h.Subscribe()
	var fastGot []int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range fast.Events() {
			fastGot = append(fastGot, e.Sequence)
		}
	}()

	// Nothing reads slow, so its queue fills after two events. Publish must
	// still return promptly every time.
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for seq := int64(1); seq <= 10; seq++ {
			if err := h.Publish(event(seq)); err != nil {
				t.Error(err)
			}
			// Let the fast reader keep up so only slow overflows.
			for len(fast.ch) > 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	if got := drain(t, slow); !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("slow got %v, want the two queued events", got)
	}
	if !errors.Is(slow.Err(), ErrSlowConsumer) {
		t.Errorf("slow.Err() = %v, want ErrSlowConsumer", slow.Err())
	}
	if h.Len() != 1 {
		t.Errorf("Len = %d, want only the fast subscriber", h.Len())
	}
	fast.Unsubscribe()
	<-done
	if want := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}; !slices.Equal(fastGot, want) {
		t.Errorf("fast got %v, want %v", fastGot, want)
	}
	slow.Unsubscribe() // already dropped: must not panic or change Err
	if !errors.Is(slow.Err(), ErrSlowConsumer) {
		t.Errorf("Err after late Unsubscribe = %v", slow.Err())
	}
}

func TestUnsubscribeFreesSubscriber(t *testing.T) {
	h := New(0)
	s, _ := h.Subscribe()
	if h.Len() != 1 {
		t.Fatalf("Len = %d", h.Len())
	}
	s.Unsubscribe()
	s.Unsubscribe()
	if h.Len() != 0 {
		t.Errorf("Len after Unsubscribe = %d", h.Len())
	}
	if err := h.Publish(event(1)); err != nil {
		t.Fatal(err)
	}
	if got := drain(t, s); len(got) != 0 {
		t.Errorf("unsubscribed subscriber got %v", got)
	}
}

func TestCloseReleasesEverySubscriber(t *testing.T) {
	h := New(0)
	subs := []*Subscription{}
	for range 3 {
		s, _ := h.Subscribe()
		subs = append(subs, s)
	}
	if err := h.Publish(event(1)); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h.Close()
	for i, s := range subs {
		if got := drain(t, s); !slices.Equal(got, []int64{1}) || !errors.Is(s.Err(), ErrClosed) {
			t.Errorf("subscriber %d got %v, Err %v", i, got, s.Err())
		}
		s.Unsubscribe()
	}
	if h.Len() != 0 {
		t.Errorf("Len after Close = %d", h.Len())
	}

	// After Close, Publish does nothing and Subscribe returns a closed one.
	if err := h.Publish(event(2)); err != nil {
		t.Errorf("Publish after Close = %v", err)
	}
	late, seq := h.Subscribe()
	if got := drain(t, late); len(got) != 0 || seq != 1 || !errors.Is(late.Err(), ErrClosed) || h.Len() != 0 {
		t.Errorf("late subscriber got %v, seq %d, Err %v, Len %d", got, seq, late.Err(), h.Len())
	}
}

// TestConcurrentSubscribeAndPublish runs with -race in CI. Every subscriber,
// whenever it joins, must see consecutive sequences starting right after its
// subscribe sequence, until it leaves or is dropped.
func TestConcurrentSubscribeAndPublish(t *testing.T) {
	const events, subscribers = 500, 20
	h := New(8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for seq := int64(1); seq <= events; seq++ {
			if err := h.Publish(event(seq)); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := range subscribers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, next := h.Subscribe()
			defer s.Unsubscribe()
			if next == events {
				return
			}
			// Odd subscribers leave early; even ones read until the end or
			// until they fall behind and are dropped.
			for e := range s.Events() {
				next++
				if e.Sequence != next {
					t.Errorf("subscriber %d got %d, want %d", i, e.Sequence, next)
					return
				}
				if e.Sequence == events || (i%2 == 1 && e.Sequence%50 == 0) {
					return
				}
			}
			if err := s.Err(); err != nil && !errors.Is(err, ErrSlowConsumer) {
				t.Errorf("subscriber %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if h.Len() != 0 {
		t.Errorf("Len after every subscriber left = %d", h.Len())
	}
	h.Close()
}
