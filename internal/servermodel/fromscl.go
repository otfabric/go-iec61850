package servermodel

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-iec61850/scl"
)

// FromSCL builds a server [Model] from the first Server element found
// in the specified IED/AccessPoint of the parsed SCL. If apName is
// empty, the first AccessPoint with a Server is used.
//
// Type information (DOType, DAType, EnumType) is resolved from the
// SCL DataTypeTemplates to produce fully expanded data objects with
// their attribute hierarchies.
func FromSCL(s *scl.SCL, iedName, apName string) (*Model, error) {
	if s == nil {
		return nil, fmt.Errorf("servermodel: nil SCL")
	}

	ied := s.FindIED(iedName)
	if ied == nil {
		return nil, fmt.Errorf("servermodel: IED %q not found", iedName)
	}

	srv, err := findServer(ied, apName)
	if err != nil {
		return nil, fmt.Errorf("servermodel: %w", err)
	}
	if srv == nil {
		if apName != "" {
			return nil, fmt.Errorf("servermodel: no Server in IED %q AccessPoint %q", iedName, apName)
		}
		return nil, fmt.Errorf("servermodel: no Server in IED %q", iedName)
	}

	dotIdx := indexDOTypes(s)
	datIdx := indexDATypes(s)
	enumIdx := indexEnumTypes(s)

	m := &Model{}
	var warnings []string
	for _, sclLD := range srv.LDevices {
		ld, err := convertLD(s, &sclLD, dotIdx, datIdx, enumIdx, &warnings)
		if err != nil {
			return nil, fmt.Errorf("servermodel: LD %q: %w", sclLD.Inst, err)
		}
		m.LogicalDevices = append(m.LogicalDevices, *ld)
	}
	m.Warnings = warnings

	return m, nil
}

func findServer(ied *scl.IED, apName string) (*scl.Server, error) {
	if apName != "" {
		for i := range ied.AccessPoints {
			ap := &ied.AccessPoints[i]
			if ap.Name == apName && ap.Server != nil {
				return ap.Server, nil
			}
		}
		return nil, nil
	}
	var found *scl.Server
	var count int
	for i := range ied.AccessPoints {
		ap := &ied.AccessPoints[i]
		if ap.Server != nil {
			found = ap.Server
			count++
		}
	}
	if count > 1 {
		return nil, fmt.Errorf("IED %q has %d AccessPoints with Server elements; specify apName to disambiguate", ied.Name, count)
	}
	return found, nil
}

func indexDOTypes(s *scl.SCL) map[string]*scl.DOType {
	idx := make(map[string]*scl.DOType, len(s.DataTypeTemplates.DOTypes))
	for i := range s.DataTypeTemplates.DOTypes {
		dot := &s.DataTypeTemplates.DOTypes[i]
		idx[dot.ID] = dot
	}
	return idx
}

func indexDATypes(s *scl.SCL) map[string]*scl.DAType {
	idx := make(map[string]*scl.DAType, len(s.DataTypeTemplates.DATypes))
	for i := range s.DataTypeTemplates.DATypes {
		dat := &s.DataTypeTemplates.DATypes[i]
		idx[dat.ID] = dat
	}
	return idx
}

func indexEnumTypes(s *scl.SCL) map[string]*scl.EnumType {
	idx := make(map[string]*scl.EnumType, len(s.DataTypeTemplates.EnumTypes))
	for i := range s.DataTypeTemplates.EnumTypes {
		et := &s.DataTypeTemplates.EnumTypes[i]
		idx[et.ID] = et
	}
	return idx
}

func convertLD(s *scl.SCL, sclLD *scl.LDevice, dotIdx map[string]*scl.DOType, datIdx map[string]*scl.DAType, enumIdx map[string]*scl.EnumType, warnings *[]string) (*LogicalDevice, error) {
	ld := &LogicalDevice{Name: sclLD.Inst}

	if sclLD.LN0 != nil {
		ln, err := convertLN(s, sclLD.LN0, dotIdx, datIdx, enumIdx, sclLD.Inst, warnings)
		if err != nil {
			return nil, fmt.Errorf("LLN0: %w", err)
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
		ln.Reports = append(ln.Reports, convertReport(&sclRpt))
	}

	for _, sclLog := range sclLN.Logs {
		ln.Logs = append(ln.Logs, LogDef{Name: sclLog.Name})
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
		obj.Attributes = append(obj.Attributes, attr)
	}

	return obj, nil
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
			for i, ev := range et.Vals {
				attr.EnumValues[i] = ev.Ord
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
			if !setDAIValue(obj, dai.Name, dai.Val) && warnings != nil {
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
				if !setDAIValue(&obj.Children[i], dai.Name, dai.Val) && warnings != nil {
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
				if !setDAIValueOnAttr(&obj.Attributes[i], dai.Name, dai.Val) && warnings != nil {
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
				if !setDAIValueOnAttr(&attr.Children[i], dai.Name, dai.Val) && warnings != nil {
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

func setDAIValue(obj *DataObject, attrName, val string) bool {
	parts := strings.SplitN(attrName, ".", 2)
	for i := range obj.Attributes {
		if obj.Attributes[i].Name == parts[0] {
			if len(parts) == 1 {
				obj.Attributes[i].InitialValue = val
				obj.Attributes[i].Overridden = true
			} else {
				return setDAIValueOnAttr(&obj.Attributes[i], parts[1], val)
			}
			return true
		}
	}
	return false
}

func setDAIValueOnAttr(attr *DataAttribute, path, val string) bool {
	parts := strings.SplitN(path, ".", 2)
	for i := range attr.Children {
		if attr.Children[i].Name == parts[0] {
			if len(parts) == 1 {
				attr.Children[i].InitialValue = val
				attr.Children[i].Overridden = true
			} else {
				return setDAIValueOnAttr(&attr.Children[i], parts[1], val)
			}
			return true
		}
	}
	return false
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
