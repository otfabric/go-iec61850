// SPDX-License-Identifier: MIT

package validate

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// specDoc wraps IED, Communication and DataTypeTemplates content in an
// SCL document, parses it and runs every validation pass.
func specDoc(t *testing.T, ied, comm, dtt string) []scl.Diagnostic {
	t.Helper()
	doc := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
<Header id="x"/>` + comm + ied + `<DataTypeTemplates>` + dtt + `</DataTypeTemplates></SCL>`
	s, err := scl.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	idx, idiags := index.Build(s)
	return All(s, idx, idiags)
}

func wantDiag(t *testing.T, diags []scl.Diagnostic, sev scl.DiagSeverity, code, substr string) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code && d.Severity == sev && (strings.Contains(d.Message, substr) || strings.Contains(d.Path, substr)) {
			return
		}
	}
	t.Errorf("no %v %q containing %q in %v", sev, code, substr, diags)
}

func noDiag(t *testing.T, diags []scl.Diagnostic, code string) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code {
			t.Errorf("unexpected %s: %s %s", code, d.Path, d.Message)
		}
	}
}

const specTemplates = `
<LNodeType id="T_LLN0" lnClass="LLN0"><DO name="Mod" type="T_ENC"/></LNodeType>
<LNodeType id="T_GGIO" lnClass="GGIO"><DO name="Ind" type="T_SPS"/><DO name="Pos" type="T_DPC"/></LNodeType>
<DOType id="T_ENC" cdc="ENC"><DA name="stVal" bType="Enum" type="E_Mod" fc="ST"/></DOType>
<DOType id="T_SPS" cdc="SPS"><DA name="stVal" bType="BOOLEAN" fc="ST"/><DA name="q" bType="Quality" fc="ST"/></DOType>
<DOType id="T_DPC" cdc="DPC"><DA name="stVal" bType="Dbpos" fc="ST"/></DOType>
<EnumType id="E_Mod"><EnumVal ord="1">on</EnumVal></EnumType>`

func specIED(ln0, lns string) string {
	return `<IED name="I"><AccessPoint name="AP"><Server><Authentication/><LDevice inst="LD0">` +
		`<LN0 lnClass="LLN0" inst="" lnType="T_LLN0">` + ln0 + `</LN0>` + lns +
		`</LDevice></Server></AccessPoint></IED>`
}

func TestSpec_SCLXSD_DataSetFCDAResolved(t *testing.T) {
	ds := `<DataSet name="DS">
<FCDA ldInst="LD0" prefix="" lnClass="GGIO" lnInst="1" doName="Ind" daName="stVal" fc="ST"/>
<FCDA ldInst="LD0" lnClass="XXXX" lnInst="1" doName="Ind" fc="ST"/>
<FCDA ldInst="LD0" lnClass="GGIO" lnInst="1" doName="NoSuch" fc="ST"/>
<FCDA ldInst="LD0" lnClass="GGIO" lnInst="1" doName="Pos" fc="MX"/>
</DataSet>`
	diags := specDoc(t, specIED(ds, `<LN lnClass="GGIO" inst="1" lnType="T_GGIO"/>`), "", specTemplates)
	wantDiag(t, diags, scl.DiagWarning, "unresolved-fcda", "XXXX")
	wantDiag(t, diags, scl.DiagWarning, "unresolved-fcda", "NoSuch")
	wantDiag(t, diags, scl.DiagWarning, "fcda-fc-mismatch", "Pos")
	for _, d := range diags {
		if strings.Contains(d.Message, "doName \"Ind\"") || strings.Contains(d.Message, "Ind.stVal") {
			t.Errorf("resolvable FCDA reported: %v", d)
		}
	}
}

func TestSpec_SCLXSD_UniqueNames(t *testing.T) {
	dtt := `<LNodeType id="T_LLN0" lnClass="LLN0"><DO name="Mod" type="T_ENC"/><DO name="Mod" type="T_ENC"/></LNodeType>
<DOType id="T_ENC" cdc="ENC"><DA name="stVal" bType="Enum" type="E_Mod" fc="ST"/></DOType>
<EnumType id="E_Mod"><EnumVal ord="1">on</EnumVal><EnumVal ord="1">blocked</EnumVal></EnumType>`
	ln0 := `<DataSet name="DS"/><GSEControl name="G" appID="a" datSet="DS"/><GSEControl name="G" appID="b" datSet="DS"/>`
	diags := specDoc(t, specIED(ln0, ""), "", dtt)
	wantDiag(t, diags, scl.DiagError, "duplicate-name", "Mod")
	wantDiag(t, diags, scl.DiagError, "duplicate-name", "GSEControl")
	wantDiag(t, diags, scl.DiagError, "duplicate-name", "ord")
}

func TestSpec_SCLXSD_StructEnumNeedType(t *testing.T) {
	dtt := `<LNodeType id="T_LLN0" lnClass="LLN0"><DO name="Mod" type="T_X"/></LNodeType>
<DOType id="T_X" cdc="ENC"><DA name="stVal" bType="Enum" fc="ST"/><DA name="s" bType="Struct" fc="ST"/></DOType>`
	diags := specDoc(t, specIED("", ""), "", dtt)
	wantDiag(t, diags, scl.DiagError, "missing-type", "stVal")
	wantDiag(t, diags, scl.DiagError, "missing-type", "s")
}

func TestSpec_SCLXSD_SMVSmpRateNofASDU(t *testing.T) {
	ln0 := `<DataSet name="DS"/><SampledValueControl name="sv" smvID="id" datSet="DS" smpRate="0" nofASDU="0"><SmvOpts/></SampledValueControl>`
	diags := specDoc(t, specIED(ln0, ""), "", specTemplates)
	wantDiag(t, diags, scl.DiagError, "invalid-attribute", "smpRate")
	wantDiag(t, diags, scl.DiagError, "invalid-attribute", "nofASDU")
}

func TestSpec_SCLXSD_LNClassMatchesLNodeType(t *testing.T) {
	diags := specDoc(t, specIED("", `<LN lnClass="FSCC" inst="1" lnType="T_GGIO"/>`), "", specTemplates)
	wantDiag(t, diags, scl.DiagError, "lnclass-mismatch", "FSCC")
	diags = specDoc(t, specIED("", `<LN lnClass="GGIO" inst="1" lnType="T_GGIO"/>`), "", specTemplates)
	noDiag(t, diags, "lnclass-mismatch")
}

func specComm(gse string) string {
	return `<Communication><SubNetwork name="N"><ConnectedAP iedName="I" apName="AP">` + gse +
		`</ConnectedAP></SubNetwork></Communication>`
}

func TestSpec_8_1_TableB1_AddressChecks(t *testing.T) {
	gse := `<GSE ldInst="LD0" cbName="G1"><Address>
<P type="MAC-Address">01-0C-CD-01-00-01</P><P type="APPID">4000</P><P type="VLAN-PRIORITY">8</P></Address></GSE>
<GSE ldInst="LD0" cbName="G2"><Address>
<P type="MAC-Address">01:0c:cd:01:00:01</P><P type="APPID">4000</P></Address></GSE>
<GSE ldInst="LD0" cbName="G3"><Address>
<P type="MAC-Address">00-0C-CD-01-00-02</P><P type="APPID">0001</P><P type="VLAN-PRIORITY">4</P></Address></GSE>
<SMV ldInst="LD0" cbName="S1"><Address>
<P type="MAC-Address">01-0C-CD-01-00-09</P><P type="APPID">0001</P><P type="APPID">0002</P></Address></SMV>`
	diags := specDoc(t, specIED("", ""), specComm(gse), specTemplates)
	wantDiag(t, diags, scl.DiagWarning, "appid-range", "GOOSE APPID 0x4000")
	wantDiag(t, diags, scl.DiagWarning, "appid-range", "SV APPID 0x0002")
	wantDiag(t, diags, scl.DiagError, "invalid-address", "VLAN-PRIORITY")
	wantDiag(t, diags, scl.DiagWarning, "address-format", "01:0c:cd:01:00:01")
	wantDiag(t, diags, scl.DiagWarning, "duplicate-address", "G2")
	wantDiag(t, diags, scl.DiagWarning, "mac-not-multicast", "00-0C-CD-01-00-02")
	wantDiag(t, diags, scl.DiagWarning, "address-format", "VLAN-PRIORITY without VLAN-ID")
	wantDiag(t, diags, scl.DiagWarning, "duplicate-p-type", "APPID")
	wantDiag(t, diags, scl.DiagInfo, "mac-range", "01-0C-CD-01-00-09")
}

func TestSpec_8_1_TableB1_AddressesClean(t *testing.T) {
	gse := `<Address><P type="IP">10.0.0.1</P></Address>
<GSE ldInst="LD0" cbName="G1"><Address>
<P type="MAC-Address">01-0C-CD-01-00-01</P><P type="APPID">0001</P><P type="VLAN-ID">000</P><P type="VLAN-PRIORITY">4</P></Address></GSE>
<SMV ldInst="LD0" cbName="S1"><Address>
<P type="MAC-Address">01-0C-CD-04-00-01</P><P type="APPID">4000</P></Address></SMV>`
	diags := Addresses(mustParse(t, specComm(gse)), nil)
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	bad := `<Address><P type="IP">10.0.0.256</P></Address><GSE ldInst="LD0" cbName="G"><Address>
<P type="MAC-Address">01-0C-CD-01-00</P><P type="APPID">zz</P><P type="VLAN-ID">1000</P></Address></GSE>`
	diags = Addresses(mustParse(t, specComm(bad)), nil)
	for _, w := range []string{"IP", "MAC-Address", "APPID", "VLAN-ID"} {
		wantDiag(t, diags, scl.DiagError, "invalid-address", w+" ")
	}
	if Addresses(&scl.SCL{}, nil) != nil {
		t.Fatal("no Communication: want nil")
	}
}

func mustParse(t *testing.T, comm string) *scl.SCL {
	t.Helper()
	s, err := scl.Parse(strings.NewReader(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="x"/>` + comm + `</SCL>`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
