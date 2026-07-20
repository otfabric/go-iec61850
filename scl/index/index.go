// SPDX-License-Identifier: MIT

package index

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
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
// Duplicates and ambiguous entries are reported as diagnostics.
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
	add := func(sev scl.DiagSeverity, code, path, msg string) {
		diags = append(diags, scl.Diagnostic{
			Severity: sev, Code: code, Path: path, Message: msg,
		})
	}

	// Templates
	for i := range s.DataTypeTemplates.LNodeTypes {
		lnt := &s.DataTypeTemplates.LNodeTypes[i]
		if _, dup := idx.LNodeTypes[lnt.ID]; dup {
			add(scl.DiagError, "duplicate-id", "LNodeType", fmt.Sprintf("duplicate LNodeType ID %q", lnt.ID))
		}
		idx.LNodeTypes[lnt.ID] = lnt
	}
	for i := range s.DataTypeTemplates.DOTypes {
		dot := &s.DataTypeTemplates.DOTypes[i]
		if _, dup := idx.DOTypes[dot.ID]; dup {
			add(scl.DiagError, "duplicate-id", "DOType", fmt.Sprintf("duplicate DOType ID %q", dot.ID))
		}
		idx.DOTypes[dot.ID] = dot
	}
	for i := range s.DataTypeTemplates.DATypes {
		dat := &s.DataTypeTemplates.DATypes[i]
		if _, dup := idx.DATypes[dat.ID]; dup {
			add(scl.DiagError, "duplicate-id", "DAType", fmt.Sprintf("duplicate DAType ID %q", dat.ID))
		}
		idx.DATypes[dat.ID] = dat
	}
	for i := range s.DataTypeTemplates.EnumTypes {
		et := &s.DataTypeTemplates.EnumTypes[i]
		if _, dup := idx.EnumTypes[et.ID]; dup {
			add(scl.DiagError, "duplicate-id", "EnumType", fmt.Sprintf("duplicate EnumType ID %q", et.ID))
		}
		idx.EnumTypes[et.ID] = et
	}

	// IEDs → APs → LDs → LNs → DataSets/Reports
	for i := range s.IEDs {
		ied := &s.IEDs[i]
		if _, dup := idx.IEDs[ied.Name]; dup {
			add(scl.DiagError, "duplicate-ied", "IED", fmt.Sprintf("duplicate IED name %q", ied.Name))
		}
		idx.IEDs[ied.Name] = ied

		for j := range ied.AccessPoints {
			ap := &ied.AccessPoints[j]
			apKey := AccessPointKey{IED: ied.Name, AP: ap.Name}
			if _, dup := idx.AccessPoints[apKey]; dup {
				add(scl.DiagError, "duplicate-access-point",
					fmt.Sprintf("IED[%s]", ied.Name),
					fmt.Sprintf("duplicate AccessPoint %q", ap.Name))
			}
			idx.AccessPoints[apKey] = ap

			if ap.Server == nil {
				continue
			}
			for k := range ap.Server.LDevices {
				ld := &ap.Server.LDevices[k]
				ldKey := LDeviceKey{IED: ied.Name, LDInst: ld.Inst}
				if _, dup := idx.LDevices[ldKey]; dup {
					add(scl.DiagError, "duplicate-ld",
						fmt.Sprintf("IED[%s]", ied.Name),
						fmt.Sprintf("duplicate LDevice %q", ld.Inst))
				}
				idx.LDevices[ldKey] = ld

				indexLN := func(ln *scl.LN) {
					lnKey := LNKey{
						IED: ied.Name, LDInst: ld.Inst,
						Prefix: ln.Prefix, LNClass: ln.LNClass, Inst: ln.Inst,
					}
					if _, dup := idx.LNs[lnKey]; dup {
						add(scl.DiagError, "duplicate-ln",
							fmt.Sprintf("IED[%s]/LD[%s]", ied.Name, ld.Inst),
							fmt.Sprintf("duplicate LN %s%s%s", ln.Prefix, ln.LNClass, ln.Inst))
					}
					idx.LNs[lnKey] = ln

					for m := range ln.DataSets {
						ds := &ln.DataSets[m]
						dsKey := DataSetKey{
							IED: ied.Name, LDInst: ld.Inst,
							Prefix: ln.Prefix, LNClass: ln.LNClass, LNInst: ln.Inst,
							Name: ds.Name,
						}
						if _, dup := idx.DataSets[dsKey]; dup {
							add(scl.DiagWarning, "duplicate-dataset",
								fmt.Sprintf("IED[%s]/LD[%s]/LN[%s%s%s]", ied.Name, ld.Inst, ln.Prefix, ln.LNClass, ln.Inst),
								fmt.Sprintf("duplicate DataSet %q", ds.Name))
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
							add(scl.DiagWarning, "duplicate-report",
								fmt.Sprintf("IED[%s]/LD[%s]/LN[%s%s%s]", ied.Name, ld.Inst, ln.Prefix, ln.LNClass, ln.Inst),
								fmt.Sprintf("duplicate ReportControl %q", rc.Name))
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
		for i := range s.Communication.SubNetworks {
			sn := &s.Communication.SubNetworks[i]
			for j := range sn.ConnectedAPs {
				cap := &sn.ConnectedAPs[j]
				capKey := AccessPointKey{IED: cap.IEDName, AP: cap.APName}
				if _, dup := idx.ConnectedAPs[capKey]; dup {
					add(scl.DiagWarning, "duplicate-connected-ap",
						fmt.Sprintf("SubNetwork[%s]", sn.Name),
						fmt.Sprintf("duplicate ConnectedAP for IED %q AP %q", cap.IEDName, cap.APName))
				}
				idx.ConnectedAPs[capKey] = cap
			}
		}
	}

	return idx, diags
}
