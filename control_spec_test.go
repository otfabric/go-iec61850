// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// Spec tests of the server control model (IEC 61850-7-2 clause 20, Table
// 47; IEC 61850-8-1 clause 20) against the in-process server.

const (
	ctlSBO    = ctlLD + "/GGIO1.SPCSO2" // SBO with normal security
	ctlDirect = ctlLD + "/GGIO1.SPCSO1" // not registered by controlServer
	ctlTm     = ctlLD + "/GGIO1.SPCTm"  // SBOw, Oper with operTm, CF sbo attributes
	ctlDPC    = ctlLD + "/GGIO1.DPCSO1" // DPC, direct with normal security
)

// specCF are the CF values of SPCTm in the spec fixture.
type specCF struct {
	sboTimeoutMs  int
	operateMany   bool
	operTimeoutMs int
}

// specControlModel is the interop GOOSE model with SPCTm (SBOw, an Oper
// with operTm, sboTimeout/sboClass/operTimeout, stSeld, origin, ctlNum)
// and DPCSO1 (a DPC: ctlVal BOOLEAN, stVal Dbpos).
func specControlSCL(t *testing.T, cf specCF) *scl.SCL {
	t.Helper()
	raw, err := os.ReadFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	sboClass := "operate-once"
	if cf.operateMany {
		sboClass = "operate-many"
	}
	replace := func(old, new string) {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("fixture: %q not found", old)
		}
		s = strings.Replace(s, old, new, 1)
	}
	replace(`<DO name="SPCSO3" type="SPC_Type"/>`, `<DO name="SPCSO3" type="SPC_Type"/>
      <DO name="SPCTm" type="SPCTm_Type"/>
      <DO name="DPCSO1" type="DPC_Type"/>`)
	replace(`<DOType id="MV_Type"`, `<DOType id="SPCTm_Type" cdc="SPC">
      <DA name="origin"      fc="ST" bType="Struct" type="Originator_Type"/>
      <DA name="ctlNum"      fc="ST" bType="INT8U"/>
      <DA name="stVal"       fc="ST" bType="BOOLEAN" dchg="true"/>
      <DA name="q"           fc="ST" bType="Quality" qchg="true"/>
      <DA name="t"           fc="ST" bType="Timestamp"/>
      <DA name="stSeld"      fc="ST" bType="BOOLEAN" dchg="true"/>
      <DA name="ctlModel"    fc="CF" bType="Enum" type="CtlModels"/>
      <DA name="sboTimeout"  fc="CF" bType="INT32U"/>
      <DA name="sboClass"    fc="CF" bType="Enum" type="SboClasses"/>
      <DA name="operTimeout" fc="CF" bType="INT32U"/>
      <DA name="SBOw"        fc="CO" bType="Struct" type="OperSPCTm_Type"/>
      <DA name="Oper"        fc="CO" bType="Struct" type="OperSPCTm_Type"/>
      <DA name="Cancel"      fc="CO" bType="Struct" type="CancelSPCTm_Type"/>
    </DOType>
    <DOType id="DPC_Type" cdc="DPC">
      <DA name="origin"   fc="ST" bType="Struct" type="Originator_Type"/>
      <DA name="ctlNum"   fc="ST" bType="INT8U"/>
      <DA name="stVal"    fc="ST" bType="Dbpos" dchg="true"/>
      <DA name="q"        fc="ST" bType="Quality" qchg="true"/>
      <DA name="t"        fc="ST" bType="Timestamp"/>
      <DA name="ctlModel" fc="CF" bType="Enum" type="CtlModels"/>
      <DA name="Oper"     fc="CO" bType="Struct" type="OperSPC_Type"/>
    </DOType>
    <DOType id="MV_Type"`)
	replace(`<DAType id="Originator_Type"`, `<DAType id="OperSPCTm_Type">
      <BDA name="ctlVal" bType="BOOLEAN"/>
      <BDA name="operTm" bType="Timestamp"/>
      <BDA name="origin" bType="Struct" type="Originator_Type"/>
      <BDA name="ctlNum" bType="INT8U"/>
      <BDA name="T"      bType="Timestamp"/>
      <BDA name="Test"   bType="BOOLEAN"/>
      <BDA name="Check"  bType="Check"/>
    </DAType>
    <DAType id="CancelSPCTm_Type">
      <BDA name="ctlVal" bType="BOOLEAN"/>
      <BDA name="operTm" bType="Timestamp"/>
      <BDA name="origin" bType="Struct" type="Originator_Type"/>
      <BDA name="ctlNum" bType="INT8U"/>
      <BDA name="T"      bType="Timestamp"/>
      <BDA name="Test"   bType="BOOLEAN"/>
    </DAType>
    <DAType id="Originator_Type"`)
	replace(`<EnumType id="OrCat"`, `<EnumType id="SboClasses">
      <EnumVal ord="0">operate-once</EnumVal>
      <EnumVal ord="1">operate-many</EnumVal>
    </EnumType>
    <EnumType id="OrCat"`)
	replace(`<DOI name="SPCSO3">`, fmt.Sprintf(`<DOI name="SPCTm">
              <DAI name="ctlModel"><Val>sbo-with-enhanced-security</Val></DAI>
              <DAI name="sboTimeout"><Val>%d</Val></DAI>
              <DAI name="sboClass"><Val>%s</Val></DAI>
              <DAI name="operTimeout"><Val>%d</Val></DAI>
            </DOI>
            <DOI name="DPCSO1">
              <DAI name="ctlModel"><Val>direct-with-normal-security</Val></DAI>
            </DOI>
            <DOI name="SPCSO3">`, cf.sboTimeoutMs, sboClass, cf.operTimeoutMs))
	doc, err := scl.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// specControlServer serves the spec fixture; register registers the
// controls the test needs.
func specControlServer(t *testing.T, cf specCF, register func(*Server)) *Server {
	t.Helper()
	model, err := NewServerModelFromSCL(specControlSCL(t, cf), "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	register(srv)
	return srv
}

func mustRegister(t *testing.T, srv *Server, doRef string, m CtlModel, h ControlHandler) {
	t.Helper()
	if err := srv.RegisterControl(ctlLD, doRef, m, h); err != nil {
		t.Fatal(err)
	}
}

// rawControlWrite writes val to the item of ref's sub-attribute sub as
// is, bypassing the client's control encoding.
func rawControlWrite(ctx context.Context, c *Client, ref Ref, sub string, val *mms.Value) error {
	domainID, itemID, err := c.refToMMS(controlSubRef(ref, sub))
	if err != nil {
		return err
	}
	_, err = c.mmsClient.Write(ctx, mms.WriteRequest{DomainID: domainID, ItemID: itemID, Value: val})
	return err
}

// refusal checks a refused control call: the ControlError's AddCause
// and the DataAccessError of the write.
func refusal(t *testing.T, what string, err error, cause AddCause, code mms.DataAccessErrorCode) {
	t.Helper()
	var ce *ControlError
	if !errors.As(err, &ce) {
		t.Errorf("%s = %v, want a ControlError", what, err)
		return
	}
	if ce.AddCause != cause || dataAccessCode(err) != code {
		t.Errorf("%s = AddCause %v, %v (%v); want %v, %v", what, ce.AddCause, dataAccessCode(err), err, cause, code)
	}
}

func boolParams(n uint8) OperateParams { return OperateParams{CtlVal: BoolCtlVal(true), CtlNum: n} }

func cancelParams(n uint8) CancelParams { return CancelParams{CtlVal: BoolCtlVal(true), CtlNum: n} }

// IEC 61850-7-2 Table 47 with two clients (IEC 61850-8-1 Table 87:
// temporarily-unavailable while selected or being operated by someone).
func TestSpec_7_2_Table47_SBOwTwoClients(t *testing.T) {
	srv := controlServer(t, ControlHandler{})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)

	if err := a.SelectWithValue(ctx, ref, boolParams(1)); err != nil {
		t.Fatal(err)
	}
	// CTL-08: the owner's re-select is object-already-selected too.
	refusal(t, "A SBOw again", a.SelectWithValue(ctx, ref, boolParams(1)), AddCauseObjectAlreadySelected, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "B SBOw", b.SelectWithValue(ctx, ref, boolParams(2)), AddCauseObjectAlreadySelected, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "B Oper", b.Operate(ctx, ref, boolParams(1)), AddCauseLockedByOtherClient, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "B Cancel", b.Cancel(ctx, ref, cancelParams(1)), AddCauseLockedByOtherClient, mms.DataAccessErrorTemporarilyUnavailable)

	if term, err := a.OperateWait(ctx, ref, boolParams(1)); err != nil || !term.Positive() {
		t.Fatalf("A OperateWait = %+v, %v", term, err)
	}
	// Operate-once: unselected after the termination.
	refusal(t, "A Oper again", a.Operate(ctx, ref, boolParams(1)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
	if err := b.SelectWithValue(ctx, ref, boolParams(2)); err != nil {
		t.Errorf("B SBOw after the operation: %v", err)
	}
}

// CTL-01, IEC 61850-7-2 20.5.2.4: an Operate without a ctlNum continues
// the SelectWithValue's sequence; ctlNum 0 can be sent explicitly.
func TestSpec_7_2_20_5_2_4_CtlNumSequence(t *testing.T) {
	var got atomic.Int32
	got.Store(-1)
	srv := controlServer(t, ControlHandler{OnOperate: func(_ context.Context, req ControlRequest) error {
		got.Store(int32(req.CtlNum))
		return nil
	}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)

	params := OperateParams{CtlVal: BoolCtlVal(true)}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OperateWait(ctx, ref, params); err != nil {
		t.Fatalf("Operate with the select's ctlNum: %v", err)
	}
	sel, _ := c.selectCtlNum(ref)
	if got.Load() != int32(sel) {
		t.Errorf("OnOperate ctlNum %d, select %d", got.Load(), sel)
	}

	zero := OperateParams{CtlVal: BoolCtlVal(true), CtlNumSet: true}
	if err := c.SelectWithValue(ctx, ref, zero); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OperateWait(ctx, ref, zero); err != nil {
		t.Fatalf("Operate ctlNum 0: %v", err)
	}
	if got.Load() != 0 {
		t.Errorf("OnOperate ctlNum %d, want 0", got.Load())
	}
}

// CTL-02, CTL-03: IEC 61850-8-1 20.10.2.2 (operTm all zero: no time
// activation), Table 82 note a (operTm 0 in the CommandTermination of an
// immediate command), IEC 61850-7-2 20.4 (time activated operate: the
// action runs at operTm), IEC 61850-8-1 20.10.3 (the termination carries
// the original operTm).
func TestSpec_8_1_20_10_OperTm(t *testing.T) {
	type call struct {
		at     time.Time
		operTm time.Time
	}
	calls := make(chan call, 4)
	srv := specControlServer(t, specCF{}, func(s *Server) {
		mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{OnOperate: func(_ context.Context, req ControlRequest) error {
			calls <- call{time.Now(), req.OperTm}
			return nil
		}})
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlTm)

	// Immediate: the client sends operTm zero because the type has it.
	if err := c.SelectWithValue(ctx, ref, boolParams(1)); err != nil {
		t.Fatalf("SBOw immediate: %v", err)
	}
	term, err := c.OperateWait(ctx, ref, boolParams(1))
	if err != nil {
		t.Fatalf("OperateWait immediate: %v", err)
	}
	if got := <-calls; !got.operTm.IsZero() {
		t.Errorf("immediate OperTm = %v, want zero", got.operTm)
	}
	m, _ := term.Oper.Structure()
	if len(m) != 7 {
		t.Fatalf("termination Oper has %d members", len(m))
	}
	if tm, _ := m[1].UTCTime(); tm.Unix() != 0 {
		t.Errorf("termination operTm = %v, want 0", tm)
	}

	// Time activated.
	operTm := time.Now().Add(300 * time.Millisecond)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 2, OperTm: operTm}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatalf("SBOw time activated: %v", err)
	}
	start := time.Now()
	term, err = c.OperateWait(ctx, ref, params)
	if err != nil || !term.Positive() {
		t.Fatalf("OperateWait time activated = %+v, %v", term, err)
	}
	got := <-calls
	if got.at.Before(operTm.Add(-5*time.Millisecond)) || time.Since(start) < 250*time.Millisecond {
		t.Errorf("OnOperate at %v, before operTm %v", got.at, operTm)
	}
	if d := got.operTm.Sub(operTm); d < -time.Millisecond || d > time.Millisecond {
		t.Errorf("req.OperTm = %v, want %v", got.operTm, operTm)
	}
	m, _ = term.Oper.Structure()
	if tm, _ := m[1].UTCTime(); tm.Sub(operTm).Abs() > time.Millisecond {
		t.Errorf("TimOperTermination operTm = %v, want %v", tm, operTm)
	}
}

// IEC 61850-7-2 Table 47 (WaitForActivationTime): the owner's Cancel
// stops a time activated operate; another client gets
// locked-by-other-client.
func TestSpec_7_2_Table47_CancelTimeActivated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var operates atomic.Int32
		srv := specControlServer(t, specCF{}, func(s *Server) {
			mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{OnOperate: func(context.Context, ControlRequest) error {
				operates.Add(1)
				return nil
			}})
		})
		a, b := controlClient(t, srv), controlClient(t, srv)
		ctx := waitCtx(t)
		ref := mustParseRef(t, ctlTm)
		operTm := time.Now().Add(400 * time.Millisecond)
		params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 3, OperTm: operTm}
		if err := a.SelectWithValue(ctx, ref, params); err != nil {
			t.Fatal(err)
		}
		if err := a.Operate(ctx, ref, params); err != nil {
			t.Fatal(err)
		}
		cp := CancelParams{CtlVal: BoolCtlVal(true), CtlNum: 3, OperTm: operTm}
		refusal(t, "B Cancel", b.Cancel(ctx, ref, cp), AddCauseLockedByOtherClient, mms.DataAccessErrorTemporarilyUnavailable)
		refusal(t, "B Oper", b.Operate(ctx, ref, params), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
		if err := a.Cancel(ctx, ref, cp); err != nil {
			t.Fatalf("A Cancel: %v", err)
		}
		time.Sleep(time.Until(operTm) + 200*time.Millisecond) // fake time: beyond operTm
		synctest.Wait()
		if n := operates.Load(); n != 0 {
			t.Errorf("OnOperate called %d times after Cancel", n)
		}
		if err := b.SelectWithValue(ctx, ref, boolParams(4)); err != nil {
			t.Errorf("B SBOw after cancel: %v", err)
		}
	})
}

// CTL-06, CTL-07: IEC 61850-7-2 Table 47 (WaitForChange): Oper, SBOw and
// Cancel during execution get command-already-in-execution; the control
// is free again after the CommandTermination.
func TestSpec_7_2_Table47_CancelDuringExecution(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	srv := controlServer(t, ControlHandler{OnExecute: func(ctx context.Context, _ ControlRequest) error {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	if err := a.SelectWithValue(ctx, ref, boolParams(5)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.OperateWait(ctx, ref, boolParams(5))
		done <- err
	}()
	<-started
	refusal(t, "A Cancel", a.Cancel(ctx, ref, cancelParams(5)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "A Oper", a.Operate(ctx, ref, boolParams(5)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "B SBOw", b.SelectWithValue(ctx, ref, boolParams(6)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("OperateWait: %v", err)
	}
	if err := b.SelectWithValue(ctx, ref, boolParams(6)); err != nil {
		t.Errorf("B SBOw after termination: %v", err)
	}
}

// CTL-06: direct with enhanced security, a second Operate during
// execution gets command-already-in-execution.
func TestSpec_7_2_Table47_DirectEnhancedOverlap(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	srv := controlServer(t, ControlHandler{})
	mustRegister(t, srv, "GGIO1.SPCSO1", CtlModelDirectEnhanced, ControlHandler{OnExecute: func(ctx context.Context, _ ControlRequest) error {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlDirect)
	done := make(chan error, 1)
	go func() {
		_, err := c.OperateWait(ctx, ref, boolParams(1))
		done <- err
	}()
	<-started
	refusal(t, "second Oper", c.Operate(ctx, ref, boolParams(2)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.OperateWait(ctx, ref, boolParams(3)); err != nil {
		t.Errorf("Operate after termination: %v", err)
	}
	// RegisterControl's model wins over the CF ctlModel (CTL-09).
	if m, err := c.ReadCtlModel(ctx, ref); err != nil || m != CtlModelDirectEnhanced {
		t.Errorf("ctlModel = %v, %v; want direct-with-enhanced-security", m, err)
	}
}

// CTL-10, CTL-11, CTL-13, CTL-14, SRV-02, SRV-03: refusal codes (IEC
// 61850-8-1 Table 87, 8.1.3.4.4.2) and parameter checks (IEC 61850-7-2
// 20.5.2, Table 47 inconsistent-parameters).
func TestSpec_8_1_Table87_RefusalCodes(t *testing.T) {
	var operates atomic.Int32
	srv := controlServer(t, ControlHandler{OnOperate: func(context.Context, ControlRequest) error {
		operates.Add(1)
		return nil
	}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)

	refusal(t, "Oper unselected", c.Operate(ctx, ref, boolParams(1)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)

	// Malformed structures: type-inconsistent, nothing executed.
	good := buildOper(boolParams(1))
	m, _ := good.Structure()
	short := mms.NewStructure(m[:5])
	badNum := mms.NewStructure(append(append([]*mms.Value{}, m[:2]...), append([]*mms.Value{mms.NewVisibleString("1")}, m[3:]...)...))
	for name, v := range map[string]*mms.Value{"5 members": short, "ctlNum string": badNum, "not a structure": mms.NewBoolean(true)} {
		if err := rawControlWrite(ctx, c, ref, "SBOw", v); dataAccessCode(err) != mms.DataAccessErrorTypeInconsistent {
			t.Errorf("SBOw %s = %v, want type-inconsistent", name, err)
		}
	}

	// SBO is read (Select), not written (CTL-11); Oper members are not
	// written alone (SRV-03).
	if err := rawControlWrite(ctx, c, ref, "SBO", mms.NewVisibleString("x")); dataAccessCode(err) == mms.DataAccessErrorNone {
		t.Error("write of SBO accepted")
	}
	ctlValRef := controlSubRef(ref, "Oper")
	ctlValRef.Path = append(ctlValRef.Path, "ctlVal")
	if err := c.Write(ctx, ctlValRef, mms.NewBoolean(true)); dataAccessCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("write of Oper$ctlVal = %v, want object-access-denied", err)
	}

	// Inconsistent parameters release the selection (libiec61850).
	test := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 7, Test: true}
	if err := c.SelectWithValue(ctx, ref, test); err != nil {
		t.Fatal(err)
	}
	refusal(t, "Oper Test differs", c.Operate(ctx, ref, boolParams(7)), AddCauseInconsistentParameters, mms.DataAccessErrorObjectAccessDenied)
	refusal(t, "Oper after inconsistent", c.Operate(ctx, ref, test), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)

	// CTL-13: a Cancel keeps the Test status of its sequence.
	if err := c.SelectWithValue(ctx, ref, test); err != nil {
		t.Fatal(err)
	}
	refusal(t, "Cancel Test differs", c.Cancel(ctx, ref, cancelParams(7)), AddCauseInconsistentParameters, mms.DataAccessErrorObjectAccessDenied)
	if err := c.Cancel(ctx, ref, CancelParams{CtlVal: BoolCtlVal(true), CtlNum: 7, Test: true}); err != nil {
		t.Errorf("Cancel with the sequence's Test: %v", err)
	}

	// CTL-10: an unregistered control.
	if err := c.Operate(ctx, mustParseRef(t, ctlDirect), boolParams(1)); dataAccessCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("Oper of an unregistered control = %v, want object-access-denied", err)
	}
	// Invalid origin: orCat outside 0..8 (IEC 61850-7-3 orCat).
	if err := c.SelectWithValue(ctx, ref, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1, Origin: &Origin{OrCat: 9}}); dataAccessCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("SBOw invalid origin = %v", err)
	}
	// CTL-09: the CF ctlModel of a registered control is not writable.
	cf := Ref{LD: ref.LD, LN: ref.LN, Path: []string{"SPCSO3", "ctlModel"}, FC: FCCF}
	if err := c.Write(ctx, cf, mms.NewInteger(1)); dataAccessCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("write of CF ctlModel = %v, want object-access-denied", err)
	}
	if n := operates.Load(); n != 0 {
		t.Errorf("OnOperate called %d times", n)
	}
}

// CTL-04, CTL-05: SBO with normal security, Select is a Read of SBO (IEC
// 61850-8-1 20.5.4); OnSelect decides, a denied select reads "" (20.5.5);
// the granted one reads LD/LN$CO$DO, as libiec61850.
func TestSpec_8_1_20_5_SelectNormal(t *testing.T) {
	var deny atomic.Bool
	srv := controlServer(t, ControlHandler{})
	if err := srv.RegisterControl(ctlLD, "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{OnSelect: func(context.Context, ControlRequest) error {
		if deny.Load() {
			return &ControlError{AddCause: AddCauseBlockedByMode, Wrapped: ErrSelectFailed}
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBO)

	got, err := a.Select(ctx, ref)
	if err != nil || got != ctlLD+"/GGIO1$CO$SPCSO2" {
		t.Fatalf("Select = %q, %v", got, err)
	}
	if _, err := b.Select(ctx, ref); err == nil {
		t.Error("B Select while A holds it succeeded")
	}
	refusal(t, "B Oper", b.Operate(ctx, ref, boolParams(1)), AddCauseLockedByOtherClient, mms.DataAccessErrorTemporarilyUnavailable)
	if err := a.Operate(ctx, ref, boolParams(1)); err != nil {
		t.Fatalf("A Oper: %v", err)
	}

	denied := mustParseRef(t, ctlDirect)
	deny.Store(true)
	if _, err := a.Select(ctx, denied); err == nil {
		t.Error("Select refused by OnSelect succeeded")
	}
	refusal(t, "Oper after denied select", a.Operate(ctx, denied, boolParams(1)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
	deny.Store(false)
	if _, err := a.Select(ctx, denied); err != nil {
		t.Errorf("Select accepted by OnSelect: %v", err)
	}
}

// CTL-09: sboTimeout (IEC 61850-7-2 20.2.2 a), sboClass operate-many
// (20.3.3 e), operTimeout (20.3.3: CmdTerm- time-limit-over); stSeld
// (IEC 61850-7-3) follows the selection.
func TestSpec_7_2_20_CFAttributes(t *testing.T) {
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlTm)
	stSeld := Ref{LD: ref.LD, LN: ref.LN, Path: []string{"SPCTm", "stSeld"}, FC: FCST}
	readSeld := func(c *Client) bool {
		t.Helper()
		v, err := c.ReadRaw(ctx, stSeld)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := v.Bool()
		return b
	}

	t.Run("sboTimeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := waitCtx(t)
			readSeld := func(c *Client) bool {
				t.Helper()
				v, err := c.ReadRaw(ctx, stSeld)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := v.Bool()
				return b
			}
			srv := specControlServer(t, specCF{sboTimeoutMs: 150}, func(s *Server) {
				mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{})
			})
			c := controlClient(t, srv)
			if err := c.SelectWithValue(ctx, ref, boolParams(1)); err != nil {
				t.Fatal(err)
			}
			if !readSeld(c) {
				t.Error("stSeld false while selected")
			}
			time.Sleep(300 * time.Millisecond) // fake time: beyond sboTimeout of 150 ms
			synctest.Wait()                    // the selection timer has run
			if readSeld(c) {
				t.Error("stSeld true after sboTimeout")
			}
			refusal(t, "Oper after sboTimeout", c.Operate(ctx, ref, boolParams(1)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
		})
	})

	t.Run("operate-many", func(t *testing.T) {
		srv := specControlServer(t, specCF{operateMany: true}, func(s *Server) {
			mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{})
		})
		c := controlClient(t, srv)
		if err := c.SelectWithValue(ctx, ref, boolParams(2)); err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			if _, err := c.OperateWait(ctx, ref, boolParams(2)); err != nil {
				t.Fatalf("operate %d: %v", i, err)
			}
		}
		if !readSeld(c) {
			t.Error("stSeld false after operate-many operation")
		}
		if err := c.Cancel(ctx, ref, cancelParams(2)); err != nil {
			t.Fatal(err)
		}
		if readSeld(c) {
			t.Error("stSeld true after cancel")
		}
	})

	t.Run("operTimeout", func(t *testing.T) {
		srv := specControlServer(t, specCF{operTimeoutMs: 100}, func(s *Server) {
			mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{OnExecute: func(ctx context.Context, _ ControlRequest) error {
				<-ctx.Done()
				return ctx.Err()
			}})
		})
		c := controlClient(t, srv)
		if err := c.SelectWithValue(ctx, ref, boolParams(3)); err != nil {
			t.Fatal(err)
		}
		term, err := c.OperateWait(ctx, ref, boolParams(3))
		if !errors.Is(err, ErrCommandTerminationNegative) || term.LastApplError.AddCause != AddCauseTimeLimit {
			t.Fatalf("OperateWait = %+v, %v; want CmdTerm- time-limit-over", term, err)
		}
	})
}

// IEC 61850-7-2 Table 47: association loss releases the selection.
func TestSpec_7_2_Table47_AssociationLossWhileSelected(t *testing.T) {
	srv := specControlServer(t, specCF{}, func(s *Server) {
		mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{})
	})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlTm)
	if err := a.SelectWithValue(ctx, ref, boolParams(1)); err != nil {
		t.Fatal(err)
	}
	refusal(t, "B SBOw", b.SelectWithValue(ctx, ref, boolParams(2)), AddCauseObjectAlreadySelected, mms.DataAccessErrorTemporarilyUnavailable)
	if err := a.mmsClient.Close(ctx); err != nil {
		t.Fatal(err)
	}
	stSeld := Ref{LD: ref.LD, LN: ref.LN, Path: []string{"SPCTm", "stSeld"}, FC: FCST}
	waitFor(t, func() bool {
		v, err := b.ReadRaw(ctx, stSeld)
		sel, _ := v.Bool()
		return err == nil && !sel
	})
	if err := b.SelectWithValue(ctx, ref, boolParams(2)); err != nil {
		t.Errorf("B SBOw after A's association ended: %v", err)
	}
}

// CTL-12: LastApplError Error (IEC 61850-8-1 20.11): 3 Operator Test Not
// OK for a failed Operate with Test, 1 Unknown for an internal error.
func TestSpec_8_1_20_11_LastApplErrorCode(t *testing.T) {
	srv := controlServer(t, ControlHandler{})
	mustRegister(t, srv, "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{OnOperate: func(_ context.Context, req ControlRequest) error {
		if req.CtlNum == 9 {
			panic("boom")
		}
		return &ControlError{AddCause: AddCauseBlockedByProcess, Wrapped: ErrOperateFailed}
	}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlDirect)
	for _, tc := range []struct {
		params OperateParams
		code   int
	}{
		{OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1, Test: true}, 3},
		{OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 2}, 0},
		{OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 9}, 1},
	} {
		err := c.Operate(ctx, ref, tc.params)
		var ce *ControlError
		if !errors.As(err, &ce) || ce.LastApplError == nil || ce.LastApplError.Error != tc.code || ce.LastApplError.CtlNum != tc.params.CtlNum {
			t.Errorf("ctlNum %d: %v (%+v), want Error %d", tc.params.CtlNum, err, ce, tc.code)
		}
	}
	// CTL-15: ReadLastApplError returns the last one received.
	lae, err := c.ReadLastApplError(ctx, ref)
	if err != nil || lae == nil || lae.CtlNum != 9 {
		t.Errorf("ReadLastApplError = %+v, %v", lae, err)
	}
	if lae, err := c.ReadLastApplError(ctx, Ref{LD: ref.LD, LN: ref.LN}); err != nil || lae == nil || lae.CtlNum != 9 {
		t.Errorf("ReadLastApplError(LN) = %+v, %v", lae, err)
	}
}

// CTL-16, TYP-01: the default operate of a DPC writes the BOOLEAN ctlVal
// as Dbpos into stVal (IEC 61850-7-3 DPC), and ctlNum and origin.
func TestSpec_7_3_DPCDefaultOperate(t *testing.T) {
	srv := specControlServer(t, specCF{}, func(s *Server) {
		mustRegister(t, s, "GGIO1.DPCSO1", CtlModelDirectNormal, ControlHandler{})
		mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{})
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlDPC)
	origin := &Origin{OrCat: OrCatStationControl, OrIdent: []byte("op")}
	if err := c.Operate(ctx, ref, OperateParams{CtlVal: DPCCtlVal(true), CtlNum: 42, Origin: origin}); err != nil {
		t.Fatal(err)
	}
	st := func(path ...string) *mms.Value {
		t.Helper()
		v, err := c.ReadRaw(ctx, Ref{LD: ref.LD, LN: ref.LN, Path: append([]string{"DPCSO1"}, path...), FC: FCST})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	bits, _ := st("stVal").BitString()
	if n, _ := st("stVal").BitStringLength(); n != 2 || len(bits) != 1 || bits[0] != 0x80 {
		t.Errorf("stVal = %v, want Dbpos on (10)", st("stVal"))
	}
	if n, _ := st("ctlNum").Uint64(); n != 42 {
		t.Errorf("ctlNum = %d, want 42", n)
	}
	if cat, _ := st("origin", "orCat").Int64(); cat != int64(OrCatStationControl) {
		t.Errorf("origin.orCat = %d", cat)
	}
	// A Dbpos ctlVal is type-inconsistent for a DPC Oper (TYP-01).
	if err := c.Operate(ctx, ref, OperateParams{CtlVal: DpCtlVal(false), CtlNum: 43}); dataAccessCode(err) != mms.DataAccessErrorTypeInconsistent {
		t.Errorf("Oper with DpCtlVal = %v, want type-inconsistent", err)
	}
	// The default operate does not store a ctlVal of another type: an
	// SPC ctlVal into a Dbpos stVal never happens; a BOOLEAN SPC works.
	tm := mustParseRef(t, ctlTm)
	if err := c.SelectWithValue(ctx, tm, boolParams(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OperateWait(ctx, tm, boolParams(5)); err != nil {
		t.Fatal(err)
	}
	v, err := c.ReadRaw(ctx, Ref{LD: tm.LD, LN: tm.LN, Path: []string{"SPCTm", "stVal"}, FC: FCST})
	if b, _ := v.Bool(); err != nil || !b {
		t.Errorf("SPCTm stVal = %v, %v", v, err)
	}
}

// Chaos: clients race SelectWithValue, OperateWait, Cancel and
// association loss on one control. Invariants (IEC 61850-7-2 Table 47):
// operations never overlap, at most one client holds the selection, and
// the control is free at the end.
func TestChaos_ControlStateMachine(t *testing.T) {
	var inFlight, maxInFlight, executed atomic.Int32
	exec := func(context.Context, ControlRequest) error {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inFlight.Add(-1)
		executed.Add(1)
		return nil
	}
	srv := controlServer(t, ControlHandler{OnOperate: exec, OnExecute: exec})
	ref := mustParseRef(t, ctlSBOw)
	ctx := waitCtx(t)

	const clients, rounds = 6, 15
	var granted atomic.Int32
	var wg sync.WaitGroup
	for i := range clients {
		c := controlClient(t, srv)
		wg.Go(func() {
			for r := range rounds {
				n := uint8(i*rounds + r)
				p := boolParams(n)
				if err := c.SelectWithValue(ctx, ref, p); err != nil {
					continue
				}
				if g := granted.Add(1); g != 1 {
					t.Errorf("%d clients hold the selection", g)
				}
				switch r % 4 {
				case 0:
					granted.Add(-1)
					if err := c.Cancel(ctx, ref, cancelParams(n)); err != nil {
						t.Errorf("owner Cancel: %v", err)
					}
				case 3:
					if i%2 == 1 {
						// Association loss while selected; the client is done.
						granted.Add(-1)
						_ = c.mmsClient.Abort(ctx)
						return
					}
					fallthrough
				default:
					granted.Add(-1) // the operation ends the selection (operate-once)
					if _, err := c.OperateWait(ctx, ref, p); err != nil {
						t.Errorf("owner OperateWait: %v", err)
					}
				}
			}
		})
	}
	wg.Wait()
	if m := maxInFlight.Load(); m > 1 {
		t.Errorf("%d operations overlapped", m)
	}
	if executed.Load() == 0 {
		t.Error("nothing executed")
	}
	c := controlClient(t, srv)
	waitFor(t, func() bool { return c.SelectWithValue(ctx, ref, boolParams(250)) == nil })
}

// CTL-02, CTL-13, TYP-01, TYP-03: client encoding.
func TestSpec_8_1_Table85_ClientEncoding(t *testing.T) {
	q := &TimeQuality{ClockNotSynchronized: true, TimeAccuracy: 31}
	oper := encodeOper(OperateParams{CtlVal: DPCCtlVal(true), TQuality: q}, true)
	m, _ := oper.Structure()
	if len(m) != 7 {
		t.Fatalf("Oper with operTm has %d members", len(m))
	}
	if tm, _ := m[1].UTCTime(); tm.Unix() != 0 || m[1].UTCTimeQuality() != 0 {
		t.Errorf("immediate operTm = %v (q %#x), want all zero", tm, m[1].UTCTimeQuality())
	}
	if got := m[4].UTCTimeQuality(); got != 0x3f {
		t.Errorf("T quality = %#x, want 0x3f", got)
	}
	if m[0].Type() != mms.ValueTypeBoolean {
		t.Errorf("DPCCtlVal type %v, want BOOLEAN", m[0].Type())
	}
	if DpCtlVal(true).Type() != mms.ValueTypeBitString {
		t.Error("DpCtlVal changed type (kept for compatibility)")
	}
	if m, _ := encodeOper(OperateParams{CtlVal: BoolCtlVal(true)}, false).Structure(); len(m) != 6 || m[3].UTCTimeQuality() != 0x2a {
		// SystemClock: not synchronised, 10 bits (v1.18.0; 0x0a before).
		t.Errorf("Oper without operTm: %d members, T quality %#x", len(m), m[3].UTCTimeQuality())
	}

	cancel := encodeCancel(CancelParams{CtlVal: BoolCtlVal(true), Test: true}, true)
	m, _ = cancel.Structure()
	if len(m) != 6 {
		t.Fatalf("Cancel with operTm has %d members", len(m))
	}
	if b, _ := m[5].Bool(); !b {
		t.Error("Cancel Test not encoded")
	}
	if m, _ := buildCancel(CancelParams{CtlVal: BoolCtlVal(true)}).Structure(); len(m) != 5 {
		t.Errorf("Cancel without operTm: %d members", len(m))
	}

	ts := func(names ...string) mms.TypeSpec {
		s := mms.TypeSpec{Type: mms.ValueTypeStructure}
		for _, n := range names {
			et := mms.TypeSpec{Type: mms.ValueTypeBoolean}
			if n == "operTm" || n == "" {
				et.Type = mms.ValueTypeUTCTime
			}
			s.Elements = append(s.Elements, mms.TypeSpecElement{Name: n, Type: et})
		}
		return s
	}
	for name, tc := range map[string]struct {
		ts   mms.TypeSpec
		want operTmLayout
	}{
		"named":     {ts("ctlVal", "operTm", "origin"), operTmPresent},
		"without":   {ts("ctlVal", "origin", "ctlNum"), operTmAbsent},
		"unnamed":   {ts("", "", ""), operTmPresent},
		"no struct": {mms.TypeSpec{Type: mms.ValueTypeBoolean}, operTmUnknown},
	} {
		if got := operTmLayoutOf(tc.ts); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}
