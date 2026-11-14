// SPDX-License-Identifier: MIT

package goose

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// closeHarness is a subscriber fed with HandleFrame (no transport, no
// goroutine of its own) and one subscription whose Handler waits for
// release before its first call, so that the test decides how many events
// are queued when the Handler runs.
type closeHarness struct {
	s       *Subscriber
	sub     *Subscription
	release chan struct{}
	calls   atomic.Int32
}

func newCloseHarness(t *testing.T, onFirst func(h *closeHarness)) *closeHarness {
	t.Helper()
	h := &closeHarness{release: make(chan struct{})}
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	h.s = s
	// Subscribe returns before the Handler can run: its first call waits.
	sub, err := s.Subscribe(SubscriptionConfig{Handler: func(Event) {
		<-h.release
		if h.calls.Add(1) == 1 {
			onFirst(h)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	h.sub = sub
	return h
}

// feed hands the subscriber one frame with state number stNum.
func (h *closeHarness) feed(t *testing.T, stNum uint32) {
	t.Helper()
	h.s.HandleFrame(mustEncode(t, &Frame{
		Dst: testDst, Src: MAC{2, 0, 0, 0, 0, 9}, AppID: 0x1000,
		PDU: PDU{GocbRef: "LD/LLN0$GO$gcb1", DatSet: "LD/LLN0$ds1", GoID: "gcb1", TimeAllowedToLive: 60000,
			T: time.Unix(0, 0), StNum: stNum, ConfRev: 1, AllData: []*mms.Value{mms.NewBoolean(true)}},
	}))
}

// A Handler may close its own subscription, or its subscriber (L10): the
// Close returns without waiting for the goroutine it runs on, the events
// that were queued are handed to the Handler afterwards, the event
// channel is closed and the Handler's goroutine ends. A later Close from
// another goroutine returns nil. synctest shows that no goroutine of the
// bubble is left.
func TestClose_FromHandler(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(h *closeHarness) error
	}{
		{"Subscription.Close", func(h *closeHarness) error { return h.sub.Close() }},
		{"Subscriber.Close", func(h *closeHarness) error { return h.s.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				returned := make(chan error, 1)
				h := newCloseHarness(t, func(h *closeHarness) { returned <- tc.close(h) })

				h.feed(t, 1)
				h.feed(t, 2)
				synctest.Wait() // the Handler holds the first event and waits
				queued := 1 + len(h.sub.events)
				if queued < 3 {
					t.Fatalf("%d events queued, want 3 or more", queued)
				}

				close(h.release)
				synctest.Wait()
				select {
				case err := <-returned:
					if err != nil {
						t.Errorf("Close from the Handler = %v, want nil", err)
					}
				default:
					t.Fatal("Close called from the Handler has not returned")
				}
				if got := int(h.calls.Load()); got != queued {
					t.Errorf("Handler called %d times, want the %d events queued before the Close", got, queued)
				}
				if _, open := <-h.sub.Events(); open {
					t.Error("the event channel is not closed")
				}

				// From another goroutine: nothing left to wait for.
				if err := h.sub.Close(); err != nil {
					t.Errorf("Subscription.Close afterwards = %v, want nil", err)
				}
				if err := h.s.Close(); err != nil {
					t.Errorf("Subscriber.Close afterwards = %v, want nil", err)
				}
				h.feed(t, 3) // not delivered: no subscription is left
				synctest.Wait()
				if got := int(h.calls.Load()); got != queued {
					t.Errorf("Handler called %d times after the Close, want %d", got, queued)
				}
			})
		})
	}
}

// A Close from another goroutine while the Handler runs waits until the
// Handler has been given every queued event and its goroutine has ended
// (L3, L4): this is unchanged.
func TestClose_FromAnotherGoroutineWaitsForHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newCloseHarness(t, func(*closeHarness) {})
		h.feed(t, 1)
		h.feed(t, 2)
		synctest.Wait() // the Handler holds the first event and waits
		queued := 1 + len(h.sub.events)

		closed := make(chan struct{})
		go func() {
			defer close(closed)
			_ = h.sub.Close()
		}()
		synctest.Wait()
		select {
		case <-closed:
			t.Fatal("Close returned while the Handler was still running")
		default:
		}

		close(h.release)
		synctest.Wait()
		select {
		case <-closed:
		default:
			t.Fatal("Close has not returned after the Handler finished")
		}
		if got := int(h.calls.Load()); got != queued {
			t.Errorf("Handler called %d times before Close returned, want %d", got, queued)
		}
	})
}
