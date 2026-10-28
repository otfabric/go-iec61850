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

// cdcSeedGap lists adapter servers that do not apply the DPS1/TotVAh
// initial values of values.json (mms-interop v0.4.0): they serve the type
// defaults, so the read cannot be compared with the fixture.
var cdcSeedGap = map[string]string{
	"libiec61850":  "libiec61850-ied-server (mms-interop v0.4.0) does not seed GGIO1.DPS1.stVal / MMTR1.TotVAh.actVal from values.json",
	"iec61850bean": "iec61850bean-ied-server (mms-interop v0.4.0) does not seed GGIO1.DPS1.stVal / MMTR1.TotVAh.actVal from values.json",
}

func skipCDCSeedGap(t *testing.T, stack string) {
	t.Helper()
	if reason, ok := cdcSeedGap[stack]; ok {
		t.Skip(reason)
	}
}

// checkDPS reads GGIO1.DPS1.stVal through c and compares the Dbpos with
// the fixture.
func checkDPS(t *testing.T, ctx context.Context, c *iec61850.Client) {
	t.Helper()
	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.DPS1.stVal[ST]")
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("ReadRaw DPS1.stVal: %v", err)
	}
	bits, ok := raw.BitString()
	if n, _ := raw.BitStringLength(); !ok || n != 2 || len(bits) == 0 {
		t.Fatalf("DPS1.stVal: want a 2-bit Dbpos bit-string, got %v", raw)
	}
	if got := int64(bits[0] >> 6); got != fixVal.DPS1StVal {
		t.Errorf("DPS1.stVal: want %d, got %d", fixVal.DPS1StVal, got)
	}
}

// checkBCR reads MMTR1.TotVAh.actVal through c and compares it with the
// fixture. Stacks map INT32U to Unsigned; an Integer is accepted too.
func checkBCR(t *testing.T, ctx context.Context, c *iec61850.Client) {
	t.Helper()
	ref, _ := iec61850.ParseRef("InteropLD/MMTR1.TotVAh.actVal[ST]")
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("ReadRaw TotVAh.actVal: %v", err)
	}
	u, ok := raw.Uint64()
	if !ok {
		i, iok := raw.Int64()
		if !iok || i < 0 {
			t.Fatalf("TotVAh.actVal: want an integer, got %v", raw)
		}
		u = uint64(i)
	}
	if u != fixVal.BCRActVal {
		t.Errorf("TotVAh.actVal: want %d, got %d", fixVal.BCRActVal, u)
	}
}

// TestLibIECClient_CDC_DPS_Read reads DPS1 from the peer stack's server.
func TestLibIECClient_CDC_DPS_Read(t *testing.T) {
	skipCDCSeedGap(t, peer().name)
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)
	checkDPS(t, ctx, c)
}

// TestBeanClient_CDC_DPS_Read reads DPS1 from the iec61850bean server.
func TestBeanClient_CDC_DPS_Read(t *testing.T) {
	skipCDCSeedGap(t, "iec61850bean")
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())
	checkDPS(t, ctx, c)
}

// adapterClientNoCDC is the skip reason of the server-direction CDC
// tests: the ied-client adapters run a fixed script without these reads.
const adapterClientNoCDC = "the ied-client adapters have no DPS1/TotVAh read step; the Go server side is covered by TestGoServer_CDC_*"

func TestLibIECServer_CDC_DPS_Read(t *testing.T) { t.Skip(adapterClientNoCDC) }

func TestBeanServer_CDC_DPS_Read(t *testing.T) { t.Skip(adapterClientNoCDC) }

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

// TestLibIECClient_CDC_BCR_Read reads TotVAh from the peer stack's server.
func TestLibIECClient_CDC_BCR_Read(t *testing.T) {
	skipCDCSeedGap(t, peer().name)
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)
	checkBCR(t, ctx, c)
}

// TestBeanClient_CDC_BCR_Read reads TotVAh from the iec61850bean server.
func TestBeanClient_CDC_BCR_Read(t *testing.T) {
	skipCDCSeedGap(t, "iec61850bean")
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())
	checkBCR(t, ctx, c)
}

func TestLibIECServer_CDC_BCR_Read(t *testing.T) { t.Skip(adapterClientNoCDC) }

func TestBeanServer_CDC_BCR_Read(t *testing.T) { t.Skip(adapterClientNoCDC) }

// Phase C4 quality and timestamp tests are in quality_timestamp_test.go.
