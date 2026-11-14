// SPDX-License-Identifier: MIT

package servermodel

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// fanoutSCL returns a document with IEDs iedCount IEDs ("I0", "I1", …)
// whose LN0 expands its DO through a chain of depth DATypes, each with
// k BDAs of the next type; nodes is the number of data objects and
// attributes one IED's model expands to (2 + k + … + k^depth).
func fanoutSCL(iedCount, depth, k int) (s *scl.SCL, nodes int) {
	s = &scl.SCL{}
	for i := range iedCount {
		s.IEDs = append(s.IEDs, scl.IED{Name: fmt.Sprintf("I%d", i), AccessPoints: []scl.AccessPoint{{Name: "AP", Server: &scl.Server{LDevices: []scl.LDevice{{
			Inst: "LD0", LN0: &scl.LN{LNClass: "LLN0", LNType: "T0"},
		}}}}}})
	}
	t := &s.DataTypeTemplates
	t.LNodeTypes = []scl.LNodeType{{ID: "T0", LNClass: "LLN0", DOs: []scl.DO{{Name: "D", Type: "DOT"}}}}
	t.DOTypes = []scl.DOType{{ID: "DOT", CDC: "X", DAs: []scl.DA{{Name: "a", FC: "ST", BType: "Struct", Type: "S0"}}}}
	nodes, level := 2, 1
	for i := range depth {
		dat := scl.DAType{ID: fmt.Sprintf("S%d", i)}
		for j := range k {
			b := scl.BDA{Name: fmt.Sprintf("b%d", j), BType: "BOOLEAN"}
			if i < depth-1 {
				b.BType, b.Type = "Struct", fmt.Sprintf("S%d", i+1)
			}
			dat.BDAs = append(dat.BDAs, b)
		}
		t.DATypes = append(t.DATypes, dat)
		level *= k
		nodes += level
	}
	return s, nodes
}

// modelNodes counts the data objects and attributes of m.
func modelNodes(m *Model) int {
	var attr func(a *DataAttribute) int
	attr = func(a *DataAttribute) int {
		n := 1
		for i := range a.Children {
			n += attr(&a.Children[i])
		}
		return n
	}
	var obj func(o *DataObject) int
	obj = func(o *DataObject) int {
		n := 1
		for i := range o.Children {
			n += obj(&o.Children[i])
		}
		for i := range o.Attributes {
			n += attr(&o.Attributes[i])
		}
		return n
	}
	total := 0
	for _, ld := range m.LogicalDevices {
		for _, ln := range ld.LogicalNodes {
			for i := range ln.DataObjects {
				total += obj(&ln.DataObjects[i])
			}
		}
	}
	return total
}

// A model of MaxExpandedNodes nodes is built; one node fewer allowed
// fails with an error that wraps scl.ErrLimitExceeded.
func TestFromSCL_MaxExpandedNodesBoundary(t *testing.T) {
	s, n := fanoutSCL(1, 5, 3)
	for _, tc := range []struct {
		max  int
		fail bool
	}{
		{0, false}, {-1, false}, {n + 1, false}, {n, false}, {n - 1, true}, {1, true},
	} {
		m, err := FromSCLWithOptions(s, "I0", "", Options{MaxExpandedNodes: tc.max})
		if tc.fail {
			if !errors.Is(err, scl.ErrLimitExceeded) || m != nil {
				t.Errorf("limit %d (%d nodes): %v, want ErrLimitExceeded", tc.max, n, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("limit %d (%d nodes): %v", tc.max, n, err)
			continue
		}
		if got := modelNodes(m); got != n {
			t.Errorf("limit %d: model of %d nodes, want %d", tc.max, got, n)
		}
	}
	if _, err := FromSCL(s, "I0", ""); err != nil {
		t.Errorf("FromSCL: %v", err)
	}
}

// A Budget shared by the models of one document limits them together.
func TestFromSCL_SharedBudget(t *testing.T) {
	s, n := fanoutSCL(2, 4, 2)
	for _, tc := range []struct {
		budget     int
		secondFail bool
	}{
		{2 * n, false}, {2*n - 1, true}, {n + 1, true},
	} {
		b := NewBudget(tc.budget)
		if _, err := FromSCLWithOptions(s, "I0", "", Options{Budget: b, MaxExpandedNodes: 1}); err != nil {
			t.Fatalf("budget %d: first model: %v (MaxExpandedNodes is not used with a Budget)", tc.budget, err)
		}
		_, err := FromSCLWithOptions(s, "I1", "", Options{Budget: b})
		if got := errors.Is(err, scl.ErrLimitExceeded); got != tc.secondFail {
			t.Errorf("budget %d for 2×%d nodes: second model: %v", tc.budget, n, err)
		}
	}
	if b := NewBudget(0); b.max != scl.DefaultMaxExpandedNodes {
		t.Errorf("NewBudget(0) = %d, want the default", b.max)
	}
}

// An exponential expansion stops at the limit with an allocation
// proportional to the limit.
func TestFromSCL_ExpansionAllocation(t *testing.T) {
	s, n := fanoutSCL(1, 20, 2)
	const limit = 10_000
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	_, err := FromSCLWithOptions(s, "I0", "", Options{MaxExpandedNodes: limit})
	runtime.ReadMemStats(&b)
	if !errors.Is(err, scl.ErrLimitExceeded) {
		t.Fatalf("%d nodes, limit %d: %v", n, limit, err)
	}
	if got, budget := b.TotalAlloc-a.TotalAlloc, uint64(limit*1024); got > budget {
		t.Errorf("allocated %d bytes, budget %d", got, budget)
	}
}
