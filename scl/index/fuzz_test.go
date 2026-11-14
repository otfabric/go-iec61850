// SPDX-License-Identifier: MIT

package index

import (
	"os"
	"reflect"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// fuzzMaxInput bounds the documents the SCL fuzz targets parse.
const fuzzMaxInput = 64 << 10

const fuzzSmallSCL = `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="t"/>
  <IED name="IED1"><AccessPoint name="AP1"><Server><LDevice inst="LD1">
    <LN0 lnClass="LLN0" inst="" lnType="LLN0T"><DataSet name="ds"/><DataSet name="ds"/></LN0>
    <LN lnClass="GGIO" inst="1" lnType="GGIOT"/><LN lnClass="GGIO" inst="1" lnType="GGIOT"/>
  </LDevice></Server></AccessPoint></IED>
  <IED name="IED1"/>
  <DataTypeTemplates>
    <LNodeType id="LLN0T" lnClass="LLN0"><DO name="Mod" type="INC"/></LNodeType>
    <LNodeType id="GGIOT" lnClass="GGIO"><DO name="Ind1" type="SPS"/></LNodeType>
    <DOType id="SPS" cdc="SPS"><DA name="stVal" fc="ST" bType="BOOLEAN"/></DOType>
    <DOType id="SPS" cdc="SPS"/>
    <DOType id="INC" cdc="INC"><DA name="stVal" fc="ST" bType="INT32"/></DOType>
  </DataTypeTemplates>
</SCL>`

// FuzzBuild indexes documents parsed from arbitrary input. Every entry
// is keyed by the names of the element it points to, and is the last
// element of the document with that key; each duplicate IED, LDevice,
// LN, data set and template ID is reported once; building twice gives
// the same diagnostics.
func FuzzBuild(f *testing.F) {
	f.Add([]byte(fuzzSmallSCL))
	if b, err := os.ReadFile("../testdata/simple.scd"); err == nil {
		f.Add(b)
	}
	f.Add([]byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="h"/></SCL>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > fuzzMaxInput {
			return
		}
		res, err := scl.ParseBytes(b, scl.ParseOptions{})
		if err != nil || res.Document == nil {
			return
		}
		s := res.Document
		idx, diags := Build(s)
		checkIndex(t, s, idx, diags)
		_, again := Build(s)
		if !reflect.DeepEqual(diags, again) {
			t.Fatalf("diagnostics differ between two builds:\n%v\n%v", diags, again)
		}
	})
}

func checkIndex(t *testing.T, s *scl.SCL, idx *Index, diags []scl.Diagnostic) {
	t.Helper()
	codes := map[string]int{}
	for _, d := range diags {
		if d.Code == "" || d.Message == "" {
			t.Fatalf("diagnostic without code or message: %+v", d)
		}
		codes[d.Code]++
	}

	tmpl := s.DataTypeTemplates
	lastLNT, lastDOT, lastDAT, lastET := map[string]*scl.LNodeType{}, map[string]*scl.DOType{}, map[string]*scl.DAType{}, map[string]*scl.EnumType{}
	for i := range tmpl.LNodeTypes {
		lastLNT[tmpl.LNodeTypes[i].ID] = &tmpl.LNodeTypes[i]
	}
	for i := range tmpl.DOTypes {
		lastDOT[tmpl.DOTypes[i].ID] = &tmpl.DOTypes[i]
	}
	for i := range tmpl.DATypes {
		lastDAT[tmpl.DATypes[i].ID] = &tmpl.DATypes[i]
	}
	for i := range tmpl.EnumTypes {
		lastET[tmpl.EnumTypes[i].ID] = &tmpl.EnumTypes[i]
	}
	if !reflect.DeepEqual(idx.LNodeTypes, lastLNT) || !reflect.DeepEqual(idx.DOTypes, lastDOT) ||
		!reflect.DeepEqual(idx.DATypes, lastDAT) || !reflect.DeepEqual(idx.EnumTypes, lastET) {
		t.Fatal("template index is not the last template of each ID")
	}
	dupIDs := len(tmpl.LNodeTypes) - len(lastLNT) + len(tmpl.DOTypes) - len(lastDOT) +
		len(tmpl.DATypes) - len(lastDAT) + len(tmpl.EnumTypes) - len(lastET)
	if codes["duplicate-id"] != dupIDs {
		t.Fatalf("%d duplicate-id diagnostics for %d duplicate template IDs", codes["duplicate-id"], dupIDs)
	}

	lastIED := map[string]*scl.IED{}
	lastLD := map[LDeviceKey]*scl.LDevice{}
	lastLN := map[LNKey]*scl.LN{}
	lastDS := map[DataSetKey]*scl.DataSet{}
	lds, lns, dss := 0, 0, 0
	for i := range s.IEDs {
		ied := &s.IEDs[i]
		lastIED[ied.Name] = ied
		for j := range ied.AccessPoints {
			ap := &ied.AccessPoints[j]
			if got := idx.AccessPoints[AccessPointKey{IED: ied.Name, AP: ap.Name}]; got == nil {
				t.Fatalf("AccessPoint %s/%s not indexed", ied.Name, ap.Name)
			}
			if ap.Server == nil {
				continue
			}
			for k := range ap.Server.LDevices {
				ld := &ap.Server.LDevices[k]
				lds++
				lastLD[LDeviceKey{IED: ied.Name, LDInst: ld.Inst}] = ld
				visit := func(ln *scl.LN) {
					lns++
					lastLN[LNKey{IED: ied.Name, LDInst: ld.Inst, Prefix: ln.Prefix, LNClass: ln.LNClass, Inst: ln.Inst}] = ln
					for m := range ln.DataSets {
						dss++
						lastDS[DataSetKey{IED: ied.Name, LDInst: ld.Inst, Prefix: ln.Prefix, LNClass: ln.LNClass,
							LNInst: ln.Inst, Name: ln.DataSets[m].Name}] = &ln.DataSets[m]
					}
				}
				if ld.LN0 != nil {
					visit(ld.LN0)
				}
				for m := range ld.LNs {
					visit(&ld.LNs[m])
				}
			}
		}
	}
	for _, c := range []struct {
		code            string
		indexed, listed int
		same            bool
	}{
		{"duplicate-ied", len(idx.IEDs), len(s.IEDs), reflect.DeepEqual(idx.IEDs, lastIED)},
		{"duplicate-ld", len(idx.LDevices), lds, reflect.DeepEqual(idx.LDevices, lastLD)},
		{"duplicate-ln", len(idx.LNs), lns, reflect.DeepEqual(idx.LNs, lastLN)},
		{"duplicate-dataset", len(idx.DataSets), dss, reflect.DeepEqual(idx.DataSets, lastDS)},
	} {
		if !c.same {
			t.Fatalf("%s: index is not the last element of each key", c.code)
		}
		if codes[c.code] != c.listed-c.indexed {
			t.Fatalf("%d %s diagnostics for %d elements under %d keys", codes[c.code], c.code, c.listed, c.indexed)
		}
	}
}
