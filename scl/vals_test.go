// SPDX-License-Identifier: MIT

package scl_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// valsSCL has a setting with a value per setting group on a DAI, a DA
// with two values and a BDA with one, for the given root attributes.
const valsSCL = `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL"%s>
  <Header id="vals"/>
  <IED name="IED1">
    <AccessPoint name="AP1">
      <Server>
        <Authentication/>
        <LDevice inst="LD0">
          <LN0 lnClass="LLN0" inst="" lnType="LLN0_T"/>
          <LN lnClass="PTOC" inst="1" lnType="PTOC_T">
            <DOI name="StrVal">
              <SDI name="setMag">
                <DAI name="f"><Val sGroup="1">100</Val><Val sGroup="2">200</Val></DAI>
              </SDI>
            </DOI>
            <DOI name="Beh"><DAI name="stVal"><Val>on</Val></DAI></DOI>
          </LN>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0_T" lnClass="LLN0"/>
    <LNodeType id="PTOC_T" lnClass="PTOC">
      <DO name="StrVal" type="ASG_T"/>
      <DO name="Beh" type="ENS_T"/>
    </LNodeType>
    <DOType id="ASG_T" cdc="ASG">
      <DA name="setMag" bType="Struct" type="AV_T" fc="SG"/>
      <DA name="d" bType="VisString255" fc="DC"><Val sGroup="1">one</Val><Val sGroup="2">two</Val></DA>
    </DOType>
    <DOType id="ENS_T" cdc="ENS">
      <DA name="stVal" bType="Enum" type="Beh_T" fc="ST"/>
    </DOType>
    <DAType id="AV_T"><BDA name="f" bType="FLOAT32"><Val>50</Val></BDA></DAType>
    <EnumType id="Beh_T"><EnumVal ord="1">on</EnumVal></EnumType>
  </DataTypeTemplates>
</SCL>`

func TestSCLKeepsEveryVal(t *testing.T) {
	for _, root := range []string{
		``, // 1.7
		` version="2007" revision="B"`,
		` version="2007" revision="B" release="4"`,
		` version="2007" revision="C" release="5"`,
	} {
		t.Run(root, func(t *testing.T) {
			res, err := scl.ParseBytes([]byte(fmt.Sprintf(valsSCL, root)), scl.ParseOptions{})
			if err != nil {
				t.Fatal(err)
			}
			checkVals(t, res.Document)

			// Generate writes every value with its setting group.
			var buf bytes.Buffer
			if err := scl.Generate(&buf, res.Document); err != nil {
				t.Fatal(err)
			}
			again, err := scl.ParseBytes(buf.Bytes(), scl.ParseOptions{})
			if err != nil {
				t.Fatalf("parse generated: %v\n%s", err, buf.String())
			}
			checkVals(t, again.Document)
		})
	}
}

func checkVals(t *testing.T, doc *scl.SCL) {
	t.Helper()
	ln := doc.IEDs[0].AccessPoints[0].Server.LDevices[0].LNs[0]
	dai := ln.DOIs[0].SDIs[0].DAIs[0]
	want := []scl.Value{{Value: "100", SGroup: 1}, {Value: "200", SGroup: 2}}
	if dai.Val != "100" || !slices.Equal(dai.Vals, want) {
		t.Errorf("DAI = %+v, want Val 100 and %+v", dai, want)
	}
	if beh := ln.DOIs[1].DAIs[0]; beh.Val != "on" || !slices.Equal(beh.Vals, []scl.Value{{Value: "on"}}) {
		t.Errorf("Beh DAI = %+v", beh)
	}
	var da scl.DA
	for _, dot := range doc.DataTypeTemplates.DOTypes {
		for _, d := range dot.DAs {
			if d.Name == "d" {
				da = d
			}
		}
	}
	if da.Val != "one" || !slices.Equal(da.Vals, []scl.Value{{Value: "one", SGroup: 1}, {Value: "two", SGroup: 2}}) {
		t.Errorf("DA = %+v", da)
	}
	bda := doc.DataTypeTemplates.DATypes[0].BDAs[0]
	if bda.Val != "50" || !slices.Equal(bda.Vals, []scl.Value{{Value: "50"}}) {
		t.Errorf("BDA = %+v", bda)
	}
}

func TestGenerateValWithoutVals(t *testing.T) {
	// A model built in code with Val only still writes its value, and an
	// attribute without a value writes no empty Val element.
	doc := &scl.SCL{
		Header: scl.Header{ID: "x"},
		DataTypeTemplates: scl.DataTypeTemplates{
			DOTypes: []scl.DOType{{ID: "T", CDC: "SPS", DAs: []scl.DA{
				{Name: "stVal", BType: "BOOLEAN", FC: "ST", Val: "true"},
				{Name: "q", BType: "Quality", FC: "ST"},
			}}},
		},
	}
	var buf bytes.Buffer
	if err := scl.Generate(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte(`<Val>true</Val>`)) || bytes.Count(buf.Bytes(), []byte(`<Val`)) != 1 {
		t.Errorf("generated:\n%s", out)
	}
}
