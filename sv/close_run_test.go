// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Subscriber.Close stops Run while Run waits for a frame on a transport
// that delivers none (L3): Run returns nil, the subscriptions are closed
// and no goroutine of the bubble is left. The times are the fake time of
// the synctest bubble.
func TestSubscriberClose_StopsRunOnQuietTransport(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := s.Subscribe(SubscriptionConfig{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel() // ends Run when the test fails
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()

		time.Sleep(time.Second) // fake time: supervision ticks, no frame arrives
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Run returned %v before Close", err)
		default:
		}

		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run after Close = %v, want nil", err)
			}
		default:
			t.Fatal("Run has not returned after Close on a transport that delivers nothing")
		}
		if _, open := <-sub.Events(); open {
			t.Error("the event channel is not closed")
		}
		if err := s.Close(); err != nil {
			t.Errorf("second Close = %v, want nil", err)
		}
		// Run on a closed subscriber returns at once.
		if err := s.Run(context.Background()); err != nil {
			t.Errorf("Run on a closed subscriber = %v, want nil", err)
		}
	})
}

// Run returns only when its supervision goroutine has ended: while a
// timeout check is still running, Run waits for it.
func TestSubscriberRun_WaitsForSupervision(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		releaseOnce := sync.OnceFunc(func() { close(release) })
		defer releaseOnce() // also when the test fails
		var armed atomic.Bool
		s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{
			SupervisionInterval: time.Second,
			// Without frames, Now is called by the timeout checks only.
			Now: func() time.Time {
				if armed.CompareAndSwap(true, false) {
					close(entered)
					<-release
				}
				return time.Now()
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()

		armed.Store(true)
		<-entered // the next timeout check is running and held
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Run returned %v while its supervision goroutine was still running", err)
		default:
		}

		releaseOnce()
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
		default:
			t.Fatal("Run has not returned after the supervision goroutine ended")
		}
	})
}
