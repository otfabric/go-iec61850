//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file read the DPS (GGIO1.DPS1.stVal, Dbpos) and BCR
// (MMTR1.TotVAh.actVal) CDCs of fixture iec61850-v3 and compare them with
// values.json, in every direction:
//
//   - TestGoServer_CDC_*: go-iec61850 client → go-iec61850 server.
//   - TestLibIECClient_CDC_* / TestBeanClient_CDC_*: go-iec61850 client →
//     the peer stack's (or iec61850bean's) IED server. Skipped where the
//     adapter server does not seed the values (cdcSeedGap).
//   - TestLibIECServer_CDC_* / TestBeanServer_CDC_*: the peer stack's (or
//     iec61850bean's) ied-client → go-iec61850 server. They assert the
//     client's read of the reference and skip, naming the gap, while the
//     adapter's fixed client script has no such read step.
//
// Quality and timestamp round trips are in quality_timestamp_test.go.
package interop

import (
	"context"
	"encoding/json"
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
// initial values of values.json (checked against mms-interop v0.4.0): they
// serve type defaults, so the read cannot be compared with the fixture.
// dscsystems and iec61850-rust seed both values and are tested.
var cdcSeedGap = map[string]string{
	"libiec61850":  "mms-interop gap: libiec61850-ied-server (v0.4.0) does not seed values.json DPS1.stVal=2 / TotVAh.actVal=42; it serves 0 (intermediate) / 0",
	"iec61850bean": "mms-interop gap: iec61850bean-ied-server (v0.4.0) does not seed values.json DPS1.stVal=2 / TotVAh.actVal=42; it serves 1 (off) / 0",
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

// Targets of the server-direction CDC reads, in the ied-client's
// "read" result format.
const (
	cdcDPSTarget = "InteropLD/GGIO1.DPS1.stVal[ST]"
	cdcBCRTarget = "InteropLD/MMTR1.TotVAh.actVal[ST]"
)

// clientCDCRead runs the ied-client adapter (the peer stack's, or
// iec61850bean's when bean is set) against the Go IED server and returns
// the value of its read of target. It skips, naming the gap, when the
// adapter's fixed client script (--host/--port only, checked against
// mms-interop v0.4.0) has no read of target.
func clientCDCRead(t *testing.T, bean bool, target string) json.RawMessage {
	t.Helper()
	srv := startGoIEDServer(t)
	var ch <-chan iedClientResult
	stack := peer().name
	if bean {
		ch, stack = startIEC61850BeanClientAdapter(t, srv.port), "iec61850bean"
	} else {
		ch = startIEDClientAdapter(t, srv.port)
	}
	r, ok := findIEDResult(t, collectIEDResults(t, ch), "read", target)
	if !ok {
		t.Skipf("mms-interop gap: %s-ied-client has no read step for %s (fixture iec61850-v3)", stack, target)
	}
	if !r.OK {
		t.Fatalf("%s-ied-client read %s: %s", stack, target, r.Error)
	}
	return r.Value
}

// dbposNames maps the Dbpos names an adapter may print to their values.
var dbposNames = map[string]int64{"intermediate": 0, "intermediate-state": 0, "off": 1, "on": 2, "bad": 3, "bad-state": 3}

// checkClientDPS compares an ied-client's DPS1.stVal read (a number or a
// Dbpos name) with the fixture.
func checkClientDPS(t *testing.T, v json.RawMessage) {
	t.Helper()
	var got int64
	var n float64
	var name string
	switch {
	case json.Unmarshal(v, &n) == nil:
		got = int64(n)
	case json.Unmarshal(v, &name) == nil:
		d, ok := dbposNames[name]
		if !ok {
			t.Fatalf("DPS1.stVal: unknown Dbpos %q", name)
		}
		got = d
	default:
		t.Fatalf("DPS1.stVal: unexpected value %s", v)
	}
	if got != fixVal.DPS1StVal {
		t.Errorf("DPS1.stVal: want %d, got %d (%s)", fixVal.DPS1StVal, got, v)
	}
}

// checkClientBCR compares an ied-client's TotVAh.actVal read with the
// fixture.
func checkClientBCR(t *testing.T, v json.RawMessage) {
	t.Helper()
	var n float64
	if err := json.Unmarshal(v, &n); err != nil {
		t.Fatalf("TotVAh.actVal: want a number, got %s", v)
	}
	if uint64(n) != fixVal.BCRActVal {
		t.Errorf("TotVAh.actVal: want %d, got %s", fixVal.BCRActVal, v)
	}
}

// TestLibIECServer_CDC_DPS_Read: the peer stack's ied-client reads DPS1
// from the Go server.
func TestLibIECServer_CDC_DPS_Read(t *testing.T) {
	checkClientDPS(t, clientCDCRead(t, false, cdcDPSTarget))
}

// TestBeanServer_CDC_DPS_Read: the iec61850bean ied-client reads DPS1
// from the Go server.
func TestBeanServer_CDC_DPS_Read(t *testing.T) {
	checkClientDPS(t, clientCDCRead(t, true, cdcDPSTarget))
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

// TestLibIECServer_CDC_BCR_Read: the peer stack's ied-client reads TotVAh
// from the Go server.
func TestLibIECServer_CDC_BCR_Read(t *testing.T) {
	checkClientBCR(t, clientCDCRead(t, false, cdcBCRTarget))
}

// TestBeanServer_CDC_BCR_Read: the iec61850bean ied-client reads TotVAh
// from the Go server.
func TestBeanServer_CDC_BCR_Read(t *testing.T) {
	checkClientBCR(t, clientCDCRead(t, true, cdcBCRTarget))
}
