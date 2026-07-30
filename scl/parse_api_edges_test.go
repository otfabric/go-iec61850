// SPDX-License-Identifier: MIT

package scl

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read boom") }

func TestParseBytes_MaxDiagnosticsAndStrict(t *testing.T) {
	// Broken references produce semantic diagnostics when ValidateSemantic is on.
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="D"/>
  <IED name="IED1">
    <AccessPoint name="AP1">
      <Server>
        <LDevice inst="LD1">
          <LN0 lnClass="LLN0" inst="" lnType="MissingType"/>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates/>
</SCL>`
	res, err := ParseBytes([]byte(xml), ParseOptions{
		ValidateSemantic: true,
		MaxDiagnostics:   1,
	})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("MaxDiagnostics: got %d", len(res.Diagnostics))
	}

	_, err = ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true, Strict: true})
	if err == nil {
		t.Fatal("expected strict mode error")
	}
}

func TestParseFileOpts_AndWrappers(t *testing.T) {
	src := filepath.Join("testdata", "minimal_v17.scd")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.icd")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ParseFileOpts(path, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseFileOpts: %v", err)
	}
	if res.Kind != KindICD {
		t.Fatalf("kind from extension = %q, want ICD", res.Kind)
	}

	doc, err := ParseFile(path)
	if err != nil || doc == nil {
		t.Fatalf("ParseFile: %v", err)
	}

	doc, err = Parse(bytes.NewReader(data))
	if err != nil || doc == nil {
		t.Fatalf("Parse: %v", err)
	}

	res, err = ParseWithOptions(bytes.NewReader(data), ParseOptions{Kind: KindSCD})
	if err != nil || res.Kind != KindSCD {
		t.Fatalf("ParseWithOptions: %v kind=%q", err, res.Kind)
	}

	if _, err := ParseFileOpts(filepath.Join(dir, "missing.scd"), ParseOptions{}); err == nil {
		t.Fatal("expected read error")
	}
	if _, err := ParseFile(filepath.Join(dir, "missing.scd")); err == nil {
		t.Fatal("expected ParseFile read error")
	}
	if _, err := Parse(errReader{}); err == nil {
		t.Fatal("expected Parse read error")
	}
	if _, err := ParseWithOptions(errReader{}, ParseOptions{}); err == nil {
		t.Fatal("expected ParseWithOptions read error")
	}
	if _, err := Parse(strings.NewReader("not-xml")); err == nil {
		t.Fatal("expected parse failure")
	}
	if _, err := ParseBytes([]byte("<not-scl/>"), ParseOptions{}); err == nil {
		t.Fatal("expected DetectVersion/unmarshal failure")
	}
}
