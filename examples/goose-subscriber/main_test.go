// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
	mms "github.com/otfabric/go-mms"
)

// TestRun feeds the subscriber from a publisher on an in-memory hub:
// states are printed with their values, and stopping the publisher
// trips the timeAllowedToLive supervision.
func TestRun(t *testing.T) {
	doc, err := demoied.SCL()
	if err != nil {
		t.Fatal(err)
	}
	cb, err := goose.ControlBlockFromSCL(doc, demoied.IED, demoied.LD, "gcb01")
	if err != nil {
		t.Fatal(err)
	}
	hub := goose.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out demoied.Output
	done := make(chan error, 1)
	go func() { done <- run(ctx, hub.NewPort(goose.MAC{}), cb, false, time.Hour, &out) }()
	out.WaitFor(t, "subscribed to", 5*time.Second)

	cfg, err := cb.PublisherConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MinTime, cfg.MaxTime, cfg.TimeAllowedToLive = 5*time.Millisecond, 20*time.Millisecond, 60*time.Millisecond
	pub, err := goose.NewPublisher(hub.NewPort(goose.MAC{}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	pctx, pcancel := context.WithCancel(ctx)
	pdone := make(chan struct{})
	go func() { defer close(pdone); _ = pub.Run(pctx) }()
	out.WaitFor(t, "state stNum=1 ", 5*time.Second)

	vals := cfg.Values
	vals[0] = mms.NewBoolean(true)
	if err := pub.Publish(vals); err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "state stNum=2 ", 5*time.Second)
	out.WaitFor(t, "GGIO1.Ind1.stVal=true", time.Second)

	pcancel()
	<-pdone
	out.WaitFor(t, "timeout: ", 5*time.Second)

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "supervision ", time.Second)
	t.Log("\n" + out.String())
}
