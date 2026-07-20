// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestRead(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	v, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if v == nil {
		t.Fatal("Read returned nil value")
	}
	if v.Type() != mms.ValueTypeInteger {
		t.Errorf("Type() = %v, want Integer", v.Type())
	}
}

func TestReadRaw(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST}
	raw, err := client.ReadRaw(ctx, ref)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	if raw == nil {
		t.Fatal("ReadRaw returned nil value")
	}
	if raw.Type() != mms.ValueTypeBoolean {
		t.Errorf("Type() = %v, want Boolean", raw.Type())
	}
}

func TestRead_Quality(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "q"}, FC: FCST}
	v, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read quality: %v", err)
	}
	q, err := v.Quality()
	if err != nil {
		t.Fatalf("Quality: %v", err)
	}
	if q.Validity() != ValidityGood {
		t.Errorf("Validity = %d, want Good", q.Validity())
	}
}

func TestRead_Timestamp(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "t"}, FC: FCST}
	v, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read timestamp: %v", err)
	}
	ts, err := v.Timestamp()
	if err != nil {
		t.Fatalf("Timestamp: %v", err)
	}
	// Default UTC time from go-mms is zero time
	_ = ts
}

func TestRead_NoFC(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}}
	_, err := client.Read(ctx, ref)
	if err == nil {
		t.Fatal("expected error for ref without FC")
	}
	var refErr *ReferenceError
	if !errors.As(err, &refErr) {
		t.Errorf("expected ReferenceError, got %T: %v", err, err)
	}
}

func TestRead_NoLN(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", FC: FCST}
	_, err := client.Read(ctx, ref)
	if err == nil {
		t.Fatal("expected error for ref without LN")
	}
}

func TestRead_ClosedClient(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	_, err := client.Read(ctx, ref)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestReadComponent(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod"}, FC: FCST}
	v, err := client.ReadComponent(ctx, ref, "stVal")
	if err != nil {
		t.Fatalf("ReadComponent: %v", err)
	}
	if v == nil {
		t.Fatal("ReadComponent returned nil value")
	}
	if v.Type() != mms.ValueTypeInteger {
		t.Errorf("Type() = %v, want Integer", v.Type())
	}
}

func TestReadComponent_EmptyComponent(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod"}, FC: FCST}
	_, err := client.ReadComponent(ctx, ref, "")
	if err == nil {
		t.Fatal("expected error for empty component")
	}
}

func TestReadComponent_NotObject(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", FC: FCST}
	_, err := client.ReadComponent(ctx, ref, "stVal")
	if err == nil {
		t.Fatal("expected error for non-object ref")
	}
	var refErr *ReferenceError
	if !errors.As(err, &refErr) {
		t.Errorf("expected ReferenceError, got %T: %v", err, err)
	}
}

func TestReadComponent_InvalidComponent(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod"}, FC: FCST}
	_, err := client.ReadComponent(ctx, ref, "a.b")
	if err == nil {
		t.Fatal("expected error for component with separator")
	}
}

// TestRead_LNLevel verifies that LN-level reads (LD/LN[FC] without
// a data object path) are accepted by the client and passed to the
// server. This test registers a structured variable at the LN$FC
// level to simulate a server that supports LN-level reads.
func TestRead_LNLevel(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	domain := "testLD"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	structVal := mms.NewStructure([]*mms.Value{
		mms.NewInteger(42),
		mms.NewBoolean(true),
	})

	if err := srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(domain),
			ItemID: "LLN0$ST",
		},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeStructure},
		Read: func(_ context.Context) (*mms.Value, error) {
			return structVal, nil
		},
	}); err != nil {
		t.Fatalf("register variable: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	ref := Ref{LD: domain, LN: "LLN0", FC: FCST}
	v, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("LN-level Read: %v", err)
	}
	if v == nil {
		t.Fatal("LN-level Read returned nil value")
	}
	if v.Type() != mms.ValueTypeStructure {
		t.Errorf("Type() = %v, want Structure", v.Type())
	}
}
