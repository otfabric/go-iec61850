//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2I — SBO normal (select-before-operate)
// in all four directions.

package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// ---------------------------------------------------------------------------
// Phase 2I — SBO (select-before-operate) interop tests
//
// Four directions, SBO normal (ctlModel=2, SPCSO2):
//   TestLibIECServer_Control_SBOOperate  — libiec61850 client → go server
//   TestLibIECClient_Control_SBOOperate  — go client → libiec61850 server
//   TestBeanServer_Control_SBOOperate    — iec61850bean client → go server
//   TestBeanClient_Control_SBOOperate    — go client → iec61850bean server
//
// Negative test:
//   TestGoServer_SBO_OperateWithoutSelect — operate rejected when not selected
// ---------------------------------------------------------------------------

const sbo2Ref = "InteropLD/GGIO1.SPCSO2"
const sbo2StValKey = "InteropLD/GGIO1$ST$SPCSO2$stVal"

// ---------------------------------------------------------------------------
// TestLibIECServer_Control_SBOOperate
// libiec61850 ied-controller --do SPCSO2 → go-iec61850 server
// ---------------------------------------------------------------------------

func TestLibIECServer_Control_SBOOperate(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	// The server starts with SPCSO2.stVal=false; the adapter selects then toggles it to true.
	ch := startIEDControllerAdapter(t, srv.port, true, "SPCSO2")
	results := collectControllerResults(t, ch)

	// 1. read-ctlmodel — must be 2 (sbo-with-normal-security).
	r, ok := findControllerOp(results, "read-ctlmodel")
	if !ok {
		t.Fatal("read-ctlmodel result missing")
	}
	if !r.OK {
		t.Fatalf("read-ctlmodel failed: %s", r.Error)
	}

	// 2. select — must succeed.
	r, ok = findControllerOp(results, "select")
	if !ok {
		t.Fatal("select result missing")
	}
	if !r.OK {
		t.Fatalf("select failed: %s", r.Error)
	}

	// 3. operate — must succeed.
	r, ok = findControllerOp(results, "operate")
	if !ok {
		t.Fatal("operate result missing")
	}
	if !r.OK {
		t.Fatalf("operate failed: %s", r.Error)
	}
	if r.CtlVal == nil || !*r.CtlVal {
		t.Error("operate ctlval: want true")
	}

	// 4. read-stval — must reflect the operated value.
	r, ok = findControllerOp(results, "read-stval")
	if !ok {
		t.Fatal("read-stval result missing")
	}
	if !r.OK {
		t.Fatalf("read-stval failed: %s", r.Error)
	}
	var stVal bool
	if err := unmarshalBool(r.Value, &stVal); err != nil {
		t.Fatalf("read-stval value: %v", err)
	}
	if !stVal {
		t.Errorf("stVal: got false, want true after SBO operate")
	}
}

// ---------------------------------------------------------------------------
// TestLibIECClient_Control_SBOOperate
// go-iec61850 client → libiec61850 ied-server (SPCSO2, SBO normal)
// ---------------------------------------------------------------------------

func TestLibIECClient_Control_SBOOperate(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := h.dial(t, ctx)
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Select SPCSO2.
	selectedRef, err := client.Select(ctx, ref)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selectedRef == "" {
		t.Fatal("Select: empty reference (select denied)")
	}
	t.Logf("select granted: %s", selectedRef)

	// Operate with ctlVal=true.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	})
	if err != nil {
		t.Fatalf("Operate: %v", err)
	}

	// Read back stVal to confirm the state change.
	stValRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO2.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef stVal: %v", err)
	}
	v, err := client.Read(ctx, stValRef)
	if err != nil {
		t.Fatalf("Read stVal: %v", err)
	}
	bv, err := v.Bool()
	if err != nil {
		t.Fatalf("stVal: expected boolean: %v", err)
	}
	if !bv {
		t.Error("stVal: got false, want true after SBO operate")
	}
}

// ---------------------------------------------------------------------------
// TestBeanServer_Control_SBOOperate
// iec61850bean ied-controller --do SPCSO2 → go-iec61850 server
// ---------------------------------------------------------------------------

func TestBeanServer_Control_SBOOperate(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ch := startIEC61850BeanControllerAdapter(t, srv.port, true, "SPCSO2")
	results := collectControllerResults(t, ch)

	// 1. read-ctlmodel.
	r, ok := findControllerOp(results, "read-ctlmodel")
	if !ok {
		t.Fatal("read-ctlmodel result missing")
	}
	if !r.OK {
		t.Fatalf("read-ctlmodel failed: %s", r.Error)
	}

	// 2. select.
	r, ok = findControllerOp(results, "select")
	if !ok {
		t.Fatal("select result missing")
	}
	if !r.OK {
		t.Fatalf("select failed: %s", r.Error)
	}

	// 3. operate.
	r, ok = findControllerOp(results, "operate")
	if !ok {
		t.Fatal("operate result missing")
	}
	if !r.OK {
		t.Fatalf("operate failed: %s", r.Error)
	}

	// 4. read-stval.
	r, ok = findControllerOp(results, "read-stval")
	if !ok {
		t.Fatal("read-stval result missing")
	}
	if !r.OK {
		t.Fatalf("read-stval failed: %s", r.Error)
	}
	var stVal bool
	if err := unmarshalBool(r.Value, &stVal); err != nil {
		t.Fatalf("read-stval value: %v", err)
	}
	if !stVal {
		t.Errorf("stVal: got false, want true after SBO operate")
	}
}

// ---------------------------------------------------------------------------
// TestBeanClient_Control_SBOOperate
// go-iec61850 client → iec61850bean ied-server (SPCSO2, SBO normal)
// ---------------------------------------------------------------------------

func TestBeanClient_Control_SBOOperate(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client := h.dial(t, ctx)
	defer client.Abort(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Select SPCSO2.
	selectedRef, err := client.Select(ctx, ref)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selectedRef == "" {
		t.Fatal("Select: empty reference (select denied)")
	}
	t.Logf("select granted: %s", selectedRef)

	// Operate with ctlVal=true.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	})
	if err != nil {
		t.Fatalf("Operate: %v", err)
	}

	// Read back stVal to confirm the state change.
	stValRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO2.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef stVal: %v", err)
	}
	v, err := client.Read(ctx, stValRef)
	if err != nil {
		t.Fatalf("Read stVal: %v", err)
	}
	bv, err := v.Bool()
	if err != nil {
		t.Fatalf("stVal: expected boolean: %v", err)
	}
	if !bv {
		t.Error("stVal: got false, want true after SBO operate")
	}
}

// ---------------------------------------------------------------------------
// TestGoServer_SBO_OperateWithoutSelect
// Operate rejected when the client has not performed a select first.
// go client → go server, SBO normal.
// ---------------------------------------------------------------------------

func TestGoServer_SBO_OperateWithoutSelect(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The go server registers bare LD domain names (no IED name prefix).
	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Operate WITHOUT select — must fail.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	})
	if err == nil {
		t.Fatal("expected error for operate without select, got nil")
	}
	t.Logf("operate without select correctly rejected: %v", err)

	// stVal must remain false.
	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal != nil {
		if b, ok := stVal.Bool(); ok && b {
			t.Error("stVal was changed despite rejected operate")
		}
	}
}

// ---------------------------------------------------------------------------
// helpers used by SBO tests
// ---------------------------------------------------------------------------

// unmarshalBool decodes a json.RawMessage boolean.
func unmarshalBool(raw []byte, out *bool) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty value")
	}
	if string(raw) == "true" {
		*out = true
		return nil
	}
	if string(raw) == "false" {
		*out = false
		return nil
	}
	return fmt.Errorf("unexpected boolean value: %s", raw)
}
