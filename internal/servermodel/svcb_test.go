// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"strings"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

func TestSvOptFlds(t *testing.T) {
	for _, o := range []SvOptFlds{{}, {RefreshTime: true, SampleRate: true}, {true, true, true, true, true}} {
		v := o.BitString()
		if n, _ := v.BitStringLength(); n != 5 {
			t.Fatalf("length %d", n)
		}
		if back, ok := SvOptFldsFromValue(v); !ok || back != o {
			t.Errorf("%+v -> %+v", o, back)
		}
	}
	if b, _ := (SvOptFlds{SampleRate: true}).BitString().BitString(); b[0] != 0x20 {
		t.Errorf("sample-rate is bit 2: %08b", b[0])
	}
	if _, ok := SvOptFldsFromValue(mms.NewInteger(1)); ok {
		t.Error("integer accepted")
	}
	for s, want := range map[string]uint8{"": 0, "SmpPerPeriod": 0, "SmpPerSec": 1, "SecPerSmp": 2} {
		if m, ok := SmpModFromSCL(s); !ok || m != want {
			t.Errorf("%q -> %d", s, m)
		}
	}
	if _, ok := SmpModFromSCL("x"); ok {
		t.Error("unknown smpMod accepted")
	}
	if SvCBItemID("LLN0", "MSVCB01") != "LLN0$MS$MSVCB01" {
		t.Error("item ID")
	}
}

func TestConvertSvCBWarnings(t *testing.T) {
	addr := &scl.SMVAddress{Address: []scl.P{{Type: "MAC-Address", Value: "01-0C-CD-04-00-01"}, {Type: "APPID", Value: "4000"}, {Type: "VLAN-PRIORITY", Value: "4"}}}
	var w []string
	d := convertSvCB(&scl.SMVControl{Name: "a", SmvID: "id", SmpRate: 80, NofASDU: 2, SmpMod: "SmpPerSec"}, addr, "LD/LLN0", &w)
	if !d.HasAddress || !d.HasVLAN || d.AppID != 0x4000 || d.NoASDU != 2 || d.SmpMod != 1 || len(w) != 0 {
		t.Fatalf("%+v %v", d, w)
	}
	cases := []struct {
		sc   scl.SMVControl
		addr *scl.SMVAddress
		msg  string
	}{
		{scl.SMVControl{Name: "b", SmpRate: 1 << 20, NofASDU: 1}, addr, "smpRate"},
		{scl.SMVControl{Name: "c", SmpRate: 80, NofASDU: 1 << 20}, addr, "nofASDU"},
		{scl.SMVControl{Name: "d", SmpRate: 80, SmpMod: "x"}, addr, "smpMod"},
		{scl.SMVControl{Name: "e", SmpRate: 80}, nil, "no SMV address"},
		{scl.SMVControl{Name: "f", SmpRate: 80}, &scl.SMVAddress{}, "missing MAC"},
	}
	for _, c := range cases {
		w = nil
		d := convertSvCB(&c.sc, c.addr, "LD/LLN0", &w)
		if len(w) != 1 || !strings.Contains(w[0], c.msg) {
			t.Errorf("%s: %v", c.sc.Name, w)
		}
		if d.NoASDU != 1 {
			t.Errorf("%s: noASDU %d", c.sc.Name, d.NoASDU)
		}
	}
	convertSvCB(&scl.SMVControl{Name: "g"}, nil, "x", nil) // nil warnings
}

func TestValidateSvCBs(t *testing.T) {
	for i, s := range [][]SvCBDef{
		{{Name: ""}},
		{{Name: "a"}, {Name: "a"}},
		{{Name: "a", DatSet: "missing"}},
	} {
		m := gocbModel()
		m.LogicalDevices[0].LogicalNodes[0].SvCBs = s
		if errs := m.Validate(); len(errs) == 0 {
			t.Errorf("case %d validated", i)
		}
	}
}

func TestFromSCLAndRegisterSvCBs(t *testing.T) {
	doc, err := scl.ParseFile("../../sv/testdata/mu-9-2le.icd")
	if err != nil {
		t.Fatal(err)
	}
	m, err := FromSCL(doc, "MUIED", "")
	if err != nil {
		t.Fatal(err)
	}
	ln := m.LogicalDevices[0].LogicalNodes[0]
	if len(ln.SvCBs) != 3 || ln.SvCBs[0].AppID != 0x4000 || !ln.SvCBs[0].HasVLAN || ln.SvCBs[1].NoASDU != 8 || ln.SvCBs[2].HasAddress {
		t.Fatalf("%+v", ln.SvCBs)
	}
	n, err := m.ResolveMember("MU01", DataSetMemberDef{LNName: "ITCTR1", DOPath: "AmpSv", FC: "MX"})
	if err != nil {
		t.Fatal(err)
	}
	if l := n.Leaves(); len(l) != 2 || l[0].BType != "INT32" || l[1].BType != "Quality" {
		t.Fatalf("leaves %+v", l)
	}

	srv := mms.NewServer(mms.ServerOptions{MMS: mms.ServerMMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	}})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })
	read := func(item string) *mms.Value {
		t.Helper()
		r, err := client.Read(ctx, mms.ReadRequest{DomainID: "MU01", ItemID: mms.ItemID(item)})
		if err != nil || r == nil || r.Value == nil {
			t.Fatalf("read %s: %v", item, err)
		}
		return r.Value
	}
	if e, ok := read("LLN0$MS$MSVCB01").Structure(); !ok || len(e) != 9 {
		t.Fatalf("MSVCB01 = %v", e)
	}
	if e, ok := read("LLN0$MS").Structure(); !ok || len(e) != 3 {
		t.Fatalf("MS group = %v", e)
	}
	if id, _ := read("LLN0$MS$MSVCB02$MsvID").VisibleString(); id != "MU01-256" {
		t.Errorf("MsvID %q", id)
	}
	if ds, _ := read("LLN0$MS$MSVCB01$DatSet").VisibleString(); ds != "MU01/LLN0$PhsMeas1" {
		t.Errorf("DatSet %q", ds)
	}
	if u, _ := read("LLN0$MS$MSVCB01$DstAddress$APPID").Uint64(); u != 0x4000 {
		t.Errorf("APPID %#x", u)
	}
	// Without the interceptor, writable attributes are stored directly.
	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "MU01", ItemID: "LLN0$MS$MSVCB01$SmpRate", Value: mms.NewUnsigned(256)}); err != nil {
		t.Fatal(err)
	}
	if u, _ := vs.Get(StoreKey("MU01", "LLN0$MS$MSVCB01$SmpRate")).Uint64(); u != 256 {
		t.Errorf("SmpRate %d", u)
	}
	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "MU01", ItemID: "LLN0$MS$MSVCB01$noASDU", Value: mms.NewUnsigned(2)}); err == nil {
		t.Error("noASDU write accepted")
	}
}
