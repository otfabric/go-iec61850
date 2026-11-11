// SPDX-License-Identifier: MIT

package scl

import (
	"strings"
	"testing"
)

const rcbLCBBody = `
  <Header id="t"/>
  <IED name="IED1">
    <AccessPoint name="AP1"><Server><Authentication/>
      <LDevice inst="LD1">
        <LN0 lnClass="LLN0" inst="" lnType="LLN0T">
          <ReportControl name="rcb" datSet="ds1" confRev="1"%s><RptEnabled max="3"/></ReportControl>
          <LogControl name="lcb1" datSet="ds1" logName="EventLog" intgPd="500" reasonCode="false"%s>
            <TrgOps dchg="true" period="true"/>
          </LogControl>
        </LN0>
        <LN lnClass="GGIO" inst="1" lnType="LLN0T">
          <LogControl name="lcb2" logName="L2" logEna="false"/>
        </LN>
      </LDevice>
    </Server></AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0T" lnClass="LLN0"><DO name="Mod" type="INS"/></LNodeType>
    <DOType id="INS" cdc="INS">
      <DA name="stVal" fc="ST" bType="INT32" dchg="true" dupd="true"/>
      <DA name="q" fc="ST" bType="Quality" qchg="true"/>
    </DOType>
  </DataTypeTemplates>
</SCL>`

// TestConvert_RCBIndexedLogControlTrgOps checks every schema converter
// for the ReportControl indexed/RptEnabled max (IEC 61850-6 §9.3.8), the
// LogControl attributes (§9.3.9 Table 26) and the DA trigger options
// (RPT-16, RPT-20, LOG-02).
func TestConvert_RCBIndexedLogControlTrgOps(t *testing.T) {
	for _, tc := range []struct {
		name, root, rcAttr, lcAttr string
		ed1                        bool
	}{
		{"2003", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL">`, "", "", true},
		{"2007B", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">`, ` indexed="false"`, ` ldInst="LD9" prefix="P" lnClass="GGIO" lnInst="2"`, false},
		{"2007B4", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4">`, ` indexed="false"`, ` ldInst="LD9" prefix="P" lnClass="GGIO" lnInst="2"`, false},
		{"2007C5", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="C" release="5">`, ` indexed="false"`, ` ldInst="LD9" prefix="P" lnClass="GGIO" lnInst="2"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := `<?xml version="1.0"?>` + tc.root + strings.Replace(strings.Replace(rcbLCBBody, "%s", tc.rcAttr, 1), "%s", tc.lcAttr, 1)
			s, err := Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			ln0 := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
			rc := ln0.Reports[0]
			if rc.Indexed != tc.ed1 || rc.MaxInstances != 3 {
				t.Errorf("RCB Indexed=%v MaxInstances=%d", rc.Indexed, rc.MaxInstances)
			}
			l := ln0.Logs[0]
			if l.Name != "lcb1" || l.LogName != "EventLog" || l.DatSet != "ds1" || l.IntgPd != 500 ||
				!l.LogEna || l.ReasonCode || !l.TrgOps.Dchg || !l.TrgOps.Period || !l.TrgOps.GI {
				t.Errorf("lcb1 = %+v", l)
			}
			if !tc.ed1 && (l.LDInst != "LD9" || l.Prefix != "P" || l.LNClass != "GGIO" || l.LNInst != "2") {
				t.Errorf("lcb1 location = %+v", l)
			}
			if l2 := s.IEDs[0].AccessPoints[0].Server.LDevices[0].LNs[0].Logs[0]; l2.LogEna || !l2.ReasonCode || l2.LogName != "L2" {
				t.Errorf("lcb2 = %+v", l2)
			}
			das := s.DataTypeTemplates.DOTypes[0].DAs
			if !das[0].Dchg || !das[0].Dupd || das[0].Qchg || !das[1].Qchg || das[1].Dchg {
				t.Errorf("DA trigger options = %+v", das)
			}
		})
	}
}

// TestGenerate_RCBLCBRoundTrip: Generate writes indexed, RptEnabled max,
// the LogControl attributes and the DA trigger options back.
func TestGenerate_RCBLCBRoundTrip(t *testing.T) {
	doc := `<?xml version="1.0"?><SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">` +
		strings.Replace(strings.Replace(rcbLCBBody, "%s", ` indexed="false"`, 1), "%s", ` ldInst="LD9"`, 1)
	s, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := Generate(&b, s); err != nil {
		t.Fatal(err)
	}
	back, err := Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, b.String())
	}
	ln0 := back.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	if rc := ln0.Reports[0]; rc.Indexed || rc.MaxInstances != 3 {
		t.Errorf("RCB after round trip = %+v", rc)
	}
	if l := ln0.Logs[0]; l.LogName != "EventLog" || l.LDInst != "LD9" || l.IntgPd != 500 || l.ReasonCode || !l.TrgOps.Period {
		t.Errorf("LCB after round trip = %+v", l)
	}
	if das := back.DataTypeTemplates.DOTypes[0].DAs; !das[0].Dchg || !das[1].Qchg || das[1].Dupd {
		t.Errorf("DA trigger options after round trip = %+v", das)
	}
}
