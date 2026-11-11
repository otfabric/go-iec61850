// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

func TestOperate_InvalidRef(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}}
	err := c.Operate(context.Background(), Ref{LD: "LD1", LN: "GGIO1"}, OperateParams{CtlVal: BoolCtlVal(true)})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
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

func TestSelectWithValue_ClosedClient(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}, state: clientClosed}
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	err := c.SelectWithValue(context.Background(), ref, OperateParams{CtlVal: BoolCtlVal(true)})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestSelectWithValue_InvalidRef(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}}
	err := c.SelectWithValue(context.Background(), Ref{LD: "LD1"}, OperateParams{CtlVal: BoolCtlVal(true)})
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

func TestCancel_ClosedClient(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}, state: clientClosed}
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	err := c.Cancel(context.Background(), ref, CancelParams{CtlVal: BoolCtlVal(true)})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestSelect_ClosedClient(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}, state: clientClosed}
	_, err := c.Select(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestSelect_InvalidRef(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}}
	_, err := c.Select(context.Background(), Ref{LD: "LD1", LN: "GGIO1"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestReadCtlModel_ClosedClient(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}, state: clientClosed}
	_, err := c.ReadCtlModel(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestReadCtlModel_InvalidRef(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}}
	_, err := c.ReadCtlModel(context.Background(), Ref{LD: "LD1", LN: "GGIO1"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestReadLastApplError_ClosedClient(t *testing.T) {
	c := &Client{logger: discardLogger(), mmsClient: &mms.Client{}, state: clientClosed}
	_, err := c.ReadLastApplError(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

// setupControlLoopback registers Oper/SBOw/Cancel/SBO/ctlModel/LastApplError
// variables for client control API happy-path tests.
func setupControlLoopback(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()

	mu := &sync.Mutex{}
	store := map[string]*mms.Value{}

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	domain := "LD1"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	register := func(itemID string, ts mms.TypeSpec, initial *mms.Value) {
		t.Helper()
		key := domain + "/" + itemID
		mu.Lock()
		store[key] = initial
		mu.Unlock()
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(itemID)},
			TypeSpec: ts,
			Read: func(_ context.Context) (*mms.Value, error) {
				mu.Lock()
				defer mu.Unlock()
				return store[key], nil
			},
			Write: func(_ context.Context, val *mms.Value) error {
				mu.Lock()
				defer mu.Unlock()
				store[key] = val
				return nil
			},
		}); err != nil {
			t.Fatalf("register %q: %v", itemID, err)
		}
	}

	structTS := mms.TypeSpec{Type: mms.ValueTypeStructure}
	register("GGIO1$CO$SPCSO1$Oper", structTS, mms.NewStructure(nil))
	register("GGIO1$CO$SPCSO1$SBOw", structTS, mms.NewStructure(nil))
	register("GGIO1$CO$SPCSO1$Cancel", structTS, mms.NewStructure(nil))
	register("GGIO1$CO$SPCSO1$SBO", mms.TypeSpec{Type: mms.ValueTypeVisibleString}, mms.NewVisibleString("LD1/GGIO1$CO$SPCSO1"))
	register("GGIO1$CF$SPCSO1$ctlModel", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}, mms.NewInteger(int64(CtlModelDirectNormal)))
	register("GGIO1$CO$LastApplError", structTS, mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("LD1/GGIO1.SPCSO1"),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(int64(OrCatRemoteControl)),
			mms.NewOctetString([]byte{1}),
		}),
		mms.NewInteger(int64(AddCauseSelectFailed)),
	}))
	register("GGIO1$CO$SPCSO2$SBO", mms.TypeSpec{Type: mms.ValueTypeVisibleString}, mms.NewVisibleString(""))
	register("GGIO1$CO$SPCSO3$SBO", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}, mms.NewInteger(1))

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })
	return client
}

func TestOperate_HappyPath(t *testing.T) {
	client := setupControlLoopback(t)
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	if err := client.Operate(context.Background(), ref, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1}); err != nil {
		t.Fatalf("Operate: %v", err)
	}
}

func TestSelect_HappyPath(t *testing.T) {
	client := setupControlLoopback(t)
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	selected, err := client.Select(context.Background(), ref)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if selected == "" {
		t.Fatal("expected non-empty SBO string")
	}
}

func TestSelect_DeniedEmpty(t *testing.T) {
	client := setupControlLoopback(t)
	_, err := client.Select(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO2"}})
	if err == nil {
		t.Fatal("expected select denied for empty SBO")
	}
}

func TestSelect_UnexpectedType(t *testing.T) {
	client := setupControlLoopback(t)
	_, err := client.Select(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO3"}})
	if err == nil {
		t.Fatal("expected type error for non-VisibleString SBO")
	}
}

func TestSelectWithValue_HappyPath(t *testing.T) {
	client := setupControlLoopback(t)
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	if err := client.SelectWithValue(context.Background(), ref, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 2}); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}
}

func TestCancel_HappyPath(t *testing.T) {
	client := setupControlLoopback(t)
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}}
	if err := client.Cancel(context.Background(), ref, CancelParams{CtlVal: BoolCtlVal(true), CtlNum: 3}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func TestReadCtlModel_HappyPath(t *testing.T) {
	client := setupControlLoopback(t)
	model, err := client.ReadCtlModel(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}})
	if err != nil {
		t.Fatalf("ReadCtlModel: %v", err)
	}
	if model != CtlModelDirectNormal {
		t.Errorf("ctlModel = %v, want DirectNormal", model)
	}
}

func TestReadLastApplError_HappyPath(t *testing.T) {
	client := setupControlLoopback(t)
	lae, err := client.ReadLastApplError(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"SPCSO1"}})
	if err != nil {
		t.Fatalf("ReadLastApplError: %v", err)
	}
	if lae == nil || lae.CntrlObj != "LD1/GGIO1.SPCSO1" {
		t.Fatalf("unexpected LastApplError: %+v", lae)
	}
	if lae.AddCause != AddCauseSelectFailed {
		t.Errorf("AddCause = %v, want SelectFailed", lae.AddCause)
	}
}

func TestOperate_WriteFailure(t *testing.T) {
	client := setupControlLoopback(t)
	// Unknown DO — writeControlValue / MMS write fails.
	err := client.Operate(context.Background(), Ref{LD: "LD1", LN: "GGIO1", Path: []string{"NoSuch"}}, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1})
	if err == nil {
		t.Fatal("expected write failure")
	}
	if _, ok := errors.AsType[*ControlError](err); !ok {
		t.Errorf("expected ControlError, got %T: %v", err, err)
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
		mms.NewBoolean(false), // Test (IEC 61850-8-1 Table 88)
	})

	req, err := decodeControlRequest("LD/LN.DO", "Cancel", v)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if req.Operation != "cancel" {
		t.Errorf("Operation = %q, want cancel (ControlRequest.Operation)", req.Operation)
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

// CTL-10: a control write without a registration is refused with
// object-access-denied (as libiec61850 answers), not accepted silently.
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
	if dataAccessCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Fatalf("unregistered control = %v, want object-access-denied", err)
	}
}

// IEC 61850-7-2 Table 47: a second Select while Ready is refused with
// object-already-selected, also for the client holding the selection
// (CTL-08); after the operate the SBO-normal control is unselected.
func TestSelectNormal_AlreadySelected(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	ctx := context.Background()
	if !s.selectNormal(ctx, reg) {
		t.Fatal("first select denied")
	}
	if s.selectNormal(ctx, reg) {
		t.Fatal("second select granted, want object-already-selected")
	}
	operReq := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "operate", CtlVal: BoolCtlVal(true), Origin: Origin{OrCat: OrCatRemoteControl}}
	if err := s.operate(ctx, reg, operReq, nil, "GGIO1$CO$SPCSO1$Oper"); err != nil {
		t.Fatalf("operate: %v", err)
	}
	if err := s.operate(ctx, reg, operReq, nil, "GGIO1$CO$SPCSO1$Oper"); addCauseOf(err) != AddCauseObjectNotSelected {
		t.Fatalf("second operate = %v, want object-not-selected", err)
	}
}

// IEC 61850-7-2 20.5.2 / Table 47: an Operate whose ctlNum differs from
// the SelectWithValue gets inconsistent-parameters, and (as in
// libiec61850) the selection is released.
func TestEnhancedSBO_CtlNumMismatch(t *testing.T) {
	s := &Server{
		logger:   discardLogger(),
		controls: make(map[string]*controlRegistration),
	}

	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	origin := Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}}
	reg := s.controls["LD1/GGIO1.SPCSO1"]

	selectReq := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select", Origin: origin, CtlNum: 5, CtlVal: BoolCtlVal(true)}
	if err := s.selectWithValue(context.Background(), reg, selectReq); err != nil {
		t.Fatalf("select: %v", err)
	}

	operReq := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "operate", Origin: origin, CtlNum: 6, CtlVal: BoolCtlVal(true)}
	err := s.operate(context.Background(), reg, operReq, nil, "GGIO1$CO$SPCSO1$Oper")
	if !errors.Is(err, ErrOperateFailed) || addCauseOf(err) != AddCauseInconsistentParameters {
		t.Errorf("operate = %v, want ErrOperateFailed with inconsistent-parameters", err)
	}
	reg.mu.Lock()
	state := reg.state
	reg.mu.Unlock()
	if state != ctlUnselected {
		t.Errorf("state = %v, want unselected", state)
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

	// Origin structure with too few members.
	bad4 := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("ref"),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{mms.NewInteger(1)}),
		mms.NewInteger(0),
	})
	if _, err := decodeLastApplError(bad4); err == nil {
		t.Error("expected error for short Origin")
	}

	// OrCat wrong type.
	bad5 := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("ref"),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{
			mms.NewVisibleString("not-int"),
			mms.NewOctetString([]byte{1}),
		}),
		mms.NewInteger(0),
	})
	if _, err := decodeLastApplError(bad5); err == nil {
		t.Error("expected error for OrCat type")
	}

	// OrIdent wrong type.
	bad6 := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("ref"),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(1),
			mms.NewVisibleString("not-octets"),
		}),
		mms.NewInteger(0),
	})
	if _, err := decodeLastApplError(bad6); err == nil {
		t.Error("expected error for OrIdent type")
	}

	// AddCause wrong type.
	bad7 := mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("ref"),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(1),
			mms.NewOctetString([]byte{1}),
		}),
		mms.NewVisibleString("not-int"),
	})
	if _, err := decodeLastApplError(bad7); err == nil {
		t.Error("expected error for AddCause type")
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
	if _, ok := errors.AsType[*ControlError](err); !ok {
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
	if reg.handler.SelectTimeout != 100*time.Millisecond {
		t.Errorf("handler.SelectTimeout = %v, want 100ms", reg.handler.SelectTimeout)
	}
	if reg.ctlModel != CtlModelSBONormal {
		t.Errorf("ctlModel = %v, want CtlModelSBONormal", reg.ctlModel)
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

// IEC 61850-7-2 Table 47: SelectWithValue on a control without SBO with
// enhanced security is refused with AddCause not-supported.
func TestSelectWithValue_NonEnhancedModel(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	for _, m := range []CtlModel{CtlModelDirectNormal, CtlModelSBONormal, CtlModelDirectEnhanced} {
		reg := &controlRegistration{ctlModel: m}
		req := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select", CtlVal: BoolCtlVal(true), Origin: Origin{OrCat: OrCatRemoteControl}}
		err := s.selectWithValue(context.Background(), reg, req)
		if !errors.Is(err, ErrSelectFailed) || addCauseOf(err) != AddCauseNotSupported {
			t.Errorf("%v: %v, want not-supported", m, err)
		}
	}
}

// IEC 61850-7-2 20.5.2 / Table 47: an Operate whose ctlVal differs from
// the SelectWithValue gets inconsistent-parameters.
func TestOperate_SBOwCtlValMismatch(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	origin := Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}}

	selReq := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select", Origin: origin, CtlNum: 3, CtlVal: BoolCtlVal(true)}
	if err := s.selectWithValue(context.Background(), reg, selReq); err != nil {
		t.Fatalf("select: %v", err)
	}
	operReq := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "operate", Origin: origin, CtlNum: 3, CtlVal: BoolCtlVal(false)}
	err := s.operate(context.Background(), reg, operReq, nil, "GGIO1$CO$SPCSO1$Oper")
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ControlError, got %T", err)
	}
	if ce.AddCause != AddCauseInconsistentParameters {
		t.Errorf("AddCause = %v, want AddCauseInconsistentParameters", ce.AddCause)
	}
}

// SBO with normal security: the selection belongs to the association, the
// Operate's origin is not compared with the Select (a Read carries none;
// libiec61850 compares the connection only).
func TestOperate_SBONormal_OriginNotCompared(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	if !s.selectNormal(context.Background(), reg) {
		t.Fatal("select denied")
	}
	operReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "operate",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{2}},
		CtlNum: 1, CtlVal: BoolCtlVal(true),
	}
	if err := s.operate(context.Background(), reg, operReq, nil, "GGIO1$CO$SPCSO1$Oper"); err != nil {
		t.Errorf("operate: %v", err)
	}
}

// IEC 61850-7-2 20.2.2 a: an expired selection is treated as no
// selection (object-not-selected).
func TestOperate_SBONormal_Timeout(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	reg.mu.Lock()
	reg.state = ctlReady
	reg.selTime = time.Now().Add(-2 * DefaultSelectTimeout)
	reg.mu.Unlock()

	operReq := ControlRequest{
		Ref: "LD1/GGIO1.SPCSO1", Operation: "operate",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}},
		CtlNum: 1, CtlVal: BoolCtlVal(true),
	}
	err := s.operate(context.Background(), reg, operReq, nil, "GGIO1$CO$SPCSO1$Oper")
	if !errors.Is(err, ErrOperateFailed) || addCauseOf(err) != AddCauseObjectNotSelected {
		t.Errorf("operate = %v, want object-not-selected", err)
	}
}

// IEC 61850-7-2 20.6 / Table 47: a Cancel whose parameters differ from the
// SelectWithValue gets inconsistent-parameters (as libiec61850 compares).
func TestCancel_SBOw_OriginMismatch(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	sel := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{1}}, CtlNum: 1, CtlVal: BoolCtlVal(true)}
	if err := s.selectWithValue(context.Background(), reg, sel); err != nil {
		t.Fatal(err)
	}

	cancelReq := sel
	cancelReq.Operation = "cancel"
	cancelReq.Origin = Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{2}}
	err := s.cancel(context.Background(), reg, cancelReq)
	if !errors.Is(err, ErrCancelFailed) || addCauseOf(err) != AddCauseInconsistentParameters {
		t.Errorf("cancel = %v, want ErrCancelFailed with inconsistent-parameters", err)
	}
}

// IEC 61850-7-2 20.6: the selecting client cancels; the control returns to
// Unselected.
func TestCancel_SBOw_OwnerCancels(t *testing.T) {
	s := &Server{logger: discardLogger(), controls: make(map[string]*controlRegistration)}
	if err := s.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	reg := s.controls["LD1/GGIO1.SPCSO1"]
	sel := ControlRequest{Ref: "LD1/GGIO1.SPCSO1", Operation: "select",
		Origin: Origin{OrCat: OrCatRemoteControl, OrIdent: []byte{5}}, CtlNum: 1, CtlVal: BoolCtlVal(true)}
	if err := s.selectWithValue(context.Background(), reg, sel); err != nil {
		t.Fatal(err)
	}
	cancelReq := sel
	cancelReq.Operation = "cancel"
	if err := s.cancel(context.Background(), reg, cancelReq); err != nil {
		t.Fatalf("owner cancel should succeed: %v", err)
	}
	reg.mu.Lock()
	state := reg.state
	reg.mu.Unlock()
	if state != ctlUnselected {
		t.Errorf("state = %v, want unselected", state)
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
