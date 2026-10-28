// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
)

func TestRun(t *testing.T) {
	_, addr := demoied.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out demoied.Output
	if err := run(ctx, addr, slog.New(slog.NewTextHandler(io.Discard, nil)), &out); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())
	out.WaitFor(t, "LD LD1\n", 0)
	out.WaitFor(t, "  LN MMXU1\n", 0)
	out.WaitFor(t, "[MX] = ", 0)
}
