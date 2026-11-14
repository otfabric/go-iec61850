// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms"
)

func sgcbTestServer(t *testing.T) (*ValueStore, *mms.Client) {
	t.Helper()
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10},
	})
	m := &Model{LogicalDevices: []LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			DataObjects: []DataObject{{Name: "Set1", Attributes: []DataAttribute{
				{Name: "setVal", FC: "SE", BType: "INT32"},
				{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
			}}},
			SettingGroup: &SettingGroupDef{NumOfSGs: 3, ActSG: 1, ResvTms: 30},
		}},
	}}}
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return vs, client
}

// IEC 61850-7-2 Table 36, IEC 61850-8-1 8.1.3.4.4.2: NumOfSG, LActTm and
// ResvTms are read-only (object-access-denied) and a written component
// must match its type (type-inconsistent), whether or not a setting
// group engine intercepts the writes.
func TestSpec_8_1_SGCBComponentWrite(t *testing.T) {
	vs, client := sgcbTestServer(t)
	ctx := context.Background()
	write := func(item string, val *mms.Value) error {
		_, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: mms.ItemID("LLN0$SP$SGCB$" + item), Value: val})
		return err
	}
	for item, val := range map[string]*mms.Value{
		"NumOfSG": mms.NewUnsigned(9),
		"LActTm":  mms.NewUnsigned(1),
		"ResvTms": mms.NewUnsigned(1),
	} {
		if err := write(item, val); daeCode(err) != mms.DataAccessErrorObjectAccessDenied {
			t.Errorf("%s write = %v, want object-access-denied", item, err)
		}
	}
	if u, _ := vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$NumOfSG")).Uint64(); u != 3 {
		t.Errorf("NumOfSG = %d after refused write", u)
	}
	for item, val := range map[string]*mms.Value{
		"ActSG":   mms.NewVisibleString("x"),
		"EditSG":  mms.NewUnsigned(256),
		"CnfEdit": mms.NewUnsigned(1),
	} {
		if err := write(item, val); daeCode(err) != mms.DataAccessErrorTypeInconsistent {
			t.Errorf("%s write = %v, want type-inconsistent", item, err)
		}
	}
	if u, _ := vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$ActSG")).Uint64(); u != 1 {
		t.Errorf("ActSG = %d after refused write", u)
	}
	// A well-typed write of a writable component reaches the
	// interceptor (none here, so it is stored).
	if err := write("ActSG", mms.NewUnsigned(2)); err != nil {
		t.Errorf("ActSG write: %v", err)
	}
	if u, _ := vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$ActSG")).Uint64(); u != 2 {
		t.Errorf("ActSG = %d, want 2", u)
	}
}

// IEC 61850-7-2 16.2.2.5: GetEditSGValue without a selected edit group
// gets Response-, also through the FC-group (LN$SE) and data object
// (LN$SE$DO) reads; the bare LN read and other FCs are not guarded.
func TestSpec_7_2_SEGroupReadGuard(t *testing.T) {
	vs, client := sgcbTestServer(t)
	ctx := context.Background()
	allowed := false
	var guarded []string
	vs.SetEditGuard(func(_ context.Context, key string, write bool) error {
		guarded = append(guarded, key)
		if write || !allowed {
			return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
		}
		return nil
	})
	read := func(item string) error {
		_, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: mms.ItemID(item)})
		return err
	}
	for _, item := range []string{"LLN0$SE", "LLN0$SE$Set1"} {
		if err := read(item); daeCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("%s read = %v, want temporarily-unavailable", item, err)
		}
	}
	for _, item := range []string{"LLN0$ST", "LLN0$ST$Set1", "LLN0"} {
		if err := read(item); err != nil {
			t.Errorf("%s read = %v", item, err)
		}
	}
	allowed = true
	for _, item := range []string{"LLN0$SE", "LLN0$SE$Set1"} {
		if err := read(item); err != nil {
			t.Errorf("%s read with edit group = %v", item, err)
		}
	}
	if len(guarded) != 4 {
		t.Errorf("edit guard calls = %v", guarded)
	}
}
