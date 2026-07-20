//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2J — SBOw / select-before-operate with
// enhanced security (ctlModel=4, SPCSO3) in all four directions.

package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// ---------------------------------------------------------------------------
// Phase 2J — SBOw (select-before-operate with enhanced security) tests
//
// Four directions, SBOw (ctlModel=4, SPCSO3):
//   TestLibIECServer_Control_SBOwOperate  — libiec61850 client → go server
//   TestLibIECClient_Control_SBOwOperate  — go client → libiec61850 server
//   TestBeanServer_Control_SBOwOperate    — iec61850bean client → go server
//   TestBeanClient_Control_SBOwOperate    — go client → iec61850bean server
//
// Negative tests:
//   TestGoServer_SBOw_OperateWithoutSelect — operate rejected when not selected
//   TestGoServer_SBOw_CancelClearsSelect   — cancel clears select; subsequent operate rejected
// ---------------------------------------------------------------------------

const sbow3Ref = "InteropLD/GGIO1.SPCSO3"
const sbow3StValKey = "InteropLD/GGIO1$ST$SPCSO3$stVal"

// ---------------------------------------------------------------------------
// TestLibIECServer_Control_SBOwOperate
// libiec61850 ied-controller --do SPCSO3 → go-iec61850 server
// ---------------------------------------------------------------------------

func TestLibIECServer_Control_SBOwOperate(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ch := startIEDControllerAdapter(t, srv.port, true, "SPCSO3")
	results := collectControllerResults(t, ch)

	// 1. read-ctlmodel — must be 4 (sbo-with-enhanced-security).
	r, ok := findControllerOp(results, "read-ctlmodel")
	if !ok {
		t.Fatal("read-ctlmodel result missing")
	}
	if !r.OK {
		t.Fatalf("read-ctlmodel failed: %s", r.Error)
	}

	// 2. select-with-value — must succeed.
	r, ok = findControllerOp(results, "select-with-value")
	if !ok {
		t.Fatal("select-with-value result missing")
	}
	if !r.OK {
		t.Fatalf("select-with-value failed: %s", r.Error)
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
		t.Errorf("stVal: got false, want true after SBOw operate")
	}
}

// ---------------------------------------------------------------------------
// TestLibIECClient_Control_SBOwOperate
// go-iec61850 client → libiec61850 ied-server (SPCSO3, SBOw enhanced)
// ---------------------------------------------------------------------------

func TestLibIECClient_Control_SBOwOperate(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := h.dial(t, ctx)
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// SBOw requires the same ctlNum in SelectWithValue and Operate so the
	// server can correlate the two. Use an explicit constant.
	const ctlNum = 7

	// SelectWithValue — provides ctlVal as part of the select.
	err = client.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: ctlNum,
	})
	if err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}

	// Operate with ctlVal=true and the same ctlNum.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: ctlNum,
	})
	if err != nil {
		t.Fatalf("Operate: %v", err)
	}

	// Read back stVal to confirm the state change.
	stValRef, err := iec61850.ParseRef("InteropLD/GGIO1.SPCSO3.stVal[ST]")
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
		t.Error("stVal: got false, want true after SBOw operate")
	}
}

// ---------------------------------------------------------------------------
// TestBeanServer_Control_SBOwOperate
// iec61850bean ied-controller --do SPCSO3 → go-iec61850 server
// ---------------------------------------------------------------------------

func TestBeanServer_Control_SBOwOperate(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ch := startIEC61850BeanControllerAdapter(t, srv.port, true, "SPCSO3")
	results := collectControllerResults(t, ch)

	// 1. read-ctlmodel.
	r, ok := findControllerOp(results, "read-ctlmodel")
	if !ok {
		t.Fatal("read-ctlmodel result missing")
	}
	if !r.OK {
		t.Fatalf("read-ctlmodel failed: %s", r.Error)
	}

	// 2. select-with-value.
	r, ok = findControllerOp(results, "select-with-value")
	if !ok {
		t.Fatal("select-with-value result missing")
	}
	if !r.OK {
		t.Fatalf("select-with-value failed: %s", r.Error)
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
		t.Errorf("stVal: got false, want true after SBOw operate")
	}
}

// ---------------------------------------------------------------------------
// TestBeanClient_Control_SBOwOperate
// go-iec61850 client → iec61850bean ied-server (SPCSO3, SBOw enhanced)
//
// iec61850bean's server does not expose the SBOw control service via plain
// MMS Write to the SBOw[CO] attribute; it requires its own internal control
// flow. This test is therefore skipped as a known compatibility gap.
// ---------------------------------------------------------------------------

func TestBeanClient_Control_SBOwOperate(t *testing.T) {
	t.Skip("iec61850bean server does not expose SBOw[CO] as a writable MMS attribute; SBOw interop with iec61850bean server is a known gap")
}

// ---------------------------------------------------------------------------
// TestGoServer_SBOw_OperateWithoutSelect
// Operate rejected when the client has not performed a SelectWithValue first.
// go client → go server, SBOw enhanced.
// ---------------------------------------------------------------------------

func TestGoServer_SBOw_OperateWithoutSelect(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The go server registers bare LD domain names (no IED name prefix).
	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Operate WITHOUT SelectWithValue — must fail.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	})
	if err == nil {
		t.Fatal("expected error for operate without SBOw select, got nil")
	}
	t.Logf("operate without SBOw select correctly rejected: %v", err)

	// stVal must remain false.
	stVal := srv.srv.ValueStore().Get(sbow3StValKey)
	if stVal != nil {
		if b, ok := stVal.Bool(); ok && b {
			t.Error("stVal was changed despite rejected operate")
		}
	}
}

// ---------------------------------------------------------------------------
// TestGoServer_SBOw_CancelClearsSelect
// SelectWithValue followed by Cancel: subsequent Operate must be rejected.
// go client → go server, SBOw enhanced.
// ---------------------------------------------------------------------------

func TestGoServer_SBOw_CancelClearsSelect(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The go server registers bare LD domain names (no IED name prefix).
	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Step 1: SelectWithValue — must succeed.
	if err := client.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 7,
	}); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}

	// Step 2: Cancel — clears the select.
	if err := client.Cancel(ctx, ref, iec61850.CancelParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 7,
	}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Step 3: Operate — must be rejected (select was cancelled).
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 7,
	})
	if err == nil {
		t.Fatal("expected error for operate after cancel, got nil")
	}
	t.Logf("operate after cancel correctly rejected: %v", err)

	// stVal must remain false.
	stVal := srv.srv.ValueStore().Get(sbow3StValKey)
	if stVal != nil {
		if b, ok := stVal.Bool(); ok && b {
			t.Error("stVal was changed despite rejected operate")
		}
	}
}
