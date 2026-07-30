// SPDX-License-Identifier: MIT

package validate

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

func hasCode(diags []scl.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func hasCodeMsg(diags []scl.Diagnostic, code, substr string) bool {
	for _, d := range diags {
		if d.Code == code && strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

func TestCommunication_Edges(t *testing.T) {
	if diags := Communication(&scl.SCL{}, nil); diags != nil {
		t.Fatalf("nil Communication: got %v", diags)
	}

	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0: &scl.LN{
							LNClass:     "LLN0",
							GSEControls: []scl.GSEControl{{Name: "gcbOK"}},
							SMVControls: []scl.SMVControl{{Name: "smvOK"}},
						},
					}},
				},
			}},
		}},
		Communication: &scl.Communication{
			SubNetworks: []scl.SubNetwork{{
				Name: "Net1",
				ConnectedAPs: []scl.ConnectedAP{
					{IEDName: "IED1", APName: "MissingAP"},
					{
						IEDName: "IED1", APName: "S1",
						GSEs: []scl.GSEAddress{
							{LDInst: "", CBName: "ignored"},
							{LDInst: "NoLD", CBName: "gcbX"},
							{LDInst: "LD0", CBName: "gcbMissing"},
							{LDInst: "LD0", CBName: "gcbOK"},
							{LDInst: "LD0", CBName: ""},
						},
						SMVs: []scl.SMVAddress{
							{LDInst: "", CBName: "ignored"},
							{LDInst: "NoLD", CBName: "smvX"},
							{LDInst: "LD0", CBName: "smvMissing"},
							{LDInst: "LD0", CBName: "smvOK"},
							{LDInst: "LD0", CBName: ""},
						},
					},
				},
			}},
		},
	}
	idx, _ := index.Build(s)
	diags := Communication(s, idx)

	if !hasCodeMsg(diags, "missing-connected-ap", "AccessPoint") {
		t.Error("expected missing AccessPoint diagnostic")
	}
	if !hasCodeMsg(diags, "missing-ld", "NoLD") {
		t.Error("expected missing-ld for GSE/SMV")
	}
	if !hasCode(diags, "unresolved-gse-control") {
		t.Error("expected unresolved-gse-control")
	}
	if !hasCode(diags, "unresolved-smv-control") {
		t.Error("expected unresolved-smv-control")
	}
}

func TestControls_Edges(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{
				{Name: "NoServer"},
				{
					Name: "S1",
					Server: &scl.Server{
						LDevices: []scl.LDevice{{
							Inst: "LD0",
							LN0: &scl.LN{
								LNClass:  "LLN0",
								DataSets: []scl.DataSet{{Name: "DS1"}},
								Reports: []scl.ReportControl{
									{Name: "RCEmpty", DatSet: ""},
									{Name: "RCOK", DatSet: "DS1"},
								},
								GSEControls: []scl.GSEControl{
									{Name: "GCEmpty", DatSet: ""},
									{Name: "GCOK", DatSet: "DS1"},
								},
								SMVControls: []scl.SMVControl{
									{Name: "SVEmpty", DatSet: ""},
									{Name: "SVBad", DatSet: "Ghost"},
								},
							},
							LNs: []scl.LN{{
								LNClass: "MMXU", Inst: "1",
								Reports: []scl.ReportControl{{Name: "RC2", DatSet: "Missing"}},
							}},
						}},
					},
				},
			},
		}},
	}
	idx, _ := index.Build(s)
	diags := Controls(s, idx)

	if !hasCodeMsg(diags, "missing-dataset", "Ghost") {
		t.Error("expected SMV missing-dataset")
	}
	if !hasCodeMsg(diags, "missing-dataset", "Missing") {
		t.Error("expected LN missing-dataset")
	}
	for _, d := range diags {
		if strings.Contains(d.Path, "RCEmpty") || strings.Contains(d.Path, "GCEmpty") || strings.Contains(d.Path, "SVEmpty") {
			t.Errorf("empty DatSet should be skipped: %s", d.Path)
		}
		if strings.Contains(d.Path, "RCOK") || strings.Contains(d.Path, "GCOK") {
			t.Errorf("valid DatSet should not error: %s", d.Path)
		}
	}
}

func TestDatasets_Edges(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{
				{Name: "NoServer"},
				{
					Name: "S1",
					Server: &scl.Server{
						LDevices: []scl.LDevice{{
							Inst: "LD0",
							LN0: &scl.LN{
								LNClass: "LLN0",
								DataSets: []scl.DataSet{{
									Name: "DS1",
									FCDAs: []scl.FCDA{
										{LDInst: "", LNClass: "MMXU"},
										{LDInst: "LD0", LNClass: ""},
										{LDInst: "GhostLD", LNClass: "MMXU"},
										{LDInst: "LD0", LNClass: "MMXU"},
									},
								}},
							},
							LNs: []scl.LN{{
								LNClass: "MMXU", Inst: "1",
								DataSets: []scl.DataSet{{
									Name:  "DS2",
									FCDAs: []scl.FCDA{{LDInst: "Other", LNClass: "GGIO"}},
								}},
							}},
						}},
					},
				},
			},
		}},
	}
	idx, _ := index.Build(s)
	diags := Datasets(s, idx)

	if !hasCodeMsg(diags, "unresolved-fcda", "GhostLD") {
		t.Error("expected unresolved-fcda for GhostLD")
	}
	if !hasCodeMsg(diags, "unresolved-fcda", "Other") {
		t.Error("expected unresolved-fcda from LN dataset")
	}
	if len(diags) != 2 {
		t.Errorf("got %d diags, want 2 (skipped incomplete FCDAs)", len(diags))
	}
}

func TestIEDs_Edges(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{
				{Name: "NoServer"},
				{
					Name: "S1",
					Server: &scl.Server{
						LDevices: []scl.LDevice{{
							Inst: "LD0",
							LN0:  &scl.LN{LNClass: "LLN0", LNType: "LNT_OK"},
							LNs: []scl.LN{
								{LNClass: "MMXU", Inst: "1", LNType: "GHOST"},
							},
						}},
					},
				},
			},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{ID: "LNT_OK", LNClass: "LLN0"}},
		},
	}
	idx, _ := index.Build(s)
	diags := IEDs(s, idx)
	if !hasCodeMsg(diags, "missing-lnodetype", "GHOST") {
		t.Error("expected missing-lnodetype for LN")
	}
	for _, d := range diags {
		if strings.Contains(d.Path, "LLN0") {
			t.Errorf("valid LN0 should not error: %v", d)
		}
	}
}

func TestTemplates_Edges(t *testing.T) {
	s := &scl.SCL{
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT1", LNClass: "LLN0", DOs: []scl.DO{{Name: "Mod", Type: "DOT1"}}},
			},
			DOTypes: []scl.DOType{{
				ID:  "DOT1",
				CDC: "SPS",
				DAs: []scl.DA{
					{Name: "plain", BType: "BOOLEAN"},
					{Name: "st", BType: "Struct", Type: "DAT_OK"},
					{Name: "badStruct", BType: "Struct", Type: "DAT_GHOST"},
					{Name: "en", BType: "Enum", Type: "ET_OK"},
					{Name: "badEnum", BType: "Enum", Type: "ET_GHOST"},
					{Name: "other", BType: "INT32", Type: "ignored"},
				},
				SDOs: []scl.SDO{
					{Name: "ok", Type: "DOT1"},
					{Name: "bad", Type: "DOT_GHOST"},
				},
			}},
			DATypes: []scl.DAType{{
				ID: "DAT_OK",
				BDAs: []scl.BDA{
					{Name: "f", BType: "FLOAT32"},
					{Name: "nested", BType: "Struct", Type: "DAT_GHOST2"},
					{Name: "en", BType: "Enum", Type: "ET_GHOST2"},
					{Name: "emptyRef", BType: "Struct", Type: ""},
				},
			}},
			EnumTypes: []scl.EnumType{{ID: "ET_OK"}},
		},
	}
	idx, _ := index.Build(s)
	diags := Templates(s, idx)

	if !hasCode(diags, "missing-datype") {
		t.Error("expected missing-datype")
	}
	if !hasCode(diags, "missing-enumtype") {
		t.Error("expected missing-enumtype")
	}
	if !hasCodeMsg(diags, "missing-dotype", "DOT_GHOST") {
		t.Error("expected missing-dotype for SDO")
	}
	// Resolved Struct/Enum refs and empty typRef must not produce errors.
	for _, d := range diags {
		if strings.Contains(d.Path, ".DA[st]") || strings.Contains(d.Path, ".DA[en]") ||
			strings.Contains(d.Path, ".BDA[emptyRef]") || strings.Contains(d.Path, ".SDO[ok]") {
			t.Errorf("unexpected diagnostic for valid/skipped ref: %v", d)
		}
	}
}

func TestTopology_checkLNodes_Edges(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0:  &scl.LN{LNClass: "LLN0", LNType: "T"},
						LNs:  []scl.LN{{LNClass: "MMXU", Inst: "1", LNType: "T"}},
					}},
				},
			}},
		}},
		Substations: []scl.Substation{{
			Name: "Sub1",
			LNodes: []scl.LNode{
				{IEDName: "IED1", LDInst: "NoLD", LNClass: "LLN0"},
				{IEDName: "IED1", LDInst: "LD0", LNClass: "GGIO", LNInst: "9"},
				{IEDName: "IED1", LDInst: "LD0", LNClass: "MMXU", LNInst: "1"},
				{IEDName: "IED1", LDInst: "", LNClass: "LLN0"}, // no LDInst → skip LD/LN checks
			},
			VoltageLevels: []scl.VoltageLevel{{
				Name: "VL1",
				LNodes: []scl.LNode{
					{IEDName: "Ghost", LDInst: "LD0", LNClass: "LLN0"},
				},
				Bays: []scl.Bay{{
					Name: "Bay1",
					LNodes: []scl.LNode{
						{IEDName: "IED1", LDInst: "LD0", Prefix: "", LNClass: "LLN0", LNInst: ""},
					},
				}},
			}},
		}},
	}
	idx, _ := index.Build(s)
	diags := Topology(s, idx)

	if !hasCodeMsg(diags, "unresolved-topology-lnode", "NoLD") {
		t.Error("expected missing LDevice")
	}
	if !hasCodeMsg(diags, "unresolved-topology-lnode", "GGIO") {
		t.Error("expected missing LN warning")
	}
	if !hasCodeMsg(diags, "unresolved-topology-lnode", "Ghost") {
		t.Error("expected missing IED under VoltageLevel")
	}

	warn := 0
	for _, d := range diags {
		if d.Severity == scl.DiagWarning {
			warn++
		}
	}
	if warn == 0 {
		t.Error("expected at least one warning for missing LN")
	}
}
