// SPDX-License-Identifier: MIT

package goose

import (
	"testing"
)

// Simulated frames a subscription does not accept (AcceptSimulation
// false) create no stream: they take none of the MaxStreams slots, so a
// non-simulated stream seen afterwards is still supervised.
func TestMaxStreams_RefusedSimulationTakesNoSlot(t *testing.T) {
	m := newCountingMetrics()
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{Metrics: m})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: 2, QueueSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	const simulated = 5
	for i := range simulated {
		f := streamFrame(i, 1, 0)
		f.Simulation = true
		s.HandleFrame(mustEncode(t, f))
	}
	if got := len(sub.Supervision()); got != 0 {
		t.Fatalf("%d streams after refused simulated frames, want 0", got)
	}
	s.HandleFrame(mustEncode(t, streamFrame(100, 1, 0)))
	if got := len(sub.Supervision()); got != 1 {
		t.Fatalf("%d streams supervised, want 1", got)
	}
	if got := sub.Counters().StreamsRefused; got != 0 {
		t.Fatalf("StreamsRefused = %d, want 0", got)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.dropped["simulation"]; got != simulated {
		t.Fatalf(`FrameDropped("simulation") %d times, want %d`, got, simulated)
	}
}
