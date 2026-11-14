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
	fcs := newFCIndex(idx)

	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				ldPath := "IED[" + q(ied.Name) + "]/LD[" + q(ld.Inst) + "]"
				checkDS := func(ln *scl.LN) {
					if len(ln.DataSets) == 0 {
						return
					}
					lnPath := ldPath + "/LN[" + q(ln.Prefix+ln.LNClass+ln.Inst) + "]"
					for _, ds := range ln.DataSets {
						dsPath := lnPath + "/DS[" + q(ds.Name) + "]"
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
									Message: fmt.Sprintf("LDevice %q not found in IED %q", q(fcda.LDInst), q(ied.Name)),
								})
								continue
							}
							if d, bad := resolveFCDA(fcs, ied.Name, &fcda); bad {
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
func resolveFCDA(fcs *fcIndex, iedName string, f *scl.FCDA) (scl.Diagnostic, bool) {
	idx := fcs.idx
	unresolved := func(format string, args ...any) (scl.Diagnostic, bool) {
		return scl.Diagnostic{Severity: scl.DiagWarning, Code: "unresolved-fcda", Message: fmt.Sprintf(format, args...)}, true
	}
	ln := idx.FindLN(iedName, f.LDInst, f.Prefix, f.LNClass, f.LNInst)
	if ln == nil {
		return unresolved("LN %s not found in LDevice %q", q(f.Prefix+f.LNClass+f.LNInst), q(f.LDInst))
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
		return unresolved("data object %q not in LNodeType %q", q(path[0]), q(lnt.ID))
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
					Message: fmt.Sprintf("data attribute %q has FC %s, FCDA says %s", q(path[i]), q(da.FC), q(f.FC))}, true
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
			return unresolved("%s %q not in DOType %q", what, q(path[i]), q(dot.ID))
		}
		dot = idx.FindDOType(next)
	}
	// The FCDA selects every attribute of the (S)DO with fc.
	if dot != nil && f.FC != "" && !fcs.doHasFC(dot, f.FC) {
		return scl.Diagnostic{Severity: scl.DiagWarning, Code: "fcda-fc-mismatch",
			Message: fmt.Sprintf("data object %q has no attribute with FC %s", q(strings.Join(path, ".")), q(f.FC))}, true
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
				Message: fmt.Sprintf("attribute %q not in DAType %q", q(name), q(dat.ID))}, true
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

// maxSDODepth is the number of SDO levels below a data object that
// doHasFC follows; deeper levels (and so cycles of SDO types, which other
// passes report) count as holding the FC.
const maxSDODepth = 16

// fcIndex answers doHasFC for one Datasets call. It computes the set of
// FCs below each DOType and SDO level once, whatever FC is asked and
// however many SDO paths lead to it: the cost is linear in the size of
// the type graph, and does not grow with the number of FCs the FCDAs of
// the document name.
type fcIndex struct {
	idx   *index.Index
	memo  map[fcKey]*fcSet
	steps int // fcSetOf calls, for tests
}

// fcKey is an fcIndex entry: DOType dot, depth SDO levels below the data
// object of the FCDA.
type fcKey struct {
	dot   *scl.DOType
	depth int
}

// fcWords is the size of an fcSet: one bit per FC name of one or two
// upper-case letters (26 + 26*26 names).
const fcWords = (26 + 26*26 + 63) / 64

// fcSet is a set of FCs of the SCL tFCEnum: the FCs of IEC 61850-7-2
// Table 2 and the extension FCs of SCL 2007C5 (tExtensionFCEnum, one or
// two upper-case letters). any marks a set that holds every FC (an SDO
// of an undefined type, or levels below maxSDODepth).
type fcSet struct {
	bits [fcWords]uint64
	any  bool
}

// fcBit returns the bit of fc in an fcSet, or false when fc is not a
// name of one or two upper-case letters.
func fcBit(fc string) (int, bool) {
	up := func(c byte) bool { return c >= 'A' && c <= 'Z' }
	switch {
	case len(fc) == 1 && up(fc[0]):
		return int(fc[0] - 'A'), true
	case len(fc) == 2 && up(fc[0]) && up(fc[1]):
		return 26 + int(fc[0]-'A')*26 + int(fc[1]-'A'), true
	}
	return 0, false
}

func (s *fcSet) add(fc string) {
	if b, ok := fcBit(fc); ok {
		s.bits[b/64] |= 1 << (b % 64)
	}
}

func (s *fcSet) has(fc string) bool {
	if s.any {
		return true
	}
	b, ok := fcBit(fc)
	return ok && s.bits[b/64]&(1<<(b%64)) != 0
}

func newFCIndex(idx *index.Index) *fcIndex {
	return &fcIndex{idx: idx, memo: make(map[fcKey]*fcSet)}
}

// doHasFC reports whether dot (or one of its SDOs) has a DA with FC fc
// (as fcMatches compares them). An SDO of a type the document does not
// define may hold one, and levels below maxSDODepth are not followed:
// both count as found. An fc that is no FC name of the SCL schema is
// found only there: no DA of the schema has it.
func (m *fcIndex) doHasFC(dot *scl.DOType, fc string) bool {
	s := m.fcSetOf(dot, 0)
	if s.has(fc) {
		return true
	}
	return (fc == "SG" || fc == "SE") && (s.has("SG") || s.has("SE"))
}

// allFCs is the set of the levels below maxSDODepth.
var allFCs = &fcSet{any: true}

// fcSetOf returns the FCs of the DAs of dot and of the DOTypes below its
// SDOs.
func (m *fcIndex) fcSetOf(dot *scl.DOType, depth int) *fcSet {
	m.steps++
	if depth > maxSDODepth {
		return allFCs // cycles are reported elsewhere; do not guess
	}
	k := fcKey{dot, depth}
	if s, ok := m.memo[k]; ok {
		return s
	}
	s := &fcSet{}
	for _, da := range dot.DAs {
		s.add(da.FC)
	}
	for _, sdo := range dot.SDOs {
		if s.any {
			break
		}
		t := m.idx.FindDOType(sdo.Type)
		if t == nil {
			s.any = true // an unresolved SDO type may hold any FC
			break
		}
		sub := m.fcSetOf(t, depth+1)
		s.any = sub.any
		for w := range s.bits {
			s.bits[w] |= sub.bits[w]
		}
	}
	m.memo[k] = s
	return s
}

// stripIndex removes an array index "(n)" from an FCDA name element.
func stripIndex(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 {
		return s[:i]
	}
	return s
}
