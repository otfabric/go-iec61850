// SPDX-License-Identifier: MIT

package servermodel

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// overrideSCL builds an IED whose MMXU uses a WYE-like DO with a sub-DO,
// nested structured DAs, array-count DAs/BDAs and self-referencing types,
// plus DOI/SDI/DAI instance overrides that walk every override path.
func overrideSCL() *scl.SCL {
	return &scl.SCL{
		Communication: &scl.Communication{SubNetworks: []scl.SubNetwork{{
			Name: "SN1",
			ConnectedAPs: []scl.ConnectedAP{
				{IEDName: "OTHER", APName: "AP1", GSEs: []scl.GSEAddress{{LDInst: "LD1", CBName: "gcb"}}},
				{IEDName: "IED1", APName: "AP1", GSEs: []scl.GSEAddress{{LDInst: "LD1", CBName: "gcb", MinTime: "4"}}},
			},
		}}},
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{LDevices: []scl.LDevice{{
					Inst: "LD1",
					LN0: &scl.LN{
						LNClass: "LLN0", LNType: "LNT_LLN0",
						GSEControls: []scl.GSEControl{
							{Name: "gcb", AppID: "app1", Type: "GOOSE"},
							{Name: "gssecb", Type: "GSSE"},
						},
					},
					LNs: []scl.LN{{
						LNClass: "MMXU", Inst: "1", LNType: "LNT_MMXU",
						DOIs: []scl.DOI{
							{
								Name: "PhV",
								DAIs: []scl.DAI{{Name: "d", Val: "phase voltages"}},
								SDIs: []scl.SDI{
									{
										Name: "phsA",
										DAIs: []scl.DAI{
											{Name: "cVal.mag.i", Val: "7"},
											{Name: "cVal.nope.i", Val: "0"},
											{Name: "nope", Val: "0"},
										},
										SDIs: []scl.SDI{{
											Name: "cVal",
											DAIs: []scl.DAI{{Name: "ang", Val: "2.0"}, {Name: "zz", Val: "0"}},
											SDIs: []scl.SDI{
												{Name: "mag", DAIs: []scl.DAI{{Name: "f", Val: "3.0"}, {Name: "yy", Val: "0"}}},
												{Name: "bogus"},
											},
										}},
									},
									{Name: "missing"},
								},
							},
							{Name: "Ghost", DAIs: []scl.DAI{{Name: "stVal", Val: "1"}}},
						},
					}},
				}}},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT_LLN0", LNClass: "LLN0", DOs: []scl.DO{{Name: "Rec", Type: "DOT_REC"}}},
				{ID: "LNT_MMXU", LNClass: "MMXU", DOs: []scl.DO{{Name: "PhV", Type: "DOT_WYE"}}},
			},
			DOTypes: []scl.DOType{
				{
					ID: "DOT_WYE", CDC: "WYE",
					SDOs: []scl.SDO{{Name: "phsA", Type: "DOT_CMV"}},
					DAs:  []scl.DA{{Name: "d", FC: "DC", BType: "VisString255"}},
				},
				{
					ID: "DOT_CMV", CDC: "CMV",
					DAs: []scl.DA{
						{Name: "cVal", FC: "MX", BType: "Struct", Type: "DAT_Vector"},
						{Name: "arr", FC: "MX", BType: "FLOAT32", Count: 3},
					},
				},
				{
					// Self-referencing DOType and DAType: expansion must
					// terminate instead of recursing forever.
					ID: "DOT_REC", CDC: "REC",
					SDOs: []scl.SDO{{Name: "self", Type: "DOT_REC"}},
					DAs:  []scl.DA{{Name: "r", FC: "CF", BType: "Struct", Type: "DAT_REC"}},
				},
			},
			DATypes: []scl.DAType{
				{ID: "DAT_Vector", BDAs: []scl.BDA{
					{Name: "mag", BType: "Struct", Type: "DAT_AV"},
					{Name: "ang", BType: "FLOAT32"},
				}},
				{ID: "DAT_AV", BDAs: []scl.BDA{
					{Name: "f", BType: "FLOAT32"},
					{Name: "i", BType: "INT32"},
					{Name: "hist", BType: "FLOAT32", Count: 2},
				}},
				{ID: "DAT_REC", BDAs: []scl.BDA{
					{Name: "v", BType: "INT32"},
					{Name: "next", BType: "Struct", Type: "DAT_REC"},
				}},
			},
		},
	}
}

func findDOByName(objs []DataObject, name string) *DataObject {
	for i := range objs {
		if objs[i].Name == name {
			return &objs[i]
		}
	}
	return nil
}

func findDAPath(attrs []DataAttribute, path ...string) *DataAttribute {
	for i := range attrs {
		if attrs[i].Name == path[0] {
			if len(path) == 1 {
				return &attrs[i]
			}
			return findDAPath(attrs[i].Children, path[1:]...)
		}
	}
	return nil
}

func hasWarning(ws []string, substrs ...string) bool {
	for _, w := range ws {
		ok := true
		for _, s := range substrs {
			if !strings.Contains(w, s) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestFromSCL_OverridesAcrossSDOsAndStructs(t *testing.T) {
	m, err := FromSCL(overrideSCL(), "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}
	ld := m.LogicalDevices[0]
	if len(ld.LogicalNodes) != 2 {
		t.Fatalf("want LLN0 + MMXU1, got %d LNs", len(ld.LogicalNodes))
	}
	mmxu := ld.LogicalNodes[1]
	phv := findDOByName(mmxu.DataObjects, "PhV")
	if phv == nil || phv.CDC != "WYE" {
		t.Fatalf("PhV = %+v", phv)
	}
	if d := findDAPath(phv.Attributes, "d"); d == nil || d.InitialValue != "phase voltages" || !d.Overridden {
		t.Errorf("PhV.d = %+v", d)
	}
	phsA := findDOByName(phv.Children, "phsA")
	if phsA == nil || phsA.CDC != "CMV" {
		t.Fatalf("phsA = %+v", phsA)
	}
	checks := []struct {
		path []string
		val  string
	}{
		{[]string{"cVal", "mag", "i"}, "7"},   // dotted DAI on the sub-DO
		{[]string{"cVal", "ang"}, "2.0"},      // DAI inside SDI on a struct attribute
		{[]string{"cVal", "mag", "f"}, "3.0"}, // DAI inside nested SDI on attribute child
	}
	for _, c := range checks {
		a := findDAPath(phsA.Attributes, c.path...)
		if a == nil {
			t.Errorf("%v: not found", c.path)
			continue
		}
		if a.InitialValue != c.val || !a.Overridden {
			t.Errorf("%v = %q (overridden=%v), want %q", c.path, a.InitialValue, a.Overridden, c.val)
		}
		if a.FC != "MX" {
			t.Errorf("%v FC = %q, want inherited MX", c.path, a.FC)
		}
	}
	if h := findDAPath(phsA.Attributes, "cVal", "mag", "hist"); h == nil || h.Overridden {
		t.Errorf("hist should exist and be untouched: %+v", h)
	}

	ws := m.Warnings
	wantWarnings := [][]string{
		{"DOI \"Ghost\" does not match any DO"},
		{"LD1/MMXU1.PhV.phsA", "DAI \"cVal.nope.i\" does not match any attribute"},
		{"LD1/MMXU1.PhV.phsA", "DAI \"nope\" does not match any attribute"},
		{"LD1/MMXU1.PhV.phsA.cVal", "DAI \"zz\" does not match any child"},
		{"LD1/MMXU1.PhV.phsA.cVal.mag", "DAI \"yy\" does not match any child"},
		{"LD1/MMXU1.PhV.phsA.cVal", "SDI \"bogus\" does not match any child"},
		{"LD1/MMXU1.PhV", "SDI \"missing\" does not match any child or attribute"},
		{"DOType \"DOT_CMV\" DA \"arr\": count=3"},
		{"DAType \"DAT_AV\" BDA \"hist\": count=2"},
	}
	for _, w := range wantWarnings {
		if !hasWarning(ws, w...) {
			t.Errorf("missing warning containing %q\nall warnings:\n%s", w, strings.Join(ws, "\n"))
		}
	}
}

func TestFromSCL_RecursiveTypesTerminate(t *testing.T) {
	m, err := FromSCL(overrideSCL(), "IED1", "AP1")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}
	lln0 := m.LogicalDevices[0].LogicalNodes[0]
	rec := findDOByName(lln0.DataObjects, "Rec")
	if rec == nil {
		t.Fatal("Rec DO missing")
	}
	// The self-referencing SDO is materialised once, without its own children.
	self := findDOByName(rec.Children, "self")
	if self == nil || self.CDC != "REC" || len(self.Children) != 0 || len(self.Attributes) != 0 {
		t.Errorf("self = %+v, want a single CDC-only stub", self)
	}
	next := findDAPath(rec.Attributes, "r", "next")
	if next == nil || len(next.Children) != 0 {
		t.Errorf("r.next = %+v, want struct stub with no children", next)
	}
	if v := findDAPath(rec.Attributes, "r", "v"); v == nil || v.FC != "CF" {
		t.Errorf("r.v = %+v", v)
	}
}

func TestFromSCL_GoCBUsesOwnConnectedAPAndSkipsGSSE(t *testing.T) {
	m, err := FromSCL(overrideSCL(), "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}
	lln0 := m.LogicalDevices[0].LogicalNodes[0]
	if len(lln0.GoCBs) != 1 {
		t.Fatalf("GoCBs = %+v, want only the GOOSE control (GSSE skipped)", lln0.GoCBs)
	}
	if lln0.GoCBs[0].Name != "gcb" {
		t.Errorf("GoCB name = %q", lln0.GoCBs[0].Name)
	}
}

func TestFromSCL_ConversionErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(s *scl.SCL)
		wantErr []string
	}{
		{
			name: "multiple servers without apName",
			mutate: func(s *scl.SCL) {
				ied := &s.IEDs[0]
				ied.AccessPoints = append(ied.AccessPoints, scl.AccessPoint{Name: "AP2", Server: &scl.Server{}})
			},
			wantErr: []string{"2 AccessPoints with Server", "specify apName"},
		},
		{
			name: "no server at all",
			mutate: func(s *scl.SCL) {
				s.IEDs[0].AccessPoints = []scl.AccessPoint{{Name: "Client"}}
			},
			wantErr: []string{`no Server in IED "IED1"`},
		},
		{
			name: "LN with empty LNType",
			mutate: func(s *scl.SCL) {
				s.IEDs[0].AccessPoints[0].Server.LDevices[0].LNs[0].LNType = ""
			},
			wantErr: []string{`LD "LD1"`, "LN MMXU1", "empty LNType"},
		},
		{
			name: "SDO with unresolved DOType",
			mutate: func(s *scl.SCL) {
				s.DataTypeTemplates.DOTypes[0].SDOs[0].Type = "DOT_NOPE"
			},
			wantErr: []string{"DO PhV", `unresolved DOType "DOT_NOPE"`},
		},
		{
			name: "struct DA without type reference",
			mutate: func(s *scl.SCL) {
				s.DataTypeTemplates.DOTypes[1].DAs[0].Type = ""
			},
			wantErr: []string{"DA cVal", `struct DA "cVal" has no type reference`},
		},
		{
			name: "struct BDA with unresolved DAType",
			mutate: func(s *scl.SCL) {
				s.DataTypeTemplates.DATypes[0].BDAs[0].Type = "DAT_NOPE"
			},
			wantErr: []string{`unresolved DAType "DAT_NOPE" for DA "mag"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := overrideSCL()
			tt.mutate(s)
			m, err := FromSCL(s, "IED1", "")
			if err == nil {
				t.Fatalf("expected error, got model %+v", m)
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
		})
	}
}
