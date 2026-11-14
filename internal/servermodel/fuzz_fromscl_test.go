// SPDX-License-Identifier: MIT

package servermodel

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

const (
	// fuzzMaxInput bounds the documents the fuzz target parses.
	fuzzMaxInput = 32 << 10
	// fuzzMaxNodes bounds the data objects and attributes a document may
	// expand to before the fuzz target builds a model from it, so that
	// one input cannot take the run's memory.
	fuzzMaxNodes = 100_000
)

// FuzzFromSCL builds server models from documents parsed from
// arbitrary input, for the first IED. An error names the package; a
// model has one logical device per LDevice of the server, in order,
// named LDevice@ldName or the IED name and the instance (or the instance
// alone with InstanceDomainNames), with no name twice; building twice
// gives the same model.
func FuzzFromSCL(f *testing.F) {
	for _, p := range []string{"../../scl/testdata/simple.scd", "../../testdata/setting-groups.cid"} {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="h"/>
<IED name="I"><AccessPoint name="A"><Server><LDevice inst="L" ldName="N"><LN0 lnClass="LLN0" inst="" lnType="T"/></LDevice>
<LDevice inst="M"><LN0 lnClass="LLN0" inst="" lnType="T"/></LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T" lnClass="LLN0"><DO name="Mod" type="D"/></LNodeType>
<DOType id="D" cdc="INC"><SDO name="s" type="D"/><DA name="stVal" fc="ST" bType="Struct" type="S"/></DOType>
<DAType id="S"><BDA name="b" bType="Struct" type="S"/><BDA name="i" bType="INT32"/></DAType>
</DataTypeTemplates></SCL>`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > fuzzMaxInput {
			return
		}
		res, err := scl.ParseBytes(b, scl.ParseOptions{})
		if err != nil || res.Document == nil || len(res.Document.IEDs) == 0 {
			return
		}
		s := res.Document
		ied := &s.IEDs[0]
		if expandedNodes(s, ied, fuzzMaxNodes) > fuzzMaxNodes {
			return
		}
		for _, inst := range []bool{false, true} {
			m, err := FromSCLWithOptions(s, ied.Name, "", Options{InstanceDomainNames: inst})
			if err != nil {
				if !strings.HasPrefix(err.Error(), "servermodel: ") {
					t.Fatalf("error %q does not name the package", err)
				}
				continue
			}
			checkModel(t, ied, m, inst)
			again, err := FromSCLWithOptions(s, ied.Name, "", Options{InstanceDomainNames: inst})
			if err != nil || !reflect.DeepEqual(m, again) {
				t.Fatalf("a second build differs (%v)", err)
			}
		}
	})
}

func checkModel(t *testing.T, ied *scl.IED, m *Model, inst bool) {
	t.Helper()
	srv, _, err := findServer(ied, "")
	if err != nil || srv == nil {
		t.Fatalf("model built without a single Server: %v", err)
	}
	if len(m.LogicalDevices) != len(srv.LDevices) {
		t.Fatalf("%d logical devices for %d LDevices", len(m.LogicalDevices), len(srv.LDevices))
	}
	seen := map[string]bool{}
	for i, ld := range m.LogicalDevices {
		want := srv.LDevices[i].Inst
		if !inst {
			if srv.LDevices[i].LdName != "" {
				want = srv.LDevices[i].LdName
			} else {
				want = ied.Name + srv.LDevices[i].Inst
			}
		}
		if ld.Inst != srv.LDevices[i].Inst || ld.Name != want {
			t.Fatalf("logical device %d: %q (inst %q), want %q (inst %q)", i, ld.Name, ld.Inst, want, srv.LDevices[i].Inst)
		}
		if seen[ld.Name] {
			t.Fatalf("logical device name %q twice", ld.Name)
		}
		seen[ld.Name] = true
	}
}

// expandedNodes counts the data objects and attributes FromSCL expands
// for the logical nodes of ied, following type references as expandDO
// and expandDA do (a type already on the path is not entered again). It
// stops counting once the count exceeds budget.
func expandedNodes(s *scl.SCL, ied *scl.IED, budget int) int {
	tmpl, _ := index.Build(s)
	n := 0
	var da func(btype, typ string, visited map[string]bool)
	da = func(btype, typ string, visited map[string]bool) {
		n++
		dat := tmpl.DATypes[typ]
		if n > budget || btype != "Struct" || dat == nil || visited[typ] {
			return
		}
		visited[typ] = true
		defer delete(visited, typ)
		for _, b := range dat.BDAs {
			da(b.BType, b.Type, visited)
		}
	}
	var do func(typ string, visited map[string]bool)
	do = func(typ string, visited map[string]bool) {
		n++
		dot := tmpl.DOTypes[typ]
		if n > budget || dot == nil || visited[typ] {
			return
		}
		visited[typ] = true
		defer delete(visited, typ)
		for _, sdo := range dot.SDOs {
			do(sdo.Type, visited)
		}
		for _, a := range dot.DAs {
			da(a.BType, a.Type, map[string]bool{})
		}
	}
	ln := func(l *scl.LN) {
		if lnt := findLNodeType(s, l.LNType); lnt != nil {
			for _, d := range lnt.DOs {
				do(d.Type, map[string]bool{})
			}
		}
	}
	for i := range ied.AccessPoints {
		srv := ied.AccessPoints[i].Server
		if srv == nil {
			continue
		}
		for j := range srv.LDevices {
			ld := &srv.LDevices[j]
			if ld.LN0 != nil {
				ln(ld.LN0)
			}
			for k := range ld.LNs {
				ln(&ld.LNs[k])
			}
		}
	}
	return n
}
