// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
)

func TestRun(t *testing.T) {
	_, addr := demoied.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out demoied.Output
	if err := run(ctx, addr, demoied.LD, "PTOC1.StrVal.setMag.f", 175, &out); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())
	for _, s := range []string{"SGCB: 2 groups, active 1", "= 100\n", "group 2: PTOC1.StrVal.setMag.f 150 -> 175", "SGCB: active 2, editing 0", "= 175\n"} {
		out.WaitFor(t, s, 0)
	}
}
