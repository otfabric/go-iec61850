// SPDX-License-Identifier: MIT

package servermodel

import (
	"testing"

	"github.com/otfabric/go-mms"
)

// An LN with log control blocks registers each LCB with the Table 65
// components and initial values, plus the LN$LG FC group.
func TestRegisterModel_LogControlBlocks(t *testing.T) {
	m, err := FromSCL(testSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	ln := &m.LogicalDevices[0].LogicalNodes[0]
	if ln.Name != "LLN0" {
		t.Fatalf("first LN %s", ln.Name)
	}
	ln.Logs = []LogDef{
		{Name: "log1", DatSet: "dsEvents", LogEna: true, IntgPd: 1500, TrgOps: TrgOpsDef{Dchg: true}},
		{Name: "log2", LogName: "GeneralLog"},
	}
	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	ld := m.LogicalDevices[0].Name
	get := func(lcb, attr string) *mms.Value {
		t.Helper()
		v := vs.Get(StoreKey(ld, LCBItemID("LLN0", lcb)+"$"+attr))
		if v == nil {
			t.Fatalf("%s.%s not registered", lcb, attr)
		}
		return v
	}
	if b, _ := get("log1", "LogEna").Bool(); !b {
		t.Error("log1 LogEna")
	}
	if s, _ := get("log1", "LogRef").VisibleString(); s != ld+"/LLN0$log1" {
		t.Errorf("log1 LogRef %q", s)
	}
	if s, _ := get("log2", "LogRef").VisibleString(); s != ld+"/LLN0$GeneralLog" {
		t.Errorf("log2 LogRef %q", s)
	}
	if s, _ := get("log1", "DatSet").VisibleString(); s != ld+"/LLN0$dsEvents" {
		t.Errorf("log1 DatSet %q", s)
	}
	if u, _ := get("log1", "IntgPd").Uint64(); u != 1500 {
		t.Errorf("log1 IntgPd %d", u)
	}
	for _, a := range []string{"OldEntrTm", "NewEntrTm"} {
		if v := get("log1", a); v.Type() != mms.ValueTypeBinaryTime {
			t.Errorf("%s type %v", a, v.Type())
		}
	}
	for _, a := range []string{"OldEnt", "NewEnt"} {
		if o, _ := get("log1", a).OctetString(); len(o) != 8 {
			t.Errorf("%s = %x", a, o)
		}
	}
	if (&LogDef{Name: "x"}).LogRef("LDa", "LLN0") != "LDa/LLN0$x" {
		t.Error("LogRef default journal")
	}
	if !LCBWritableFields["LogEna"] || LCBWritableFields["OldEnt"] {
		t.Error("LCBWritableFields")
	}
}

// HasTrgOps and Leaf walk a resolved data set member.
func TestMemberNode_HasTrgOpsAndLeaf(t *testing.T) {
	m := gocbModel()
	ds, _ := m.FindDataSet("LD1", "ds")
	n, err := m.ResolveMember("LD1", ds.Members[0])
	if err != nil {
		t.Fatal(err)
	}
	keys := n.Keys()
	if len(keys) < 2 {
		t.Fatalf("keys %v", keys)
	}
	if n.HasTrgOps() {
		t.Error("no trigger options in the fixture")
	}
	n.Children[1].Dchg = true
	if !n.HasTrgOps() || n.Children[0].HasTrgOps() {
		t.Error("HasTrgOps after setting one leaf")
	}

	vs := NewValueStore()
	vs.Set(keys[1], mms.NewBoolean(true))
	v := vs.ReadMember(n)
	leaf, lv, ok := n.Leaf(v, keys[1])
	if !ok || leaf.Key != keys[1] || lv == nil {
		t.Fatalf("Leaf(%s) = %+v %v %v", keys[1], leaf, lv, ok)
	}
	if _, lv, ok := n.Leaf(nil, keys[0]); !ok || lv != nil {
		t.Errorf("Leaf without a value: %v %v", lv, ok)
	}
	if _, _, ok := n.Leaf(mms.NewBoolean(true), "nope"); ok {
		t.Error("Leaf found a missing key")
	}
}
