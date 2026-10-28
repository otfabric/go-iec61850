// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	mms "github.com/otfabric/go-mms"
)

// TestRun subscribes to the demo IED's URCB and receives the general
// interrogation and a data-change report.
func TestRun(t *testing.T) {
	srv, addr := demoied.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out demoied.Output
	done := make(chan error, 1)
	go func() { done <- run(ctx, addr, "", "", 2, &out) }()
	out.WaitFor(t, "RptID=DemoURCB01", 5*time.Second)
	out.WaitFor(t, "report DemoURCB01 seq=", 5*time.Second)
	srv.SetValue(ctx, demoied.LD+"/MMXU1$MX$PhV$phsA$cVal$mag$f", mms.NewFloat(230))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())
	out.WaitFor(t, "230 (", 0)
}
