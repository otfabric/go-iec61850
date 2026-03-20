package servermodel

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

func testSCL() *scl.SCL {
	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0: &scl.LN{
							LNClass: "LLN0",
							Inst:    "",
							LNType:  "LNT_LLN0",
							DataSets: []scl.DataSet{{
								Name: "dsEvents",
								FCDAs: []scl.FCDA{{
									LNClass: "LLN0",
									DOName:  "Mod",
									DAName:  "stVal",
									FC:      "ST",
								}},
							}},
							Reports: []scl.ReportControl{{
								Name:      "brcbEvents01",
								RptID:     "rpt01",
								DatSet:    "dsEvents",
								ConfRev:   1,
								Buffered:  true,
								TrgOps:    scl.TrgOps{Dchg: true, Qchg: true},
								OptFields: scl.OptFields{SeqNum: true, TimeStamp: true},
							}},
						},
						LNs: []scl.LN{{
							Prefix:  "",
							LNClass: "GGIO",
							Inst:    "1",
							LNType:  "LNT_GGIO",
							DOIs: []scl.DOI{{
								Name: "Ind1",
								DAIs: []scl.DAI{{Name: "stVal", Val: "true"}},
							}},
						}},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{
					ID:      "LNT_LLN0",
					LNClass: "LLN0",
					DOs: []scl.DO{
						{Name: "Mod", Type: "DOT_INS"},
						{Name: "Health", Type: "DOT_INS"},
					},
				},
				{
					ID:      "LNT_GGIO",
					LNClass: "GGIO",
					DOs: []scl.DO{
						{Name: "Ind1", Type: "DOT_SPS"},
					},
				},
			},
			DOTypes: []scl.DOType{
				{
					ID:  "DOT_INS",
					CDC: "INS",
					DAs: []scl.DA{
						{Name: "stVal", FC: "ST", BType: "INT32"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					},
				},
				{
					ID:  "DOT_SPS",
					CDC: "SPS",
					DAs: []scl.DA{
						{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					},
				},
			},
		},
	}
}

func TestFromSCL_Basic(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	if len(m.LogicalDevices) != 1 {
		t.Fatalf("got %d LDs, want 1", len(m.LogicalDevices))
	}
	ld := m.LogicalDevices[0]
	if ld.Name != "LD1" {
		t.Errorf("LD name = %q, want LD1", ld.Name)
	}

	if len(ld.LogicalNodes) != 2 {
		t.Fatalf("got %d LNs, want 2", len(ld.LogicalNodes))
	}

	lln0 := ld.LogicalNodes[0]
	if lln0.Name != "LLN0" {
		t.Errorf("LN0 name = %q, want LLN0", lln0.Name)
	}
	if len(lln0.DataObjects) != 2 {
		t.Errorf("LLN0 DOs = %d, want 2", len(lln0.DataObjects))
	}
	if len(lln0.DataSets) != 1 {
		t.Errorf("LLN0 DataSets = %d, want 1", len(lln0.DataSets))
	}
	if len(lln0.Reports) != 1 {
		t.Errorf("LLN0 Reports = %d, want 1", len(lln0.Reports))
	}

	ggio := ld.LogicalNodes[1]
	if ggio.Name != "GGIO1" {
		t.Errorf("GGIO name = %q, want GGIO1", ggio.Name)
	}
	if len(ggio.DataObjects) != 1 {
		t.Fatalf("GGIO1 DOs = %d, want 1", len(ggio.DataObjects))
	}
	ind1 := ggio.DataObjects[0]
	if ind1.CDC != "SPS" {
		t.Errorf("Ind1 CDC = %q, want SPS", ind1.CDC)
	}
	if len(ind1.Attributes) != 3 {
		t.Errorf("Ind1 attrs = %d, want 3", len(ind1.Attributes))
	}
}

func TestFromSCL_DataObjects(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	mod := m.LogicalDevices[0].LogicalNodes[0].DataObjects[0]
	if mod.Name != "Mod" {
		t.Errorf("DO name = %q, want Mod", mod.Name)
	}
	if mod.CDC != "INS" {
		t.Errorf("Mod CDC = %q, want INS", mod.CDC)
	}
	if len(mod.Attributes) != 3 {
		t.Fatalf("Mod attrs = %d, want 3", len(mod.Attributes))
	}

	stVal := mod.Attributes[0]
	if stVal.Name != "stVal" || stVal.FC != "ST" || stVal.BType != "INT32" {
		t.Errorf("stVal = %+v, want stVal/ST/INT32", stVal)
	}
}

func TestFromSCL_DAIOverrides(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	ggio := m.LogicalDevices[0].LogicalNodes[1]
	ind1 := ggio.DataObjects[0]
	stVal := ind1.Attributes[0]
	if stVal.InitialValue != "true" {
		t.Errorf("stVal InitialValue = %q, want 'true'", stVal.InitialValue)
	}
}

func TestFromSCL_DataSets(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	ds := m.LogicalDevices[0].LogicalNodes[0].DataSets[0]
	if ds.Name != "dsEvents" {
		t.Errorf("dataset name = %q, want dsEvents", ds.Name)
	}
	if len(ds.Members) != 1 {
		t.Fatalf("dataset members = %d, want 1", len(ds.Members))
	}
	member := ds.Members[0]
	if member.LNName != "LLN0" || member.DOPath != "Mod.stVal" || member.FC != "ST" {
		t.Errorf("member = %+v, want LLN0/Mod.stVal[ST]", member)
	}
}

func TestFromSCL_Reports(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	rpt := m.LogicalDevices[0].LogicalNodes[0].Reports[0]
	if rpt.Name != "brcbEvents01" {
		t.Errorf("report name = %q, want brcbEvents01", rpt.Name)
	}
	if !rpt.Buffered {
		t.Error("expected buffered")
	}
	if !rpt.TrgOps.Dchg || !rpt.TrgOps.Qchg {
		t.Error("expected Dchg and Qchg trigger ops")
	}
	if !rpt.OptFlds.SeqNum || !rpt.OptFlds.TimeStamp {
		t.Error("expected SeqNum and TimeStamp opt fields")
	}
}

func TestFromSCL_IEDNotFound(t *testing.T) {
	s := testSCL()
	_, err := FromSCL(s, "NonExistent", "")
	if err == nil {
		t.Fatal("expected error for non-existent IED")
	}
}

func TestFromSCL_NilSCL(t *testing.T) {
	_, err := FromSCL(nil, "IED1", "")
	if err == nil {
		t.Fatal("expected error for nil SCL")
	}
}

func TestFromSCL_SpecificAP(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "AP1")
	if err != nil {
		t.Fatalf("FromSCL with AP: %v", err)
	}
	if len(m.LogicalDevices) != 1 {
		t.Fatalf("got %d LDs, want 1", len(m.LogicalDevices))
	}
}

func TestFromSCL_APNotFound(t *testing.T) {
	s := testSCL()
	_, err := FromSCL(s, "IED1", "AP_MISSING")
	if err == nil {
		t.Fatal("expected error for non-existent AP")
	}
}

func TestFromSCL_ModelValidates(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}
	if errs := m.Validate(); len(errs) != 0 {
		t.Errorf("model validation errors: %v", errs)
	}
}

func TestFromSCL_UnresolvedLNodeType(t *testing.T) {
	s := testSCL()
	s.DataTypeTemplates.LNodeTypes = nil
	_, err := FromSCL(s, "IED1", "")
	if err == nil {
		t.Fatal("expected error for unresolved LNodeType")
	}
	if !strings.Contains(err.Error(), "unresolved LNodeType") {
		t.Errorf("error = %q, want 'unresolved LNodeType'", err)
	}
}

func TestFromSCL_UnresolvedDOType(t *testing.T) {
	s := testSCL()
	s.DataTypeTemplates.DOTypes = nil
	_, err := FromSCL(s, "IED1", "")
	if err == nil {
		t.Fatal("expected error for unresolved DOType")
	}
	if !strings.Contains(err.Error(), "unresolved DOType") {
		t.Errorf("error = %q, want 'unresolved DOType'", err)
	}
}

func TestFromSCL_UnresolvedDAType(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0:  &scl.LN{LNClass: "LLN0", Inst: "", LNType: "LNT_LLN0"},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID:      "LNT_LLN0",
				LNClass: "LLN0",
				DOs:     []scl.DO{{Name: "Mod", Type: "DOT_MV"}},
			}},
			DOTypes: []scl.DOType{{
				ID:  "DOT_MV",
				CDC: "MV",
				DAs: []scl.DA{{Name: "mag", FC: "MX", BType: "Struct", Type: "DAT_Missing"}},
			}},
		},
	}
	_, err := FromSCL(s, "IED1", "")
	if err == nil {
		t.Fatal("expected error for unresolved DAType")
	}
	if !strings.Contains(err.Error(), "unresolved DAType") {
		t.Errorf("error = %q, want 'unresolved DAType'", err)
	}
}

func TestFromSCL_NestedDAIOverride(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0: &scl.LN{
							LNClass: "LLN0",
							Inst:    "",
							LNType:  "LNT_LLN0",
							DOIs: []scl.DOI{{
								Name: "AnIn1",
								DAIs: []scl.DAI{
									{Name: "mag.f", Val: "3.14"},
								},
							}},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID: "LNT_LLN0", LNClass: "LLN0",
				DOs: []scl.DO{{Name: "AnIn1", Type: "DOT_MV"}},
			}},
			DOTypes: []scl.DOType{{
				ID: "DOT_MV", CDC: "MV",
				DAs: []scl.DA{
					{Name: "mag", FC: "MX", BType: "Struct", Type: "DAT_AnalogValue"},
					{Name: "q", FC: "MX", BType: "Quality"},
					{Name: "t", FC: "MX", BType: "Timestamp"},
				},
			}},
			DATypes: []scl.DAType{{
				ID: "DAT_AnalogValue",
				BDAs: []scl.BDA{
					{Name: "f", BType: "FLOAT32"},
				},
			}},
		},
	}

	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	anin := m.LogicalDevices[0].LogicalNodes[0].DataObjects[0]
	if anin.Name != "AnIn1" {
		t.Fatalf("DO name = %q, want AnIn1", anin.Name)
	}

	var mag *DataAttribute
	for i := range anin.Attributes {
		if anin.Attributes[i].Name == "mag" {
			mag = &anin.Attributes[i]
			break
		}
	}
	if mag == nil {
		t.Fatal("expected 'mag' attribute")
	}
	if len(mag.Children) == 0 {
		t.Fatal("expected children in 'mag' Struct attribute")
	}

	fAttr := mag.Children[0]
	if fAttr.Name != "f" {
		t.Fatalf("child name = %q, want f", fAttr.Name)
	}
	if fAttr.InitialValue != "3.14" {
		t.Errorf("nested DAI override: f.InitialValue = %q, want 3.14", fAttr.InitialValue)
	}
}

func TestFromSCL_SDINestedOverride(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0: &scl.LN{
							LNClass: "LLN0",
							Inst:    "",
							LNType:  "LNT_LLN0",
							DOIs: []scl.DOI{{
								Name: "AnIn1",
								SDIs: []scl.SDI{{
									Name: "mag",
									DAIs: []scl.DAI{{Name: "f", Val: "2.71"}},
								}},
							}},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID: "LNT_LLN0", LNClass: "LLN0",
				DOs: []scl.DO{{Name: "AnIn1", Type: "DOT_MV"}},
			}},
			DOTypes: []scl.DOType{{
				ID: "DOT_MV", CDC: "MV",
				DAs: []scl.DA{
					{Name: "mag", FC: "MX", BType: "Struct", Type: "DAT_AnalogValue"},
					{Name: "q", FC: "MX", BType: "Quality"},
					{Name: "t", FC: "MX", BType: "Timestamp"},
				},
			}},
			DATypes: []scl.DAType{{
				ID: "DAT_AnalogValue",
				BDAs: []scl.BDA{
					{Name: "f", BType: "FLOAT32"},
				},
			}},
		},
	}

	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	anin := m.LogicalDevices[0].LogicalNodes[0].DataObjects[0]
	var mag *DataAttribute
	for i := range anin.Attributes {
		if anin.Attributes[i].Name == "mag" {
			mag = &anin.Attributes[i]
			break
		}
	}
	if mag == nil {
		t.Fatal("expected 'mag' attribute")
	}

	fAttr := mag.Children[0]
	if fAttr.InitialValue != "2.71" {
		t.Errorf("SDI nested DAI override: f.InitialValue = %q, want 2.71", fAttr.InitialValue)
	}
}
