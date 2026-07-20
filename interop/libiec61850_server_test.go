//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2A — Step 8b:
//
//	libiec61850 IED client → go-iec61850 server
//
// Each test starts the go-iec61850 server in-process, launches the
// libiec61850 IED client adapter (Docker or local binary via
// IEC61850_CLIENT_BINARY), reads the JSON Lines from the client's stdout,
// and asserts the operation results.
package interop

import (
	"encoding/json"
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Phase 2A — libiec61850 IED client → go-iec61850 server
// ---------------------------------------------------------------------------

// TestLibIECServer_GetServerDirectory verifies GetServerDirectory returns InteropLD.
func TestLibIECServer_GetServerDirectory(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	r, ok := findIEDOp(results, "get-server-directory")
	if !ok {
		t.Fatal("no 'get-server-directory' result in client output")
	}
	if !r.OK {
		t.Fatalf("get-server-directory failed: %s", r.Error)
	}

	found := false
	for _, n := range r.Names {
		if n == "InteropLD" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'InteropLD' in get-server-directory names: %v", r.Names)
	}
}

// TestLibIECServer_GetLDDirectory verifies GetLogicalDeviceDirectory returns
// LLN0, GGIO1, MMXU1 for InteropLD.
func TestLibIECServer_GetLDDirectory(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	r, ok := findIEDResult(results, "get-ld-directory", "InteropLD")
	if !ok {
		t.Fatal("no 'get-ld-directory' result for InteropLD")
	}
	if !r.OK {
		t.Fatalf("get-ld-directory failed: %s", r.Error)
	}

	want := map[string]bool{"LLN0": false, "GGIO1": false, "MMXU1": false}
	for _, n := range r.Names {
		want[n] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected logical node %q in get-ld-directory: %v", name, r.Names)
		}
	}
}

// TestLibIECServer_GetLNDirectory verifies GetLogicalNodeDirectory returns
// SPS1 and SPCSO1 for InteropLD/GGIO1.
func TestLibIECServer_GetLNDirectory(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	r, ok := findIEDResult(results, "get-ln-directory", "InteropLD/GGIO1")
	if !ok {
		t.Fatal("no 'get-ln-directory' result for InteropLD/GGIO1")
	}
	if !r.OK {
		t.Fatalf("get-ln-directory failed: %s", r.Error)
	}

	want := map[string]bool{"SPS1": false, "SPCSO1": false}
	for _, n := range r.Names {
		want[n] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected data object %q in get-ln-directory: %v", name, r.Names)
		}
	}
}

// TestLibIECServer_Read_ST verifies the libiec61850 client reads GGIO1.SPS1.stVal
// as false from the go-iec61850 server.
func TestLibIECServer_Read_ST(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	target := "InteropLD/GGIO1.SPS1.stVal[ST]"
	r, ok := findIEDResult(results, "read", target)
	if !ok {
		t.Fatalf("no 'read' result for %q", target)
	}
	if !r.OK {
		t.Fatalf("read %q failed: %s", target, r.Error)
	}

	var b bool
	if err := json.Unmarshal(r.Value, &b); err != nil {
		t.Fatalf("parse value for %q: %v (raw: %s)", target, err, r.Value)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("%s: want %v, got %v", target, fixVal.SPS1StVal, b)
	}
}

// TestLibIECServer_Read_MX verifies the libiec61850 client reads MMXU1.TotW.mag.f
// as ~1234.5 from the go-iec61850 server.
func TestLibIECServer_Read_MX(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	target := "InteropLD/MMXU1.TotW.mag.f[MX]"
	r, ok := findIEDResult(results, "read", target)
	if !ok {
		t.Fatalf("no 'read' result for %q", target)
	}
	if !r.OK {
		t.Fatalf("read %q failed: %s", target, r.Error)
	}

	var fv float64
	if err := json.Unmarshal(r.Value, &fv); err != nil {
		t.Fatalf("parse value for %q: %v (raw: %s)", target, err, r.Value)
	}
	if math.Abs(fv-fixVal.TotWMagF) > 0.1 {
		t.Errorf("%s: want ~%g, got %g", target, fixVal.TotWMagF, fv)
	}
}

// TestLibIECServer_Read_CF verifies the libiec61850 client reads LLN0.Mod.ctlModel
// as 1 from the go-iec61850 server.
func TestLibIECServer_Read_CF(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	target := "InteropLD/LLN0.Mod.ctlModel[CF]"
	r, ok := findIEDResult(results, "read", target)
	if !ok {
		t.Fatalf("no 'read' result for %q", target)
	}
	if !r.OK {
		t.Fatalf("read %q failed: %s", target, r.Error)
	}

	var iv float64
	if err := json.Unmarshal(r.Value, &iv); err != nil {
		t.Fatalf("parse value for %q: %v (raw: %s)", target, err, r.Value)
	}
	if int64(iv) != fixVal.ModCtlModel {
		t.Errorf("%s: want %d, got %d", target, fixVal.ModCtlModel, int64(iv))
	}
}

// TestLibIECServer_Read_DC verifies the libiec61850 client reads LLN0.Mod.d
// as "mode" from the go-iec61850 server.
func TestLibIECServer_Read_DC(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	target := "InteropLD/LLN0.Mod.d[DC]"
	r, ok := findIEDResult(results, "read", target)
	if !ok {
		t.Fatalf("no 'read' result for %q", target)
	}
	if !r.OK {
		t.Fatalf("read %q failed: %s", target, r.Error)
	}

	var sv string
	if err := json.Unmarshal(r.Value, &sv); err != nil {
		t.Fatalf("parse value for %q: %v (raw: %s)", target, err, r.Value)
	}
	if sv != fixVal.ModD {
		t.Errorf("%s: want %q, got %q", target, fixVal.ModD, sv)
	}
}

// TestLibIECServer_Write verifies the libiec61850 client successfully writes
// to LLN0.Mod.stVal[ST].
func TestLibIECServer_Write(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	target := "InteropLD/LLN0.Mod.stVal[ST]"
	r, ok := findIEDResult(results, "write", target)
	if !ok {
		t.Fatalf("no 'write' result for %q", target)
	}
	if !r.OK {
		t.Errorf("write %q failed: %s", target, r.Error)
	}
}

// TestLibIECServer_ReadDataSet verifies the libiec61850 client reads dsInterop
// and gets two values.
func TestLibIECServer_ReadDataSet(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	target := "InteropLD/LLN0$dsInterop"
	r, ok := findIEDResult(results, "read-dataset", target)
	if !ok {
		t.Fatalf("no 'read-dataset' result for %q", target)
	}
	if !r.OK {
		t.Fatalf("read-dataset %q failed: %s", target, r.Error)
	}

	var values []json.RawMessage
	if err := json.Unmarshal(r.Values, &values); err != nil {
		t.Fatalf("parse dataset values: %v (raw: %s)", err, r.Values)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 dataset values, got %d", len(values))
	}

	// Member 0: GGIO1.SPS1.stVal[ST] — boolean false
	var b bool
	if err := json.Unmarshal(values[0], &b); err != nil {
		t.Fatalf("dataset[0]: %v (raw: %s)", err, values[0])
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("dataset[0] (SPS1.stVal): want %v, got %v", fixVal.SPS1StVal, b)
	}

	// Member 1: LLN0.Mod.stVal[ST] — the libiec61850 client writes 5 to this
	// attribute at step 8, before reading the dataset at step 9.
	// The go-iec61850 server persists writes to the ValueStore, so the
	// dataset read reflects the updated value.
	const wantModStVal = 5
	var iv float64
	if err := json.Unmarshal(values[1], &iv); err != nil {
		t.Fatalf("dataset[1]: %v (raw: %s)", err, values[1])
	}
	if int64(iv) != wantModStVal {
		t.Errorf("dataset[1] (Mod.stVal): want %d, got %d", wantModStVal, int64(iv))
	}
}

// TestLibIECServer_Conclude verifies the client concludes cleanly.
func TestLibIECServer_Conclude(t *testing.T) {
	srv := startGoIEDServer(t)
	ch := startIEDClientAdapter(t, srv.port)
	results := collectIEDResults(t, ch)

	r, ok := findIEDOp(results, "conclude")
	if !ok {
		t.Fatal("no 'conclude' result in client output")
	}
	if !r.OK {
		t.Errorf("conclude failed: %s", r.Error)
	}
}
