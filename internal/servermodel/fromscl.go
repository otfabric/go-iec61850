// SPDX-License-Identifier: MIT

package servermodel

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Options control how [FromSCLWithOptions] builds a model.
type Options struct {
	// InstanceDomainNames names each logical device (MMS domain) by its
	// LDevice@inst alone ("InteropLD"), as go-iec61850 did before v1.7.0.
	// By default a logical device has its IEC 61850-8-1 name, as the
	// other IEC 61850 stacks give it: LDevice@ldName when the SCL has
	// one, otherwise the IED name followed by the instance
	// ("InteropIEDInteropLD"). Data set members, and the references the
	// server builds from the domain (data sets, report, GOOSE and SV
	// control blocks), follow the name.
	InstanceDomainNames bool
}

// FromSCL builds a server [Model] from the first Server element found
// in the specified IED/AccessPoint of the parsed SCL. If apName is
// empty, the first AccessPoint with a Server is used.
//
// Type information (DOType, DAType, EnumType) is resolved from the
// SCL DataTypeTemplates to produce fully expanded data objects with
// their attribute hierarchies. Logical devices get their IEC 61850-8-1
// names (see [Options]).
func FromSCL(s *scl.SCL, iedName, apName string) (*Model, error) {
	return FromSCLWithOptions(s, iedName, apName, Options{})
}

// FromSCLWithOptions is [FromSCL] with options.
func FromSCLWithOptions(s *scl.SCL, iedName, apName string, opts Options) (*Model, error) {
	if s == nil {
		return nil, fmt.Errorf("servermodel: nil SCL")
	}

	ied := s.FindIED(iedName)
	if ied == nil {
		return nil, fmt.Errorf("servermodel: IED %q not found", iedName)
	}

	srv, apUsed, err := findServer(ied, apName)
	if err != nil {
		return nil, fmt.Errorf("servermodel: %w", err)
	}
	if srv == nil {
		if apName != "" {
			return nil, fmt.Errorf("servermodel: no Server in IED %q AccessPoint %q", iedName, apName)
		}
		return nil, fmt.Errorf("servermodel: no Server in IED %q", iedName)
	}

	// Type templates by ID. Duplicate IDs (last one wins) are reported
	// by scl/validate, not here.
	tmpl, _ := index.Build(s)
	dotIdx, datIdx, enumIdx := tmpl.DOTypes, tmpl.DATypes, tmpl.EnumTypes

	m := &Model{}
	var warnings []string
	gse, smv := gseIndex(s, iedName, apUsed)
	for _, sclLD := range srv.LDevices {
		ld, err := convertLD(s, &sclLD, dotIdx, datIdx, enumIdx, gse, smv, &warnings)
		if err != nil {
			return nil, fmt.Errorf("servermodel: LD %q: %w", sclLD.Inst, err)
		}
		m.LogicalDevices = append(m.LogicalDevices, *ld)
	}
	m.Warnings = warnings

	if !opts.InstanceDomainNames {
		if err := standardDomainNames(m, ied.Name, srv.LDevices); err != nil {
			return nil, fmt.Errorf("servermodel: %w", err)
		}
	}
	return m, nil
}

// standardDomainNames renames the logical devices of m to their
// IEC 61850-8-1 names (LDevice@ldName, else the IED name followed by the
// instance) and points the data set members at them.
func standardDomainNames(m *Model, iedName string, lds []scl.LDevice) error {
	names := make(map[string]string, len(lds))
	used := make(map[string]string, len(lds))
	for i := range lds {
		name := lds[i].LdName
		if name == "" {
			name = iedName + lds[i].Inst
		}
		if other, dup := used[name]; dup {
			return fmt.Errorf("LDs %q and %q both have the domain name %q", other, lds[i].Inst, name)
		}
		used[name] = lds[i].Inst
		names[lds[i].Inst] = name
	}
	for i := range m.LogicalDevices {
		ld := &m.LogicalDevices[i]
		ld.Name = names[ld.Inst]
		for j := range ld.LogicalNodes {
			for k := range ld.LogicalNodes[j].Logs {
				lg := &ld.LogicalNodes[j].Logs[k]
				if name, ok := names[lg.LogLD]; ok {
					lg.LogLD = name
				}
			}
			for k := range ld.LogicalNodes[j].DataSets {
				ds := &ld.LogicalNodes[j].DataSets[k]
				for n := range ds.Members {
					if name, ok := names[ds.Members[n].LDInst]; ok {
						ds.Members[n].LDInst = name
					}
				}
			}
		}
	}
	return nil
}

func findServer(ied *scl.IED, apName string) (*scl.Server, string, error) {
	if apName != "" {
		for i := range ied.AccessPoints {
			ap := &ied.AccessPoints[i]
			if ap.Name == apName && ap.Server != nil {
				return ap.Server, ap.Name, nil
			}
		}
		return nil, "", nil
	}
	var found *scl.Server
	var name string
	var count int
	for i := range ied.AccessPoints {
		ap := &ied.AccessPoints[i]
		if ap.Server != nil {
			found = ap.Server
			name = ap.Name
			count++
		}
	}
	if count > 1 {
		return nil, "", fmt.Errorf("IED %q has %d AccessPoints with Server elements; specify apName to disambiguate", ied.Name, count)
	}
	return found, name, nil
}

// gseIndex maps "ldInst/cbName" to the GSE and SMV addresses of the
// given IED/AccessPoint in the Communication section.
func gseIndex(s *scl.SCL, iedName, apName string) (map[string]*scl.GSEAddress, map[string]*scl.SMVAddress) {
	idx := map[string]*scl.GSEAddress{}
	smv := map[string]*scl.SMVAddress{}
	if s.Communication == nil {
		return idx, smv
	}
	for i := range s.Communication.SubNetworks {
		sn := &s.Communication.SubNetworks[i]
		for j := range sn.ConnectedAPs {
			cap := &sn.ConnectedAPs[j]
			if cap.IEDName != iedName || cap.APName != apName {
				continue
			}
			for k := range cap.GSEs {
				g := &cap.GSEs[k]
				idx[g.LDInst+"/"+g.CBName] = g
			}
			for k := range cap.SMVs {
				a := &cap.SMVs[k]
				smv[a.LDInst+"/"+a.CBName] = a
			}
		}
	}
	return idx, smv
}

func convertLD(s *scl.SCL, sclLD *scl.LDevice, dotIdx map[string]*scl.DOType, datIdx map[string]*scl.DAType, enumIdx map[string]*scl.EnumType, gse map[string]*scl.GSEAddress, smv map[string]*scl.SMVAddress, warnings *[]string) (*LogicalDevice, error) {
	ld := &LogicalDevice{Name: sclLD.Inst, Inst: sclLD.Inst}

	if sclLD.LN0 != nil {
		ln, err := convertLN(s, sclLD.LN0, dotIdx, datIdx, enumIdx, sclLD.Inst, warnings)
		if err != nil {
			return nil, fmt.Errorf("LLN0: %w", err)
		}
		for i := range sclLD.LN0.GSEControls {
			gc := &sclLD.LN0.GSEControls[i]
			if gc.Type != "" && gc.Type != "GOOSE" {
				continue // GSSE is not supported
			}
			ln.GoCBs = append(ln.GoCBs, convertGoCB(gc, gse[sclLD.Inst+"/"+gc.Name], sclLD.Inst+"/"+ln.Name, warnings))
		}
		for i := range sclLD.LN0.SMVControls {
			sc := &sclLD.LN0.SMVControls[i]
			if !sc.Multicast {
				// 6 Table 30: multicast="false" is a unicast (USVCB)
				// block (7-2 19.3), which is not implemented; serving
				// it as an MSVCB would misrepresent it.
				if warnings != nil {
					*warnings = append(*warnings, fmt.Sprintf("%s/%s: SampledValueControl %q is unicast (multicast=\"false\", USVCB): not supported, skipped", sclLD.Inst, ln.Name, sc.Name))
				}
				continue
			}
			ln.SvCBs = append(ln.SvCBs, convertSvCB(sc, smv[sclLD.Inst+"/"+sc.Name], sclLD.Inst+"/"+ln.Name, warnings))
		}
		ld.LogicalNodes = append(ld.LogicalNodes, *ln)
	}

	for i := range sclLD.LNs {
		ln, err := convertLN(s, &sclLD.LNs[i], dotIdx, datIdx, enumIdx, sclLD.Inst, warnings)
		if err != nil {
			return nil, fmt.Errorf("LN %s: %w", lnName(&sclLD.LNs[i]), err)
		}
		ld.LogicalNodes = append(ld.LogicalNodes, *ln)
	}
	setGroupInitialValues(ld, warnings)

	return ld, nil
}

func lnName(ln *scl.LN) string {
	return ln.Prefix + ln.LNClass + ln.Inst
}

func convertLN(s *scl.SCL, sclLN *scl.LN, dotIdx map[string]*scl.DOType, datIdx map[string]*scl.DAType, enumIdx map[string]*scl.EnumType, ldInst string, warnings *[]string) (*LogicalNode, error) {
	name := lnName(sclLN)
	ln := &LogicalNode{
		Name:    name,
		LNClass: sclLN.LNClass,
	}

	if sclLN.LNType == "" {
		return nil, fmt.Errorf("empty LNType")
	}
	lnt := findLNodeType(s, sclLN.LNType)
	if lnt == nil {
		return nil, fmt.Errorf("unresolved LNodeType %q", sclLN.LNType)
	}

	doNames := make(map[string]bool, len(lnt.DOs))
	for _, do := range lnt.DOs {
		doNames[do.Name] = true
		obj, err := expandDO(do, dotIdx, datIdx, enumIdx, nil, warnings)
		if err != nil {
			return nil, fmt.Errorf("DO %s: %w", do.Name, err)
		}
		prefix := ldInst + "/" + name + "." + do.Name
		applyDAIOverrides(&obj, sclLN.DOIs, prefix, warnings)
		ln.DataObjects = append(ln.DataObjects, obj)
	}

	if warnings != nil {
		for _, doi := range sclLN.DOIs {
			if !doNames[doi.Name] {
				*warnings = append(*warnings, fmt.Sprintf(
					"%s/%s: DOI %q does not match any DO in LNodeType %q",
					ldInst, name, doi.Name, sclLN.LNType))
			}
		}
	}

	for _, sclDS := range sclLN.DataSets {
		ln.DataSets = append(ln.DataSets, convertDataSet(&sclDS))
	}

	for _, sclRpt := range sclLN.Reports {
		ln.Reports = append(ln.Reports, expandReport(&sclRpt)...)
	}

	for _, sclLog := range sclLN.Logs {
		ln.Logs = append(ln.Logs, convertLog(&sclLog, ldInst))
	}

	if sclLN.SettingControl != nil {
		actSG := sclLN.SettingControl.ActSG
		if actSG == 0 {
			actSG = 1
		}
		ln.SettingGroup = &SettingGroupDef{
			NumOfSGs: sclLN.SettingControl.NumOfSGs,
			ActSG:    actSG,
			ResvTms:  sclLN.SettingControl.ResvTms,
		}
	}

	return ln, nil
}

func findLNodeType(s *scl.SCL, id string) *scl.LNodeType {
	for i := range s.DataTypeTemplates.LNodeTypes {
		if s.DataTypeTemplates.LNodeTypes[i].ID == id {
			return &s.DataTypeTemplates.LNodeTypes[i]
		}
	}
	return nil
}

func expandDO(do scl.DO, dotIdx map[string]*scl.DOType, datIdx map[string]*scl.DAType, enumIdx map[string]*scl.EnumType, visited map[string]bool, warnings *[]string) (DataObject, error) {
	obj := DataObject{Name: do.Name}

	dot, ok := dotIdx[do.Type]
	if !ok {
		return obj, fmt.Errorf("unresolved DOType %q", do.Type)
	}
	obj.CDC = dot.CDC

	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[do.Type] {
		return obj, nil
	}
	visited[do.Type] = true
	defer delete(visited, do.Type)

	for _, sdo := range dot.SDOs {
		child, err := expandDO(scl.DO{Name: sdo.Name, Type: sdo.Type}, dotIdx, datIdx, enumIdx, visited, warnings)
		if err != nil {
			return obj, err
		}
		obj.Children = append(obj.Children, child)
	}

	for _, da := range dot.DAs {
		if da.Count > 1 && warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf(
				"DOType %q DA %q: count=%d will be registered as scalar (array expansion not supported)",
				do.Type, da.Name, da.Count))
		}
		attr, err := expandDA(da.Name, da.FC, da.BType, da.Type, da.Val, datIdx, enumIdx, nil, warnings)
		if err != nil {
			return obj, fmt.Errorf("DA %s: %w", da.Name, err)
		}
		setTrgOps(&attr, da.Dchg, da.Qchg, da.Dupd)
		obj.Attributes = append(obj.Attributes, attr)
	}
	addImpliedSG(&obj)

	return obj, nil
}

// setTrgOps sets the trigger options of a DA on it and all its
// sub-attributes: a BDA has no TrgOp of its own (IEC 61850-7-3 §5.5.2,
// IEC 61850-6 tBDA).
func setTrgOps(a *DataAttribute, dchg, qchg, dupd bool) {
	a.Dchg, a.Qchg, a.Dupd = dchg, qchg, dupd
	for i := range a.Children {
		setTrgOps(&a.Children[i], dchg, qchg, dupd)
	}
}

func expandDA(name, fc, btype, typeRef, val string, datIdx map[string]*scl.DAType, enumIdx map[string]*scl.EnumType, visited map[string]bool, warnings *[]string) (DataAttribute, error) {
	attr := DataAttribute{
		Name:         name,
		FC:           fc,
		BType:        btype,
		InitialValue: val,
	}

	if btype == "Enum" && typeRef != "" && enumIdx != nil {
		if et, ok := enumIdx[typeRef]; ok {
			attr.EnumValues = make([]int, len(et.Vals))
			attr.EnumNames = make(map[string]int, len(et.Vals))
			for i, ev := range et.Vals {
				attr.EnumValues[i] = ev.Ord
				attr.EnumNames[ev.Value] = ev.Ord
			}
		}
	}

	if btype != "Struct" {
		return attr, nil
	}
	if typeRef == "" {
		return attr, fmt.Errorf("struct DA %q has no type reference", name)
	}

	dat, ok := datIdx[typeRef]
	if !ok {
		return attr, fmt.Errorf("unresolved DAType %q for DA %q", typeRef, name)
	}

	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[typeRef] {
		return attr, nil
	}
	visited[typeRef] = true
	defer delete(visited, typeRef)

	for _, bda := range dat.BDAs {
		if bda.Count > 1 && warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf(
				"DAType %q BDA %q: count=%d will be registered as scalar (array expansion not supported)",
				typeRef, bda.Name, bda.Count))
		}
		child, err := expandDA(bda.Name, fc, bda.BType, bda.Type, bda.Val, datIdx, enumIdx, visited, warnings)
		if err != nil {
			return attr, err
		}
		attr.Children = append(attr.Children, child)
	}

	return attr, nil
}

func applyDAIOverrides(obj *DataObject, dois []scl.DOI, prefix string, warnings *[]string) {
	for _, doi := range dois {
		if doi.Name != obj.Name {
			continue
		}
		for _, dai := range doi.DAIs {
			if !setDAIValue(obj, dai.Name, dai) && warnings != nil {
				*warnings = append(*warnings, fmt.Sprintf(
					"%s: DAI %q does not match any attribute", prefix, dai.Name))
			}
		}
		for _, sdi := range doi.SDIs {
			applySDIOverrides(obj, &sdi, prefix, warnings)
		}
	}
}

func applySDIOverrides(obj *DataObject, sdi *scl.SDI, prefix string, warnings *[]string) {
	for i := range obj.Children {
		if obj.Children[i].Name == sdi.Name {
			childPrefix := prefix + "." + sdi.Name
			for _, dai := range sdi.DAIs {
				if !setDAIValue(&obj.Children[i], dai.Name, dai) && warnings != nil {
					*warnings = append(*warnings, fmt.Sprintf(
						"%s: DAI %q does not match any attribute", childPrefix, dai.Name))
				}
			}
			for _, child := range sdi.SDIs {
				applySDIOverrides(&obj.Children[i], &child, childPrefix, warnings)
			}
			return
		}
	}
	for i := range obj.Attributes {
		if obj.Attributes[i].Name == sdi.Name {
			attrPrefix := prefix + "." + sdi.Name
			for _, dai := range sdi.DAIs {
				if !setDAIValueOnAttr(&obj.Attributes[i], dai.Name, dai) && warnings != nil {
					*warnings = append(*warnings, fmt.Sprintf(
						"%s: DAI %q does not match any child", attrPrefix, dai.Name))
				}
			}
			for _, child := range sdi.SDIs {
				applySDIOnAttr(&obj.Attributes[i], &child, attrPrefix, warnings)
			}
			return
		}
	}
	if warnings != nil {
		*warnings = append(*warnings, fmt.Sprintf(
			"%s: SDI %q does not match any child or attribute", prefix, sdi.Name))
	}
}

func applySDIOnAttr(attr *DataAttribute, sdi *scl.SDI, prefix string, warnings *[]string) {
	for i := range attr.Children {
		if attr.Children[i].Name == sdi.Name {
			childPrefix := prefix + "." + sdi.Name
			for _, dai := range sdi.DAIs {
				if !setDAIValueOnAttr(&attr.Children[i], dai.Name, dai) && warnings != nil {
					*warnings = append(*warnings, fmt.Sprintf(
						"%s: DAI %q does not match any child", childPrefix, dai.Name))
				}
			}
			for _, child := range sdi.SDIs {
				applySDIOnAttr(&attr.Children[i], &child, childPrefix, warnings)
			}
			return
		}
	}
	if warnings != nil {
		*warnings = append(*warnings, fmt.Sprintf(
			"%s: SDI %q does not match any child", prefix, sdi.Name))
	}
}

// setDAIValue sets the value of DAI attrName on the attributes of obj
// with that name. A DAI has no FC, so a setting declared under both SG
// and SE (IEC 61850-6 Table 47) gets the value in both.
func setDAIValue(obj *DataObject, attrName string, dai scl.DAI) bool {
	parts := strings.SplitN(attrName, ".", 2)
	found := false
	for i := range obj.Attributes {
		if obj.Attributes[i].Name != parts[0] {
			continue
		}
		if len(parts) == 1 {
			applyDAIVals(&obj.Attributes[i], dai)
			found = true
		} else if setDAIValueOnAttr(&obj.Attributes[i], parts[1], dai) {
			found = true
		}
	}
	return found
}

func setDAIValueOnAttr(attr *DataAttribute, path string, dai scl.DAI) bool {
	parts := strings.SplitN(path, ".", 2)
	for i := range attr.Children {
		if attr.Children[i].Name == parts[0] {
			if len(parts) == 1 {
				applyDAIVals(&attr.Children[i], dai)
			} else {
				return setDAIValueOnAttr(&attr.Children[i], parts[1], dai)
			}
			return true
		}
	}
	return false
}

// applyDAIVals sets the value of a DAI on attr. A Val with sGroup is the
// value of a setting in that setting group (IEC 61850-6 9.5.4.4); a Val
// without sGroup applies to every group. The first Val stays the
// InitialValue, as before setting groups had value banks, until
// setGroupInitialValues picks the active group's.
func applyDAIVals(attr *DataAttribute, dai scl.DAI) {
	var groups map[uint8]string
	plain, hasPlain := "", false
	for _, v := range dai.Vals {
		switch {
		case v.SGroup == 0:
			if !hasPlain {
				plain, hasPlain = v.Value, true
			}
		case v.SGroup <= 255:
			if groups == nil {
				groups = map[uint8]string{}
			}
			if _, dup := groups[uint8(v.SGroup)]; !dup {
				groups[uint8(v.SGroup)] = v.Value
			}
		}
	}
	if groups != nil {
		if hasPlain {
			groups[0] = plain
		} else {
			// No Val for all groups: the others keep the type's value.
			groups[0] = attr.InitialValue
		}
		attr.GroupValues = groups
	}
	attr.InitialValue = dai.Val
	if hasPlain {
		attr.InitialValue = plain
	}
	attr.Overridden = true
}

// addImpliedSG adds the FC=SG attribute that an FC=SE attribute implies
// (IEC 61850-6 Table 47: "fc=SE always also implies fc=SG"), so the
// active group's value can be read when the SCL declares only SE.
func addImpliedSG(obj *DataObject) {
	has := map[string]bool{}
	for _, a := range obj.Attributes {
		if a.FC == "SG" {
			has[a.Name] = true
		}
	}
	for i := range len(obj.Attributes) {
		a := obj.Attributes[i]
		if a.FC != "SE" || has[a.Name] {
			continue
		}
		sg := cloneAttr(a)
		setFC(&sg, "SG")
		obj.Attributes = append(obj.Attributes, sg)
		has[a.Name] = true
	}
}

func cloneAttr(a DataAttribute) DataAttribute {
	if a.Children != nil {
		ch := make([]DataAttribute, len(a.Children))
		for i := range a.Children {
			ch[i] = cloneAttr(a.Children[i])
		}
		a.Children = ch
	}
	return a
}

func setFC(a *DataAttribute, fc string) {
	a.FC = fc
	for i := range a.Children {
		setFC(&a.Children[i], fc)
	}
}

// setGroupInitialValues makes the active group's value (SettingControl
// actSG) the InitialValue of every setting of the LD that has values per
// group, and reports sGroup values beyond numOfSGs (IEC 61850-6 9.5.4.4:
// the sGroup should be checked against numOfSGs).
func setGroupInitialValues(ld *LogicalDevice, warnings *[]string) {
	var sg *SettingGroupDef
	for i := range ld.LogicalNodes {
		if ld.LogicalNodes[i].SettingGroup != nil {
			sg = ld.LogicalNodes[i].SettingGroup
		}
	}
	if sg == nil {
		return
	}
	var walk func(prefix string, a *DataAttribute)
	walk = func(prefix string, a *DataAttribute) {
		name := prefix + "." + a.Name
		for i := range a.Children {
			walk(name, &a.Children[i])
		}
		if a.GroupValues == nil {
			return
		}
		for g := range a.GroupValues {
			if g > sg.NumOfSGs && warnings != nil {
				*warnings = append(*warnings, fmt.Sprintf("%s: Val sGroup=%d exceeds numOfSGs %d: ignored", name, g, sg.NumOfSGs))
			}
		}
		if v, ok := a.GroupValues[sg.ActSG]; ok {
			a.InitialValue = v
		} else {
			a.InitialValue = a.GroupValues[0]
		}
	}
	var walkDO func(prefix string, o *DataObject)
	walkDO = func(prefix string, o *DataObject) {
		name := prefix + "." + o.Name
		for i := range o.Children {
			walkDO(name, &o.Children[i])
		}
		for i := range o.Attributes {
			walk(name, &o.Attributes[i])
		}
	}
	for i := range ld.LogicalNodes {
		ln := &ld.LogicalNodes[i]
		for j := range ln.DataObjects {
			walkDO(ld.Inst+"/"+ln.Name, &ln.DataObjects[j])
		}
	}
}

func convertDataSet(ds *scl.DataSet) DataSetDef {
	def := DataSetDef{Name: ds.Name}
	for _, fcda := range ds.FCDAs {
		m := DataSetMemberDef{
			LDInst: fcda.LDInst,
			LNName: fcda.Prefix + fcda.LNClass + fcda.LNInst,
			FC:     fcda.FC,
		}
		doPath := fcda.DOName
		if fcda.DAName != "" {
			doPath += "." + fcda.DAName
		}
		m.DOPath = doPath
		def.Members = append(def.Members, m)
	}
	return def
}

// expandReport returns the instances of an SCL ReportControl: with
// indexed, instances named after the control block with a two-digit
// suffix from 01 up to RptEnabled max (at most 99); without, one instance
// named as the control block (IEC 61850-8-1 §17.1.2 and §17.1.3.2,
// IEC 61850-6 §9.3.8).
func expandReport(rpt *scl.ReportControl) []ReportDef {
	base := convertReport(rpt)
	if !rpt.Indexed {
		return []ReportDef{base}
	}
	n := max(rpt.MaxInstances, 1)
	n = min(n, 99)
	out := make([]ReportDef, n)
	for i := range out {
		out[i] = base
		out[i].Name = fmt.Sprintf("%s%02d", rpt.Name, i+1)
	}
	return out
}

// convertLog maps an SCL LogControl onto an LCB definition. The log is
// "<prefix><lnClass><lnInst>" (default LLN0) in LD ldInst (default the LD
// of the control block) (IEC 61850-6 §9.3.9 Table 26). LogLD holds the LD
// instance until standardDomainNames maps it to the domain name.
func convertLog(lg *scl.Log, ldInst string) LogDef {
	def := LogDef{
		Name:       lg.Name,
		LogName:    lg.LogName,
		LogLD:      lg.LDInst,
		DatSet:     lg.DatSet,
		IntgPd:     lg.IntgPd,
		LogEna:     lg.LogEna,
		ReasonCode: lg.ReasonCode,
		TrgOps: TrgOpsDef{
			Dchg:   lg.TrgOps.Dchg,
			Qchg:   lg.TrgOps.Qchg,
			Dupd:   lg.TrgOps.Dupd,
			Period: lg.TrgOps.Period,
			GI:     lg.TrgOps.GI,
		},
	}
	if def.LogLD == "" {
		def.LogLD = ldInst
	}
	lnClass := lg.LNClass
	if lnClass == "" {
		lnClass = "LLN0"
	}
	def.LogLN = lg.Prefix + lnClass + lg.LNInst
	if def.LogName == "" {
		// Without a configured log name the default journal is
		// GeneralLog (IEC 61850-8-1 §17.3.3.3.2).
		def.LogName = "GeneralLog"
	}
	return def
}

func convertReport(rpt *scl.ReportControl) ReportDef {
	def := ReportDef{
		Name:     rpt.Name,
		RptID:    rpt.RptID,
		DatSet:   rpt.DatSet,
		ConfRev:  rpt.ConfRev,
		Buffered: rpt.Buffered,
		BufTime:  rpt.BufTime,
		IntgPd:   rpt.IntgPd,
		TrgOps: TrgOpsDef{
			Dchg:   rpt.TrgOps.Dchg,
			Qchg:   rpt.TrgOps.Qchg,
			Dupd:   rpt.TrgOps.Dupd,
			Period: rpt.TrgOps.Period,
			GI:     rpt.TrgOps.GI,
		},
		OptFlds: OptFieldsDef{
			SeqNum:     rpt.OptFields.SeqNum,
			TimeStamp:  rpt.OptFields.TimeStamp,
			DataSet:    rpt.OptFields.DataSet,
			ReasonCode: rpt.OptFields.ReasonCode,
			DataRef:    rpt.OptFields.DataRef,
			EntryID:    rpt.OptFields.EntryID,
			ConfigRef:  rpt.OptFields.ConfigRef,
			BufOvfl:    rpt.OptFields.BufOvfl,
		},
	}
	return def
}
