//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2B — Step 9a:
//
//	go-iec61850 client ← iec61850bean IED server
//
// Each test starts the iec61850bean IED server adapter (Docker or local
// binary via IEC61850BEAN_SERVER_BINARY), waits for the JSON readiness
// event, and then exercises the go-iec61850 client API.
//
// The assertions mirror Phase 2A (go_iec61850_client_test.go) so that both
// independent server implementations are held to the same contract.
package interop

import (
	"context"
	"math"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// Phase 2B — go-iec61850 client ← iec61850bean IED server
// ---------------------------------------------------------------------------

func TestBeanClient_ListLogicalDevices(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	lds, err := c.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
	t.Logf("logical devices: %v", lds)

	found := false
	for _, ld := range lds {
		if ld.Name == "InteropLD" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected logical device 'InteropLD' in %v", lds)
	}
}

func TestBeanClient_ListLogicalNodes(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	lns, err := c.ListLogicalNodes(ctx, "InteropLD")
	if err != nil {
		t.Fatalf("ListLogicalNodes(InteropLD): %v", err)
	}
	t.Logf("logical nodes: %v", lns)

	want := map[string]bool{"LLN0": false, "GGIO1": false, "MMXU1": false}
	for _, ln := range lns {
		want[ln.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected logical node %q in InteropLD, got %v", name, lns)
		}
	}
}

func TestBeanClient_ListDataObjects_GGIO1(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	dos, err := c.ListDataObjects(ctx, "InteropLD", "GGIO1")
	if err != nil {
		t.Fatalf("ListDataObjects(InteropLD, GGIO1): %v", err)
	}
	t.Logf("data objects in GGIO1: %v", dos)

	want := map[string]bool{"SPS1": false, "SPCSO1": false}
	for _, do := range dos {
		want[do.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected data object %q in GGIO1, got %v", name, dos)
		}
	}
}

func TestBeanClient_Read_ST(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("Read GGIO1.SPS1.stVal[ST]: %v", err)
	}
	t.Logf("GGIO1.SPS1.stVal = %s", raw)

	b, ok := raw.Bool()
	if !ok {
		t.Fatalf("expected boolean, got type %s", raw.Type())
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("GGIO1.SPS1.stVal: want %v, got %v", fixVal.SPS1StVal, b)
	}
}

func TestBeanClient_Read_MX(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/MMXU1.TotW.mag.f[MX]")
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("Read MMXU1.TotW.mag.f[MX]: %v", err)
	}
	t.Logf("MMXU1.TotW.mag.f = %s", raw)

	fv, ok := raw.Float64()
	if !ok {
		t.Fatalf("expected float, got type %s", raw.Type())
	}
	if math.Abs(fv-fixVal.TotWMagF) > 0.1 {
		t.Errorf("MMXU1.TotW.mag.f: want ~%g, got %g", fixVal.TotWMagF, fv)
	}
}

func TestBeanClient_Read_CF(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("Read LLN0.Mod.ctlModel[CF]: %v", err)
	}
	t.Logf("LLN0.Mod.ctlModel = %s", raw)

	iv, ok := raw.Int64()
	if !ok {
		t.Fatalf("expected integer, got type %s", raw.Type())
	}
	if iv != fixVal.ModCtlModel {
		t.Errorf("LLN0.Mod.ctlModel: want %d, got %d", fixVal.ModCtlModel, iv)
	}
}

func TestBeanClient_Read_DC(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.d[DC]")
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("Read LLN0.Mod.d[DC]: %v", err)
	}
	t.Logf("LLN0.Mod.d = %s", raw)

	sv, ok := raw.VisibleString()
	if !ok {
		t.Fatalf("expected visible-string, got type %s", raw.Type())
	}
	if sv != fixVal.ModD {
		t.Errorf("LLN0.Mod.d: want %q, got %q", fixVal.ModD, sv)
	}
}

func TestBeanClient_Write(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	// iec61850bean enforces IEC 61850 write semantics: ST (Status) attributes
	// are read-only from the client side. Use CF (Configuration) instead.
	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")
	newVal := fixVal.ModCtlModel + 1

	if err := c.Write(ctx, ref, mms.NewInteger(newVal)); err != nil {
		t.Fatalf("Write LLN0.Mod.ctlModel[CF]=%d: %v", newVal, err)
	}

	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("Read LLN0.Mod.ctlModel[CF] after write: %v", err)
	}

	iv, ok := raw.Int64()
	if !ok {
		t.Fatalf("expected integer after write, got type %s", raw.Type())
	}
	if iv != newVal {
		t.Errorf("LLN0.Mod.ctlModel after write: want %d, got %d", newVal, iv)
	}

	// Restore original value so subsequent tests see a predictable state.
	if err := c.Write(ctx, ref, mms.NewInteger(fixVal.ModCtlModel)); err != nil {
		t.Logf("restore LLN0.Mod.ctlModel[CF]=%d: %v (non-fatal)", fixVal.ModCtlModel, err)
	}
}

func TestBeanClient_ListDataSets(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	dss, err := c.ListDataSets(ctx, "InteropLD")
	if err != nil {
		t.Fatalf("ListDataSets(InteropLD): %v", err)
	}
	t.Logf("datasets: %v", dss)

	found := false
	for _, ds := range dss {
		if ds == "dsInterop" || ds == "LLN0$dsInterop" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dataset 'dsInterop' in %v", dss)
	}
}

// TestBeanClient_Write_TypeMismatch writes a boolean value to an INT32
// attribute (LLN0.Mod.stVal[ST]) and verifies that:
//
//	(a) the iec61850bean server rejects it (type-inconsistent);
//	(b) the association remains usable and still returns the original value.
//
// This proves that the unconditional-null ServerEventListener does not bypass
// the library's own MMS-layer type enforcement, and that a single rejected
// write does not break the session.
func TestBeanClient_Write_TypeMismatch(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.stVal[ST]")

	// Write a boolean to an INT32 attribute — should be type-inconsistent.
	err := c.Write(ctx, ref, mms.NewBoolean(true))
	if err == nil {
		t.Error("expected type-inconsistent error writing boolean to INT32 attribute, got nil")
	} else {
		t.Logf("correctly rejected type-mismatched write: %v", err)
	}

	// The association must survive a rejected write and return the initial value.
	raw, readErr := c.ReadRaw(ctx, ref)
	if readErr != nil {
		t.Fatalf("read after rejected write: %v", readErr)
	}
	iv, ok := raw.Int64()
	if !ok {
		t.Fatalf("expected integer after rejected write, got type %s", raw.Type())
	}
	if iv != fixVal.ModStVal {
		t.Errorf("value after rejected write: want %d (initial), got %d", fixVal.ModStVal, iv)
	}
}

// TestBeanClient_Reconnect opens a second independent association to
// the iec61850bean server after the first has completed and verifies that the
// server handles concurrent or sequential connections correctly.
func TestBeanClient_Reconnect(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First association — read a value and close.
	c1 := h.dial(t, ctx)
	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, err := c1.ReadRaw(ctx, ref); err != nil {
		t.Fatalf("first connection read: %v", err)
	}
	// Use a short close context so that a non-responsive server cannot block
	// the full test timeout; a 2-second grace period is sufficient for a clean
	// ACSE release, after which the connection is forcibly torn down.
	closeCtx, closeCancel := context.WithTimeout(ctx, 2*time.Second)
	_ = c1.Close(closeCtx)
	closeCancel()

	// Second association — server must still be reachable and serving.
	c2 := h.dial(t, ctx)
	defer c2.Abort(context.Background())

	raw, err := c2.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("second connection read: %v", err)
	}
	b, ok := raw.Bool()
	if !ok {
		t.Fatalf("expected boolean on second connection, got %s", raw.Type())
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("second connection: GGIO1.SPS1.stVal want %v, got %v", fixVal.SPS1StVal, b)
	}
}

func TestBeanClient_ReadDataSet(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	values, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$dsInterop")
	if err != nil {
		t.Fatalf("ReadDataSet(InteropLD, dsInterop): %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 dataset members, got %d", len(values))
	}

	// Member 0: GGIO1.SPS1.stVal[ST] — boolean false
	if values[0].Err != nil {
		t.Fatalf("dataset[0] error: %v", values[0].Err)
	}
	b, err := values[0].Value.Bool()
	if err != nil {
		t.Fatalf("dataset[0]: %v", err)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("dataset[0] (SPS1.stVal): want %v, got %v", fixVal.SPS1StVal, b)
	}

	// Member 1: LLN0.Mod.stVal[ST] — integer 1
	if values[1].Err != nil {
		t.Fatalf("dataset[1] error: %v", values[1].Err)
	}
	iv, err := values[1].Value.Int64()
	if err != nil {
		t.Fatalf("dataset[1]: %v", err)
	}
	if iv != fixVal.ModStVal {
		t.Errorf("dataset[1] (Mod.stVal): want %d, got %d", fixVal.ModStVal, iv)
	}
}

// TestBeanClient_ReadMultiple reads two attributes in a single MMS multi-variable
// request and checks both results.
func TestBeanClient_ReadMultiple(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	cfRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")

	results, err := c.ReadMultiple(ctx, []iec61850.Ref{stRef, cfRef})
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Err != nil {
		t.Fatalf("results[0] error: %v", results[0].Err)
	}
	b, bErr := results[0].Value.Bool()
	if bErr != nil {
		t.Fatalf("results[0] value: %v", bErr)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("SPS1.stVal: want %v, got %v", fixVal.SPS1StVal, b)
	}

	if results[1].Err != nil {
		t.Fatalf("results[1] error: %v", results[1].Err)
	}
	iv, ivErr := results[1].Value.Int64()
	if ivErr != nil {
		t.Fatalf("results[1] value: %v", ivErr)
	}
	if iv != fixVal.ModCtlModel {
		t.Errorf("LLN0.Mod.ctlModel: want %d, got %d", fixVal.ModCtlModel, iv)
	}
}
