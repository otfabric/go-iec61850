// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// fanOutTypes builds DOTypes T0..Td where each Ti below Td has fanOut SDOs
// of type Ti+1 and Td has one DA with FC ST.
func fanOutTypes(fanOut, d int) (*index.Index, *scl.DOType) {
	idx := &index.Index{DOTypes: make(map[string]*scl.DOType)}
	for i := range d + 1 {
		dot := &scl.DOType{ID: fmt.Sprintf("T%d", i)}
		if i == d {
			dot.DAs = []scl.DA{{Name: "stVal", FC: "ST"}}
		}
		for k := range fanOut {
			if i < d {
				dot.SDOs = append(dot.SDOs, scl.SDO{Name: fmt.Sprintf("s%d", k), Type: fmt.Sprintf("T%d", i+1)})
			}
		}
		idx.DOTypes[dot.ID] = dot
	}
	return idx, idx.DOTypes["T0"]
}

// TestDatasets_FCWalkIsLinear: the FC check of an FCDA walks each DOType
// at most once per FC and SDO level, however many SDO paths lead to it:
// the number of steps stays within (maxSDODepth+1) times the SDO count of
// the type graph, here fan-out 4 at every level down to maxSDODepth.
func TestDatasets_FCWalkIsLinear(t *testing.T) {
	const fanOut = 4
	for d := 1; d <= maxSDODepth; d++ {
		idx, root := fanOutTypes(fanOut, d)
		budget := 1 + (maxSDODepth+1)*fanOut*d
		m := newFCIndex(idx)
		if m.doHasFC(root, "MX") {
			t.Fatalf("depth %d: FC MX found, the types have only ST", d)
		}
		if m.steps > budget {
			t.Fatalf("depth %d, fan-out %d: %d steps, budget %d", d, fanOut, m.steps, budget)
		}
		if !newFCIndex(idx).doHasFC(root, "ST") {
			t.Fatalf("depth %d: FC ST not found", d)
		}
	}
}

// TestDatasets_FCWalkDepthLimit: SDO levels below maxSDODepth count as
// holding the FC, as before; up to that level the walk answers exactly.
func TestDatasets_FCWalkDepthLimit(t *testing.T) {
	for _, tc := range []struct {
		depth int
		want  bool
	}{{maxSDODepth, false}, {maxSDODepth + 1, true}} {
		idx, root := fanOutTypes(1, tc.depth)
		if got := newFCIndex(idx).doHasFC(root, "MX"); got != tc.want {
			t.Errorf("chain of %d SDO levels: doHasFC(MX) = %v, want %v", tc.depth, got, tc.want)
		}
	}
}

// refDoHasFC is the FC walk without a memo: the reference the memoised
// walk is compared with.
func refDoHasFC(idx *index.Index, dot *scl.DOType, fc string, depth int) bool {
	if depth > maxSDODepth {
		return true
	}
	for _, da := range dot.DAs {
		if fcMatches(da.FC, fc) {
			return true
		}
	}
	for _, sdo := range dot.SDOs {
		if t := idx.FindDOType(sdo.Type); t == nil || refDoHasFC(idx, t, fc, depth+1) {
			return true
		}
	}
	return false
}

// TestDatasets_FCWalkSameResults: on random type graphs with cycles,
// undefined SDO types and chains deeper than maxSDODepth, the memoised
// walk gives the answers of the walk without a memo, for every DOType
// and FC, with one memo shared by all questions as in one Datasets call.
func TestDatasets_FCWalkSameResults(t *testing.T) {
	fcs := []string{"ST", "MX", "SG", "SE", "CF"}
	rng := rand.New(rand.NewPCG(61850, 7))
	check := func(name string, idx *index.Index) {
		m := newFCIndex(idx)
		for id, dot := range idx.DOTypes {
			for _, fc := range fcs {
				if got, want := m.doHasFC(dot, fc), refDoHasFC(idx, dot, fc, 0); got != want {
					t.Fatalf("%s: DOType %s, FC %s: %v, want %v", name, id, fc, got, want)
				}
			}
		}
	}
	for g := range 300 {
		n := 1 + rng.IntN(6)
		idx := &index.Index{DOTypes: make(map[string]*scl.DOType)}
		for i := range n {
			dot := &scl.DOType{ID: fmt.Sprintf("T%d", i)}
			for range rng.IntN(2) {
				dot.DAs = append(dot.DAs, scl.DA{Name: "a", FC: fcs[rng.IntN(len(fcs))]})
			}
			for k := range rng.IntN(3) {
				typ := fmt.Sprintf("T%d", rng.IntN(n+1)) // T<n> is not defined
				dot.SDOs = append(dot.SDOs, scl.SDO{Name: fmt.Sprintf("s%d", k), Type: typ})
			}
			idx.DOTypes[dot.ID] = dot
		}
		check(fmt.Sprintf("graph %d", g), idx)
	}
	for d := maxSDODepth - 2; d <= maxSDODepth+3; d++ {
		idx, _ := fanOutTypes(2, d)
		check(fmt.Sprintf("fan-out 2, depth %d", d), idx)
	}
}
