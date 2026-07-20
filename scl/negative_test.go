// SPDX-License-Identifier: MIT

package scl

import (
	"strings"
	"testing"
)

func TestNeg_UnknownSchemaTuple(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="A"><Header id="t"/></SCL>`
	_, err := ParseBytes([]byte(xml), ParseOptions{})
	if err == nil {
		t.Fatal("expected error for unknown schema tuple 2007A")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %v, want 'unsupported'", err)
	}
}

func TestNeg_MalformedRelease(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="XYZ"><Header id="t"/></SCL>`
	vi, err := DetectVersion([]byte(xml))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if vi.ReleaseNum != 0 {
		t.Errorf("ReleaseNum = %d, want 0 for malformed", vi.ReleaseNum)
	}
}

func TestNeg_MissingTemplateRef(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <DataTypeTemplates>
    <LNodeType id="LNT1" lnClass="LLN0">
      <DO name="Mod" type="GHOST_DOType"/>
    </LNodeType>
  </DataTypeTemplates>
</SCL>`
	result, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "missing-dotype" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-dotype diagnostic")
	}
}

func TestNeg_MissingDatasetForControl(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <IED name="I1"><AccessPoint name="S1"><Server><LDevice inst="LD1">
    <LN0 lnClass="LLN0" inst="" lnType="t1">
      <ReportControl name="rc1" datSet="no_such_ds" confRev="1"/>
    </LN0>
  </LDevice></Server></AccessPoint></IED>
  <DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
</SCL>`
	result, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "missing-dataset" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-dataset diagnostic")
	}
}

func TestNeg_BadConnectedAP(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <Communication>
    <SubNetwork name="Net1">
      <ConnectedAP iedName="GHOST_IED" apName="S1"/>
    </SubNetwork>
  </Communication>
</SCL>`
	result, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "missing-connected-ap" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-connected-ap diagnostic")
	}
}

func TestNeg_DuplicateIED(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <IED name="IED1"/><IED name="IED1"/>
</SCL>`
	result, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "duplicate-ied" {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate-ied diagnostic")
	}
}

func TestNeg_DuplicateLNodeType(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <DataTypeTemplates>
    <LNodeType id="LNT1" lnClass="LLN0"/>
    <LNodeType id="LNT1" lnClass="MMXU"/>
  </DataTypeTemplates>
</SCL>`
	result, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "duplicate-id" && strings.Contains(d.Message, "LNT1") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate-id diagnostic for LNodeType")
	}
}

func TestNeg_StrictRejectsErrors(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <IED name="I1"><AccessPoint name="S1"><Server><LDevice inst="LD1">
    <LN0 lnClass="LLN0" inst="" lnType="GHOST"/>
  </LDevice></Server></AccessPoint></IED>
</SCL>`
	_, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true, Strict: true})
	if err == nil {
		t.Fatal("expected error in strict mode with missing lnType")
	}
}

func TestNeg_BrokenFile(t *testing.T) {
	result, err := ParseFileWithOptions("testdata/broken.scd", ParseOptions{ValidateSemantic: true})
	if err != nil {
		t.Fatalf("ParseFileWithOptions: %v", err)
	}
	if !result.HasErrors() {
		t.Error("expected validation errors for broken.scd")
	}
	codes := make(map[string]bool)
	for _, d := range result.Diagnostics {
		codes[d.Code] = true
	}
	for _, want := range []string{"missing-dotype", "missing-lnodetype", "missing-dataset"} {
		if !codes[want] {
			t.Errorf("missing expected diagnostic code %q", want)
		}
	}
}
