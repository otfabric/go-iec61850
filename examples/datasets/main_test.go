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
	if err := run(ctx, addr, demoied.Domain, &out); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"DemoIEDLD1/LLN0.dsGoose (3 members", "created DemoIEDLD1/LLN0$dsExample", "DemoIEDLD1/GGIO1.Ind1.stVal[ST] = false", "deleted ", "not found (object-non-existent), as expected"} {
		out.WaitFor(t, s, 0)
	}
	t.Log("\n" + out.String())
}
