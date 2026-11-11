// SPDX-License-Identifier: MIT

package servermodel

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

func reportNames(m *Model) []string {
	var out []string
	for _, r := range m.LogicalDevices[0].LogicalNodes[0].Reports {
		out = append(out, r.Name)
	}
	return out
}

// TestFromSCL_IndexedRCBExpands: an RCB with indexed=true expands to
// instances numbered from 01; with indexed=false the single instance
// keeps its name without a number (IEC 61850-8-1 §17.1.2) (RPT-20).
func TestFromSCL_IndexedRCBExpands(t *testing.T) {
	for _, tc := range []struct {
		indexed bool
		max     uint32
		want    []string
	}{
		{true, 3, []string{"brcbA01", "brcbA02", "brcbA03"}},
		{true, 0, []string{"brcbA01"}},
		{false, 1, []string{"brcbA"}},
	} {
		s := testSCL()
		rc := &s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.Reports[0]
		rc.Name, rc.Indexed, rc.MaxInstances = "brcbA", tc.indexed, tc.max
		m, err := FromSCL(s, "IED1", "")
		if err != nil {
			t.Fatal(err)
		}
		if got := reportNames(m); !slices.Equal(got, tc.want) {
			t.Errorf("indexed=%v max=%d: reports %v, want %v", tc.indexed, tc.max, got, tc.want)
		}
	}
}

const lcbSCL = `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="t"/>
  <IED name="IED1">
    <AccessPoint name="AP1"><Server><Authentication/>
      <LDevice inst="LD1">
        <LN0 lnClass="LLN0" inst="" lnType="LLN0T">
          <DataSet name="ds1"><FCDA ldInst="LD1" lnClass="LLN0" doName="Mod" daName="stVal" fc="ST"/></DataSet>
          <ReportControl name="rcb" datSet="ds1" confRev="1"><RptEnabled max="2"/></ReportControl>
          <ReportControl name="single" datSet="ds1" confRev="1" indexed="false"/>
          <LogControl name="lcb1" datSet="ds1" logName="EventLog" intgPd="1000" reasonCode="false">
            <TrgOps dchg="true" period="true"/>
          </LogControl>
          <LogControl name="lcb2" logName="Other" logEna="false" ldInst="LD2" lnClass="GGIO" lnInst="1"/>
          <Log name="EventLog"/>
        </LN0>
      </LDevice>
      <LDevice inst="LD2">
        <LN0 lnClass="LLN0" inst="" lnType="LLN0T"/>
        <LN lnClass="GGIO" inst="1" lnType="LLN0T"/>
      </LDevice>
    </Server></AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0T" lnClass="LLN0"><DO name="Mod" type="INS"/></LNodeType>
    <DOType id="INS" cdc="INS">
      <DA name="stVal" fc="ST" bType="INT32" dchg="true"/>
      <DA name="q" fc="ST" bType="Quality" qchg="true"/>
      <DA name="t" fc="ST" bType="Timestamp"/>
    </DOType>
  </DataTypeTemplates>
</SCL>`

// TestFromSCL_LogControlUsesLogName: the LCB writes to the log named by
// logName in the LN given by ldInst/prefix/lnClass/lnInst (IEC 61850-6
// §9.3.9 Table 26); its DatSet, TrgOps, IntgPd and LogEna are kept
// (LOG-02). DA trigger options are carried into the model (RPT-16), and
// an absent indexed attribute means indexed (RPT-20).
func TestFromSCL_LogControlUsesLogName(t *testing.T) {
	doc, err := scl.Parse(strings.NewReader(lcbSCL))
	if err != nil {
		t.Fatal(err)
	}
	m, err := FromSCL(doc, "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	ln := m.LogicalDevices[0].LogicalNodes[0]
	if got := reportNames(m); !slices.Equal(got, []string{"rcb01", "rcb02", "single"}) {
		t.Errorf("reports = %v", got)
	}
	if len(ln.Logs) != 2 {
		t.Fatalf("logs = %+v", ln.Logs)
	}
	l1, l2 := ln.Logs[0], ln.Logs[1]
	if l1.Name != "lcb1" || l1.LogName != "EventLog" || l1.DatSet != "ds1" || l1.IntgPd != 1000 ||
		!l1.LogEna || l1.ReasonCode || !l1.TrgOps.Dchg || !l1.TrgOps.Period || l1.TrgOps.Qchg {
		t.Errorf("lcb1 = %+v", l1)
	}
	if d, j := l1.JournalName("IED1LD1", "LLN0"); d != "IED1LD1" || j != "LLN0$EventLog" {
		t.Errorf("lcb1 journal = %s/%s", d, j)
	}
	if l2.LogEna {
		t.Error("lcb2 LogEna = true, want false")
	}
	if d, j := l2.JournalName("IED1LD1", "LLN0"); d != "IED1LD2" || j != "GGIO1$Other" {
		t.Errorf("lcb2 journal = %s/%s, want IED1LD2/GGIO1$Other", d, j)
	}
	var stVal, q, tm DataAttribute
	for _, a := range ln.DataObjects[0].Attributes {
		switch a.Name {
		case "stVal":
			stVal = a
		case "q":
			q = a
		case "t":
			tm = a
		}
	}
	if !stVal.Dchg || stVal.Qchg || !q.Qchg || q.Dchg || tm.Dchg || tm.Qchg || tm.Dupd {
		t.Errorf("trigger options stVal=%v/%v q=%v/%v t=%v/%v/%v", stVal.Dchg, stVal.Qchg, q.Dchg, q.Qchg, tm.Dchg, tm.Qchg, tm.Dupd)
	}
}

// TestFromSCL_LogControlDefaultLogName: "The name of the default journal
// shall be “GeneralLog”" (IEC 61850-8-1 §17.3.3.3.2).
func TestFromSCL_LogControlDefaultLogName(t *testing.T) {
	def := convertLog(&scl.Log{Name: "lcb"}, "LD1")
	if def.LogName != "GeneralLog" || def.LogLN != "LLN0" || def.LogLD != "LD1" {
		t.Errorf("default log = %+v", def)
	}
	legacy := LogDef{Name: "log1"}
	if d, j := legacy.JournalName("LD1", "LLN0"); d != "LD1" || j != "LLN0$log1" {
		t.Errorf("legacy journal = %s/%s", d, j)
	}
}

// TestSetTrgOpsInherited: a BDA carries the TrgOp of its DA.
func TestSetTrgOpsInherited(t *testing.T) {
	a := DataAttribute{Name: "cVal", Children: []DataAttribute{{Name: "mag", Children: []DataAttribute{{Name: "f"}}}}}
	setTrgOps(&a, true, false, true)
	if f := a.Children[0].Children[0]; !f.Dchg || f.Qchg || !f.Dupd {
		t.Errorf("leaf = %+v", f)
	}
}

// TestValidate_LogControlBlocks: LCB names are unique in the LN and the
// data set must exist (IEC 61850-6 §9.3.9).
func TestValidate_LogControlBlocks(t *testing.T) {
	m := &Model{LogicalDevices: []LogicalDevice{{Name: "LD1", LogicalNodes: []LogicalNode{{
		Name: "LLN0", LNClass: "LLN0",
		Logs: []LogDef{{Name: "lcb"}, {Name: "lcb"}, {Name: "x", DatSet: "nope"}},
	}}}}}
	err := errors.Join(m.Validate()...)
	if err == nil || !strings.Contains(err.Error(), "duplicate log control block") || !strings.Contains(err.Error(), "non-existent dataset") {
		t.Errorf("Validate = %v", err)
	}
}

func TestParseLCBItem(t *testing.T) {
	for in, want := range map[string][2]string{
		"LLN0$LG$lcb1$LogEna": {"LLN0$LG$lcb1", "LogEna"},
		"LLN0$LG$lcb1":        {"", ""},
		"LLN0$LG$lcb1$a$b":    {"", ""},
		"LLN0$ST$Mod$stVal":   {"", ""},
		"$LG$x$y":             {"", ""},
	} {
		item, attr, ok := ParseLCBItem(in)
		if item != want[0] || attr != want[1] || ok != (want[0] != "") {
			t.Errorf("ParseLCBItem(%q) = %q %q %v", in, item, attr, ok)
		}
	}
}
