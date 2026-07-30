// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

func TestDecodeSGCB(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(4),
		mms.NewUnsigned(2),
		mms.NewUnsigned(0),
		mms.NewBoolean(false),
		mms.NewUTCTime(time.Time{}),
		mms.NewUnsigned(60),
	})

	info, err := decodeSGCB(v)
	if err != nil {
		t.Fatalf("decodeSGCB: %v", err)
	}
	if info.NumOfSGs != 4 {
		t.Errorf("NumOfSGs = %d, want 4", info.NumOfSGs)
	}
	if info.ActSG != 2 {
		t.Errorf("ActSG = %d, want 2", info.ActSG)
	}
	if info.EditSG != 0 {
		t.Errorf("EditSG = %d, want 0", info.EditSG)
	}
	if info.CnfEdit {
		t.Error("CnfEdit should be false")
	}
	if info.ResvTms != 60 {
		t.Errorf("ResvTms = %d, want 60", info.ResvTms)
	}
}

func TestDecodeSGCB_WithoutResvTms(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(2),
		mms.NewUnsigned(1),
		mms.NewUnsigned(0),
		mms.NewBoolean(false),
		mms.NewUTCTime(time.Time{}),
	})

	info, err := decodeSGCB(v)
	if err != nil {
		t.Fatalf("decodeSGCB: %v", err)
	}
	if info.NumOfSGs != 2 || info.ActSG != 1 {
		t.Errorf("unexpected: %+v", info)
	}
	if info.ResvTms != 0 {
		t.Errorf("ResvTms = %d, want 0", info.ResvTms)
	}
}

func TestDecodeSGCB_TooFewMembers(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(1),
		mms.NewUnsigned(1),
	})
	_, err := decodeSGCB(v)
	if err == nil {
		t.Error("expected error for too few members")
	}
}

func TestDecodeSGCB_WrongType(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("not a number"),
		mms.NewUnsigned(1),
		mms.NewUnsigned(0),
		mms.NewBoolean(false),
		mms.NewUTCTime(time.Time{}),
	})
	_, err := decodeSGCB(v)
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestDecodeSGCB_NotStructure(t *testing.T) {
	_, err := decodeSGCB(mms.NewInteger(42))
	if err == nil {
		t.Error("expected error for non-structure")
	}
}

func newTestSGServer(t *testing.T) *Server {
	t.Helper()
	model := &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				SettingGroup: &servermodel.SettingGroupDef{
					NumOfSGs: 4,
					ActSG:    1,
					ResvTms:  0,
				},
			}},
		}},
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestSettingGroupEngine_ActiveSG(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	engine := srv.SettingGroupEngine()
	if engine == nil {
		t.Fatal("engine is nil")
	}

	if got := engine.GetActiveSettingGroup("LD1"); got != 1 {
		t.Errorf("initial ActSG = %d, want 1", got)
	}

	ctx := context.Background()
	if err := srv.ChangeActiveSettingGroup(ctx, "LD1", 3); err != nil {
		t.Fatalf("ChangeActiveSettingGroup: %v", err)
	}
	if got := engine.GetActiveSettingGroup("LD1"); got != 3 {
		t.Errorf("ActSG after change = %d, want 3", got)
	}

	v := srv.ValueStore().Get(servermodel.StoreKey("LD1", "LLN0$SP$SGCB$ActSG"))
	if v == nil {
		t.Fatal("ActSG not in store")
	}
	u, ok := v.Uint32()
	if !ok || u != 3 {
		t.Errorf("store ActSG = %d, want 3", u)
	}
}

func TestSettingGroupEngine_ActiveSG_OutOfRange(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()
	if err := srv.ChangeActiveSettingGroup(ctx, "LD1", 0); err == nil {
		t.Error("expected error for sg=0")
	}
	if err := srv.ChangeActiveSettingGroup(ctx, "LD1", 5); err == nil {
		t.Error("expected error for sg=5 (max is 4)")
	}
}

func TestSettingGroupEngine_ActiveSG_Rejected(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{
		OnActiveSGChanged: func(_ context.Context, _ string, _ uint8) error {
			return fmt.Errorf("not allowed")
		},
	})

	ctx := context.Background()
	if err := srv.ChangeActiveSettingGroup(ctx, "LD1", 2); err == nil {
		t.Error("expected rejection")
	}
	if got := srv.SettingGroupEngine().GetActiveSettingGroup("LD1"); got != 1 {
		t.Errorf("ActSG should remain 1 after rejection, got %d", got)
	}
}

func TestSettingGroupEngine_EditSG(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	engine := srv.SettingGroupEngine()
	ctx := context.Background()

	if got := engine.GetEditSettingGroup("unknownLD"); got != 0 {
		t.Errorf("unknown LD EditSG = %d, want 0", got)
	}

	if got := engine.GetEditSettingGroup("LD1"); got != 0 {
		t.Errorf("initial EditSG = %d, want 0", got)
	}

	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(2)); err != nil {
		t.Fatalf("select edit SG: %v", err)
	}
	if got := engine.GetEditSettingGroup("LD1"); got != 2 {
		t.Errorf("EditSG = %d, want 2", got)
	}

	// Release edit session by writing 0.
	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(0)); err != nil {
		t.Fatalf("release edit SG: %v", err)
	}
	if got := engine.GetEditSettingGroup("LD1"); got != 0 {
		t.Errorf("EditSG after release = %d, want 0", got)
	}
}

func TestSettingGroupEngine_EditSG_Conflict(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	engine := srv.SettingGroupEngine()
	ctx := context.Background()

	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(2)); err != nil {
		t.Fatalf("first select: %v", err)
	}
	// Another client tries to edit a different group.
	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(3)); err == nil {
		t.Error("expected conflict error when another edit session is active")
	}
	// Same group re-select should succeed.
	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(2)); err != nil {
		t.Errorf("re-selecting same group should succeed: %v", err)
	}
}

func TestSettingGroupEngine_EditSG_Rejected(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{
		OnEditSGSelected: func(_ context.Context, _ string, _ uint8) error {
			return fmt.Errorf("busy")
		},
	})

	ctx := context.Background()
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(1)); err == nil {
		t.Error("expected rejection")
	}
}

func TestSettingGroupEngine_ConfirmEdit(t *testing.T) {
	srv := newTestSGServer(t)

	var confirmedSG uint8
	srv.EnableSettingGroups(SettingGroupHandler{
		OnConfirmEdit: func(_ context.Context, _ string, sg uint8) error {
			confirmedSG = sg
			return nil
		},
	})

	engine := srv.SettingGroupEngine()
	ctx := context.Background()

	// Select edit group.
	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(3)); err != nil {
		t.Fatalf("select edit: %v", err)
	}

	// Confirm.
	if err := engine.HandleSGCBWrite(ctx, "LD1", "CnfEdit", mms.NewBoolean(true)); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	if confirmedSG != 3 {
		t.Errorf("confirmedSG = %d, want 3", confirmedSG)
	}
	if got := engine.GetEditSettingGroup("LD1"); got != 0 {
		t.Errorf("EditSG should be 0 after confirm, got %d", got)
	}
}

func TestSettingGroupEngine_ConfirmEdit_NoSession(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "LD1", "CnfEdit", mms.NewBoolean(true)); err == nil {
		t.Error("expected error when no edit session")
	}
}

func TestSettingGroupEngine_ConfirmEdit_Rejected(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{
		OnConfirmEdit: func(_ context.Context, _ string, _ uint8) error {
			return fmt.Errorf("validation failed")
		},
	})

	engine := srv.SettingGroupEngine()
	ctx := context.Background()

	if err := engine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(2)); err != nil {
		t.Fatal(err)
	}
	if err := engine.HandleSGCBWrite(ctx, "LD1", "CnfEdit", mms.NewBoolean(true)); err == nil {
		t.Error("expected confirmation rejection")
	}
	// Edit session should remain active after rejected confirm.
	if got := engine.GetEditSettingGroup("LD1"); got != 2 {
		t.Errorf("EditSG should remain 2 after rejected confirm, got %d", got)
	}
}

func TestSettingGroupEngine_CnfEditFalse(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()
	// Writing false is a no-op.
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "LD1", "CnfEdit", mms.NewBoolean(false)); err != nil {
		t.Errorf("CnfEdit=false should be no-op: %v", err)
	}
}

func TestSettingGroupEngine_ReadOnlySubfield(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "LD1", "NumOfSGs", mms.NewUnsigned(10)); err == nil {
		t.Error("expected error for read-only subfield")
	}
}

func TestSettingGroupEngine_UnknownLD(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "UNKNOWN", "ActSG", mms.NewUnsigned(1)); err == nil {
		t.Error("expected error for unknown LD")
	}
}

func TestSettingGroupEngine_NotEnabled(t *testing.T) {
	srv := newTestSGServer(t)
	if srv.SettingGroupEngine() != nil {
		t.Error("engine should be nil before EnableSettingGroups")
	}

	ctx := context.Background()
	if err := srv.ChangeActiveSettingGroup(ctx, "LD1", 1); err == nil {
		t.Error("expected error when engine not enabled")
	}
}

func TestSettingGroupEngine_SGCBInStore(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	vs := srv.ValueStore()
	numOfSGs := vs.Get(servermodel.StoreKey("LD1", "LLN0$SP$SGCB$NumOfSGs"))
	if numOfSGs == nil {
		t.Fatal("NumOfSGs not in store")
	}
	u, ok := numOfSGs.Uint32()
	if !ok || u != 4 {
		t.Errorf("NumOfSGs = %d, want 4", u)
	}

	actSG := vs.Get(servermodel.StoreKey("LD1", "LLN0$SP$SGCB$ActSG"))
	if actSG == nil {
		t.Fatal("ActSG not in store")
	}
	u, ok = actSG.Uint32()
	if !ok || u != 1 {
		t.Errorf("ActSG = %d, want 1", u)
	}
}

func TestSettingGroupEngine_EditSG_OutOfRange(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(99)); err == nil {
		t.Error("expected error for out-of-range edit SG")
	}
}

func TestSettingGroupEngine_NoSGCBInModel(t *testing.T) {
	model := &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
			}},
		}},
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.EnableSettingGroups(SettingGroupHandler{})
	// No SGCB → engine exists but has no registrations.
	if got := srv.SettingGroupEngine().GetActiveSettingGroup("LD1"); got != 0 {
		t.Errorf("expected 0 for LD without SGCB, got %d", got)
	}
}
