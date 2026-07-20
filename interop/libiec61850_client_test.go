//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2A — Step 8a:
//
//	go-iec61850 client ← libiec61850 IED server
//
// Each test starts the libiec61850 IED server adapter (Docker or local
// binary via IEC61850_SERVER_BINARY), waits for the JSON readiness event,
// and then exercises the go-iec61850 client API.
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
// Phase 2A — go-iec61850 client ← libiec61850 IED server
// ---------------------------------------------------------------------------

// TestLibIECClient_ListLogicalDevices verifies GetServerDirectory returns InteropLD.
func TestLibIECClient_ListLogicalDevices(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_ListLogicalNodes verifies InteropLD contains LLN0, GGIO1, MMXU1.
func TestLibIECClient_ListLogicalNodes(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_ListDataObjects verifies GGIO1 contains SPS1 and SPCSO1.
func TestLibIECClient_ListDataObjects_GGIO1(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Read_ST reads the boolean status GGIO1.SPS1.stVal[ST].
func TestLibIECClient_Read_ST(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Read_MX reads the float measurement MMXU1.TotW.mag.f[MX].
func TestLibIECClient_Read_MX(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Read_CF reads the integer configuration LLN0.Mod.ctlModel[CF].
func TestLibIECClient_Read_CF(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Read_DC reads the description string LLN0.Mod.d[DC].
func TestLibIECClient_Read_DC(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Write writes a new ctlModel value to LLN0.Mod.ctlModel[CF] and reads it back.
// libiec61850 does not allow direct writes to ST attributes; CF is writable via setWriteAccessPolicy.
func TestLibIECClient_Write(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")
	newVal := int64(2) // sbo-with-normal-security

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
		t.Errorf("LLN0.Mod.ctlModel[CF] after write: want %d, got %d", newVal, iv)
	}
}

// TestLibIECClient_ListDataSets verifies dsInterop is listed under InteropLD.
func TestLibIECClient_ListDataSets(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_ReadDataSet reads dsInterop and asserts both members.
func TestLibIECClient_ReadDataSet(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

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

// TestLibIECClient_Write_TypeMismatch verifies that writing a boolean value to
// an INT32 attribute is rejected and the session remains usable afterward.
func TestLibIECClient_Write_TypeMismatch(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	ref, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.stVal[ST]")

	// Write a boolean to an INT32 attribute — should fail.
	err := c.Write(ctx, ref, mms.NewBoolean(true))
	if err == nil {
		t.Error("expected type-inconsistent error writing boolean to INT32 attribute, got nil")
	} else {
		t.Logf("correctly rejected type-mismatched write: %v", err)
	}

	// The session must survive a rejected write.
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

// TestLibIECClient_Reconnect opens a second independent client connection to
// the libiec61850 server after the first one has been cleanly closed.
func TestLibIECClient_Reconnect(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")

	// First connection — read one value and close.
	c1 := h.dial(t, ctx)
	if _, err := c1.ReadRaw(ctx, ref); err != nil {
		t.Fatalf("first connection read: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 2*time.Second)
	_ = c1.Close(closeCtx)
	closeCancel()

	// Second connection — server must still be reachable and serving.
	c2 := h.dial(t, ctx)
	defer c2.Close(ctx)

	raw, err := c2.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("second connection read: %v", err)
	}
	b, ok := raw.Bool()
	if !ok {
		t.Fatalf("expected boolean on second connection, got type %s", raw.Type())
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("second connection SPS1.stVal: want %v, got %v", fixVal.SPS1StVal, b)
	}
}

// TestLibIECClient_ReadMultiple reads multiple attributes in a single MMS request.
func TestLibIECClient_ReadMultiple(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	refs := []iec61850.Ref{}
	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	cfRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")
	refs = append(refs, stRef, cfRef)

	results, err := c.ReadMultiple(ctx, refs)
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Result 0: SPS1.stVal — boolean false
	if results[0].Err != nil {
		t.Fatalf("results[0] error: %v", results[0].Err)
	}
	b, err := results[0].Value.Bool()
	if err != nil {
		t.Fatalf("results[0] value: %v", err)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("SPS1.stVal: want %v, got %v", fixVal.SPS1StVal, b)
	}

	// Result 1: LLN0.Mod.ctlModel — integer
	if results[1].Err != nil {
		t.Fatalf("results[1] error: %v", results[1].Err)
	}
	iv, err := results[1].Value.Int64()
	if err != nil {
		t.Fatalf("results[1] value: %v", err)
	}
	if iv != fixVal.ModCtlModel {
		t.Errorf("LLN0.Mod.ctlModel: want %d, got %d", fixVal.ModCtlModel, iv)
	}
}
