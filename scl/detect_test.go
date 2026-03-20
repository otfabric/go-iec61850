package scl

import (
	"strings"
	"testing"
)

func TestDetectVersion_Valid17(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="test"/></SCL>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != Version17 {
		t.Errorf("Schema = %q, want %q", vi.Schema, Version17)
	}
	if vi.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", vi.Confidence)
	}
	if vi.Namespace != iecNamespace {
		t.Errorf("Namespace = %q", vi.Namespace)
	}
	if vi.ReleaseNum != -1 {
		t.Errorf("ReleaseNum = %d, want -1 (absent)", vi.ReleaseNum)
	}
}

func TestDetectVersion_Valid2007B(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != Version2007B {
		t.Errorf("Schema = %q, want %q", vi.Schema, Version2007B)
	}
	if vi.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", vi.Confidence)
	}
	if vi.ReleaseNum != -1 {
		t.Errorf("ReleaseNum = %d, want -1", vi.ReleaseNum)
	}
}

func TestDetectVersion_Valid2007B4(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != Version2007B4 {
		t.Errorf("Schema = %q, want %q", vi.Schema, Version2007B4)
	}
	if vi.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", vi.Confidence)
	}
	if vi.Version != "2007" || vi.Revision != "B" || vi.Release != "4" {
		t.Errorf("attrs = %q/%q/%q", vi.Version, vi.Revision, vi.Release)
	}
	if vi.ReleaseNum != 4 {
		t.Errorf("ReleaseNum = %d, want 4", vi.ReleaseNum)
	}
}

func TestDetectVersion_Valid2007C5(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="C" release="5"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != Version2007C5 {
		t.Errorf("Schema = %q, want %q", vi.Schema, Version2007C5)
	}
	if vi.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", vi.Confidence)
	}
	if vi.ReleaseNum != 5 {
		t.Errorf("ReleaseNum = %d, want 5", vi.ReleaseNum)
	}
}

func TestDetectVersion_UnknownTuple(t *testing.T) {
	cases := []struct {
		name string
		xml  string
	}{
		{"unknown revision A", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="A"/>`},
		{"future version", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2099" revision="Z"/>`},
		{"release 3 on B", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="3"/>`},
		{"release 6 on C", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="C" release="6"/>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vi, err := DetectVersion([]byte(tc.xml))
			if err != nil {
				t.Fatal(err)
			}
			if vi.Schema != VersionUnknown {
				t.Errorf("Schema = %q, want unknown", vi.Schema)
			}
			if vi.Confidence != ConfidenceLow {
				t.Errorf("Confidence = %q, want low", vi.Confidence)
			}
			if len(vi.Reasons) == 0 {
				t.Error("expected at least one reason")
			}
		})
	}
}

func TestDetectVersion_MalformedRelease(t *testing.T) {
	data := []byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="abc"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != VersionUnknown {
		t.Errorf("Schema = %q, want unknown", vi.Schema)
	}
	if vi.ReleaseNum != 0 {
		t.Errorf("ReleaseNum = %d, want 0 (malformed)", vi.ReleaseNum)
	}
	found := false
	for _, r := range vi.Reasons {
		if strings.Contains(r, "malformed release") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'malformed release' reason, got %v", vi.Reasons)
	}
}

func TestDetectVersion_MalformedRoot(t *testing.T) {
	data := []byte(`<?xml version="1.0"?>this is not xml at all`)
	_, err := DetectVersion(data)
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

func TestDetectVersion_NonSCLElement(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><Configuration><Header id="test"/></Configuration>`)
	_, err := DetectVersion(data)
	if err == nil {
		t.Fatal("expected error for non-SCL root element")
	}
	if !strings.Contains(err.Error(), "Configuration") {
		t.Errorf("error should mention actual element name: %v", err)
	}
}

func TestDetectVersion_EmptyDocument(t *testing.T) {
	_, err := DetectVersion([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty document")
	}
}

func TestDetectVersion_DoesNotParseWholeFile(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">` +
		strings.Repeat(`<IED name="test"/>`, 10000) + `</SCL>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != Version2007B {
		t.Errorf("Schema = %q, want %q", vi.Schema, Version2007B)
	}
}

func TestDetectVersion_NonIECNamespace(t *testing.T) {
	data := []byte(`<SCL xmlns="urn:custom:ns" version="2007" revision="B"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != VersionUnknown {
		t.Errorf("Schema = %q, want unknown", vi.Schema)
	}
	if vi.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", vi.Confidence)
	}
}

func TestDetectVersion_VendorNamespaces(t *testing.T) {
	data := []byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL"` +
		` xmlns:abb="http://www.abb.com/61850"` +
		` xmlns:siemens="http://www.siemens.com/scl"` +
		` version="2007" revision="B"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.Schema != Version2007B {
		t.Errorf("Schema = %q, want %q", vi.Schema, Version2007B)
	}
	if len(vi.VendorNamespaces) != 2 {
		t.Fatalf("VendorNamespaces = %d, want 2: %v", len(vi.VendorNamespaces), vi.VendorNamespaces)
	}
}

func TestDetectVersion_NoVendorNamespaces(t *testing.T) {
	data := []byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(vi.VendorNamespaces) != 0 {
		t.Errorf("VendorNamespaces = %v, want empty", vi.VendorNamespaces)
	}
}

func TestDetectVersion_ReleaseNumAbsent(t *testing.T) {
	data := []byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B"/>`)
	vi, err := DetectVersion(data)
	if err != nil {
		t.Fatal(err)
	}
	if vi.ReleaseNum != -1 {
		t.Errorf("ReleaseNum = %d, want -1 (absent)", vi.ReleaseNum)
	}
}

func TestDetectFile_MissingFile(t *testing.T) {
	_, err := DetectFile("/nonexistent/path/test.scd")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
