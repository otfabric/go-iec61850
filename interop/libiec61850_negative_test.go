//go:build interop

// SPDX-License-Identifier: MIT

// Phase 2F — go-iec61850 client negative tests (libiec61850 IED server direction)
//
// Tests that the go-iec61850 client returns typed errors on invalid requests
// and that the association remains usable after each error.
//
// Assertion style: assert error returned (not nil); check association survives
// by issuing a valid read after each failure.
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// Phase 2F — go-iec61850 client ← libiec61850 IED server (negative tests)
// ---------------------------------------------------------------------------

// TestLibIECClient_Neg_UnknownLD reads from a non-existent logical device.
// The server must return an error and the association must remain open.
func TestLibIECClient_Neg_UnknownLD(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Neg_UnknownLN reads from a non-existent logical node in a known LD.
func TestLibIECClient_Neg_UnknownLN(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Neg_UnknownDO reads from a non-existent data object path in a known LN.
func TestLibIECClient_Neg_UnknownDO(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.NoSuchDO.stVal[ST]")
	_, err := c.ReadRaw(ctx, ref)
	if err == nil {
		t.Error("expected error reading non-existent DO, got nil")
	} else {
		t.Logf("correctly rejected unknown-DO read: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("association broken after unknown-DO read: %v", err)
	}
}

// TestLibIECClient_Neg_WriteReadOnly attempts to write to a read-only ST attribute.
// The libiec61850 server enforces IEC 61850 write access: ST (Status) attributes
// may not be written by external clients, even when the value type is correct.
func TestLibIECClient_Neg_WriteReadOnly(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")

	// SPS1.stVal[ST] is a boolean attribute that is read-only from external
	// clients on the libiec61850 server (ST = Status, server-owned).
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

// TestLibIECClient_Neg_InvalidDataSet reads a dataset that does not exist.
func TestLibIECClient_Neg_InvalidDataSet(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Neg_URCBDoubleReserve opens two clients and attempts to
// reserve the same URCB from both. The second reservation must be rejected.
func TestLibIECClient_Neg_URCBDoubleReserve(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c1 := h.dial(t, ctx)
	defer c1.Close(ctx)
	c2 := h.dial(t, ctx)
	defer c2.Close(ctx)

	// Client 1 reserves successfully.
	if err := c1.ReserveURCB(ctx, urcbLD, urcbID); err != nil {
		t.Fatalf("client1 ReserveURCB: %v", err)
	}

	// Client 2 reservation must fail.
	err := c2.ReserveURCB(ctx, urcbLD, urcbID)
	if err == nil {
		t.Error("expected error reserving already-reserved URCB from second client, got nil")
	} else {
		t.Logf("correctly rejected double-reserve: %v", err)
	}

	// Release the reservation so the URCB is left in a clean state.
	if releaseErr := c1.SetReportControlBlock(ctx, urcbLD, urcbID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldResv,
		Resv:   false,
	}); releaseErr != nil {
		t.Logf("release URCB reservation: %v (non-fatal)", releaseErr)
	}
}

// ---------------------------------------------------------------------------
// Phase 2F — go-iec61850 server negative tests (go-iec61850 client direction)
// ---------------------------------------------------------------------------

// TestGoServer_Neg_UnknownLD verifies go-iec61850 server returns an error for
// reads on a non-existent logical device and keeps the association alive.
func TestGoServer_Neg_UnknownLD(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("NoSuchLD/GGIO1.SPS1.stVal[ST]")
	_, err := c.ReadRaw(ctx, ref)
	if err == nil {
		t.Error("expected error reading from non-existent LD on go server, got nil")
	} else {
		t.Logf("go server correctly rejected unknown-LD: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("go server: association broken after unknown-LD read: %v", err)
	}
}

// TestGoServer_Neg_UnknownLN verifies the go-iec61850 server returns an error
// for reads on a non-existent logical node and keeps the association alive.
func TestGoServer_Neg_UnknownLN(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("InteropLD/NoSuchLN.SPS1.stVal[ST]")
	_, err := c.ReadRaw(ctx, ref)
	if err == nil {
		t.Error("expected error reading from non-existent LN on go server, got nil")
	} else {
		t.Logf("go server correctly rejected unknown-LN: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("go server: association broken after unknown-LN read: %v", err)
	}
}

// TestGoServer_Neg_WriteReadOnly writes a type-mismatched value to an ST
// attribute and verifies that the go-iec61850 server rejects it and the
// association remains alive. The go server enforces type safety: writing a
// boolean to an INT32 attribute (LLN0.Mod.stVal[ST]) must fail.
func TestGoServer_Neg_WriteReadOnly(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.stVal[ST]")

	// Write a boolean to an INT32 attribute — the go server enforces type safety.
	err := c.Write(ctx, ref, mms.NewBoolean(true))
	if err == nil {
		t.Error("expected type-mismatch error writing boolean to INT32 ST attribute, got nil")
	} else {
		t.Logf("go server correctly rejected type-mismatched write: %v", err)
	}

	// Association must survive and value must be unchanged.
	raw, readErr := c.ReadRaw(ctx, ref)
	if readErr != nil {
		t.Fatalf("read after rejected write: %v", readErr)
	}
	iv, ok := raw.Int64()
	if !ok {
		t.Fatalf("expected integer after rejected write, got type %s", raw.Type())
	}
	if iv != fixVal.ModStVal {
		t.Errorf("go server LLN0.Mod.stVal after rejected write: want %d (unchanged), got %d", fixVal.ModStVal, iv)
	}
}

// TestGoServer_Neg_InvalidDataSet reads a dataset that does not exist on the
// go-iec61850 server and verifies an error is returned with the association intact.
func TestGoServer_Neg_InvalidDataSet(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	_, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$noSuchDataSet")
	if err == nil {
		t.Error("expected error reading non-existent dataset on go server, got nil")
	} else {
		t.Logf("go server correctly rejected non-existent dataset read: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c.ReadRaw(ctx, okRef); err != nil {
		t.Errorf("go server: association broken after invalid dataset read: %v", err)
	}
}
