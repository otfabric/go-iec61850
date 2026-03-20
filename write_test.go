package iec61850

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/otfabric/go-mms"
)

func setupWritableLoopback(t *testing.T) (*Client, *writableVars) {
	t.Helper()
	ctx := context.Background()

	wv := &writableVars{
		values: make(map[string]*mms.Value),
	}

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	domain := "simpleIOGenericIO"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	vars := []struct {
		itemID   string
		typeSpec mms.TypeSpec
		initial  *mms.Value
	}{
		{"LLN0$ST$Mod$stVal", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}, mms.NewInteger(1)},
		{"LLN0$ST$Mod$q", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}, mms.NewBitStringWithLength([]byte{0, 0}, 13)},
		{"LLN0$SP$Mod$ctlModel", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}, mms.NewInteger(0)},
		{"GGIO1$ST$Ind1$stVal", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
		{"GGIO1$CO$SPCSO1$Oper$ctlVal", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
	}

	for _, v := range vars {
		key := domain + "/" + v.itemID
		wv.mu.Lock()
		wv.values[key] = v.initial
		wv.mu.Unlock()

		capturedKey := key
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(v.itemID)},
			TypeSpec: v.typeSpec,
			Read: func(_ context.Context) (*mms.Value, error) {
				wv.mu.Lock()
				defer wv.mu.Unlock()
				return wv.values[capturedKey], nil
			},
			Write: func(_ context.Context, val *mms.Value) error {
				wv.mu.Lock()
				defer wv.mu.Unlock()
				wv.values[capturedKey] = val
				return nil
			},
		}); err != nil {
			t.Fatalf("register variable %q: %v", v.itemID, err)
		}
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

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client, wv
}

type writableVars struct {
	mu     sync.Mutex
	values map[string]*mms.Value
}

func TestWrite(t *testing.T) {
	client, _ := setupWritableLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}

	err := client.Write(ctx, ref, mms.NewInteger(2))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read back to verify
	v, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	i, err := v.Int64()
	if err != nil {
		t.Fatalf("Int64: %v", err)
	}
	if i != 2 {
		t.Errorf("read-back = %d, want 2", i)
	}
}

func TestWrite_Bool(t *testing.T) {
	client, _ := setupWritableLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST}

	err := client.Write(ctx, ref, mms.NewBoolean(true))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	v, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	b, err := v.Bool()
	if err != nil {
		t.Fatalf("Bool: %v", err)
	}
	if !b {
		t.Error("read-back = false, want true")
	}
}

func TestWrite_NoFC(t *testing.T) {
	client, _ := setupWritableLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}}
	err := client.Write(ctx, ref, mms.NewInteger(1))
	if err == nil {
		t.Fatal("expected error for ref without FC")
	}
}

func TestWrite_NotObject(t *testing.T) {
	client, _ := setupWritableLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", FC: FCST}
	err := client.Write(ctx, ref, mms.NewInteger(1))
	if err == nil {
		t.Fatal("expected error for non-object ref")
	}
}

func TestWrite_NilValue(t *testing.T) {
	client, _ := setupWritableLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	err := client.Write(ctx, ref, nil)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
}

func TestWrite_ClosedClient(t *testing.T) {
	client, _ := setupWritableLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	err := client.Write(ctx, ref, mms.NewInteger(1))
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}
