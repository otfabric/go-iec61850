// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"math/rand"
	"testing"
)

// chaosRun publishes n consecutive counters through a faulty hub and
// feeds every delivered frame to a subscriber.
func chaosRun(t *testing.T, faults Faults, n int) (Stats, HubStats, int) {
	t.Helper()
	hub := NewHub()
	tx, rx := hub.NewPort(MAC{}), hub.NewPort(MAC{})
	s, _ := NewSubscriber(nil, SubscriberConfig{})
	delivered := 0
	sub, _ := s.Subscribe(SubscriptionConfig{SvID: "MU01", SmpCntWrap: 4000, Layout: LELayout, OnASDU: func(*Sample) { delivered++ }})
	var received uint64
	drain := func() {
		// Receive exactly what the hub delivered so far.
		for received < hub.Stats().Delivered {
			f, err := rx.Recv(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			received++
			s.HandleFrame(f)
		}
	}
	send := func(c int) {
		if err := tx.Send(frameWith(t, nil, uint16(c%4000))); err != nil {
			t.Fatal(err)
		}
		drain()
	}
	hub.SetFaults(Faults{Seed: 1})
	send(0)
	hub.SetFaults(faults)
	for c := 1; c < n-1; c++ {
		send(c)
	}
	hub.Flush()
	drain()
	hub.SetFaults(Faults{})
	send(n - 1) // a clean last frame closes any trailing gap
	return sub.Stats()[0], hub.Stats(), delivered
}

// TestChaosAccounting checks the sequence accounting under loss,
// duplication and reordering across several counter wraps: every
// counter is either delivered once or reported lost, every duplicate
// is detected, and every late frame is reported out of order.
func TestChaosAccounting(t *testing.T) {
	t.Parallel()
	const n = 12001
	st, hs, delivered := chaosRun(t, Faults{Loss: 0.05, Duplicate: 0.05, Reorder: 0.05, Seed: 7}, n)
	if delivered+int(st.Lost) != n {
		t.Errorf("delivered %d + lost %d != %d", delivered, st.Lost, n)
	}
	if st.Duplicates != hs.Duplicated {
		t.Errorf("duplicates %d, hub duplicated %d", st.Duplicates, hs.Duplicated)
	}
	if st.OutOfOrder != hs.Reordered {
		t.Errorf("out of order %d, hub reordered %d", st.OutOfOrder, hs.Reordered)
	}
	if st.Lost != hs.Lost+hs.Reordered {
		t.Errorf("lost %d, hub lost %d + reordered %d", st.Lost, hs.Lost, hs.Reordered)
	}
	if hs.Lost == 0 || hs.Duplicated == 0 || hs.Reordered == 0 {
		t.Errorf("faults not exercised: %+v", hs)
	}
}

// TestChaosMutation flips random bits in every frame: the subscriber
// must survive and count each frame as delivered, invalid or dropped.
func TestChaosMutation(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(3))
	st, hs, delivered := chaosRun(t, Faults{Seed: 3, Mutate: func(b []byte) []byte {
		for i := rng.Intn(3); i >= 0; i-- {
			b[rng.Intn(len(b))] ^= 1 << rng.Intn(8)
		}
		return b
	}}, 3000)
	if hs.Delivered != 3000 || delivered == 0 || delivered >= 3000 {
		t.Errorf("hub %+v, delivered %d", hs, delivered)
	}
	if st.Frames == 0 {
		t.Errorf("stats %+v", st)
	}
}
