// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// sgSpecServer serves LD1 with an SGCB (NumOfSG 3) and an FC=SE
// attribute LLN0.Set1.setVal.
func sgSpecServer(t *testing.T, resvTms uint16, h SettingGroupHandler) *Server {
	t.Helper()
	model := &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []servermodel.LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			DataObjects: []servermodel.DataObject{{
				Name: "Set1",
				Attributes: []servermodel.DataAttribute{
					{Name: "setVal", FC: "SE", BType: "INT32"},
					{Name: "setVal", FC: "SG", BType: "INT32"},
				},
			}},
			SettingGroup: &servermodel.SettingGroupDef{NumOfSGs: 3, ActSG: 1, ResvTms: resvTms},
		}},
	}}}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	srv.EnableSettingGroups(h)
	return srv
}

var seRef = Ref{LD: "LD1", LN: "LLN0", Path: []string{"Set1", "setVal"}}

// IEC 61850-7-2 16.3.3.1: the edit reservation belongs to one client; it
// ends with ConfirmEditSGValues, SelectEditSG 0, ResvTms or association
// loss. Another client gets temporarily-unavailable (IEC 61850-8-1
// 8.1.3.4.4.2, instance-locked-by-other-client).
func TestSpec_7_2_16_3_3_1_EditReservation(t *testing.T) {
	srv := sgSpecServer(t, 0, SettingGroupHandler{})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)

	if err := a.SelectEditSG(ctx, "LD1", 2); err != nil {
		t.Fatal(err)
	}
	refused := []struct {
		name string
		err  error
	}{
		{"B SelectEditSG(2)", b.SelectEditSG(ctx, "LD1", 2)},
		{"B SelectEditSG(3)", b.SelectEditSG(ctx, "LD1", 3)},
		{"B SelectEditSG(0)", b.SelectEditSG(ctx, "LD1", 0)},
		{"B ConfirmEditSG", b.ConfirmEditSG(ctx, "LD1")},
		{"B SetEditSGValue", b.SetEditSGValue(ctx, seRef, mms.NewInteger(5))},
	}
	for _, r := range refused {
		if dataAccessCode(r.err) != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("%s = %v, want temporarily-unavailable", r.name, r.err)
		}
	}
	if err := a.SetEditSGValue(ctx, seRef, mms.NewInteger(4)); err != nil {
		t.Errorf("A SetEditSGValue: %v", err)
	}
	// B may read the edit value (7-2 restricts the reservation, not reads).
	if _, err := b.GetEditSGValue(ctx, seRef); err != nil {
		t.Errorf("B GetEditSGValue: %v", err)
	}
	// SG-01: SelectEditSG 0 cancels A's reservation, then B may select.
	if err := a.CancelEditSG(ctx, "LD1"); err != nil {
		t.Fatalf("A CancelEditSG: %v", err)
	}
	if info, err := a.GetSettingGroupInfo(ctx, "LD1"); err != nil || info.EditSG != 0 {
		t.Fatalf("after cancel: %+v %v", info, err)
	}
	if err := b.SelectEditSG(ctx, "LD1", 3); err != nil {
		t.Fatalf("B after cancel: %v", err)
	}
	// Association loss releases B's reservation.
	if err := b.mmsClient.Close(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return a.SelectEditSG(ctx, "LD1", 1) == nil })
	if err := a.ConfirmEditSG(ctx, "LD1"); err != nil {
		t.Fatalf("A confirm: %v", err)
	}
}

// IEC 61850-7-2 16.2.2.8 / 16.3.3.1: the reservation ends ResvTms seconds
// after SelectEditSG without a ConfirmEditSGValues.
func TestSpec_7_2_16_2_2_8_ResvTmsExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := sgSpecServer(t, 1, SettingGroupHandler{})
		a, b := controlClient(t, srv), controlClient(t, srv)
		ctx := waitCtx(t)
		if err := a.SelectEditSG(ctx, "LD1", 1); err != nil {
			t.Fatal(err)
		}
		if err := b.SelectEditSG(ctx, "LD1", 1); dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
			t.Fatalf("B before ResvTms = %v", err)
		}
		time.Sleep(1500 * time.Millisecond) // fake time: beyond ResvTms of 1 s
		synctest.Wait()                     // the ResvTms timer has run
		if info, err := b.GetSettingGroupInfo(ctx, "LD1"); err != nil || info.EditSG != 0 {
			t.Fatalf("after ResvTms: %+v %v", info, err)
		}
		if err := b.SelectEditSG(ctx, "LD1", 2); err != nil {
			t.Fatalf("B after ResvTms: %v", err)
		}
		if err := a.ConfirmEditSG(ctx, "LD1"); dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("A confirm after expiry = %v", err)
		}
	})
}

// IEC 61850-7-2 16.2.2.5: with EditSG = 0, SetEditSGValue and
// GetEditSGValue get Response- (temporarily-unavailable, IEC 61850-8-1
// 8.1.3.4.4.2: writability depends on another object).
func TestSpec_7_2_16_2_2_5_SEWithoutEditSG(t *testing.T) {
	srv := sgSpecServer(t, 0, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	if err := c.SetEditSGValue(ctx, seRef, mms.NewInteger(5)); dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("SetEditSGValue without edit = %v", err)
	}
	if _, err := c.GetEditSGValue(ctx, seRef); dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("GetEditSGValue without edit = %v", err)
	}
	if err := c.SelectEditSG(ctx, "LD1", 1); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, seRef, mms.NewInteger(5)); err != nil {
		t.Errorf("SetEditSGValue with edit: %v", err)
	}
	if v, err := c.GetEditSGValue(ctx, seRef); err != nil {
		t.Errorf("GetEditSGValue with edit: %v", err)
	} else if i, _ := v.Int64(); i != 5 {
		t.Errorf("SE = %d, want 5", i)
	}
}

// IEC 61850-8-1 16.2 / 8.1.3.4.4.2 Table 23: SGCB write errors.
func TestSpec_8_1_16_2_SGCBWriteErrorCodes(t *testing.T) {
	refuse := errors.New("refused by application")
	srv := sgSpecServer(t, 0, SettingGroupHandler{
		OnActiveSGChanged: func(_ context.Context, _ string, sg uint8) error {
			if sg == 3 {
				return refuse
			}
			if sg == 2 {
				return ErrWriteHardwareFault
			}
			return nil
		},
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	sgcb := sgcbObjectName("LD1")
	tests := []struct {
		name  string
		field string
		val   *mms.Value
		want  mms.DataAccessErrorCode
	}{
		{"ActSG 0", "ActSG", mms.NewUnsigned(0), mms.DataAccessErrorObjectValueInvalid},
		{"ActSG > NumOfSG", "ActSG", mms.NewUnsigned(4), mms.DataAccessErrorObjectValueInvalid},
		{"ActSG not unsigned", "ActSG", mms.NewBoolean(true), mms.DataAccessErrorTypeInconsistent},
		{"ActSG refused", "ActSG", mms.NewUnsigned(3), mms.DataAccessErrorObjectAccessDenied},
		{"ActSG sentinel", "ActSG", mms.NewUnsigned(2), mms.DataAccessErrorHardwareFault},
		{"EditSG > NumOfSG", "EditSG", mms.NewUnsigned(9), mms.DataAccessErrorObjectValueInvalid},
		{"NumOfSG read-only", "NumOfSG", mms.NewUnsigned(2), mms.DataAccessErrorObjectAccessDenied},
		{"CnfEdit without edit", "CnfEdit", mms.NewBoolean(true), mms.DataAccessErrorTemporarilyUnavailable},
		{"CnfEdit not boolean", "CnfEdit", mms.NewUnsigned(1), mms.DataAccessErrorTypeInconsistent},
		{"ActSG ok", "ActSG", mms.NewUnsigned(1), mms.DataAccessErrorNone},
	}
	for _, tc := range tests {
		err := c.mmsClient.WriteComponent(ctx, sgcb, tc.field, tc.val)
		if got := dataAccessCode(err); got != tc.want {
			t.Errorf("%s: %v (%v), want %v", tc.name, got, err, tc.want)
		}
	}
	if err := srv.SettingGroupEngine().HandleSGCBWrite(ctx, "LDX", "ActSG", mms.NewUnsigned(1)); dataAccessCode(err) != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("unknown LD = %v", err)
	}
}

// SG-05: GetSettingGroupInfo surfaces LActTm (IEC 61850-8-1 Table 60).
func TestDecodeSGCB_LActTm(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	info, err := decodeSGCB(mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(3), mms.NewUnsigned(1), mms.NewUnsigned(0), mms.NewBoolean(false),
		mms.NewUTCTimeWithQuality(when, 0x8a),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !info.LActTm.Time.Equal(when) || !info.LActTm.Quality.LeapSecondKnown || info.LActTm.Quality.TimeAccuracy != 10 {
		t.Errorf("LActTm = %+v", info.LActTm)
	}
	info, err = decodeSGCB(mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(3), mms.NewUnsigned(1), mms.NewUnsigned(0), mms.NewBoolean(false), mms.NewBoolean(false),
	}))
	if err != nil || !info.LActTm.IsZero() {
		t.Errorf("non-time LActTm: %+v %v", info, err)
	}
}
