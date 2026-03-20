package scl

import (
	"path/filepath"
	"testing"
)

func TestE2E_Version17(t *testing.T) {
	runE2E(t, "testdata/minimal_v17.scd", Version17, KindSCD)
}

func TestE2E_Version2007B(t *testing.T) {
	runE2E(t, "testdata/simple.scd", Version2007B, KindSCD)
}

func TestE2E_Version2007C5(t *testing.T) {
	path := filepath.Join("specs", "IEC_61850-6.2025.SCL.2007C5.full", "Annex D - SCL example.scd")
	runE2E(t, path, Version2007C5, KindSCD)
}

func TestE2E_ABB_V2007B4(t *testing.T) {
	files, err := filepath.Glob("testdata/abb/*.cid")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no ABB CID files found")
	}
	runE2E(t, files[0], Version2007B4, KindCID)
}

func runE2E(t *testing.T, path string, wantVersion SchemaVersion, wantKind DocumentKind) {
	t.Helper()

	vi, err := DetectFile(path)
	if err != nil {
		t.Fatalf("DetectFile: %v", err)
	}
	if vi.Schema != wantVersion {
		t.Errorf("DetectFile schema = %q, want %q", vi.Schema, wantVersion)
	}

	result, err := ParseFileWithOptions(path, ParseOptions{
		Kind:             wantKind,
		ValidateSemantic: true,
	})
	if err != nil {
		t.Fatalf("ParseFileWithOptions: %v", err)
	}
	if result.Version.Schema != wantVersion {
		t.Errorf("result.Version.Schema = %q, want %q", result.Version.Schema, wantVersion)
	}
	if result.Kind != wantKind {
		t.Errorf("result.Kind = %q, want %q", result.Kind, wantKind)
	}

	doc := result.Document
	if doc == nil {
		t.Fatal("Document is nil")
	}
	if doc.Metadata == nil {
		t.Error("Document.Metadata is nil")
	}

	if len(doc.IEDs) == 0 {
		t.Error("no IEDs parsed")
	}
	if len(doc.DataTypeTemplates.LNodeTypes) == 0 {
		t.Error("no LNodeTypes parsed")
	}

	sum := Summarize(doc)
	if sum.IEDs == 0 {
		t.Error("Summarize returned 0 IEDs")
	}
	if sum.LogicalDevices == 0 {
		t.Error("Summarize returned 0 LogicalDevices")
	}

	for _, d := range result.Diagnostics {
		if d.Severity == DiagError {
			t.Logf("diagnostic: [%s] %s: %s", d.Code, d.Path, d.Message)
		}
	}
}
