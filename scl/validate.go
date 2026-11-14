// SPDX-License-Identifier: MIT

package scl

import "fmt"

// Validate performs semantic validation on an SCL model beyond basic
// parsing. It checks cross-references, type chains, logical
// consistency, topology LNode references and required attributes.
//
// Deprecated: Use [validate.All] from the scl/validate package for
// pass-based validation with a shared index. This function is kept
// for convenience but may not cover all checks that the pass-based
// validator provides.
//
// A name quoted in a diagnostic is cut to [DefaultMaxDiagnosticName]
// bytes.
func Validate(s *SCL) []Diagnostic {
	sink := newDiagSink(0, DefaultMaxDiagnosticName)
	validateInto(s, sink)
	return sink.diags
}

// validateInto runs [Validate] into sink; it stops once sink is full.
func validateInto(s *SCL, sink *diagSink) {
	v := &validator{
		sink:    sink,
		lntIdx:  make(map[string]bool),
		lntCls:  make(map[string]string),
		dotIdx:  make(map[string]bool),
		datIdx:  make(map[string]bool),
		enumIdx: make(map[string]bool),
	}

	v.buildIndexes(s)
	v.validateTypeTemplates(s)
	v.validateIEDs(s)
	v.validateCommunication(s)
	v.validateTopology(s)
	if !sink.full() {
		validateRequiredInto(s, sink)
	}
}

type validator struct {
	sink    *diagSink
	lntIdx  map[string]bool
	lntCls  map[string]string // LNodeType id -> lnClass
	dotIdx  map[string]bool
	datIdx  map[string]bool
	enumIdx map[string]bool
	iedIdx  map[string]*iedInfo
}

type iedInfo struct {
	aps map[string]bool
	lds map[string]bool
}

func (v *validator) buildIndexes(s *SCL) {
	for _, lnt := range s.DataTypeTemplates.LNodeTypes {
		if v.lntIdx[lnt.ID] {
			v.addErr("duplicate-id", "LNodeType", fmt.Sprintf("duplicate ID %q", v.n(lnt.ID)))
		}
		v.lntIdx[lnt.ID] = true
		v.lntCls[lnt.ID] = lnt.LNClass
	}
	for _, dot := range s.DataTypeTemplates.DOTypes {
		if v.dotIdx[dot.ID] {
			v.addErr("duplicate-id", "DOType", fmt.Sprintf("duplicate ID %q", v.n(dot.ID)))
		}
		v.dotIdx[dot.ID] = true
	}
	for _, dat := range s.DataTypeTemplates.DATypes {
		if v.datIdx[dat.ID] {
			v.addErr("duplicate-id", "DAType", fmt.Sprintf("duplicate ID %q", v.n(dat.ID)))
		}
		v.datIdx[dat.ID] = true
	}
	for _, et := range s.DataTypeTemplates.EnumTypes {
		if v.enumIdx[et.ID] {
			v.addErr("duplicate-id", "EnumType", fmt.Sprintf("duplicate ID %q", v.n(et.ID)))
		}
		v.enumIdx[et.ID] = true
	}
	v.iedIdx = make(map[string]*iedInfo)
	for _, ied := range s.IEDs {
		if v.iedIdx[ied.Name] != nil {
			v.addErr("duplicate-ied", "IED", fmt.Sprintf("duplicate name %q", v.n(ied.Name)))
		}
		info := &iedInfo{aps: make(map[string]bool), lds: make(map[string]bool)}
		for _, ap := range ied.AccessPoints {
			info.aps[ap.Name] = true
			if ap.Server != nil {
				for _, ld := range ap.Server.LDevices {
					info.lds[ld.Inst] = true
				}
			}
		}
		v.iedIdx[ied.Name] = info
	}
}

func (v *validator) addErr(code, path, msg string) {
	v.sink.add(Diagnostic{Severity: DiagError, Code: code, Path: path, Message: msg})
}

func (v *validator) addWarn(code, path, msg string) {
	v.sink.add(Diagnostic{Severity: DiagWarning, Code: code, Path: path, Message: msg})
}

// n returns a name of the document as a diagnostic quotes it.
func (v *validator) n(name string) string { return v.sink.name(name) }

func (v *validator) validateTypeTemplates(s *SCL) {
	for _, lnt := range s.DataTypeTemplates.LNodeTypes {
		if v.sink.full() {
			return
		}
		path := fmt.Sprintf("LNodeType[%s]", v.n(lnt.ID))
		for _, do := range lnt.DOs {
			if !v.dotIdx[do.Type] {
				v.addErr("missing-dotype", path+".DO["+v.n(do.Name)+"]",
					fmt.Sprintf("references DOType %q which does not exist", v.n(do.Type)))
			}
		}
	}
	for _, dot := range s.DataTypeTemplates.DOTypes {
		if v.sink.full() {
			return
		}
		path := fmt.Sprintf("DOType[%s]", v.n(dot.ID))
		for _, da := range dot.DAs {
			v.checkTypeRef(path, ".DA[", da.Name, da.BType, da.Type)
		}
		for _, sdo := range dot.SDOs {
			if !v.dotIdx[sdo.Type] {
				v.addErr("missing-dotype", path+".SDO["+v.n(sdo.Name)+"]",
					fmt.Sprintf("references DOType %q which does not exist", v.n(sdo.Type)))
			}
		}
	}
	for _, dat := range s.DataTypeTemplates.DATypes {
		if v.sink.full() {
			return
		}
		path := fmt.Sprintf("DAType[%s]", v.n(dat.ID))
		for _, bda := range dat.BDAs {
			v.checkTypeRef(path, ".BDA[", bda.Name, bda.BType, bda.Type)
		}
	}
}

// checkTypeRef checks the type reference of the DA or BDA name in the
// type at parent; elem is ".DA[" or ".BDA[". The path is built only for
// a diagnostic.
func (v *validator) checkTypeRef(parent, elem, name, bType, typRef string) {
	if typRef == "" {
		return
	}
	path := func() string { return parent + elem + v.n(name) + "]" }
	switch bType {
	case "Struct":
		if !v.datIdx[typRef] {
			v.addErr("missing-datype", path(), fmt.Sprintf("references DAType %q which does not exist", v.n(typRef)))
		}
	case "Enum":
		if !v.enumIdx[typRef] {
			v.addErr("missing-enumtype", path(), fmt.Sprintf("references EnumType %q which does not exist", v.n(typRef)))
		}
	}
}

func (v *validator) validateIEDs(s *SCL) {
	for _, ied := range s.IEDs {
		if v.sink.full() {
			return
		}
		iedPath := fmt.Sprintf("IED[%s]", v.n(ied.Name))
		apNames := make(map[string]bool)
		for _, ap := range ied.AccessPoints {
			if apNames[ap.Name] {
				v.addErr("duplicate-access-point", iedPath, fmt.Sprintf("duplicate AccessPoint name %q", v.n(ap.Name)))
			}
			apNames[ap.Name] = true
		}
		ldInsts := make(map[string]bool)
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				if v.sink.full() {
					return
				}
				if ldInsts[ld.Inst] {
					v.addErr("duplicate-ld", iedPath, fmt.Sprintf("duplicate LDevice instance %q", v.n(ld.Inst)))
				}
				ldInsts[ld.Inst] = true
				ldPath := iedPath + "/LD[" + v.n(ld.Inst) + "]"
				if ld.LN0 != nil {
					v.validateLN(ldPath, *ld.LN0)
				}
				for _, ln := range ld.LNs {
					v.validateLN(ldPath, ln)
				}
			}
		}
	}
}

func (v *validator) validateLN(ldPath string, ln LN) {
	if v.sink.full() {
		return
	}
	lnPath := ldPath + "/LN[" + v.n(ln.Prefix+ln.LNClass+ln.Inst) + "]"
	if !v.lntIdx[ln.LNType] {
		v.addErr("missing-lnodetype", lnPath, fmt.Sprintf("references LNodeType %q which does not exist", v.n(ln.LNType)))
	} else if c := v.lntCls[ln.LNType]; c != ln.LNClass {
		// SCL.xsd LNodeTypeKey (@id, @lnClass) and its keyrefs.
		v.addErr("lnclass-mismatch", lnPath, fmt.Sprintf("lnClass %q references LNodeType %q of lnClass %q (SCL key LNodeTypeKey)", v.n(ln.LNClass), v.n(ln.LNType), v.n(c)))
	}
	dsNames := make(map[string]bool)
	for _, ds := range ln.DataSets {
		dsNames[ds.Name] = true
	}
	for _, rc := range ln.Reports {
		if rc.DatSet != "" && !dsNames[rc.DatSet] {
			v.addErr("missing-dataset", lnPath+"/RC["+v.n(rc.Name)+"]",
				fmt.Sprintf("references datSet %q which is not defined in this LN", v.n(rc.DatSet)))
		}
	}
	for _, gc := range ln.GSEControls {
		if gc.DatSet != "" && !dsNames[gc.DatSet] {
			v.addErr("missing-dataset", lnPath+"/GC["+v.n(gc.Name)+"]",
				fmt.Sprintf("references datSet %q which is not defined in this LN", v.n(gc.DatSet)))
		}
	}
	for _, sv := range ln.SMVControls {
		if sv.DatSet != "" && !dsNames[sv.DatSet] {
			v.addErr("missing-dataset", lnPath+"/SV["+v.n(sv.Name)+"]",
				fmt.Sprintf("references datSet %q which is not defined in this LN", v.n(sv.DatSet)))
		}
	}
}

func (v *validator) validateCommunication(s *SCL) {
	if s.Communication == nil {
		return
	}
	for _, sn := range s.Communication.SubNetworks {
		snPath := fmt.Sprintf("SubNetwork[%s]", v.n(sn.Name))
		for _, cap := range sn.ConnectedAPs {
			if v.sink.full() {
				return
			}
			capPath := snPath + "/ConnectedAP[" + v.n(cap.IEDName) + "/" + v.n(cap.APName) + "]"
			info, exists := v.iedIdx[cap.IEDName]
			if !exists {
				v.addErr("missing-connected-ap", capPath, fmt.Sprintf("references IED %q which does not exist", v.n(cap.IEDName)))
				continue
			}
			if !info.aps[cap.APName] {
				v.addErr("missing-connected-ap", capPath, fmt.Sprintf("references AccessPoint %q which does not exist in IED %q", v.n(cap.APName), v.n(cap.IEDName)))
			}
			for _, gse := range cap.GSEs {
				if gse.LDInst != "" && !info.lds[gse.LDInst] {
					v.addWarn("missing-ld", capPath+"/GSE["+v.n(gse.CBName)+"]",
						fmt.Sprintf("references LDevice %q which does not exist in IED %q", v.n(gse.LDInst), v.n(cap.IEDName)))
				}
			}
			for _, smv := range cap.SMVs {
				if smv.LDInst != "" && !info.lds[smv.LDInst] {
					v.addWarn("missing-ld", capPath+"/SMV["+v.n(smv.CBName)+"]",
						fmt.Sprintf("references LDevice %q which does not exist in IED %q", v.n(smv.LDInst), v.n(cap.IEDName)))
				}
			}
		}
	}
}

func (v *validator) validateTopology(s *SCL) {
	for _, sub := range s.Substations {
		if v.sink.full() {
			return
		}
		subPath := fmt.Sprintf("Substation[%s]", v.n(sub.Name))
		v.checkLNodeRefs(subPath, sub.LNodes)
		for _, vl := range sub.VoltageLevels {
			vlPath := subPath + "/VoltageLevel[" + v.n(vl.Name) + "]"
			v.checkLNodeRefs(vlPath, vl.LNodes)
			for _, bay := range vl.Bays {
				bayPath := vlPath + "/Bay[" + v.n(bay.Name) + "]"
				v.checkLNodeRefs(bayPath, bay.LNodes)
			}
		}
	}
}

func (v *validator) checkLNodeRefs(parentPath string, lnodes []LNode) {
	for _, ln := range lnodes {
		if ln.IEDName == "" || ln.IEDName == "None" {
			continue
		}
		lnPath := func() string {
			return fmt.Sprintf("%s/LNode[%s/%s/%s]",
				parentPath, v.n(ln.IEDName), v.n(ln.LDInst), v.n(ln.Prefix+ln.LNClass+ln.LNInst))
		}
		if v.iedIdx[ln.IEDName] == nil {
			v.addErr("unresolved-topology-lnode", lnPath(),
				fmt.Sprintf("references IED %q which does not exist", v.n(ln.IEDName)))
			continue
		}
		if ln.LDInst != "" && !v.iedIdx[ln.IEDName].lds[ln.LDInst] {
			v.addErr("unresolved-topology-lnode", lnPath(),
				fmt.Sprintf("references LDevice %q which does not exist in IED %q", v.n(ln.LDInst), v.n(ln.IEDName)))
		}
	}
}
