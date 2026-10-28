// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
)

// TestRun publishes the demo GoCB on an in-memory hub and checks that a
// subscriber configured from the same SCL sees the toggles.
func TestRun(t *testing.T) {
	cb, err := loadControlBlock("", demoied.IED, demoied.LD, "gcb01")
	if err != nil {
		t.Fatal(err)
	}
	hub := goose.NewHub()
	subTr := hub.NewPort(goose.MAC{})
	sub, err := goose.NewSubscriber(subTr, goose.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	sc := cb.SubscriptionConfig()
	sc.OnlyStateChanges = true
	s, err := sub.Subscribe(sc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sub.Run(ctx) }()

	var out demoied.Output
	done := make(chan error, 1)
	go func() { done <- run(ctx, hub.NewPort(goose.MAC{}), cb, 20*time.Millisecond, &out) }()

	var states []bool
	timeout := time.After(10 * time.Second)
	for len(states) < 3 {
		select {
		case ev := <-s.Events():
			if ev.Kind == goose.EventInvalid {
				t.Fatalf("invalid: %v", ev.Err)
			}
			if ev.Kind == goose.EventMessage && ev.NewState {
				b, _ := ev.Frame.PDU.AllData[0].Bool()
				states = append(states, b)
			}
		case <-timeout:
			t.Fatalf("states %v; output:\n%s", states, out.String())
		}
	}
	if states[0] == states[1] || states[1] == states[2] {
		t.Errorf("states %v do not toggle", states)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "stopped: stNum", time.Second)
}
