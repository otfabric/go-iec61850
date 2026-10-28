// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

func loadSCL(t *testing.T, path string) *scl.SCL {
	t.Helper()
	doc, err := scl.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestControlBlocksFromInteropSCL(t *testing.T) {
	doc := loadSCL(t, "testdata/interop-goose.cid")
	cbs, err := ControlBlocksFromSCL(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(cbs) != 2 {
		t.Fatalf("got %d control blocks", len(cbs))
	}
	all := cbs[0]
	if all.GocbRef != "InteropIEDInteropLD/LLN0$GO$gcbAll" || all.DatSet != "InteropIEDInteropLD/LLN0$dsGooseAll" ||
		all.GoID != "InteropGooseAll" || all.ConfRev != 1 || all.AppID != 0x1001 ||
		all.Dst != (MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01}) || all.VLAN == nil || all.VLAN.Priority != 4 || all.VLAN.ID != 0 ||
		all.MinTime != 10*time.Millisecond || all.MaxTime != time.Second || !all.HasAddress {
		t.Fatalf("gcbAll = %+v", all)
	}
	wantTypes := []mms.ValueType{
		mms.ValueTypeBoolean, mms.ValueTypeInteger, mms.ValueTypeUnsigned, mms.ValueTypeFloat,
		mms.ValueTypeBitString, mms.ValueTypeUTCTime, mms.ValueTypeBitString, mms.ValueTypeStructure,
	}
	if len(all.Members) != len(wantTypes) {
		t.Fatalf("members = %+v", all.Members)
	}
	for i, w := range wantTypes {
		if all.Members[i].Type.Type != w {
			t.Errorf("member %d (%s) type %v, want %v", i, all.Members[i].Ref, all.Members[i].Type.Type, w)
		}
	}
	if all.Members[0].Ref != "InteropLD/GGIO1.SPS1.stVal" || all.Members[0].FC != "ST" {
		t.Errorf("member 0 = %+v", all.Members[0])
	}
	// MMXU1.TotW [MX] is { mag { f }, q, t }.
	totw := all.Members[7].Type
	if len(totw.Elements) != 3 || totw.Elements[0].Name != "mag" || totw.Elements[0].Type.Elements[0].Name != "f" {
		t.Errorf("TotW layout = %+v", totw)
	}

	cb, err := ControlBlockFromSCL(doc, "InteropIED", "InteropLD", "gcbBool")
	if err != nil || cb.AppID != 0x1002 || len(cb.Members) != 2 {
		t.Fatalf("gcbBool: %+v %v", cb, err)
	}
	if _, err := ControlBlockFromSCL(doc, "InteropIED", "InteropLD", "nope"); !errors.Is(err, ErrNotFound) || !errors.Is(err, ErrSCL) {
		t.Errorf("missing: %v", err)
	}
	if _, err := ControlBlocksFromSCL(nil); !errors.Is(err, ErrSCL) {
		t.Error(err)
	}
}

// TestSCLPublisherToSubscriber wires a publisher and a validating
// subscriber purely from SCL over a hub.
func TestSCLPublisherToSubscriber(t *testing.T) {
	t.Parallel()
	doc := loadSCL(t, "testdata/interop-goose.cid")
	cb, err := ControlBlockFromSCL(doc, "InteropIED", "InteropLD", "gcbAll")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	pc, err := cb.PublisherConfig()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := NewPublisher(hub.NewPort(MAC{}), pc)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
	sub, _ := s.Subscribe(cb.SubscriptionConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	go func() { _ = pub.Run(ctx) }()
	vals := cb.DefaultValues()
	vals[0] = mms.NewBoolean(true)
	if err := pub.Publish(vals); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case ev := <-sub.Events():
			if ev.Kind == EventInvalid {
				t.Fatalf("invalid: %v", ev.Err)
			}
			if ev.Kind == EventMessage {
				if b, _ := ev.Frame.PDU.AllData[0].Bool(); !b {
					continue
				}
				if ev.Frame.VLAN == nil || ev.Frame.VLAN.Priority != 4 {
					t.Errorf("VLAN = %+v", ev.Frame.VLAN)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("timeout")
		}
	}
}

func TestControlBlocksFromLibiecExample(t *testing.T) {
	doc := loadSCL(t, "../scl/testdata/examples/server_example_goose/simpleIO_direct_control_goose.cid")
	cbs, err := ControlBlocksFromSCL(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(cbs) != 2 {
		t.Fatalf("got %d blocks", len(cbs))
	}
	for _, cb := range cbs {
		if !strings.Contains(cb.GocbRef, "$GO$gcb") || cb.AppID != 0x1000 || cb.ConfRev != 2 || len(cb.Members) == 0 {
			t.Errorf("%+v", cb)
		}
	}
}

func TestControlBlockWithoutAddress(t *testing.T) {
	cb := &ControlBlock{GocbRef: "X/LLN0$GO$g", Members: []Member{
		{Type: mms.TypeSpec{Type: mms.ValueTypeArray, Count: 2, Element: &mms.TypeSpec{Type: mms.ValueTypeBoolean}}},
		{Type: mms.TypeSpec{Type: mms.ValueTypeArray, Count: 1}},
		{Type: mms.TypeSpec{Type: mms.ValueTypeBinaryTime}},
		{Type: mms.TypeSpec{Type: mms.ValueTypeVisibleString}},
	}}
	if _, err := cb.PublisherConfig(); !errors.Is(err, ErrInvalidConfig) {
		t.Error(err)
	}
	sc := cb.SubscriptionConfig()
	if !sc.Dst.IsZero() || sc.AppIDs != nil || len(sc.Members) != 4 {
		t.Errorf("%+v", sc)
	}
	for i, v := range cb.DefaultValues() {
		if _, err := EncodeData(v); err != nil {
			t.Errorf("default %d not encodable: %v", i, err)
		}
	}
}

func TestControlBlocksFromBrokenSCL(t *testing.T) {
	doc := loadSCL(t, "testdata/interop-goose.cid")
	// Point gcbBool at a dataset member that does not exist.
	ln0 := doc.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	for i := range ln0.DataSets {
		if ln0.DataSets[i].Name == "dsGooseBool" {
			ln0.DataSets[i].FCDAs[0].DOName = "Missing"
		}
	}
	cbs, err := ControlBlocksFromSCL(doc)
	if !errors.Is(err, ErrSCL) || len(cbs) != 1 {
		t.Fatalf("cbs=%d err=%v", len(cbs), err)
	}
	if _, err := ControlBlockFromSCL(doc, "InteropIED", "InteropLD", "gcbBool"); !errors.Is(err, ErrSCL) {
		t.Error(err)
	}
	// An unknown LNodeType breaks the whole access point.
	doc.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.LNType = "nope"
	if _, err := ControlBlocksFromSCL(doc); !errors.Is(err, ErrSCL) {
		t.Error(err)
	}
}
