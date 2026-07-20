// SPDX-License-Identifier: MIT

package scl

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestFlatten(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	rows := Flatten(s)
	if len(rows) == 0 {
		t.Fatal("Flatten returned 0 rows")
	}

	// LLN0 has Mod (4 DAs: stVal, q, t, ctlModel) and Beh (3 DAs: stVal, q, t)
	// GGIO has Ind1 (3 DAs: stVal, q, t) and AnIn1 (3 DAs: mag.f, q, t) = 6 per GGIO
	// LLN0: 7 rows
	// GGIO1: 6 rows (Ind1:3 + AnIn1: mag.f + q + t = 3)
	// GGIO2: 6 rows
	// Total: 7 + 6 + 6 = 19
	t.Logf("Flatten produced %d rows", len(rows))

	hasMod := false
	hasInd1 := false
	hasMagF := false
	for _, r := range rows {
		if r.LN == "LLN0" && r.Path == "Mod.stVal" {
			hasMod = true
			if r.FC != "ST" {
				t.Errorf("Mod.stVal FC = %q, want ST", r.FC)
			}
			if r.BType != "Enum" {
				t.Errorf("Mod.stVal BType = %q, want Enum", r.BType)
			}
			if r.CDC != "INC" {
				t.Errorf("Mod.stVal CDC = %q, want INC", r.CDC)
			}
		}
		if r.LN == "GGIO1" && r.Path == "Ind1.stVal" {
			hasInd1 = true
			if r.BType != "BOOLEAN" {
				t.Errorf("Ind1.stVal BType = %q", r.BType)
			}
		}
		if r.LN == "GGIO1" && r.Path == "AnIn1.mag.f" {
			hasMagF = true
			if r.BType != "FLOAT32" {
				t.Errorf("AnIn1.mag.f BType = %q", r.BType)
			}
		}
	}

	if !hasMod {
		t.Error("missing Mod.stVal row")
	}
	if !hasInd1 {
		t.Error("missing Ind1.stVal row")
	}
	if !hasMagF {
		t.Error("missing AnIn1.mag.f row")
	}
}

func TestFlatten_AllRowsHaveIED(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	rows := Flatten(s)
	for i, r := range rows {
		if r.IED == "" {
			t.Errorf("row %d: empty IED", i)
		}
		if r.LD == "" {
			t.Errorf("row %d: empty LD", i)
		}
		if r.LN == "" {
			t.Errorf("row %d: empty LN", i)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	rows := Flatten(s)

	var buf bytes.Buffer
	if err := WriteCSV(&buf, rows); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	csv := buf.String()
	if !strings.HasPrefix(csv, "IED,AccessPoint,LD,LN,Path,FC,BType,CDC,Desc,Status\n") {
		t.Error("CSV header missing or wrong")
	}

	lines := strings.Split(strings.TrimSpace(csv), "\n")
	// header + data rows
	if len(lines) != len(rows)+1 {
		t.Errorf("CSV lines = %d, want %d (header + %d rows)", len(lines), len(rows)+1, len(rows))
	}
}

func TestPrintTree(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	var buf bytes.Buffer
	if err := PrintTree(&buf, s); err != nil {
		t.Fatalf("PrintTree: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "IED: IED1") {
		t.Error("tree output missing IED name")
	}
	if !strings.Contains(output, "LD: LD1") {
		t.Error("tree output missing LD name")
	}
	if !strings.Contains(output, "BRCB: brcb01") {
		t.Error("tree output missing BRCB")
	}
	if !strings.Contains(output, "URCB: urcb01") {
		t.Error("tree output missing URCB")
	}
	if !strings.Contains(output, "DS: ds1") {
		t.Error("tree output missing DataSet")
	}
}

func TestFlatten_EmptySCL(t *testing.T) {
	s := &SCL{}
	rows := Flatten(s)
	if len(rows) != 0 {
		t.Errorf("got %d rows for empty SCL, want 0", len(rows))
	}
}

type failWriter struct{ err error }

func (fw *failWriter) Write(_ []byte) (int, error) { return 0, fw.err }

func TestPrintTree_WriterFailure(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	writeErr := errors.New("broken pipe")
	err = PrintTree(&failWriter{err: writeErr}, s)
	if err == nil {
		t.Fatal("expected error from PrintTree")
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("got %v, want wrapped %v", err, writeErr)
	}
}

func TestWriteCSV_WriterFailure(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	rows := Flatten(s)
	writeErr := errors.New("disk full")
	err = WriteCSV(&failWriter{err: writeErr}, rows)
	if err == nil {
		t.Fatal("expected error from WriteCSV")
	}
}

// --- M8.6 Export helpers tests ---

func TestExportDataSets(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	rows := ExportDataSets(s)
	if len(rows) != 1 {
		t.Fatalf("got %d DataSetRows, want 1", len(rows))
	}

	ds := rows[0]
	if ds.IED != "IED1" {
		t.Errorf("IED = %q", ds.IED)
	}
	if ds.LD != "LD1" {
		t.Errorf("LD = %q", ds.LD)
	}
	if ds.LN != "LLN0" {
		t.Errorf("LN = %q", ds.LN)
	}
	if ds.DataSet != "ds1" {
		t.Errorf("DataSet = %q", ds.DataSet)
	}
	if ds.MemberCount != 2 {
		t.Errorf("MemberCount = %d, want 2", ds.MemberCount)
	}
}

func TestExportReports(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	rows := ExportReports(s)
	if len(rows) != 2 {
		t.Fatalf("got %d ReportRows, want 2", len(rows))
	}

	brcb := rows[0]
	if brcb.Name != "brcb01" {
		t.Errorf("Name = %q", brcb.Name)
	}
	if !brcb.Buffered {
		t.Error("brcb should be buffered")
	}
	if brcb.RptID != "rpt01" {
		t.Errorf("RptID = %q", brcb.RptID)
	}

	urcb := rows[1]
	if urcb.Name != "urcb01" {
		t.Errorf("Name = %q", urcb.Name)
	}
	if urcb.Buffered {
		t.Error("urcb should not be buffered")
	}
}

// --- M8.7 Lookup helpers tests ---

func TestFindIED(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ied := s.FindIED("IED1")
	if ied == nil {
		t.Fatal("FindIED returned nil for IED1")
	}
	if ied.Name != "IED1" {
		t.Errorf("Name = %q", ied.Name)
	}

	if s.FindIED("NonExistent") != nil {
		t.Error("FindIED should return nil for non-existent IED")
	}
}

func TestFindLDevice(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ld := s.FindLDevice("LD1")
	if ld == nil {
		t.Fatal("FindLDevice returned nil")
	}
	if ld.Inst != "LD1" {
		t.Errorf("Inst = %q", ld.Inst)
	}

	if s.FindLDevice("NoLD") != nil {
		t.Error("FindLDevice should return nil for non-existent LD")
	}
}

func TestIED_FindLDevice(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ied := s.FindIED("IED1")
	ld := ied.FindLDevice("LD1")
	if ld == nil {
		t.Fatal("IED.FindLDevice returned nil")
	}

	if ied.FindLDevice("NoLD") != nil {
		t.Error("IED.FindLDevice should return nil for non-existent LD")
	}
}

func TestLDevice_FindLN(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	ld := s.FindLDevice("LD1")
	ln0 := ld.FindLN("", "LLN0", "")
	if ln0 == nil {
		t.Fatal("FindLN returned nil for LLN0")
	}

	ggio1 := ld.FindLN("", "GGIO", "1")
	if ggio1 == nil {
		t.Fatal("FindLN returned nil for GGIO1")
	}
	if ggio1.LNClass != "GGIO" || ggio1.Inst != "1" {
		t.Errorf("LN = %s%s%s", ggio1.Prefix, ggio1.LNClass, ggio1.Inst)
	}

	if ld.FindLN("", "XCBR", "1") != nil {
		t.Error("FindLN should return nil for non-existent LN")
	}
}

func TestFindLNodeType(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	lnt := s.FindLNodeType("LLN0_Type")
	if lnt == nil {
		t.Fatal("FindLNodeType returned nil")
	}
	if lnt.LNClass != "LLN0" {
		t.Errorf("LNClass = %q", lnt.LNClass)
	}

	if s.FindLNodeType("NoType") != nil {
		t.Error("FindLNodeType should return nil for non-existent type")
	}
}

func TestFindDOType(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	dot := s.FindDOType("SPS_Ind")
	if dot == nil {
		t.Fatal("FindDOType returned nil")
	}
	if dot.CDC != "SPS" {
		t.Errorf("CDC = %q", dot.CDC)
	}

	if s.FindDOType("NoType") != nil {
		t.Error("FindDOType should return nil for non-existent type")
	}
}

func TestFindDAType(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	dat := s.FindDAType("AnalogueValue")
	if dat == nil {
		t.Fatal("FindDAType returned nil")
	}

	if s.FindDAType("NoType") != nil {
		t.Error("FindDAType should return nil for non-existent type")
	}
}

func TestFindEnumType(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	et := s.FindEnumType("Mod_Enum")
	if et == nil {
		t.Fatal("FindEnumType returned nil")
	}
	if len(et.Vals) != 5 {
		t.Errorf("got %d vals", len(et.Vals))
	}

	if s.FindEnumType("NoEnum") != nil {
		t.Error("FindEnumType should return nil for non-existent enum")
	}
}

// --- M8.3 Validation tests ---

func TestValidate_ValidSCL(t *testing.T) {
	s, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	findings := Validate(s)
	for _, f := range findings {
		if f.Severity == DiagError {
			t.Errorf("unexpected error: %s", f)
		}
	}
}

func TestValidate_MissingDOType(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{
				{ID: "LNT1", LNClass: "LLN0", DOs: []DO{
					{Name: "Mod", Type: "NonExistentDOType"},
				}},
			},
		},
	}

	findings := Validate(s)
	hasError := false
	for _, f := range findings {
		if f.Severity == DiagError && strings.Contains(f.Message, "NonExistentDOType") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for missing DOType reference")
	}
}

func TestValidate_MissingLNodeType(t *testing.T) {
	s := &SCL{
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name: "S1",
				Server: &Server{
					LDevices: []LDevice{{
						Inst: "LD1",
						LN0:  &LN{LNClass: "LLN0", LNType: "NonExistentLNT"},
					}},
				},
			}},
		}},
	}

	findings := Validate(s)
	hasError := false
	for _, f := range findings {
		if f.Severity == DiagError && strings.Contains(f.Message, "NonExistentLNT") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for missing LNodeType reference")
	}
}

func TestValidate_MissingDAType(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			DOTypes: []DOType{
				{ID: "DOT1", CDC: "SPS", DAs: []DA{
					{Name: "attr", FC: "ST", BType: "Struct", Type: "MissingDAType"},
				}},
			},
		},
	}

	findings := Validate(s)
	hasError := false
	for _, f := range findings {
		if f.Severity == DiagError && strings.Contains(f.Message, "MissingDAType") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for missing DAType reference")
	}
}

func TestValidate_MissingEnumType(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			DOTypes: []DOType{
				{ID: "DOT1", CDC: "INC", DAs: []DA{
					{Name: "stVal", FC: "ST", BType: "Enum", Type: "MissingEnum"},
				}},
			},
		},
	}

	findings := Validate(s)
	hasError := false
	for _, f := range findings {
		if f.Severity == DiagError && strings.Contains(f.Message, "MissingEnum") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for missing EnumType reference")
	}
}

func TestValidate_CommunicationBadIED(t *testing.T) {
	s := &SCL{
		Communication: &Communication{
			SubNetworks: []SubNetwork{{
				Name: "Net1",
				ConnectedAPs: []ConnectedAP{{
					IEDName: "GhostIED",
					APName:  "S1",
				}},
			}},
		},
	}

	findings := Validate(s)
	hasError := false
	for _, f := range findings {
		if f.Severity == DiagError && strings.Contains(f.Message, "GhostIED") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for non-existent IED in Communication")
	}
}

// --- M8.4 Generation / round-trip tests ---

func TestGenerate_Roundtrip(t *testing.T) {
	s1, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	var buf bytes.Buffer
	if err := Generate(&buf, s1); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	s2, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}

	if s2.Header.ID != s1.Header.ID {
		t.Errorf("Header.ID mismatch: %q vs %q", s2.Header.ID, s1.Header.ID)
	}
	if len(s2.IEDs) != len(s1.IEDs) {
		t.Errorf("IED count: %d vs %d", len(s2.IEDs), len(s1.IEDs))
	}
	if s2.IEDs[0].Name != s1.IEDs[0].Name {
		t.Errorf("IED name mismatch")
	}

	// Verify DTT roundtrip
	if len(s2.DataTypeTemplates.LNodeTypes) != len(s1.DataTypeTemplates.LNodeTypes) {
		t.Errorf("LNodeType count mismatch")
	}
	if len(s2.DataTypeTemplates.DOTypes) != len(s1.DataTypeTemplates.DOTypes) {
		t.Errorf("DOType count mismatch")
	}
	if len(s2.DataTypeTemplates.EnumTypes) != len(s1.DataTypeTemplates.EnumTypes) {
		t.Errorf("EnumType count mismatch")
	}

	// Verify Substation roundtrip
	if len(s2.Substations) != len(s1.Substations) {
		t.Errorf("Substation count mismatch")
	}
	if len(s2.Substations) > 0 && s2.Substations[0].Name != s1.Substations[0].Name {
		t.Errorf("Substation name mismatch")
	}

	// Verify Communication roundtrip
	if s2.Communication == nil {
		t.Fatal("Communication lost in roundtrip")
	}
	if len(s2.Communication.SubNetworks) != len(s1.Communication.SubNetworks) {
		t.Errorf("SubNetwork count mismatch")
	}

	// Verify Reports roundtrip
	ld1 := s1.IEDs[0].AccessPoints[0].Server.LDevices[0]
	ld2 := s2.IEDs[0].AccessPoints[0].Server.LDevices[0]
	if len(ld2.LN0.Reports) != len(ld1.LN0.Reports) {
		t.Errorf("Report count mismatch")
	}
	if ld2.LN0.Reports[0].Buffered != ld1.LN0.Reports[0].Buffered {
		t.Error("Buffered flag lost in roundtrip")
	}
	if ld2.LN0.Reports[0].ConfRev != ld1.LN0.Reports[0].ConfRev {
		t.Errorf("ConfRev mismatch: %d vs %d", ld2.LN0.Reports[0].ConfRev, ld1.LN0.Reports[0].ConfRev)
	}
}

func TestGenerate_WriterFailure(t *testing.T) {
	s := &SCL{Header: Header{ID: "test"}}
	writeErr := errors.New("broken pipe")
	err := Generate(&failWriter{err: writeErr}, s)
	if err == nil {
		t.Fatal("expected error from Generate")
	}
}

func TestGenerate_MinimalSCL(t *testing.T) {
	s := &SCL{Header: Header{ID: "min", Version: "1"}}

	var buf bytes.Buffer
	if err := Generate(&buf, s); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	s2, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if s2.Header.ID != "min" {
		t.Errorf("Header.ID = %q", s2.Header.ID)
	}
}

func TestParse_DOI_SDI(t *testing.T) {
	xml := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL">
	<Header id="t"/>
	<IED name="I1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
		<LN0 lnClass="LLN0" inst="" lnType="t1">
			<DOI name="Mod" desc="Mode">
				<DAI name="stVal"><Val>on</Val></DAI>
				<SDI name="origin">
					<DAI name="orCat"><Val>remote-control</Val></DAI>
					<SDI name="nested">
						<DAI name="deep"><Val>42</Val></DAI>
					</SDI>
				</SDI>
			</DOI>
		</LN0>
	</LDevice></Server></AccessPoint></IED>
	<DataTypeTemplates><LNodeType id="t1" lnClass="LLN0"/></DataTypeTemplates>
	</SCL>`
	s, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	ln0 := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	if len(ln0.DOIs) != 1 {
		t.Fatalf("got %d DOIs, want 1", len(ln0.DOIs))
	}

	doi := ln0.DOIs[0]
	if doi.Name != "Mod" || doi.Desc != "Mode" {
		t.Errorf("DOI = {%q, %q}", doi.Name, doi.Desc)
	}
	if len(doi.DAIs) != 1 || doi.DAIs[0].Name != "stVal" || doi.DAIs[0].Val != "on" {
		t.Errorf("DOI.DAIs = %+v", doi.DAIs)
	}
	if len(doi.SDIs) != 1 || doi.SDIs[0].Name != "origin" {
		t.Fatalf("DOI.SDIs = %+v", doi.SDIs)
	}

	sdi := doi.SDIs[0]
	if len(sdi.DAIs) != 1 || sdi.DAIs[0].Val != "remote-control" {
		t.Errorf("SDI.DAIs = %+v", sdi.DAIs)
	}
	if len(sdi.SDIs) != 1 || sdi.SDIs[0].Name != "nested" {
		t.Fatalf("nested SDIs = %+v", sdi.SDIs)
	}
	if len(sdi.SDIs[0].DAIs) != 1 || sdi.SDIs[0].DAIs[0].Val != "42" {
		t.Errorf("nested DAI = %+v", sdi.SDIs[0].DAIs)
	}
}

func TestGenerate_DOI_SDI_Roundtrip(t *testing.T) {
	s := &SCL{
		Header: Header{ID: "doi-test"},
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name: "S1",
				Server: &Server{LDevices: []LDevice{{
					Inst: "LD1",
					LN0: &LN{
						LNClass: "LLN0", LNType: "t1",
						DOIs: []DOI{{
							Name: "Mod", Desc: "Mode",
							DAIs: []DAI{{Name: "stVal", Val: "on"}},
							SDIs: []SDI{{
								Name: "origin",
								DAIs: []DAI{{Name: "orCat", SAddr: "0x1234"}},
								SDIs: []SDI{{
									Name: "nested",
									DAIs: []DAI{{Name: "deep", Val: "42"}},
								}},
							}},
						}},
					},
				}}},
			}},
		}},
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{ID: "t1", LNClass: "LLN0"}},
		},
	}

	var buf bytes.Buffer
	if err := Generate(&buf, s); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	s2, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}

	ln0 := s2.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	if len(ln0.DOIs) != 1 {
		t.Fatalf("roundtrip: got %d DOIs", len(ln0.DOIs))
	}
	doi := ln0.DOIs[0]
	if doi.Name != "Mod" {
		t.Errorf("roundtrip: DOI.Name = %q", doi.Name)
	}
	if len(doi.DAIs) != 1 || doi.DAIs[0].Val != "on" {
		t.Errorf("roundtrip: DOI.DAIs = %+v", doi.DAIs)
	}
	if len(doi.SDIs) != 1 || doi.SDIs[0].Name != "origin" {
		t.Fatalf("roundtrip: DOI.SDIs = %+v", doi.SDIs)
	}
	if doi.SDIs[0].DAIs[0].SAddr != "0x1234" {
		t.Errorf("roundtrip: SAddr = %q", doi.SDIs[0].DAIs[0].SAddr)
	}
	if len(doi.SDIs[0].SDIs) != 1 || doi.SDIs[0].SDIs[0].DAIs[0].Val != "42" {
		t.Errorf("roundtrip: nested SDI lost")
	}
}

func TestValidate_ReportDatSetWarning(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{ID: "t1", LNClass: "LLN0"}},
		},
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name: "S1",
				Server: &Server{LDevices: []LDevice{{
					Inst: "LD1",
					LN0: &LN{
						LNClass: "LLN0", LNType: "t1",
						Reports: []ReportControl{{
							Name: "rc1", DatSet: "nonExistentDS",
						}},
					},
				}}},
			}},
		}},
	}

	findings := Validate(s)
	hasError := false
	for _, f := range findings {
		if f.Severity == DiagError && strings.Contains(f.Message, "nonExistentDS") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for missing datSet reference")
	}
}

func TestFlatten_SDOChain(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{
				ID: "LNT1", LNClass: "GGIO",
				DOs: []DO{{Name: "Pos", Type: "DPC"}},
			}},
			DOTypes: []DOType{
				{ID: "DPC", CDC: "DPC", SDOs: []SDO{{Name: "origin", Type: "OrigT"}}},
				{ID: "OrigT", CDC: "xxx", DAs: []DA{
					{Name: "orCat", FC: "ST", BType: "Enum"},
				}},
			},
		},
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name: "AP1",
				Server: &Server{LDevices: []LDevice{{
					Inst: "LD1",
					LNs:  []LN{{LNClass: "GGIO", Inst: "1", LNType: "LNT1"}},
				}}},
			}},
		}},
	}

	rows := Flatten(s)
	found := false
	for _, r := range rows {
		if r.Path == "Pos.origin.orCat" {
			found = true
			if r.FC != "ST" {
				t.Errorf("FC = %q", r.FC)
			}
		}
	}
	if !found {
		t.Error("missing Pos.origin.orCat row from SDO chain")
	}
}

func TestFlatten_StructBDA(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{
				ID: "LNT1", LNClass: "MMXU",
				DOs: []DO{{Name: "PhV", Type: "WYE"}},
			}},
			DOTypes: []DOType{
				{ID: "WYE", CDC: "WYE", DAs: []DA{
					{Name: "phsA", FC: "MX", BType: "Struct", Type: "CMV"},
				}},
			},
			DATypes: []DAType{
				{ID: "CMV", BDAs: []BDA{
					{Name: "cVal", BType: "Struct", Type: "Vector"},
				}},
				{ID: "Vector", BDAs: []BDA{
					{Name: "mag", BType: "FLOAT32"},
					{Name: "ang", BType: "FLOAT32"},
				}},
			},
		},
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name: "AP1",
				Server: &Server{LDevices: []LDevice{{
					Inst: "LD1",
					LNs:  []LN{{LNClass: "MMXU", Inst: "1", LNType: "LNT1"}},
				}}},
			}},
		}},
	}

	rows := Flatten(s)
	foundMag := false
	foundAng := false
	for _, r := range rows {
		if r.Path == "PhV.phsA.cVal.mag" {
			foundMag = true
		}
		if r.Path == "PhV.phsA.cVal.ang" {
			foundAng = true
		}
	}
	if !foundMag {
		t.Error("missing PhV.phsA.cVal.mag from nested Struct BDA chain")
	}
	if !foundAng {
		t.Error("missing PhV.phsA.cVal.ang from nested Struct BDA chain")
	}
}

func TestGenerate_Services_Roundtrip(t *testing.T) {
	s1, err := ParseFile("testdata/simple.scd")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s1.IEDs[0].Services == nil {
		t.Fatal("source has no Services")
	}

	var buf bytes.Buffer
	if err := Generate(&buf, s1); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	s2, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}

	svc := s2.IEDs[0].Services
	if svc == nil {
		t.Fatal("Services lost in roundtrip")
	}
	if !svc.DynAssociation {
		t.Error("DynAssociation lost")
	}
	if !svc.ReadWrite {
		t.Error("ReadWrite lost")
	}
	if !svc.FileHandling {
		t.Error("FileHandling lost")
	}
	if svc.ConfDataSet == nil || svc.ConfDataSet.Max != 10 {
		t.Errorf("ConfDataSet lost or wrong: %+v", svc.ConfDataSet)
	}
	if svc.ConfReportCtrl == nil || svc.ConfReportCtrl.Max != 20 {
		t.Errorf("ConfReportCtrl lost or wrong: %+v", svc.ConfReportCtrl)
	}
	if svc.GOOSE == nil || svc.GOOSE.Max != 5 {
		t.Errorf("GOOSE lost or wrong: %+v", svc.GOOSE)
	}
	if svc.SMVsc == nil || svc.SMVsc.Max != 3 {
		t.Errorf("SMVsc lost or wrong: %+v", svc.SMVsc)
	}
	if svc.ReportSettings == nil || svc.ReportSettings.DatSet != "Dyn" {
		t.Errorf("ReportSettings lost or wrong: %+v", svc.ReportSettings)
	}
}

func TestValidate_GSE_BadLDInst(t *testing.T) {
	s := &SCL{
		Communication: &Communication{
			SubNetworks: []SubNetwork{{
				Name: "Net1",
				ConnectedAPs: []ConnectedAP{{
					IEDName: "IED1",
					APName:  "S1",
					GSEs: []GSEAddress{{
						LDInst: "NonExistentLD",
						CBName: "gcb01",
					}},
				}},
			}},
		},
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name:   "S1",
				Server: &Server{LDevices: []LDevice{{Inst: "LD1"}}},
			}},
		}},
	}

	findings := Validate(s)
	hasWarning := false
	for _, f := range findings {
		if f.Severity == DiagWarning && strings.Contains(f.Message, "NonExistentLD") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected warning for GSE referencing non-existent LDevice")
	}
}

func TestValidate_DuplicateTypeIDs(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{
				{ID: "LNT1", LNClass: "LLN0"},
				{ID: "LNT1", LNClass: "LLN0"},
			},
			DOTypes: []DOType{
				{ID: "DOT1", CDC: "SPS"},
				{ID: "DOT1", CDC: "SPS"},
			},
		},
		IEDs: []IED{
			{Name: "IED1"},
			{Name: "IED1"},
		},
	}

	findings := Validate(s)
	var dupLNT, dupDOT, dupIED bool
	for _, f := range findings {
		if f.Severity == DiagError {
			if strings.Contains(f.Message, "duplicate ID \"LNT1\"") && f.Path == "LNodeType" {
				dupLNT = true
			}
			if strings.Contains(f.Message, "duplicate ID \"DOT1\"") && f.Path == "DOType" {
				dupDOT = true
			}
			if strings.Contains(f.Message, "duplicate name \"IED1\"") && f.Path == "IED" {
				dupIED = true
			}
		}
	}
	if !dupLNT {
		t.Error("expected error for duplicate LNodeType ID")
	}
	if !dupDOT {
		t.Error("expected error for duplicate DOType ID")
	}
	if !dupIED {
		t.Error("expected error for duplicate IED name")
	}
}

func TestFlatten_CyclicDOType(t *testing.T) {
	s := &SCL{
		DataTypeTemplates: DataTypeTemplates{
			LNodeTypes: []LNodeType{{ID: "t1", LNClass: "LLN0", DOs: []DO{{Name: "Mod", Type: "dot1"}}}},
			DOTypes: []DOType{
				{ID: "dot1", CDC: "SPS",
					DAs:  []DA{{Name: "stVal", BType: "BOOLEAN", FC: "ST"}},
					SDOs: []SDO{{Name: "sub", Type: "dot1"}}},
			},
		},
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{{
				Name: "S1",
				Server: &Server{LDevices: []LDevice{{
					Inst: "LD1",
					LN0:  &LN{LNClass: "LLN0", LNType: "t1"},
				}}},
			}},
		}},
	}

	// Should not infinite-loop; cycle is broken by visited set.
	rows := Flatten(s)

	// Expect rows from the top-level DA and the one SDO level
	// (second level is skipped due to cycle detection).
	if len(rows) < 1 {
		t.Fatalf("expected at least 1 row, got %d", len(rows))
	}

	// Verify the top-level DA is present.
	found := false
	for _, r := range rows {
		if r.Path == "Mod.stVal" {
			found = true
		}
	}
	if !found {
		t.Error("expected Mod.stVal row from top-level DOType DA")
	}
}
