// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..")
}

func fixturePath(rel string) string {
	return filepath.Join(repoRoot(), rel)
}

// executeCmd runs rootCmd with the given args, capturing stdout. Returns any error from Execute.
func executeCmd(args ...string) error {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(args)

	// Reset flags to defaults before each run since cobra persists state.
	detectJSON = false
	detectQuiet = false
	summaryJSON = false
	summaryPretty = false
	summaryCountsOnly = false
	validateJSON = false
	validateStrict = false
	validateMaxErrors = 0
	validateWarningsAsErrors = false
	validateNoWarnings = false
	dumpJSONPretty = false
	dumpJSONOutput = ""
	dumpJSONIncludeMeta = false
	listIEDsJSON = false
	listLNsJSON = false
	listDataSetsJSON = false
	listReportsJSON = false
	listGooseJSON = false
	listSMVJSON = false
	listConnectedAPJSON = false
	listTypesJSON = false
	inspectJSON = false

	return rootCmd.Execute()
}

// --- detect ---

func TestDetect_ABB_CID(t *testing.T) {
	path := fixturePath("scl/testdata/abb/BLG065J1M101Q04A1.cid")
	if _, err := os.Stat(path); err != nil {
		t.Skip("ABB fixture not available")
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("detect", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		out := make([]byte, 4096)
		n, _ := r.Read(out)
		t.Fatalf("unexpected error: %v\noutput: %s", err, string(out[:n]))
	}
}

func TestDetect_JSON(t *testing.T) {
	path := fixturePath("scl/testdata/abb/BLG065J1M101Q04A1.cid")
	if _, err := os.Stat(path); err != nil {
		t.Skip("ABB fixture not available")
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("detect", "--json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["schema"] != "2007B4" {
		t.Errorf("schema = %v", out["schema"])
	}
	if out["kind"] != "cid" {
		t.Errorf("kind = %v", out["kind"])
	}
}

func TestDetect_V17(t *testing.T) {
	path := fixturePath("scl/testdata/minimal_v17.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("detect", "--json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["schema"] != "1.7" {
		t.Errorf("schema = %v, want 1.7", out["schema"])
	}
}

// --- summary ---

func TestSummary_OfficialExample(t *testing.T) {
	path := fixturePath("scl/specs/IEC_61850-6.2025.SCL.2007C5.full/Annex D - SCL example.scd")
	if _, err := os.Stat(path); err != nil {
		t.Skip("IEC example not available")
	}

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("summary", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSummary_JSON(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("summary", "--json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Schema string `json:"schema"`
		Kind   string `json:"kind"`
		Counts struct {
			IEDs int `json:"ieds"`
		} `json:"counts"`
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Schema != "2007B" {
		t.Errorf("schema = %q", out.Schema)
	}
	if out.Counts.IEDs != 1 {
		t.Errorf("ieds = %d", out.Counts.IEDs)
	}
}

// --- validate ---

func TestValidate_Valid(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("validate", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- dump-json ---

func TestDumpJSON_Simple(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("dump-json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var doc map[string]any
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	hdr, ok := doc["Header"].(map[string]any)
	if !ok {
		t.Fatal("Header missing")
	}
	if hdr["ID"] != "TestSCL" {
		t.Errorf("Header.ID = %v", hdr["ID"])
	}
}

func TestDumpJSON_WithMetadata(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("dump-json", "--include-metadata", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["schema"] != "2007B" {
		t.Errorf("schema = %v", out["schema"])
	}
	if out["model"] == nil {
		t.Error("model is nil")
	}
}

func TestDumpJSON_OutputFile(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")
	out := filepath.Join(t.TempDir(), "out.json")

	err := executeCmd("dump-json", "--output", out, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "TestSCL") {
		t.Error("output file does not contain expected content")
	}
}

// --- list-ieds ---

func TestListIEDs_Table(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-ieds", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "IED1") {
		t.Errorf("output missing IED1: %s", out)
	}
	if !strings.Contains(out, "TestMfr") {
		t.Errorf("output missing manufacturer: %s", out)
	}
}

func TestListIEDs_JSON(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-ieds", "--json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 IED, got %d", len(out))
	}
	if out[0]["name"] != "IED1" {
		t.Errorf("name = %v", out[0]["name"])
	}
}

// --- list-datasets ---

func TestListDataSets_Table(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-datasets", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "ds1") {
		t.Errorf("output missing ds1: %s", out)
	}
}

// --- list-reports ---

func TestListReports_Table(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-reports", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "brcb01") {
		t.Errorf("output missing brcb01: %s", out)
	}
	if !strings.Contains(out, "urcb01") {
		t.Errorf("output missing urcb01: %s", out)
	}
}

// --- validate broken ---

func TestValidate_Broken(t *testing.T) {
	path := fixturePath("scl/testdata/broken.scd")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("validate", path)

	_ = w.Close()
	os.Stdout = old

	if err == nil {
		t.Fatal("expected validation error for broken.scd")
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected exitError, got %T: %v", err, err)
	}
	if ee.code != exitValidation {
		t.Errorf("exit code = %d, want %d", ee.code, exitValidation)
	}
}

// --- list-goose ---

func TestListGoose_Table(t *testing.T) {
	path := fixturePath("scl/testdata/examples/server_example_goose/simpleIO_direct_control_goose.cid")
	if _, err := os.Stat(path); err != nil {
		t.Skip("GOOSE fixture not available")
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-goose", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "gcbEvents") {
		t.Errorf("output missing gcbEvents: %s", out)
	}
	if !strings.Contains(out, "GOOSE") {
		t.Errorf("output missing GOOSE type: %s", out)
	}
}

func TestListGoose_JSON(t *testing.T) {
	path := fixturePath("scl/testdata/examples/server_example_goose/simpleIO_direct_control_goose.cid")
	if _, err := os.Stat(path); err != nil {
		t.Skip("GOOSE fixture not available")
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-goose", "--json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 GOOSE controls, got %d", len(out))
	}
}

// --- list-connected-ap ---

func TestListConnectedAP_Table(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-connected-ap", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "IED1") {
		t.Errorf("output missing IED1: %s", out)
	}
	if !strings.Contains(out, "Net1") {
		t.Errorf("output missing Net1: %s", out)
	}
}

// --- list-types ---

func TestListTypes_Table(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("list-types", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "LNodeType") {
		t.Errorf("output missing LNodeType: %s", out)
	}
	if !strings.Contains(out, "DOType") {
		t.Errorf("output missing DOType: %s", out)
	}
}

// --- inspect ---

func TestInspect_Simple(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("inspect", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "2007B") {
		t.Errorf("output missing schema: %s", out)
	}
	if !strings.Contains(out, "scd") {
		t.Errorf("output missing kind: %s", out)
	}
}

func TestInspect_JSON(t *testing.T) {
	path := fixturePath("scl/testdata/simple.scd")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := executeCmd("inspect", "--json", path)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["schema"] != "2007B" {
		t.Errorf("schema = %v", out["schema"])
	}
}

// --- usage ---

func TestUsage_NoArgs(t *testing.T) {
	err := executeCmd("detect")
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}
