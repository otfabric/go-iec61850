// SPDX-License-Identifier: MIT

package scl

import (
	"slices"
	"strings"
	"testing"
)

func diagPaths(diags []Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Path+" "+d.Message)
		}
	}
	return out
}

// Every required string attribute of the model is checked once, with a
// path that names the element by position when its name is missing.
func TestValidateRequired_AllElements(t *testing.T) {
	s := &SCL{
		Substations:   []Substation{{VoltageLevels: []VoltageLevel{{Bays: []Bay{{ConductingEquipments: []ConductingEquipment{{}}}}}}}},
		Communication: &Communication{SubNetworks: []SubNetwork{{ConnectedAPs: []ConnectedAP{{}}}}},
		IEDs: []IED{{AccessPoints: []AccessPoint{
			{},
			{Server: &Server{LDevices: []LDevice{{LNs: []LN{{
				DataSets:    []DataSet{{FCDAs: []FCDA{{}}}},
				Reports:     []ReportControl{{}},
				GSEControls: []GSEControl{{}},
			}}}}}},
		}}},
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{DOs: []DO{{}}}},
			DOTypes:    []DOType{{DAs: []DA{{}}}},
			DATypes:    []DAType{{BDAs: []BDA{{}}}},
			EnumTypes:  []EnumType{{}},
		},
	}
	got := diagPaths(ValidateRequired(s), "missing-attribute")
	for _, want := range []string{
		"Substation[#1] Substation has no name",
		"Substation[#1]/VoltageLevel[#1] VoltageLevel has no name",
		"Substation[#1]/VoltageLevel[#1]/Bay[#1] Bay has no name",
		"Substation[#1]/VoltageLevel[#1]/Bay[#1]/ConductingEquipment[#1] ConductingEquipment has no name",
		"Substation[#1]/VoltageLevel[#1]/Bay[#1]/ConductingEquipment[#1] ConductingEquipment has no type",
		"SubNetwork[#1] SubNetwork has no name",
		"SubNetwork[#1]/ConnectedAP[#1] ConnectedAP has no iedName",
		"SubNetwork[#1]/ConnectedAP[#1] ConnectedAP has no apName",
		"IED[#1] IED has no name",
		"IED[#1]/AccessPoint[#1] AccessPoint has no name",
		"IED[#1]/AccessPoint[#2] AccessPoint has no name",
		"IED[#1]/AccessPoint[#2]/LDevice[#1] LDevice has no inst",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1] LN has no lnClass",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1] LN has no lnType",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1]/DataSet[#1] DataSet has no name",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1]/DataSet[#1]/FCDA[#1] FCDA has no fc",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1]/ReportControl[#1] ReportControl has no name",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1]/GSEControl[#1] GSEControl has no name",
		"IED[#1]/AccessPoint[#2]/LDevice[#1]/LN[#1]/GSEControl[#1] GSEControl has no appID",
		"LNodeType[#1] LNodeType has no id",
		"LNodeType[#1] LNodeType has no lnClass",
		"LNodeType[#1]/DO[#1] DO has no name",
		"LNodeType[#1]/DO[#1] DO has no type",
		"DOType[#1] DOType has no id",
		"DOType[#1] DOType has no cdc",
		"DOType[#1]/DA[#1] DA has no name",
		"DOType[#1]/DA[#1] DA has no bType",
		"DOType[#1]/DA[#1] DA has no fc",
		"DAType[#1] DAType has no id",
		"DAType[#1]/BDA[#1] BDA has no name",
		"DAType[#1]/BDA[#1] BDA has no bType",
		"EnumType[#1] EnumType has no id",
	} {
		if !slices.ContainsFunc(got, func(g string) bool { return strings.HasPrefix(g, want) }) {
			t.Errorf("missing finding %q", want)
		}
	}
	if len(got) != 32 {
		t.Errorf("%d findings, want 32:\n%s", len(got), strings.Join(got, "\n"))
	}
	if ValidateRequired(nil) != nil {
		t.Error("nil document")
	}
}

func TestValidateRequired_CompleteModelIsClean(t *testing.T) {
	doc, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatal(err)
	}
	if got := ValidateRequired(doc); len(got) != 0 {
		t.Errorf("simple.scd: %+v", got)
	}
}

func TestValidateKind(t *testing.T) {
	none, one, two := &SCL{}, &SCL{IEDs: []IED{{Name: "A"}}}, &SCL{IEDs: []IED{{Name: "A"}, {Name: "B"}}}
	withSub := &SCL{Substations: []Substation{{Name: "S"}}}
	for _, tc := range []struct {
		name string
		s    *SCL
		kind DocumentKind
		want []DiagSeverity
	}{
		{"ICD without IED", none, KindICD, []DiagSeverity{DiagError}},
		{"IID without IED", none, KindIID, []DiagSeverity{DiagError}},
		{"CID without IED", none, KindCID, []DiagSeverity{DiagError}},
		{"ICD with one IED", one, KindICD, nil},
		{"IID with two IEDs", two, KindIID, []DiagSeverity{DiagWarning}},
		{"CID with client IEDs", two, KindCID, nil},
		{"SSD without substation", none, KindSSD, []DiagSeverity{DiagWarning}},
		{"SSD with substation", withSub, KindSSD, nil},
		{"SCD without anything", none, KindSCD, nil},
		{"unknown kind", none, KindUnknown, nil},
		{"nil document", nil, KindICD, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []DiagSeverity
			for _, d := range ValidateKind(tc.s, tc.kind) {
				if d.Code != "kind-content" {
					t.Errorf("code = %q", d.Code)
				}
				got = append(got, d.Severity)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("severities = %v, want %v", got, tc.want)
			}
		})
	}
}

// Parsing with semantic validation applies the kind of the file.
func TestParseValidateKind(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"><Header id="t"/></SCL>`
	res, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true, Kind: KindIID})
	if err != nil {
		t.Fatal(err)
	}
	if got := diagPaths(res.Diagnostics, "kind-content"); len(got) != 1 {
		t.Errorf("kind findings = %v", got)
	}
	if _, err := ParseBytes([]byte(xml), ParseOptions{ValidateSemantic: true, Kind: KindIID, Strict: true}); err == nil {
		t.Error("strict parse of an IID without an IED succeeded")
	}
}
