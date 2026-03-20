package scl

import (
	"strings"
	"testing"
)

func FuzzParse(f *testing.F) {
	f.Add(`<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="test" version="1" revision="0"/>
</SCL>`)

	f.Add(`<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="full" version="1" revision="0"/>
  <IED name="IED1">
    <AccessPoint name="AP1">
      <Server>
        <LDevice inst="LD1">
          <LN0 lnClass="LLN0" inst="" lnType="LNT1"/>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LNT1" lnClass="LLN0">
      <DO name="Mod" type="DOT1"/>
    </LNodeType>
    <DOType id="DOT1" cdc="INS">
      <DA name="stVal" fc="ST" bType="INT32"/>
    </DOType>
  </DataTypeTemplates>
</SCL>`)

	f.Add("")
	f.Add("<")
	f.Add("<SCL>")
	f.Add("not xml at all")

	f.Fuzz(func(t *testing.T, input string) {
		s, err := Parse(strings.NewReader(input))
		if err != nil {
			return
		}
		if s == nil {
			t.Error("Parse returned nil without error")
			return
		}
		_ = Validate(s)
		_ = Flatten(s)
		_ = ExportDataSets(s)
		_ = ExportReports(s)
	})
}
