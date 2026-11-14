// SPDX-License-Identifier: MIT

package validate

import (
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// fuzzMaxInput bounds the documents the fuzz target parses.
const fuzzMaxInput = 64 << 10

// FuzzAll validates documents parsed from arbitrary input. All returns
// the index diagnostics first and then exactly the diagnostics of the
// passes in their documented order; with every pass skipped only the
// index diagnostics remain; every diagnostic has a known severity, a
// code and a message; validating twice gives the same result.
func FuzzAll(f *testing.F) {
	if b, err := os.ReadFile("../testdata/simple.scd"); err == nil {
		f.Add(b)
	}
	f.Add([]byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="h"/>
<IED name="I"><AccessPoint name="A"><Server><LDevice inst="L"><LN0 lnClass="LLN0" inst="" lnType="T">
<DataSet name="d"><FCDA ldInst="L" lnClass="LLN0" doName="Mod" fc="ST"/></DataSet>
<ReportControl name="r" datSet="d"/><GSEControl name="g" datSet="x" appID="a"/>
</LN0></LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T" lnClass="LLN0"><DO name="Mod" type="D"/></LNodeType>
<DOType id="D" cdc="INC"><SDO name="s" type="D"/><DA name="stVal" fc="ST" bType="Enum" type="E"/></DOType>
</DataTypeTemplates></SCL>`))
	f.Add([]byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL"/>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > fuzzMaxInput {
			return
		}
		res, err := scl.ParseBytes(b, scl.ParseOptions{})
		if err != nil || res.Document == nil {
			return
		}
		s := res.Document
		idx, idxDiags := index.Build(s)
		all := All(s, idx, idxDiags)
		var want []scl.Diagnostic
		want = append(want, idxDiags...)
		for _, pass := range []func(*scl.SCL, *index.Index) []scl.Diagnostic{
			Templates, IEDs, Communication, Addresses, Datasets, Controls, Topology,
		} {
			want = append(want, pass(s, idx)...)
		}
		want = append(want, scl.ValidateRequired(s)...)
		if !reflect.DeepEqual(all, want) {
			t.Fatalf("All is not the index diagnostics followed by the passes in order:\n%v\n%v", all, want)
		}
		for _, d := range all {
			if !slices.Contains([]scl.DiagSeverity{scl.DiagError, scl.DiagWarning, scl.DiagInfo}, d.Severity) || d.Code == "" || d.Message == "" {
				t.Fatalf("diagnostic %+v", d)
			}
		}
		none := WithOptions(s, idx, idxDiags, Options{SkipTemplates: true, SkipIEDs: true, SkipCommunication: true,
			SkipAddresses: true, SkipDatasets: true, SkipControls: true, SkipTopology: true, SkipRequired: true})
		if !reflect.DeepEqual(none, append([]scl.Diagnostic(nil), idxDiags...)) {
			t.Fatalf("every pass skipped: %v, want the index diagnostics %v", none, idxDiags)
		}
		if again := All(s, idx, idxDiags); !reflect.DeepEqual(again, all) {
			t.Fatal("two validations of one document differ")
		}
	})
}
