//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2E — direct control in all four directions:
//
//  1. TestLibIECServer_Control_DirectOperate — libiec61850 client → go-iec61850 server
//  2. TestLibIECClient_Control_DirectOperate — go-iec61850 client → libiec61850 server
//  3. TestBeanServer_Control_DirectOperate   — iec61850bean client → go-iec61850 server
//  4. TestBeanClient_Control_DirectOperate   — go-iec61850 client → iec61850bean server
//
// Each test:
//  1. Confirms ctlModel == 1 (direct-with-normal-security).
//  2. Issues Operate with ctlVal=!initial (toggles the initial false value to true).
//  3. Reads back stVal and asserts it matches the commanded value.
//  4. Confirms the association remains open after the control sequence.
package interop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// ---------------------------------------------------------------------------
// Direction 1: libiec61850 client → go-iec61850 server
// ---------------------------------------------------------------------------

// TestLibIECServer_Control_DirectOperate exercises a direct-control Operate
// issued by the libiec61850 adapter against the go-iec61850 IED server.
func TestLibIECServer_Control_DirectOperate(t *testing.T) {
	// The go-iec61850 server starts with SPCSO1.stVal=false (fixVal.SPCSO1StVal).
	// The controller adapter toggles it to true (--ctlval 1).
	srv := startGoIEDServerWithControls(t)
	ch := startIEDControllerAdapter(t, srv.port, true, "SPCSO1")
	results := collectControllerResults(t, ch)

	// 1. read-ctlmodel: value must be 1 (direct-with-normal-security).
	r, ok := findControllerOp(results, "read-ctlmodel")
	if !ok {
		t.Fatal("read-ctlmodel result missing")
	}
	if !r.OK {
		t.Fatalf("read-ctlmodel failed: %s", r.Error)
	}
	var ctlModelVal int
	if err := json.Unmarshal(r.Value, &ctlModelVal); err != nil {
		t.Fatalf("decode ctlModel value: %v", err)
	}
	if ctlModelVal != 1 {
		t.Errorf("ctlModel: want 1 (direct-with-normal-security), got %d", ctlModelVal)
	}

	// 2. operate: must succeed and report ctlval=true.
	r, ok = findControllerOp(results, "operate")
	if !ok {
		t.Fatal("operate result missing")
	}
	if !r.OK {
		t.Fatalf("operate failed: %s", r.Error)
	}
	if r.CtlVal == nil || !*r.CtlVal {
		t.Errorf("operate ctlval: want true, got %v", r.CtlVal)
	}

	// 3. read-stval: must equal the commanded value (true).
	r, ok = findControllerOp(results, "read-stval")
	if !ok {
		t.Fatal("read-stval result missing")
	}
	if !r.OK {
		t.Fatalf("read-stval failed: %s", r.Error)
	}
	var stVal bool
	if err := json.Unmarshal(r.Value, &stVal); err != nil {
		t.Fatalf("decode stVal: %v", err)
	}
	if !stVal {
		t.Errorf("stVal after operate: want true, got false")
	}

	// 4. conclude: clean disconnect.
	r, ok = findControllerOp(results, "conclude")
	if !ok {
		t.Fatal("conclude result missing")
	}
	if !r.OK {
		t.Fatalf("conclude failed: %s", r.Error)
	}
}

// ---------------------------------------------------------------------------
// Direction 2: go-iec61850 client → libiec61850 server
// ---------------------------------------------------------------------------

// TestLibIECClient_Control_DirectOperate verifies the go-iec61850 client
// can issue a direct Operate command against the libiec61850 IED server.
func TestLibIECClient_Control_DirectOperate(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := h.dial(t, ctx)
	defer c.Close(ctx)

	// Confirm ctlModel == 1 (direct-with-normal-security).
	ctlModelRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO1.ctlModel[CF]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	v, err := c.ReadRaw(ctx, ctlModelRef)
	if err != nil {
		t.Fatalf("read ctlModel: %v", err)
	}
	ctlModel, ok := v.Int32()
	if !ok {
		t.Fatalf("ctlModel: not an integer value")
	}
	if ctlModel != 1 {
		t.Errorf("ctlModel: want 1 (direct-with-normal-security), got %d", ctlModel)
	}

	// Issue direct Operate: toggle initial false to true.
	ctlRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO1")
	if err != nil {
		t.Fatalf("ParseRef control: %v", err)
	}
	if err := c.Operate(ctx, ctlRef, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	}); err != nil {
		t.Fatalf("Operate: %v", err)
	}

	// Read back stVal — expect true.
	stValRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO1.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef stVal: %v", err)
	}
	sv, err := c.ReadRaw(ctx, stValRef)
	if err != nil {
		t.Fatalf("read stVal: %v", err)
	}
	got, ok := sv.Bool()
	if !ok {
		t.Fatalf("stVal: not a boolean value")
	}
	if !got {
		t.Errorf("stVal after Operate: want true, got false")
	}

	// Association must still be usable.
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Errorf("ListLogicalDevices after Operate: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Direction 3: iec61850bean client → go-iec61850 server
// ---------------------------------------------------------------------------

// TestBeanServer_Control_DirectOperate exercises a direct-control Operate
// issued by the iec61850bean adapter against the go-iec61850 IED server.
func TestBeanServer_Control_DirectOperate(t *testing.T) {
	srv := startGoIEDServerWithControls(t)
	ch := startIEC61850BeanControllerAdapter(t, srv.port, true, "SPCSO1")
	results := collectControllerResults(t, ch)

	// 1. read-ctlmodel.
	r, ok := findControllerOp(results, "read-ctlmodel")
	if !ok {
		t.Fatal("read-ctlmodel result missing")
	}
	if !r.OK {
		t.Fatalf("read-ctlmodel failed: %s", r.Error)
	}
	var ctlModelVal int
	if err := json.Unmarshal(r.Value, &ctlModelVal); err != nil {
		t.Fatalf("decode ctlModel: %v", err)
	}
	if ctlModelVal != 1 {
		t.Errorf("ctlModel: want 1, got %d", ctlModelVal)
	}

	// 2. operate.
	r, ok = findControllerOp(results, "operate")
	if !ok {
		t.Fatal("operate result missing")
	}
	if !r.OK {
		t.Fatalf("operate failed: %s", r.Error)
	}
	if r.CtlVal == nil || !*r.CtlVal {
		t.Errorf("operate ctlval: want true, got %v", r.CtlVal)
	}

	// 3. read-stval.
	r, ok = findControllerOp(results, "read-stval")
	if !ok {
		t.Fatal("read-stval result missing")
	}
	if !r.OK {
		t.Fatalf("read-stval failed: %s", r.Error)
	}
	var stVal bool
	if err := json.Unmarshal(r.Value, &stVal); err != nil {
		t.Fatalf("decode stVal: %v", err)
	}
	if !stVal {
		t.Errorf("stVal after operate: want true, got false")
	}
}

// ---------------------------------------------------------------------------
// Direction 4: go-iec61850 client → iec61850bean server
// ---------------------------------------------------------------------------

// TestBeanClient_Control_DirectOperate verifies the go-iec61850 client
// can issue a direct Operate command against the iec61850bean IED server.
//
// Note: iec61850bean's SPC control sequence requires writing to CO.Oper.
// The server enforces ctlModel=1 (direct-with-normal-security), and
// go-iec61850's Client.Operate builds the correct Oper structure.
func TestBeanClient_Control_DirectOperate(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c := h.dial(t, ctx)
	defer c.Abort(ctx) //nolint:errcheck

	// Confirm ctlModel == 1.
	ctlModelRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO1.ctlModel[CF]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	v, err := c.ReadRaw(ctx, ctlModelRef)
	if err != nil {
		t.Fatalf("read ctlModel: %v", err)
	}
	ctlModel, ok := v.Int32()
	if !ok {
		t.Fatalf("ctlModel: not an integer value")
	}
	if ctlModel != 1 {
		t.Errorf("ctlModel: want 1, got %d", ctlModel)
	}

	// Issue direct Operate: toggle initial false to true.
	ctlRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO1")
	if err != nil {
		t.Fatalf("ParseRef control: %v", err)
	}
	if err := c.Operate(ctx, ctlRef, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	}); err != nil {
		t.Fatalf("Operate: %v", err)
	}

	// Read back stVal — expect true.
	stValRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO1.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef stVal: %v", err)
	}
	sv, err := c.ReadRaw(ctx, stValRef)
	if err != nil {
		t.Fatalf("read stVal: %v", err)
	}
	got, ok := sv.Bool()
	if !ok {
		t.Fatalf("stVal: not a boolean value")
	}
	if !got {
		t.Errorf("stVal after Operate: want true, got false")
	}

	// Association must remain open.
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Errorf("ListLogicalDevices after Operate: %v", err)
	}
}
