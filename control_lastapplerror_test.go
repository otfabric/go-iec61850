// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/leakcheck"
	"github.com/otfabric/go-iec61850/scl"
)

const (
	ctlLD   = "InteropIEDInteropLD"
	ctlSBOw = ctlLD + "/GGIO1.SPCSO3"
)

// controlPair serves the interop GOOSE model with SPCSO3 (SBO with
// enhanced security) under handler and returns a connected client.
func controlPair(t *testing.T, handler ControlHandler) *Client {
	t.Helper()
	return controlClient(t, controlServer(t, handler))
}

// controlServer serves the interop GOOSE model with SPCSO3 (SBO with
// enhanced security) under handler and SPCSO2 (SBO with normal security).
func controlServer(t *testing.T, handler ControlHandler) *Server {
	t.Helper()
	doc, err := scl.ParseFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	if err := srv.RegisterControl(ctlLD, "GGIO1.SPCSO3", CtlModelSBOEnhanced, handler); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterControl(ctlLD, "GGIO1.SPCSO2", CtlModelSBONormal, ControlHandler{}); err != nil {
		t.Fatal(err)
	}
	return srv
}

// controlClient connects a new client to srv.
func controlClient(t *testing.T, srv *Server) *Client {
	t.Helper()
	sctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT, serverT := loopbackPair(t)
	go func() { _ = srv.Serve(sctx, serverT) }()
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()); _ = mc.Close(context.Background()) })
	return c
}

func waitCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestOperateWait_NegativeTermination(t *testing.T) {
	c := controlPair(t, ControlHandler{
		OnExecute: func(context.Context, ControlRequest) error {
			return &ControlError{AddCause: AddCauseBlockedByInterlocking}
		},
	})
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 7}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatal(err)
	}
	term, err := c.OperateWait(ctx, ref, params)
	if !errors.Is(err, ErrCommandTerminationNegative) || !errors.Is(err, ErrOperateFailed) {
		t.Fatalf("OperateWait = %v, want a negative termination", err)
	}
	var ce *ControlError
	if !errors.As(err, &ce) || ce.AddCause != AddCauseBlockedByInterlocking || ce.LastApplError == nil {
		t.Fatalf("ControlError = %+v", ce)
	}
	if term == nil || term.Positive() || term.Oper == nil {
		t.Fatalf("termination = %+v", term)
	}
	lae := term.LastApplError
	if lae.CntrlObj != ctlLD+"/GGIO1$CO$SPCSO3$Oper" || lae.CtlNum != 7 || lae.Error != 0 || lae.Origin.OrCat != OrCatRemoteControl {
		t.Errorf("LastApplError = %+v", lae)
	}
}

func TestOperateWait_NegativeTerminationUnknownCause(t *testing.T) {
	c := controlPair(t, ControlHandler{
		OnExecute: func(context.Context, ControlRequest) error { panic("boom") },
	})
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 8}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatal(err)
	}
	term, err := c.OperateWait(ctx, ref, params)
	if !errors.Is(err, ErrCommandTerminationNegative) || term.LastApplError.AddCause != AddCauseUnknown {
		t.Fatalf("OperateWait = %+v, %v", term, err)
	}
}

// An operate without a select is refused; the server reports why in a
// LastApplError before the negative write response.
func TestOperate_RefusedCarriesLastApplError(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	_, err := c.OperateWait(ctx, ref, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 9})
	var ce *ControlError
	if !errors.As(err, &ce) || errors.Is(err, ErrCommandTerminationNegative) {
		t.Fatalf("OperateWait without select = %v", err)
	}
	if ce.LastApplError == nil || ce.LastApplError.CtlNum != 9 || ce.AddCause != ce.LastApplError.AddCause || ce.AddCause != AddCauseObjectNotSelected {
		t.Fatalf("ControlError = %+v, LastApplError %+v", ce, ce.LastApplError)
	}
}

func TestSelectWithValueAndCancel_RefusedCarryLastApplError(t *testing.T) {
	c := controlPair(t, ControlHandler{
		OnSelect: func(context.Context, ControlRequest) error {
			return &ControlError{AddCause: AddCauseBlockedByMode}
		},
		OnCancel: func(context.Context, ControlRequest) error {
			return &ControlError{AddCause: AddCauseCommandAlreadyExec}
		},
	})
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	err := c.SelectWithValue(ctx, ref, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 3})
	var ce *ControlError
	if !errors.As(err, &ce) || ce.Operation != "select" || ce.AddCause != AddCauseBlockedByMode ||
		ce.LastApplError == nil || ce.LastApplError.CntrlObj != ctlLD+"/GGIO1$CO$SPCSO3$SBOw" {
		t.Fatalf("SelectWithValue = %v (%+v)", err, ce)
	}

	err = c.Cancel(ctx, ref, CancelParams{CtlVal: BoolCtlVal(true), CtlNum: 4})
	if !errors.As(err, &ce) || ce.Operation != "cancel" || ce.AddCause != AddCauseCommandAlreadyExec ||
		ce.LastApplError == nil || ce.LastApplError.CtlNum != 4 {
		t.Fatalf("Cancel = %v (%+v)", err, ce)
	}
}

func TestOperateWait_ClosedAndInvalid(t *testing.T) {
	c := controlPair(t, ControlHandler{
		OnExecute: func(ctx context.Context, _ ControlRequest) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	ctx := waitCtx(t)
	if _, err := c.OperateWait(ctx, Ref{LD: ctlLD, LN: "GGIO1"}, OperateParams{CtlVal: BoolCtlVal(true)}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("OperateWait without a data object = %v", err)
	}
	if _, err := c.OperateWait(ctx, Ref{LD: "bad/ld", LN: "GGIO1", Path: []string{"SPCSO3"}}, OperateParams{CtlVal: BoolCtlVal(true)}); err == nil {
		t.Error("OperateWait with a bad LD succeeded")
	}

	// The client closes while OperateWait waits for the termination.
	ref := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 11}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.OperateWait(ctx, ref, params)
		done <- err
	}()
	// The operate write has been answered once OperateWait waits for the
	// termination.
	if !leakcheck.WaitBlocked("iec61850.(*Client).OperateWait", 5*time.Second) {
		t.Fatal("OperateWait does not wait for the termination")
	}
	_ = c.Close(ctx)
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Errorf("OperateWait after Close = %v, want ErrClosed", err)
	}
	if _, err := c.OperateWait(ctx, ref, params); !errors.Is(err, ErrClosed) {
		t.Errorf("OperateWait on a closed client = %v", err)
	}
	if _, err := c.addTerminationWaiter("k", "r"); !errors.Is(err, ErrClosed) {
		t.Errorf("addTerminationWaiter after close = %v", err)
	}
}

// The connection drops while OperateWait waits for the termination.
func TestOperateWait_ConnectionLost(t *testing.T) {
	c := controlPair(t, ControlHandler{
		OnExecute: func(ctx context.Context, _ ControlRequest) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 12}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.OperateWait(ctx, ref, params)
		done <- err
	}()
	if !leakcheck.WaitBlocked("iec61850.(*Client).OperateWait", 5*time.Second) {
		t.Fatal("OperateWait does not wait for the termination")
	}
	_ = c.mmsClient.Abort(ctx)
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Errorf("OperateWait after the server closed = %v, want ErrClosed", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func laeValue(cntrlObj string, ctlNum *mms.Value, cause int64) *mms.Value {
	members := []*mms.Value{
		mms.NewVisibleString(cntrlObj),
		mms.NewInteger(1),
		mms.NewStructure([]*mms.Value{mms.NewInteger(2), mms.NewOctetString([]byte("op"))}),
	}
	if ctlNum != nil {
		members = append(members, ctlNum)
	}
	return mms.NewStructure(append(members, mms.NewInteger(cause)))
}

func TestDecodeLastApplError_Layouts(t *testing.T) {
	lae, err := decodeLastApplError(laeValue("LD/LN$CO$DO$Oper", mms.NewUnsigned(42), 18))
	if err != nil || lae.CtlNum != 42 || lae.AddCause != AddCauseObjectNotSelected || lae.Error != 1 ||
		lae.Origin.OrCat != OrCat(2) || string(lae.Origin.OrIdent) != "op" || lae.CntrlObj != "LD/LN$CO$DO$Oper" {
		t.Fatalf("five members = %+v, %v", lae, err)
	}
	if lae, err := decodeLastApplError(laeValue("x", mms.NewInteger(5), 10)); err != nil || lae.CtlNum != 5 || lae.AddCause != AddCauseBlockedByInterlocking {
		t.Errorf("ctlNum as integer = %+v, %v", lae, err)
	}
	if lae, err := decodeLastApplError(laeValue("x", nil, 3)); err != nil || lae.CtlNum != 0 || lae.AddCause != AddCauseSelectFailed {
		t.Errorf("four members = %+v, %v", lae, err)
	}
	for name, v := range map[string]*mms.Value{
		"negative ctlNum":   laeValue("x", mms.NewInteger(-1), 1),
		"ctlNum too large":  laeValue("x", mms.NewUnsigned(256), 1),
		"ctlNum not number": laeValue("x", mms.NewBoolean(true), 1),
		"cause not number":  mms.NewStructure([]*mms.Value{mms.NewVisibleString("x"), mms.NewInteger(0), mms.NewStructure([]*mms.Value{mms.NewInteger(2), mms.NewOctetString(nil)}), mms.NewUnsigned(1), mms.NewBoolean(true)}),
	} {
		if _, err := decodeLastApplError(v); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

func TestControlKey(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"LD", "GGIO1$CO$SPCSO1$Oper"}:   "LD/GGIO1$CO$SPCSO1",
		{"LD", "GGIO1$CO$SPCSO1$SBOw"}:   "LD/GGIO1$CO$SPCSO1",
		{"LD", "GGIO1$CO$SPCSO1$SBO"}:    "LD/GGIO1$CO$SPCSO1",
		{"LD", "GGIO1$CO$SPCSO1$Cancel"}: "LD/GGIO1$CO$SPCSO1",
		{"LD", "GGIO1$CO$Pos"}:           "LD/GGIO1$CO$Pos",
	} {
		if got := controlKey(in[0], in[1]); got != want {
			t.Errorf("controlKey(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
	for in, want := range map[string]string{
		"LD/GGIO1$CO$SPCSO1$Oper\x00\x00": "LD/GGIO1$CO$SPCSO1",
		"no-slash":                        "",
		"/item":                           "",
		"":                                "",
	} {
		if got := controlKeyOf(in); got != want {
			t.Errorf("controlKeyOf(%q) = %q, want %q", in, got, want)
		}
	}
	if cntrlObjOf(nil) != "" {
		t.Error("cntrlObjOf(nil)")
	}
}

func TestHandleControlReport_Routing(t *testing.T) {
	c := &Client{logger: discardLogger()}
	oper := mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "GGIO1$CO$SPCSO1$Oper"}
	laeName := mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: "LastApplError"}
	list := mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: "RPT"}

	// Not control reports.
	for name, r := range map[string]*mms.InformationReportIndication{
		"named list":      {ListName: &list, Values: []*mms.Value{mms.NewBoolean(true)}},
		"no variables":    {Values: []*mms.Value{mms.NewBoolean(true)}},
		"length mismatch": {Variables: []mms.ObjectName{oper, laeName}, Values: []*mms.Value{mms.NewBoolean(true)}},
		"other variable":  {Variables: []mms.ObjectName{{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "GGIO1$ST$Ind1"}}, Values: []*mms.Value{mms.NewBoolean(true)}},
	} {
		if c.handleControlReport(r) {
			t.Errorf("%s: handled as a control report", name)
		}
	}

	// A termination nobody waits for, an undecodable LastApplError alone.
	if !c.handleControlReport(&mms.InformationReportIndication{Variables: []mms.ObjectName{oper}, Values: []*mms.Value{mms.NewBoolean(true)}}) {
		t.Error("termination without a waiter not handled")
	}
	if !c.handleControlReport(&mms.InformationReportIndication{Variables: []mms.ObjectName{laeName}, Values: []*mms.Value{mms.NewBoolean(true)}}) {
		t.Error("undecodable LastApplError not handled")
	}

	// A LastApplError alone goes to the control write in flight.
	w := c.watchLastApplError("LD/GGIO1$CO$SPCSO1", 1)
	other := c.watchLastApplError("LD/GGIO1$CO$SPCSO2", 1)
	// CTL-17: a write of the same control with another ctlNum does not
	// get it (IEC 61850-8-1 20.11 NOTE).
	otherNum := c.watchLastApplError("LD/GGIO1$CO$SPCSO1", 2)
	c.handleControlReport(&mms.InformationReportIndication{
		Variables: []mms.ObjectName{laeName},
		Values:    []*mms.Value{laeValue("LD/GGIO1$CO$SPCSO1$Oper", mms.NewUnsigned(1), 18)},
	})
	if w.result() == nil || w.result().AddCause != AddCauseObjectNotSelected || other.result() != nil || otherNum.result() != nil {
		t.Errorf("LastApplError routed to %+v / %+v / %+v", w.result(), other.result(), otherNum.result())
	}
	// The four-member layout has no ctlNum: matched by the control alone.
	c.handleControlReport(&mms.InformationReportIndication{
		Variables: []mms.ObjectName{laeName},
		Values:    []*mms.Value{laeValue("LD/GGIO1$CO$SPCSO1$Oper", nil, 19)},
	})
	if otherNum.result() == nil || otherNum.result().AddCause != AddCauseObjectAlreadySelected {
		t.Errorf("four-member LastApplError not routed: %+v", otherNum.result())
	}
	c.unwatchLastApplError(w)
	c.unwatchLastApplError(other)
	c.unwatchLastApplError(otherNum)

	// A negative termination with an undecodable LastApplError is still
	// negative; waiters are served in order.
	key := controlKey("LD", "GGIO1$CO$SPCSO1$Oper")
	first, _ := c.addTerminationWaiter(key, "LD/GGIO1.SPCSO1")
	second, _ := c.addTerminationWaiter(key, "LD/GGIO1.SPCSO1")
	c.handleControlReport(&mms.InformationReportIndication{
		Variables: []mms.ObjectName{laeName, oper},
		Values:    []*mms.Value{mms.NewBoolean(true), mms.NewBoolean(true)},
	})
	select {
	case term := <-first.ch:
		if term.Positive() || term.LastApplError.CntrlObj != key {
			t.Errorf("termination = %+v", term)
		}
	default:
		t.Fatal("first waiter not served")
	}
	if len(second.ch) != 0 {
		t.Error("second waiter served too")
	}
	c.removeTerminationWaiter(key, second)
	c.removeTerminationWaiter(key, second) // already gone
	if len(c.ctlWaiters) != 0 {
		t.Errorf("waiters left: %v", c.ctlWaiters)
	}
}

func TestAddCauseOf(t *testing.T) {
	if got := addCauseOf(errors.New("x")); got != AddCauseUnknown {
		t.Errorf("plain error = %v", got)
	}
	if got := addCauseOf(&ControlError{AddCause: AddCauseStepLimit}); got != AddCauseStepLimit {
		t.Errorf("ControlError = %v", got)
	}
}

// A second client is refused a select another client holds, and an
// operate of a control another connection selected.
func TestControl_OtherClientRefusals(t *testing.T) {
	srv := controlServer(t, ControlHandler{})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)

	enhanced := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 31}
	if err := a.SelectWithValue(ctx, enhanced, params); err != nil {
		t.Fatal(err)
	}
	var ce *ControlError
	if err := b.SelectWithValue(ctx, enhanced, params); !errors.As(err, &ce) || ce.AddCause != AddCauseObjectAlreadySelected || ce.LastApplError == nil {
		t.Errorf("second select = %v", err)
	}
	if err := b.Operate(ctx, enhanced, params); !errors.As(err, &ce) || ce.AddCause != AddCauseLockedByOtherClient || ce.LastApplError == nil {
		t.Errorf("operate from the other client = %v", err)
	}

	normal := mustParseRef(t, ctlLD+"/GGIO1.SPCSO2")
	if _, err := a.Select(ctx, normal); err != nil {
		t.Fatal(err)
	}
	if err := b.Operate(ctx, normal, OperateParams{CtlVal: BoolCtlVal(true)}); !errors.As(err, &ce) || ce.AddCause != AddCauseLockedByOtherClient {
		t.Errorf("SBO operate from the other client = %v", err)
	}
}

// Close racing OperateWait between its open check and its registration.
func TestOperateWait_ClosedBeforeRegistration(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	c.ctlMu.Lock()
	c.ctlClosed = true
	c.ctlMu.Unlock()
	if _, err := c.OperateWait(waitCtx(t), mustParseRef(t, ctlSBOw), OperateParams{CtlVal: BoolCtlVal(true)}); !errors.Is(err, ErrClosed) {
		t.Errorf("OperateWait = %v, want ErrClosed", err)
	}
}

func TestLogSendFailure(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	s.logSendFailure("LastApplError", "LD/LN$CO$DO$Oper", nil)
	if buf.Len() != 0 {
		t.Errorf("logged without an error: %s", buf.String())
	}
	s.logSendFailure("LastApplError", "LD/LN$CO$DO$Oper", errors.New("gone"))
	if !strings.Contains(buf.String(), "LastApplError send failed") || !strings.Contains(buf.String(), "gone") {
		t.Errorf("log = %q", buf.String())
	}
}
