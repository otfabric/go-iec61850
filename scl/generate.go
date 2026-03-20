package scl

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

// Generate writes an SCL model as XML to the given writer.
//
// The output is deterministic: elements appear in the order they are
// stored in the model slices. Round-trip loss is minimal for the
// supported SCL subset (Header, Substation, Communication, IED,
// DataTypeTemplates).
func Generate(w io.Writer, s *SCL) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("scl: generate: %w", err)
	}

	raw := modelToGenXML(s)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(raw); err != nil {
		return fmt.Errorf("scl: generate: %w", err)
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("scl: generate: %w", err)
	}
	return nil
}

// --- XML serialization types (local to Generate) ---

type genSCL struct {
	XMLName           xml.Name             `xml:"SCL"`
	XMLNS             string               `xml:"xmlns,attr"`
	Version           string               `xml:"version,attr,omitempty"`
	Revision          string               `xml:"revision,attr,omitempty"`
	Release           string               `xml:"release,attr,omitempty"`
	Header            genHeader            `xml:"Header"`
	Substations       []genSubstation      `xml:"Substation"`
	Communication     *genCommunication    `xml:"Communication"`
	IEDs              []genIED             `xml:"IED"`
	DataTypeTemplates genDataTypeTemplates `xml:"DataTypeTemplates"`
}

type genHeader struct {
	ID       string `xml:"id,attr"`
	Version  string `xml:"version,attr"`
	Revision string `xml:"revision,attr"`
}

type genIED struct {
	Name         string           `xml:"name,attr"`
	Desc         string           `xml:"desc,attr"`
	Manufacturer string           `xml:"manufacturer,attr"`
	Type         string           `xml:"type,attr"`
	Services     *genServices     `xml:"Services"`
	AccessPoints []genAccessPoint `xml:"AccessPoint"`
}

type genSubstation struct {
	Name          string            `xml:"name,attr"`
	Desc          string            `xml:"desc,attr"`
	VoltageLevels []genVoltageLevel `xml:"VoltageLevel"`
}

type genVoltageLevel struct {
	Name    string   `xml:"name,attr"`
	Desc    string   `xml:"desc,attr"`
	Voltage *genVal  `xml:"Voltage"`
	Bays    []genBay `xml:"Bay"`
}

type genVal struct {
	Value string `xml:",chardata"`
}

type genBay struct {
	Name                 string                   `xml:"name,attr"`
	Desc                 string                   `xml:"desc,attr"`
	ConductingEquipments []genConductingEquipment `xml:"ConductingEquipment"`
}

type genConductingEquipment struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Desc string `xml:"desc,attr"`
}

type genCommunication struct {
	SubNetworks []genSubNetwork `xml:"SubNetwork"`
}

type genSubNetwork struct {
	Name         string           `xml:"name,attr"`
	Desc         string           `xml:"desc,attr"`
	Type         string           `xml:"type,attr"`
	ConnectedAPs []genConnectedAP `xml:"ConnectedAP"`
}

type genConnectedAP struct {
	IEDName string      `xml:"iedName,attr"`
	APName  string      `xml:"apName,attr"`
	Address *genAddress `xml:"Address"`
	GSEs    []genGSE    `xml:"GSE"`
	SMVs    []genSMV    `xml:"SMV"`
}

type genAddress struct {
	Ps []genP `xml:"P"`
}

type genP struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type genGSE struct {
	LDInst  string      `xml:"ldInst,attr"`
	CBName  string      `xml:"cbName,attr"`
	Address *genAddress `xml:"Address"`
	MinTime *genVal     `xml:"MinTime"`
	MaxTime *genVal     `xml:"MaxTime"`
}

type genSMV struct {
	LDInst  string      `xml:"ldInst,attr"`
	CBName  string      `xml:"cbName,attr"`
	Address *genAddress `xml:"Address"`
}

type genServices struct {
	DynAssociation          *struct{} `xml:"DynAssociation"`
	GetDirectory            *struct{} `xml:"GetDirectory"`
	GetDataObjectDefinition *struct{} `xml:"GetDataObjectDefinition"`
	GetDataSetValue         *struct{} `xml:"GetDataSetValue"`
	DataSetDirectory        *struct{} `xml:"DataSetDirectory"`
	ReadWrite               *struct{} `xml:"ReadWrite"`
	GetCBValues             *struct{} `xml:"GetCBValues"`
	FileHandling            *struct{} `xml:"FileHandling"`

	ConfDataSet    *genConfDataSet       `xml:"ConfDataSet"`
	ConfReportCtrl *genConfReportControl `xml:"ConfReportControl"`
	ReportSettings *genReportSettings    `xml:"ReportSettings"`
	ConfLNs        *genConfLNs           `xml:"ConfLNs"`
	GOOSE          *genGOOSEService      `xml:"GOOSE"`
	SMVsc          *genSMVService        `xml:"SMVsc"`
}

type genConfDataSet struct {
	Max           string `xml:"max,attr"`
	MaxAttributes string `xml:"maxAttributes,attr"`
	Modify        string `xml:"modify,attr"`
}

type genConfReportControl struct {
	Max     string `xml:"max,attr"`
	BufMode string `xml:"bufMode,attr"`
}

type genReportSettings struct {
	CBName    string `xml:"cbName,attr"`
	DatSet    string `xml:"datSet,attr"`
	RptID     string `xml:"rptID,attr"`
	OptFields string `xml:"optFields,attr"`
	BufTime   string `xml:"bufTime,attr"`
	TrgOps    string `xml:"trgOps,attr"`
	IntgPd    string `xml:"intgPd,attr"`
}

type genConfLNs struct {
	FixPrefix string `xml:"fixPrefix,attr"`
	FixLnInst string `xml:"fixLnInst,attr"`
}

type genGOOSEService struct {
	Max       string `xml:"max,attr"`
	FixedOffs string `xml:"fixedOffs,attr"`
}

type genSMVService struct {
	Max string `xml:"max,attr"`
}

type genAccessPoint struct {
	Name   string     `xml:"name,attr"`
	Server *genServer `xml:"Server"`
}

type genServer struct {
	LDevices []genLDevice `xml:"LDevice"`
}

type genLDevice struct {
	Inst string  `xml:"inst,attr"`
	Desc string  `xml:"desc,attr"`
	LN0  *genLN  `xml:"LN0"`
	LNs  []genLN `xml:"LN"`
}

type genLN struct {
	Prefix  string `xml:"prefix,attr"`
	LNClass string `xml:"lnClass,attr"`
	Inst    string `xml:"inst,attr"`
	LNType  string `xml:"lnType,attr"`
	Desc    string `xml:"desc,attr"`

	DOIs     []genDOI           `xml:"DOI"`
	DataSets []genDataSet       `xml:"DataSet"`
	Reports  []genReportControl `xml:"ReportControl"`
	Logs     []genLog           `xml:"LogControl"`
}

type genDOI struct {
	Name string   `xml:"name,attr"`
	Desc string   `xml:"desc,attr"`
	DAIs []genDAI `xml:"DAI"`
	SDIs []genSDI `xml:"SDI"`
}

type genSDI struct {
	Name string   `xml:"name,attr"`
	DAIs []genDAI `xml:"DAI"`
	SDIs []genSDI `xml:"SDI"`
}

type genDAI struct {
	Name  string `xml:"name,attr"`
	SAddr string `xml:"sAddr,attr"`
	Val   string `xml:"Val"`
}

type genDataSet struct {
	Name  string    `xml:"name,attr"`
	Desc  string    `xml:"desc,attr"`
	FCDAs []genFCDA `xml:"FCDA"`
}

type genFCDA struct {
	LDInst  string `xml:"ldInst,attr"`
	Prefix  string `xml:"prefix,attr"`
	LNClass string `xml:"lnClass,attr"`
	LNInst  string `xml:"lnInst,attr"`
	DOName  string `xml:"doName,attr"`
	DAName  string `xml:"daName,attr"`
	FC      string `xml:"fc,attr"`
}

type genReportControl struct {
	Name      string        `xml:"name,attr"`
	Desc      string        `xml:"desc,attr"`
	RptID     string        `xml:"rptID,attr"`
	DatSet    string        `xml:"datSet,attr"`
	ConfRev   string        `xml:"confRev,attr"`
	Buffered  string        `xml:"buffered,attr"`
	BufTime   string        `xml:"bufTime,attr"`
	IntgPd    string        `xml:"intgPd,attr"`
	TrgOps    *genTrgOps    `xml:"TrgOps"`
	OptFields *genOptFields `xml:"OptFields"`
}

type genTrgOps struct {
	Dchg   string `xml:"dchg,attr"`
	Qchg   string `xml:"qchg,attr"`
	Dupd   string `xml:"dupd,attr"`
	Period string `xml:"period,attr"`
	GI     string `xml:"gi,attr"`
}

type genOptFields struct {
	SeqNum     string `xml:"seqNum,attr"`
	TimeStamp  string `xml:"timeStamp,attr"`
	DataSet    string `xml:"dataSet,attr"`
	ReasonCode string `xml:"reasonCode,attr"`
	DataRef    string `xml:"dataRef,attr"`
	EntryID    string `xml:"entryID,attr"`
	ConfigRef  string `xml:"configRef,attr"`
	BufOvfl    string `xml:"bufOvfl,attr"`
}

type genLog struct {
	Name string `xml:"name,attr"`
	Desc string `xml:"desc,attr"`
}

type genDataTypeTemplates struct {
	LNodeTypes []genLNodeType `xml:"LNodeType"`
	DOTypes    []genDOType    `xml:"DOType"`
	DATypes    []genDAType    `xml:"DAType"`
	EnumTypes  []genEnumType  `xml:"EnumType"`
}

type genLNodeType struct {
	ID      string  `xml:"id,attr"`
	LNClass string  `xml:"lnClass,attr"`
	Desc    string  `xml:"desc,attr"`
	DOs     []genDO `xml:"DO"`
}

type genDO struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Desc string `xml:"desc,attr"`
}

type genDOType struct {
	ID   string   `xml:"id,attr"`
	CDC  string   `xml:"cdc,attr"`
	Desc string   `xml:"desc,attr"`
	DAs  []genDA  `xml:"DA"`
	SDOs []genSDO `xml:"SDO"`
}

type genSDO struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Desc string `xml:"desc,attr"`
}

type genDA struct {
	Name  string `xml:"name,attr"`
	FC    string `xml:"fc,attr"`
	BType string `xml:"bType,attr"`
	Type  string `xml:"type,attr"`
	Desc  string `xml:"desc,attr"`
	Count string `xml:"count,attr"`
	Val   string `xml:"Val"`
}

type genDAType struct {
	ID   string   `xml:"id,attr"`
	Desc string   `xml:"desc,attr"`
	BDAs []genBDA `xml:"BDA"`
}

type genBDA struct {
	Name  string `xml:"name,attr"`
	BType string `xml:"bType,attr"`
	Type  string `xml:"type,attr"`
	Desc  string `xml:"desc,attr"`
	Count string `xml:"count,attr"`
	Val   string `xml:"Val"`
}

type genEnumType struct {
	ID   string       `xml:"id,attr"`
	Desc string       `xml:"desc,attr"`
	Vals []genEnumVal `xml:"EnumVal"`
}

type genEnumVal struct {
	Ord  string `xml:"ord,attr"`
	Desc string `xml:",chardata"`
}

// --- Conversion helpers (model → gen*) ---

func modelToGenXML(s *SCL) *genSCL {
	raw := &genSCL{
		XMLNS:    "http://www.iec.ch/61850/2003/SCL",
		Version:  "2007",
		Revision: "B",
		Header: genHeader{
			ID:       s.Header.ID,
			Version:  s.Header.Version,
			Revision: s.Header.Revision,
		},
	}

	for _, sub := range s.Substations {
		raw.Substations = append(raw.Substations, substationToGenXML(sub))
	}

	if s.Communication != nil {
		raw.Communication = communicationToGenXML(s.Communication)
	}

	for _, ied := range s.IEDs {
		raw.IEDs = append(raw.IEDs, iedToGenXML(ied))
	}

	raw.DataTypeTemplates = dttToGenXML(s.DataTypeTemplates)

	return raw
}

func substationToGenXML(sub Substation) genSubstation {
	raw := genSubstation{Name: sub.Name, Desc: sub.Desc}
	for _, vl := range sub.VoltageLevels {
		rawVL := genVoltageLevel{Name: vl.Name, Desc: vl.Desc}
		if vl.Voltage != "" {
			rawVL.Voltage = &genVal{Value: vl.Voltage}
		}
		for _, bay := range vl.Bays {
			rawBay := genBay{Name: bay.Name, Desc: bay.Desc}
			for _, ce := range bay.ConductingEquipments {
				rawBay.ConductingEquipments = append(rawBay.ConductingEquipments, genConductingEquipment(ce))
			}
			rawVL.Bays = append(rawVL.Bays, rawBay)
		}
		raw.VoltageLevels = append(raw.VoltageLevels, rawVL)
	}
	return raw
}

func communicationToGenXML(comm *Communication) *genCommunication {
	raw := &genCommunication{}
	for _, sn := range comm.SubNetworks {
		rawSN := genSubNetwork{Name: sn.Name, Desc: sn.Desc, Type: sn.Type}
		for _, cap := range sn.ConnectedAPs {
			rawAP := genConnectedAP{IEDName: cap.IEDName, APName: cap.APName}
			if len(cap.Address) > 0 {
				rawAP.Address = &genAddress{Ps: psToGenXML(cap.Address)}
			}
			for _, gse := range cap.GSEs {
				rawGSE := genGSE{LDInst: gse.LDInst, CBName: gse.CBName}
				if len(gse.Address) > 0 {
					rawGSE.Address = &genAddress{Ps: psToGenXML(gse.Address)}
				}
				if gse.MinTime != "" {
					rawGSE.MinTime = &genVal{Value: gse.MinTime}
				}
				if gse.MaxTime != "" {
					rawGSE.MaxTime = &genVal{Value: gse.MaxTime}
				}
				rawAP.GSEs = append(rawAP.GSEs, rawGSE)
			}
			for _, smv := range cap.SMVs {
				rawSMV := genSMV{LDInst: smv.LDInst, CBName: smv.CBName}
				if len(smv.Address) > 0 {
					rawSMV.Address = &genAddress{Ps: psToGenXML(smv.Address)}
				}
				rawAP.SMVs = append(rawAP.SMVs, rawSMV)
			}
			rawSN.ConnectedAPs = append(rawSN.ConnectedAPs, rawAP)
		}
		raw.SubNetworks = append(raw.SubNetworks, rawSN)
	}
	return raw
}

func psToGenXML(ps []P) []genP {
	raw := make([]genP, len(ps))
	for i, p := range ps {
		raw[i] = genP(p)
	}
	return raw
}

func iedToGenXML(ied IED) genIED {
	raw := genIED{
		Name:         ied.Name,
		Desc:         ied.Desc,
		Manufacturer: ied.Manufacturer,
		Type:         ied.Type,
	}
	if ied.Services != nil {
		raw.Services = servicesToGenXML(ied.Services)
	}
	for _, ap := range ied.AccessPoints {
		rawAP := genAccessPoint{Name: ap.Name}
		if ap.Server != nil {
			rawSrv := &genServer{}
			for _, ld := range ap.Server.LDevices {
				rawSrv.LDevices = append(rawSrv.LDevices, ldToGenXML(ld))
			}
			rawAP.Server = rawSrv
		}
		raw.AccessPoints = append(raw.AccessPoints, rawAP)
	}
	return raw
}

func ldToGenXML(ld LDevice) genLDevice {
	raw := genLDevice{Inst: ld.Inst, Desc: ld.Desc}
	if ld.LN0 != nil {
		ln := lnToGenXML(*ld.LN0)
		raw.LN0 = &ln
	}
	for _, ln := range ld.LNs {
		raw.LNs = append(raw.LNs, lnToGenXML(ln))
	}
	return raw
}

func lnToGenXML(ln LN) genLN {
	raw := genLN{
		Prefix: ln.Prefix, LNClass: ln.LNClass,
		Inst: ln.Inst, LNType: ln.LNType, Desc: ln.Desc,
	}
	for _, doi := range ln.DOIs {
		raw.DOIs = append(raw.DOIs, doiToGenXML(doi))
	}
	for _, ds := range ln.DataSets {
		raw.DataSets = append(raw.DataSets, dsToGenXML(ds))
	}
	for _, rc := range ln.Reports {
		raw.Reports = append(raw.Reports, rcToGenXML(rc))
	}
	for _, lg := range ln.Logs {
		raw.Logs = append(raw.Logs, genLog(lg))
	}
	return raw
}

func doiToGenXML(doi DOI) genDOI {
	raw := genDOI{Name: doi.Name, Desc: doi.Desc}
	for _, dai := range doi.DAIs {
		raw.DAIs = append(raw.DAIs, genDAI(dai))
	}
	for _, sdi := range doi.SDIs {
		raw.SDIs = append(raw.SDIs, sdiToGenXML(sdi))
	}
	return raw
}

func sdiToGenXML(sdi SDI) genSDI {
	raw := genSDI{Name: sdi.Name}
	for _, dai := range sdi.DAIs {
		raw.DAIs = append(raw.DAIs, genDAI(dai))
	}
	for _, sub := range sdi.SDIs {
		raw.SDIs = append(raw.SDIs, sdiToGenXML(sub))
	}
	return raw
}

func dsToGenXML(ds DataSet) genDataSet {
	raw := genDataSet{Name: ds.Name, Desc: ds.Desc}
	for _, f := range ds.FCDAs {
		raw.FCDAs = append(raw.FCDAs, genFCDA(f))
	}
	return raw
}

func rcToGenXML(rc ReportControl) genReportControl {
	raw := genReportControl{
		Name:   rc.Name,
		Desc:   rc.Desc,
		RptID:  rc.RptID,
		DatSet: rc.DatSet,
	}
	if rc.ConfRev > 0 {
		raw.ConfRev = strconv.FormatUint(uint64(rc.ConfRev), 10)
	}
	if rc.Buffered {
		raw.Buffered = "true"
	}
	if rc.BufTime > 0 {
		raw.BufTime = strconv.FormatUint(uint64(rc.BufTime), 10)
	}
	if rc.IntgPd > 0 {
		raw.IntgPd = strconv.FormatUint(uint64(rc.IntgPd), 10)
	}
	raw.TrgOps = &genTrgOps{
		Dchg: genBoolStr(rc.TrgOps.Dchg), Qchg: genBoolStr(rc.TrgOps.Qchg),
		Dupd: genBoolStr(rc.TrgOps.Dupd), Period: genBoolStr(rc.TrgOps.Period),
		GI: genBoolStr(rc.TrgOps.GI),
	}
	raw.OptFields = &genOptFields{
		SeqNum: genBoolStr(rc.OptFields.SeqNum), TimeStamp: genBoolStr(rc.OptFields.TimeStamp),
		DataSet: genBoolStr(rc.OptFields.DataSet), ReasonCode: genBoolStr(rc.OptFields.ReasonCode),
		DataRef: genBoolStr(rc.OptFields.DataRef), EntryID: genBoolStr(rc.OptFields.EntryID),
		ConfigRef: genBoolStr(rc.OptFields.ConfigRef), BufOvfl: genBoolStr(rc.OptFields.BufOvfl),
	}
	return raw
}

func genBoolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func dttToGenXML(dtt DataTypeTemplates) genDataTypeTemplates {
	raw := genDataTypeTemplates{}
	for _, lnt := range dtt.LNodeTypes {
		rawLNT := genLNodeType{ID: lnt.ID, LNClass: lnt.LNClass, Desc: lnt.Desc}
		for _, do := range lnt.DOs {
			rawLNT.DOs = append(rawLNT.DOs, genDO(do))
		}
		raw.LNodeTypes = append(raw.LNodeTypes, rawLNT)
	}
	for _, dot := range dtt.DOTypes {
		rawDOT := genDOType{ID: dot.ID, CDC: dot.CDC, Desc: dot.Desc}
		for _, da := range dot.DAs {
			rawDA := genDA{
				Name: da.Name, FC: da.FC, BType: da.BType,
				Type: da.Type, Desc: da.Desc, Val: da.Val,
			}
			if da.Count > 0 {
				rawDA.Count = strconv.Itoa(da.Count)
			}
			rawDOT.DAs = append(rawDOT.DAs, rawDA)
		}
		for _, sdo := range dot.SDOs {
			rawDOT.SDOs = append(rawDOT.SDOs, genSDO(sdo))
		}
		raw.DOTypes = append(raw.DOTypes, rawDOT)
	}
	for _, dat := range dtt.DATypes {
		rawDAT := genDAType{ID: dat.ID, Desc: dat.Desc}
		for _, bda := range dat.BDAs {
			rawBDA := genBDA{
				Name: bda.Name, BType: bda.BType, Type: bda.Type,
				Desc: bda.Desc, Val: bda.Val,
			}
			if bda.Count > 0 {
				rawBDA.Count = strconv.Itoa(bda.Count)
			}
			rawDAT.BDAs = append(rawDAT.BDAs, rawBDA)
		}
		raw.DATypes = append(raw.DATypes, rawDAT)
	}
	for _, et := range dtt.EnumTypes {
		rawET := genEnumType{ID: et.ID, Desc: et.Desc}
		for _, ev := range et.Vals {
			rawET.Vals = append(rawET.Vals, genEnumVal{
				Ord: strconv.Itoa(ev.Ord), Desc: ev.Value,
			})
		}
		raw.EnumTypes = append(raw.EnumTypes, rawET)
	}
	return raw
}

func servicesToGenXML(svc *Services) *genServices {
	raw := &genServices{}
	if svc.DynAssociation {
		raw.DynAssociation = &struct{}{}
	}
	if svc.GetDirectory {
		raw.GetDirectory = &struct{}{}
	}
	if svc.GetDataObjectDefinition {
		raw.GetDataObjectDefinition = &struct{}{}
	}
	if svc.GetDataSetValue {
		raw.GetDataSetValue = &struct{}{}
	}
	if svc.DataSetDirectory {
		raw.DataSetDirectory = &struct{}{}
	}
	if svc.ReadWrite {
		raw.ReadWrite = &struct{}{}
	}
	if svc.GetCBValues {
		raw.GetCBValues = &struct{}{}
	}
	if svc.FileHandling {
		raw.FileHandling = &struct{}{}
	}
	if svc.ConfDataSet != nil {
		raw.ConfDataSet = &genConfDataSet{
			Max:           strconv.Itoa(svc.ConfDataSet.Max),
			MaxAttributes: strconv.Itoa(svc.ConfDataSet.MaxAttributes),
			Modify:        genBoolStr(svc.ConfDataSet.Modify),
		}
	}
	if svc.ConfReportCtrl != nil {
		raw.ConfReportCtrl = &genConfReportControl{
			Max:     strconv.Itoa(svc.ConfReportCtrl.Max),
			BufMode: svc.ConfReportCtrl.BufMode,
		}
	}
	if svc.ReportSettings != nil {
		raw.ReportSettings = &genReportSettings{
			CBName:    svc.ReportSettings.CBName,
			DatSet:    svc.ReportSettings.DatSet,
			RptID:     svc.ReportSettings.RptID,
			OptFields: svc.ReportSettings.OptFields,
			BufTime:   svc.ReportSettings.BufTime,
			TrgOps:    svc.ReportSettings.TrgOps,
			IntgPd:    svc.ReportSettings.IntgPd,
		}
	}
	if svc.ConfLNs != nil {
		raw.ConfLNs = &genConfLNs{
			FixPrefix: genBoolStr(svc.ConfLNs.FixPrefix),
			FixLnInst: genBoolStr(svc.ConfLNs.FixLnInst),
		}
	}
	if svc.GOOSE != nil {
		raw.GOOSE = &genGOOSEService{
			Max:       strconv.Itoa(svc.GOOSE.Max),
			FixedOffs: genBoolStr(svc.GOOSE.FixedOffs),
		}
	}
	if svc.SMVsc != nil {
		raw.SMVsc = &genSMVService{
			Max: strconv.Itoa(svc.SMVsc.Max),
		}
	}
	return raw
}
