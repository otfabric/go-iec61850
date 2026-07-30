// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestControlClient_ValidationAndClosed(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()
	badRef := Ref{LD: "x"} // incomplete
	okRef := Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"SPCSO1"}}

	if err := client.Operate(ctx, badRef, OperateParams{CtlVal: mms.NewBoolean(true)}); err == nil {
		t.Fatal("Operate bad ref")
	}
	if err := client.Operate(ctx, okRef, OperateParams{}); err == nil {
		t.Fatal("Operate nil CtlVal")
	}
	if _, err := client.Select(ctx, badRef); err == nil {
		t.Fatal("Select bad ref")
	}
	if err := client.SelectWithValue(ctx, badRef, OperateParams{CtlVal: mms.NewBoolean(true)}); err == nil {
		t.Fatal("SBOw bad ref")
	}
	if err := client.SelectWithValue(ctx, okRef, OperateParams{}); err == nil {
		t.Fatal("SBOw nil CtlVal")
	}
	if err := client.Cancel(ctx, badRef, CancelParams{}); err == nil {
		t.Fatal("Cancel bad ref")
	}
	if _, err := client.ReadCtlModel(ctx, badRef); err == nil {
		t.Fatal("ReadCtlModel bad ref")
	}
	if _, err := client.ReadLastApplError(ctx, badRef); err == nil {
		t.Fatal("ReadLastApplError bad ref")
	}

	_ = client.Close(ctx)
	if err := client.Operate(ctx, okRef, OperateParams{CtlVal: mms.NewBoolean(true)}); err == nil {
		t.Fatal("Operate closed")
	}
	if _, err := client.Select(ctx, okRef); err == nil {
		t.Fatal("Select closed")
	}
	if err := client.SelectWithValue(ctx, okRef, OperateParams{CtlVal: mms.NewBoolean(true)}); err == nil {
		t.Fatal("SBOw closed")
	}
	if err := client.Cancel(ctx, okRef, CancelParams{}); err == nil {
		t.Fatal("Cancel closed")
	}
	if _, err := client.ReadCtlModel(ctx, okRef); err == nil {
		t.Fatal("ReadCtlModel closed")
	}
	if _, err := client.ReadLastApplError(ctx, okRef); err == nil {
		t.Fatal("ReadLastApplError closed")
	}
}

func TestControlClient_WirePaths(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()
	ref := Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"SPCSO1"}}

	// Operate writes Oper structure to CO path (registered Oper$ctlVal only —
	// full Oper structure write may fail; still exercises writeControlValue).
	_ = client.Operate(ctx, ref, OperateParams{CtlVal: mms.NewBoolean(true), CtlNum: 1})
	_ = client.SelectWithValue(ctx, ref, OperateParams{CtlVal: mms.NewBoolean(true), CtlNum: 2})
	_ = client.Cancel(ctx, ref, CancelParams{CtlVal: mms.NewBoolean(true), CtlNum: 3})

	// Select reads SBO — expect error (not registered) but covers path.
	_, _ = client.Select(ctx, ref)
	_, _ = client.ReadCtlModel(ctx, ref)
	_, _ = client.ReadLastApplError(ctx, ref)
}

func TestListReportsVerified_Empty(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()
	// Domain has no RCB names → empty candidates.
	got, err := client.ListReportsVerified(ctx, "simpleIOGenericIO")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("%v", got)
	}
	// Direct verify with mixed candidates (all fail read → empty).
	verified := client.verifyReportCandidates(ctx, "simpleIOGenericIO", []string{"LLN0$RP$urcbA", "notRCB"})
	if len(verified) != 0 {
		t.Fatalf("%v", verified)
	}
	if _, err := client.ListReportsVerified(ctx, ""); err == nil {
		t.Fatal("empty LD")
	}
}

func TestSelect_SBOResponseShapes(t *testing.T) {
	ctx := context.Background()
	domain := "ctrlLD"
	item := "GGIO1$CO$SPCSO1$SBO"

	cases := []struct {
		name string
		val  *mms.Value
		ok   bool
	}{
		{"success", mms.NewVisibleString("ctrlLD/GGIO1.SPCSO1"), true},
		{"denied_empty", mms.NewVisibleString(""), false},
		{"wrong_type", mms.NewBoolean(true), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mms.NewServer(mms.ServerOptions{
				MMS: mms.ServerMMSOptions{
					MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
					DataStructureNestingLevel: 10,
				},
			})
			if err := srv.RegisterDomain(domain); err != nil {
				t.Fatal(err)
			}
			v := tc.val
			if err := srv.RegisterVariable(mms.Variable{
				Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(item)},
				TypeSpec: mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129},
				Read: func(context.Context) (*mms.Value, error) {
					return v, nil
				},
			}); err != nil {
				t.Fatal(err)
			}

			clientT, serverT := loopbackPair()
			go func() { _ = srv.Serve(ctx, serverT) }()
			mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = mmsClient.Close(ctx) })

			c := &Client{mmsClient: mmsClient, logger: discardLogger()}
			got, err := c.Select(ctx, Ref{LD: domain, LN: "GGIO1", Path: []string{"SPCSO1"}})
			if tc.ok {
				if err != nil || got == "" {
					t.Fatalf("got %q err=%v", got, err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestOperate_AutoCtlNum(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()
	ref := Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"SPCSO1"}}
	// CtlNum 0 triggers nextCtlNum inside Operate / writeControlValue path.
	_ = client.Operate(ctx, ref, OperateParams{CtlVal: mms.NewBoolean(true)})
}

func TestWriteControlValue_RefToMMSError(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}}
	err := c.writeControlValue(context.Background(), Ref{LD: "bad/ld", LN: "LN", Path: []string{"Oper"}, FC: FCCO}, mms.NewBoolean(true))
	if err == nil {
		t.Fatal("expected refToMMS error")
	}
}

func TestSelect_NilValue(t *testing.T) {
	ctx := context.Background()
	domain := "ctrlLD2"
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "GGIO1$CO$SPCSO1$SBO"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129},
		Read:     func(context.Context) (*mms.Value, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mmsClient.Close(ctx) })
	c := &Client{mmsClient: mmsClient, logger: discardLogger()}
	if _, err := c.Select(ctx, Ref{LD: domain, LN: "GGIO1", Path: []string{"SPCSO1"}}); err == nil {
		t.Fatal("expected empty response error")
	}
}
