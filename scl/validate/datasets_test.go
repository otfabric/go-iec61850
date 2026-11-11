// SPDX-License-Identifier: MIT

package validate

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// FCDA paths through SDOs, constructed attributes (BDAs) and array
// indices resolve; a missing step is reported with the element it
// stopped at, and the FC of a whole (S)DO is checked through its SDOs.
func TestDatasets_FCDAPathWalk(t *testing.T) {
	dtt := `
<LNodeType id="T_LLN0" lnClass="LLN0"><DO name="Mod" type="T_ENC"/></LNodeType>
<LNodeType id="T_MHAI" lnClass="MHAI"><DO name="HA" type="T_HWYE"/><DO name="Ghost" type="T_NONE"/><DO name="Hz" type="T_MV"/></LNodeType>
<DOType id="T_ENC" cdc="ENC"><DA name="stVal" bType="Enum" type="E_Mod" fc="ST"/></DOType>
<DOType id="T_HWYE" cdc="HWYE"><SDO name="phsAHar" type="T_CMV"/><SDO name="lost" type="T_NONE"/><DA name="numHar" bType="INT16U" fc="CF"/></DOType>
<DOType id="T_CMV" cdc="CMV"><DA name="cVal" bType="Struct" type="T_Vector" fc="MX"/><DA name="q" bType="Quality" fc="MX"/></DOType>
<DOType id="T_MV" cdc="MV"><DA name="mag" bType="Struct" type="T_AV" fc="MX"/><DA name="ext" bType="Struct" type="T_NODA" fc="MX"/></DOType>
<DAType id="T_Vector"><BDA name="mag" bType="Struct" type="T_AV"/><BDA name="ang" bType="Struct" type="T_AV"/></DAType>
<DAType id="T_AV"><BDA name="f" bType="FLOAT32"/></DAType>
<EnumType id="E_Mod"><EnumVal ord="1">on</EnumVal></EnumType>`
	fcda := func(do, da, fc string) string {
		return `<FCDA ldInst="LD0" lnClass="MHAI" lnInst="1" doName="` + do + `" daName="` + da + `" fc="` + fc + `"/>`
	}
	tests := []struct {
		name, fcda, code, substr string
	}{
		{"SDO and BDA path", fcda("HA.phsAHar(3)", "cVal.mag.f", "MX"), "", ""},
		{"SDO named in daName", fcda("HA", "phsAHar(1).cVal.ang", "MX"), "", ""},
		{"unknown BDA", fcda("HA.phsAHar(1)", "cVal.mag.i", "MX"), "unresolved-fcda", `"i" not in DAType "T_AV"`},
		{"unknown nested BDA", fcda("Hz", "mag.x", "MX"), "unresolved-fcda", `"x" not in DAType`},
		{"BDA type missing", fcda("Hz", "ext.y", "MX"), "", ""},
		{"unknown SDO", fcda("HA.phsBHar", "", "MX"), "unresolved-fcda", `sub data object "phsBHar"`},
		{"unknown DA", fcda("HA", "nope", "MX"), "unresolved-fcda", `data attribute "nope"`},
		{"DO type missing", fcda("Ghost", "x", "MX"), "", ""},
		{"SDO type missing", fcda("HA.lost", "x", "MX"), "", ""},
		{"FC through an SDO", fcda("HA", "", "MX"), "", ""},
		{"FC on the DO", fcda("HA", "", "CF"), "", ""},
		{"FC absent", fcda("HA.phsAHar", "", "ST"), "fcda-fc-mismatch", "no attribute with FC ST"},
		{"FC of a DA", fcda("HA.phsAHar", "q", "ST"), "fcda-fc-mismatch", `"q" has FC MX`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ln0 := `<DataSet name="DS">` + tc.fcda + `</DataSet>`
			diags := specDoc(t, specIED(ln0, `<LN lnClass="MHAI" inst="1" lnType="T_MHAI"/>`), "", dtt)
			if tc.code == "" {
				noDiag(t, diags, "unresolved-fcda")
				noDiag(t, diags, "fcda-fc-mismatch")
				return
			}
			wantDiag(t, diags, scl.DiagWarning, tc.code, tc.substr)
		})
	}
}

// An SDO whose type is missing may hold the FC; a self-referencing SDO
// chain stops at the depth bound instead of recursing forever.
func TestDatasets_DoHasFCBounds(t *testing.T) {
	dtt := `
<LNodeType id="T_LLN0" lnClass="LLN0"><DO name="Mod" type="T_ENC"/></LNodeType>
<LNodeType id="T_X" lnClass="GGIO"><DO name="Loop" type="T_LOOP"/><DO name="Open" type="T_OPEN"/></LNodeType>
<DOType id="T_ENC" cdc="ENC"><DA name="stVal" bType="Enum" type="E_Mod" fc="ST"/></DOType>
<DOType id="T_LOOP" cdc="WYE"><SDO name="self" type="T_LOOP"/></DOType>
<DOType id="T_OPEN" cdc="WYE"><SDO name="gone" type="T_NONE"/></DOType>
<EnumType id="E_Mod"><EnumVal ord="1">on</EnumVal></EnumType>`
	ds := `<DataSet name="DS">
<FCDA ldInst="LD0" lnClass="GGIO" lnInst="1" doName="Loop" fc="MX"/>
<FCDA ldInst="LD0" lnClass="GGIO" lnInst="1" doName="Open" fc="MX"/>
</DataSet>`
	diags := specDoc(t, specIED(ds, `<LN lnClass="GGIO" inst="1" lnType="T_X"/>`), "", dtt)
	for _, d := range diags {
		if d.Code == "fcda-fc-mismatch" && (strings.Contains(d.Message, "Loop") || strings.Contains(d.Message, "Open")) {
			t.Errorf("guessed a mismatch: %v", d)
		}
	}
	if stripIndex("a(2)") != "a" || stripIndex("b") != "b" {
		t.Error("stripIndex")
	}
}
