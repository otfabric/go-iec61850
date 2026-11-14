// SPDX-License-Identifier: MIT

package servermodel

import (
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// findLNodeType returns the first LNodeType of s with ID id, as the
// lookup of FromSCL resolves it.
func findLNodeType(s *scl.SCL, id string) *scl.LNodeType {
	for i := range s.DataTypeTemplates.LNodeTypes {
		if s.DataTypeTemplates.LNodeTypes[i].ID == id {
			return &s.DataTypeTemplates.LNodeTypes[i]
		}
	}
	return nil
}

// oneLNSCL returns a document with IED "I" whose LN0 has LNodeType "T0"
// with the single DO "D" of DOType "D0".
func oneLNSCL() *scl.SCL {
	s := &scl.SCL{IEDs: []scl.IED{{Name: "I", AccessPoints: []scl.AccessPoint{{Name: "AP", Server: &scl.Server{LDevices: []scl.LDevice{{
		Inst: "LD0", LN0: &scl.LN{LNClass: "LLN0", LNType: "T0"},
	}}}}}}}}
	s.DataTypeTemplates.LNodeTypes = []scl.LNodeType{{ID: "T0", LNClass: "LLN0", DOs: []scl.DO{{Name: "D", Type: "D0"}}}}
	return s
}

// doChainSCL returns a document whose DO has a chain of n DOTypes: D0
// has an SDO of type D1, and so on; D<n-1> has one DA. The last DOType
// is n type references below the logical node.
func doChainSCL(n int) *scl.SCL {
	s := oneLNSCL()
	for i := range n {
		dot := scl.DOType{ID: fmt.Sprintf("D%d", i), CDC: "X"}
		if i < n-1 {
			dot.SDOs = []scl.SDO{{Name: "s", Type: fmt.Sprintf("D%d", i+1)}}
		} else {
			dot.DAs = []scl.DA{{Name: "v", FC: "ST", BType: "BOOLEAN"}}
		}
		s.DataTypeTemplates.DOTypes = append(s.DataTypeTemplates.DOTypes, dot)
	}
	return s
}

// daChainSCL returns a document whose DO has a Struct DA with a chain of
// n DATypes: S0 has a Struct BDA of type S1, and so on. The last DAType
// is n+1 type references below the logical node.
func daChainSCL(n int) *scl.SCL {
	s := oneLNSCL()
	s.DataTypeTemplates.DOTypes = []scl.DOType{{ID: "D0", CDC: "X", DAs: []scl.DA{{Name: "a", FC: "ST", BType: "Struct", Type: "S0"}}}}
	for i := range n {
		dat := scl.DAType{ID: fmt.Sprintf("S%d", i)}
		if i < n-1 {
			dat.BDAs = []scl.BDA{{Name: "b", BType: "Struct", Type: fmt.Sprintf("S%d", i+1)}}
		} else {
			dat.BDAs = []scl.BDA{{Name: "v", BType: "BOOLEAN"}}
		}
		s.DataTypeTemplates.DATypes = append(s.DataTypeTemplates.DATypes, dat)
	}
	return s
}

// withSmallStack runs f with a maximum goroutine stack of 1 MiB, so that
// a recursion that follows a long type chain ends the test binary
// instead of going on for hundreds of MiB.
func withSmallStack(t *testing.T, f func()) {
	t.Helper()
	old := debug.SetMaxStack(1 << 20)
	defer debug.SetMaxStack(old)
	f()
}

// A chain of DOTypes or DATypes deeper than MaxTypeDepth fails with an
// error that wraps scl.ErrLimitExceeded, without recursing along the
// whole chain; up to the limit the model is built.
func TestFromSCL_TypeChainDepth(t *testing.T) {
	withSmallStack(t, func() {
		for name, s := range map[string]*scl.SCL{"DOType": doChainSCL(20_000), "DAType": daChainSCL(20_000)} {
			_, err := FromSCL(s, "I", "")
			if !errors.Is(err, scl.ErrLimitExceeded) || !strings.Contains(err.Error(), "MaxTypeDepth") {
				t.Errorf("%s chain of 20000: %v, want ErrLimitExceeded", name, err)
			}
		}
	})
	max := scl.DefaultMaxTypeDepth
	for _, tc := range []struct {
		name string
		s    *scl.SCL
		opt  int
		fail bool
	}{
		{"DO at the limit", doChainSCL(max), 0, false},
		{"DO beyond the limit", doChainSCL(max + 1), 0, true},
		{"DA at the limit", daChainSCL(max - 1), 0, false},
		{"DA beyond the limit", daChainSCL(max), 0, true},
		{"DO at a lower limit", doChainSCL(3), 3, false},
		{"DO beyond a lower limit", doChainSCL(4), 3, true},
		{"DA beyond a lower limit", daChainSCL(3), 3, true},
		{"DO beyond the limit, raised", doChainSCL(max + 1), max + 1, false},
	} {
		m, err := FromSCLWithOptions(tc.s, "I", "", Options{MaxTypeDepth: tc.opt})
		if tc.fail {
			if m != nil || !errors.Is(err, scl.ErrLimitExceeded) {
				t.Errorf("%s: %v, want ErrLimitExceeded", tc.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// The node limit stops a chain of SDOs too.
func TestFromSCL_SDOChainNodeLimit(t *testing.T) {
	if _, err := FromSCLWithOptions(doChainSCL(3), "I", "", Options{MaxExpandedNodes: 1}); !errors.Is(err, scl.ErrLimitExceeded) {
		t.Errorf("SDO chain in a budget of 1: %v", err)
	}
}

// The LNodeType of an LN is looked up by ID; an ID given twice resolves
// to the first, as before.
func TestFromSCL_LNodeTypeLookup(t *testing.T) {
	const n = 5000
	s := oneLNSCL()
	s.DataTypeTemplates.DOTypes = []scl.DOType{{ID: "D0", CDC: "X", DAs: []scl.DA{{Name: "v", FC: "ST", BType: "BOOLEAN"}}}}
	lnts := []scl.LNodeType{{ID: "T0", LNClass: "LLN0", DOs: []scl.DO{{Name: "D", Type: "D0"}}}}
	for i := range n {
		lnts = append(lnts, scl.LNodeType{ID: fmt.Sprintf("G%d", i), LNClass: "GGIO"})
	}
	// A second "last" type: the first with that ID is used.
	lnts = append(lnts, scl.LNodeType{ID: "last", LNClass: "GGIO", DOs: []scl.DO{{Name: "First", Type: "D0"}}},
		scl.LNodeType{ID: "last", LNClass: "GGIO", DOs: []scl.DO{{Name: "Second", Type: "D0"}}})
	s.DataTypeTemplates.LNodeTypes = lnts
	ld := &s.IEDs[0].AccessPoints[0].Server.LDevices[0]
	for i := range n {
		ld.LNs = append(ld.LNs, scl.LN{LNClass: "GGIO", Inst: fmt.Sprint(i + 1), LNType: "last"})
	}
	m, err := FromSCL(s, "I", "")
	if err != nil {
		t.Fatal(err)
	}
	lns := m.LogicalDevices[0].LogicalNodes
	if len(lns) != n+1 || lns[1].DataObjects[0].Name != "First" || lns[n].DataObjects[0].Name != "First" {
		t.Fatalf("%d LNs, first GGIO %+v", len(lns), lns[1].DataObjects)
	}
}

// Each report control block instance counts against MaxExpandedNodes:
// an indexed ReportControl of 99 instances does not fit a budget of the
// data model alone.
func TestFromSCL_ReportInstancesCount(t *testing.T) {
	s := doChainSCL(1) // D and v: two nodes
	s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.Reports = []scl.ReportControl{{Name: "rcb", Indexed: true, MaxInstances: 99}}
	for _, tc := range []struct {
		max  int
		fail bool
	}{{2 + 99, false}, {2 + 98, true}, {3, true}} {
		m, err := FromSCLWithOptions(s, "I", "", Options{MaxExpandedNodes: tc.max})
		if tc.fail {
			if !errors.Is(err, scl.ErrLimitExceeded) || !strings.Contains(err.Error(), "ReportControl rcb") {
				t.Errorf("limit %d: %v, want ErrLimitExceeded", tc.max, err)
			}
			continue
		}
		if err != nil || len(m.LogicalDevices[0].LogicalNodes[0].Reports) != 99 {
			t.Errorf("limit %d: %v", tc.max, err)
		}
	}
	// Without indexed, the block is one instance.
	s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.Reports[0].Indexed = false
	if _, err := FromSCLWithOptions(s, "I", "", Options{MaxExpandedNodes: 3}); err != nil {
		t.Errorf("one instance in a budget of 3: %v", err)
	}
	if _, err := FromSCLWithOptions(s, "I", "", Options{MaxExpandedNodes: 2}); !errors.Is(err, scl.ErrLimitExceeded) {
		t.Errorf("one instance in a budget of 2: %v", err)
	}
}

// Templates built once give the models built without them; Templates of
// another document are refused. An access point is found by name, the
// first with a Server, in the first IED of that name.
func TestFromSCL_SharedTemplates(t *testing.T) {
	s := doChainSCL(2)
	ied := &s.IEDs[0]
	ied.AccessPoints = append([]scl.AccessPoint{{Name: "AP"}}, ied.AccessPoints...)
	ied.AccessPoints = append(ied.AccessPoints, scl.AccessPoint{Name: "AP", Server: &scl.Server{}})
	s.IEDs = append(s.IEDs, scl.IED{Name: "I", AccessPoints: []scl.AccessPoint{{Name: "AP", Server: &scl.Server{}}}})
	s.Communication = &scl.Communication{SubNetworks: []scl.SubNetwork{{Name: "N", ConnectedAPs: []scl.ConnectedAP{
		{IEDName: "I", APName: "AP", GSEs: []scl.GSEAddress{{LDInst: "LD0", CBName: "g"}}, SMVs: []scl.SMVAddress{{LDInst: "LD0", CBName: "v"}}},
		{IEDName: "other", APName: "AP", GSEs: []scl.GSEAddress{{LDInst: "LD0", CBName: "g"}}},
	}}}}
	tmpl := NewTemplates(s)
	for _, ap := range []string{"AP", ""} {
		want, err := FromSCL(s, "I", ap)
		if ap == "" {
			// Two AccessPoints of IED I have a Server.
			if err == nil {
				t.Fatalf("no apName: %v, want an ambiguity error", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := FromSCLWithOptions(s, "I", ap, Options{Templates: tmpl})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("with Templates: %v", err)
		}
		if len(want.LogicalDevices) != 1 {
			t.Fatalf("AP %q: %d LDs, want the first AccessPoint with a Server", ap, len(want.LogicalDevices))
		}
	}
	if _, err := FromSCLWithOptions(oneLNSCL(), "I", "AP", Options{Templates: tmpl}); err == nil || !strings.Contains(err.Error(), "another document") {
		t.Errorf("Templates of another document: %v", err)
	}
	if _, err := FromSCLWithOptions(s, "I", "missing", Options{Templates: tmpl}); err == nil {
		t.Error("unknown AccessPoint: no error")
	}
	empty := &scl.SCL{}
	if tm := NewTemplates(empty); len(tm.gse) != 0 || len(tm.ieds) != 0 {
		t.Errorf("Templates of an empty document: %+v", tm)
	}
}

// Every DOI of a DO's name applies to it, in document order.
func TestFromSCL_RepeatedDOI(t *testing.T) {
	s := doChainSCL(1)
	s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.DOIs = []scl.DOI{
		{Name: "D", DAIs: []scl.DAI{{Name: "v", Val: "false"}}},
		{Name: "other"},
		{Name: "D", DAIs: []scl.DAI{{Name: "v", Val: "true"}}},
	}
	var warnings []string
	m, err := FromSCL(s, "I", "")
	if err != nil {
		t.Fatal(err)
	}
	warnings = m.Warnings
	a := m.LogicalDevices[0].LogicalNodes[0].DataObjects[0].Attributes[0]
	if a.InitialValue != "true" || !a.Overridden {
		t.Errorf("v = %q (overridden %v), want the last DOI's value", a.InitialValue, a.Overridden)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `DOI "other"`) {
		t.Errorf("warnings %q", warnings)
	}
}
