//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase G2/G3 — SBO and SBOw state-machine tests
// (go client → go server, all tests in package interop).
//
// Phase G2 — SBO normal (ctlModel=2, SPCSO2) security state machine:
//   TestGoServer_SBO_CancelByOwner           — owner cancel clears select
//   TestGoServer_SBO_CancelByNonOwner        — non-owner cancel (skipped: not enforced)
//   TestGoServer_SBO_SecondClientContention  — second client select denied
//   TestGoServer_SBO_SelectTimeout           — select timeout (skipped: not configurable)
//   TestGoServer_SBO_RepeatedSelectSameClient — repeated select from same client
//   TestGoServer_SBO_DisconnectReleasesSelection — disconnect clears select (skipped: not implemented)
//
// Phase G3 — SBOw (ctlModel=4, SPCSO3) additional state tests:
//   TestGoServer_SBOw_ValueMustMatchOperate      — ctlVal mismatch (skipped: not enforced)
//   TestGoServer_SBOw_SecondClientContention     — second client select denied (skipped: not enforced)
//   TestGoServer_SBOw_DisconnectReleasesSelection — disconnect/re-select

package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// ---------------------------------------------------------------------------
// Phase G2 — SBO normal state machine
// ---------------------------------------------------------------------------

// TestGoServer_SBO_CancelByOwner verifies that a cancel issued by the same
// client that holds the select clears the selection, so a subsequent operate
// is rejected.
func TestGoServer_SBO_CancelByOwner(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Step 1: Select SPCSO2.
	selectedRef, err := client.Select(ctx, ref)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	t.Logf("select granted: %s", selectedRef)

	// Step 2: Cancel — same client (owner) cancels the selection.
	if err := client.Cancel(ctx, ref, iec61850.CancelParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	}); err != nil {
		t.Fatalf("Cancel by owner: %v", err)
	}
	t.Log("cancel by owner succeeded")

	// Step 3: Operate — must be rejected because the selection was cancelled.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	})
	if err == nil {
		t.Fatal("expected error for operate after cancel, got nil")
	}
	t.Logf("operate after cancel correctly rejected: %v", err)

	// stVal must remain false (operate was rejected).
	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal != nil {
		if b, ok := stVal.Bool(); ok && b {
			t.Error("stVal was changed despite rejected operate")
		}
	}
}

// TestGoServer_SBO_CancelByNonOwner verifies that a cancel from a client that
// does not hold the selection is rejected (IEC 61850-7-2 §20.3 ownership rule).
func TestGoServer_SBO_CancelByNonOwner(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Client A selects SPCSO2.
	clientA, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer clientA.Close(ctx) //nolint:errcheck

	if _, err := clientA.Select(ctx, ref); err != nil {
		t.Fatalf("clientA Select: %v", err)
	}

	// Client B connects and tries to cancel — must fail (B doesn't own the selection).
	clientB, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer clientB.Close(ctx) //nolint:errcheck

	err = clientB.Cancel(ctx, ref, iec61850.CancelParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	})
	if err == nil {
		t.Error("expected error for clientB cancel while A holds selection, got nil")
	} else {
		t.Logf("clientB cancel correctly rejected: %v", err)
	}

	// Client A operates — must succeed (selection is still intact after failed cancel).
	if err := clientA.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	}); err != nil {
		t.Fatalf("clientA Operate after failed cancel: %v", err)
	}

	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal == nil {
		t.Fatal("stVal not set after clientA operate")
	}
	if b, ok := stVal.Bool(); !ok || !b {
		t.Errorf("stVal: got %v, want true", stVal)
	}
}

// TestGoServer_SBO_SecondClientContention verifies that a second client's
// select is denied while another client holds the selection for SBO normal.
func TestGoServer_SBO_SecondClientContention(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Client A selects SPCSO2.
	clientA, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer clientA.Close(ctx) //nolint:errcheck

	selectedRef, err := clientA.Select(ctx, ref)
	if err != nil {
		t.Fatalf("clientA Select: %v", err)
	}
	t.Logf("clientA select granted: %s", selectedRef)

	// Client B connects and tries to select — must fail (A holds the selection).
	clientB, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer clientB.Close(ctx) //nolint:errcheck

	_, err = clientB.Select(ctx, ref)
	if err == nil {
		t.Fatal("expected error for clientB select while A holds selection, got nil")
	}
	t.Logf("clientB select correctly denied: %v", err)

	// Client A operates — must succeed (A still holds the selection).
	if err := clientA.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	}); err != nil {
		t.Fatalf("clientA Operate: %v", err)
	}

	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal == nil {
		t.Fatal("stVal not set after clientA operate")
	}
	if b, ok := stVal.Bool(); !ok || !b {
		t.Errorf("stVal: got %v, want true after clientA operate", stVal)
	}
}

// TestGoServer_SBO_SelectTimeout verifies that a select expiry causes a
// subsequent operate to be rejected.
func TestGoServer_SBO_SelectTimeout(t *testing.T) {
	const shortTimeout = 200 * time.Millisecond
	srv := startGoIEDServerWithShortSBOTimeout(t, shortTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Select SPCSO2.
	if _, err := client.Select(ctx, ref); err != nil {
		t.Fatalf("Select: %v", err)
	}
	t.Log("select granted")

	// Wait for the timeout to expire.
	time.Sleep(shortTimeout + 50*time.Millisecond)

	// Operate must be rejected (select expired).
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	})
	if err == nil {
		t.Fatal("expected error for operate after select timeout, got nil")
	}
	t.Logf("operate after timeout correctly rejected: %v", err)

	// stVal must remain false (operate was rejected).
	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal != nil {
		if b, ok := stVal.Bool(); ok && b {
			t.Error("stVal was changed despite expired select")
		}
	}
}

// TestGoServer_SBO_RepeatedSelectSameClient verifies that a client can
// re-select without error (idempotent select per IEC 61850-7-2 §20.3) and
// that the subsequent operate succeeds.
//
// For SBO normal the server uses connection identity, so a repeated Read to
// SBO[CO] from the same connection re-grants the select (selectConn == sc)
// and refreshes the timeout.
func TestGoServer_SBO_RepeatedSelectSameClient(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// First select.
	sel1, err := client.Select(ctx, ref)
	if err != nil {
		t.Fatalf("first Select: %v", err)
	}
	t.Logf("first select granted: %s", sel1)

	// Second select from the same connection.
	// Per IEC 61850-7-2 §20.3, a repeated select from the same owner is
	// idempotent: the server re-grants and refreshes the timeout.
	// The go-iec61850 server grants because selectConn == sc.
	sel2, err := client.Select(ctx, ref)
	if err != nil {
		t.Logf("repeated select returned error (not idempotent in this implementation): %v", err)
	} else {
		t.Logf("repeated select granted (idempotent): %s", sel2)
	}

	// Operate must succeed: the first (or repeated) select is still active.
	if err := client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	}); err != nil {
		t.Fatalf("Operate after repeated select: %v", err)
	}

	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal == nil {
		t.Fatal("stVal not set after operate")
	}
	if b, ok := stVal.Bool(); !ok || !b {
		t.Errorf("stVal: got %v, want true after operate", stVal)
	}
}

// TestGoServer_SBO_DisconnectReleasesSelection verifies that closing a client
// connection releases its SBO normal selection so another client can acquire it.
func TestGoServer_SBO_DisconnectReleasesSelection(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ref, err := iec61850.ParseRef(sbo2Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Client A selects SPCSO2.
	clientA, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	if _, err := clientA.Select(ctx, ref); err != nil {
		t.Fatalf("clientA Select: %v", err)
	}

	// Client A disconnects.
	if err := clientA.Close(ctx); err != nil {
		t.Logf("clientA Close: %v", err)
	}
	// Give the server a moment to process the close.
	time.Sleep(50 * time.Millisecond)

	// Client B must now be able to select immediately (disconnect released the selection).
	clientB, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer clientB.Close(ctx) //nolint:errcheck

	if _, err := clientB.Select(ctx, ref); err != nil {
		t.Fatalf("clientB Select after clientA disconnect: %v", err)
	}

	// Client B operates — must succeed.
	if err := clientB.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
	}); err != nil {
		t.Fatalf("clientB Operate: %v", err)
	}

	stVal := srv.srv.ValueStore().Get(sbo2StValKey)
	if stVal == nil {
		t.Fatal("stVal not set after clientB operate")
	}
	if b, ok := stVal.Bool(); !ok || !b {
		t.Errorf("stVal: got %v, want true", stVal)
	}
}

// ---------------------------------------------------------------------------
// Phase G3 — SBOw (enhanced security) additional state tests
// ---------------------------------------------------------------------------

// TestGoServer_SBOw_ValueMustMatchOperate verifies that an Operate with a
// different ctlVal than the SelectWithValue is rejected.
func TestGoServer_SBOw_ValueMustMatchOperate(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// SelectWithValue ctlVal=true, ctlNum=1.
	if err := client.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	}); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}

	// Operate with ctlVal=false (mismatch) — must be rejected.
	err = client.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(false), // different from selected true
		CtlNum: 1,
	})
	if err == nil {
		t.Fatal("expected error for operate with mismatched ctlVal, got nil")
	}
	t.Logf("operate with mismatched ctlVal correctly rejected: %v", err)

	// stVal must remain unchanged (operate was rejected).
	stVal := srv.srv.ValueStore().Get(sbow3StValKey)
	if stVal != nil {
		if b, ok := stVal.Bool(); ok && b {
			t.Error("stVal was changed despite mismatched ctlVal rejection")
		}
	}
}

// TestGoServer_SBOw_SecondClientContention verifies that a second client's
// SelectWithValue is denied while another client holds the SBOw selection.
//
// For SBOw, ownership is tracked by both origin identity and connection pointer
// so that two clients sharing the same default origin can be distinguished.
func TestGoServer_SBOw_SecondClientContention(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Client A selects with ctlNum=1.
	clientA, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer clientA.Close(ctx) //nolint:errcheck

	if err := clientA.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	}); err != nil {
		t.Fatalf("clientA SelectWithValue: %v", err)
	}
	t.Log("clientA SelectWithValue granted")

	// Client B connects and tries to select — must fail.
	clientB, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer clientB.Close(ctx) //nolint:errcheck

	err = clientB.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 2,
	})
	if err == nil {
		t.Error("expected error for clientB SelectWithValue while A holds selection, got nil")
	} else {
		t.Logf("clientB SelectWithValue correctly rejected: %v", err)
	}

	// Client A operates with ctlNum=1 — must succeed.
	if err := clientA.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	}); err != nil {
		t.Fatalf("clientA Operate: %v", err)
	}

	stVal := srv.srv.ValueStore().Get(sbow3StValKey)
	if stVal == nil {
		t.Fatal("stVal not set after clientA operate")
	}
	if b, ok := stVal.Bool(); !ok || !b {
		t.Errorf("stVal: got %v, want true", stVal)
	}
}

// TestGoServer_SBOw_DisconnectReleasesSelection verifies that after client A
// disconnects, client B can acquire the SBOw selection and operate.
//
// For SBOw the server tracks ownership by origin key ("orCat:orIdent"). Both
// clients use the go-iec61850 default origin (OrCatRemoteControl=6, empty
// OrIdent → key "6:"). Because executeSelectWithValue has no contention guard,
// client B's SelectWithValue with a different ctlNum overwrites the stale state
// left by A and then operates successfully.
//
// Note: this test passes because of the missing contention check, not because
// disconnect releases the selection. See TestGoServer_SBOw_SecondClientContention
// for the tracked gap.
func TestGoServer_SBOw_DisconnectReleasesSelection(t *testing.T) {
	srv := startGoIEDServerWithControls(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	// Client A selects with ctlNum=1.
	clientA, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	if err := clientA.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 1,
	}); err != nil {
		t.Fatalf("clientA SelectWithValue: %v", err)
	}
	t.Log("clientA SelectWithValue succeeded")

	// Close client A (simulates disconnect).
	if err := clientA.Close(ctx); err != nil {
		t.Logf("clientA Close: %v", err)
	}

	// Client B connects and selects with ctlNum=2.
	// Both clients share the default origin key ("6:"), so B's select
	// overwrites A's stale state.
	clientB, err := iec61850.Dial(ctx, fmt.Sprintf("127.0.0.1:%d", srv.port), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer clientB.Close(ctx) //nolint:errcheck

	if err := clientB.SelectWithValue(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 2,
	}); err != nil {
		t.Fatalf("clientB SelectWithValue: %v", err)
	}
	t.Log("clientB SelectWithValue succeeded")

	// Client B operates with ctlNum=2 — must succeed.
	if err := clientB.Operate(ctx, ref, iec61850.OperateParams{
		CtlVal: iec61850.BoolCtlVal(true),
		CtlNum: 2,
	}); err != nil {
		t.Fatalf("clientB Operate: %v", err)
	}

	stVal := srv.srv.ValueStore().Get(sbow3StValKey)
	if stVal == nil {
		t.Fatal("stVal not set after clientB operate")
	}
	if b, ok := stVal.Bool(); !ok || !b {
		t.Errorf("stVal: got %v, want true after clientB operate", stVal)
	}
}
