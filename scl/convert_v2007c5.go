package scl

import (
	"fmt"
	"strconv"

	raw "github.com/otfabric/go-iec61850/scl/internal/raw/v2007c5"
)

func convertV2007C5(r *raw.SCL) (*SCL, []Diagnostic, error) {
	var diags []Diagnostic

	s := &SCL{
		Header: Header{
			ID:       r.Header.Id,
			Version:  r.Header.Version,
			Revision: r.Header.Revision,
		},
	}

	for _, sub := range r.Substation {
		s.Substations = append(s.Substations, convSubstation2007c5(sub))
	}

	if r.Communication != nil {
		s.Communication = convComm2007c5(r.Communication)
	}

	for _, ied := range r.IED {
		s.IEDs = append(s.IEDs, convIED2007c5(ied, &diags))
	}

	if r.DataTypeTemplates != nil {
		s.DataTypeTemplates = convDTT2007c5(r.DataTypeTemplates, &diags)
	}

	return s, diags, nil
}

func convIED2007c5(r raw.IED, diags *[]Diagnostic) IED {
	ied := IED{
		Name:         r.Name,
		Desc:         r.Desc,
		Manufacturer: r.Manufacturer,
		Type:         r.Type,
	}

	if r.Services != nil {
		ied.Services = convServices2007c5(r.Services)
	}

	for _, ap := range r.AccessPoint {
		ied.AccessPoints = append(ied.AccessPoints, convAP2007c5(ap, diags))
	}
	for _, p := range r.Private {
		ied.Private = append(ied.Private, Private{Type: p.Type, Source: p.Source, InnerXML: p.InnerXML})
	}
	return ied
}

func convAP2007c5(r raw.AccessPoint, diags *[]Diagnostic) AccessPoint {
	ap := AccessPoint{Name: r.Name}
	if r.Server != nil {
		srv := &Server{}
		for _, ld := range r.Server.LDevice {
			srv.LDevices = append(srv.LDevices, convLD2007c5(ld, diags))
		}
		ap.Server = srv
	}
	for _, p := range r.Private {
		ap.Private = append(ap.Private, Private{Type: p.Type, Source: p.Source, InnerXML: p.InnerXML})
	}
	return ap
}

func convLD2007c5(r raw.LDevice, diags *[]Diagnostic) LDevice {
	ld := LDevice{Inst: r.Inst, Desc: r.Desc}
	ln0 := convLN02007c5(r.LN0, diags)
	ld.LN0 = &ln0
	for _, ln := range r.LN {
		ld.LNs = append(ld.LNs, convLN2007c5(ln, diags))
	}
	for _, p := range r.Private {
		ld.Private = append(ld.Private, Private{Type: p.Type, Source: p.Source, InnerXML: p.InnerXML})
	}
	return ld
}

func convLN02007c5(r raw.LN0, _ *[]Diagnostic) LN {
	ln := LN{
		LNClass: r.LnClass,
		Inst:    r.Inst,
		LNType:  r.LnType,
		Desc:    r.Desc,
	}
	for _, doi := range r.DOI {
		ln.DOIs = append(ln.DOIs, convDOI2007c5(doi))
	}
	for _, ds := range r.DataSet {
		ln.DataSets = append(ln.DataSets, convDS2007c5(ds))
	}
	for _, rc := range r.ReportControl {
		ln.Reports = append(ln.Reports, convRC2007c5(rc))
	}
	for _, gc := range r.GSEControl {
		ln.GSEControls = append(ln.GSEControls, GSEControl{
			Name: gc.Name, Desc: gc.Desc, AppID: gc.AppID,
			Type: string(gc.Type), DatSet: gc.DatSet,
			ConfRev: gc.ConfRev, FixedOffs: gc.FixedOffs,
		})
	}
	for _, sv := range r.SampledValueControl {
		ln.SMVControls = append(ln.SMVControls, SMVControl{
			Name: sv.Name, Desc: sv.Desc, SmvID: sv.SmvID,
			DatSet: sv.DatSet, ConfRev: sv.ConfRev,
			SmpRate: sv.SmpRate, NofASDU: sv.NofASDU, Multicast: sv.Multicast,
		})
	}
	for _, lc := range r.LogControl {
		ln.Logs = append(ln.Logs, Log{Name: lc.Name, Desc: lc.Desc})
	}
	if r.SettingControl != nil {
		ln.SettingControl = &SettingControl{
			NumOfSGs: uint8(r.SettingControl.NumOfSGs),
			ActSG:    uint8(r.SettingControl.ActSG),
			ResvTms:  r.SettingControl.ResvTms,
		}
	}
	for _, p := range r.Private {
		ln.Private = append(ln.Private, Private{Type: p.Type, Source: p.Source, InnerXML: p.InnerXML})
	}
	return ln
}

func convLN2007c5(r raw.LN, _ *[]Diagnostic) LN {
	ln := LN{
		Prefix:  r.Prefix,
		LNClass: r.LnClass,
		Inst:    r.Inst,
		LNType:  r.LnType,
		Desc:    r.Desc,
	}
	for _, doi := range r.DOI {
		ln.DOIs = append(ln.DOIs, convDOI2007c5(doi))
	}
	for _, ds := range r.DataSet {
		ln.DataSets = append(ln.DataSets, convDS2007c5(ds))
	}
	for _, rc := range r.ReportControl {
		ln.Reports = append(ln.Reports, convRC2007c5(rc))
	}
	for _, lc := range r.LogControl {
		ln.Logs = append(ln.Logs, Log{Name: lc.Name, Desc: lc.Desc})
	}
	for _, p := range r.Private {
		ln.Private = append(ln.Private, Private{Type: p.Type, Source: p.Source, InnerXML: p.InnerXML})
	}
	return ln
}

func convDOI2007c5(r raw.DOI) DOI {
	doi := DOI{Name: r.Name, Desc: r.Desc}
	for _, d := range r.DAI {
		doi.DAIs = append(doi.DAIs, convDAI2007c5(d))
	}
	for _, s := range r.SDI {
		doi.SDIs = append(doi.SDIs, convSDI2007c5(s))
	}
	return doi
}

func convSDI2007c5(r raw.SDI) SDI {
	sdi := SDI{Name: r.Name}
	for _, d := range r.DAI {
		sdi.DAIs = append(sdi.DAIs, convDAI2007c5(d))
	}
	for _, s := range r.SDI {
		sdi.SDIs = append(sdi.SDIs, convSDI2007c5(*s))
	}
	return sdi
}

func convDAI2007c5(r raw.DAI) DAI {
	val := ""
	if len(r.Val) > 0 {
		val = r.Val[0].Value
	}
	return DAI{Name: r.Name, SAddr: r.SAddr, Val: val}
}

func convDS2007c5(r raw.DataSet) DataSet {
	ds := DataSet{Name: r.Name, Desc: r.Desc}
	for _, f := range r.FCDA {
		ds.FCDAs = append(ds.FCDAs, FCDA{
			LDInst:  f.LdInst,
			Prefix:  f.Prefix,
			LNClass: f.LnClass,
			LNInst:  f.LnInst,
			DOName:  f.DoName,
			DAName:  f.DaName,
			FC:      f.Fc,
		})
	}
	return ds
}

func convRC2007c5(r raw.ReportControl) ReportControl {
	rc := ReportControl{
		Name:     r.Name,
		Desc:     r.Desc,
		RptID:    r.RptID,
		DatSet:   r.DatSet,
		ConfRev:  r.ConfRev,
		Buffered: r.Buffered,
		BufTime:  r.BufTime,
		IntgPd:   r.IntgPd,
	}
	if r.TrgOps != nil {
		rc.TrgOps = TrgOps{
			Dchg:   r.TrgOps.Dchg,
			Qchg:   r.TrgOps.Qchg,
			Dupd:   r.TrgOps.Dupd,
			Period: r.TrgOps.Period,
			GI:     r.TrgOps.Gi,
		}
	}
	rc.OptFields = OptFields{
		SeqNum:     r.OptFields.SeqNum,
		TimeStamp:  r.OptFields.TimeStamp,
		DataSet:    r.OptFields.DataSet,
		ReasonCode: r.OptFields.ReasonCode,
		DataRef:    r.OptFields.DataRef,
		EntryID:    r.OptFields.EntryID,
		ConfigRef:  r.OptFields.ConfigRef,
		BufOvfl:    r.OptFields.BufOvfl,
	}
	return rc
}

func convDTT2007c5(r *raw.DataTypeTemplates, diags *[]Diagnostic) DataTypeTemplates {
	var dtt DataTypeTemplates
	for _, lnt := range r.LNodeType {
		t := LNodeType{ID: lnt.Id, LNClass: lnt.LnClass, Desc: lnt.Desc}
		for _, d := range lnt.DO {
			t.DOs = append(t.DOs, DO{Name: d.Name, Type: d.Type, Desc: d.Desc})
		}
		dtt.LNodeTypes = append(dtt.LNodeTypes, t)
	}
	for _, dot := range r.DOType {
		t := DOType{ID: dot.Id, CDC: dot.Cdc, Desc: dot.Desc}
		for _, d := range dot.DA {
			da := DA{
				Name:  d.Name,
				FC:    d.Fc,
				BType: d.BType,
				Type:  d.Type,
				Desc:  d.Desc,
			}
			if d.Count != "" {
				v, err := strconv.Atoi(d.Count)
				if err != nil {
					*diags = append(*diags, Diagnostic{
						Severity: DiagWarning,
						Code:     "invalid-count",
						Path:     fmt.Sprintf("DOType[%s].DA[%s]", dot.Id, d.Name),
						Message:  fmt.Sprintf("invalid count %q: %v", d.Count, err),
					})
				} else {
					da.Count = v
				}
			}
			if len(d.Val) > 0 {
				da.Val = d.Val[0].Value
			}
			t.DAs = append(t.DAs, da)
		}
		for _, s := range dot.SDO {
			t.SDOs = append(t.SDOs, SDO{Name: s.Name, Type: s.Type, Desc: s.Desc})
		}
		dtt.DOTypes = append(dtt.DOTypes, t)
	}
	for _, dat := range r.DAType {
		t := DAType{ID: dat.Id, Desc: dat.Desc}
		for _, b := range dat.BDA {
			bda := BDA{
				Name:  b.Name,
				BType: b.BType,
				Type:  b.Type,
				Desc:  b.Desc,
			}
			if b.Count != "" {
				v, err := strconv.Atoi(b.Count)
				if err != nil {
					*diags = append(*diags, Diagnostic{
						Severity: DiagWarning,
						Code:     "invalid-count",
						Path:     fmt.Sprintf("DAType[%s].BDA[%s]", dat.Id, b.Name),
						Message:  fmt.Sprintf("invalid count %q: %v", b.Count, err),
					})
				} else {
					bda.Count = v
				}
			}
			if len(b.Val) > 0 {
				bda.Val = b.Val[0].Value
			}
			t.BDAs = append(t.BDAs, bda)
		}
		dtt.DATypes = append(dtt.DATypes, t)
	}
	for _, et := range r.EnumType {
		t := EnumType{ID: et.Id, Desc: et.Desc}
		for _, v := range et.EnumVal {
			t.Vals = append(t.Vals, EnumVal{Ord: v.Ord, Value: v.Value})
		}
		dtt.EnumTypes = append(dtt.EnumTypes, t)
	}
	return dtt
}

func convSubstation2007c5(r raw.Substation) Substation {
	sub := Substation{Name: r.Name, Desc: r.Desc}
	for _, ln := range r.LNode {
		sub.LNodes = append(sub.LNodes, convLNode2007c5(ln))
	}
	for _, vl := range r.VoltageLevel {
		s := VoltageLevel{Name: vl.Name, Desc: vl.Desc}
		if vl.Voltage != nil {
			s.Voltage = fmt.Sprintf("%v", vl.Voltage.Value)
		}
		for _, ln := range vl.LNode {
			s.LNodes = append(s.LNodes, convLNode2007c5(ln))
		}
		for _, bay := range vl.Bay {
			b := Bay{Name: bay.Name, Desc: bay.Desc}
			for _, ln := range bay.LNode {
				b.LNodes = append(b.LNodes, convLNode2007c5(ln))
			}
			for _, ce := range bay.ConductingEquipment {
				b.ConductingEquipments = append(b.ConductingEquipments, ConductingEquipment{
					Name: ce.Name, Type: ce.Type, Desc: ce.Desc,
				})
			}
			s.Bays = append(s.Bays, b)
		}
		sub.VoltageLevels = append(sub.VoltageLevels, s)
	}
	return sub
}

func convLNode2007c5(r raw.LNode) LNode {
	return LNode{
		IEDName: r.IedName, LDInst: r.LdInst, Prefix: r.Prefix,
		LNClass: r.LnClass, LNInst: r.LnInst, LNType: r.LnType,
		Desc: r.Desc,
	}
}

func convComm2007c5(r *raw.Communication) *Communication {
	comm := &Communication{}
	for _, sn := range r.SubNetwork {
		s := SubNetwork{Name: sn.Name, Desc: sn.Desc, Type: sn.Type}
		for _, cap := range sn.ConnectedAP {
			c := ConnectedAP{IEDName: cap.IedName, APName: cap.ApName}
			if cap.Address != nil {
				for _, p := range cap.Address.P {
					c.Address = append(c.Address, P{Type: p.Type, Value: p.Value})
				}
			}
			for _, g := range cap.GSE {
				gse := GSEAddress{LDInst: g.LdInst, CBName: g.CbName}
				if g.Address != nil {
					for _, p := range g.Address.P {
						gse.Address = append(gse.Address, P{Type: p.Type, Value: p.Value})
					}
				}
				if g.MinTime != nil {
					gse.MinTime = fmt.Sprintf("%v", g.MinTime.Value)
				}
				if g.MaxTime != nil {
					gse.MaxTime = fmt.Sprintf("%v", g.MaxTime.Value)
				}
				c.GSEs = append(c.GSEs, gse)
			}
			for _, sv := range cap.SMV {
				smv := SMVAddress{LDInst: sv.LdInst, CBName: sv.CbName}
				if sv.Address != nil {
					for _, p := range sv.Address.P {
						smv.Address = append(smv.Address, P{Type: p.Type, Value: p.Value})
					}
				}
				c.SMVs = append(c.SMVs, smv)
			}
			s.ConnectedAPs = append(s.ConnectedAPs, c)
		}
		comm.SubNetworks = append(comm.SubNetworks, s)
	}
	return comm
}

func convServices2007c5(r *raw.Services) *Services {
	svc := &Services{
		DynAssociation:          r.DynAssociation != nil,
		GetDirectory:            r.GetDirectory != nil,
		GetDataObjectDefinition: r.GetDataObjectDefinition != nil,
		GetDataSetValue:         r.GetDataSetValue != nil,
		DataSetDirectory:        r.DataSetDirectory != nil,
		ReadWrite:               r.ReadWrite != nil,
		GetCBValues:             r.GetCBValues != nil,
		FileHandling:            r.FileHandling != nil,
	}
	if r.ConfDataSet != nil {
		svc.ConfDataSet = &ConfDataSet{
			Max:           int(r.ConfDataSet.Max),
			MaxAttributes: int(r.ConfDataSet.MaxAttributes),
			Modify:        r.ConfDataSet.Modify,
		}
	}
	if r.ConfReportControl != nil {
		svc.ConfReportCtrl = &ConfReportControl{
			Max:     int(r.ConfReportControl.Max),
			BufMode: r.ConfReportControl.BufMode,
		}
	}
	if r.ReportSettings != nil {
		svc.ReportSettings = &ReportSettings{
			CBName:    string(r.ReportSettings.CbName),
			DatSet:    string(r.ReportSettings.DatSet),
			RptID:     string(r.ReportSettings.RptID),
			OptFields: string(r.ReportSettings.OptFields),
			BufTime:   string(r.ReportSettings.BufTime),
			TrgOps:    string(r.ReportSettings.TrgOps),
			IntgPd:    string(r.ReportSettings.IntgPd),
		}
	}
	if r.ConfLNs != nil {
		svc.ConfLNs = &ConfLNs{
			FixPrefix: r.ConfLNs.FixPrefix,
			FixLnInst: r.ConfLNs.FixLnInst,
		}
	}
	if r.GOOSE != nil {
		svc.GOOSE = &GOOSEService{
			Max:       int(r.GOOSE.Max),
			FixedOffs: r.GOOSE.FixedOffs,
		}
	}
	if r.SMVsc != nil {
		svc.SMVsc = &SMVService{
			Max: int(r.SMVsc.Max),
		}
	}
	return svc
}
