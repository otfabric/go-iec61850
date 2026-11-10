// SPDX-License-Identifier: MIT

package scl

import (
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

// TestXSITypeAfterType: in <P type="IP" xsi:type="tP_IP">, the order
// ABB's tools write, the address type is the type attribute, not the
// xsi:type that follows it; in the other order too.
func TestXSITypeAfterType(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
     xsi:schemaLocation="http://www.iec.ch/61850/2003/SCL SCL.xsd" version="2007" revision="B" release="4">
<Header id="x"/>
<Communication><SubNetwork name="S"><ConnectedAP iedName="I" apName="AP">
<Address>
<P type="IP" xsi:type="tP_IP">10.0.0.1</P>
<P xsi:type="tP_IP-SUBNET" type="IP-SUBNET">255.255.255.0</P>
</Address>
<GSE ldInst="LD0" cbName="gcb"><Address><P type="MAC-Address" xsi:type="tP_MAC-Address">01-0C-CD-01-00-01</P></Address></GSE>
</ConnectedAP></SubNetwork></Communication>
</SCL>`
	s, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	ap := s.Communication.SubNetworks[0].ConnectedAPs[0]
	want := []P{{Type: "IP", Value: "10.0.0.1"}, {Type: "IP-SUBNET", Value: "255.255.255.0"}}
	if len(ap.Address) != 2 || ap.Address[0] != want[0] || ap.Address[1] != want[1] {
		t.Errorf("Address = %+v", ap.Address)
	}
	if g := ap.GSEs[0].Address; len(g) != 1 || g[0].Type != "MAC-Address" {
		t.Errorf("GSE address = %+v", g)
	}
}

// TestABBAddresses: every anonymized ABB CID writes type before xsi:type;
// each connected access point keeps its IP address.
func TestABBAddresses(t *testing.T) {
	files, err := filepath.Glob("testdata/abb/*.cid")
	if err != nil || len(files) == 0 {
		t.Fatalf("no ABB fixtures: %v", err)
	}
	for _, f := range files {
		s, err := ParseFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if s.Communication == nil {
			continue
		}
		found := false
		for _, sn := range s.Communication.SubNetworks {
			for _, ap := range sn.ConnectedAPs {
				for _, p := range ap.Address {
					if strings.HasPrefix(p.Type, "tP_") {
						t.Errorf("%s: P type %q is the xsi:type", f, p.Type)
					}
					if p.Type == "IP" {
						if _, err := netip.ParseAddr(p.Value); err != nil {
							t.Errorf("%s: IP %q: %v", f, p.Value, err)
						}
						found = true
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no IP address", f)
		}
	}
}

const defaultsDoc = `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4">
<Header id="x"/>
<IED name="I"><Services><ConfDataSet max="10" maxAttributes="100"/></Services>
<AccessPoint name="AP"><Server><Authentication/><LDevice inst="LD0">
<LN0 lnClass="LLN0" inst="" lnType="T">
<ReportControl name="absent" datSet="ds" confRev="1"><TrgOps dchg="true"/><OptFields/></ReportControl>
<ReportControl name="off" datSet="ds" confRev="1"><TrgOps dchg="true" gi="false"/><OptFields bufOvfl="false"/></ReportControl>
<SampledValueControl name="sv" smvID="SV" smpRate="80" nofASDU="1" confRev="1"/>
<SampledValueControl name="sv2" smvID="SV2" smpRate="80" nofASDU="1" confRev="1" multicast="false"/>
</LN0>
</LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T" lnClass="LLN0"/></DataTypeTemplates>
</SCL>`

// TestDefaultTrueAttributes: an optional boolean attribute whose schema
// default is true reads true when absent and false only when written so.
func TestDefaultTrueAttributes(t *testing.T) {
	s, err := Parse(strings.NewReader(defaultsDoc))
	if err != nil {
		t.Fatal(err)
	}
	ied := s.IEDs[0]
	ln0 := ied.AccessPoints[0].Server.LDevices[0].LN0
	absent, off := ln0.Reports[0], ln0.Reports[1]
	if !absent.TrgOps.GI || !absent.OptFields.BufOvfl {
		t.Errorf("absent: GI %v, BufOvfl %v; want true", absent.TrgOps.GI, absent.OptFields.BufOvfl)
	}
	if off.TrgOps.GI || off.OptFields.BufOvfl {
		t.Errorf("written false: GI %v, BufOvfl %v", off.TrgOps.GI, off.OptFields.BufOvfl)
	}
	if !ln0.SMVControls[0].Multicast || ln0.SMVControls[1].Multicast {
		t.Errorf("Multicast = %v, %v; want true, false", ln0.SMVControls[0].Multicast, ln0.SMVControls[1].Multicast)
	}
	if ied.Services == nil || ied.Services.ConfDataSet == nil || !ied.Services.ConfDataSet.Modify {
		t.Errorf("ConfDataSet = %+v; want modify true", ied.Services.ConfDataSet)
	}
}

// TestEdition1GI: an Edition 1 (schema 1.7) TrgOps cannot state gi; general
// interrogation is allowed, as the 2007 schemas' default says.
func TestEdition1GI(t *testing.T) {
	const doc = `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="x"/>
<IED name="I"><AccessPoint name="AP"><Server><Authentication/><LDevice inst="LD0">
<LN0 lnClass="LLN0" inst="" lnType="T">
<ReportControl name="r" datSet="ds" confRev="1"><TrgOps dchg="true"/><OptFields/></ReportControl>
</LN0></LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T" lnClass="LLN0"/></DataTypeTemplates>
</SCL>`
	s, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if r := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.Reports[0]; !r.TrgOps.GI || !r.TrgOps.Dchg {
		t.Errorf("TrgOps = %+v; want dchg and gi", r.TrgOps)
	}
}
