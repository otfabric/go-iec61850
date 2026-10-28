// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/sv"
)

// TestRun reconfigures the demo IED's MSVCB01 and checks on an
// in-memory hub that the stream now carries the new smpRate.
func TestRun(t *testing.T) {
	hub := sv.NewHub()
	srv, err := demoied.NewServer(iec61850.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.EnableSV(iec61850.SVOptions{Transport: hub.NewPort(sv.MAC{})}); err != nil {
		t.Fatal(err)
	}
	addr := demoied.Serve(t, srv)

	sub, err := sv.NewSubscriber(hub.NewPort(sv.MAC{}), sv.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	rates := make(chan uint16, 1)
	if _, err := sub.Subscribe(sv.SubscriptionConfig{
		AppIDs: []uint16{0x4000},
		OnASDU: func(s *sv.Sample) {
			if s.ASDU.HasSmpRate {
				select {
				case rates <- s.ASDU.SmpRate:
				default:
				}
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = sub.Run(ctx) }()

	var out demoied.Output
	if err := run(ctx, addr, demoied.LD, "LLN0$MS$MSVCB01", 96, &out); err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "SvEna=true", 0)
	out.WaitFor(t, "SmpRate=96 ", 0)
	t.Log("\n" + out.String())
	select {
	case r := <-rates:
		if r != 96 {
			t.Errorf("smpRate on the wire = %d", r)
		}
	case <-ctx.Done():
		t.Fatal("no ASDU with smpRate")
	}
}
