// SPDX-License-Identifier: MIT

package scl

import (
	"strings"
	"testing"
)

// A version tuple that is not a published schema is parsed with the
// closest bindings and reported; one in a foreign namespace or with a
// malformed release is refused.
func TestNeg_UnknownSchemaTuple(t *testing.T) {
	for _, tc := range []struct {
		name, attrs string
		want        SchemaVersion
	}{
		{"draft revision A", `version="2007" revision="A"`, Version2007B},
		{"release 5 of B", `version="2007" revision="B" release="5"`, Version2007B4},
		{"release 3 of B", `version="2007" revision="B" release="3"`, Version2007B4},
		{"release 6 of C", `version="2007" revision="C" release="6"`, Version2007C5},
		{"revision D", `version="2007" revision="D" release="1"`, Version2007C5},
		{"other version", `version="2019" revision="B" release="1"`, Version2007C5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" ` + tc.attrs + `><Header id="t"/><IED name="I1"/></SCL>`
			res, err := ParseBytes([]byte(xml), ParseOptions{})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if res.Version.Schema != VersionUnknown {
				t.Errorf("detected schema = %q, want unknown (detection is unchanged)", res.Version.Schema)
			}
			if len(res.Diagnostics) == 0 || res.Diagnostics[0].Code != "schema-fallback" || res.Diagnostics[0].Severity != DiagWarning ||
				!strings.Contains(res.Diagnostics[0].Message, string(tc.want)) {
				t.Errorf("diagnostics = %+v, want a schema-fallback warning naming %s", res.Diagnostics, tc.want)
			}
			if res.Document == nil || res.Document.Header.ID != "t" || len(res.Document.IEDs) != 1 {
				t.Errorf("document = %+v", res.Document)
			}
		})
	}
	for name, xml := range map[string]string{
		"fallback decode fails": `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2019" revision="B" release="1"><Header id="t"/><IED name="x"></SCL>`,
		"foreign namespace":     `<?xml version="1.0"?><SCL xmlns="urn:custom" version="2019"><Header id="t"/></SCL>`,
		"malformed release":     `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="x"><Header id="t"/></SCL>`,
	} {
		if _, err := ParseBytes([]byte(xml), ParseOptions{}); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
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

// Tools write UTF-8 under other names ("utf8"); those and US-ASCII parse,
// any other encoding is refused with its name.
func TestParse_EncodingLabels(t *testing.T) {
	body := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"><Header id="t"/><IED name="I1"/></SCL>`
	for _, enc := range []string{"utf8", "UTF8", "UTF-8", "us-ascii", "ASCII"} {
		res, err := ParseBytes([]byte(`<?xml version='1.0' encoding='`+enc+`'?>`+body), ParseOptions{})
		if err != nil || res.Document.Header.ID != "t" || len(res.Document.IEDs) != 1 {
			t.Errorf("encoding %q: %v", enc, err)
		}
	}
	_, err := ParseBytes([]byte(`<?xml version='1.0' encoding='ISO-8859-1'?>`+body), ParseOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported encoding "ISO-8859-1"`) {
		t.Errorf("ISO-8859-1: err = %v, want the encoding named", err)
	}
	if _, err := DetectVersion(nil); err == nil || err.Error() != "scl: no SCL root element found" {
		t.Errorf("empty input: %v", err)
	}
}
