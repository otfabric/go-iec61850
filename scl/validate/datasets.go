// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Datasets checks dataset integrity (6 Ed2 9.3.10 FCDA): each FCDA
// must address existing data with its functional constraint. The
// logical device, the logical node, the data object (with "."-separated
// sub data objects), the data attribute (with "."-separated BDAs) and
// the FC are resolved in turn; the first step that fails is reported as
// a warning "unresolved-fcda" (FC mismatch: "fcda-fc-mismatch"), the
// severity this check has always used, since vendor files often carry
// such dangling members. Steps whose type cannot be resolved (missing
// LNodeType/DOType/DAType, reported by other passes) end the check
// silently.
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
							path := fmt.Sprintf("%s/FCDA[%d]", dsPath, fi)
							targetLD := idx.FindLDevice(ied.Name, fcda.LDInst)
							if targetLD == nil {
								diags = append(diags, scl.Diagnostic{
									Severity: scl.DiagWarning, Code: "unresolved-fcda",
									Path:    path,
									Message: fmt.Sprintf("LDevice %q not found in IED %q", fcda.LDInst, ied.Name),
								})
								continue
							}
							if d, bad := resolveFCDA(idx, ied.Name, &fcda); bad {
								d.Path = path
								diags = append(diags, d)
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

// resolveFCDA resolves an FCDA below its logical device (see
// [Datasets]); it returns a diagnostic and true when a step fails.
func resolveFCDA(idx *index.Index, iedName string, f *scl.FCDA) (scl.Diagnostic, bool) {
	unresolved := func(format string, args ...any) (scl.Diagnostic, bool) {
		return scl.Diagnostic{Severity: scl.DiagWarning, Code: "unresolved-fcda", Message: fmt.Sprintf(format, args...)}, true
	}
	ln := idx.FindLN(iedName, f.LDInst, f.Prefix, f.LNClass, f.LNInst)
	if ln == nil {
		return unresolved("LN %s%s%s not found in LDevice %q", f.Prefix, f.LNClass, f.LNInst, f.LDInst)
	}
	if f.DOName == "" {
		return scl.Diagnostic{}, false
	}
	lnt := idx.FindLNodeType(ln.LNType)
	if lnt == nil {
		return scl.Diagnostic{}, false
	}
	// doName is the DO with its SDOs; some tools put SDOs (for example
	// an array of CMV such as phsAHar(n)) in daName instead, so the walk
	// continues through SDOs until the first DA.
	path := strings.Split(f.DOName, ".")
	nDO := len(path)
	if f.DAName != "" {
		path = append(path, strings.Split(f.DAName, ".")...)
	}
	var doType string
	for _, do := range lnt.DOs {
		if do.Name == stripIndex(path[0]) {
			doType = do.Type
		}
	}
	if doType == "" {
		return unresolved("data object %q not in LNodeType %q", path[0], lnt.ID)
	}
	dot := idx.FindDOType(doType)
	i := 1
	for ; i < len(path); i++ {
		if dot == nil {
			return scl.Diagnostic{}, false
		}
		name := stripIndex(path[i])
		var da *scl.DA
		for k := range dot.DAs {
			if dot.DAs[k].Name == name {
				da = &dot.DAs[k]
			}
		}
		if da != nil && i >= nDO {
			if f.FC != "" && !fcMatches(da.FC, f.FC) {
				return scl.Diagnostic{Severity: scl.DiagWarning, Code: "fcda-fc-mismatch",
					Message: fmt.Sprintf("data attribute %q has FC %s, FCDA says %s", path[i], da.FC, f.FC)}, true
			}
			return resolveBDAs(idx, da.Type, path[i+1:])
		}
		next := ""
		for _, x := range dot.SDOs {
			if x.Name == name {
				next = x.Type
			}
		}
		if next == "" {
			what := "sub data object"
			if i >= nDO {
				what = "data attribute"
			}
			return unresolved("%s %q not in DOType %q", what, path[i], dot.ID)
		}
		dot = idx.FindDOType(next)
	}
	// The FCDA selects every attribute of the (S)DO with fc.
	if dot != nil && f.FC != "" && !doHasFC(idx, dot, f.FC, 0) {
		return scl.Diagnostic{Severity: scl.DiagWarning, Code: "fcda-fc-mismatch",
			Message: fmt.Sprintf("data object %q has no attribute with FC %s", strings.Join(path, "."), f.FC)}, true
	}
	return scl.Diagnostic{}, false
}

// resolveBDAs walks the BDA names below a DA of DAType typ.
func resolveBDAs(idx *index.Index, typ string, names []string) (scl.Diagnostic, bool) {
	for _, name := range names {
		dat := idx.FindDAType(typ)
		if dat == nil {
			return scl.Diagnostic{}, false
		}
		typ = ""
		found := false
		for _, b := range dat.BDAs {
			if b.Name == stripIndex(name) {
				typ, found = b.Type, true
			}
		}
		if !found {
			return scl.Diagnostic{Severity: scl.DiagWarning, Code: "unresolved-fcda",
				Message: fmt.Sprintf("attribute %q not in DAType %q", name, dat.ID)}, true
		}
	}
	return scl.Diagnostic{}, false
}

// fcMatches reports whether a DA with FC daFC is addressed by an FCDA
// with fc: equal, or SG/SE for each other (7-2 Table 2: SE is the
// editable copy of the setting group values of FC SG, so a setting
// attribute is reachable under both).
func fcMatches(daFC, fc string) bool {
	sg := func(x string) bool { return x == "SG" || x == "SE" }
	return daFC == fc || (sg(daFC) && sg(fc))
}

// doHasFC reports whether dot (or one of its SDOs) has a DA with FC fc.
func doHasFC(idx *index.Index, dot *scl.DOType, fc string, depth int) bool {
	if depth > 16 {
		return true // cycles are reported elsewhere; do not guess
	}
	for _, da := range dot.DAs {
		if fcMatches(da.FC, fc) {
			return true
		}
	}
	for _, sdo := range dot.SDOs {
		if t := idx.FindDOType(sdo.Type); t == nil || doHasFC(idx, t, fc, depth+1) {
			return true // an unresolved SDO type may hold it
		}
	}
	return false
}

// stripIndex removes an array index "(n)" from an FCDA name element.
func stripIndex(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 {
		return s[:i]
	}
	return s
}
