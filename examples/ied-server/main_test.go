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
	"github.com/otfabric/go-iec61850/sv"
	"github.com/otfabric/go-mms/transport/iso"
)

// TestRun serves the demo IED with GOOSE and SV on in-memory hubs and
// checks the simulated values, the control and the streams.
func TestRun(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ghub, shub := goose.NewHub(), sv.NewHub()
	gsub, _ := goose.NewSubscriber(ghub.NewPort(goose.MAC{}), goose.SubscriberConfig{})
	gs, err := gsub.Subscribe(goose.SubscriptionConfig{AppIDs: []uint16{0x1001}})
	if err != nil {
		t.Fatal(err)
	}
	ssub, _ := sv.NewSubscriber(shub.NewPort(sv.MAC{}), sv.SubscriberConfig{})
	ss, err := ssub.Subscribe(sv.SubscriptionConfig{AppIDs: []uint16{0x4000}, SampleQueue: 16})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = gsub.Run(ctx) }()
	go func() { _ = ssub.Run(ctx) }()
	var out demoied.Output
	done := make(chan error, 1)
	cfg := config{
		Listener: ln, GOOSE: ghub.NewPort(goose.MAC{}), SV: shub.NewPort(sv.MAC{}),
		Interval: 5 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() { done <- run(ctx, cfg, &out) }()
	out.WaitFor(t, "serving DemoIED", 5*time.Second)
	out.WaitFor(t, "goose", 0)

	c, err := iec61850.Dial(ctx, ln.Addr().String(), iec61850.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := iec61850.ParseRef("LD1/GGIO1.SPCSO1")
	if err := c.Operate(ctx, ref, iec61850.OperateParams{CtlVal: iec61850.BoolCtlVal(true)}); err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "SPCSO1 operated: true", time.Second)
	_ = c.Close(ctx)

	select {
	case <-gs.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("no GOOSE")
	}
	select {
	case <-ss.Samples():
	case <-time.After(5 * time.Second):
		t.Fatal("no SV")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "shut down", 0)
}
