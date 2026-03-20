package genir_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/otfabric/go-iec61850/scl/internal/genir"
	v17 "github.com/otfabric/go-iec61850/scl/internal/raw/v17"
	v2007b4 "github.com/otfabric/go-iec61850/scl/internal/raw/v2007b4"
	v2007c5 "github.com/otfabric/go-iec61850/scl/internal/raw/v2007c5"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func TestParseBundles(t *testing.T) {
	specs := filepath.Join(repoRoot(), "scl", "specs")
	bundles := []struct {
		dir     string
		version string
	}{
		{"IEC_61850-6.2003.SCL.1.7.full", "v17"},
		{"IEC_61850-6.2009.SCL.2007B.full", "v2007b"},
		{"IEC_61850-6.2018.SCL.2007B4.full", "v2007b4"},
		{"IEC_61850-6.2025.SCL.2007C5.full", "v2007c5"},
	}

	for _, b := range bundles {
		t.Run(b.version, func(t *testing.T) {
			dir := filepath.Join(specs, b.dir)
			if _, err := os.Stat(filepath.Join(dir, "SCL.xsd")); os.IsNotExist(err) {
				t.Skipf("bundle %s not found", b.dir)
			}

			schema, err := genir.ParseBundle(dir, b.version)
			if err != nil {
				t.Fatalf("ParseBundle: %v", err)
			}

			if schema.Namespace != "http://www.iec.ch/61850/2003/SCL" {
				t.Errorf("unexpected namespace: %s", schema.Namespace)
			}
			if len(schema.ComplexTypes) == 0 {
				t.Error("no complex types parsed")
			}
			if len(schema.SimpleTypes) == 0 {
				t.Error("no simple types parsed")
			}
			if len(schema.Elements) == 0 {
				t.Error("no elements parsed")
			}

			resolved, err := genir.Resolve(schema)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			if len(resolved.TopElements) == 0 {
				t.Error("no top-level elements")
			}
		})
	}
}

func TestDeterminism(t *testing.T) {
	dir := filepath.Join(repoRoot(), "scl", "specs", "IEC_61850-6.2025.SCL.2007C5.full")
	if _, err := os.Stat(filepath.Join(dir, "SCL.xsd")); os.IsNotExist(err) {
		t.Skip("2007C5 bundle not found")
	}

	// Generate twice to temp dirs and compare
	var outputs [2]map[string][]byte
	for i := 0; i < 2; i++ {
		schema, err := genir.ParseBundle(dir, "v2007c5")
		if err != nil {
			t.Fatalf("ParseBundle: %v", err)
		}
		resolved, err := genir.Resolve(schema)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		tmpDir := t.TempDir()
		emitter := genir.NewEmitter(resolved, tmpDir, "v2007c5")
		if err := emitter.Emit(); err != nil {
			t.Fatalf("Emit: %v", err)
		}

		outputs[i] = make(map[string][]byte)
		entries, _ := os.ReadDir(tmpDir)
		for _, e := range entries {
			data, _ := os.ReadFile(filepath.Join(tmpDir, e.Name()))
			outputs[i][e.Name()] = data
		}
	}

	for name, data1 := range outputs[0] {
		data2, ok := outputs[1][name]
		if !ok {
			t.Errorf("file %s missing in second run", name)
			continue
		}
		if string(data1) != string(data2) {
			t.Errorf("file %s differs between runs", name)
		}
	}
}

func TestDecodeV2007C5_OfficialExample(t *testing.T) {
	path := filepath.Join(repoRoot(), "scl", "specs",
		"IEC_61850-6.2025.SCL.2007C5.full", "Annex D - SCL example.scd")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("official example not found: %v", err)
	}

	var scl v2007c5.SCL
	if err := xml.Unmarshal(data, &scl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if scl.Version != "2007" {
		t.Errorf("version = %q, want 2007", scl.Version)
	}
	if scl.Revision != "C" {
		t.Errorf("revision = %q, want C", scl.Revision)
	}
	if scl.Header.Id == "" {
		t.Error("Header.Id is empty")
	}
	if len(scl.IED) == 0 {
		t.Error("no IEDs decoded")
	}
	if scl.DataTypeTemplates == nil {
		t.Error("no DataTypeTemplates decoded")
	}
}

func TestDecodeV2007B4_ABBFile(t *testing.T) {
	path := filepath.Join(repoRoot(), "scl", "testdata", "abb", "BLG065J1M101Q04A1.cid")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("ABB sample not found: %v", err)
	}

	var scl v2007b4.SCL
	if err := xml.Unmarshal(data, &scl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if scl.Version != "2007" {
		t.Errorf("version = %q, want 2007", scl.Version)
	}
	if scl.Revision != "B" {
		t.Errorf("revision = %q, want B", scl.Revision)
	}
	if scl.Header.Id == "" {
		t.Error("Header.Id is empty")
	}
	if len(scl.IED) == 0 {
		t.Error("no IEDs decoded")
	}
	if len(scl.Substation) == 0 {
		t.Error("no Substations decoded")
	}
}

func TestDecodeV17_SimpleFile(t *testing.T) {
	path := filepath.Join(repoRoot(), "scl", "testdata", "simple.scd")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("simple.scd not found: %v", err)
	}

	var scl v17.SCL
	if err := xml.Unmarshal(data, &scl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if scl.Header.Id == "" {
		t.Error("Header.Id is empty")
	}
	if len(scl.IED) == 0 {
		t.Error("no IEDs decoded")
	}
}
