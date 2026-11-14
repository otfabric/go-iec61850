// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Lifecycle contract tests (TESTING.md, "Lifecycle contract matrix").

// L1: a second Close of a Subscriber, a Subscription and a Publisher
// returns nil; what is used after Close fails with ErrClosed.
func TestLifecycle_CloseTwice(t *testing.T) {
	hub := NewHub()
	s, err := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Subscribe(SubscriptionConfig{SampleQueue: 4})
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
	if _, ok := <-sub.Samples(); ok {
		t.Fatal("Samples not closed after Close")
	}
	for i := range 2 {
		if err := s.Close(); err != nil {
			t.Fatalf("Subscriber.Close #%d: %v", i+1, err)
		}
	}
	if _, err := s.Subscribe(SubscriptionConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe after Close: %v, want ErrClosed", err)
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run after Close: %v, want nil", err)
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
	if err := pub.Run(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after Close: %v, want ErrClosed", err)
	}
}

// L2: Close from two goroutines at once on a running Subscriber, its
// subscription and a running Publisher: every call returns nil, both
// Run calls return nil, and no goroutine of the bubble is left.
func TestLifecycle_ConcurrentClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		pub, err := NewPublisher(hub.NewPort(MAC{}), pubConfig())
		if err != nil {
			t.Fatal(err)
		}
		s, err := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := s.Subscribe(SubscriptionConfig{})
		if err != nil {
			t.Fatal(err)
		}
		runs := make(chan error, 2)
		go func() { runs <- s.Run(context.Background()) }()
		go func() { runs <- pub.Run(context.Background()) }()
		time.Sleep(10 * time.Millisecond) // virtual time: samples flow
		synctest.Wait()
		if st := sub.Stats(); len(st) != 1 || st[0].ASDUs == 0 {
			t.Fatalf("no samples before Close: %+v", st)
		}

		closers := []func() error{s.Close, sub.Close, pub.Close}
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
		for range 2 {
			if err := <-runs; err != nil {
				t.Fatalf("Run after Close: %v", err)
			}
		}
		for range sub.Events() {
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

// L4: Close while Run sends: Run returns nil, every frame sent is
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
		time.Sleep(5 * time.Millisecond) // virtual time: 20 samples at 4000/s
		synctest.Wait()
		if err := pub.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("Run after Close: %v", err)
		}
		before := tr.sent()
		if len(before) == 0 {
			t.Fatal("nothing sent before Close")
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

// L9: OnASDU may call the methods of its subscription and subscriber:
// Stats includes the ASDU being delivered, and Subscribe adds a
// subscription that receives the next frame.
func TestLifecycle_OnASDUCallsPublicMethods(t *testing.T) {
	s, err := NewSubscriber(nil, SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var (
		sub   *Subscription
		other *Subscription
		seen  []uint64
	)
	sub, err = s.Subscribe(SubscriptionConfig{OnASDU: func(smp *Sample) {
		st := sub.Stats()
		if len(st) != 1 {
			t.Errorf("Stats from OnASDU: %d streams", len(st))
			return
		}
		seen = append(seen, st[0].ASDUs)
		if other == nil {
			o, err := s.Subscribe(SubscriptionConfig{})
			if err != nil {
				t.Errorf("Subscribe from OnASDU: %v", err)
			}
			other = o
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.HandleFrame(frameWith(t, nil, 1))
	s.HandleFrame(frameWith(t, nil, 2))
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("ASDUs counted when OnASDU ran: %v, want [1 2]", seen)
	}
	if st := other.Stats(); len(st) != 1 || st[0].ASDUs != 1 || st[0].LastSmpCnt != 2 {
		t.Fatalf("the subscription added from OnASDU: %+v", st)
	}
}

// L10: OnASDU may close its subscription and its subscriber while Run
// receives: both Close calls return nil, Run returns nil, OnASDU is not
// called again and the Events channel is closed.
func TestLifecycle_OnASDUClosesSubscriber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hub := NewHub()
		pub, err := NewPublisher(hub.NewPort(MAC{}), pubConfig())
		if err != nil {
			t.Fatal(err)
		}
		s, err := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		var (
			sub   *Subscription
			calls int
			errs  []error
		)
		sub, err = s.Subscribe(SubscriptionConfig{OnASDU: func(*Sample) {
			calls++
			if calls == 1 {
				errs = append(errs, sub.Close(), s.Close())
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		runs := make(chan error, 1)
		go func() { runs <- s.Run(context.Background()) }()
		go func() { _ = pub.Run(context.Background()) }()
		if err := <-runs; err != nil {
			t.Fatalf("Run after Close from OnASDU: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // virtual time: the publisher goes on sending
		synctest.Wait()
		if err := pub.Close(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if calls != 1 || len(errs) != 2 || errs[0] != nil || errs[1] != nil {
			t.Fatalf("OnASDU called %d times; Close from OnASDU returned %v", calls, errs)
		}
		for range sub.Events() {
		}
	})
}
