// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Datasets checks dataset integrity: FCDA references resolve to
// real logical nodes via the index.
func Datasets(s *scl.SCL, idx *index.Index) []scl.Diagnostic {
	var diags []scl.Diagnostic

	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				checkDS := func(ln *scl.LN) {
					lnPath := fmt.Sprintf("IED[%s]/LD[%s]/LN[%s%s%s]", ied.Name, ld.Inst, ln.Prefix, ln.LNClass, ln.Inst)
					for _, ds := range ln.DataSets {
						dsPath := lnPath + "/DS[" + ds.Name + "]"
						for fi, fcda := range ds.FCDAs {
							if fcda.LDInst == "" || fcda.LNClass == "" {
								continue
							}
							targetLD := idx.FindLDevice(ied.Name, fcda.LDInst)
							if targetLD == nil {
								diags = append(diags, scl.Diagnostic{
									Severity: scl.DiagWarning, Code: "unresolved-fcda",
									Path:    fmt.Sprintf("%s/FCDA[%d]", dsPath, fi),
									Message: fmt.Sprintf("LDevice %q not found in IED %q", fcda.LDInst, ied.Name),
								})
							}
						}
					}
				}
				if ld.LN0 != nil {
					checkDS(ld.LN0)
				}
				for i := range ld.LNs {
					checkDS(&ld.LNs[i])
				}
			}
		}
	}

	return diags
}
