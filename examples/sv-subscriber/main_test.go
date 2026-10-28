// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/sv"
)

// TestRun feeds the subscriber from a 9-2LE publisher on an in-memory
// hub with 10 % frame loss: it reports the stream, the loss and the RMS
// values, and a timeout once the publisher stops.
func TestRun(t *testing.T) {
	doc, err := demoied.SCL()
	if err != nil {
		t.Fatal(err)
	}
	cb, err := sv.ControlBlockFromSCL(doc, demoied.IED, demoied.LD, "MSVCB01")
	if err != nil {
		t.Fatal(err)
	}
	hub := sv.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out demoied.Output
	done := make(chan error, 1)
	go func() { done <- run(ctx, hub.NewPort(sv.MAC{}), cb, 50*time.Millisecond, &out) }()
	out.WaitFor(t, "subscribed to", 5*time.Second)

	cfg, err := cb.PublisherConfig()
	if err != nil {
		t.Fatal(err)
	}
	var le sv.LESample
	le.Current[0].Value = sv.AmpsCount(10) // DC 10 A: RMS 10 A
	cfg.Sample = le.Append(nil)
	pub, err := sv.NewPublisher(hub.NewPort(sv.MAC{}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	hub.SetFaults(sv.Faults{Loss: 0.1})
	pctx, pcancel := context.WithCancel(ctx)
	pdone := make(chan struct{})
	go func() { defer close(pdone); _ = pub.Run(pctx) }()

	out.WaitFor(t, "start: ", 5*time.Second)
	out.WaitFor(t, "IA 10.0 ", 5*time.Second)
	out.WaitFor(t, "gap: ", 5*time.Second)
	pcancel()
	<-pdone
	out.WaitFor(t, "timeout: ", 5*time.Second)

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String()[:min(len(out.String()), 1500)])
}
