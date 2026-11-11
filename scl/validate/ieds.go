// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// IEDs checks IED-level structure: missing LN type references and LN
// types of another lnClass.
// Duplicate IED/AP/LD/LN checks are already handled by [index.Build].
func IEDs(s *scl.SCL, idx *index.Index) []scl.Diagnostic {
	var diags []scl.Diagnostic

	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				ldPath := fmt.Sprintf("IED[%s]/LD[%s]", ied.Name, ld.Inst)
				if ld.LN0 != nil {
					diags = append(diags, checkLN(idx, ldPath, ld.LN0)...)
				}
				for i := range ld.LNs {
					diags = append(diags, checkLN(idx, ldPath, &ld.LNs[i])...)
				}
			}
		}
	}

	return diags
}

func checkLN(idx *index.Index, ldPath string, ln *scl.LN) []scl.Diagnostic {
	var diags []scl.Diagnostic
	lnPath := fmt.Sprintf("%s/LN[%s%s%s]", ldPath, ln.Prefix, ln.LNClass, ln.Inst)

	lnt := idx.FindLNodeType(ln.LNType)
	if lnt == nil {
		diags = append(diags, scl.Diagnostic{
			Severity: scl.DiagError, Code: "missing-lnodetype",
			Path:    lnPath,
			Message: fmt.Sprintf("references LNodeType %q which does not exist", ln.LNType),
		})
	} else if lnt.LNClass != ln.LNClass {
		// SCL.xsd LNodeTypeKey is (@id, @lnClass); the keyrefs
		// ref2LNodeTypeDomain1/2 and ref2LNodeTypeLLN0 require the
		// LN's (lnType, lnClass) pair to exist.
		diags = append(diags, lnClassMismatch(lnPath, ln, lnt.LNClass))
	}
	return diags
}

func lnClassMismatch(lnPath string, ln *scl.LN, typeClass string) scl.Diagnostic {
	return scl.Diagnostic{
		Severity: scl.DiagError, Code: "lnclass-mismatch", Path: lnPath,
		Message: fmt.Sprintf("lnClass %q references LNodeType %q of lnClass %q (SCL key LNodeTypeKey)", ln.LNClass, ln.LNType, typeClass),
	}
}
