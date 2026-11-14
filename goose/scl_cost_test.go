// SPDX-License-Identifier: MIT

package goose

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// accessPointsSCL returns a document of n IEDs with one AccessPoint
// each, and one IED with n AccessPoints, all with an empty Server.
func accessPointsSCL(n int) *scl.SCL {
	s := &scl.SCL{}
	many := scl.IED{Name: "many"}
	for i := range n {
		s.IEDs = append(s.IEDs, scl.IED{Name: fmt.Sprintf("I%d", i), AccessPoints: []scl.AccessPoint{{Name: "AP", Server: &scl.Server{}}}})
		many.AccessPoints = append(many.AccessPoints, scl.AccessPoint{Name: fmt.Sprintf("AP%d", i), Server: &scl.Server{}})
	}
	s.IEDs = append(s.IEDs, many)
	return s
}

// ControlBlocksFromSCL builds the lookups of the document once, not once
// per access point: four times the access points cost about four times
// the bytes allocated (sixteen times when each access point indexes the
// whole document).
func TestControlBlocksFromSCL_CostLinearInAccessPoints(t *testing.T) {
	allocated := func(n int) float64 {
		doc := accessPointsSCL(n)
		var a, b runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&a)
		cbs, err := ControlBlocksFromSCL(doc)
		runtime.ReadMemStats(&b)
		if err != nil || len(cbs) != 0 {
			t.Fatalf("%d blocks, %v", len(cbs), err)
		}
		return float64(b.TotalAlloc - a.TotalAlloc)
	}
	small, large := allocated(500), allocated(2000)
	if ratio := large / small; ratio > 8 {
		t.Errorf("%.0f bytes allocated for 500 access points, %.0f for 2000: ratio %.1f, want about 4", small, large, ratio)
	}
}
