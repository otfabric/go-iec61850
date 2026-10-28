// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-mms/transport/iso"
)

// TestRun runs the server with GOOSE on an in-memory hub and MMS on
// loopback: the simulated input changes arrive as GOOSE states and the
// GoCB is readable over MMS.
func TestRun(t *testing.T) {
	hub := goose.NewHub()
	sub, err := goose.NewSubscriber(hub.NewPort(goose.MAC{}), goose.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sub.Subscribe(goose.SubscriptionConfig{AppIDs: []uint16{0x1001}, GocbRef: "LD1/LLN0$GO$gcb01", OnlyStateChanges: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sub.Run(ctx) }()

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var out demoied.Output
	done := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { done <- run(ctx, hub.NewPort(goose.MAC{}), ln, 20*time.Millisecond, logger, &out) }()
	out.WaitFor(t, "MMS on", 5*time.Second)

	seen := map[bool]bool{}
	timeout := time.After(10 * time.Second)
	for !seen[true] || !seen[false] {
		select {
		case ev := <-s.Events():
			if ev.Kind == goose.EventMessage {
				b, _ := ev.Frame.PDU.AllData[0].Bool()
				seen[b] = true
			}
		case <-timeout:
			t.Fatalf("GOOSE states seen %v; output:\n%s", seen, out.String())
		}
	}

	c, err := iec61850.Dial(ctx, ln.Addr().String(), iec61850.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cb, err := c.GetGoCB(ctx, demoied.LD, "LLN0$GO$gcb01")
	if err != nil {
		t.Fatal(err)
	}
	if !cb.GoEna || cb.GoID != "DemoGoose" {
		t.Errorf("GoCB = %+v", cb)
	}
	_ = c.Close(ctx)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return")
	}
}
