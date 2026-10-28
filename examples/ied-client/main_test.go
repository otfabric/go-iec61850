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
	mms "github.com/otfabric/go-mms"
)

func TestRun(t *testing.T) {
	srv, err := demoied.NewServer(iec61850.ServerOptions{Identity: &iec61850.ServerIdentity{Vendor: "OTFabric", Model: "DemoIED", Revision: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	addr := demoied.Serve(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out demoied.Output
	done := make(chan error, 1)
	go func() { done <- run(ctx, addr, slog.New(slog.NewTextHandler(io.Discard, nil)), 2, &out) }()
	out.WaitFor(t, "subscribed to LLN0$RP$urcb01 (RptID DemoURCB01)", 5*time.Second)
	out.WaitFor(t, "report seq=1", 5*time.Second)
	srv.SetValue(ctx, demoied.LD+"/MMXU1$MX$PhV$phsA$cVal$mag$f", mms.NewFloat(231))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())
	out.WaitFor(t, "connected to OTFabric DemoIED 1", 0)
	out.WaitFor(t, "report seq=2: 231", 0)
}
