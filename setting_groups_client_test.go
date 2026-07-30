// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func setupSGCBClientLoopback(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()
	domain := "LD1"

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	sgcb := []*mms.Value{
		mms.NewUnsigned(4),          // NumOfSGs
		mms.NewUnsigned(1),          // ActSG
		mms.NewUnsigned(0),          // EditSG
		mms.NewBoolean(false),       // CnfEdit
		mms.NewUTCTime(time.Time{}), // LActTm
		mms.NewUnsigned(30),         // ResvTms
	}
	sgcbTS := mms.TypeSpec{
		Type: mms.ValueTypeStructure,
		Elements: []mms.TypeSpecElement{
			{Name: "NumOfSGs", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}},
			{Name: "ActSG", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}},
			{Name: "EditSG", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}},
			{Name: "CnfEdit", Type: mms.TypeSpec{Type: mms.ValueTypeBoolean}},
			{Name: "LActTm", Type: mms.TypeSpec{Type: mms.ValueTypeUTCTime}},
			{Name: "ResvTms", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
		},
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: sgcbItemID},
		TypeSpec: sgcbTS,
		Read: func(context.Context) (*mms.Value, error) {
			mu.Lock()
			defer mu.Unlock()
			cp := make([]*mms.Value, len(sgcb))
			copy(cp, sgcb)
			return mms.NewStructure(cp), nil
		},
		Write: func(_ context.Context, val *mms.Value) error {
			elems, ok := val.Structure()
			if !ok || len(elems) != len(sgcb) {
				return fmt.Errorf("bad SGCB write")
			}
			mu.Lock()
			copy(sgcb, elems)
			mu.Unlock()
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	// SE / SG leaf values for Get/Set helpers.
	seVal := mms.NewInteger(10)
	sgVal := mms.NewInteger(20)
	for _, v := range []struct {
		item string
		get  func() *mms.Value
		set  func(*mms.Value)
		ts   mms.TypeSpec
	}{
		{
			item: "LLN0$SE$Mod$setVal",
			get:  func() *mms.Value { mu.Lock(); defer mu.Unlock(); return seVal },
			set:  func(val *mms.Value) { mu.Lock(); seVal = val; mu.Unlock() },
			ts:   mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32},
		},
		{
			item: "LLN0$SG$Mod$setVal",
			get:  func() *mms.Value { mu.Lock(); defer mu.Unlock(); return sgVal },
			set:  func(val *mms.Value) { mu.Lock(); sgVal = val; mu.Unlock() },
			ts:   mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32},
		},
	} {
		get, set, ts := v.get, v.set, v.ts
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(v.item)},
			TypeSpec: ts,
			Read:     func(context.Context) (*mms.Value, error) { return get(), nil },
			Write: func(_ context.Context, val *mms.Value) error {
				set(val)
				return nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })
	return client
}

func TestGetSettingGroupInfo_AndSelects(t *testing.T) {
	client := setupSGCBClientLoopback(t)
	ctx := context.Background()

	info, err := client.GetSettingGroupInfo(ctx, "LD1")
	if err != nil {
		t.Fatalf("GetSettingGroupInfo: %v", err)
	}
	if info.NumOfSGs != 4 || info.ActSG != 1 || info.ResvTms != 30 {
		t.Fatalf("%+v", info)
	}

	if err := client.SelectActiveSG(ctx, "LD1", 2); err != nil {
		t.Fatalf("SelectActiveSG: %v", err)
	}
	info, err = client.GetSettingGroupInfo(ctx, "LD1")
	if err != nil || info.ActSG != 2 {
		t.Fatalf("ActSG after select: %+v %v", info, err)
	}

	if err := client.SelectEditSG(ctx, "LD1", 3); err != nil {
		t.Fatalf("SelectEditSG: %v", err)
	}
	info, err = client.GetSettingGroupInfo(ctx, "LD1")
	if err != nil || info.EditSG != 3 {
		t.Fatalf("EditSG after select: %+v %v", info, err)
	}

	if err := client.ConfirmEditSG(ctx, "LD1"); err != nil {
		t.Fatalf("ConfirmEditSG: %v", err)
	}
	info, err = client.GetSettingGroupInfo(ctx, "LD1")
	if err != nil || !info.CnfEdit {
		t.Fatalf("CnfEdit after confirm: %+v %v", info, err)
	}

	ref := Ref{LD: "LD1", LN: "LLN0", Path: []string{"Mod", "setVal"}}
	if err := client.SetEditSGValue(ctx, ref, mms.NewInteger(42)); err != nil {
		t.Fatalf("SetEditSGValue: %v", err)
	}
	v, err := client.GetEditSGValue(ctx, ref)
	if err != nil {
		t.Fatalf("GetEditSGValue: %v", err)
	}
	if i, _ := v.Int64(); i != 42 {
		t.Fatalf("SE value = %d", i)
	}
	v, err = client.GetActiveSGValue(ctx, ref)
	if err != nil {
		t.Fatalf("GetActiveSGValue: %v", err)
	}
	if i, _ := v.Int64(); i != 20 {
		t.Fatalf("SG value = %d", i)
	}
}

func TestSettingGroupClient_Validation(t *testing.T) {
	client := setupSGCBClientLoopback(t)
	ctx := context.Background()

	if _, err := client.GetSettingGroupInfo(ctx, ""); err == nil {
		t.Fatal("empty LD GetSettingGroupInfo")
	}
	if err := client.SelectActiveSG(ctx, "", 1); err == nil {
		t.Fatal("empty LD SelectActiveSG")
	}
	if err := client.SelectActiveSG(ctx, "LD1", 0); err == nil {
		t.Fatal("sg=0 SelectActiveSG")
	}
	if err := client.SelectEditSG(ctx, "", 1); err == nil {
		t.Fatal("empty LD SelectEditSG")
	}
	if err := client.SelectEditSG(ctx, "LD1", 0); err == nil {
		t.Fatal("sg=0 SelectEditSG")
	}
	if err := client.ConfirmEditSG(ctx, ""); err == nil {
		t.Fatal("empty LD ConfirmEditSG")
	}

	_ = client.Close(ctx)
	if _, err := client.GetSettingGroupInfo(ctx, "LD1"); err == nil {
		t.Fatal("closed GetSettingGroupInfo")
	}
	if err := client.SelectActiveSG(ctx, "LD1", 1); err == nil {
		t.Fatal("closed SelectActiveSG")
	}
	if err := client.SelectEditSG(ctx, "LD1", 1); err == nil {
		t.Fatal("closed SelectEditSG")
	}
	if err := client.ConfirmEditSG(ctx, "LD1"); err == nil {
		t.Fatal("closed ConfirmEditSG")
	}
}

func TestGetSettingGroupInfo_ReadErrors(t *testing.T) {
	client := setupWritableLoopback(t) // no SGCB registered
	ctx := context.Background()
	if _, err := client.GetSettingGroupInfo(ctx, "simpleIOGenericIO"); err == nil {
		t.Fatal("expected missing SGCB error")
	}
	// WriteComponent failures (no SGCB structure).
	if err := client.SelectActiveSG(ctx, "simpleIOGenericIO", 1); err == nil {
		t.Fatal("SelectActiveSG write error")
	}
	if err := client.SelectEditSG(ctx, "simpleIOGenericIO", 1); err == nil {
		t.Fatal("SelectEditSG write error")
	}
	if err := client.ConfirmEditSG(ctx, "simpleIOGenericIO"); err == nil {
		t.Fatal("ConfirmEditSG write error")
	}
}

func TestDecodeSGCB_MemberTypeErrors(t *testing.T) {
	base := []*mms.Value{
		mms.NewUnsigned(4),
		mms.NewUnsigned(1),
		mms.NewUnsigned(0),
		mms.NewBoolean(false),
		mms.NewUTCTime(time.Time{}),
	}
	cases := []struct {
		name string
		idx  int
		val  *mms.Value
	}{
		{"ActSG", 1, mms.NewBoolean(true)},
		{"EditSG", 2, mms.NewBoolean(true)},
		{"CnfEdit", 3, mms.NewUnsigned(1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := append([]*mms.Value(nil), base...)
			m[tc.idx] = tc.val
			if _, err := decodeSGCB(mms.NewStructure(m)); err == nil {
				t.Fatal("expected type error")
			}
		})
	}
}

func TestSgcbObjectName(t *testing.T) {
	n := sgcbObjectName("LD1")
	if n.Scope != mms.ObjectScopeDomain || n.Domain != "LD1" || string(n.ItemID) != sgcbItemID {
		t.Fatalf("%+v", n)
	}
}
