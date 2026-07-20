// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestReadMultiple(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	refs := []Ref{
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "q"}, FC: FCST},
		{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
	}

	results, err := client.ReadMultiple(ctx, refs)
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// First result: integer
	if results[0].Err != nil {
		t.Fatalf("results[0].Err: %v", results[0].Err)
	}
	if results[0].Value == nil {
		t.Fatal("results[0].Value is nil")
	}
	if results[0].Value.Type() != mms.ValueTypeInteger {
		t.Errorf("results[0].Type = %v, want Integer", results[0].Value.Type())
	}

	// Second result: quality bit string
	if results[1].Err != nil {
		t.Fatalf("results[1].Err: %v", results[1].Err)
	}
	if results[1].Value == nil {
		t.Fatal("results[1].Value is nil")
	}
	q, err := results[1].Value.Quality()
	if err != nil {
		t.Fatalf("results[1].Quality: %v", err)
	}
	_ = q

	// Third result: boolean
	if results[2].Err != nil {
		t.Fatalf("results[2].Err: %v", results[2].Err)
	}
	if results[2].Value.Type() != mms.ValueTypeBoolean {
		t.Errorf("results[2].Type = %v, want Boolean", results[2].Value.Type())
	}

	// Verify refs are preserved in order
	for i, r := range results {
		if r.Ref.String() != refs[i].String() {
			t.Errorf("results[%d].Ref = %s, want %s", i, r.Ref.String(), refs[i].String())
		}
	}
}

func TestReadMultiple_PartialError(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	refs := []Ref{
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"NoSuchAttr"}, FC: FCST},
		{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
	}

	results, err := client.ReadMultiple(ctx, refs)
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// First: valid
	if results[0].Err != nil {
		t.Errorf("results[0] should succeed, got err: %v", results[0].Err)
	}
	if results[0].Value == nil {
		t.Error("results[0].Value should not be nil")
	}

	// Second: non-existent → per-item error
	if results[1].Err == nil {
		t.Error("results[1] should have per-item error for missing attribute")
	}
	if results[1].Value != nil {
		t.Error("results[1].Value should be nil on error")
	}

	// Third: valid
	if results[2].Err != nil {
		t.Errorf("results[2] should succeed, got err: %v", results[2].Err)
	}
	if results[2].Value == nil {
		t.Error("results[2].Value should not be nil")
	}

	// Verify order is preserved
	for i, r := range results {
		if r.Ref.String() != refs[i].String() {
			t.Errorf("results[%d].Ref = %s, want %s", i, r.Ref.String(), refs[i].String())
		}
	}
}

func TestReadMultiple_Empty(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	results, err := client.ReadMultiple(ctx, nil)
	if err != nil {
		t.Fatalf("ReadMultiple(nil): %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for nil input, got %v", results)
	}
}

func TestReadMultiple_NoFC(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	refs := []Ref{
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}},
	}
	_, err := client.ReadMultiple(ctx, refs)
	if err == nil {
		t.Fatal("expected error for ref without FC")
	}
}

func TestReadMultiple_ClosedClient(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	refs := []Ref{
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
	}
	_, err := client.ReadMultiple(ctx, refs)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestWriteMultiple(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	requests := []WriteRequest{
		{
			Ref:   Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
			Value: mms.NewInteger(3),
		},
		{
			Ref:   Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
			Value: mms.NewBoolean(true),
		},
	}

	results, err := client.WriteMultiple(ctx, requests)
	if err != nil {
		t.Fatalf("WriteMultiple: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	for i, r := range results {
		if !r.Success {
			t.Errorf("results[%d].Success = false, err: %v", i, r.Err)
		}
		if r.Ref.String() != requests[i].Ref.String() {
			t.Errorf("results[%d].Ref = %s, want %s", i, r.Ref.String(), requests[i].Ref.String())
		}
	}

	// Verify write-back
	ref1 := requests[0].Ref
	v, err := client.Read(ctx, ref1)
	if err != nil {
		t.Fatalf("Read after WriteMultiple: %v", err)
	}
	i, err := v.Int64()
	if err != nil {
		t.Fatalf("Int64: %v", err)
	}
	if i != 3 {
		t.Errorf("read-back = %d, want 3", i)
	}
}

func TestWriteMultiple_Empty(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	results, err := client.WriteMultiple(ctx, nil)
	if err != nil {
		t.Fatalf("WriteMultiple(nil): %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for nil input, got %v", results)
	}
}

func TestWriteMultiple_NilValue(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	requests := []WriteRequest{
		{
			Ref:   Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
			Value: nil,
		},
	}
	_, err := client.WriteMultiple(ctx, requests)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
}

func TestWriteMultiple_ClosedClient(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	requests := []WriteRequest{
		{
			Ref:   Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
			Value: mms.NewInteger(1),
		},
	}
	_, err := client.WriteMultiple(ctx, requests)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

// TestWriteMultiple_MixedFCs writes to different FCs in a single batch,
// verifying that mixed-domain/FC writes are supported.
func TestWriteMultiple_MixedFCs(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	requests := []WriteRequest{
		{
			Ref:   Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
			Value: mms.NewInteger(5),
		},
		{
			Ref:   Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "ctlModel"}, FC: FCSP},
			Value: mms.NewInteger(2),
		},
	}

	results, err := client.WriteMultiple(ctx, requests)
	if err != nil {
		t.Fatalf("WriteMultiple mixed FCs: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	for i, r := range results {
		if !r.Success {
			t.Errorf("results[%d] failed: %v", i, r.Err)
		}
	}

	// Verify both values were written
	v1, err := client.Read(ctx, requests[0].Ref)
	if err != nil {
		t.Fatalf("Read ref[0]: %v", err)
	}
	i1, _ := v1.Int64()
	if i1 != 5 {
		t.Errorf("ref[0] read-back = %d, want 5", i1)
	}

	v2, err := client.Read(ctx, requests[1].Ref)
	if err != nil {
		t.Fatalf("Read ref[1]: %v", err)
	}
	i2, _ := v2.Int64()
	if i2 != 2 {
		t.Errorf("ref[1] read-back = %d, want 2", i2)
	}
}

// TestReadMultiple_MixedFCs reads attributes from different FCs in a
// single batch, verifying mixed-FC reads are supported.
func TestReadMultiple_MixedFCs(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	refs := []Ref{
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "ctlModel"}, FC: FCSP},
		{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"SPCSO1", "Oper", "ctlVal"}, FC: FCCO},
	}

	results, err := client.ReadMultiple(ctx, refs)
	if err != nil {
		t.Fatalf("ReadMultiple mixed FCs: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	for i, r := range results {
		if r.Err != nil {
			t.Errorf("results[%d] error: %v", i, r.Err)
		}
		if r.Value == nil {
			t.Errorf("results[%d] value is nil", i)
		}
	}
}

func TestReadMultiple_RejectsLNOnlyRef(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.ReadMultiple(ctx, []Ref{
		{LD: "simpleIOGenericIO", LN: "LLN0", FC: FCST},
	})
	if err == nil {
		t.Fatal("expected error for LN-only ref without path")
	}
}

func TestReadMultiple_MixedDomains(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	for _, d := range []string{"LD1", "LD2"} {
		if err := srv.RegisterDomain(d); err != nil {
			t.Fatalf("register domain %q: %v", d, err)
		}
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewInteger(42), nil },
	}); err != nil {
		t.Fatalf("register LD1 var: %v", err)
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD2", ItemID: "GGIO1$ST$Ind1$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeBoolean},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewBoolean(true), nil },
	}); err != nil {
		t.Fatalf("register LD2 var: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	refs := []Ref{
		{LD: "LD1", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
		{LD: "LD2", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
	}

	results, err := client.ReadMultiple(ctx, refs)
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if results[0].Err != nil {
		t.Errorf("LD1 read error: %v", results[0].Err)
	}
	if results[1].Err != nil {
		t.Errorf("LD2 read error: %v", results[1].Err)
	}

	if results[0].Value != nil {
		if i, err := results[0].Value.Int64(); err == nil && i != 42 {
			t.Errorf("LD1 value = %d, want 42", i)
		}
	}
	if results[1].Value != nil {
		if b, err := results[1].Value.Bool(); err == nil && !b {
			t.Error("LD2 value = false, want true")
		}
	}
}

func TestReadMultiple_MixedDomainPartialError(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatalf("register domain: %v", err)
	}
	if err := srv.RegisterDomain("LD2"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
	}); err != nil {
		t.Fatalf("register var: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	refs := []Ref{
		{LD: "LD1", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST},
		{LD: "LD2", LN: "GGIO1", Path: []string{"NoSuchVar"}, FC: FCST},
	}

	results, err := client.ReadMultiple(ctx, refs)
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if results[0].Err != nil {
		t.Errorf("LD1 should succeed, got: %v", results[0].Err)
	}
	if results[1].Err == nil {
		t.Error("LD2 non-existent variable should produce per-item error")
	}
}

func TestWriteMultiple_MixedDomains(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	var mu sync.Mutex
	store := make(map[string]*mms.Value)

	for _, d := range []string{"LD1", "LD2"} {
		if err := srv.RegisterDomain(d); err != nil {
			t.Fatalf("register domain %q: %v", d, err)
		}
	}

	for _, spec := range []struct {
		domain string
		itemID string
		ts     mms.TypeSpec
	}{
		{"LD1", "LLN0$ST$Mod$stVal", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}},
		{"LD2", "GGIO1$CO$SPCSO1$Oper$ctlVal", mms.TypeSpec{Type: mms.ValueTypeBoolean}},
	} {
		d, id := spec.domain, spec.itemID
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(d), ItemID: mms.ItemID(id)},
			TypeSpec: spec.ts,
			Read: func(_ context.Context) (*mms.Value, error) {
				mu.Lock()
				defer mu.Unlock()
				if v := store[d+"/"+id]; v != nil {
					return v, nil
				}
				return spec.ts.DefaultValue(), nil
			},
			Write: func(_ context.Context, val *mms.Value) error {
				mu.Lock()
				defer mu.Unlock()
				store[d+"/"+id] = val
				return nil
			},
		}); err != nil {
			t.Fatalf("register %s/%s: %v", d, id, err)
		}
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	reqs := []WriteRequest{
		{Ref: Ref{LD: "LD1", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}, Value: mms.NewInteger(5)},
		{Ref: Ref{LD: "LD2", LN: "GGIO1", Path: []string{"SPCSO1", "Oper", "ctlVal"}, FC: FCCO}, Value: mms.NewBoolean(true)},
	}

	results, err := client.WriteMultiple(ctx, reqs)
	if err != nil {
		t.Fatalf("WriteMultiple: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	for i, r := range results {
		if !r.Success {
			t.Errorf("write[%d] failed: %v", i, r.Err)
		}
	}
}
