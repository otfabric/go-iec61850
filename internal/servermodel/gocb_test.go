// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"

	"strings"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

func TestParseGSEAddress(t *testing.T) {
	ok := []scl.P{
		{Type: "MAC-Address", Value: "01-0C-CD-01-00-01"},
		{Type: "APPID", Value: "1001"},
		{Type: "VLAN-ID", Value: "0x00A"},
		{Type: "VLAN-PRIORITY", Value: "4"},
		{Type: "Other", Value: "ignored"},
	}
	a, err := ParseGSEAddress(ok)
	if err != nil || a.Dst != [6]byte{1, 0x0c, 0xcd, 1, 0, 1} || a.AppID != 0x1001 || a.VLANID != 10 || a.VLANPriority != 4 || !a.HasVLAN {
		t.Fatalf("%+v %v", a, err)
	}
	a, err = ParseGSEAddress([]scl.P{{Type: "MAC-Address", Value: "01:0c:cd:01:00:02"}, {Type: "APPID", Value: "0x3FFF"}})
	if err != nil || a.HasVLAN || a.AppID != 0x3fff {
		t.Fatalf("%+v %v", a, err)
	}
	bad := [][]scl.P{
		{{Type: "MAC-Address", Value: "01-0C-CD"}, {Type: "APPID", Value: "1"}},
		{{Type: "MAC-Address", Value: "01-0C-CD-01-00-ZZ"}, {Type: "APPID", Value: "1"}},
		{{Type: "MAC-Address", Value: "01-0C-CD-01-00-01"}, {Type: "APPID", Value: "xyz"}},
		{{Type: "MAC-Address", Value: "01-0C-CD-01-00-01"}, {Type: "APPID", Value: "1"}, {Type: "VLAN-ID", Value: "1000"}},
		{{Type: "MAC-Address", Value: "01-0C-CD-01-00-01"}, {Type: "APPID", Value: "1"}, {Type: "VLAN-PRIORITY", Value: "8"}},
		{{Type: "APPID", Value: "1"}},
		{{Type: "MAC-Address", Value: "01-0C-CD-01-00-01"}},
	}
	for i, ps := range bad {
		if _, err := ParseGSEAddress(ps); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestParseMillis(t *testing.T) {
	for in, want := range map[string]uint32{"": 0, "10": 10, " 2.6 ": 3} {
		if got, err := parseMillis(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"-1", "x", "1e12"} {
		if _, err := parseMillis(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestConvertGoCBWarnings(t *testing.T) {
	gc := &scl.GSEControl{Name: "g", AppID: "id", DatSet: "ds", ConfRev: 3, FixedOffs: true}
	var w []string
	if d := convertGoCB(gc, nil, "LD/LLN0", &w); d.HasAddress || d.ConfRev != 3 || !d.FixedOffs || len(w) != 1 {
		t.Fatalf("%+v %v", d, w)
	}
	w = nil
	convertGoCB(gc, &scl.GSEAddress{Address: []scl.P{{Type: "APPID", Value: "1"}}}, "LD/LLN0", &w)
	if len(w) != 1 || !strings.Contains(w[0], "MAC-Address") {
		t.Fatalf("%v", w)
	}
	w = nil
	gse := &scl.GSEAddress{
		Address: []scl.P{{Type: "MAC-Address", Value: "01-0C-CD-01-00-01"}, {Type: "APPID", Value: "1"}},
		MinTime: "bad", MaxTime: "-3",
	}
	if d := convertGoCB(gc, gse, "LD/LLN0", &w); !d.HasAddress || len(w) != 2 {
		t.Fatalf("%+v %v", d, w)
	}
	convertGoCB(gc, nil, "x", nil) // nil warnings tolerated
}

func TestQualifiedDatSetAndItemID(t *testing.T) {
	if GoCBItemID("LLN0", "g") != "LLN0$GO$g" {
		t.Error("item id")
	}
	if QualifiedDatSet("LD", "LLN0", "") != "" || QualifiedDatSet("LD", "LLN0", "X/Y$z") != "X/Y$z" || QualifiedDatSet("LD", "LLN0", "ds") != "LD/LLN0$ds" {
		t.Error("qualified")
	}
}

func gocbModel() *Model {
	return &Model{LogicalDevices: []LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []LogicalNode{
			{
				Name: "LLN0", LNClass: "LLN0",
				DataObjects: []DataObject{{Name: "Mod", Attributes: []DataAttribute{{Name: "stVal", FC: "ST", BType: "INT32"}}}},
				DataSets: []DataSetDef{{Name: "ds", Members: []DataSetMemberDef{
					{LNName: "GGIO1", DOPath: "Ind", FC: "ST"},
					{LNName: "GGIO1", DOPath: "AnIn.mag.f", FC: "MX"},
				}}},
				GoCBs: []GoCBDef{
					{Name: "gA", DatSet: "ds", ConfRev: 1, HasAddress: true, DstAddr: [6]byte{1, 0x0c, 0xcd, 1, 0, 1}, AppID: 1, VLANPriority: 4, MinTime: 10, MaxTime: 1000},
					{Name: "gB", GoID: "custom"},
				},
			},
			{
				Name: "GGIO1", LNClass: "GGIO",
				DataObjects: []DataObject{
					{Name: "Ind", Attributes: []DataAttribute{
						{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "d", FC: "DC", BType: "VisString255"},
					}, Children: []DataObject{{Name: "sub", Attributes: []DataAttribute{{Name: "x", FC: "ST", BType: "INT32"}}}}},
					{Name: "AnIn", Attributes: []DataAttribute{
						{Name: "mag", FC: "MX", BType: "Struct", Children: []DataAttribute{{Name: "f", BType: "FLOAT32"}}},
						{Name: "weird", FC: "MX", BType: "Struct", Children: []DataAttribute{{Name: "z", BType: "NOPE"}}},
						{Name: "bad", FC: "MX", BType: "NOPE"},
					}},
				},
			},
		},
	}}}
}

func TestResolveMember(t *testing.T) {
	m := gocbModel()
	ds, ln := m.FindDataSet("LD1", "ds")
	if ds == nil || ln.Name != "LLN0" {
		t.Fatal("FindDataSet local")
	}
	if ds2, _ := m.FindDataSet("LD1", "LD1/LLN0$ds"); ds2 != ds {
		t.Fatal("FindDataSet qualified")
	}
	if d, _ := m.FindDataSet("LD2", "ds"); d != nil {
		t.Fatal("wrong LD")
	}
	if d, _ := m.FindDataSet("LD1", "nope"); d != nil {
		t.Fatal("missing")
	}

	n, err := m.ResolveMember("LD1", ds.Members[0])
	if err != nil {
		t.Fatal(err)
	}
	// DO-level [ST]: stVal, q, then sub-DO sub{x}; DC attribute excluded.
	keys := n.Keys()
	want := []string{"LD1/GGIO1$ST$Ind$stVal", "LD1/GGIO1$ST$Ind$q", "LD1/GGIO1$ST$Ind$sub$x"}
	if strings.Join(keys, ",") != strings.Join(want, ",") || n.Type.Type != mms.ValueTypeStructure {
		t.Fatalf("keys %v", keys)
	}
	n2, err := m.ResolveMember("LD1", ds.Members[1])
	if err != nil || n2.Key != "LD1/GGIO1$MX$AnIn$mag$f" || n2.Type.Type != mms.ValueTypeFloat {
		t.Fatalf("%+v %v", n2, err)
	}
	if n3, err := m.ResolveMember("LD1", DataSetMemberDef{LDInst: "LD1", LNName: "GGIO1", DOPath: "AnIn.mag", FC: "MX"}); err != nil || len(n3.Children) != 1 {
		t.Fatalf("%+v %v", n3, err)
	}
	if n4, err := m.ResolveMember("LD1", DataSetMemberDef{LNName: "GGIO1", DOPath: "Ind.sub", FC: "ST"}); err != nil || len(n4.Keys()) != 1 {
		t.Fatalf("%+v %v", n4, err)
	}

	vs := NewValueStore()
	v := vs.ReadMember(n)
	if e, ok := v.Structure(); !ok || len(e) != 3 {
		t.Fatalf("ReadMember %v", v)
	}
	vs.Set(want[0], mms.NewBoolean(true))
	if e, _ := vs.ReadMember(n).Structure(); e[0].String() == "" {
		t.Fatal("stored value")
	}

	for i, mem := range []DataSetMemberDef{
		{LNName: "GGIO9", DOPath: "Ind", FC: "ST"},
		{LNName: "GGIO1", DOPath: "Nope", FC: "ST"},
		{LNName: "GGIO1", DOPath: "Ind", FC: "CO"},
		{LNName: "GGIO1", DOPath: "Ind.nope", FC: "ST"},
		{LNName: "GGIO1", DOPath: "AnIn.mag.nope", FC: "MX"},
		{LNName: "GGIO1", DOPath: "AnIn.bad", FC: "MX"},
		{LNName: "GGIO1", DOPath: "AnIn.weird", FC: "MX"},
	} {
		if _, err := m.ResolveMember("LD1", mem); err == nil {
			t.Errorf("case %d resolved", i)
		}
	}
}

func TestRegisterGoCB(t *testing.T) {
	m := gocbModel()
	anIn := &m.LogicalDevices[0].LogicalNodes[1].DataObjects[1]
	anIn.Attributes = anIn.Attributes[:1] // drop the unsupported types
	srv := mms.NewServer(mms.ServerOptions{MMS: mms.ServerMMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	}})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := vs.Get(StoreKey("LD1", "LLN0$GO$gB$GoID")).VisibleString(); id != "custom" {
		t.Errorf("gB GoID %q", id)
	}
	if ds, _ := vs.Get(StoreKey("LD1", "LLN0$GO$gA$DatSet")).VisibleString(); ds != "LD1/LLN0$ds" {
		t.Errorf("gA DatSet %q", ds)
	}
	if nds, _ := vs.Get(StoreKey("LD1", "LLN0$GO$gB$NdsCom")).Bool(); !nds {
		t.Error("gB without address must have NdsCom")
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
		r, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: mms.ItemID(item)})
		if err != nil || r == nil || r.Value == nil {
			t.Fatalf("read %s: %v", item, err)
		}
		return r.Value
	}
	write := func(item string, v *mms.Value) error {
		_, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: mms.ItemID(item), Value: v})
		return err
	}
	if e, ok := read("LLN0$GO$gA").Structure(); !ok || len(e) != 9 {
		t.Fatalf("gA = %v", e)
	}
	if e, ok := read("LLN0$GO").Structure(); !ok || len(e) != 2 {
		t.Fatalf("GO group = %v", e)
	}
	if e, ok := read("LLN0").Structure(); !ok || len(e) == 0 {
		t.Fatal("LLN0")
	}
	if u, _ := read("LLN0$GO$gA$DstAddress$APPID").Uint64(); u != 1 {
		t.Errorf("APPID %d", u)
	}
	if u, _ := read("LLN0$GO$gA$DstAddress$PRIORITY").Uint64(); u != 4 {
		t.Errorf("PRIORITY %d", u)
	}
	// Defaults are substituted for cleared values.
	vs.Set(StoreKey("LD1", "LLN0$GO$gA$DstAddress"), nil)
	vs.Set(StoreKey("LD1", "LLN0$GO$gA$MinTime"), nil)
	read("LLN0$GO$gA$DstAddress$Addr")
	read("LLN0$GO$gA$MinTime")
	read("LLN0$GO$gA")

	if err := write("LLN0$GO$gA$GoEna", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	if b, _ := vs.Get(StoreKey("LD1", "LLN0$GO$gA$GoEna")).Bool(); !b {
		t.Error("GoEna not stored")
	}
	for _, c := range []struct {
		item string
		v    *mms.Value
	}{
		{"LLN0$GO$gA$GoEna", mms.NewInteger(1)},
		{"LLN0$GO$gA$GoID", mms.NewVisibleString(strings.Repeat("x", 200))},
		{"LLN0$GO$gA$ConfRev", mms.NewUnsigned(5)},
		{"LLN0$GO$gA$DstAddress$VID", mms.NewUnsigned(5)},
		{"LLN0$GO$gA", read("LLN0$GO$gA")},
	} {
		if err := write(c.item, c.v); err == nil {
			t.Errorf("write %s accepted", c.item)
		}
	}
	var seen string
	vs.SetWriteInterceptor(func(_ context.Context, key string, _ *mms.Value) (bool, error) {
		seen = key
		return true, &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	})
	if err := write("LLN0$GO$gA$GoID", mms.NewVisibleString("y")); err == nil || seen != "LD1/LLN0$GO$gA$GoID" {
		t.Errorf("interceptor: %v %q", err, seen)
	}
}

func TestValidateGoCBs(t *testing.T) {
	for i, g := range [][]GoCBDef{
		{{Name: ""}},
		{{Name: "a"}, {Name: "a"}},
		{{Name: "a", DatSet: "missing"}},
	} {
		m := gocbModel()
		m.LogicalDevices[0].LogicalNodes[0].GoCBs = g
		if errs := m.Validate(); len(errs) == 0 {
			t.Errorf("case %d validated", i)
		}
	}
}

func TestFromSCLGoCBs(t *testing.T) {
	doc, err := scl.ParseFile("../../goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	m, err := FromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	ln := m.LogicalDevices[0].LogicalNodes[0]
	if len(ln.GoCBs) != 2 || ln.GoCBs[0].AppID != 0x1001 || ln.GoCBs[0].MaxTime != 1000 || !ln.GoCBs[0].HasVLAN {
		t.Fatalf("%+v", ln.GoCBs)
	}
	// Without Communication every GoCB is address-less, with warnings.
	doc.Communication = nil
	m, err = FromSCL(doc, "InteropIED", "")
	if err != nil || m.LogicalDevices[0].LogicalNodes[0].GoCBs[0].HasAddress {
		t.Fatal(err)
	}
	if len(m.Warnings) == 0 {
		t.Error("expected warnings")
	}
}

func FuzzParseGSEAddress(f *testing.F) {
	f.Add("01-0C-CD-01-00-01", "1001", "000", "4", "10.5")
	f.Fuzz(func(t *testing.T, mac, appID, vid, prio, ms string) {
		a, err := ParseGSEAddress([]scl.P{{Type: "MAC-Address", Value: mac}, {Type: "APPID", Value: appID}, {Type: "VLAN-ID", Value: vid}, {Type: "VLAN-PRIORITY", Value: prio}})
		if err == nil && (a.VLANID > 0xfff || a.VLANPriority > 7) {
			t.Fatalf("out of range: %+v", a)
		}
		_, _ = parseMillis(ms)
	})
}
