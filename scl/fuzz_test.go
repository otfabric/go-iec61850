// SPDX-License-Identifier: MIT

package scl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// Limits of the fuzz run, small enough that every input stays cheap.
const (
	fuzzMaxNodes       = 2000
	fuzzMaxDiagnostics = 3
	fuzzMaxDiagName    = 16
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

	// Struct types referenced several times by the next one.
	f.Add(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"><Header id="x"/>
<IED name="I"><AccessPoint name="AP"><Server><LDevice inst="LD0"><LN0 lnClass="LLN0" inst="" lnType="T0"/><LN lnClass="GGIO" inst="1" lnType="none"/></LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T0" lnClass="LLN0"><DO name="D" type="DOT"/></LNodeType>
<DOType id="DOT" cdc="X"><DA name="a" fc="ST" bType="Struct" type="S0"/><SDO name="s" type="DOT"/></DOType>
<DAType id="S0"><BDA name="b0" bType="Struct" type="S1"/><BDA name="b1" bType="Struct" type="S1"/><BDA name="b2" bType="Struct" type="S1"/></DAType>
<DAType id="S1"><BDA name="b0" bType="Struct" type="S2"/><BDA name="b1" bType="Struct" type="S2"/><BDA name="b2" bType="Struct" type="S2"/></DAType>
<DAType id="S2"><BDA name="b0" bType="BOOLEAN"/><BDA name="b1" bType="BOOLEAN" count="x"/></DAType></DataTypeTemplates></SCL>`)

	f.Add("")
	f.Add("<")
	f.Add("<SCL>")
	f.Add("not xml at all")

	f.Fuzz(func(t *testing.T, input string) {
		s, err := scl.Parse(strings.NewReader(input))
		res, errBytes := scl.ParseBytes([]byte(input), scl.ParseOptions{})
		if (err == nil) != (errBytes == nil) {
			t.Fatalf("Parse: %v, ParseBytes: %v", err, errBytes)
		}
		if err != nil {
			return
		}
		if s == nil || res.Document == nil {
			t.Fatal("Parse returned nil without error")
		}

		// MaxBytes: the input itself is accepted, one byte less is refused.
		if len(input) > 1 { // MaxBytes 0 is the default
			if _, err := scl.ParseBytes([]byte(input), scl.ParseOptions{MaxBytes: int64(len(input))}); err != nil {
				t.Errorf("MaxBytes = input length: %v", err)
			}
			if _, err := scl.ParseBytes([]byte(input), scl.ParseOptions{MaxBytes: int64(len(input) - 1)}); !errors.Is(err, scl.ErrLimitExceeded) {
				t.Errorf("MaxBytes = input length - 1: %v, want ErrLimitExceeded", err)
			}
		}

		// MaxDiagnostics keeps the first diagnostics of the full list;
		// quoted names are cut.
		full, err := scl.ParseBytes([]byte(input), scl.ParseOptions{ValidateSemantic: true})
		if err != nil {
			t.Fatalf("ValidateSemantic: %v", err)
		}
		few, err := scl.ParseBytes([]byte(input), scl.ParseOptions{ValidateSemantic: true, MaxDiagnostics: fuzzMaxDiagnostics, MaxDiagnosticName: fuzzMaxDiagName})
		if err != nil {
			t.Fatalf("MaxDiagnostics: %v", err)
		}
		if len(few.Diagnostics) != min(len(full.Diagnostics), fuzzMaxDiagnostics) {
			t.Errorf("%d diagnostics of %d with MaxDiagnostics %d", len(few.Diagnostics), len(full.Diagnostics), fuzzMaxDiagnostics)
		}
		for i, d := range few.Diagnostics {
			if d.Code != full.Diagnostics[i].Code || d.Severity != full.Diagnostics[i].Severity {
				t.Errorf("diagnostic %d: %s, want %s", i, d.Code, full.Diagnostics[i].Code)
			}
		}

		_ = scl.Validate(s)
		_ = scl.ExportDataSets(s)
		_ = scl.ExportReports(s)

		// Flatten under a small limit: at most the limit, the stop
		// marked; within the limit the rows of Flatten.
		rows, diags := scl.FlattenWithOptions(s, scl.FlattenOptions{MaxExpandedNodes: fuzzMaxNodes})
		if len(rows) > fuzzMaxNodes+1 {
			t.Errorf("%d rows for a limit of %d nodes", len(rows), fuzzMaxNodes)
		}
		if len(diags) > 0 {
			if diags[0].Code != "expansion-limit" || len(rows) == 0 || rows[len(rows)-1].Status != "expansion-limit" {
				t.Errorf("stopped expansion: diagnostics %v, last row %+v", diags, rows[len(rows)-1])
			}
		} else if all := scl.Flatten(s); len(all) != len(rows) {
			t.Errorf("FlattenWithOptions: %d rows, Flatten: %d", len(rows), len(all))
		}

		// The server model of each IED stays within the same limit.
		for _, ied := range s.IEDs {
			m, err := servermodel.FromSCLWithOptions(s, ied.Name, "", servermodel.Options{MaxExpandedNodes: fuzzMaxNodes})
			if err != nil {
				continue
			}
			if n := modelNodes(m); n > fuzzMaxNodes {
				t.Errorf("IED %q: model of %d nodes for a limit of %d", ied.Name, n, fuzzMaxNodes)
			}
		}
	})
}

// modelNodes counts the data objects and attributes of m.
func modelNodes(m *servermodel.Model) int {
	var attr func(a *servermodel.DataAttribute) int
	attr = func(a *servermodel.DataAttribute) int {
		n := 1
		for i := range a.Children {
			n += attr(&a.Children[i])
		}
		return n
	}
	var obj func(o *servermodel.DataObject) int
	obj = func(o *servermodel.DataObject) int {
		n := 1
		for i := range o.Children {
			n += obj(&o.Children[i])
		}
		for i := range o.Attributes {
			n += attr(&o.Attributes[i])
		}
		return n
	}
	total := 0
	for _, ld := range m.LogicalDevices {
		for _, ln := range ld.LogicalNodes {
			for i := range ln.DataObjects {
				total += obj(&ln.DataObjects[i])
			}
		}
	}
	return total
}
