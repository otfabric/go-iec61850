// SPDX-License-Identifier: MIT

package scl

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
)

// flatChainSCL returns a document whose LN0 DO has a chain of n DOTypes
// (each with an SDO named sdo of the next; the last with one DA), or,
// with da, a Struct DA with a chain of n DATypes.
func flatChainSCL(n int, da bool, sdo string) *SCL {
	s := &SCL{IEDs: []IED{{Name: "I", AccessPoints: []AccessPoint{{Name: "AP", Server: &Server{LDevices: []LDevice{{
		Inst: "LD0", LN0: &LN{LNClass: "LLN0", LNType: "T0"},
	}}}}}}}}
	t := &s.DataTypeTemplates
	t.LNodeTypes = []LNodeType{{ID: "T0", LNClass: "LLN0", DOs: []DO{{Name: "D", Type: "D0"}}}}
	if da {
		t.DOTypes = []DOType{{ID: "D0", CDC: "X", DAs: []DA{{Name: "a", FC: "ST", BType: "Struct", Type: "S0"}}}}
		for i := range n {
			b := BDA{Name: "v", BType: "BOOLEAN"}
			if i < n-1 {
				b = BDA{Name: "b", BType: "Struct", Type: fmt.Sprintf("S%d", i+1)}
			}
			t.DATypes = append(t.DATypes, DAType{ID: fmt.Sprintf("S%d", i), BDAs: []BDA{b}})
		}
		return s
	}
	for i := range n {
		dot := DOType{ID: fmt.Sprintf("D%d", i), CDC: "X"}
		if i < n-1 {
			dot.SDOs = []SDO{{Name: sdo, Type: fmt.Sprintf("D%d", i+1)}}
		} else {
			dot.DAs = []DA{{Name: "v", FC: "ST", BType: "BOOLEAN"}}
		}
		t.DOTypes = append(t.DOTypes, dot)
	}
	return s
}

// checkDepthStop checks that the expansion stopped at the type depth
// limit max.
func checkDepthStop(t *testing.T, name string, rows []FlatRow, diags []Diagnostic, max int) {
	t.Helper()
	if len(diags) != 1 || diags[0].Code != "expansion-limit" ||
		!strings.Contains(diags[0].Message, ErrLimitExceeded.Error()) ||
		!strings.Contains(diags[0].Message, fmt.Sprintf("deeper than %d levels (FlattenOptions.MaxTypeDepth)", max)) {
		t.Errorf("%s: diagnostics %v", name, diags)
	}
	if len(rows) == 0 || rows[len(rows)-1].Status != "expansion-limit" {
		t.Errorf("%s: %d rows", name, len(rows))
	}
}

// A chain of DOTypes or DATypes deeper than MaxTypeDepth stops with an
// "expansion-limit" row and diagnostic, without recursing along the
// whole chain; up to the limit it is expanded.
func TestFlatten_TypeChainDepth(t *testing.T) {
	old := debug.SetMaxStack(1 << 20)
	defer debug.SetMaxStack(old)
	for _, da := range []bool{false, true} {
		rows, diags := FlattenWithOptions(flatChainSCL(50_000, da, "s"), FlattenOptions{})
		checkDepthStop(t, fmt.Sprintf("chain of 50000 (DA %v)", da), rows, diags, DefaultMaxTypeDepth)
		if got := Flatten(flatChainSCL(50_000, da, "s")); got[len(got)-1].Status != "expansion-limit" {
			t.Errorf("Flatten, chain of 50000 (DA %v): last row %+v", da, got[len(got)-1])
		}
	}
	debug.SetMaxStack(old)

	max := DefaultMaxTypeDepth
	for _, tc := range []struct {
		name  string
		s     *SCL
		depth int
		stop  bool
	}{
		{"DO at the limit", flatChainSCL(max, false, "s"), 0, false},
		{"DO beyond the limit", flatChainSCL(max+1, false, "s"), 0, true},
		{"DA at the limit", flatChainSCL(max-1, true, ""), 0, false},
		{"DA beyond the limit", flatChainSCL(max, true, ""), 0, true},
		{"DA beyond a lower limit", flatChainSCL(2, true, ""), 2, true},
		{"DO at a lower limit", flatChainSCL(5, false, "s"), 5, false},
		{"DO beyond a lower limit", flatChainSCL(6, false, "s"), 5, true},
	} {
		rows, diags := FlattenWithOptions(tc.s, FlattenOptions{MaxTypeDepth: tc.depth})
		if !tc.stop {
			if len(diags) != 0 || len(rows) != 1 || rows[0].Status != "" {
				t.Errorf("%s: %d rows, %v", tc.name, len(rows), diags)
			}
			continue
		}
		limit := tc.depth
		if limit == 0 {
			limit = max
		}
		checkDepthStop(t, tc.name, rows, diags, limit)
	}
	// A Struct DA directly below the limit: the stop is at the DA.
	rows, diags := FlattenWithOptions(flatChainSCL(1, true, ""), FlattenOptions{MaxTypeDepth: 1})
	checkDepthStop(t, "Struct DA at depth 1", rows, diags, 1)
	if rows[len(rows)-1].Path != "D.a" {
		t.Errorf("stop row %+v, want D.a", rows[len(rows)-1])
	}
}

// The path of a row counts against MaxExpandedNodes, one node per 256
// bytes: a chain of SDOs with long names stops with the expansion-limit
// row and diagnostic, and the rows hold at most about 256 bytes of path
// per node of the limit.
func TestFlatten_PathBytesCount(t *testing.T) {
	long := strings.Repeat("n", 4096)
	const limit = 500
	rows, diags := FlattenWithOptions(flatChainSCL(60, false, long), FlattenOptions{MaxExpandedNodes: limit})
	if len(diags) != 1 || diags[0].Code != "expansion-limit" || !strings.Contains(diags[0].Message, "MaxExpandedNodes") {
		t.Fatalf("diagnostics %v", diags)
	}
	if rows[len(rows)-1].Status != "expansion-limit" {
		t.Fatalf("last row %+v", rows[len(rows)-1])
	}
	total := 0
	for _, r := range rows {
		total += len(r.Path)
	}
	if total > 256*(limit+len(rows)) {
		t.Errorf("%d rows hold %d bytes of path, limit %d nodes", len(rows), total, limit)
	}
	// Short paths count one node each, as before.
	rows, diags = FlattenWithOptions(flatChainSCL(3, false, "s"), FlattenOptions{MaxExpandedNodes: 4})
	if len(diags) != 0 || len(rows) != 1 {
		t.Errorf("3 DOs and a DA in a limit of 4: %d rows, %v", len(rows), diags)
	}
}
