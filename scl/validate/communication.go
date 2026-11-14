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
	// The GSEControl and SampledValueControl names of each LN0, built
	// once per LDevice that a GSE or SMV names.
	type cbNames struct{ gse, smv map[string]bool }
	names := map[*scl.LDevice]cbNames{}
	ln0Names := func(ld *scl.LDevice) cbNames {
		if n, ok := names[ld]; ok {
			return n
		}
		n := cbNames{gse: map[string]bool{}, smv: map[string]bool{}}
		for _, gc := range ld.LN0.GSEControls {
			n.gse[gc.Name] = true
		}
		for _, sv := range ld.LN0.SMVControls {
			n.smv[sv.Name] = true
		}
		names[ld] = n
		return n
	}

	for _, sn := range s.Communication.SubNetworks {
		snPath := "SubNetwork[" + q(sn.Name) + "]"
		for _, cap := range sn.ConnectedAPs {
			capPath := snPath + "/ConnectedAP[" + q(cap.IEDName) + "/" + q(cap.APName) + "]"

			ied := idx.FindIED(cap.IEDName)
			if ied == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagError, Code: "missing-connected-ap",
					Path:    capPath,
					Message: fmt.Sprintf("references IED %q which does not exist", q(cap.IEDName)),
				})
				continue
			}

			if idx.FindAccessPoint(cap.IEDName, cap.APName) == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagError, Code: "missing-connected-ap",
					Path:    capPath,
					Message: fmt.Sprintf("references AccessPoint %q which does not exist in IED %q", q(cap.APName), q(cap.IEDName)),
				})
			}

			for _, gse := range cap.GSEs {
				if gse.LDInst == "" {
					continue
				}
				gsePath := capPath + "/GSE[" + q(gse.CBName) + "]"
				ld := idx.FindLDevice(cap.IEDName, gse.LDInst)
				if ld == nil {
					diags = append(diags, scl.Diagnostic{
						Severity: scl.DiagWarning, Code: "missing-ld",
						Path:    gsePath,
						Message: fmt.Sprintf("references LDevice %q which does not exist in IED %q", q(gse.LDInst), q(cap.IEDName)),
					})
					continue
				}
				if gse.CBName != "" && ld.LN0 != nil && !ln0Names(ld).gse[gse.CBName] {
					diags = append(diags, scl.Diagnostic{
						Severity: scl.DiagWarning, Code: "unresolved-gse-control",
						Path: gsePath,
						Message: fmt.Sprintf("cbName %q does not match any GSEControl in IED %q LD %q LLN0",
							q(gse.CBName), q(cap.IEDName), q(gse.LDInst)),
					})
				}
			}
			for _, smv := range cap.SMVs {
				if smv.LDInst == "" {
					continue
				}
				smvPath := capPath + "/SMV[" + q(smv.CBName) + "]"
				ld := idx.FindLDevice(cap.IEDName, smv.LDInst)
				if ld == nil {
					diags = append(diags, scl.Diagnostic{
						Severity: scl.DiagWarning, Code: "missing-ld",
						Path:    smvPath,
						Message: fmt.Sprintf("references LDevice %q which does not exist in IED %q", q(smv.LDInst), q(cap.IEDName)),
					})
					continue
				}
				if smv.CBName != "" && ld.LN0 != nil && !ln0Names(ld).smv[smv.CBName] {
					diags = append(diags, scl.Diagnostic{
						Severity: scl.DiagWarning, Code: "unresolved-smv-control",
						Path: smvPath,
						Message: fmt.Sprintf("cbName %q does not match any SampledValueControl in IED %q LD %q LLN0",
							q(smv.CBName), q(cap.IEDName), q(smv.LDInst)),
					})
				}
			}
		}
	}

	return diags
}
