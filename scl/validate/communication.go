// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Communication checks ConnectedAP references and GSE/SMV linkage.
// Beyond verifying IED/AP/LD existence, it checks that GSE cbName
// references match an actual GSEControl and SMV cbName references
// match an actual SMVControl in the target LN0.
func Communication(s *scl.SCL, idx *index.Index) []scl.Diagnostic {
	if s.Communication == nil {
		return nil
	}

	var diags []scl.Diagnostic

	for _, sn := range s.Communication.SubNetworks {
		snPath := fmt.Sprintf("SubNetwork[%s]", sn.Name)
		for _, cap := range sn.ConnectedAPs {
			capPath := snPath + "/ConnectedAP[" + cap.IEDName + "/" + cap.APName + "]"

			ied := idx.FindIED(cap.IEDName)
			if ied == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagError, Code: "missing-connected-ap",
					Path:    capPath,
					Message: fmt.Sprintf("references IED %q which does not exist", cap.IEDName),
				})
				continue
			}

			if idx.FindAccessPoint(cap.IEDName, cap.APName) == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagError, Code: "missing-connected-ap",
					Path:    capPath,
					Message: fmt.Sprintf("references AccessPoint %q which does not exist in IED %q", cap.APName, cap.IEDName),
				})
			}

			for _, gse := range cap.GSEs {
				gsePath := capPath + "/GSE[" + gse.CBName + "]"
				if gse.LDInst == "" {
					continue
				}
				ld := idx.FindLDevice(cap.IEDName, gse.LDInst)
				if ld == nil {
					diags = append(diags, scl.Diagnostic{
						Severity: scl.DiagWarning, Code: "missing-ld",
						Path:    gsePath,
						Message: fmt.Sprintf("references LDevice %q which does not exist in IED %q", gse.LDInst, cap.IEDName),
					})
					continue
				}
				if gse.CBName != "" && ld.LN0 != nil {
					found := false
					for _, gc := range ld.LN0.GSEControls {
						if gc.Name == gse.CBName {
							found = true
							break
						}
					}
					if !found {
						diags = append(diags, scl.Diagnostic{
							Severity: scl.DiagWarning, Code: "unresolved-gse-control",
							Path: gsePath,
							Message: fmt.Sprintf("cbName %q does not match any GSEControl in IED %q LD %q LLN0",
								gse.CBName, cap.IEDName, gse.LDInst),
						})
					}
				}
			}
			for _, smv := range cap.SMVs {
				smvPath := capPath + "/SMV[" + smv.CBName + "]"
				if smv.LDInst == "" {
					continue
				}
				ld := idx.FindLDevice(cap.IEDName, smv.LDInst)
				if ld == nil {
					diags = append(diags, scl.Diagnostic{
						Severity: scl.DiagWarning, Code: "missing-ld",
						Path:    smvPath,
						Message: fmt.Sprintf("references LDevice %q which does not exist in IED %q", smv.LDInst, cap.IEDName),
					})
					continue
				}
				if smv.CBName != "" && ld.LN0 != nil {
					found := false
					for _, sv := range ld.LN0.SMVControls {
						if sv.Name == smv.CBName {
							found = true
							break
						}
					}
					if !found {
						diags = append(diags, scl.Diagnostic{
							Severity: scl.DiagWarning, Code: "unresolved-smv-control",
							Path: smvPath,
							Message: fmt.Sprintf("cbName %q does not match any SampledValueControl in IED %q LD %q LLN0",
								smv.CBName, cap.IEDName, smv.LDInst),
						})
					}
				}
			}
		}
	}

	return diags
}
