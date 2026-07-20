// SPDX-License-Identifier: MIT

package scl

import (
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if s.Header.ID != "TestSCL" {
		t.Errorf("Header.ID = %q, want %q", s.Header.ID, "TestSCL")
	}
	if s.Header.Version != "1" {
		t.Errorf("Header.Version = %q", s.Header.Version)
	}
	if s.Header.Revision != "0" {
		t.Errorf("Header.Revision = %q", s.Header.Revision)
	}
}

func TestParse_IEDs(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(s.IEDs) != 1 {
		t.Fatalf("got %d IEDs, want 1", len(s.IEDs))
	}

	ied := s.IEDs[0]
	if ied.Name != "IED1" {
		t.Errorf("IED.Name = %q", ied.Name)
	}
	if ied.Desc != "Test IED" {
		t.Errorf("IED.Desc = %q", ied.Desc)
	}
	if ied.Manufacturer != "TestMfr" {
		t.Errorf("IED.Manufacturer = %q", ied.Manufacturer)
	}
	if len(ied.AccessPoints) != 1 {
		t.Fatalf("got %d APs, want 1", len(ied.AccessPoints))
	}
}

func TestParse_LDevice(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	srv := s.IEDs[0].AccessPoints[0].Server
	if srv == nil {
		t.Fatal("Server is nil")
	}
	if len(srv.LDevices) != 1 {
		t.Fatalf("got %d LDevices, want 1", len(srv.LDevices))
	}

	ld := srv.LDevices[0]
	if ld.Inst != "LD1" {
		t.Errorf("LD.Inst = %q", ld.Inst)
	}
	if ld.LN0 == nil {
		t.Fatal("LN0 is nil")
	}
	if len(ld.LNs) != 2 {
		t.Fatalf("got %d LNs, want 2", len(ld.LNs))
	}
}

func TestParse_LN0_DataSets(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ln0 := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	if len(ln0.DataSets) != 1 {
		t.Fatalf("got %d DataSets, want 1", len(ln0.DataSets))
	}

	ds := ln0.DataSets[0]
	if ds.Name != "ds1" {
		t.Errorf("DataSet.Name = %q", ds.Name)
	}
	if len(ds.FCDAs) != 2 {
		t.Fatalf("got %d FCDAs, want 2", len(ds.FCDAs))
	}

	fcda := ds.FCDAs[0]
	if fcda.LDInst != "LD1" || fcda.LNClass != "GGIO" || fcda.DOName != "Ind1" || fcda.FC != "ST" {
		t.Errorf("FCDA = %+v", fcda)
	}
}

func TestParse_ReportControls(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ln0 := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	if len(ln0.Reports) != 2 {
		t.Fatalf("got %d Reports, want 2", len(ln0.Reports))
	}

	brcb := ln0.Reports[0]
	if brcb.Name != "brcb01" {
		t.Errorf("BRCB.Name = %q", brcb.Name)
	}
	if !brcb.Buffered {
		t.Error("BRCB.Buffered should be true")
	}
	if brcb.RptID != "rpt01" {
		t.Errorf("BRCB.RptID = %q", brcb.RptID)
	}
	if brcb.ConfRev != 1 {
		t.Errorf("BRCB.ConfRev = %d", brcb.ConfRev)
	}
	if brcb.BufTime != 100 {
		t.Errorf("BRCB.BufTime = %d", brcb.BufTime)
	}
	if brcb.IntgPd != 5000 {
		t.Errorf("BRCB.IntgPd = %d", brcb.IntgPd)
	}
	if !brcb.TrgOps.Dchg || !brcb.TrgOps.Qchg || brcb.TrgOps.Dupd || !brcb.TrgOps.Period || !brcb.TrgOps.GI {
		t.Errorf("BRCB.TrgOps = %+v", brcb.TrgOps)
	}
	if !brcb.OptFields.SeqNum || !brcb.OptFields.TimeStamp || brcb.OptFields.DataSet ||
		!brcb.OptFields.ReasonCode || brcb.OptFields.DataRef || !brcb.OptFields.EntryID ||
		!brcb.OptFields.ConfigRef || !brcb.OptFields.BufOvfl {
		t.Errorf("BRCB.OptFields = %+v", brcb.OptFields)
	}

	urcb := ln0.Reports[1]
	if urcb.Name != "urcb01" {
		t.Errorf("URCB.Name = %q", urcb.Name)
	}
	if urcb.Buffered {
		t.Error("URCB.Buffered should be false")
	}
}

func TestParse_LogControls(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ln0 := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	if len(ln0.Logs) != 1 {
		t.Fatalf("got %d Logs, want 1", len(ln0.Logs))
	}
	if ln0.Logs[0].Name != "lcb01" {
		t.Errorf("Log.Name = %q", ln0.Logs[0].Name)
	}
}

func TestParse_DataTypeTemplates(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	dtt := s.DataTypeTemplates
	if len(dtt.LNodeTypes) != 2 {
		t.Errorf("got %d LNodeTypes, want 2", len(dtt.LNodeTypes))
	}
	if len(dtt.DOTypes) != 4 {
		t.Errorf("got %d DOTypes, want 4", len(dtt.DOTypes))
	}
	if len(dtt.DATypes) != 1 {
		t.Errorf("got %d DATypes, want 1", len(dtt.DATypes))
	}
	if len(dtt.EnumTypes) != 2 {
		t.Errorf("got %d EnumTypes, want 2", len(dtt.EnumTypes))
	}

	modEnum := dtt.EnumTypes[0]
	if modEnum.ID != "Mod_Enum" {
		t.Errorf("EnumType.ID = %q", modEnum.ID)
	}
	if len(modEnum.Vals) != 5 {
		t.Fatalf("got %d enum vals, want 5", len(modEnum.Vals))
	}
	if modEnum.Vals[0].Ord != 1 || modEnum.Vals[0].Value != "on" {
		t.Errorf("EnumVal[0] = %+v", modEnum.Vals[0])
	}
}

func TestParse_DOType_DAs(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	var sps *DOType
	for i := range s.DataTypeTemplates.DOTypes {
		if s.DataTypeTemplates.DOTypes[i].ID == "SPS_Ind" {
			sps = &s.DataTypeTemplates.DOTypes[i]
			break
		}
	}
	if sps == nil {
		t.Fatal("DOType SPS_Ind not found")
	}

	if sps.CDC != "SPS" {
		t.Errorf("CDC = %q", sps.CDC)
	}
	if len(sps.DAs) != 3 {
		t.Fatalf("got %d DAs, want 3", len(sps.DAs))
	}

	stVal := sps.DAs[0]
	if stVal.Name != "stVal" || stVal.FC != "ST" || stVal.BType != "BOOLEAN" {
		t.Errorf("stVal DA = %+v", stVal)
	}
}

func TestParse_MalformedXML(t *testing.T) {
	_, err := Parse(strings.NewReader("<not valid xml"))
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	_, err := Parse(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParse_MinimalSCL(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="min"/></SCL>`
	s, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Header.ID != "min" {
		t.Errorf("Header.ID = %q", s.Header.ID)
	}
	if len(s.IEDs) != 0 {
		t.Errorf("got %d IEDs, want 0", len(s.IEDs))
	}
}

func TestParse_InvalidConfRev(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN0 lnClass="LLN0" inst="" lnType="t1">
			<ReportControl name="rc1" confRev="abc"/>
		</LN0>
	</LDevice></Server></AccessPoint></IED>
	<DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
	</SCL>`
	_, err := Parse(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for invalid confRev")
	}
}

func TestParse_InvalidBufTime(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN0 lnClass="LLN0" inst="" lnType="t1">
			<ReportControl name="rc1" bufTime="xyz"/>
		</LN0>
	</LDevice></Server></AccessPoint></IED>
	<DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
	</SCL>`
	_, err := Parse(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for invalid bufTime")
	}
}

func TestParse_InvalidDACount(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<DataTypeTemplates>
		<DOType id="d1" cdc="SPS">
			<DA name="a" fc="ST" bType="INT32" count="notanumber"/>
		</DOType>
	</DataTypeTemplates>
	</SCL>`
	_, err := Parse(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for invalid DA count")
	}
}

func TestParse_MissingIEDName(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/><IED name=""/></SCL>`
	s, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(s.IEDs) != 1 {
		t.Fatalf("expected 1 IED, got %d", len(s.IEDs))
	}
	if s.IEDs[0].Name != "" {
		t.Errorf("IED.Name = %q, want empty", s.IEDs[0].Name)
	}
}

func TestParse_MissingLDeviceInst(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server>
		<LDevice inst=""/>
	</Server></AccessPoint></IED></SCL>`
	s, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(s.IEDs) == 0 {
		t.Fatal("expected IED to be parsed")
	}
}

func TestParse_MissingLNClass(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN lnClass="" inst="1" lnType="t1"/>
	</LDevice></Server></AccessPoint></IED></SCL>`
	s, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(s.IEDs) == 0 {
		t.Fatal("expected IED to be parsed")
	}
}

func TestParse_MissingLNType(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN lnClass="GGIO" inst="1" lnType=""/>
	</LDevice></Server></AccessPoint></IED></SCL>`
	s, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(s.IEDs) == 0 {
		t.Fatal("expected IED to be parsed")
	}
}

func TestParse_InvalidBuffered(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN0 lnClass="LLN0" inst="" lnType="t1">
			<ReportControl name="rc1" buffered="yes"/>
		</LN0>
	</LDevice></Server></AccessPoint></IED>
	<DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
	</SCL>`
	_, err := Parse(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for invalid buffered attribute")
	}
	if !strings.Contains(err.Error(), "ParseBool") && !strings.Contains(err.Error(), "buffered") {
		t.Errorf("error should mention 'ParseBool' or 'buffered', got: %v", err)
	}
}

func TestParse_InvalidTrgOps(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN0 lnClass="LLN0" inst="" lnType="t1">
			<ReportControl name="rc1"><TrgOps dchg="maybe"/></ReportControl>
		</LN0>
	</LDevice></Server></AccessPoint></IED>
	<DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
	</SCL>`
	_, err := Parse(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for invalid TrgOps boolean")
	}
	if !strings.Contains(err.Error(), "ParseBool") && !strings.Contains(err.Error(), "TrgOps") {
		t.Errorf("error should mention 'ParseBool' or 'TrgOps', got: %v", err)
	}
}

func TestParse_InvalidOptFields(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN0 lnClass="LLN0" inst="" lnType="t1">
			<ReportControl name="rc1"><OptFields seqNum="2"/></ReportControl>
		</LN0>
	</LDevice></Server></AccessPoint></IED>
	<DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
	</SCL>`
	_, err := Parse(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for invalid OptFields boolean")
	}
	if !strings.Contains(err.Error(), "ParseBool") && !strings.Contains(err.Error(), "OptFields") {
		t.Errorf("error should mention 'ParseBool' or 'OptFields', got: %v", err)
	}
}

// --- M8.1 Communication parsing tests ---

func TestParse_Communication(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if s.Communication == nil {
		t.Fatal("Communication is nil")
	}
	if len(s.Communication.SubNetworks) != 1 {
		t.Fatalf("got %d SubNetworks, want 1", len(s.Communication.SubNetworks))
	}

	sn := s.Communication.SubNetworks[0]
	if sn.Name != "Net1" {
		t.Errorf("SubNetwork.Name = %q", sn.Name)
	}
	if sn.Desc != "Station Bus" {
		t.Errorf("SubNetwork.Desc = %q", sn.Desc)
	}
	if sn.Type != "8-MMS" {
		t.Errorf("SubNetwork.Type = %q", sn.Type)
	}
	if len(sn.ConnectedAPs) != 1 {
		t.Fatalf("got %d ConnectedAPs, want 1", len(sn.ConnectedAPs))
	}

	cap := sn.ConnectedAPs[0]
	if cap.IEDName != "IED1" {
		t.Errorf("ConnectedAP.IEDName = %q", cap.IEDName)
	}
	if cap.APName != "S1" {
		t.Errorf("ConnectedAP.APName = %q", cap.APName)
	}
	if len(cap.Address) == 0 {
		t.Fatal("ConnectedAP has no address parameters")
	}

	foundIP := false
	for _, p := range cap.Address {
		if p.Type == "IP" && p.Value == "192.168.1.10" {
			foundIP = true
		}
	}
	if !foundIP {
		t.Error("missing IP address parameter")
	}
}

func TestParse_GSE(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cap := s.Communication.SubNetworks[0].ConnectedAPs[0]
	if len(cap.GSEs) != 1 {
		t.Fatalf("got %d GSEs, want 1", len(cap.GSEs))
	}

	gse := cap.GSEs[0]
	if gse.LDInst != "LD1" {
		t.Errorf("GSE.LDInst = %q", gse.LDInst)
	}
	if gse.CBName != "gcb01" {
		t.Errorf("GSE.CBName = %q", gse.CBName)
	}
	if gse.MinTime != "10" {
		t.Errorf("GSE.MinTime = %q", gse.MinTime)
	}
	if gse.MaxTime != "1000" {
		t.Errorf("GSE.MaxTime = %q", gse.MaxTime)
	}

	foundMAC := false
	for _, p := range gse.Address {
		if p.Type == "MAC-Address" {
			foundMAC = true
		}
	}
	if !foundMAC {
		t.Error("GSE missing MAC-Address parameter")
	}
}

func TestParse_SMV(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cap := s.Communication.SubNetworks[0].ConnectedAPs[0]
	if len(cap.SMVs) != 1 {
		t.Fatalf("got %d SMVs, want 1", len(cap.SMVs))
	}

	smv := cap.SMVs[0]
	if smv.LDInst != "LD1" {
		t.Errorf("SMV.LDInst = %q", smv.LDInst)
	}
	if smv.CBName != "smv01" {
		t.Errorf("SMV.CBName = %q", smv.CBName)
	}
	if len(smv.Address) == 0 {
		t.Error("SMV has no address parameters")
	}
}

// --- M8.2 Substation parsing tests ---

func TestParse_Substation(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(s.Substations) != 1 {
		t.Fatalf("got %d Substations, want 1", len(s.Substations))
	}

	sub := s.Substations[0]
	if sub.Name != "Sub1" {
		t.Errorf("Substation.Name = %q", sub.Name)
	}
	if sub.Desc != "Test Substation" {
		t.Errorf("Substation.Desc = %q", sub.Desc)
	}
	if len(sub.VoltageLevels) != 1 {
		t.Fatalf("got %d VoltageLevels, want 1", len(sub.VoltageLevels))
	}

	vl := sub.VoltageLevels[0]
	if vl.Name != "E1" {
		t.Errorf("VoltageLevel.Name = %q", vl.Name)
	}
	if vl.Voltage != "110" {
		t.Errorf("VoltageLevel.Voltage = %q, want %q", vl.Voltage, "110")
	}
	if len(vl.Bays) != 2 {
		t.Fatalf("got %d Bays, want 2", len(vl.Bays))
	}

	bay := vl.Bays[0]
	if bay.Name != "Q1" {
		t.Errorf("Bay.Name = %q", bay.Name)
	}
	if len(bay.ConductingEquipments) != 2 {
		t.Fatalf("got %d ConductingEquipments, want 2", len(bay.ConductingEquipments))
	}

	ce := bay.ConductingEquipments[0]
	if ce.Name != "QA1" || ce.Type != "CBR" {
		t.Errorf("CE = {Name:%q, Type:%q}", ce.Name, ce.Type)
	}
}

// --- M8.5 Services parsing tests ---

func TestParse_Services(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ied := s.IEDs[0]
	if ied.Services == nil {
		t.Fatal("IED.Services is nil")
	}

	svc := ied.Services
	if !svc.DynAssociation {
		t.Error("DynAssociation should be true")
	}
	if !svc.GetDirectory {
		t.Error("GetDirectory should be true")
	}
	if !svc.ReadWrite {
		t.Error("ReadWrite should be true")
	}
	if !svc.FileHandling {
		t.Error("FileHandling should be true")
	}

	if svc.ConfDataSet == nil {
		t.Fatal("ConfDataSet is nil")
	}
	if svc.ConfDataSet.Max != 10 {
		t.Errorf("ConfDataSet.Max = %d", svc.ConfDataSet.Max)
	}
	if svc.ConfDataSet.MaxAttributes != 100 {
		t.Errorf("ConfDataSet.MaxAttributes = %d", svc.ConfDataSet.MaxAttributes)
	}
	if !svc.ConfDataSet.Modify {
		t.Error("ConfDataSet.Modify should be true")
	}

	if svc.ConfReportCtrl == nil {
		t.Fatal("ConfReportCtrl is nil")
	}
	if svc.ConfReportCtrl.Max != 20 {
		t.Errorf("ConfReportCtrl.Max = %d", svc.ConfReportCtrl.Max)
	}
	if svc.ConfReportCtrl.BufMode != "both" {
		t.Errorf("ConfReportCtrl.BufMode = %q", svc.ConfReportCtrl.BufMode)
	}

	if svc.ReportSettings == nil {
		t.Fatal("ReportSettings is nil")
	}
	if svc.ReportSettings.DatSet != "Dyn" {
		t.Errorf("ReportSettings.DatSet = %q", svc.ReportSettings.DatSet)
	}

	if svc.GOOSE == nil {
		t.Fatal("GOOSE is nil")
	}
	if svc.GOOSE.Max != 5 {
		t.Errorf("GOOSE.Max = %d", svc.GOOSE.Max)
	}

	if svc.SMVsc == nil {
		t.Fatal("SMVsc is nil")
	}
	if svc.SMVsc.Max != 3 {
		t.Errorf("SMVsc.Max = %d", svc.SMVsc.Max)
	}
}

func TestParseBool_Variants(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		err   bool
	}{
		{"true", true, false},
		{"false", false, false},
		{"", false, false},
		{"True", true, false},
		{"FALSE", false, false},
		{" TRUE ", true, false},
		{"yes", false, true},
		{"1", false, true},
	}
	for _, tt := range tests {
		got, err := parseBool(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("parseBool(%q): err = %v, wantErr = %v", tt.input, err, tt.err)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("parseBool(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
