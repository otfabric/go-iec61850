// SPDX-License-Identifier: MIT

package scl

import "testing"

func TestPrivate_Preserved(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <IED name="IED1">
    <Private type="ABB_UnitInfo">vendor-data-here</Private>
    <AccessPoint name="S1">
      <Private type="ABB_AP_Extra">ap-data</Private>
      <Server>
        <LDevice inst="LD1">
          <Private type="ABB_LD_Info">ld-data</Private>
          <LN0 lnClass="LLN0" inst="" lnType="t1">
            <Private type="ABB_LN0_Info">ln0-data</Private>
          </LN0>
          <LN lnClass="GGIO" inst="1" lnType="t1">
            <Private type="ABB_LN_Info">ln-data</Private>
          </LN>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
</SCL>`

	result, err := ParseBytes([]byte(xml), ParseOptions{})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	doc := result.Document

	if len(doc.IEDs) != 1 {
		t.Fatalf("IEDs = %d", len(doc.IEDs))
	}
	ied := doc.IEDs[0]

	if len(ied.Private) != 1 || ied.Private[0].Type != "ABB_UnitInfo" {
		t.Errorf("IED.Private = %+v", ied.Private)
	}

	ap := ied.AccessPoints[0]
	if len(ap.Private) != 1 || ap.Private[0].Type != "ABB_AP_Extra" {
		t.Errorf("AP.Private = %+v", ap.Private)
	}

	ld := ap.Server.LDevices[0]
	if len(ld.Private) != 1 || ld.Private[0].Type != "ABB_LD_Info" {
		t.Errorf("LD.Private = %+v", ld.Private)
	}

	if ld.LN0 == nil {
		t.Fatal("LN0 is nil")
	}
	if len(ld.LN0.Private) != 1 || ld.LN0.Private[0].Type != "ABB_LN0_Info" {
		t.Errorf("LN0.Private = %+v", ld.LN0.Private)
	}

	if len(ld.LNs) != 1 {
		t.Fatalf("LNs = %d", len(ld.LNs))
	}
	if len(ld.LNs[0].Private) != 1 || ld.LNs[0].Private[0].Type != "ABB_LN_Info" {
		t.Errorf("LN.Private = %+v", ld.LNs[0].Private)
	}
}

func TestPrivate_TypeAndSource(t *testing.T) {
	xml := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <IED name="IED1">
    <Private type="VendorData" source="tool.cfg"/>
  </IED>
</SCL>`
	result, err := ParseBytes([]byte(xml), ParseOptions{})
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	ied := result.Document.IEDs[0]
	if len(ied.Private) != 1 {
		t.Fatalf("Private count = %d", len(ied.Private))
	}
	p := ied.Private[0]
	if p.Type != "VendorData" {
		t.Errorf("Type = %q, want VendorData", p.Type)
	}
	if p.Source != "tool.cfg" {
		t.Errorf("Source = %q, want tool.cfg", p.Source)
	}
}

func TestPrivate_ABBFile(t *testing.T) {
	doc, err := ParseFile("testdata/abb/BLG065J1M101Q04A1.cid")
	if err != nil {
		t.Skipf("ABB fixture not available: %v", err)
	}
	if len(doc.IEDs) == 0 {
		t.Fatal("no IEDs")
	}

	totalPrivate := 0
	for _, ied := range doc.IEDs {
		totalPrivate += len(ied.Private)
		for _, ap := range ied.AccessPoints {
			totalPrivate += len(ap.Private)
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				totalPrivate += len(ld.Private)
				if ld.LN0 != nil {
					totalPrivate += len(ld.LN0.Private)
				}
				for _, ln := range ld.LNs {
					totalPrivate += len(ln.Private)
				}
			}
		}
	}

	if totalPrivate == 0 {
		t.Error("expected at least one Private element in ABB CID file")
	}
	t.Logf("found %d Private elements across IED/AP/LD/LN hierarchy", totalPrivate)
}

func TestPrivate_VendorNamespacesSurvive(t *testing.T) {
	result, err := ParseFileWithOptions("testdata/abb/BLG065J1M101Q04A1.cid", ParseOptions{})
	if err != nil {
		t.Skipf("ABB fixture not available: %v", err)
	}
	if result.Document.Metadata == nil {
		t.Fatal("Metadata is nil")
	}
	if len(result.Document.Metadata.VendorNamespaces) == 0 {
		t.Error("expected vendor namespaces from ABB CID")
	}
	t.Logf("vendor namespaces: %v", result.Document.Metadata.VendorNamespaces)
}

func TestSummary_PrivateCount(t *testing.T) {
	doc, err := ParseFile("testdata/abb/BLG065J1M101Q04A1.cid")
	if err != nil {
		t.Skipf("ABB fixture not available: %v", err)
	}
	sum := Summarize(doc)
	if sum.PrivateCount == 0 {
		t.Error("expected PrivateCount > 0 for ABB CID")
	}
	t.Logf("PrivateCount = %d", sum.PrivateCount)
}
