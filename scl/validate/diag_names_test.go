// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// unresolvedSCL returns a document in which every name is name and
// every reference the passes check is dangling.
func unresolvedSCL(name string) *scl.SCL {
	ln := scl.LN{LNClass: "GGIO", Inst: "1", LNType: "T",
		DataSets: []scl.DataSet{{Name: name, FCDAs: []scl.FCDA{
			{LDInst: name, LNClass: "X"},
			{LDInst: "LD", Prefix: name, LNClass: "X"},
			{LDInst: "LD", LNClass: "GGIO", LNInst: "1", DOName: name + "x", FC: "ST"},
			{LDInst: "LD", LNClass: "GGIO", LNInst: "1", DOName: "D." + name, FC: "ST"},
			{LDInst: "LD", LNClass: "GGIO", LNInst: "1", DOName: "D", DAName: "a", FC: name},
			{LDInst: "LD", LNClass: "GGIO", LNInst: "1", DOName: "D", DAName: "s." + name, FC: "ST"},
			{LDInst: "LD", LNClass: "GGIO", LNInst: "1", DOName: "D", DAName: "a." + name, FC: "ST"},
			{LDInst: "LD", LNClass: "GGIO", LNInst: "1", DOName: "D", FC: name},
		}}},
		Reports:     []scl.ReportControl{{Name: name, DatSet: name + "x"}},
		GSEControls: []scl.GSEControl{{Name: name, DatSet: name + "x"}},
		SMVControls: []scl.SMVControl{{Name: name, DatSet: name + "x"}},
	}
	bad := ln
	bad.Prefix, bad.Inst, bad.LNType = name, "2", name
	ld := scl.LDevice{Inst: "LD", LN0: &scl.LN{LNClass: "LLN0", LNType: "T"}, LNs: []scl.LN{ln, bad}}
	s := &scl.SCL{IEDs: []scl.IED{{Name: "I", AccessPoints: []scl.AccessPoint{{Name: "AP", Server: &scl.Server{LDevices: []scl.LDevice{ld}}}}}}}
	t := &s.DataTypeTemplates
	t.LNodeTypes = []scl.LNodeType{{ID: "T", LNClass: "LLN0", DOs: []scl.DO{{Name: "D", Type: "DT"}, {Name: name, Type: name}}}}
	t.DOTypes = []scl.DOType{{ID: "DT", DAs: []scl.DA{{Name: "a", FC: "MX", BType: "Struct", Type: "AT"}, {Name: name, BType: "Struct", Type: name}, {Name: "e", BType: name}, {Name: "m", BType: "Struct"}},
		SDOs: []scl.SDO{{Name: "s", Type: "ST"}, {Name: name, Type: name}}}, {ID: "ST"}}
	t.DATypes = []scl.DAType{{ID: "AT", BDAs: []scl.BDA{{Name: "b", BType: "Enum", Type: name}}}}
	p := []scl.P{{Type: name, Value: name}, {Type: name, Value: name}, {Type: "MAC-Address", Value: name}, {Type: "APPID", Value: name}}
	s.Communication = &scl.Communication{SubNetworks: []scl.SubNetwork{{Name: name, ConnectedAPs: []scl.ConnectedAP{
		{IEDName: name, APName: name},
		{IEDName: "I", APName: name, Address: p,
			GSEs: []scl.GSEAddress{{LDInst: name, CBName: name, Address: p}, {LDInst: "LD", CBName: name}},
			SMVs: []scl.SMVAddress{{LDInst: name, CBName: name, Address: p}, {LDInst: "LD", CBName: name}}},
	}}}}
	s.Substations = []scl.Substation{{Name: name, LNodes: []scl.LNode{{IEDName: name}, {IEDName: "I", LDInst: name}, {IEDName: "I", LDInst: "LD", Prefix: name, LNClass: "X"}},
		VoltageLevels: []scl.VoltageLevel{{Name: name, Bays: []scl.Bay{{Name: name}}}}}}
	return s
}

// Every diagnostic of the validation passes quotes the names of the
// document cut to scl.DefaultMaxDiagnosticName bytes.
func TestAll_DiagnosticNamesCapped(t *testing.T) {
	name := strings.Repeat("N", 64<<10)
	s := unresolvedSCL(name)
	idx, idxDiags := index.Build(s)
	diags := All(s, idx, idxDiags)
	codes := map[string]bool{}
	for _, d := range diags {
		codes[d.Code] = true
		if n := len(d.Path) + len(d.Message); n > 12*scl.DefaultMaxDiagnosticName {
			t.Errorf("%s diagnostic of %d bytes: %.120q", d.Code, n, d.Path+" "+d.Message)
		}
	}
	for _, c := range []string{"missing-dotype", "missing-datype", "missing-enumtype", "missing-lnodetype",
		"lnclass-mismatch", "missing-connected-ap", "missing-ld", "unresolved-gse-control", "unresolved-smv-control",
		"duplicate-p-type", "invalid-address", "unresolved-fcda", "fcda-fc-mismatch", "missing-dataset",
		"unresolved-topology-lnode", "missing-type"} {
		if !codes[c] {
			t.Errorf("no %s diagnostic", c)
		}
	}
}

// The FC check costs the same whatever FCs the FCDAs name: the FCs below
// a DOType are computed once per SDO level, and an FC that is no FC name
// of the schema is answered without a walk.
func TestDatasets_FCCostIndependentOfFC(t *testing.T) {
	idx, root := fanOutTypes(4, maxSDODepth)
	m := newFCIndex(idx)
	var fcs []string
	for a := 'A'; a <= 'Z'; a++ {
		fcs = append(fcs, string(a))
		for b := 'A'; b <= 'Z'; b++ {
			fcs = append(fcs, string(a)+string(b))
		}
	}
	for i := range 2000 {
		fcs = append(fcs, fmt.Sprintf("fc%d", i))
	}
	for _, fc := range fcs {
		if got := m.doHasFC(root, fc); got != (fc == "ST") {
			t.Fatalf("doHasFC(%q) = %v", fc, got)
		}
	}
	if budget := len(fcs) + (maxSDODepth+1)*4*maxSDODepth; m.steps > budget {
		t.Errorf("%d FCs: %d steps, budget %d", len(fcs), m.steps, budget)
	}
	if len(m.memo) > maxSDODepth+1 {
		t.Errorf("%d memo entries for %d types", len(m.memo), maxSDODepth+1)
	}
	// SG and SE address each other; an undefined SDO type may hold any FC.
	idx.DOTypes["T"+fmt.Sprint(maxSDODepth)].DAs = []scl.DA{{Name: "v", FC: "SE"}}
	if m := newFCIndex(idx); !m.doHasFC(root, "SG") || m.doHasFC(root, "SP") {
		t.Error("SG/SE")
	}
	root.SDOs = append(root.SDOs, scl.SDO{Name: "x", Type: "undefined"})
	if !newFCIndex(idx).doHasFC(root, "zz") {
		t.Error("an undefined SDO type does not count as holding the FC")
	}
}
