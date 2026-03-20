package index

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
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
								{Name: "DS1", FCDAs: []scl.FCDA{{LDInst: "LD0", LNClass: "MMXU", FC: "MX"}}},
							},
							Reports: []scl.ReportControl{
								{Name: "RC1", DatSet: "DS1"},
							},
						},
						LNs: []scl.LN{
							{Prefix: "", LNClass: "MMXU", Inst: "1", LNType: "LNT_MMXU"},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT_LLN0", LNClass: "LLN0"},
				{ID: "LNT_MMXU", LNClass: "MMXU"},
			},
			DOTypes: []scl.DOType{
				{ID: "DOT_SPS", CDC: "SPS"},
			},
			DATypes: []scl.DAType{
				{ID: "DAT_Struct"},
			},
			EnumTypes: []scl.EnumType{
				{ID: "ET_Health"},
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

func TestBuild_Basic(t *testing.T) {
	s := testModel()
	idx, diags := Build(s)

	for _, d := range diags {
		if d.Severity == scl.DiagError {
			t.Errorf("unexpected error diagnostic: %s: %s", d.Path, d.Message)
		}
	}

	if len(idx.IEDs) != 1 {
		t.Errorf("IEDs = %d, want 1", len(idx.IEDs))
	}
	if len(idx.AccessPoints) != 1 {
		t.Errorf("AccessPoints = %d, want 1", len(idx.AccessPoints))
	}
	if len(idx.LDevices) != 1 {
		t.Errorf("LDevices = %d, want 1", len(idx.LDevices))
	}
	if len(idx.LNs) != 2 {
		t.Errorf("LNs = %d, want 2 (LLN0 + MMXU1)", len(idx.LNs))
	}
	if len(idx.LNodeTypes) != 2 {
		t.Errorf("LNodeTypes = %d, want 2", len(idx.LNodeTypes))
	}
	if len(idx.DOTypes) != 1 {
		t.Errorf("DOTypes = %d, want 1", len(idx.DOTypes))
	}
	if len(idx.DATypes) != 1 {
		t.Errorf("DATypes = %d, want 1", len(idx.DATypes))
	}
	if len(idx.EnumTypes) != 1 {
		t.Errorf("EnumTypes = %d, want 1", len(idx.EnumTypes))
	}
	if len(idx.DataSets) != 1 {
		t.Errorf("DataSets = %d, want 1", len(idx.DataSets))
	}
	if len(idx.Reports) != 1 {
		t.Errorf("Reports = %d, want 1", len(idx.Reports))
	}
	if len(idx.ConnectedAPs) != 1 {
		t.Errorf("ConnectedAPs = %d, want 1", len(idx.ConnectedAPs))
	}
}

func TestBuild_DuplicateIED(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{
			{Name: "IED1"},
			{Name: "IED1"},
		},
	}
	_, diags := Build(s)
	found := false
	for _, d := range diags {
		if d.Code == "duplicate-ied" {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate-ied diagnostic")
	}
}

func TestBuild_DuplicateLNodeType(t *testing.T) {
	s := &scl.SCL{
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT1", LNClass: "LLN0"},
				{ID: "LNT1", LNClass: "MMXU"},
			},
		},
	}
	_, diags := Build(s)
	found := false
	for _, d := range diags {
		if d.Code == "duplicate-id" && strings.Contains(d.Message, "LNodeType") {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate-id diagnostic for LNodeType")
	}
}

func TestBuild_DuplicateAccessPoint(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{
				{Name: "S1"},
				{Name: "S1"},
			},
		}},
	}
	_, diags := Build(s)
	found := false
	for _, d := range diags {
		if d.Code == "duplicate-access-point" {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate-access-point diagnostic")
	}
}

func TestBuild_DuplicateLDevice(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{
						{Inst: "LD0"},
						{Inst: "LD0"},
					},
				},
			}},
		}},
	}
	_, diags := Build(s)
	found := false
	for _, d := range diags {
		if d.Code == "duplicate-ld" {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate-ld diagnostic")
	}
}

func TestBuild_EmptySCL(t *testing.T) {
	s := &scl.SCL{}
	idx, diags := Build(s)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for empty SCL, got %d", len(diags))
	}
	if len(idx.IEDs) != 0 {
		t.Errorf("IEDs = %d, want 0", len(idx.IEDs))
	}
}

func TestResolvers(t *testing.T) {
	s := testModel()
	idx, _ := Build(s)

	if ied := idx.FindIED("IED1"); ied == nil {
		t.Error("FindIED(IED1) = nil")
	}
	if ied := idx.FindIED("NoSuch"); ied != nil {
		t.Error("FindIED(NoSuch) should be nil")
	}

	if ap := idx.FindAccessPoint("IED1", "S1"); ap == nil {
		t.Error("FindAccessPoint(IED1, S1) = nil")
	}

	if ld := idx.FindLDevice("IED1", "LD0"); ld == nil {
		t.Error("FindLDevice(IED1, LD0) = nil")
	}

	if ln := idx.FindLN("IED1", "LD0", "", "LLN0", ""); ln == nil {
		t.Error("FindLN LLN0 = nil")
	}
	if ln := idx.FindLN("IED1", "LD0", "", "MMXU", "1"); ln == nil {
		t.Error("FindLN MMXU1 = nil")
	}

	if lnt := idx.FindLNodeType("LNT_LLN0"); lnt == nil {
		t.Error("FindLNodeType(LNT_LLN0) = nil")
	}
	if dot := idx.FindDOType("DOT_SPS"); dot == nil {
		t.Error("FindDOType(DOT_SPS) = nil")
	}
	if dat := idx.FindDAType("DAT_Struct"); dat == nil {
		t.Error("FindDAType(DAT_Struct) = nil")
	}
	if et := idx.FindEnumType("ET_Health"); et == nil {
		t.Error("FindEnumType(ET_Health) = nil")
	}

	if ds := idx.FindDataSet("IED1", "LD0", "", "LLN0", "", "DS1"); ds == nil {
		t.Error("FindDataSet DS1 = nil")
	}
	if rc := idx.FindReport("IED1", "LD0", "", "LLN0", "", "RC1"); rc == nil {
		t.Error("FindReport RC1 = nil")
	}

	if cap := idx.FindConnectedAP("IED1", "S1"); cap == nil {
		t.Error("FindConnectedAP(IED1, S1) = nil")
	}
}

func TestResolveLNType(t *testing.T) {
	s := testModel()
	idx, _ := Build(s)

	ln := idx.FindLN("IED1", "LD0", "", "MMXU", "1")
	if ln == nil {
		t.Fatal("LN not found")
	}
	lnt := idx.ResolveLNType(ln)
	if lnt == nil {
		t.Fatal("ResolveLNType returned nil")
	}
	if lnt.ID != "LNT_MMXU" {
		t.Errorf("LNodeType.ID = %q, want LNT_MMXU", lnt.ID)
	}

	if idx.ResolveLNType(nil) != nil {
		t.Error("ResolveLNType(nil) should return nil")
	}
}

func TestBuild_RealFile(t *testing.T) {
	s, err := scl.ParseFile("../testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	idx, diags := Build(s)
	for _, d := range diags {
		if d.Severity == scl.DiagError {
			t.Errorf("diagnostic: %s: %s", d.Path, d.Message)
		}
	}
	if len(idx.IEDs) == 0 {
		t.Error("expected at least one IED")
	}
	if len(idx.LNodeTypes) == 0 {
		t.Error("expected at least one LNodeType")
	}
}
