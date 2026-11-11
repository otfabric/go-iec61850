// SPDX-License-Identifier: MIT

package scl

import (
	"strings"
	"testing"
)

// LN0 carries the same required attributes as LN plus those of its
// control blocks (SCL_IED.xsd tLN0/tAnyLN, tDataSet, tFCDA, tControl,
// tGSEControl, tSampledValueControl).
func TestSpec_SCLXSD_LN0RequiredAttributes(t *testing.T) {
	doc := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
<Header id="x"/>
<IED name="I"><AccessPoint name="AP"><Server><Authentication/><LDevice inst="LD0">
<LN0 lnClass="LLN0" inst="" lnType="">
<DataSet name=""><FCDA ldInst="LD0" lnClass="GGIO" lnInst="1" doName="Ind" fc=""/></DataSet>
<GSEControl name="" appID="" datSet=""/>
<SampledValueControl name="sv" smvID="" smpRate="80" nofASDU="1"><SmvOpts/></SampledValueControl>
</LN0>
</LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T_LLN0" lnClass="LLN0"/></DataTypeTemplates>
</SCL>`
	s, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(diagPaths(ValidateRequired(s), "missing-attribute"), "\n")
	for _, want := range []string{"lnType", "DataSet", "fc", "GSEControl", "appID", "smvID"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing-attribute for %s not reported:\n%s", want, got)
		}
	}
}

// LN lnClass must equal the lnClass of its LNodeType (SCL_IED.xsd
// LNodeType keyref; the template is defined per class).
func TestSpec_SCLXSD_LNClassMismatch(t *testing.T) {
	s := &SCL{
		IEDs: []IED{{Name: "I", AccessPoints: []AccessPoint{{Name: "AP", Server: &Server{LDevices: []LDevice{{
			Inst: "LD0",
			LNs:  []LN{{LNClass: "FSCC", Inst: "1", LNType: "T"}, {LNClass: "FSCH", Inst: "2", LNType: "T"}},
		}}}}}}},
		DataTypeTemplates: DataTypeTemplates{LNodeTypes: []LNodeType{{ID: "T", LNClass: "FSCH"}}},
	}
	got := diagPaths(Validate(s), "lnclass-mismatch")
	if len(got) != 1 || !strings.Contains(got[0], "FSCC") {
		t.Fatalf("lnclass-mismatch = %v", got)
	}
}

// Unresolved and cyclic type references produce a row with a Status
// instead of disappearing from the flattened model.
func TestSpec_SCLXSD_FlattenUnresolvedStatus(t *testing.T) {
	s := &SCL{
		IEDs: []IED{{Name: "I", AccessPoints: []AccessPoint{{Name: "AP", Server: &Server{LDevices: []LDevice{{
			Inst: "LD0",
			LNs:  []LN{{LNClass: "GGIO", Inst: "1", LNType: "T"}},
		}}}}}}},
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{ID: "T", LNClass: "GGIO", DOs: []DO{{Name: "D", Type: "DOT"}}}},
			DOTypes: []DOType{{ID: "DOT", CDC: "X",
				SDOs: []SDO{{Name: "missing", Type: "NoDOT"}, {Name: "self", Type: "DOT"}},
				DAs: []DA{
					{Name: "nostruct", BType: "Struct", Type: "NoDAT", FC: "ST"},
					{Name: "cyc", BType: "Struct", Type: "DAT", FC: "ST"},
				}}},
			DATypes: []DAType{{ID: "DAT", BDAs: []BDA{
				{Name: "leaf", BType: "BOOLEAN"},
				{Name: "loop", BType: "Struct", Type: "DAT"},
				{Name: "gone", BType: "Struct", Type: "NoDAT"},
			}}},
		},
	}
	status := map[string]string{}
	for _, r := range Flatten(s) {
		status[r.Path] = r.Status
	}
	for path, want := range map[string]string{
		"D.missing":  "unresolved-dotype",
		"D.self":     "type-cycle",
		"D.nostruct": "unresolved-datype",
		"D.cyc.leaf": "",
		"D.cyc.loop": "type-cycle",
		"D.cyc.gone": "unresolved-datype",
	} {
		got, ok := status[path]
		if !ok || got != want {
			t.Errorf("%s: status %q (present %v), want %q; rows %v", path, got, ok, want, status)
		}
	}
}
