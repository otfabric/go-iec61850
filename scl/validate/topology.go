// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Topology checks substation LNode references resolve to real
// IED/LDevice/LN combinations via the shared index.
func Topology(s *scl.SCL, idx *index.Index) []scl.Diagnostic {
	var diags []scl.Diagnostic

	for _, sub := range s.Substations {
		subPath := fmt.Sprintf("Substation[%s]", sub.Name)
		diags = append(diags, checkLNodes(idx, subPath, sub.LNodes)...)

		for _, vl := range sub.VoltageLevels {
			vlPath := subPath + "/VoltageLevel[" + vl.Name + "]"
			diags = append(diags, checkLNodes(idx, vlPath, vl.LNodes)...)

			for _, bay := range vl.Bays {
				bayPath := vlPath + "/Bay[" + bay.Name + "]"
				diags = append(diags, checkLNodes(idx, bayPath, bay.LNodes)...)
			}
		}
	}

	return diags
}

func checkLNodes(idx *index.Index, parentPath string, lnodes []scl.LNode) []scl.Diagnostic {
	var diags []scl.Diagnostic
	for _, ln := range lnodes {
		if ln.IEDName == "" || ln.IEDName == "None" {
			continue
		}
		lnPath := fmt.Sprintf("%s/LNode[%s/%s/%s%s%s]",
			parentPath, ln.IEDName, ln.LDInst, ln.Prefix, ln.LNClass, ln.LNInst)

		if idx.FindIED(ln.IEDName) == nil {
			diags = append(diags, scl.Diagnostic{
				Severity: scl.DiagError, Code: "unresolved-topology-lnode",
				Path:    lnPath,
				Message: fmt.Sprintf("references IED %q which does not exist", ln.IEDName),
			})
			continue
		}
		if ln.LDInst != "" && idx.FindLDevice(ln.IEDName, ln.LDInst) == nil {
			diags = append(diags, scl.Diagnostic{
				Severity: scl.DiagError, Code: "unresolved-topology-lnode",
				Path:    lnPath,
				Message: fmt.Sprintf("references LDevice %q which does not exist in IED %q", ln.LDInst, ln.IEDName),
			})
			continue
		}
		if ln.LDInst != "" && ln.LNClass != "" {
			target := idx.FindLN(ln.IEDName, ln.LDInst, ln.Prefix, ln.LNClass, ln.LNInst)
			if target == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagWarning, Code: "unresolved-topology-lnode",
					Path: lnPath,
					Message: fmt.Sprintf("LN %s%s%s not found in IED %q LD %q",
						ln.Prefix, ln.LNClass, ln.LNInst, ln.IEDName, ln.LDInst),
				})
			}
		}
	}
	return diags
}
