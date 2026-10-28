// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	mms "github.com/otfabric/go-mms"
)

// TestRun reads the entries logged so far, then follows the journal
// while the server logs more.
func TestRun(t *testing.T) {
	srv, addr := demoied.Start(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const key = demoied.LD + "/MMXU1$MX$PhV$phsA$cVal$mag$f"
	srv.SetValue(ctx, key, mms.NewFloat(230.5))
	srv.SetValue(ctx, key, mms.NewFloat(231.5))

	var out demoied.Output
	done := make(chan error, 1)
	go func() { done <- run(ctx, addr, demoied.LD, time.Hour, true, 20*time.Millisecond, &out) }()
	out.WaitFor(t, "LD1/LLN0$EventLog: 2 entries", 5*time.Second)
	out.WaitFor(t, "MMXU1$MX$PhV$phsA$cVal$mag$f = 231.5", time.Second)

	srv.SetValue(ctx, key, mms.NewFloat(232.5))
	out.WaitFor(t, "= 232.5", 5*time.Second)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())
	if n := strings.Count(out.String(), "= 230.5"); n != 1 {
		t.Errorf("entry 1 printed %d times: following must only return new entries", n)
	}
}
