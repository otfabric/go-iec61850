// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// Lifecycle contract tests (TESTING.md, "Lifecycle contract matrix").

// lifecycleFrame is a GOOSE frame of LD/LLN0$GO$gcb1 with state number
// stNum.
func lifecycleFrame(t *testing.T, stNum uint32) []byte {
	t.Helper()
	return mustEncode(t, &Frame{
		Dst: testDst, Src: MAC{2, 0, 0, 0, 0, 9}, AppID: 0x1000,
		PDU: PDU{GocbRef: "LD/LLN0$GO$gcb1", DatSet: "LD/LLN0$ds1", GoID: "gcb1", TimeAllowedToLive: 60000,
			T: time.Unix(0, 0), StNum: stNum, ConfRev: 1, AllData: []*mms.Value{mms.NewBoolean(true)}},
	})
}

// L1: a second Close of a Subscriber, a Subscription and a Publisher
// returns nil and changes nothing; what is used after Close fails with
// ErrClosed.
func TestLifecycle_CloseTwice(t *testing.T) {
	hub := NewHub()
	s, err := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Subscribe(SubscriptionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := sub.Close(); err != nil {
			t.Fatalf("Subscription.Close #%d: %v", i+1, err)
		}
	}
	if _, ok := <-sub.Events(); ok {
		t.Fatal("Events not closed after Close")
	}
	for i := range 2 {
		if err := s.Close(); err != nil {
			t.Fatalf("Subscriber.Close #%d: %v", i+1, err)
		}
	}
	if _, err := s.Subscribe(SubscriptionConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe after Close: %v, want ErrClosed", err)
	}

	pub, err := NewPublisher(hub.NewPort(MAC{}), pubConfig())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := pub.Close(); err != nil {
			t.Fatalf("Publisher.Close #%d: %v", i+1, err)
		}
	}
	if err := pub.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewInteger(2)}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish after Close: %v, want ErrClosed", err)
	}
	if err := pub.Run(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after Close: %v, want ErrClosed", err)
	}
}

// L2: Close called from two goroutines at once on a Subscriber, its
// subscriptions (one with a Handler) and a Publisher: every call returns
// nil, the event channels are closed once, and no goroutine of the
// bubble is left (synctest).
func TestLifecycle_ConcurrentClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		s, err := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		plain, err := s.Subscribe(SubscriptionConfig{})
		if err != nil {
			t.Fatal(err)
		}
		var handled atomic.Int32
		withHandler, err := s.Subscribe(SubscriptionConfig{Handler: func(Event) { handled.Add(1) }})
		if err != nil {
			t.Fatal(err)
		}
		pub, err := NewPublisher(hub.NewPort(MAC{}), pubConfig())
		if err != nil {
			t.Fatal(err)
		}
		s.HandleFrame(lifecycleFrame(t, 1))
		synctest.Wait()

		closers := []func() error{s.Close, plain.Close, withHandler.Close, pub.Close}
		errs := make(chan error, 2*len(closers))
		var wg sync.WaitGroup
		for range 2 {
			for _, c := range closers {
				wg.Go(func() { errs <- c() })
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent Close: %v", err)
			}
		}
		for _, sub := range []*Subscription{plain, withHandler} {
			for range sub.Events() {
			}
		}
		if handled.Load() == 0 {
			t.Fatal("the Handler was not called before Close")
		}
	})
}

// recordTransport keeps a copy of every frame sent.
type recordTransport struct {
	Transport
	mu     sync.Mutex
	frames [][]byte
}

func (r *recordTransport) Send(b []byte) error {
	r.mu.Lock()
	r.frames = append(r.frames, append([]byte(nil), b...))
	r.mu.Unlock()
	return nil
}

func (r *recordTransport) sent() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.frames...)
}

// L4: Close while Run retransmits: Run returns nil, every frame sent is
// complete, and nothing is sent after Close.
func TestLifecycle_PublisherCloseDuringRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &recordTransport{Transport: NewHub().NewPort(MAC{})}
		pub, err := NewPublisher(tr, pubConfig())
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- pub.Run(context.Background()) }()
		time.Sleep(200 * time.Millisecond) // virtual time: the retransmission curve reaches MaxTime
		synctest.Wait()
		if err := pub.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("Run after Close: %v", err)
		}
		before := tr.sent()
		if len(before) < 2 {
			t.Fatalf("%d frames before Close, want the initial state and retransmissions", len(before))
		}
		for i, raw := range before {
			if _, err := Decode(raw); err != nil {
				t.Fatalf("frame %d: %v", i, err)
			}
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if after := tr.sent(); len(after) != len(before) {
			t.Fatalf("%d frames sent after Close", len(after)-len(before))
		}
		_ = tr.Close()
	})
}

// L9: a Handler may call the methods of its own subscription and of its
// subscriber: Supervision and Counters return the state that includes
// the event, and Subscribe adds a subscription that receives the next
// frame.
func TestLifecycle_HandlerCallsPublicMethods(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		var (
			sub   *Subscription
			ready = make(chan struct{})
			added = make(chan *Subscription, 1)
			mu    sync.Mutex
			msgs  []uint64
		)
		sub, err = s.Subscribe(SubscriptionConfig{Handler: func(ev Event) {
			<-ready
			if ev.Kind != EventMessage {
				return
			}
			sup := sub.Supervision()
			if len(sup) != 1 {
				t.Errorf("Supervision from the Handler: %d streams", len(sup))
				return
			}
			mu.Lock()
			msgs = append(msgs, sup[0].Messages)
			n := len(msgs)
			mu.Unlock()
			_ = sub.Counters()
			if n == 1 {
				other, err := s.Subscribe(SubscriptionConfig{})
				if err != nil {
					t.Errorf("Subscribe from the Handler: %v", err)
				}
				added <- other
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		close(ready)
		s.HandleFrame(lifecycleFrame(t, 1))
		synctest.Wait()
		other := <-added
		s.HandleFrame(lifecycleFrame(t, 2))
		synctest.Wait()
		mu.Lock()
		defer mu.Unlock()
		if len(msgs) != 2 || msgs[0] < 1 || msgs[1] <= msgs[0] {
			t.Fatalf("Messages seen from the Handler: %v", msgs)
		}
		if got := other.Supervision(); len(got) != 1 || got[0].LastStNum != 2 {
			t.Fatalf("the subscription added by the Handler: %+v", got)
		}
	})
}

// L11: a Handler that does not return holds up its own subscription
// only: HandleFrame returns, another subscription's Handler receives
// every frame, and the blocked subscription's queue drops what does not
// fit and counts it.
func TestLifecycle_SlowHandlerHoldsOnlyItsSubscription(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		slow, err := s.Subscribe(SubscriptionConfig{QueueSize: 1, Handler: func(Event) { <-release }})
		if err != nil {
			t.Fatal(err)
		}
		var fast atomic.Int32
		if _, err := s.Subscribe(SubscriptionConfig{QueueSize: 64, Handler: func(ev Event) {
			if ev.Kind == EventMessage {
				fast.Add(1)
			}
		}}); err != nil {
			t.Fatal(err)
		}
		const frames = 5
		for st := range uint32(frames) {
			s.HandleFrame(lifecycleFrame(t, st+1))
			synctest.Wait()
		}
		if got := fast.Load(); got != frames {
			t.Fatalf("the other subscription's Handler got %d messages, want %d", got, frames)
		}
		sup := slow.Supervision()
		if len(sup) != 1 || sup[0].Messages != frames || sup[0].DroppedEvents == 0 {
			t.Fatalf("blocked subscription: %+v, want %d messages and dropped events", sup, frames)
		}
		close(release)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
