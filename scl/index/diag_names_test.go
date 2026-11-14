// SPDX-License-Identifier: MIT

package index

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// longNameSCL returns a document in which every name is name and every
// kind of duplicate the index reports occurs k times.
func longNameSCL(name string, k int) *scl.SCL {
	ln := scl.LN{Prefix: name, LNClass: "GGIO", Inst: "1", LNType: name}
	for range k {
		ln.DataSets = append(ln.DataSets, scl.DataSet{Name: name})
		ln.Reports = append(ln.Reports, scl.ReportControl{Name: name})
		ln.GSEControls = append(ln.GSEControls, scl.GSEControl{Name: name})
		ln.SMVControls = append(ln.SMVControls, scl.SMVControl{Name: name})
	}
	ld := scl.LDevice{Inst: name, LNs: []scl.LN{ln, ln}}
	ied := scl.IED{Name: name, AccessPoints: []scl.AccessPoint{
		{Name: name, Server: &scl.Server{LDevices: []scl.LDevice{ld, ld}}}, {Name: name},
	}}
	s := &scl.SCL{IEDs: []scl.IED{ied, ied}}
	t := &s.DataTypeTemplates
	t.LNodeTypes = []scl.LNodeType{{ID: name, DOs: []scl.DO{{Name: name}, {Name: name}}}, {ID: name}}
	t.DOTypes = []scl.DOType{{ID: name, DAs: []scl.DA{{Name: name, FC: name}, {Name: name, FC: "ST"}}, SDOs: []scl.SDO{{Name: name}}}, {ID: name}}
	t.DATypes = []scl.DAType{{ID: name, BDAs: []scl.BDA{{Name: name}, {Name: name}}}, {ID: name}}
	t.EnumTypes = []scl.EnumType{{ID: name, Vals: []scl.EnumVal{{Ord: 1}, {Ord: 1}}}, {ID: name}}
	cap := scl.ConnectedAP{IEDName: name, APName: name,
		GSEs: []scl.GSEAddress{{LDInst: name, CBName: name}, {LDInst: name, CBName: name}},
		SMVs: []scl.SMVAddress{{LDInst: name, CBName: name}, {LDInst: name, CBName: name}}}
	sn := scl.SubNetwork{Name: name, ConnectedAPs: []scl.ConnectedAP{cap, cap}}
	s.Communication = &scl.Communication{SubNetworks: []scl.SubNetwork{sn, sn}}
	return s
}

// Every diagnostic of Build quotes the names of the document cut to
// scl.DefaultMaxDiagnosticName bytes, so the diagnostics grow with the
// number of duplicates, not with their product with the name length.
func TestBuild_DiagnosticNamesCapped(t *testing.T) {
	name := strings.Repeat("N", 64<<10)
	_, diags := Build(longNameSCL(name, 50))
	codes := map[string]bool{}
	for _, d := range diags {
		codes[d.Code] = true
		if n := len(d.Path) + len(d.Message); n > 8*scl.DefaultMaxDiagnosticName {
			t.Fatalf("%s diagnostic of %d bytes: path %.80q", d.Code, n, d.Path)
		}
	}
	for _, c := range []string{"duplicate-name", "duplicate-id", "duplicate-ied", "duplicate-access-point",
		"duplicate-ld", "duplicate-ln", "duplicate-dataset", "duplicate-report", "duplicate-connected-ap"} {
		if !codes[c] {
			t.Errorf("no %s diagnostic", c)
		}
	}
	// Short names are quoted in full.
	_, diags = Build(longNameSCL("x", 2))
	for _, d := range diags {
		if strings.Contains(d.Path+d.Message, "…") {
			t.Errorf("short name cut: %v", d)
		}
	}
}
