// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"strings"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

func TestApplyDAIVals(t *testing.T) {
	a := DataAttribute{Name: "f", InitialValue: "7"}
	applyDAIVals(&a, scl.DAI{Val: "1", Vals: []scl.Value{
		{Value: "1", SGroup: 1}, {Value: "2", SGroup: 2}, {Value: "9", SGroup: 1},
		{Value: "x", SGroup: 300}, {Value: "5"}, {Value: "6"},
	}})
	if a.InitialValue != "5" || !a.Overridden {
		t.Errorf("InitialValue %q Overridden %v, want the Val without sGroup", a.InitialValue, a.Overridden)
	}
	if len(a.GroupValues) != 3 || a.GroupValues[0] != "5" || a.GroupValues[1] != "1" || a.GroupValues[2] != "2" {
		t.Errorf("GroupValues = %v", a.GroupValues)
	}

	// Only sGroup values: the others keep the type's value.
	b := DataAttribute{Name: "f", InitialValue: "7"}
	applyDAIVals(&b, scl.DAI{Val: "1", Vals: []scl.Value{{Value: "1", SGroup: 1}}})
	if b.InitialValue != "1" || b.GroupValues[0] != "7" {
		t.Errorf("InitialValue %q GroupValues %v", b.InitialValue, b.GroupValues)
	}

	// A plain value: no groups.
	c := DataAttribute{Name: "f"}
	applyDAIVals(&c, scl.DAI{Val: "3"})
	if c.InitialValue != "3" || c.GroupValues != nil {
		t.Errorf("InitialValue %q GroupValues %v", c.InitialValue, c.GroupValues)
	}
}

func TestAddImpliedSG(t *testing.T) {
	o := DataObject{Name: "StrVal", Attributes: []DataAttribute{
		{Name: "setMag", FC: "SE", Children: []DataAttribute{{Name: "f", FC: "SE", BType: "FLOAT32"}}},
		{Name: "setVal", FC: "SE", BType: "INT32"},
		{Name: "setVal", FC: "SG", BType: "INT32"},
		{Name: "units", FC: "CF", BType: "INT32"},
	}}
	addImpliedSG(&o)
	if len(o.Attributes) != 5 {
		t.Fatalf("attributes = %+v", o.Attributes)
	}
	sg := o.Attributes[4]
	if sg.Name != "setMag" || sg.FC != "SG" || sg.Children[0].FC != "SG" {
		t.Errorf("implied SG = %+v", sg)
	}
	if o.Attributes[0].Children[0].FC != "SE" {
		t.Error("the SE attribute was changed")
	}
}

func TestSetGroupInitialValues(t *testing.T) {
	ld := &LogicalDevice{Name: "LD", Inst: "LD", LogicalNodes: []LogicalNode{
		{Name: "LLN0", SettingGroup: &SettingGroupDef{NumOfSGs: 2, ActSG: 2}},
		{Name: "PTOC1", DataObjects: []DataObject{{
			Name:     "StrVal",
			Children: []DataObject{{Name: "sub", Attributes: []DataAttribute{{Name: "x", FC: "SG", BType: "INT32", GroupValues: map[uint8]string{0: "4", 1: "1"}}}}},
			Attributes: []DataAttribute{{
				Name: "setMag", FC: "SG",
				Children: []DataAttribute{{Name: "f", FC: "SG", BType: "FLOAT32", GroupValues: map[uint8]string{0: "0", 2: "2", 5: "5"}}},
			}},
		}}},
	}}
	var warnings []string
	setGroupInitialValues(ld, &warnings)
	if got := ld.LogicalNodes[1].DataObjects[0].Attributes[0].Children[0].InitialValue; got != "2" {
		t.Errorf("setMag.f InitialValue = %q, want group 2's", got)
	}
	if got := ld.LogicalNodes[1].DataObjects[0].Children[0].Attributes[0].InitialValue; got != "4" {
		t.Errorf("sub.x InitialValue = %q, want the value for all groups", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "sGroup=5 exceeds numOfSGs 2") {
		t.Errorf("warnings = %v", warnings)
	}
	// No SGCB: nothing to do.
	setGroupInitialValues(&LogicalDevice{}, nil)
}

func TestSettings(t *testing.T) {
	ld := &LogicalDevice{Name: "LD", LogicalNodes: []LogicalNode{
		{Name: "LLN0", DataObjects: []DataObject{{Name: "Mod", Attributes: []DataAttribute{{Name: "stVal", FC: "ST", BType: "INT32"}}}}},
		{Name: "PTOC1", DataObjects: []DataObject{
			{Name: "StrVal", Attributes: []DataAttribute{
				{Name: "setMag", FC: "SE", Children: []DataAttribute{{Name: "f", BType: "FLOAT32", GroupValues: map[uint8]string{0: "", 1: "1.5"}}}},
				{Name: "setMag", FC: "SG", Children: []DataAttribute{{Name: "f", BType: "FLOAT32", GroupValues: map[uint8]string{2: "2.5"}}}},
			}},
			{Name: "Crv", Attributes: []DataAttribute{
				{Name: "setCharact", FC: "SE", BType: "Enum", EnumNames: map[string]int{"a": 3}, GroupValues: map[uint8]string{1: "a"}},
				{Name: "odd", FC: "SG", BType: "NoSuchType"},
			}},
			{Name: "Parent", Children: []DataObject{{Name: "Sub", Attributes: []DataAttribute{{Name: "setVal", FC: "SG", BType: "INT32"}}}}},
		}},
	}}
	got, err := Settings(ld)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("Settings = %+v", got)
	}
	crv, sub, str := got[0], got[1], got[2]
	if crv.SEKey != "LD/PTOC1$SE$Crv$setCharact" || crv.SGKey != "" {
		t.Errorf("Crv = %+v", crv)
	}
	if i, _ := crv.Groups[1].Int64(); i != 3 {
		t.Errorf("Crv group 1 = %v", crv.Groups[1])
	}
	if sub.SGKey != "LD/PTOC1$SG$Parent$Sub$setVal" || sub.Groups != nil {
		t.Errorf("Sub = %+v", sub)
	}
	if str.SGKey != "LD/PTOC1$SG$StrVal$setMag$f" || str.SEKey != "LD/PTOC1$SE$StrVal$setMag$f" {
		t.Errorf("StrVal = %+v", str)
	}
	// The SG attribute's values win over the SE twin's.
	if f, _ := str.Groups[2].Float64(); f != 2.5 || len(str.Groups) != 1 {
		t.Errorf("StrVal groups = %v", str.Groups)
	}

	// SE first, SG without values: the SE values stay.
	ld.LogicalNodes[1].DataObjects[0].Attributes[1].Children[0].GroupValues = nil
	got, _ = Settings(ld)
	if f, _ := got[2].Groups[1].Float64(); f != 1.5 {
		t.Errorf("StrVal group 1 = %v", got[2].Groups[1])
	}
	if f, _ := got[2].Groups[0].Float64(); f != 0 {
		t.Errorf("StrVal group 0 = %v, want the zero value", got[2].Groups[0])
	}

	ld.LogicalNodes[1].DataObjects[1].Attributes[0].GroupValues[2] = "b"
	if _, err := Settings(ld); err == nil || !strings.Contains(err.Error(), "group 2") {
		t.Errorf("unparsable group value: %v", err)
	}
	if v := DefaultValue(mms.TypeSpec{Type: mms.ValueTypeBinaryTime}); v == nil {
		t.Error("DefaultValue")
	}
}

// SetEditSGValue of a constructed FC=SE attribute sets its leaves.
func TestRegister_SECompoundWriteSetsLeaves(t *testing.T) {
	m := &Model{LogicalDevices: []LogicalDevice{{Name: "LD", LogicalNodes: []LogicalNode{{
		Name: "LLN0", LNClass: "LLN0",
		DataObjects: []DataObject{{Name: "StrVal", Attributes: []DataAttribute{{
			Name: "setMag", FC: "SE",
			Children: []DataAttribute{{Name: "f", FC: "SE", BType: "FLOAT32"}, {Name: "i", FC: "SE", BType: "INT32"}},
		}}}},
	}}}}}
	srv := mms.NewServer(mms.ServerOptions{MMS: mms.ServerMMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
		DataStructureNestingLevel: 10,
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
	write := func(v *mms.Value) error {
		_, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD", ItemID: "LLN0$SE$StrVal$setMag", Value: v})
		return err
	}
	if err := write(mms.NewStructure([]*mms.Value{mms.NewFloat(2), mms.NewInteger(3)})); err != nil {
		t.Fatal(err)
	}
	if f, _ := vs.Get("LD/LLN0$SE$StrVal$setMag$f").Float64(); f != 2 {
		t.Errorf("f = %v", vs.Get("LD/LLN0$SE$StrVal$setMag$f"))
	}
	if i, _ := vs.Get("LD/LLN0$SE$StrVal$setMag$i").Int64(); i != 3 {
		t.Errorf("i = %v", vs.Get("LD/LLN0$SE$StrVal$setMag$i"))
	}
	if err := write(mms.NewFloat(1)); err == nil {
		t.Error("a float written to setMag")
	}
	// The edit guard applies to the compound write too.
	vs.SetEditGuard(func(context.Context, string, bool) error {
		return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	})
	if err := write(mms.NewStructure([]*mms.Value{mms.NewFloat(5), mms.NewInteger(3)})); err == nil {
		t.Error("write past the edit guard")
	}
}

// fromscl: a DAI under SDI sets the value per group on SG and SE twins.
func TestFromSCL_SGroupValues(t *testing.T) {
	doc, err := scl.ParseFile("../../testdata/setting-groups.cid")
	if err != nil {
		t.Fatal(err)
	}
	m, err := FromSCL(doc, "SGIED", "")
	if err != nil {
		t.Fatal(err)
	}
	var ptoc *LogicalNode
	for i := range m.LogicalDevices[0].LogicalNodes {
		if m.LogicalDevices[0].LogicalNodes[i].Name == "PTOC1" {
			ptoc = &m.LogicalDevices[0].LogicalNodes[i]
		}
	}
	if ptoc == nil {
		t.Fatal("no PTOC1")
	}
	for _, o := range ptoc.DataObjects {
		if o.Name != "OpDlTmms" {
			continue
		}
		// The DAI applies to both twins; each keeps its DA's own value
		// for the other groups (SG declares 100, SE none).
		for _, a := range o.Attributes {
			want := map[string]string{"SG": "100", "SE": ""}[a.FC]
			if a.GroupValues[1] != "300" || a.GroupValues[2] != "500" || a.GroupValues[0] != want {
				t.Errorf("OpDlTmms.setVal [%s] GroupValues = %v", a.FC, a.GroupValues)
			}
		}
	}
}
