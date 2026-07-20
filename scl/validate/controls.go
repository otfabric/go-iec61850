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
				checkRC := func(ln *scl.LN) {
					lnPath := fmt.Sprintf("IED[%s]/LD[%s]/LN[%s%s%s]", ied.Name, ld.Inst, ln.Prefix, ln.LNClass, ln.Inst)

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
								Path:    lnPath + "/RC[" + rc.Name + "]",
								Message: fmt.Sprintf("references datSet %q which is not defined in this LN", rc.DatSet),
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
								Path:    lnPath + "/GC[" + gc.Name + "]",
								Message: fmt.Sprintf("references datSet %q which is not defined in this LN", gc.DatSet),
							})
						}
					}
					for _, sv := range ln.SMVControls {
						if sv.DatSet == "" {
							continue
						}
						if !dsNames[sv.DatSet] {
							diags = append(diags, scl.Diagnostic{
								Severity: scl.DiagError, Code: "missing-dataset",
								Path:    lnPath + "/SV[" + sv.Name + "]",
								Message: fmt.Sprintf("references datSet %q which is not defined in this LN", sv.DatSet),
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
