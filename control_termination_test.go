// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// TestServerCommandTermination checks that an enhanced-security operate
// is followed by a positive CommandTermination (InformationReport of
// LN$CO$DO$Oper) on the operating connection, and that a normal-security
// operate is not.
func TestServerCommandTermination(t *testing.T) {
	ctx := context.Background()
	doc, err := scl.ParseFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := srv.RegisterControl("InteropIEDInteropLD", "GGIO1.SPCSO3", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterControl("InteropIEDInteropLD", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{}); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(sctx, serverT) }()
	mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reports := make(chan *mms.InformationReportIndication, 8)
	mc.OnInformationReport(func(r *mms.InformationReportIndication) { reports <- r })
	c, _ := NewClient(mc, ClientOptions{})
	defer func() { _ = c.Close(ctx) }()

	direct := mustParseRef(t, "InteropIEDInteropLD/GGIO1.SPCSO1")
	if err := c.Operate(ctx, direct, OperateParams{CtlVal: BoolCtlVal(true)}); err != nil {
		t.Fatal(err)
	}
	sbow := mustParseRef(t, "InteropIEDInteropLD/GGIO1.SPCSO3")
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 7}
	if err := c.SelectWithValue(ctx, sbow, params); err != nil {
		t.Fatal(err)
	}
	if err := c.Operate(ctx, sbow, params); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-reports:
		if len(r.Variables) != 1 || r.Variables[0].Domain != "InteropIEDInteropLD" || r.Variables[0].ItemID != "GGIO1$CO$SPCSO3$Oper" || len(r.Values) != 1 {
			t.Fatalf("CommandTermination = %+v", r)
		}
		oper, ok := r.Values[0].Structure()
		if !ok || len(oper) == 0 {
			t.Fatalf("Oper value %v", r.Values[0])
		}
		if b, _ := oper[0].Bool(); !b {
			t.Errorf("ctlVal = %v", oper[0])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no CommandTermination")
	}
	select {
	case r := <-reports:
		t.Errorf("unexpected report %+v", r)
	case <-time.After(2 * commandTerminationDelay):
	}
}

// TestServerCommandTerminationStopsOnClose closes the server inside the
// CommandTermination delay. The send goroutine must stop with the
// server (Close waits for it) instead of reporting later on its own.
func TestServerCommandTerminationStopsOnClose(t *testing.T) {
	ctx := context.Background()
	doc, err := scl.ParseFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterControl("InteropIEDInteropLD", "GGIO1.SPCSO3", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(sctx, serverT) }()
	mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reports := make(chan *mms.InformationReportIndication, 8)
	mc.OnInformationReport(func(r *mms.InformationReportIndication) { reports <- r })
	c, _ := NewClient(mc, ClientOptions{})
	defer func() { _ = c.Close(ctx) }()

	sbow := mustParseRef(t, "InteropIEDInteropLD/GGIO1.SPCSO3")
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 9}
	if err := c.SelectWithValue(ctx, sbow, params); err != nil {
		t.Fatal(err)
	}
	if err := c.Operate(ctx, sbow, params); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	srv.Close()
	if d := time.Since(start); d > commandTerminationDelay/2 {
		t.Errorf("Close took %v, want the pending send cancelled", d)
	}
	select {
	case r := <-reports:
		t.Errorf("CommandTermination sent after Close: %+v", r)
	case <-time.After(3 * commandTerminationDelay):
	}
	// A later enhanced operate starts no new background send.
	if srv.bg.start(func(context.Context) { t.Error("task started after Close") }) {
		t.Error("start after Close reported true")
	}
}

func TestBackgroundTasks(t *testing.T) {
	var b backgroundTasks
	started := make(chan struct{})
	ended := make(chan struct{})
	if !b.start(func(life context.Context) {
		close(started)
		<-life.Done()
		close(ended)
	}) {
		t.Fatal("start on zero value failed")
	}
	<-started
	b.stop()
	select {
	case <-ended:
	default:
		t.Fatal("stop returned before the task ended")
	}
	b.stop() // idempotent
	if b.start(func(context.Context) {}) {
		t.Error("start after stop")
	}
	var unused backgroundTasks
	unused.stop() // stop without any task
}
