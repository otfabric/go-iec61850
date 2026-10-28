// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/sv"
)

// TestRun publishes the waveform on an in-memory hub and checks the
// received samples with a subscriber configured from the same SCL.
func TestRun(t *testing.T) {
	cb, err := loadControlBlock("", demoied.IED, demoied.LD, "MSVCB01")
	if err != nil {
		t.Fatal(err)
	}
	hub := sv.NewHub()
	sub, err := sv.NewSubscriber(hub.NewPort(sv.MAC{}), sv.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var n int
	var peakIA, peakUA, sumIN float64
	sc := cb.SubscriptionConfig()
	sc.OnASDU = func(s *sv.Sample) {
		var le sv.LESample
		if err := sv.DecodeLE(s.ASDU.Sample, &le); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		n++
		peakIA = math.Max(peakIA, sv.Amps(le.Current[0].Value))
		peakUA = math.Max(peakUA, sv.Volts(le.Voltage[0].Value))
		sumIN += math.Abs(sv.Amps(le.Current[3].Value))
	}
	if _, err := sub.Subscribe(sc); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sub.Run(ctx) }()

	var out demoied.Output
	done := make(chan error, 1)
	w := Waveform{Freq: 50, Amps: 100, Volts: 63500}
	go func() { done <- run(ctx, hub.NewPort(sv.MAC{}), cb, w, false, &out) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		got := n
		mu.Unlock()
		if got >= 400 { // 0.1 s, five periods
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d samples; output:\n%s", got, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "stopped: ", time.Second)

	mu.Lock()
	defer mu.Unlock()
	if math.Abs(peakIA-100*math.Sqrt2) > 1 {
		t.Errorf("IA peak %.2f A, want %.2f", peakIA, 100*math.Sqrt2)
	}
	if math.Abs(peakUA-63500*math.Sqrt2) > 100 {
		t.Errorf("UA peak %.0f V, want %.0f", peakUA, 63500*math.Sqrt2)
	}
	if sumIN/float64(n) > 0.01 {
		t.Errorf("mean |IN| %.3f A, want ~0", sumIN/float64(n))
	}
}
