// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Controls checks that report control blocks reference datasets
// that exist within their owning logical node.
func Controls(s *scl.SCL, idx *index.Index) []scl.Diagnostic {
	var diags []scl.Diagnostic

	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				ldPath := "IED[" + q(ied.Name) + "]/LD[" + q(ld.Inst) + "]"
				checkRC := func(ln *scl.LN) {
					lnPath := ldPath + "/LN[" + q(ln.Prefix+ln.LNClass+ln.Inst) + "]"

					dsNames := make(map[string]bool, len(ln.DataSets))
					for _, ds := range ln.DataSets {
						dsNames[ds.Name] = true
					}

					for _, rc := range ln.Reports {
						if rc.DatSet == "" {
							continue
						}
						if !dsNames[rc.DatSet] {
							diags = append(diags, scl.Diagnostic{
								Severity: scl.DiagError, Code: "missing-dataset",
								Path:    lnPath + "/RC[" + q(rc.Name) + "]",
								Message: fmt.Sprintf("references datSet %q which is not defined in this LN", q(rc.DatSet)),
							})
						}
					}
					for _, gc := range ln.GSEControls {
						if gc.DatSet == "" {
							continue
						}
						if !dsNames[gc.DatSet] {
							diags = append(diags, scl.Diagnostic{
								Severity: scl.DiagError, Code: "missing-dataset",
								Path:    lnPath + "/GC[" + q(gc.Name) + "]",
								Message: fmt.Sprintf("references datSet %q which is not defined in this LN", q(gc.DatSet)),
							})
						}
					}
					for _, sv := range ln.SMVControls {
						// smpRate and nofASDU are required (6 Ed2 Table
						// 30, tSampledValueControl); 0 is no usable value.
						if sv.SmpRate == 0 || sv.NofASDU == 0 {
							diags = append(diags, scl.Diagnostic{
								Severity: scl.DiagError, Code: "invalid-attribute",
								Path:    lnPath + "/SV[" + q(sv.Name) + "]",
								Message: fmt.Sprintf("smpRate %d / nofASDU %d: both must be at least 1", sv.SmpRate, sv.NofASDU),
							})
						}
						if sv.DatSet == "" {
							continue
						}
						if !dsNames[sv.DatSet] {
							diags = append(diags, scl.Diagnostic{
								Severity: scl.DiagError, Code: "missing-dataset",
								Path:    lnPath + "/SV[" + q(sv.Name) + "]",
								Message: fmt.Sprintf("references datSet %q which is not defined in this LN", q(sv.DatSet)),
							})
						}
					}
				}
				if ld.LN0 != nil {
					checkRC(ld.LN0)
				}
				for i := range ld.LNs {
					checkRC(&ld.LNs[i])
				}
			}
		}
	}

	return diags
}
