// SPDX-License-Identifier: MIT

package scl

import "fmt"

// Validate performs semantic validation on an SCL model beyond basic
// parsing. It checks cross-references, type chains, logical
// consistency, and topology LNode references.
//
// Deprecated: Use [validate.All] from the scl/validate package for
// pass-based validation with a shared index. This function is kept
// for convenience but may not cover all checks that the pass-based
// validator provides.
func Validate(s *SCL) []Diagnostic {
	v := &validator{
		lntIdx:  make(map[string]bool),
		dotIdx:  make(map[string]bool),
		datIdx:  make(map[string]bool),
		enumIdx: make(map[string]bool),
	}

	v.buildIndexes(s)
	v.validateTypeTemplates(s)
	v.validateIEDs(s)
	v.validateCommunication(s)
	v.validateTopology(s)

	return v.diags
}

type validator struct {
	diags   []Diagnostic
	lntIdx  map[string]bool
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
			v.addErr("duplicate-id", "LNodeType", fmt.Sprintf("duplicate ID %q", lnt.ID))
		}
		v.lntIdx[lnt.ID] = true
	}
	for _, dot := range s.DataTypeTemplates.DOTypes {
		if v.dotIdx[dot.ID] {
			v.addErr("duplicate-id", "DOType", fmt.Sprintf("duplicate ID %q", dot.ID))
		}
		v.dotIdx[dot.ID] = true
	}
	for _, dat := range s.DataTypeTemplates.DATypes {
		if v.datIdx[dat.ID] {
			v.addErr("duplicate-id", "DAType", fmt.Sprintf("duplicate ID %q", dat.ID))
		}
		v.datIdx[dat.ID] = true
	}
	for _, et := range s.DataTypeTemplates.EnumTypes {
		if v.enumIdx[et.ID] {
			v.addErr("duplicate-id", "EnumType", fmt.Sprintf("duplicate ID %q", et.ID))
		}
		v.enumIdx[et.ID] = true
	}
	v.iedIdx = make(map[string]*iedInfo)
	for _, ied := range s.IEDs {
		if v.iedIdx[ied.Name] != nil {
			v.addErr("duplicate-ied", "IED", fmt.Sprintf("duplicate name %q", ied.Name))
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
	v.diags = append(v.diags, Diagnostic{Severity: DiagError, Code: code, Path: path, Message: msg})
}

func (v *validator) addWarn(code, path, msg string) {
	v.diags = append(v.diags, Diagnostic{Severity: DiagWarning, Code: code, Path: path, Message: msg})
}

func (v *validator) validateTypeTemplates(s *SCL) {
	for _, lnt := range s.DataTypeTemplates.LNodeTypes {
		path := fmt.Sprintf("LNodeType[%s]", lnt.ID)
		for _, do := range lnt.DOs {
			if !v.dotIdx[do.Type] {
				v.addErr("missing-dotype", path+".DO["+do.Name+"]",
					fmt.Sprintf("references DOType %q which does not exist", do.Type))
			}
		}
	}
	for _, dot := range s.DataTypeTemplates.DOTypes {
		path := fmt.Sprintf("DOType[%s]", dot.ID)
		for _, da := range dot.DAs {
			v.checkTypeRef(path+".DA["+da.Name+"]", da.BType, da.Type)
		}
		for _, sdo := range dot.SDOs {
			if !v.dotIdx[sdo.Type] {
				v.addErr("missing-dotype", path+".SDO["+sdo.Name+"]",
					fmt.Sprintf("references DOType %q which does not exist", sdo.Type))
			}
		}
	}
	for _, dat := range s.DataTypeTemplates.DATypes {
		path := fmt.Sprintf("DAType[%s]", dat.ID)
		for _, bda := range dat.BDAs {
			v.checkTypeRef(path+".BDA["+bda.Name+"]", bda.BType, bda.Type)
		}
	}
}

func (v *validator) checkTypeRef(path, bType, typRef string) {
	if typRef == "" {
		return
	}
	switch bType {
	case "Struct":
		if !v.datIdx[typRef] {
			v.addErr("missing-datype", path, fmt.Sprintf("references DAType %q which does not exist", typRef))
		}
	case "Enum":
		if !v.enumIdx[typRef] {
			v.addErr("missing-enumtype", path, fmt.Sprintf("references EnumType %q which does not exist", typRef))
		}
	}
}

func (v *validator) validateIEDs(s *SCL) {
	for _, ied := range s.IEDs {
		iedPath := fmt.Sprintf("IED[%s]", ied.Name)
		apNames := make(map[string]bool)
		for _, ap := range ied.AccessPoints {
			if apNames[ap.Name] {
				v.addErr("duplicate-access-point", iedPath, fmt.Sprintf("duplicate AccessPoint name %q", ap.Name))
			}
			apNames[ap.Name] = true
		}
		ldInsts := make(map[string]bool)
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				if ldInsts[ld.Inst] {
					v.addErr("duplicate-ld", iedPath, fmt.Sprintf("duplicate LDevice instance %q", ld.Inst))
				}
				ldInsts[ld.Inst] = true
				ldPath := iedPath + "/LD[" + ld.Inst + "]"
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
	lnPath := ldPath + "/LN[" + ln.Prefix + ln.LNClass + ln.Inst + "]"
	if !v.lntIdx[ln.LNType] {
		v.addErr("missing-lnodetype", lnPath, fmt.Sprintf("references LNodeType %q which does not exist", ln.LNType))
	}
	dsNames := make(map[string]bool)
	for _, ds := range ln.DataSets {
		dsNames[ds.Name] = true
	}
	for _, rc := range ln.Reports {
		if rc.DatSet != "" && !dsNames[rc.DatSet] {
			v.addErr("missing-dataset", lnPath+"/RC["+rc.Name+"]",
				fmt.Sprintf("references datSet %q which is not defined in this LN", rc.DatSet))
		}
	}
	for _, gc := range ln.GSEControls {
		if gc.DatSet != "" && !dsNames[gc.DatSet] {
			v.addErr("missing-dataset", lnPath+"/GC["+gc.Name+"]",
				fmt.Sprintf("references datSet %q which is not defined in this LN", gc.DatSet))
		}
	}
	for _, sv := range ln.SMVControls {
		if sv.DatSet != "" && !dsNames[sv.DatSet] {
			v.addErr("missing-dataset", lnPath+"/SV["+sv.Name+"]",
				fmt.Sprintf("references datSet %q which is not defined in this LN", sv.DatSet))
		}
	}
}

func (v *validator) validateCommunication(s *SCL) {
	if s.Communication == nil {
		return
	}
	for _, sn := range s.Communication.SubNetworks {
		snPath := fmt.Sprintf("SubNetwork[%s]", sn.Name)
		for _, cap := range sn.ConnectedAPs {
			capPath := snPath + "/ConnectedAP[" + cap.IEDName + "/" + cap.APName + "]"
			info, exists := v.iedIdx[cap.IEDName]
			if !exists {
				v.addErr("missing-connected-ap", capPath, fmt.Sprintf("references IED %q which does not exist", cap.IEDName))
				continue
			}
			if !info.aps[cap.APName] {
				v.addErr("missing-connected-ap", capPath, fmt.Sprintf("references AccessPoint %q which does not exist in IED %q", cap.APName, cap.IEDName))
			}
			for _, gse := range cap.GSEs {
				if gse.LDInst != "" && !info.lds[gse.LDInst] {
					v.addWarn("missing-ld", capPath+"/GSE["+gse.CBName+"]",
						fmt.Sprintf("references LDevice %q which does not exist in IED %q", gse.LDInst, cap.IEDName))
				}
			}
			for _, smv := range cap.SMVs {
				if smv.LDInst != "" && !info.lds[smv.LDInst] {
					v.addWarn("missing-ld", capPath+"/SMV["+smv.CBName+"]",
						fmt.Sprintf("references LDevice %q which does not exist in IED %q", smv.LDInst, cap.IEDName))
				}
			}
		}
	}
}

func (v *validator) validateTopology(s *SCL) {
	for _, sub := range s.Substations {
		subPath := fmt.Sprintf("Substation[%s]", sub.Name)
		v.checkLNodeRefs(subPath, sub.LNodes)
		for _, vl := range sub.VoltageLevels {
			vlPath := subPath + "/VoltageLevel[" + vl.Name + "]"
			v.checkLNodeRefs(vlPath, vl.LNodes)
			for _, bay := range vl.Bays {
				bayPath := vlPath + "/Bay[" + bay.Name + "]"
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
		lnPath := fmt.Sprintf("%s/LNode[%s/%s/%s%s%s]",
			parentPath, ln.IEDName, ln.LDInst, ln.Prefix, ln.LNClass, ln.LNInst)
		if v.iedIdx[ln.IEDName] == nil {
			v.addErr("unresolved-topology-lnode", lnPath,
				fmt.Sprintf("references IED %q which does not exist", ln.IEDName))
			continue
		}
		if ln.LDInst != "" && !v.iedIdx[ln.IEDName].lds[ln.LDInst] {
			v.addErr("unresolved-topology-lnode", lnPath,
				fmt.Sprintf("references LDevice %q which does not exist in IED %q", ln.LDInst, ln.IEDName))
		}
	}
}
