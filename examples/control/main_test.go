// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
)

func TestRun(t *testing.T) {
	srv, err := demoied.NewServer(iec61850.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var operated atomic.Int32
	if err := srv.RegisterControl(demoied.LD, "GGIO1.SPCSO1", iec61850.CtlModelDirectNormal, iec61850.ControlHandler{
		OnOperate: func(_ context.Context, req iec61850.ControlRequest) error {
			if b, _ := req.CtlVal.Bool(); !b {
				return errors.New("refused: only on")
			}
			operated.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	addr := demoied.Serve(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out demoied.Output
	if err := run(ctx, addr, "", true, &out); err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "LD1/GGIO1.SPCSO1 ctlModel=", 0)

	if err := run(ctx, addr, "LD1/GGIO1.SPCSO1", true, &out); err != nil {
		t.Fatal(err)
	}
	out.WaitFor(t, "operated: ctlVal=true", 0)
	if operated.Load() != 1 {
		t.Errorf("operated %d times", operated.Load())
	}
	if err := run(ctx, addr, "LD1/GGIO1.SPCSO1", false, &out); err == nil {
		t.Error("refused operate succeeded")
	}
	t.Log("\n" + out.String())
}
