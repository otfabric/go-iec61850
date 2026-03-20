package validate

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

func testModel() *scl.SCL {
	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0: &scl.LN{
							LNClass: "LLN0", LNType: "LNT_LLN0",
							DataSets: []scl.DataSet{
								{Name: "DS1"},
							},
							Reports: []scl.ReportControl{
								{Name: "RC1", DatSet: "DS1"},
							},
						},
						LNs: []scl.LN{
							{LNClass: "MMXU", Inst: "1", LNType: "LNT_MMXU"},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT_LLN0", LNClass: "LLN0", DOs: []scl.DO{{Name: "Mod", Type: "DOT_SPS"}}},
				{ID: "LNT_MMXU", LNClass: "MMXU"},
			},
			DOTypes: []scl.DOType{
				{ID: "DOT_SPS", CDC: "SPS"},
			},
		},
		Communication: &scl.Communication{
			SubNetworks: []scl.SubNetwork{{
				Name: "Net1",
				ConnectedAPs: []scl.ConnectedAP{
					{IEDName: "IED1", APName: "S1"},
				},
			}},
		},
	}
}

func TestAll_ValidModel(t *testing.T) {
	s := testModel()
	idx, idxDiags := index.Build(s)
	diags := All(s, idx, idxDiags)

	for _, d := range diags {
		if d.Severity == scl.DiagError {
			t.Errorf("unexpected error: [%s] %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

func TestTemplates_MissingDOType(t *testing.T) {
	s := &scl.SCL{
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT1", LNClass: "LLN0", DOs: []scl.DO{{Name: "Mod", Type: "GHOST"}}},
			},
		},
	}
	idx, _ := index.Build(s)
	diags := Templates(s, idx)
	found := false
	for _, d := range diags {
		if d.Code == "missing-dotype" && strings.Contains(d.Message, "GHOST") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-dotype diagnostic")
	}
}

func TestIEDs_MissingLNodeType(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0:  &scl.LN{LNClass: "LLN0", LNType: "GHOST"},
					}},
				},
			}},
		}},
	}
	idx, _ := index.Build(s)
	diags := IEDs(s, idx)
	found := false
	for _, d := range diags {
		if d.Code == "missing-lnodetype" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-lnodetype diagnostic")
	}
}

func TestCommunication_MissingIED(t *testing.T) {
	s := &scl.SCL{
		Communication: &scl.Communication{
			SubNetworks: []scl.SubNetwork{{
				Name: "Net1",
				ConnectedAPs: []scl.ConnectedAP{
					{IEDName: "NoSuchIED", APName: "S1"},
				},
			}},
		},
	}
	idx, _ := index.Build(s)
	diags := Communication(s, idx)
	found := false
	for _, d := range diags {
		if d.Code == "missing-connected-ap" && strings.Contains(d.Message, "NoSuchIED") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-connected-ap diagnostic")
	}
}

func TestControls_MissingDataset(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0: &scl.LN{
							LNClass: "LLN0", LNType: "LNT1",
							Reports: []scl.ReportControl{{Name: "RC1", DatSet: "NonExistent"}},
						},
					}},
				},
			}},
		}},
	}
	idx, _ := index.Build(s)
	diags := Controls(s, idx)
	found := false
	for _, d := range diags {
		if d.Code == "missing-dataset" && strings.Contains(d.Message, "NonExistent") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-dataset diagnostic")
	}
}

func TestAll_RealFile(t *testing.T) {
	s, err := scl.ParseFile("../testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	idx, idxDiags := index.Build(s)
	diags := All(s, idx, idxDiags)
	for _, d := range diags {
		if d.Severity == scl.DiagError {
			t.Errorf("unexpected error: [%s] %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

func TestTopology_UnresolvedLNode(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0:  &scl.LN{LNClass: "LLN0", LNType: "LNT1"},
					}},
				},
			}},
		}},
		Substations: []scl.Substation{{
			Name: "Sub1",
			VoltageLevels: []scl.VoltageLevel{{
				Name: "VL1",
				Bays: []scl.Bay{{
					Name: "Bay1",
					LNodes: []scl.LNode{
						{IEDName: "IED1", LDInst: "LD0", LNClass: "LLN0"},
						{IEDName: "NoSuchIED", LDInst: "LD0", LNClass: "MMXU", LNInst: "1"},
					},
				}},
			}},
		}},
	}
	idx, _ := index.Build(s)
	diags := Topology(s, idx)
	found := false
	for _, d := range diags {
		if d.Code == "unresolved-topology-lnode" && strings.Contains(d.Message, "NoSuchIED") {
			found = true
		}
	}
	if !found {
		t.Error("expected unresolved-topology-lnode diagnostic for missing IED")
	}
}

func TestTopology_LNodeNone(t *testing.T) {
	s := &scl.SCL{
		Substations: []scl.Substation{{
			Name: "Sub1",
			LNodes: []scl.LNode{
				{IEDName: "None", LNClass: "MMXU"},
				{IEDName: "", LNClass: "MMXU"},
			},
		}},
	}
	idx, _ := index.Build(s)
	diags := Topology(s, idx)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for IEDName=None/empty, got %d", len(diags))
	}
}

func TestControls_GSE_MissingDataset(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0: &scl.LN{
							LNClass: "LLN0", LNType: "LNT1",
							GSEControls: []scl.GSEControl{{Name: "GC1", DatSet: "NonExistent"}},
						},
					}},
				},
			}},
		}},
	}
	idx, _ := index.Build(s)
	diags := Controls(s, idx)
	found := false
	for _, d := range diags {
		if d.Code == "missing-dataset" && strings.Contains(d.Path, "GC[GC1]") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-dataset diagnostic for GSEControl")
	}
}
