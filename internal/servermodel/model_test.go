// SPDX-License-Identifier: MIT

package servermodel

import (
	"strings"
	"testing"
)

func TestValidate_Valid(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
			}},
		}},
	}
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidate_Empty(t *testing.T) {
	m := &Model{}
	errs := m.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors for empty model")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "no logical devices") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'no logical devices' error")
	}
}

func TestValidate_DuplicateLD(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{
			{Name: "LD1", LogicalNodes: []LogicalNode{{Name: "LLN0", LNClass: "LLN0"}}},
			{Name: "LD1", LogicalNodes: []LogicalNode{{Name: "LLN0", LNClass: "LLN0"}}},
		},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "duplicate logical device") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate LD error")
	}
}

func TestValidate_MissingLLN0(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "GGIO1",
				LNClass: "GGIO",
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "missing mandatory LLN0") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing LLN0 error")
	}
}

func TestValidate_DuplicateLN(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{
				{Name: "LLN0", LNClass: "LLN0"},
				{Name: "GGIO1", LNClass: "GGIO"},
				{Name: "GGIO1", LNClass: "GGIO"},
			},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "duplicate logical node") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate LN error")
	}
}

func TestValidate_EmptyDataSet(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataSets: []DataSetDef{{
					Name:    "dsEmpty",
					Members: nil,
				}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "has no members") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty dataset error")
	}
}

func TestValidate_ReportEmptyDatSet(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				Reports: []ReportDef{{
					Name: "brcb01",
				}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "empty DatSet") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty DatSet error")
	}
}

func TestValidate_DuplicateDataSet(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataSets: []DataSetDef{
					{Name: "ds1", Members: []DataSetMemberDef{{LNName: "LLN0", DOPath: "Mod.stVal", FC: "ST"}}},
					{Name: "ds1", Members: []DataSetMemberDef{{LNName: "LLN0", DOPath: "Mod.q", FC: "ST"}}},
				},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "duplicate dataset") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate dataset error")
	}
}

func TestValidate_EmptyLNClass(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{
				{Name: "LLN0", LNClass: "LLN0"},
				{Name: "GGIO1", LNClass: ""},
			},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "empty LNClass") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty LNClass error")
	}
}

func TestValidate_DuplicateDO(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []DataObject{
					{Name: "Mod", CDC: "INS", Attributes: []DataAttribute{{Name: "stVal", FC: "ST", BType: "INT32"}}},
					{Name: "Mod", CDC: "INS", Attributes: []DataAttribute{{Name: "stVal", FC: "ST", BType: "INT32"}}},
				},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "duplicate DO") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate DO error")
	}
}

func TestValidate_DuplicateDA(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []DataObject{{
					Name: "Mod",
					CDC:  "INS",
					Attributes: []DataAttribute{
						{Name: "stVal", FC: "ST", BType: "INT32"},
						{Name: "stVal", FC: "ST", BType: "INT32"},
					},
				}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "duplicate DA") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate DA error")
	}
}

func TestValidate_DAEmptyFC(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []DataObject{{
					Name: "Mod",
					CDC:  "INS",
					Attributes: []DataAttribute{
						{Name: "stVal", FC: "", BType: "INT32"},
					},
				}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "empty FC") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty FC error")
	}
}

func TestValidate_DAEmptyBType(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []DataObject{{
					Name: "Mod",
					CDC:  "INS",
					Attributes: []DataAttribute{
						{Name: "stVal", FC: "ST", BType: ""},
					},
				}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "empty BType") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty BType error")
	}
}

func TestValidate_DataSetMemberEmptyFields(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataSets: []DataSetDef{{
					Name: "ds1",
					Members: []DataSetMemberDef{
						{LNName: "", DOPath: "", FC: ""},
					},
				}},
			}},
		}},
	}
	errs := m.Validate()

	var foundLN, foundDO, foundFC bool
	for _, err := range errs {
		msg := err.Error()
		if strings.Contains(msg, "empty LNName") {
			foundLN = true
		}
		if strings.Contains(msg, "empty DOPath") {
			foundDO = true
		}
		if strings.Contains(msg, "empty FC") {
			foundFC = true
		}
	}
	if !foundLN {
		t.Error("expected empty LNName error")
	}
	if !foundDO {
		t.Error("expected empty DOPath error")
	}
	if !foundFC {
		t.Error("expected empty FC error for dataset member")
	}
}

func TestValidate_ReportDatSetNotExist(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				Reports: []ReportDef{{
					Name:   "rpt01",
					DatSet: "dsMissing",
				}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "non-existent dataset") {
			found = true
		}
	}
	if !found {
		t.Error("expected non-existent dataset error")
	}
}

func TestValidate_NumOfSGsZero(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				SettingGroup: &SettingGroupDef{
					NumOfSGs: 0,
					ActSG:    1,
				},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "NumOfSGs must be >= 1") {
			found = true
		}
	}
	if !found {
		t.Error("expected NumOfSGs validation error")
	}
}

func TestValidate_LogEmptyName(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				Logs:    []LogDef{{Name: ""}},
			}},
		}},
	}
	errs := m.Validate()
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "log with empty name") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty log name error")
	}
}
