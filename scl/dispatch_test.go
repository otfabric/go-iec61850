// SPDX-License-Identifier: MIT

package scl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Document kind ---

func TestKindFromExtension(t *testing.T) {
	tests := []struct {
		path string
		want DocumentKind
	}{
		{"foo.scd", KindSCD},
		{"foo.cid", KindCID},
		{"foo.icd", KindICD},
		{"foo.iid", KindIID},
		{"foo.ssd", KindSSD},
		{"foo.SCD", KindSCD},
		{"foo.xml", KindUnknown},
		{"foo", KindUnknown},
	}
	for _, tt := range tests {
		got := kindFromExtension(tt.path)
		if got != tt.want {
			t.Errorf("kindFromExtension(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// --- ParseWithOptions ---

func TestParseWithOptions_Simple(t *testing.T) {
	f, err := os.Open("testdata/simple.scd")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	result, err := ParseWithOptions(f, ParseOptions{Kind: KindSCD})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version.Schema != Version2007B {
		t.Errorf("Schema = %q, want %q", result.Version.Schema, Version2007B)
	}
	if result.Kind != KindSCD {
		t.Errorf("Kind = %q, want %q", result.Kind, KindSCD)
	}
	if result.Document == nil {
		t.Fatal("Document is nil")
	}
	if len(result.Document.IEDs) != 1 {
		t.Errorf("IEDs = %d, want 1", len(result.Document.IEDs))
	}
	if result.HasErrors() {
		t.Error("unexpected errors in diagnostics")
	}
}

func TestParseFileWithOptions_InfersKind(t *testing.T) {
	result, err := ParseFileWithOptions("testdata/simple.scd", ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindSCD {
		t.Errorf("Kind = %q, want %q", result.Kind, KindSCD)
	}
}

func TestParseWithOptions_UnknownVersionIsError(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="urn:custom"><Header id="t"/></SCL>`
	_, err := ParseWithOptions(strings.NewReader(xml), ParseOptions{})
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
	if !strings.Contains(err.Error(), "unsupported schema version") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseWithOptions_Strict(t *testing.T) {
	data := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B"><Header id="t"/></SCL>`
	_, err := ParseWithOptions(strings.NewReader(data), ParseOptions{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
}

// --- Round-trip decode with real files ---

func TestDispatch_OfficialExample2007C5(t *testing.T) {
	path := filepath.Join("specs", "IEC_61850-6.2025.SCL.2007C5.full", "Annex D - SCL example.scd")
	result, err := ParseFileWithOptions(path, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseFileWithOptions: %v", err)
	}
	if result.Version.Schema != Version2007C5 {
		t.Errorf("Schema = %q, want %q", result.Version.Schema, Version2007C5)
	}
	if result.Kind != KindSCD {
		t.Errorf("Kind = %q, want %q", result.Kind, KindSCD)
	}
	doc := result.Document
	if doc == nil {
		t.Fatal("Document is nil")
	}
	if doc.Header.ID == "" {
		t.Error("Header.ID is empty")
	}
	if len(doc.IEDs) == 0 {
		t.Error("no IEDs parsed")
	}
	if len(doc.DataTypeTemplates.LNodeTypes) == 0 {
		t.Error("no LNodeTypes parsed")
	}
	if len(doc.DataTypeTemplates.DOTypes) == 0 {
		t.Error("no DOTypes parsed")
	}
}

func TestDispatch_ABBFiles_V2007B4(t *testing.T) {
	files, err := filepath.Glob("testdata/abb/*.cid")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no ABB CID files found")
	}

	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			result, err := ParseFileWithOptions(path, ParseOptions{})
			if err != nil {
				t.Fatalf("ParseFileWithOptions(%s): %v", name, err)
			}
			if result.Document == nil {
				t.Fatal("Document is nil")
			}
			if len(result.Document.IEDs) == 0 {
				t.Error("no IEDs parsed")
			}
			if result.Version.Schema == VersionUnknown {
				t.Errorf("Schema is unknown for %s", name)
			}
		})
	}
}

func TestDispatch_ExampleCIDFiles(t *testing.T) {
	files, err := filepath.Glob("testdata/examples/*/*.cid")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no example CID files found")
	}

	unsupported := map[string]bool{
		"cid_example_deadband.cid": true,
		"substitution_example.cid": true,
	}

	for _, path := range files {
		rel, _ := filepath.Rel("testdata/examples", path)
		t.Run(rel, func(t *testing.T) {
			result, err := ParseFileWithOptions(path, ParseOptions{})
			if unsupported[filepath.Base(path)] {
				if err == nil {
					t.Fatal("expected error for unsupported schema version")
				}
				if !strings.Contains(err.Error(), "unsupported schema version") {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFileWithOptions: %v", err)
			}
			if result.Document == nil {
				t.Fatal("Document is nil")
			}
		})
	}
}

// --- Verify ParseWithOptions backward-compatible wrappers ---

func TestParseWithOptions_BackwardCompat(t *testing.T) {
	result, err := ParseFileWithOptions("testdata/simple.scd", ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc := result.Document

	if doc.Header.ID != "TestSCL" {
		t.Errorf("Header.ID = %q", doc.Header.ID)
	}
	if len(doc.IEDs) != 1 {
		t.Fatalf("IEDs = %d", len(doc.IEDs))
	}

	ied := doc.IEDs[0]
	if ied.Name != "IED1" {
		t.Errorf("IED.Name = %q", ied.Name)
	}

	srv := ied.AccessPoints[0].Server
	if srv == nil {
		t.Fatal("Server is nil")
	}
	ld := srv.LDevices[0]
	if ld.Inst != "LD1" {
		t.Errorf("LDevice.Inst = %q", ld.Inst)
	}
	if ld.LN0 == nil {
		t.Fatal("LN0 is nil")
	}
	if len(ld.LN0.Reports) != 2 {
		t.Errorf("LN0.Reports = %d", len(ld.LN0.Reports))
	}

	brcb := ld.LN0.Reports[0]
	if brcb.Name != "brcb01" || !brcb.Buffered {
		t.Errorf("BRCB = {Name:%q, Buffered:%v}", brcb.Name, brcb.Buffered)
	}
	if !brcb.TrgOps.GI {
		t.Error("BRCB.TrgOps.GI should be true")
	}
	if !brcb.OptFields.BufOvfl {
		t.Error("BRCB.OptFields.BufOvfl should be true")
	}

	if ied.Services == nil {
		t.Fatal("Services is nil")
	}
	if ied.Services.SMVsc == nil {
		t.Fatal("SMVsc is nil")
	}
	if ied.Services.SMVsc.Max != 3 {
		t.Errorf("SMVsc.Max = %d", ied.Services.SMVsc.Max)
	}
	if ied.Services.ConfReportCtrl == nil {
		t.Fatal("ConfReportCtrl is nil")
	}
	if ied.Services.ConfReportCtrl.BufMode != "both" {
		t.Errorf("BufMode = %q", ied.Services.ConfReportCtrl.BufMode)
	}
}

// --- Result helpers ---

func TestResult_HasErrors(t *testing.T) {
	r := &Result{
		Diagnostics: []Diagnostic{
			{Severity: DiagWarning, Message: "warning"},
		},
	}
	if r.HasErrors() {
		t.Error("no errors expected")
	}

	r.Diagnostics = append(r.Diagnostics, Diagnostic{
		Severity: DiagError, Message: "error",
	})
	if !r.HasErrors() {
		t.Error("errors expected")
	}
}
