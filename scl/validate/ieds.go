package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// IEDs checks IED-level structure: missing LN type references.
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

	if idx.FindLNodeType(ln.LNType) == nil {
		diags = append(diags, scl.Diagnostic{
			Severity: scl.DiagError, Code: "missing-lnodetype",
			Path:    lnPath,
			Message: fmt.Sprintf("references LNodeType %q which does not exist", ln.LNType),
		})
	}
	return diags
}
