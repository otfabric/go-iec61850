// SPDX-License-Identifier: MIT

package scl

import "fmt"

// ValidateRequired reports required attributes that are missing or empty.
// The attributes are those the IEC 61850-6 SCL schemas (specs/, 2007B4)
// declare use="required" with a type that cannot be empty (tName, tID, a
// pattern requiring a first character, an enumeration). Left out: Header
// id (xs:normalizedString, may be empty), LN inst (empty on LN0) and
// numeric attributes, whose absence cannot be told from their zero value.
// A parsed document without a Header element is reported too
// (missing-element, from [DocumentMetadata.HasHeader]); a model built in
// code has no metadata and is not checked for it.
func ValidateRequired(s *SCL) []Diagnostic {
	if s == nil {
		return nil
	}
	var diags []Diagnostic
	if s.Metadata != nil && !s.Metadata.HasHeader {
		diags = append(diags, Diagnostic{
			Severity: DiagError, Code: "missing-element", Path: "SCL",
			Message: "SCL has no Header element (required by the SCL schema)",
		})
	}
	need := func(path, elem, attr, value string) {
		if value == "" {
			diags = append(diags, Diagnostic{
				Severity: DiagError, Code: "missing-attribute", Path: path,
				Message: fmt.Sprintf("%s has no %s (required by the SCL schema)", elem, attr),
			})
		}
	}
	at := func(name string, i int) string {
		if name != "" {
			return name
		}
		return fmt.Sprintf("#%d", i+1)
	}

	for i, sub := range s.Substations {
		sp := "Substation[" + at(sub.Name, i) + "]"
		need(sp, "Substation", "name", sub.Name)
		for j, vl := range sub.VoltageLevels {
			vp := sp + "/VoltageLevel[" + at(vl.Name, j) + "]"
			need(vp, "VoltageLevel", "name", vl.Name)
			for k, bay := range vl.Bays {
				bp := vp + "/Bay[" + at(bay.Name, k) + "]"
				need(bp, "Bay", "name", bay.Name)
				for m, ce := range bay.ConductingEquipments {
					cp := bp + "/ConductingEquipment[" + at(ce.Name, m) + "]"
					need(cp, "ConductingEquipment", "name", ce.Name)
					need(cp, "ConductingEquipment", "type", ce.Type)
				}
			}
		}
	}

	if c := s.Communication; c != nil {
		for i, sn := range c.SubNetworks {
			sp := "SubNetwork[" + at(sn.Name, i) + "]"
			need(sp, "SubNetwork", "name", sn.Name)
			for j, cap := range sn.ConnectedAPs {
				cp := fmt.Sprintf("%s/ConnectedAP[#%d]", sp, j+1)
				need(cp, "ConnectedAP", "iedName", cap.IEDName)
				need(cp, "ConnectedAP", "apName", cap.APName)
			}
		}
	}

	for i, ied := range s.IEDs {
		ip := "IED[" + at(ied.Name, i) + "]"
		need(ip, "IED", "name", ied.Name)
		for j, ap := range ied.AccessPoints {
			ap2 := ip + "/AccessPoint[" + at(ap.Name, j) + "]"
			need(ap2, "AccessPoint", "name", ap.Name)
			if ap.Server == nil {
				continue
			}
			for k, ld := range ap.Server.LDevices {
				lp := ap2 + "/LDevice[" + at(ld.Inst, k) + "]"
				need(lp, "LDevice", "inst", ld.Inst)
				for m, ln := range ld.LNs {
					np := fmt.Sprintf("%s/LN[%s%s%s]", lp, ln.Prefix, at(ln.LNClass, m), ln.Inst)
					need(np, "LN", "lnClass", ln.LNClass)
					need(np, "LN", "lnType", ln.LNType)
					for n, ds := range ln.DataSets {
						dp := np + "/DataSet[" + at(ds.Name, n) + "]"
						need(dp, "DataSet", "name", ds.Name)
						for q, f := range ds.FCDAs {
							need(fmt.Sprintf("%s/FCDA[#%d]", dp, q+1), "FCDA", "fc", f.FC)
						}
					}
					for n, rc := range ln.Reports {
						need(np+"/ReportControl["+at(rc.Name, n)+"]", "ReportControl", "name", rc.Name)
					}
					for n, gc := range ln.GSEControls {
						gp := np + "/GSEControl[" + at(gc.Name, n) + "]"
						need(gp, "GSEControl", "name", gc.Name)
						need(gp, "GSEControl", "appID", gc.AppID)
					}
				}
			}
		}
	}

	t := s.DataTypeTemplates
	for i, lnt := range t.LNodeTypes {
		p := "LNodeType[" + at(lnt.ID, i) + "]"
		need(p, "LNodeType", "id", lnt.ID)
		need(p, "LNodeType", "lnClass", lnt.LNClass)
		for j, do := range lnt.DOs {
			dp := p + "/DO[" + at(do.Name, j) + "]"
			need(dp, "DO", "name", do.Name)
			need(dp, "DO", "type", do.Type)
		}
	}
	for i, dot := range t.DOTypes {
		p := "DOType[" + at(dot.ID, i) + "]"
		need(p, "DOType", "id", dot.ID)
		need(p, "DOType", "cdc", dot.CDC)
		for j, da := range dot.DAs {
			dp := p + "/DA[" + at(da.Name, j) + "]"
			need(dp, "DA", "name", da.Name)
			need(dp, "DA", "bType", da.BType)
			need(dp, "DA", "fc", da.FC)
		}
	}
	for i, dat := range t.DATypes {
		p := "DAType[" + at(dat.ID, i) + "]"
		need(p, "DAType", "id", dat.ID)
		for j, bda := range dat.BDAs {
			bp := p + "/BDA[" + at(bda.Name, j) + "]"
			need(bp, "BDA", "name", bda.Name)
			need(bp, "BDA", "bType", bda.BType)
		}
	}
	for i, et := range t.EnumTypes {
		need("EnumType["+at(et.ID, i)+"]", "EnumType", "id", et.ID)
	}
	return diags
}

// ValidateKind reports content a document of the given kind must have
// (IEC 61850-6, SCL file types): an ICD, IID or CID describes an IED, so
// it has at least one IED section; an ICD or IID describes exactly one
// (vendor CIDs also carry their client IEDs, so CID is not limited); an
// SSD specifies a substation. KindUnknown and KindSCD add nothing.
func ValidateKind(s *SCL, kind DocumentKind) []Diagnostic {
	if s == nil {
		return nil
	}
	var diags []Diagnostic
	switch kind {
	case KindICD, KindIID, KindCID:
		if len(s.IEDs) == 0 {
			diags = append(diags, Diagnostic{
				Severity: DiagError, Code: "kind-content", Path: "SCL",
				Message: fmt.Sprintf("a %s file describes an IED, but this one has no IED section", kind),
			})
		} else if kind != KindCID && len(s.IEDs) > 1 {
			diags = append(diags, Diagnostic{
				Severity: DiagWarning, Code: "kind-content", Path: "SCL",
				Message: fmt.Sprintf("a %s file describes one IED, but this one has %d", kind, len(s.IEDs)),
			})
		}
	case KindSSD:
		if len(s.Substations) == 0 {
			diags = append(diags, Diagnostic{
				Severity: DiagWarning, Code: "kind-content", Path: "SCL",
				Message: "an SSD file specifies a substation, but this one has no Substation section",
			})
		}
	case KindSCD, KindUnknown:
		// An SCD may be any part of a system; an unknown kind has no rule.
	}
	return diags
}
