// SPDX-License-Identifier: MIT

package iec61850

import (
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func TestCtlModel_String(t *testing.T) {
	tests := []struct {
		m    CtlModel
		want string
	}{
		{CtlModelStatusOnly, "status-only"},
		{CtlModelDirectNormal, "direct-with-normal-security"},
		{CtlModelSBONormal, "sbo-with-normal-security"},
		{CtlModelDirectEnhanced, "direct-with-enhanced-security"},
		{CtlModelSBOEnhanced, "sbo-with-enhanced-security"},
		{CtlModel(99), "ctlModel(99)"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("CtlModel(%d).String() = %q, want %q", int(tt.m), got, tt.want)
		}
	}
}

func TestCtlModel_Predicates(t *testing.T) {
	if CtlModelStatusOnly.IsControllable() {
		t.Error("StatusOnly should not be controllable")
	}
	if !CtlModelDirectNormal.IsControllable() {
		t.Error("DirectNormal should be controllable")
	}
	if !CtlModelSBONormal.IsSBO() {
		t.Error("SBONormal should be SBO")
	}
	if CtlModelDirectNormal.IsSBO() {
		t.Error("DirectNormal should not be SBO")
	}
	if !CtlModelSBOEnhanced.IsEnhanced() {
		t.Error("SBOEnhanced should be enhanced")
	}
	if CtlModelSBONormal.IsEnhanced() {
		t.Error("SBONormal should not be enhanced")
	}
}

func TestOrCat_String(t *testing.T) {
	tests := []struct {
		c    OrCat
		want string
	}{
		{OrCatNotSupported, "not-supported"},
		{OrCatBayControl, "bay-control"},
		{OrCatStationControl, "station-control"},
		{OrCatRemoteControl, "remote-control"},
		{OrCatAutomaticBay, "automatic-bay"},
		{OrCatAutomaticStation, "automatic-station"},
		{OrCatAutomaticRemote, "automatic-remote"},
		{OrCatMaintenance, "maintenance"},
		{OrCatProcess, "process"},
		{OrCat(99), "orCat(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("OrCat(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestOrigin_toMMS(t *testing.T) {
	o := Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1, 2, 3}}
	v := o.toMMS()
	members, ok := v.Structure()
	if !ok || len(members) != 2 {
		t.Fatalf("expected structure with 2 members, got %v (ok=%v)", v, ok)
	}
	cat, ok := members[0].Int32()
	if !ok || cat != int32(OrCatRemoteControl) {
		t.Errorf("OrCat = %d, want %d", cat, OrCatRemoteControl)
	}
	ident, ok := members[1].OctetString()
	if !ok || len(ident) != 3 {
		t.Errorf("OrIdent len = %d, want 3", len(ident))
	}
}

// The Check bits on the wire: synchrocheck is bit 0 (0x80) and
// interlock-check bit 1 (0x40), as libiec61850 reads them
// (src/iec61850/server/mms_mapping/control.c: synchroCheck =
// MmsValue_getBitStringBit(check, 0), interlockCheck = ...(check, 1)).
func TestCheckConditions_Wire(t *testing.T) {
	for _, tt := range []struct {
		c    CheckConditions
		wire byte
	}{
		{0, 0x00},
		{CheckSynchroCheck, 0x80},
		{CheckInterlockCheck, 0x40},
		{CheckSynchroCheck | CheckInterlockCheck, 0xC0},
	} {
		v := tt.c.toMMS()
		bits, ok := v.BitString()
		if !ok || len(bits) != 1 || bits[0] != tt.wire {
			t.Errorf("toMMS(%02b) = %08b, want %08b", tt.c, bits, tt.wire)
		}
		if got := checkFromMMS(tt.wire); got != tt.c {
			t.Errorf("checkFromMMS(%08b) = %02b, want %02b", tt.wire, got, tt.c)
		}
	}
	// Unused trailing bits are ignored.
	if got := checkFromMMS(0x9F); got != CheckSynchroCheck {
		t.Errorf("checkFromMMS(10011111) = %02b", got)
	}
}

func TestBuildOper_Structure(t *testing.T) {
	params := OperateParams{
		CtlVal: BoolCtlVal(true),
		Origin: &Origin{OrCat: OrCatRemoteControl},
		CtlNum: 42,
		Test:   true,
		Check:  CheckInterlockCheck,
	}

	v := buildOper(params)
	members, ok := v.Structure()
	if !ok {
		t.Fatal("expected structure")
	}
	// Without OperTm: ctlVal, origin, ctlNum, T, Test, Check = 6 members.
	if len(members) != 6 {
		t.Fatalf("expected 6 members (no operTm), got %d", len(members))
	}

	ctlVal, ok := members[0].Bool()
	if !ok || !ctlVal {
		t.Error("ctlVal should be true")
	}

	// origin at index 1 (no operTm)
	ctlNum, ok := members[2].Uint32()
	if !ok || ctlNum != 42 {
		t.Errorf("ctlNum = %d, want 42", ctlNum)
	}

	// Test at index 4 (ctlNum+2; T is at ctlNum+1)
	test, ok := members[4].Bool()
	if !ok || !test {
		t.Error("Test should be true")
	}
}

func TestBuildOper_DefaultOrigin(t *testing.T) {
	params := OperateParams{CtlVal: BoolCtlVal(false)}
	v := buildOper(params)
	members, _ := v.Structure()
	// origin is at index 1 when there is no operTm
	originMembers, ok := members[1].Structure()
	if !ok || len(originMembers) < 2 {
		t.Fatal("expected origin structure")
	}
	cat, _ := originMembers[0].Int32()
	if cat != int32(OrCatRemoteControl) {
		t.Errorf("default origin should use OrCatRemoteControl, got %d", cat)
	}
}

func TestBuildOper_WithOperTm(t *testing.T) {
	scheduled := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	params := OperateParams{
		CtlVal: IntCtlVal(1),
		OperTm: scheduled,
	}
	v := buildOper(params)
	members, _ := v.Structure()
	operTm, ok := members[1].UTCTime()
	if !ok {
		t.Fatal("expected UTCTime for operTm")
	}
	if !operTm.Equal(scheduled) {
		t.Errorf("operTm = %v, want %v", operTm, scheduled)
	}
}

func TestBuildCancel_Structure(t *testing.T) {
	params := CancelParams{
		CtlVal: BoolCtlVal(true),
		Origin: &Origin{OrCat: OrCatMaintenance},
		CtlNum: 7,
	}

	v := buildCancel(params)
	members, ok := v.Structure()
	if !ok {
		t.Fatal("expected structure")
	}
	// Without OperTm: ctlVal, origin, ctlNum, T, Test = 5 members.
	if len(members) != 5 {
		t.Fatalf("expected 5 members (no operTm), got %d", len(members))
	}

	testBit, ok := members[4].Bool()
	if !ok || testBit {
		t.Error("Cancel Test bit should be false")
	}
}

func TestBuildCancel_DefaultOriginAndOperTm(t *testing.T) {
	// Nil Origin → OrCatRemoteControl default.
	v := buildCancel(CancelParams{CtlVal: BoolCtlVal(false), CtlNum: 1})
	members, ok := v.Structure()
	if !ok || len(members) != 5 {
		t.Fatalf("default origin: %v len=%d", ok, len(members))
	}

	scheduled := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	v = buildCancel(CancelParams{
		CtlVal: BoolCtlVal(true),
		OperTm: scheduled,
		CtlNum: 2,
	})
	members, ok = v.Structure()
	if !ok || len(members) != 6 {
		t.Fatalf("with OperTm: %v len=%d", ok, len(members))
	}
	operTm, ok := members[1].UTCTime()
	if !ok || !operTm.Equal(scheduled) {
		t.Fatalf("operTm = %v ok=%v", operTm, ok)
	}
}

func TestAddCause_String(t *testing.T) {
	// Values and names of IEC 61850-7-2 AddCause, as libiec61850
	// (iec61850_common.h, ControlAddCause) encodes them.
	tests := []struct {
		c    AddCause
		n    int
		want string
	}{
		{AddCauseUnknown, 0, "unknown"},
		{AddCauseNotSupported, 1, "not-supported"},
		{AddCauseBlocked, 2, "blocked-by-switching-hierarchy"},
		{AddCauseSelectFailed, 3, "select-failed"},
		{AddCauseInvalidPosition, 4, "invalid-position"},
		{AddCausePositionReached, 5, "position-reached"},
		{AddCauseParameterChange, 6, "parameter-change-in-execution"},
		{AddCauseStepLimit, 7, "step-limit"},
		{AddCauseBlockedByMode, 8, "blocked-by-mode"},
		{AddCauseBlockedByProcess, 9, "blocked-by-process"},
		{AddCauseBlockedByInterlocking, 10, "blocked-by-interlocking"},
		{AddCauseBlockedBySynchrocheck, 11, "blocked-by-synchrocheck"},
		{AddCauseCommandAlreadyExec, 12, "command-already-in-execution"},
		{AddCauseBlockedByHealth, 13, "blocked-by-health"},
		{AddCause1ofN, 14, "1-of-n-control"},
		{AddCauseAbortionByCancel, 15, "abortion-by-cancel"},
		{AddCauseTimeLimit, 16, "time-limit-over"},
		{AddCauseAbortionByTrip, 17, "abortion-by-trip"},
		{AddCauseObjectNotSelected, 18, "object-not-selected"},
		{AddCauseObjectAlreadySelected, 19, "object-already-selected"},
		{AddCauseNoAccessAuthority, 20, "no-access-authority"},
		{AddCauseEndedWithOvershoot, 21, "ended-with-overshoot"},
		{AddCauseAbortionDueToDeviation, 22, "abortion-due-to-deviation"},
		{AddCauseAbortionByCommunicationLoss, 23, "abortion-by-communication-loss"},
		{AddCauseBlockedByCommand, 24, "blocked-by-command"},
		{AddCauseNone, 25, "none"},
		{AddCauseInconsistentParameters, 26, "inconsistent-parameters"},
		{AddCauseLockedByOtherClient, 27, "locked-by-other-client"},
		{AddCauseBlockedBySwitch, 2, "blocked-by-switching-hierarchy"},
		{AddCause1of1, 14, "1-of-n-control"},
		{AddCauseAbort, 15, "abortion-by-cancel"},
		{AddCause(99), 99, "addCause(99)"},
		{AddCause(-1), -1, "addCause(-1)"},
	}
	for _, tt := range tests {
		if int(tt.c) != tt.n {
			t.Errorf("%s = %d, want %d", tt.want, int(tt.c), tt.n)
		}
		if got := tt.c.String(); got != tt.want {
			t.Errorf("AddCause(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestCtlValConstructors(t *testing.T) {
	if b, ok := BoolCtlVal(true).Bool(); !ok || !b {
		t.Error("BoolCtlVal(true) should produce true boolean")
	}
	if i, ok := IntCtlVal(42).Int32(); !ok || i != 42 {
		t.Error("IntCtlVal(42) should produce int32(42)")
	}
	if f, ok := FloatCtlVal(3.14).Float64(); !ok || f < 3.13 || f > 3.15 {
		t.Error("FloatCtlVal(3.14) should produce ~3.14")
	}
	if s, ok := StringCtlVal("test").VisibleString(); !ok || s != "test" {
		t.Error("StringCtlVal should produce visible string")
	}
}

func TestDpCtlVal(t *testing.T) {
	on := DpCtlVal(true)
	bits, ok := on.BitString()
	if !ok || len(bits) == 0 {
		t.Fatal("expected bitstring for DpCtlVal(true)")
	}
	if bits[0]&0xC0 != 0x80 {
		t.Errorf("DpCtlVal(true) top 2 bits = %02x, want 10", bits[0]>>6)
	}

	off := DpCtlVal(false)
	bits, ok = off.BitString()
	if !ok || len(bits) == 0 {
		t.Fatal("expected bitstring for DpCtlVal(false)")
	}
	if bits[0]&0xC0 != 0x40 {
		t.Errorf("DpCtlVal(false) top 2 bits = %02x, want 01", bits[0]>>6)
	}
}

func TestDecodeLastApplError(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("LD/LN.SPCSO1"),
		mms.NewInteger(5),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(int64(OrCatRemoteControl)),
			mms.NewOctetString([]byte{10, 0, 0, 1}),
		}),
		mms.NewInteger(int64(AddCauseBlockedByInterlocking)),
	})

	lae, err := decodeLastApplError(v)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if lae.CntrlObj != "LD/LN.SPCSO1" {
		t.Errorf("CntrlObj = %q, want LD/LN.SPCSO1", lae.CntrlObj)
	}
	if lae.Error != 5 {
		t.Errorf("Error = %d, want 5", lae.Error)
	}
	if lae.Origin.OrCat != OrCatRemoteControl {
		t.Errorf("Origin.OrCat = %d, want RemoteControl", lae.Origin.OrCat)
	}
	if lae.AddCause != AddCauseBlockedByInterlocking {
		t.Errorf("AddCause = %d, want BlockedByInterlocking", lae.AddCause)
	}
}
