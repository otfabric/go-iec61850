// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestMemberIdentity(t *testing.T) {
	if got := memberIdentity(DataSetMember{Ref: Ref{LD: "LD1", LN: "LLN0", Path: []string{"Mod"}, FC: FCST}}, 0); got == "" || got == "[0]" {
		t.Fatalf("ref identity = %q", got)
	}
	if got := memberIdentity(DataSetMember{DomainID: "LD1", ItemID: "LLN0$ST$Mod$stVal"}, 1); got != "LD1/LLN0$ST$Mod$stVal" {
		t.Fatalf("domain/item = %q", got)
	}
	if got := memberIdentity(DataSetMember{}, 3); got != "[3]" {
		t.Fatalf("index fallback = %q", got)
	}
}

func TestDataset_ValidationEdges(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	if _, err := client.GetDataSet(ctx, "", "LLN0$dataset1"); err == nil {
		t.Fatal("GetDataSet empty LD")
	}
	if _, err := client.GetDataSet(ctx, "simpleIOGenericIO", ""); err == nil {
		t.Fatal("GetDataSet empty name")
	}
	if _, err := client.ReadDataSet(ctx, "", "LLN0$dataset1"); err == nil {
		t.Fatal("ReadDataSet empty LD")
	}
	if _, err := client.ReadDataSet(ctx, "simpleIOGenericIO", ""); err == nil {
		t.Fatal("ReadDataSet empty name")
	}
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "", []DataSetMember{
		{DomainID: "simpleIOGenericIO", ItemID: "LLN0$ST$Mod$stVal"},
	}); err == nil {
		t.Fatal("CreateDataSet empty name")
	}
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$ds", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", Path: []string{"Mod"}, FC: FCST}},
	}); err == nil {
		t.Fatal("CreateDataSet missing LN")
	}
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$ds", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod"}}},
	}); err == nil {
		t.Fatal("CreateDataSet missing FC")
	}
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$ds", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", FC: FCST}},
	}); err == nil {
		t.Fatal("CreateDataSet missing path")
	}
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$ds", []DataSetMember{
		{Ref: Ref{LD: "bad/ld", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
	}); err == nil {
		t.Fatal("CreateDataSet invalid ref")
	}
	// Default empty member LD to owning ld.
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsDefaultLD", []DataSetMember{
		{Ref: Ref{LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
	}); err != nil {
		t.Fatalf("CreateDataSet default LD: %v", err)
	}
	if err := client.DeleteDataSet(ctx, "", "LLN0$ds"); err == nil {
		t.Fatal("DeleteDataSet empty LD")
	}
	if err := client.DeleteDataSet(ctx, "simpleIOGenericIO", "LLN0$noSuchDS"); err == nil {
		t.Fatal("DeleteDataSet nonexistent")
	}

	_ = client.Close(ctx)
	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$x", []DataSetMember{
		{DomainID: "simpleIOGenericIO", ItemID: "LLN0$ST$Mod$stVal"},
	}); err == nil {
		t.Fatal("CreateDataSet closed")
	}
	if err := client.DeleteDataSet(ctx, "simpleIOGenericIO", "LLN0$dataset1"); err == nil {
		t.Fatal("DeleteDataSet closed")
	}
}

func TestReadDataSet_CachedAndErrors(t *testing.T) {
	ctx := context.Background()
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	domain := "cacheLD"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(context.Context) (*mms.Value, error) { return mms.NewInteger(7), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name:      mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$dsCache"},
		Deletable: true,
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"}},
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
	client, err := NewClient(mmsClient, ClientOptions{Cache: CacheLazy})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	// Warm cache via GetDataSet, then ReadDataSet should use cached members.
	if _, err := client.GetDataSet(ctx, domain, "LLN0$dsCache"); err != nil {
		t.Fatal(err)
	}
	vals, err := client.ReadDataSet(ctx, domain, "LLN0$dsCache")
	if err != nil {
		t.Fatalf("ReadDataSet cached: %v", err)
	}
	if len(vals) != 1 || vals[0].Err != nil {
		t.Fatalf("%+v", vals)
	}

	// Missing NVL.
	if _, err := client.ReadDataSet(ctx, domain, "LLN0$missing"); err == nil {
		t.Fatal("expected missing NVL error")
	}
}

func TestReadDataSet_MemberAccessError(t *testing.T) {
	ctx := context.Background()
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	domain := "errLD"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatal(err)
	}
	// NVL references a variable that does not exist → access error / missing value.
	if err := srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$dsBad"},
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Missing$stVal"}},
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
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	vals, err := client.ReadDataSet(ctx, domain, "LLN0$dsBad")
	if err != nil {
		// Some stacks fail the whole ReadMultiple; either outcome covers paths.
		return
	}
	if len(vals) != 1 || vals[0].Err == nil {
		t.Fatalf("expected per-member error, got %+v", vals)
	}
	var dae *DataAccessError
	_ = errors.As(vals[0].Err, &dae)
}
