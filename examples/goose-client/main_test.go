// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
)

// TestRun runs the client against the demo IED with GOOSE on an
// in-memory hub.
func TestRun(t *testing.T) {
	srv, err := demoied.NewServer(iec61850.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.EnableGOOSE(iec61850.GOOSEOptions{Transport: goose.NewHub().NewPort(goose.MAC{})}); err != nil {
		t.Fatal(err)
	}
	addr := demoied.Serve(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out demoied.Output
	if err := run(ctx, addr, demoied.LD, "LLN0$GO$gcb01", "Renamed", &out); err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "rejected (temporarily-unavailable)", 0)
	out.WaitFor(t, `GoEna=true GoID="Renamed"`, 0)
	t.Log("\n" + out.String())
}
