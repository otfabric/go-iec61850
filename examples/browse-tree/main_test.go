// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
)

func TestRun(t *testing.T) {
	_, addr := demoied.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out demoied.Output
	if err := run(ctx, addr, iec61850.TreeOptions{MaxDepth: 4, IncludeFCs: true}, &out); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String()[:min(800, len(out.String()))])
	out.WaitFor(t, "    StrVal [", 0)
	out.WaitFor(t, "MMXU1", 0)
}
