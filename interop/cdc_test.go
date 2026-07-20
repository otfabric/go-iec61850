//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase C3 (DPS and BCR CDCs) and Phase C4
// (quality and timestamp semantics).
//
// Phase C3 — go-to-go tests only (adapter containers do not yet carry
// the DPS1/BCR1 fixture additions):
//   - TestGoServer_CDC_DPS_Read: read DPS1.stVal from go-iec61850 server.
//   - TestGoServer_CDC_BCR_Read: read MMTR1.TotVAh.actVal from go-iec61850 server.
//   - Adapter-facing variants are skipped with a TODO until mms-interop is rebuilt.
//
// Phase C4 — go-to-go only:
//   - TestGoServer_Quality_Round_Trip: write SPS1.q, read it back via client.
//   - TestGoServer_Timestamp_Round_Trip: write SPS1.t, read it back via client.
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// ---------------------------------------------------------------------------
// Phase C3 — DPS (Double-point status)
// ---------------------------------------------------------------------------

// TestGoServer_CDC_DPS_Read reads DPS1.stVal from the go-iec61850 server and
// verifies it matches the fixture value (2 = "on", encoded as Dbpos BitString).
func TestGoServer_CDC_DPS_Read(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	ref, err := iec61850.ParseRef("InteropLD/GGIO1.DPS1.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("ReadRaw DPS1.stVal: %v", err)
	}
	t.Logf("DPS1.stVal raw type=%v value=%v", raw.Type(), raw)

	// Decode the Dbpos 2-bit BitString to an integer.
	// Bit pattern (MSB-first): 00=intermediate, 01=off, 10=on, 11=bad-state.
	bits, ok := raw.BitString()
	if !ok {
		t.Fatalf("expected BitString for Dbpos, got %v", raw.Type())
	}
	if len(bits) == 0 {
		t.Fatal("empty Dbpos BitString")
	}
	// Extract the 2-bit value from the high bits of the first byte.
	dbposInt := int64(bits[0] >> 6)
	t.Logf("DPS1.stVal decoded integer=%d (fixture=%d)", dbposInt, fixVal.DPS1StVal)

	if dbposInt != fixVal.DPS1StVal {
		t.Errorf("DPS1.stVal: want %d, got %d", fixVal.DPS1StVal, dbposInt)
	}
}

// TestLibIECClient_CDC_DPS_Read is skipped until mms-interop is rebuilt with
// the DPS1 fixture addition. TODO: remove skip once adapters carry the new ICD.
func TestLibIECClient_CDC_DPS_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// TestBeanClient_CDC_DPS_Read is skipped until mms-interop is rebuilt.
func TestBeanClient_CDC_DPS_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// TestLibIECServer_CDC_DPS_Read is skipped until mms-interop is rebuilt.
func TestLibIECServer_CDC_DPS_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// TestBeanServer_CDC_DPS_Read is skipped until mms-interop is rebuilt.
func TestBeanServer_CDC_DPS_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// ---------------------------------------------------------------------------
// Phase C3 — BCR (Binary counter reading)
// ---------------------------------------------------------------------------

// TestGoServer_CDC_BCR_Read reads MMTR1.TotVAh.actVal from the go-iec61850
// server and verifies it matches the fixture value (42, INT32U).
func TestGoServer_CDC_BCR_Read(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	ref, err := iec61850.ParseRef("InteropLD/MMTR1.TotVAh.actVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("ReadRaw TotVAh.actVal: %v", err)
	}
	t.Logf("TotVAh.actVal raw type=%v", raw.Type())

	u, ok := raw.Uint64()
	if !ok {
		t.Fatalf("expected Unsigned for INT32U actVal, got %v", raw.Type())
	}
	t.Logf("TotVAh.actVal=%d (fixture=%d)", u, fixVal.BCRActVal)

	if u != fixVal.BCRActVal {
		t.Errorf("TotVAh.actVal: want %d, got %d", fixVal.BCRActVal, u)
	}
}

// TestLibIECClient_CDC_BCR_Read is skipped until mms-interop is rebuilt.
func TestLibIECClient_CDC_BCR_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// TestBeanClient_CDC_BCR_Read is skipped until mms-interop is rebuilt.
func TestBeanClient_CDC_BCR_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// TestLibIECServer_CDC_BCR_Read is skipped until mms-interop is rebuilt.
func TestLibIECServer_CDC_BCR_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// TestBeanServer_CDC_BCR_Read is skipped until mms-interop is rebuilt.
func TestBeanServer_CDC_BCR_Read(t *testing.T) {
	t.Skip("TODO: requires mms-interop rebuild for DPS/BCR fixture")
}

// Phase C4 quality and timestamp tests are in quality_timestamp_test.go.
