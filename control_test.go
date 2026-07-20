// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

var zeroTime time.Time

func TestValidateControlRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     Ref
		wantErr bool
	}{
		{
			name:    "valid",
			ref:     Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}},
			wantErr: false,
		},
		{
			name:    "empty LD",
			ref:     Ref{LN: "GGIO1", Path: []string{"SPCSO1"}},
			wantErr: true,
		},
		{
			name:    "empty LN",
			ref:     Ref{LD: "LD1", Path: []string{"SPCSO1"}},
			wantErr: true,
		},
		{
			name:    "no path",
			ref:     Ref{LD: "LD1", LN: "GGIO1"},
			wantErr: true,
		},
		{
			name:    "FC set",
			ref:     Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}, FC: FCCO},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateControlRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateControlRef() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestControlSubRef(t *testing.T) {
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}

	operRef := controlSubRef(ref, "Oper")
	if operRef.FC != FCCO {
		t.Errorf("FC = %q, want CO", operRef.FC)
	}
	if len(operRef.Path) != 2 || operRef.Path[1] != "Oper" {
		t.Errorf("Path = %v, want [SPCSO1 Oper]", operRef.Path)
	}

	sboRef := controlSubRef(ref, "SBO")
	if len(sboRef.Path) != 2 || sboRef.Path[1] != "SBO" {
		t.Errorf("Path = %v, want [SPCSO1 SBO]", sboRef.Path)
	}

	if operRef.Path[0] != ref.Path[0] {
		t.Error("original ref path should not be modified")
	}
}

func TestOperate_NilCtlVal(t *testing.T) {
	c := &Client{
		logger:    discardLogger(),
		mmsClient: &mms.Client{},
	}

	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	err := c.Operate(context.Background(), ref, OperateParams{})
	if err == nil {
		t.Fatal("expected error for nil CtlVal")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestOperate_ClosedClient(t *testing.T) {
	c := &Client{
		logger:    discardLogger(),
		mmsClient: &mms.Client{},
		state:     clientClosed,
	}

	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	err := c.Operate(context.Background(), ref, OperateParams{CtlVal: BoolCtlVal(true)})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestSelectWithValue_NilCtlVal(t *testing.T) {
	c := &Client{
		logger:    discardLogger(),
		mmsClient: &mms.Client{},
	}

	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	err := c.SelectWithValue(context.Background(), ref, OperateParams{})
	if err == nil {
		t.Fatal("expected error for nil CtlVal")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestCancel_NilCtlVal(t *testing.T) {
	c := &Client{
		logger:    discardLogger(),
		mmsClient: &mms.Client{},
	}

	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	err := c.Cancel(context.Background(), ref, CancelParams{})
	if err == nil {
		t.Fatal("expected error for nil CtlVal")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestDecodeControlRequest_Oper(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true),
		mms.NewUTCTime(zeroTime),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(int64(OrCatRemoteControl)),
			mms.NewOctetString(nil),
		}),
		mms.NewUnsigned(42),
		mms.NewUTCTime(zeroTime),
		mms.NewBoolean(false),
		mms.NewBitStringWithLength([]byte{0xC0}, 2),
	})

	req, err := decodeControlRequest("LD/LN.DO", "Oper", v)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if req.Operation != "operate" {
		t.Errorf("Operation = %q, want operate", req.Operation)
	}
	if req.CtlNum != 42 {
		t.Errorf("CtlNum = %d, want 42", req.CtlNum)
	}
	if req.Origin.OrCat != OrCatRemoteControl {
		t.Errorf("Origin.OrCat = %d, want RemoteControl", req.Origin.OrCat)
	}
	if req.Check != CheckSynchroCheck|CheckInterlockCheck {
		t.Errorf("Check = %d, want SynchroCheck|InterlockCheck", req.Check)
	}
}

func TestDecodeControlRequest_Cancel(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true),
		mms.NewUTCTime(zeroTime),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(int64(OrCatMaintenance)),
			mms.NewOctetString(nil),
		}),
		mms.NewUnsigned(7),
		mms.NewUTCTime(zeroTime),
	})

	req, err := decodeControlRequest("LD/LN.DO", "Cancel", v)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if req.Operation != "Cancel" {
		t.Errorf("Operation = %q, want Cancel", req.Operation)
	}
	if req.CtlNum != 7 {
		t.Errorf("CtlNum = %d, want 7", req.CtlNum)
	}
}

func TestDecodeControlRequest_InvalidStructure(t *testing.T) {
	v := mms.NewBoolean(true)
	_, err := decodeControlRequest("LD/LN.DO", "Oper", v)
	if err == nil {
		t.Fatal("expected error for non-structure value")
	}
}

func TestDecodeControlRequest_TooFewMembers(t *testing.T) {
	v := mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true),
	})
	_, err := decodeControlRequest("LD/LN.DO", "Oper", v)
	if err == nil {
		t.Fatal("expected error for too few members")
	}
}

func TestPathWithoutSuffix(t *testing.T) {
	path := []string{"SPCSO1", "Oper"}
	result := pathWithoutSuffix(path, "Oper")
	if len(result) != 1 || result[0] != "SPCSO1" {
		t.Errorf("pathWithoutSuffix = %v, want [SPCSO1]", result)
	}

	path2 := []string{"SPCSO1"}
	result2 := pathWithoutSuffix(path2, "Oper")
	if len(result2) != 1 {
		t.Errorf("pathWithoutSuffix should not remove non-matching suffix")
	}
}

func TestNextCtlNum(t *testing.T) {
	a := nextCtlNum()
	b := nextCtlNum()
	if a == b {
		t.Error("nextCtlNum should produce different values")
	}
}

func TestRegisterControl_StatusOnly(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelStatusOnly, ControlHandler{})
	if !errors.Is(err, ErrNotControllable) {
		t.Errorf("expected ErrNotControllable, got %v", err)
	}
}

func TestRegisterControl_Duplicate(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	err = s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{})
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegisterControl_EmptyArgs(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	err := s.RegisterControl("", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty LD, got %v", err)
	}
}

func TestServerControlDispatch_DirectOperate(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	var operateCalled bool
	handler := ControlHandler{
		OnOperate: func(ctx context.Context, req ControlRequest) error {
			operateCalled = true
			if req.CtlNum != 1 {
				return fmt.Errorf("unexpected ctlNum: %d", req.CtlNum)
			}
			return nil
		},
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, handler); err != nil {
		t.Fatalf("register: %v", err)
	}

	operVal := buildOper(OperateParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
	})

	err := s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
	if err != nil {
		t.Fatalf("handleControlWrite: %v", err)
	}
	if !operateCalled {
		t.Error("OnOperate handler was not called")
	}
}

func TestServerControlDispatch_SBOLifecycle(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	var selectCalled, operateCalled bool
	handler := ControlHandler{
		OnSelect: func(ctx context.Context, req ControlRequest) error {
			selectCalled = true
			return nil
		},
		OnOperate: func(ctx context.Context, req ControlRequest) error {
			operateCalled = true
			return nil
		},
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, handler); err != nil {
		t.Fatalf("register: %v", err)
	}

	sbowVal := buildOper(OperateParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
		Origin: &Origin{OrCat: OrCatRemoteControl},
	})

	err := s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "SBOw"}, "SBOw", sbowVal)
	if err != nil {
		t.Fatalf("SBOw: %v", err)
	}
	if !selectCalled {
		t.Error("OnSelect was not called")
	}

	operVal := buildOper(OperateParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
	})

	err = s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
	if err != nil {
		t.Fatalf("Oper after select: %v", err)
	}
	if !operateCalled {
		t.Error("OnOperate was not called")
	}
}

func TestServerControlDispatch_OperateWithoutSelect(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	operVal := buildOper(OperateParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
	})

	err := s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
	if err == nil {
		t.Fatal("expected error for operate without select")
	}
	if !errors.Is(err, ErrOperateFailed) {
		t.Errorf("expected ErrOperateFailed, got %v", err)
	}
}

func TestServerControlDispatch_Cancel(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	var cancelCalled bool
	handler := ControlHandler{
		OnCancel: func(ctx context.Context, req ControlRequest) error {
			cancelCalled = true
			return nil
		},
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, handler); err != nil {
		t.Fatalf("register: %v", err)
	}

	cancelVal := buildCancel(CancelParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
	})

	err := s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Cancel"}, "Cancel", cancelVal)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !cancelCalled {
		t.Error("OnCancel was not called")
	}
}

func TestServerControlDispatch_OperateRejected(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	handler := ControlHandler{
		OnOperate: func(ctx context.Context, req ControlRequest) error {
			return &ControlError{
				Ref:       req.Ref,
				Operation: "operate",
				AddCause:  AddCauseBlockedByInterlocking,
				Wrapped:   ErrOperateFailed,
			}
		},
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, handler); err != nil {
		t.Fatalf("register: %v", err)
	}

	operVal := buildOper(OperateParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
	})

	err := s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ControlError, got %T", err)
	}
	if ce.AddCause != AddCauseBlockedByInterlocking {
		t.Errorf("AddCause = %v, want BlockedByInterlocking", ce.AddCause)
	}
}

func TestServerControlDispatch_Unregistered(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	operVal := buildOper(OperateParams{
		CtlVal: BoolCtlVal(true),
		CtlNum: 1,
	})

	err := s.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
	if err != nil {
		t.Fatalf("unregistered control should be silently ignored, got: %v", err)
	}
}

func TestSBOOwnershipEnforcement(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	origin1 := &Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}}
	origin2 := &Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{2}}

	// Client 1 selects.
	sboVal := mms.NewVisibleString("select")
	selectReq, _ := decodeControlRequest("LD1/GGIO1.SPCSO1", "SBO", sboVal)
	selectReq.Origin = *origin1
	selectReq.CtlNum = 1

	reg := s.controls["LD1/GGIO1.SPCSO1"]
	if err := s.executeSelect(context.Background(), reg, selectReq); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Client 2 tries to operate — should be denied (owner mismatch).
	operReq := ControlRequest{
		Ref:       "LD1/GGIO1.SPCSO1",
		Operation: "operate",
		CtlVal:    BoolCtlVal(true),
		Origin:    *origin2,
		CtlNum:    1,
	}
	err := s.executeOperate(context.Background(), reg, operReq)
	if err == nil {
		t.Fatal("expected error for owner mismatch")
	}
	if !errors.Is(err, ErrOperateFailed) {
		t.Errorf("expected ErrOperateFailed, got %v", err)
	}

	// Client 1 operates — should succeed.
	operReq.Origin = *origin1
	err = s.executeOperate(context.Background(), reg, operReq)
	if err != nil {
		t.Fatalf("operate by owner should succeed: %v", err)
	}
}

func TestEnhancedSBO_CtlNumMismatch(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	origin := &Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}}
	reg := s.controls["LD1/GGIO1.SPCSO1"]

	// Select with ctlNum=5.
	selectReq := ControlRequest{
		Ref:       "LD1/GGIO1.SPCSO1",
		Operation: "select",
		Origin:    *origin,
		CtlNum:    5,
		CtlVal:    BoolCtlVal(true),
	}
	if err := s.executeSelect(context.Background(), reg, selectReq); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Operate with mismatched ctlNum=6.
	operReq := ControlRequest{
		Ref:       "LD1/GGIO1.SPCSO1",
		Operation: "operate",
		Origin:    *origin,
		CtlNum:    6,
		CtlVal:    BoolCtlVal(true),
	}
	err := s.executeOperate(context.Background(), reg, operReq)
	if err == nil {
		t.Fatal("expected error for ctlNum mismatch in enhanced mode")
	}
	if !errors.Is(err, ErrOperateFailed) {
		t.Errorf("expected ErrOperateFailed, got %v", err)
	}
}

func TestDecodeLastApplError_Strict(t *testing.T) {
	// Valid input should still work.
	valid := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("LD/LN.DO"),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(int64(OrCatRemoteControl)),
			mms.NewOctetString([]byte{1}),
		}),
		mms.NewInteger(int64(AddCauseSelectFailed)),
	})
	lae, err := decodeLastApplError(valid)
	if err != nil {
		t.Fatalf("valid input: %v", err)
	}
	if lae.CntrlObj != "LD/LN.DO" {
		t.Errorf("CntrlObj = %q", lae.CntrlObj)
	}

	// Wrong type for CntrlObj.
	bad1 := mms.NewStructure([]*mms.Value{
		mms.NewInteger(42), // wrong type
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(1),
			mms.NewOctetString([]byte{1}),
		}),
		mms.NewInteger(0),
	})
	if _, err := decodeLastApplError(bad1); err == nil {
		t.Error("expected error for wrong CntrlObj type")
	}

	// Wrong type for Error.
	bad2 := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("ref"),
		mms.NewVisibleString("not-an-int"), // wrong type
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(1),
			mms.NewOctetString([]byte{1}),
		}),
		mms.NewInteger(0),
	})
	if _, err := decodeLastApplError(bad2); err == nil {
		t.Error("expected error for wrong Error type")
	}

	// Origin not a structure.
	bad3 := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("ref"),
		mms.NewInteger(1),
		mms.NewInteger(42), // not a structure
		mms.NewInteger(0),
	})
	if _, err := decodeLastApplError(bad3); err == nil {
		t.Error("expected error for Origin not being a structure")
	}
}

func TestPathWithoutSuffix_DefensiveCopy(t *testing.T) {
	original := []string{"A", "B", "Oper"}
	result := pathWithoutSuffix(original, "Oper")

	// Modify result — should not affect original.
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}
	result[0] = "X"
	if original[0] != "A" {
		t.Error("modifying result corrupted original — not a defensive copy")
	}
}

func TestControlError_UnwrapMatchesBoth(t *testing.T) {
	inner := fmt.Errorf("inner-cause")
	err := &ControlError{
		Ref:       "LD/LN.DO",
		Operation: "operate",
		Wrapped:   inner,
	}
	if !errors.Is(err, ErrOperateFailed) {
		t.Error("should match ErrOperateFailed even with Wrapped set")
	}
	if !errors.Is(err, inner) {
		t.Error("should match inner cause")
	}
}

// TestControlHandler_PanicContainment verifies that a panic in an application
// callback is caught and returned as a ControlError, not a process crash.
func TestControlHandler_PanicContainment(t *testing.T) {
	ctx := context.Background()
	req := ControlRequest{Ref: "LD/LN.DO", Operation: "operate"}

	panicHandler := func(_ context.Context, _ ControlRequest) error {
		panic("test panic in control callback")
	}

	err := callControlHandler(panicHandler, ctx, req)
	if err == nil {
		t.Fatal("expected error from panicking handler, got nil")
	}
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ControlError from panic, got %T: %v", err, err)
	}
	if ce.Ref != req.Ref {
		t.Errorf("Ref = %q, want %q", ce.Ref, req.Ref)
	}
	if ce.Operation != req.Operation {
		t.Errorf("Operation = %q, want %q", ce.Operation, req.Operation)
	}
	t.Logf("panic correctly recovered: %v", err)
}

// TestControlHandler_PanicContainment_Select same as above but for OnSelect.
func TestControlHandler_PanicContainment_Select(t *testing.T) {
	ctx := context.Background()
	req := ControlRequest{Ref: "LD/LN.DO", Operation: "select"}

	panicFn := func(_ context.Context, _ ControlRequest) error {
		panic("panic in OnSelect")
	}

	err := callControlHandler(panicFn, ctx, req)
	if err == nil {
		t.Fatal("expected error from panicking OnSelect, got nil")
	}
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ControlError, got %T: %v", err, err)
	}
}

// TestSelectTimeout_ConfigurableOverride verifies that a ControlHandler
// with a short SelectTimeout is used instead of DefaultSelectTimeout.
func TestSelectTimeout_ConfigurableOverride(t *testing.T) {
	h := ControlHandler{
		SelectTimeout: 100 * time.Millisecond,
	}
	reg := &controlRegistration{
		handler:       h,
		ctlModel:      CtlModelSBONormal,
		selectTimeout: h.SelectTimeout,
	}
	if reg.selectTimeout == 0 || reg.selectTimeout == DefaultSelectTimeout {
		t.Errorf("selectTimeout = %v, want 100ms", reg.selectTimeout)
	}
}

// TestIsConnActive_NilAndEmpty verifies the nil/empty guard paths of isConnActive.
func TestIsConnActive_NilAndEmpty(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}
	// nil conn — short-circuit before even checking the server.
	if s.isConnActive(nil) {
		t.Error("nil conn: want false")
	}
	// nil s.mms — short-circuit at the mms nil check.
	var sc mms.ServerConn
	if s.isConnActive(&sc) {
		t.Error("nil s.mms, non-nil conn: want false")
	}
	// Non-nil mms.Server with zero connections — loop runs but returns false.
	s.mms = mms.NewServer(mms.ServerOptions{})
	if s.isConnActive(&sc) {
		t.Error("empty server, conn not in list: want false")
	}
}

// TestMmsValuesEqual covers nil and value equality branches.
func TestMmsValuesEqual(t *testing.T) {
	if !mmsValuesEqual(nil, nil) {
		t.Error("nil == nil: want true")
	}
	if mmsValuesEqual(nil, mms.NewBoolean(true)) {
		t.Error("nil != non-nil: want false")
	}
	if mmsValuesEqual(mms.NewBoolean(true), nil) {
		t.Error("non-nil != nil: want false")
	}
	if !mmsValuesEqual(mms.NewBoolean(true), mms.NewBoolean(true)) {
		t.Error("equal booleans: want true")
	}
	if mmsValuesEqual(mms.NewBoolean(true), mms.NewBoolean(false)) {
		t.Error("different booleans: want false")
	}
	if !mmsValuesEqual(mms.NewUnsigned(42), mms.NewUnsigned(42)) {
		t.Error("equal unsigned: want true")
	}
	if mmsValuesEqual(mms.NewUnsigned(42), mms.NewUnsigned(43)) {
		t.Error("different unsigned: want false")
	}
}

// TestExecuteSelect_NonSBOModel verifies that executeSelect rejects a
// direct-normal model with an error.
func TestExecuteSelect_NonSBOModel(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	reg := &controlRegistration{ctlModel: CtlModelDirectNormal, selectTimeout: DefaultSelectTimeout}
	req := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select"}
	if err := s.executeSelect(context.Background(), reg, req); err == nil {
		t.Fatal("expected error for non-SBO ctlModel, got nil")
	}
}

// TestExecuteSelectWithValue_NonEnhancedModel verifies that
// executeSelectWithValue rejects SBO-normal with a helpful message.
func TestExecuteSelectWithValue_NonEnhancedModel(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	reg := &controlRegistration{ctlModel: CtlModelSBONormal, selectTimeout: DefaultSelectTimeout}
	req := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select", CtlVal: BoolCtlVal(true)}
	err := s.executeSelectWithValue(context.Background(), reg, req)
	if err == nil {
		t.Fatal("expected error for non-enhanced ctlModel, got nil")
	}
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ControlError, got %T", err)
	}
}

// TestExecuteOperate_SBOwCtlValMismatch verifies that an SBOw Operate is
// rejected when the ctlVal differs from the one used at select time.
func TestExecuteOperate_SBOwCtlValMismatch(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	origin := Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}}

	// Select with CtlVal=true.
	selReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "select",
		Origin: origin, CtlNum: 3, CtlVal: BoolCtlVal(true),
	}
	if err := s.executeSelect(context.Background(), reg, selReq); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Operate with CtlVal=false — ctlVal mismatch must be caught.
	operReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "operate",
		Origin: origin, CtlNum: 3, CtlVal: BoolCtlVal(false),
	}
	err := s.executeOperate(context.Background(), reg, operReq)
	if err == nil {
		t.Fatal("expected error for ctlVal mismatch, got nil")
	}
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ControlError, got %T", err)
	}
	if ce.AddCause != AddCauseParameterChange {
		t.Errorf("AddCause = %v, want AddCauseParameterChange", ce.AddCause)
	}
}

// TestExecuteOperate_SBONormal_OriginMismatch exercises the write-path
// ownership check: when selectOwner is set (from a Write-to-SBO select)
// and the operate arrives with a different origin, it must be denied.
func TestExecuteOperate_SBONormal_OriginMismatch(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]

	// Simulate a write-path select by origin "1:01".
	reg.mu.Lock()
	reg.selectOwner = fmt.Sprintf("%d:%x", OrCatRemoteControl, []byte{1})
	reg.selectTime = time.Now()
	reg.selectConn = nil // write-path: no connection stored
	reg.mu.Unlock()

	// Operate arrives with origin "1:02" — different owner.
	operReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "operate",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{2}},
		CtlNum: 1, CtlVal: BoolCtlVal(true),
	}
	err := s.executeOperate(context.Background(), reg, operReq)
	if !errors.Is(err, ErrOperateFailed) {
		t.Errorf("expected ErrOperateFailed for origin mismatch, got %v", err)
	}
}

// TestExecuteOperate_SBONormal_Timeout verifies that an expired SBO
// select is treated as if no select is active.
func TestExecuteOperate_SBONormal_Timeout(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]

	// Seed a timed-out selection.
	reg.mu.Lock()
	reg.selectOwner = fmt.Sprintf("%d:%x", OrCatRemoteControl, []byte{1})
	reg.selectTime = time.Now().Add(-2 * DefaultSelectTimeout) // well in the past
	reg.mu.Unlock()

	operReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "operate",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}},
		CtlNum: 1, CtlVal: BoolCtlVal(true),
	}
	err := s.executeOperate(context.Background(), reg, operReq)
	if !errors.Is(err, ErrOperateFailed) {
		t.Errorf("expected ErrOperateFailed for timed-out select, got %v", err)
	}
}

// TestExecuteCancel_SBOw_OriginMismatch verifies that a cancel by a
// different origin is rejected for SBOw controls.
func TestExecuteCancel_SBOw_OriginMismatch(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]

	// Seed a selection by origin "1:01".
	reg.mu.Lock()
	reg.selectOwner = fmt.Sprintf("%d:%x", OrCatRemoteControl, []byte{1})
	reg.mu.Unlock()

	// Cancel arrives from origin "1:02".
	cancelReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "cancel",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{2}},
		CtlNum: 1, CtlVal: BoolCtlVal(true),
	}
	err := s.executeCancel(context.Background(), reg, cancelReq)
	if !errors.Is(err, ErrSelectFailed) {
		t.Errorf("expected ErrSelectFailed for origin mismatch, got %v", err)
	}
}

// TestExecuteCancel_SBOw_OwnerCancels verifies that the selecting origin
// can successfully cancel, clearing the selection.
func TestExecuteCancel_SBOw_OwnerCancels(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	origin := Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{5}}
	ownerKey := fmt.Sprintf("%d:%x", origin.OrCat, origin.OrIdent)

	reg.mu.Lock()
	reg.selectOwner = ownerKey
	reg.mu.Unlock()

	cancelReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "cancel",
		Origin: origin, CtlNum: 1, CtlVal: BoolCtlVal(true),
	}
	if err := s.executeCancel(context.Background(), reg, cancelReq); err != nil {
		t.Fatalf("owner cancel should succeed: %v", err)
	}
	reg.mu.Lock()
	owner := reg.selectOwner
	reg.mu.Unlock()
	if owner != "" {
		t.Errorf("selectOwner should be cleared after cancel, got %q", owner)
	}
}

// TestInstallSBONormalReadHandler_InvalidDoRef verifies that a doRef
// without a dot separator is silently ignored (logged and returned).
func TestInstallSBONormalReadHandler_InvalidDoRef(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
		mms:      mms.NewServer(mms.ServerOptions{}),
	}
	reg := &controlRegistration{ctlModel: CtlModelSBONormal, selectTimeout: DefaultSelectTimeout}
	// doRef without a dot — should not panic, returns early after logging.
	s.installSBONormalReadHandler("LD1", "SPCSO1nodot", reg)
}
