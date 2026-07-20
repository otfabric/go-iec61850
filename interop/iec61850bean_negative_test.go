//go:build interop

// SPDX-License-Identifier: MIT

// Phase 2F — go-iec61850 client negative tests (iec61850bean IED server direction)
//
// Tests that the go-iec61850 client returns typed errors on invalid requests
// against the iec61850bean server and that the association remains usable
// after each error.
//
// Assertion style: assert error returned (not nil); check association survives
// by issuing a valid read after each failure.
//
// Bean-specific: use defer c.Abort(context.Background()) instead of
// defer c.Close(ctx), matching the pattern of all other bean tests.
package interop

import (
	"context"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// Phase 2F — go-iec61850 client ← iec61850bean IED server (negative tests)
// ---------------------------------------------------------------------------

// TestBeanClient_Neg_UnknownLD reads from a non-existent logical device.
// The server must return an error and the association must remain open.
func TestBeanClient_Neg_UnknownLD(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("NoSuchLD/GGIO1.SPS1.stVal[ST]")
	_, err := c.ReadRaw(ctx, ref)
	if err == nil {
		t.Error("expected error reading from non-existent LD, got nil")
	} else {
		t.Logf("correctly rejected unknown-LD read: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("association broken after unknown-LD read: %v", err)
	}
}

// TestBeanClient_Neg_UnknownLN reads from a non-existent logical node in a known LD.
func TestBeanClient_Neg_UnknownLN(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/NoSuchLN.SPS1.stVal[ST]")
	_, err := c.ReadRaw(ctx, ref)
	if err == nil {
		t.Error("expected error reading from non-existent LN, got nil")
	} else {
		t.Logf("correctly rejected unknown-LN read: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("association broken after unknown-LN read: %v", err)
	}
}

// TestBeanClient_Neg_UnknownDO reads from a non-existent data object path in a known LN.
// NOTE: iec61850bean terminates the MMS association on an unknown-DO read rather than
// returning an error response. This is a known iec61850bean quirk; we document it here
// rather than asserting that the association survives.
func TestBeanClient_Neg_UnknownDO(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.NoSuchDO.stVal[ST]")
	_, err := c.ReadRaw(ctx, ref)
	if err == nil {
		t.Error("expected error reading non-existent DO, got nil")
	} else {
		t.Logf("correctly rejected unknown-DO read: %v", err)
	}

	// iec61850bean drops the connection on an unrecognised object reference.
	// Verify the association is indeed broken (a follow-up read fails).
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Logf("association broken after unknown-DO read (expected iec61850bean behaviour): %v", err)
	} else {
		t.Log("association survived unknown-DO read (iec61850bean may have been updated to send error response)")
	}
}

// TestBeanClient_Neg_WriteReadOnly attempts to write to a read-only ST attribute.
// The iec61850bean server enforces IEC 61850 write access: ST (Status) attributes
// may not be written by external clients, even when the value type is correct.
func TestBeanClient_Neg_WriteReadOnly(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")

	// SPS1.stVal[ST] is a boolean attribute that is read-only from external
	// clients on the iec61850bean server (ST = Status, server-owned).
	err := c.Write(ctx, ref, mms.NewBoolean(true))
	if err == nil {
		t.Error("expected error writing to read-only ST attribute, got nil")
	} else {
		t.Logf("correctly rejected write to read-only ST attribute: %v", err)
	}

	// Association must survive and value must be unchanged.
	raw, readErr := c.ReadRaw(ctx, ref)
	if readErr != nil {
		t.Fatalf("read after rejected write: %v", readErr)
	}
	b, ok := raw.Bool()
	if !ok {
		t.Fatalf("expected boolean after rejected write, got type %s", raw.Type())
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("SPS1.stVal after rejected write: want %v (unchanged), got %v", fixVal.SPS1StVal, b)
	}
}

// TestBeanClient_Neg_InvalidDataSet reads a dataset that does not exist.
func TestBeanClient_Neg_InvalidDataSet(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	_, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$noSuchDataSet")
	if err == nil {
		t.Error("expected error reading non-existent dataset, got nil")
	} else {
		t.Logf("correctly rejected non-existent dataset read: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("association broken after invalid dataset read: %v", err)
	}
}

// TestBeanClient_Neg_URCBDoubleReserve opens two clients and attempts to
// reserve the same URCB from both. The second reservation must be rejected.
//
// Note: this test requires the iec61850bean server to expose a URCB with the
// known urcbID ("LLN0$RP$urcb0101"). If the bean server does not implement
// URCB reservation, the test is skipped.
func TestBeanClient_Neg_URCBDoubleReserve(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c1 := h.dial(t, ctx)
	defer c1.Abort(context.Background())
	c2 := h.dial(t, ctx)
	defer c2.Abort(context.Background())

	// Probe whether this bean server instance exposes the URCB.
	if err := c1.ReserveURCB(ctx, urcbLD, urcbID); err != nil {
		t.Skipf("iec61850bean server does not support URCB reservation (%v); skipping double-reserve test", err)
	}

	// Client 2 reservation must fail since client 1 already holds it.
	err := c2.ReserveURCB(ctx, urcbLD, urcbID)
	if err == nil {
		t.Error("expected error reserving already-reserved URCB from second client, got nil")
	} else {
		t.Logf("correctly rejected double-reserve: %v", err)
	}

	// Release the reservation so the shared bean server is left in a clean state.
	if releaseErr := c1.SetReportControlBlock(ctx, urcbLD, urcbID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldResv,
		Resv:   false,
	}); releaseErr != nil {
		t.Logf("release URCB reservation: %v (non-fatal)", releaseErr)
	}
}
