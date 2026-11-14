// SPDX-License-Identifier: MIT

package index

import (
	"fmt"
	"strconv"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/internal/diagname"
)

// Index provides O(1) lookup into a normalized SCL model.
// Build one with [Build].
type Index struct {
	IEDs         map[string]*scl.IED
	AccessPoints map[AccessPointKey]*scl.AccessPoint
	LDevices     map[LDeviceKey]*scl.LDevice
	LNs          map[LNKey]*scl.LN
	LNodeTypes   map[string]*scl.LNodeType
	DOTypes      map[string]*scl.DOType
	DATypes      map[string]*scl.DAType
	EnumTypes    map[string]*scl.EnumType
	DataSets     map[DataSetKey]*scl.DataSet
	Reports      map[ControlKey]*scl.ReportControl
	GSEControls  map[ControlKey]*scl.GSEControl
	SMVControls  map[ControlKey]*scl.SMVControl
	ConnectedAPs map[AccessPointKey]*scl.ConnectedAP
}

// Build constructs an Index from a parsed SCL model.
// Duplicates and ambiguous entries are reported as diagnostics. A
// diagnostic quotes each name of the document cut to
// [scl.DefaultMaxDiagnosticName] bytes.
func Build(s *scl.SCL) (*Index, []scl.Diagnostic) {
	idx := &Index{
		IEDs:         make(map[string]*scl.IED),
		AccessPoints: make(map[AccessPointKey]*scl.AccessPoint),
		LDevices:     make(map[LDeviceKey]*scl.LDevice),
		LNs:          make(map[LNKey]*scl.LN),
		LNodeTypes:   make(map[string]*scl.LNodeType),
		DOTypes:      make(map[string]*scl.DOType),
		DATypes:      make(map[string]*scl.DAType),
		EnumTypes:    make(map[string]*scl.EnumType),
		DataSets:     make(map[DataSetKey]*scl.DataSet),
		Reports:      make(map[ControlKey]*scl.ReportControl),
		GSEControls:  make(map[ControlKey]*scl.GSEControl),
		SMVControls:  make(map[ControlKey]*scl.SMVControl),
		ConnectedAPs: make(map[AccessPointKey]*scl.ConnectedAP),
	}

	var diags []scl.Diagnostic
	q := diagname.Q
	add := func(sev scl.DiagSeverity, code, path, msg string) {
		diags = append(diags, scl.Diagnostic{
			Severity: sev, Code: code, Path: path, Message: msg,
		})
	}

	// unique reports a duplicate name in one parent element (an
	// xs:unique of the SCL schema, named in code).
	unique := func(path, what string, names []string) {
		seen := make(map[string]bool, len(names))
		for _, n := range names {
			if seen[n] {
				add(scl.DiagError, "duplicate-name", path, fmt.Sprintf("duplicate %s %q", what, q(n)))
			}
			seen[n] = true
		}
	}

	// Templates (SCL_DataTypeTemplates.xsd uniqueDOInLNodeType,
	// uniqueDAorSDOInDOType, uniqueBDAInDAType, uniqueOrdInEnumType).
	for _, lnt := range s.DataTypeTemplates.LNodeTypes {
		var names []string
		for _, do := range lnt.DOs {
			names = append(names, do.Name)
		}
		unique("LNodeType["+q(lnt.ID)+"]", "DO", names)
	}
	for _, dot := range s.DataTypeTemplates.DOTypes {
		// The same DA name under two FCs (for example setVal as SP and
		// SG in setting-group examples) also violates the schema but is
		// common in tool output: a warning. Other duplicates are errors.
		fcs := map[string]string{}
		var names []string
		for _, da := range dot.DAs {
			if fc, dup := fcs[da.Name]; dup && fc != da.FC {
				add(scl.DiagWarning, "duplicate-name", "DOType["+q(dot.ID)+"]",
					fmt.Sprintf("DA %q given for FC %s and %s (uniqueDAorSDOInDOType)", q(da.Name), q(fc), q(da.FC)))
				continue
			}
			fcs[da.Name] = da.FC
			names = append(names, da.Name)
		}
		for _, sdo := range dot.SDOs {
			names = append(names, sdo.Name)
		}
		unique("DOType["+q(dot.ID)+"]", "DA/SDO", names)
	}
	for _, dat := range s.DataTypeTemplates.DATypes {
		var names []string
		for _, b := range dat.BDAs {
			names = append(names, b.Name)
		}
		unique("DAType["+q(dat.ID)+"]", "BDA", names)
	}
	for _, et := range s.DataTypeTemplates.EnumTypes {
		var ords []string
		for _, v := range et.Vals {
			ords = append(ords, strconv.Itoa(v.Ord))
		}
		unique("EnumType["+q(et.ID)+"]", "EnumVal ord", ords)
	}

	for i := range s.DataTypeTemplates.LNodeTypes {
		lnt := &s.DataTypeTemplates.LNodeTypes[i]
		if _, dup := idx.LNodeTypes[lnt.ID]; dup {
			add(scl.DiagError, "duplicate-id", "LNodeType", fmt.Sprintf("duplicate LNodeType ID %q", q(lnt.ID)))
		}
		idx.LNodeTypes[lnt.ID] = lnt
	}
	for i := range s.DataTypeTemplates.DOTypes {
		dot := &s.DataTypeTemplates.DOTypes[i]
		if _, dup := idx.DOTypes[dot.ID]; dup {
			add(scl.DiagError, "duplicate-id", "DOType", fmt.Sprintf("duplicate DOType ID %q", q(dot.ID)))
		}
		idx.DOTypes[dot.ID] = dot
	}
	for i := range s.DataTypeTemplates.DATypes {
		dat := &s.DataTypeTemplates.DATypes[i]
		if _, dup := idx.DATypes[dat.ID]; dup {
			add(scl.DiagError, "duplicate-id", "DAType", fmt.Sprintf("duplicate DAType ID %q", q(dat.ID)))
		}
		idx.DATypes[dat.ID] = dat
	}
	for i := range s.DataTypeTemplates.EnumTypes {
		et := &s.DataTypeTemplates.EnumTypes[i]
		if _, dup := idx.EnumTypes[et.ID]; dup {
			add(scl.DiagError, "duplicate-id", "EnumType", fmt.Sprintf("duplicate EnumType ID %q", q(et.ID)))
		}
		idx.EnumTypes[et.ID] = et
	}

	// IEDs → APs → LDs → LNs → DataSets/Reports
	for i := range s.IEDs {
		ied := &s.IEDs[i]
		if _, dup := idx.IEDs[ied.Name]; dup {
			add(scl.DiagError, "duplicate-ied", "IED", fmt.Sprintf("duplicate IED name %q", q(ied.Name)))
		}
		idx.IEDs[ied.Name] = ied
		iedPath := "IED[" + q(ied.Name) + "]"

		for j := range ied.AccessPoints {
			ap := &ied.AccessPoints[j]
			apKey := AccessPointKey{IED: ied.Name, AP: ap.Name}
			if _, dup := idx.AccessPoints[apKey]; dup {
				add(scl.DiagError, "duplicate-access-point", iedPath,
					fmt.Sprintf("duplicate AccessPoint %q", q(ap.Name)))
			}
			idx.AccessPoints[apKey] = ap

			if ap.Server == nil {
				continue
			}
			for k := range ap.Server.LDevices {
				ld := &ap.Server.LDevices[k]
				ldKey := LDeviceKey{IED: ied.Name, LDInst: ld.Inst}
				if _, dup := idx.LDevices[ldKey]; dup {
					add(scl.DiagError, "duplicate-ld", iedPath,
						fmt.Sprintf("duplicate LDevice %q", q(ld.Inst)))
				}
				idx.LDevices[ldKey] = ld
				ldPath := iedPath + "/LD[" + q(ld.Inst) + "]"

				indexLN := func(ln *scl.LN) {
					lnKey := LNKey{
						IED: ied.Name, LDInst: ld.Inst,
						Prefix: ln.Prefix, LNClass: ln.LNClass, Inst: ln.Inst,
					}
					lnName := q(ln.Prefix + ln.LNClass + ln.Inst)
					if _, dup := idx.LNs[lnKey]; dup {
						add(scl.DiagError, "duplicate-ln", ldPath,
							fmt.Sprintf("duplicate LN %s", lnName))
					}
					idx.LNs[lnKey] = ln
					// The path of the diagnostics of the LN, built once.
					lnPath := ldPath + "/LN[" + lnName + "]"
					// SCL_IED.xsd uniqueGSEControlInLN0 and
					// uniqueSampledValueControlInLN0. (uniqueDOIinLN is
					// not checked: the model drops DOI@ix, which tells
					// array elements of the same name apart.)
					var gcs, svs []string
					for _, gc := range ln.GSEControls {
						gcs = append(gcs, gc.Name)
					}
					for _, sv := range ln.SMVControls {
						svs = append(svs, sv.Name)
					}
					unique(lnPath, "GSEControl", gcs)
					unique(lnPath, "SampledValueControl", svs)

					for m := range ln.DataSets {
						ds := &ln.DataSets[m]
						dsKey := DataSetKey{
							IED: ied.Name, LDInst: ld.Inst,
							Prefix: ln.Prefix, LNClass: ln.LNClass, LNInst: ln.Inst,
							Name: ds.Name,
						}
						if _, dup := idx.DataSets[dsKey]; dup {
							// DataSetKeyLN0 / DataSetKeyInLN are
							// xs:key constraints: an error.
							add(scl.DiagError, "duplicate-dataset", lnPath,
								fmt.Sprintf("duplicate DataSet %q", q(ds.Name)))
						}
						idx.DataSets[dsKey] = ds
					}
					for m := range ln.Reports {
						rc := &ln.Reports[m]
						rcKey := ControlKey{
							IED: ied.Name, LDInst: ld.Inst,
							Prefix: ln.Prefix, LNClass: ln.LNClass, LNInst: ln.Inst,
							Name: rc.Name,
						}
						if _, dup := idx.Reports[rcKey]; dup {
							add(scl.DiagWarning, "duplicate-report", lnPath,
								fmt.Sprintf("duplicate ReportControl %q", q(rc.Name)))
						}
						idx.Reports[rcKey] = rc
					}
					for m := range ln.GSEControls {
						gc := &ln.GSEControls[m]
						gcKey := ControlKey{
							IED: ied.Name, LDInst: ld.Inst,
							Prefix: ln.Prefix, LNClass: ln.LNClass, LNInst: ln.Inst,
							Name: gc.Name,
						}
						idx.GSEControls[gcKey] = gc
					}
					for m := range ln.SMVControls {
						sv := &ln.SMVControls[m]
						svKey := ControlKey{
							IED: ied.Name, LDInst: ld.Inst,
							Prefix: ln.Prefix, LNClass: ln.LNClass, LNInst: ln.Inst,
							Name: sv.Name,
						}
						idx.SMVControls[svKey] = sv
					}
				}

				if ld.LN0 != nil {
					indexLN(ld.LN0)
				}
				for m := range ld.LNs {
					indexLN(&ld.LNs[m])
				}
			}
		}
	}

	// Communication → ConnectedAPs
	if s.Communication != nil {
		var sns []string
		for _, sn := range s.Communication.SubNetworks {
			sns = append(sns, sn.Name)
		}
		unique("Communication", "SubNetwork", sns) // uniqueSubNetwork
		for i := range s.Communication.SubNetworks {
			sn := &s.Communication.SubNetworks[i]
			snPath := "SubNetwork[" + q(sn.Name) + "]"
			for j := range sn.ConnectedAPs {
				cap := &sn.ConnectedAPs[j]
				// uniqueGSEinConnectedAP / uniqueSMVinConnectedAP:
				// (cbName, ldInst).
				var gses, smvs []string
				for _, g := range cap.GSEs {
					gses = append(gses, g.LDInst+"/"+g.CBName)
				}
				for _, v := range cap.SMVs {
					smvs = append(smvs, v.LDInst+"/"+v.CBName)
				}
				capPath := snPath + "/ConnectedAP[" + q(cap.IEDName) + "/" + q(cap.APName) + "]"
				unique(capPath, "GSE", gses)
				unique(capPath, "SMV", smvs)
				capKey := AccessPointKey{IED: cap.IEDName, AP: cap.APName}
				if _, dup := idx.ConnectedAPs[capKey]; dup {
					add(scl.DiagWarning, "duplicate-connected-ap", snPath,
						fmt.Sprintf("duplicate ConnectedAP for IED %q AP %q", q(cap.IEDName), q(cap.APName)))
				}
				idx.ConnectedAPs[capKey] = cap
			}
		}
	}

	return idx, diags
}
