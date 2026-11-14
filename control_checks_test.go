// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// Spec tests of the control model's check conditions (IEC 61850-7-2
// 20.2.1, 20.5.2.7, Table 54; IEC 61850-8-1 20.11; IEC 61850-7-4 CILO,
// RSYN, XCBR OpCnt) and of the opt-in process response.

const (
	ctlPos = ctlLD + "/XCBR1.Pos" // DPC: SBOw, Oper/Cancel with operTm
)

// checksCF are the CF values of XCBR1.Pos in the checks fixture.
type checksCF struct {
	operTimeoutMs int
}

// checksSCL is the interop GOOSE model with XCBR1 (Pos: a DPC with
// SBOw, an Oper with operTm, stSeld, origin, ctlNum; OpCnt: INS), CILO1
// (EnaOpn, EnaCls) and RSYN1 (Rel).
func checksSCL(t *testing.T, cf checksCF) *scl.SCL {
	t.Helper()
	raw, err := os.ReadFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	replace := func(old, new string) {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("fixture: %q not found", old)
		}
		s = strings.Replace(s, old, new, 1)
	}
	replace(`<LN prefix="" lnClass="MMXU" inst="1"`, fmt.Sprintf(`<LN prefix="" lnClass="XCBR" inst="1" lnType="XCBR1_Type">
            <DOI name="Pos">
              <DAI name="ctlModel"><Val>sbo-with-enhanced-security</Val></DAI>
              <DAI name="operTimeout"><Val>%d</Val></DAI>
            </DOI>
          </LN>
          <LN prefix="" lnClass="CILO" inst="1" lnType="CILO1_Type"/>
          <LN prefix="" lnClass="RSYN" inst="1" lnType="RSYN1_Type"/>
          <LN prefix="" lnClass="MMXU" inst="1"`, cf.operTimeoutMs))
	replace(`<LNodeType id="MMXU1_Type"`, `<LNodeType id="XCBR1_Type" lnClass="XCBR">
      <DO name="Pos"   type="DPCTm_Type"/>
      <DO name="OpCnt" type="INS_Type"/>
    </LNodeType>
    <LNodeType id="CILO1_Type" lnClass="CILO">
      <DO name="EnaOpn" type="SPS_Type"/>
      <DO name="EnaCls" type="SPS_Type"/>
    </LNodeType>
    <LNodeType id="RSYN1_Type" lnClass="RSYN">
      <DO name="Rel" type="SPS_Type"/>
    </LNodeType>
    <LNodeType id="MMXU1_Type"`)
	replace(`<DOType id="MV_Type"`, `<DOType id="DPCTm_Type" cdc="DPC">
      <DA name="origin"      fc="ST" bType="Struct" type="Originator_Type"/>
      <DA name="ctlNum"      fc="ST" bType="INT8U"/>
      <DA name="stVal"       fc="ST" bType="Dbpos" dchg="true"/>
      <DA name="q"           fc="ST" bType="Quality" qchg="true"/>
      <DA name="t"           fc="ST" bType="Timestamp"/>
      <DA name="stSeld"      fc="ST" bType="BOOLEAN" dchg="true"/>
      <DA name="ctlModel"    fc="CF" bType="Enum" type="CtlModels"/>
      <DA name="operTimeout" fc="CF" bType="INT32U"/>
      <DA name="SBOw"        fc="CO" bType="Struct" type="OperDPCTm_Type"/>
      <DA name="Oper"        fc="CO" bType="Struct" type="OperDPCTm_Type"/>
      <DA name="Cancel"      fc="CO" bType="Struct" type="CancelDPCTm_Type"/>
    </DOType>
    <DOType id="MV_Type"`)
	replace(`<DAType id="Originator_Type"`, `<DAType id="OperDPCTm_Type">
      <BDA name="ctlVal" bType="BOOLEAN"/>
      <BDA name="operTm" bType="Timestamp"/>
      <BDA name="origin" bType="Struct" type="Originator_Type"/>
      <BDA name="ctlNum" bType="INT8U"/>
      <BDA name="T"      bType="Timestamp"/>
      <BDA name="Test"   bType="BOOLEAN"/>
      <BDA name="Check"  bType="Check"/>
    </DAType>
    <DAType id="CancelDPCTm_Type">
      <BDA name="ctlVal" bType="BOOLEAN"/>
      <BDA name="operTm" bType="Timestamp"/>
      <BDA name="origin" bType="Struct" type="Originator_Type"/>
      <BDA name="ctlNum" bType="INT8U"/>
      <BDA name="T"      bType="Timestamp"/>
      <BDA name="Test"   bType="BOOLEAN"/>
    </DAType>
    <DAType id="Originator_Type"`)
	doc, err := scl.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// checksServer serves the checks fixture with XCBR1.Pos registered under
// model and h.
func checksServer(t *testing.T, cf checksCF, model CtlModel, h ControlHandler) *Server {
	t.Helper()
	m, err := NewServerModelFromSCL(checksSCL(t, cf), "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(m, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	if err := srv.RegisterControl(ctlLD, "XCBR1.Pos", model, h); err != nil {
		t.Fatal(err)
	}
	return srv
}

// setStVal sets the ST stVal of a data object (LN.DO) of the fixture.
func setStVal(srv *Server, lnDO string, v *mms.Value) {
	ln, do, _ := strings.Cut(lnDO, ".")
	srv.SetValue(context.Background(), servermodel.StoreKey(ctlLD, ln+"$ST$"+do+"$stVal"), v)
}

// getST reads an ST attribute of the fixture from the store.
func getST(srv *Server, lnDO, attr string) *mms.Value {
	ln, do, _ := strings.Cut(lnDO, ".")
	return srv.ValueStore().Get(servermodel.StoreKey(ctlLD, ln+"$ST$"+do+"$"+attr))
}

// stBool reads a BOOLEAN ST attribute; false when unset.
func stBool(srv *Server, lnDO, attr string) bool {
	v := getST(srv, lnDO, attr)
	if v == nil {
		return false
	}
	b, _ := v.Bool()
	return b
}

// stInt reads an integer ST attribute; 0 when unset.
func stInt(srv *Server, lnDO, attr string) int64 {
	v := getST(srv, lnDO, attr)
	if v == nil {
		return 0
	}
	if n, ok := v.Int64(); ok {
		return n
	}
	n, _ := v.Uint64()
	return int64(n)
}

// dbpos returns the first octet of a Dbpos value (00, 40, 80, C0).
func dbpos(t *testing.T, v *mms.Value) byte {
	t.Helper()
	if v == nil {
		t.Fatal("Dbpos unset")
	}
	bits, ok := v.BitString()
	if !ok || len(bits) != 1 {
		t.Fatalf("not a Dbpos: %v", v)
	}
	return bits[0] & 0xC0
}

// laeCause checks a refusal with object-access-denied (IEC 61850-8-1
// Table 87) and the AddCause of its LastApplError (20.11, Table 97).
func laeCause(t *testing.T, what string, err error, cause AddCause) {
	t.Helper()
	refusal(t, what, err, cause, mms.DataAccessErrorObjectAccessDenied)
	var ce *ControlError
	if errors.As(err, &ce) && (ce.LastApplError == nil || ce.LastApplError.AddCause != cause) {
		t.Errorf("%s: LastApplError = %+v, want AddCause %v", what, ce.LastApplError, cause)
	}
}

func posParams(n uint8, on bool, check CheckConditions) OperateParams {
	return OperateParams{CtlVal: DPCCtlVal(on), CtlNum: n, Check: check}
}

// IEC 61850-7-2 20.2.1, 20.5.2.7: interlock-check is an operative test
// run at SelectWithValue and Operate; synchrocheck a dynamic test run at
// Operate only; a check not asked for is not run.
func TestSpec_7_2_20_5_2_7_CheckCombinations(t *testing.T) {
	type calls struct{ interlock, synchro []string }
	var mu sync.Mutex
	var got calls
	srv := checksServer(t, checksCF{}, CtlModelSBOEnhanced, ControlHandler{
		CheckInterlock: func(_ context.Context, req ControlRequest) error {
			mu.Lock()
			got.interlock = append(got.interlock, req.Operation)
			mu.Unlock()
			return nil
		},
		CheckSynchro: func(_ context.Context, req ControlRequest) error {
			mu.Lock()
			got.synchro = append(got.synchro, req.Operation)
			mu.Unlock()
			return nil
		},
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	for i, tc := range []struct {
		check CheckConditions
		want  calls
	}{
		{0, calls{}},
		{CheckInterlockCheck, calls{interlock: []string{"select", "operate"}}},
		{CheckSynchroCheck, calls{synchro: []string{"operate"}}},
		{CheckInterlockCheck | CheckSynchroCheck, calls{interlock: []string{"select", "operate"}, synchro: []string{"operate"}}},
	} {
		mu.Lock()
		got = calls{}
		mu.Unlock()
		p := posParams(uint8(i+1), i%2 == 0, tc.check)
		if err := c.SelectWithValue(ctx, ref, p); err != nil {
			t.Fatalf("check %02b: SBOw: %v", tc.check, err)
		}
		if term, err := c.OperateWait(ctx, ref, p); err != nil || !term.Positive() {
			t.Fatalf("check %02b: OperateWait = %+v, %v", tc.check, term, err)
		}
		mu.Lock()
		if fmt.Sprint(got.interlock) != fmt.Sprint(tc.want.interlock) || fmt.Sprint(got.synchro) != fmt.Sprint(tc.want.synchro) {
			t.Errorf("check %02b: calls %+v, want %+v", tc.check, got, tc.want)
		}
		mu.Unlock()
	}
}

// IEC 61850-7-2 Table 54 blocked-by-interlocking, IEC 61850-7-4 CILO:
// with interlock-check, a close needs EnaCls TRUE and an open EnaOpn
// TRUE, at SelectWithValue and again at Operate; without the Check bit
// the CILO does not gate (20.5.2.7).
func TestSpec_7_4_CILO_EnaOpnEnaCls(t *testing.T) {
	srv := checksServer(t, checksCF{}, CtlModelSBOEnhanced, ControlHandler{Interlocking: "CILO1"})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	il := CheckInterlockCheck
	setStVal(srv, "CILO1.EnaOpn", mms.NewBoolean(true))
	setStVal(srv, "CILO1.EnaCls", mms.NewBoolean(false))

	laeCause(t, "SBOw close", c.SelectWithValue(ctx, ref, posParams(1, true, il)), AddCauseBlockedByInterlocking)
	if stBool(srv, "XCBR1.Pos", "stSeld") {
		t.Error("stSeld TRUE after a refused select")
	}
	// Open is enabled.
	if err := c.SelectWithValue(ctx, ref, posParams(2, false, il)); err != nil {
		t.Fatalf("SBOw open: %v", err)
	}
	if term, err := c.OperateWait(ctx, ref, posParams(2, false, il)); err != nil || !term.Positive() {
		t.Fatalf("Operate open = %+v, %v", term, err)
	}
	// Without interlock-check the CILO is not consulted.
	if err := c.SelectWithValue(ctx, ref, posParams(3, true, 0)); err != nil {
		t.Fatalf("SBOw close without Check: %v", err)
	}
	if _, err := c.OperateWait(ctx, ref, posParams(3, true, 0)); err != nil {
		t.Fatalf("Operate close without Check: %v", err)
	}
	// Enabled at select, blocked at operate: Oper_resp- and the
	// selection ends.
	setStVal(srv, "CILO1.EnaCls", mms.NewBoolean(true))
	if err := c.SelectWithValue(ctx, ref, posParams(4, true, il)); err != nil {
		t.Fatalf("SBOw close enabled: %v", err)
	}
	setStVal(srv, "CILO1.EnaCls", mms.NewBoolean(false))
	laeCause(t, "Oper close", c.Operate(ctx, ref, posParams(4, true, il)), AddCauseBlockedByInterlocking)
	refusal(t, "Oper after refusal", c.Operate(ctx, ref, posParams(4, true, il)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
}

// IEC 61850-7-2 Table 54: a CheckInterlock refusal is
// blocked-by-interlocking unless it names another AddCause; a panicking
// check is an internal error (LastApplError Error 1, IEC 61850-8-1
// 20.11).
func TestSpec_7_2_Table54_InterlockHookCauses(t *testing.T) {
	srv := checksServer(t, checksCF{}, CtlModelDirectEnhanced, ControlHandler{
		CheckInterlock: func(_ context.Context, req ControlRequest) error {
			switch req.CtlNum {
			case 1:
				return errors.New("bay interlocked")
			case 2:
				return &ControlError{AddCause: AddCauseBlockedByMode}
			case 3:
				panic("boom")
			}
			return nil
		},
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	il := CheckInterlockCheck
	laeCause(t, "plain error", c.Operate(ctx, ref, posParams(1, true, il)), AddCauseBlockedByInterlocking)
	laeCause(t, "ControlError", c.Operate(ctx, ref, posParams(2, true, il)), AddCauseBlockedByMode)
	err := c.Operate(ctx, ref, posParams(3, true, il))
	var ce *ControlError
	if !errors.As(err, &ce) || ce.LastApplError == nil || ce.LastApplError.Error != 1 || ce.AddCause != AddCauseUnknown {
		t.Errorf("panic = %v (%+v), want Error 1, AddCause unknown", err, ce)
	}
	if term, err := c.OperateWait(ctx, ref, posParams(4, true, il)); err != nil || !term.Positive() {
		t.Errorf("allowed OperateWait = %+v, %v", term, err)
	}
}

// IEC 61850-7-2 Table 54 blocked-by-synchrocheck (time limit passed
// without synchronism), IEC 61850-7-4 RSYN Rel: the Operate waits up to
// SynchroTimeout for the release.
func TestSpec_7_4_RSYN_Release(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const limit = 150 * time.Millisecond
		srv := checksServer(t, checksCF{}, CtlModelDirectEnhanced, ControlHandler{Synchrocheck: "RSYN1", SynchroTimeout: limit})
		c := controlClient(t, srv)
		ctx := waitCtx(t)
		ref := mustParseRef(t, ctlPos)
		sy := CheckSynchroCheck

		start := time.Now()
		laeCause(t, "no release", c.Operate(ctx, ref, posParams(1, true, sy)), AddCauseBlockedBySynchrocheck)
		if d := time.Since(start); d < limit {
			t.Errorf("refused after %v, before the time limit %v", d, limit)
		}
		// The release comes within the limit.
		released := make(chan struct{})
		go func() {
			defer close(released)
			time.Sleep(50 * time.Millisecond) // fake time: within the limit of 150 ms
			setStVal(srv, "RSYN1.Rel", mms.NewBoolean(true))
		}()
		term, err := c.OperateWait(ctx, ref, posParams(2, true, sy))
		<-released
		if err != nil || !term.Positive() {
			t.Fatalf("released OperateWait = %+v, %v", term, err)
		}
		// Without synchrocheck the RSYN is not consulted.
		setStVal(srv, "RSYN1.Rel", mms.NewBoolean(false))
		if _, err := c.OperateWait(ctx, ref, posParams(3, true, 0)); err != nil {
			t.Errorf("OperateWait without Check: %v", err)
		}
	})
}

// SynchroTimeout zero checks the RSYN release once; CheckSynchro's
// errors are blocked-by-synchrocheck unless they name another AddCause,
// and its context ends at SynchroTimeout.
func TestSpec_7_2_Table54_SynchroHook(t *testing.T) {
	srv := checksServer(t, checksCF{}, CtlModelDirectNormal, ControlHandler{Synchrocheck: "RSYN1"})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	sy := CheckSynchroCheck
	laeCause(t, "RSYN once", c.Operate(ctx, ref, posParams(1, true, sy)), AddCauseBlockedBySynchrocheck)

	var deadline atomic.Bool
	srv2 := checksServer(t, checksCF{}, CtlModelDirectNormal, ControlHandler{
		SynchroTimeout: 80 * time.Millisecond,
		CheckSynchro: func(ctx context.Context, req ControlRequest) error {
			switch req.CtlNum {
			case 1:
				<-ctx.Done()
				_, ok := ctx.Deadline()
				deadline.Store(ok)
				return ctx.Err()
			case 2:
				return &ControlError{AddCause: AddCauseBlockedByHealth}
			}
			return nil
		},
	})
	c2 := controlClient(t, srv2)
	laeCause(t, "hook time limit", c2.Operate(ctx, ref, posParams(1, true, sy)), AddCauseBlockedBySynchrocheck)
	if !deadline.Load() {
		t.Error("CheckSynchro context had no deadline")
	}
	laeCause(t, "hook cause", c2.Operate(ctx, ref, posParams(2, true, sy)), AddCauseBlockedByHealth)
	if err := c2.Operate(ctx, ref, posParams(3, true, sy)); err != nil {
		t.Errorf("allowed Operate: %v", err)
	}
	if b := dbpos(t, getST(srv2, "XCBR1.Pos", "stVal")); b != 0x80 {
		t.Errorf("stVal = %02x after the allowed operate, want on (80)", b)
	}
}

// IEC 61850-7-2 Figures 41, 42: a time activated operate runs its tests
// when operTm is reached; a failed test is a negative
// TimeActivatedOperate termination (IEC 61850-8-1 20.9.2.3: LastApplError
// with the AddCause, then the Oper).
func TestSpec_7_2_TimeActivatedChecksTerminate(t *testing.T) {
	var interlocked atomic.Bool
	interlocked.Store(true)
	srv := checksServer(t, checksCF{}, CtlModelSBOEnhanced, ControlHandler{
		CheckInterlock: func(_ context.Context, req ControlRequest) error {
			if req.Operation == "operate" && interlocked.Load() {
				return errors.New("interlocked")
			}
			return nil
		},
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	p := posParams(5, true, CheckInterlockCheck)
	p.OperTm = time.Now().Add(150 * time.Millisecond)
	if err := c.SelectWithValue(ctx, ref, p); err != nil {
		t.Fatal(err)
	}
	term, err := c.OperateWait(ctx, ref, p)
	if !errors.Is(err, ErrCommandTerminationNegative) || term == nil || term.LastApplError == nil ||
		term.LastApplError.AddCause != AddCauseBlockedByInterlocking || term.LastApplError.CtlNum != 5 {
		t.Fatalf("OperateWait = %+v, %v; want a negative termination, blocked-by-interlocking", term, err)
	}
	if time.Now().Before(p.OperTm) {
		t.Error("termination before operTm")
	}
	// Free again; the next one passes.
	interlocked.Store(false)
	p = posParams(6, true, CheckInterlockCheck)
	p.OperTm = time.Now().Add(100 * time.Millisecond)
	if err := c.SelectWithValue(ctx, ref, p); err != nil {
		t.Fatal(err)
	}
	if term, err := c.OperateWait(ctx, ref, p); err != nil || !term.Positive() {
		t.Fatalf("OperateWait = %+v, %v", term, err)
	}
}

// IEC 61850-7-2 Figure 42 (WaitForExecution, Cancel_req): the owner's
// Cancel while the synchrocheck waits is accepted and the time activated
// operate ends with a negative termination, abortion-by-cancel; Table 47:
// another client's Cancel is locked-by-other-client, an Operate or SBOw
// command-already-in-execution.
func TestSpec_7_2_Table47_CancelDuringSynchrocheck(t *testing.T) {
	waiting := make(chan struct{}, 1)
	var operated atomic.Int32
	srv := checksServer(t, checksCF{}, CtlModelSBOEnhanced, ControlHandler{
		SynchroTimeout: 5 * time.Second,
		CheckSynchro: func(ctx context.Context, _ ControlRequest) error {
			waiting <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
		OnOperate: func(context.Context, ControlRequest) error {
			operated.Add(1)
			return nil
		},
	})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	p := posParams(7, true, CheckSynchroCheck)
	p.OperTm = time.Now().Add(50 * time.Millisecond)
	if err := a.SelectWithValue(ctx, ref, p); err != nil {
		t.Fatal(err)
	}
	type result struct {
		term *CommandTermination
		err  error
	}
	done := make(chan result, 1)
	go func() {
		term, err := a.OperateWait(ctx, ref, p)
		done <- result{term, err}
	}()
	<-waiting
	cp := CancelParams{CtlVal: DPCCtlVal(true), CtlNum: 7, OperTm: p.OperTm}
	refusal(t, "B Cancel", b.Cancel(ctx, ref, cp), AddCauseLockedByOtherClient, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "B Oper", b.Operate(ctx, ref, posParams(8, true, 0)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	refusal(t, "B SBOw", b.SelectWithValue(ctx, ref, posParams(8, true, 0)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	if err := a.Cancel(ctx, ref, cp); err != nil {
		t.Fatalf("A Cancel: %v", err)
	}
	r := <-done
	if !errors.Is(r.err, ErrCommandTerminationNegative) || r.term == nil || r.term.LastApplError == nil ||
		r.term.LastApplError.AddCause != AddCauseAbortionByCancel {
		t.Fatalf("OperateWait = %+v, %v; want negative, abortion-by-cancel", r.term, r.err)
	}
	if operated.Load() != 0 {
		t.Error("OnOperate called after Cancel")
	}
	if err := b.SelectWithValue(ctx, ref, posParams(9, true, 0)); err != nil {
		t.Errorf("B SBOw after the cancel: %v", err)
	}
}

// IEC 61850-7-2 Table 54 position-reached, as an opt-in operative test of
// the process response.
func TestSpec_7_2_Table54_PositionReached(t *testing.T) {
	srv := checksServer(t, checksCF{}, CtlModelDirectNormal, ControlHandler{Process: &ProcessResponse{PositionReached: true, OpCnt: "XCBR1.OpCnt"}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	if err := c.Operate(ctx, ref, posParams(1, true, 0)); err != nil {
		t.Fatal(err)
	}
	laeCause(t, "close again", c.Operate(ctx, ref, posParams(2, true, 0)), AddCausePositionReached)
	if err := c.Operate(ctx, ref, posParams(3, false, 0)); err != nil {
		t.Fatal(err)
	}
	if n := stInt(srv, "XCBR1.OpCnt", "stVal"); n != 2 {
		t.Errorf("OpCnt = %d, want 2", n)
	}
	// An SPC stVal (BOOLEAN) is a position too.
	srv2 := controlServer(t, ControlHandler{})
	mustRegister(t, srv2, "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{Process: &ProcessResponse{PositionReached: true}})
	c2 := controlClient(t, srv2)
	spc := mustParseRef(t, ctlDirect)
	srv2.SetValue(ctx, servermodel.StoreKey(ctlLD, "GGIO1$ST$SPCSO1$stVal"), mms.NewBoolean(false))
	laeCause(t, "SPC off at off", c2.Operate(ctx, spc, OperateParams{CtlVal: BoolCtlVal(false), CtlNum: 1}), AddCausePositionReached)
	if err := c2.Operate(ctx, spc, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 2}); err != nil {
		t.Errorf("SPC on: %v", err)
	}
}

// IEC 61850-7-3 Dbpos intermediate-state, IEC 61850-7-2 20.3.3 d
// (WaitForChange: CommandTermination positive when the position is
// reached, negative at operTimeout): the opt-in travel of a DPC.
func TestSpec_7_3_DPCTravelEnhanced(t *testing.T) {
	const travel = 200 * time.Millisecond
	srv := checksServer(t, checksCF{}, CtlModelDirectEnhanced, ControlHandler{Process: &ProcessResponse{TravelTime: travel, OpCnt: "XCBR1.OpCnt"}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := c.OperateWait(ctx, ref, posParams(1, true, 0))
		done <- err
	}()
	waitFor(t, func() bool { return stInt(srv, "XCBR1.Pos", "ctlNum") == 1 })
	if b := dbpos(t, getST(srv, "XCBR1.Pos", "stVal")); b != 0x00 {
		t.Errorf("stVal during travel = %02x, want intermediate-state (00)", b)
	}
	if err := <-done; err != nil {
		t.Fatalf("OperateWait: %v", err)
	}
	if d := time.Since(start); d < travel {
		t.Errorf("termination after %v, before the travel time", d)
	}
	if b := dbpos(t, getST(srv, "XCBR1.Pos", "stVal")); b != 0x80 {
		t.Errorf("stVal = %02x, want on (80)", b)
	}
	if n := stInt(srv, "XCBR1.OpCnt", "stVal"); n != 1 {
		t.Errorf("OpCnt = %d, want 1", n)
	}

	// operTimeout shorter than the travel: time-limit-over, the device
	// stays in intermediate-state, nothing is counted.
	slow := checksServer(t, checksCF{operTimeoutMs: 50}, CtlModelDirectEnhanced, ControlHandler{Process: &ProcessResponse{TravelTime: travel, OpCnt: "XCBR1.OpCnt"}})
	cs := controlClient(t, slow)
	term, err := cs.OperateWait(ctx, ref, posParams(2, true, 0))
	if !errors.Is(err, ErrCommandTerminationNegative) || term == nil || term.LastApplError == nil || term.LastApplError.AddCause != AddCauseTimeLimit {
		t.Fatalf("OperateWait = %+v, %v; want negative, time-limit-over", term, err)
	}
	if b := dbpos(t, getST(slow, "XCBR1.Pos", "stVal")); b != 0x00 {
		t.Errorf("stVal = %02x, want intermediate-state (00)", b)
	}
	if n := stInt(slow, "XCBR1.OpCnt", "stVal"); n != 0 {
		t.Errorf("OpCnt = %d, want 0", n)
	}
}

// IEC 61850-7-2 20.2: with normal security the Operate response does not
// wait for the device; the control is in execution until the end
// position (Table 47 Operate: command-already-in-execution).
func TestSpec_7_3_DPCTravelNormal(t *testing.T) {
	srv := checksServer(t, checksCF{}, CtlModelDirectNormal, ControlHandler{Process: &ProcessResponse{TravelTime: 150 * time.Millisecond}})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlPos)
	if err := c.Operate(ctx, ref, posParams(1, false, 0)); err != nil {
		t.Fatal(err)
	}
	if b := dbpos(t, getST(srv, "XCBR1.Pos", "stVal")); b != 0x00 {
		t.Errorf("stVal after the response = %02x, want intermediate-state (00)", b)
	}
	refusal(t, "Oper during travel", c.Operate(ctx, ref, posParams(2, true, 0)), AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)
	waitFor(t, func() bool { return dbpos(t, getST(srv, "XCBR1.Pos", "stVal")) == 0x40 })
	waitFor(t, func() bool { return c.Operate(ctx, ref, posParams(3, true, 0)) == nil })
}

// RegisterControl refuses bindings the model has not.
func TestRegisterControl_CheckBindings(t *testing.T) {
	m, err := NewServerModelFromSCL(checksSCL(t, checksCF{}), "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(m, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	for _, h := range []ControlHandler{
		{Interlocking: "CILO9"},
		{Interlocking: "RSYN1"},
		{Synchrocheck: "CILO1"},
		{Synchrocheck: "OtherLD/RSYN1"},
		{Process: &ProcessResponse{OpCnt: "XCBR1"}},
		{Process: &ProcessResponse{OpCnt: "XCBR1.Pos"}},
		{Process: &ProcessResponse{OpCnt: "CILO1.EnaOpn"}},
	} {
		if err := srv.RegisterControl(ctlLD, "XCBR1.Pos", CtlModelDirectNormal, h); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%+v: %v, want ErrInvalidArgument", h, err)
		}
	}
	h := ControlHandler{Interlocking: ctlLD + "/CILO1", Synchrocheck: "RSYN1", Process: &ProcessResponse{OpCnt: ctlLD + "/XCBR1.OpCnt"}}
	if err := srv.RegisterControl(ctlLD, "XCBR1.Pos", CtlModelDirectNormal, h); err != nil {
		t.Errorf("valid bindings: %v", err)
	}
}

// Without a model the bindings are not validated (stub servers).
func TestResolveChecks_NoStore(t *testing.T) {
	s := &Server{}
	c, err := s.resolveChecks("LD", ControlHandler{Interlocking: "CILO1", Synchrocheck: "RSYN1", Process: &ProcessResponse{OpCnt: "XCBR1.OpCnt"}})
	if err != nil || c.enaCls != "LD/CILO1$ST$EnaCls$stVal" || c.rel != "LD/RSYN1$ST$Rel$stVal" || c.opCnt != "LD/XCBR1$ST$OpCnt$stVal" {
		t.Errorf("resolveChecks = %+v, %v", c, err)
	}
	if s.storeBool("x") {
		t.Error("storeBool without a store")
	}
	s.countOperation(context.Background(), &controlRegistration{checks: c}) // no store: no-op
}

// The operation counter saturates at the INT32 maximum.
func TestCountOperation_Saturates(t *testing.T) {
	srv := checksServer(t, checksCF{}, CtlModelDirectNormal, ControlHandler{})
	reg := &controlRegistration{checks: controlChecks{opCnt: servermodel.StoreKey(ctlLD, "XCBR1$ST$OpCnt$stVal")}}
	setStVal(srv, "XCBR1.OpCnt", mms.NewInteger(1<<31-1))
	srv.countOperation(context.Background(), reg)
	if n := stInt(srv, "XCBR1.OpCnt", "stVal"); n != 1<<31-1 {
		t.Errorf("OpCnt = %d", n)
	}
}

// Travel with OnExecute: the travel completes before OnExecute runs.
func TestExecuteFn_TravelThenExecute(t *testing.T) {
	var atExec atomic.Uint32
	var srv *Server
	srv = checksServer(t, checksCF{}, CtlModelDirectEnhanced, ControlHandler{
		Process: &ProcessResponse{TravelTime: 30 * time.Millisecond},
		OnExecute: func(context.Context, ControlRequest) error {
			bits, _ := getST(srv, "XCBR1.Pos", "stVal").BitString()
			atExec.Store(uint32(bits[0]))
			return nil
		},
	})
	c := controlClient(t, srv)
	if _, err := c.OperateWait(waitCtx(t), mustParseRef(t, ctlPos), posParams(1, true, 0)); err != nil {
		t.Fatal(err)
	}
	if atExec.Load() != 0x80 {
		t.Errorf("stVal at OnExecute = %02x, want on (80)", atExec.Load())
	}
}

// Chaos: clients race SelectWithValue and OperateWait with random Check
// bits while the CILO, the RSYN and the interlocking hook change.
// Invariants (IEC 61850-7-2 20.2.1, Table 47, Table 54): an operation
// runs only after the checks its Check asked for passed, operations never
// overlap, refusals carry an AddCause of the control model, and the
// control is free at the end.
func TestChaos_ControlChecks(t *testing.T) {
	var passed sync.Map // ctlNum -> Check bits whose checks passed at Operate
	var inFlight, overlap, executed atomic.Int32
	var blocked atomic.Bool
	srv := checksServer(t, checksCF{}, CtlModelSBOEnhanced, ControlHandler{
		Interlocking:   "CILO1",
		Synchrocheck:   "RSYN1",
		SynchroTimeout: 20 * time.Millisecond,
		CheckInterlock: func(_ context.Context, req ControlRequest) error {
			if blocked.Load() {
				return errors.New("blocked")
			}
			return nil
		},
		CheckSynchro: func(_ context.Context, req ControlRequest) error {
			passed.Store(req.CtlNum, req.Check)
			return nil
		},
		OnOperate: func(_ context.Context, req ControlRequest) error {
			if inFlight.Add(1) > 1 {
				overlap.Add(1)
			}
			defer inFlight.Add(-1)
			if req.Check.Has(CheckSynchroCheck) {
				if v, ok := passed.LoadAndDelete(req.CtlNum); !ok || v.(CheckConditions) != req.Check {
					t.Errorf("ctlNum %d operated without a passed synchrocheck", req.CtlNum)
				}
			}
			executed.Add(1)
			time.Sleep(time.Millisecond)
			return nil
		},
	})
	ref := mustParseRef(t, ctlPos)
	ctx := waitCtx(t)
	allowed := map[AddCause]bool{
		AddCauseBlockedByInterlocking: true, AddCauseBlockedBySynchrocheck: true,
		AddCauseObjectAlreadySelected: true, AddCauseCommandAlreadyExec: true,
		AddCauseLockedByOtherClient: true, AddCauseObjectNotSelected: true,
	}
	stopFlip := make(chan struct{})
	var flip sync.WaitGroup
	flip.Go(func() {
		r := rand.New(rand.NewPCG(1, 2))
		for {
			select {
			case <-stopFlip:
				return
			case <-time.After(3 * time.Millisecond):
			}
			setStVal(srv, "CILO1.EnaOpn", mms.NewBoolean(r.IntN(3) > 0))
			setStVal(srv, "CILO1.EnaCls", mms.NewBoolean(r.IntN(3) > 0))
			setStVal(srv, "RSYN1.Rel", mms.NewBoolean(r.IntN(2) > 0))
			blocked.Store(r.IntN(5) == 0)
		}
	})
	const clients, rounds = 5, 12
	var wg sync.WaitGroup
	for i := range clients {
		c := controlClient(t, srv)
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(i), 7))
			for k := range rounds {
				n := uint8(i*rounds + k)
				p := posParams(n, r.IntN(2) == 0, CheckConditions(r.IntN(4)))
				check := func(what string, err error) {
					var ce *ControlError
					if err != nil && (!errors.As(err, &ce) || !allowed[ce.AddCause]) {
						t.Errorf("%s ctlNum %d: unexpected refusal %v", what, n, err)
					}
				}
				err := c.SelectWithValue(ctx, ref, p)
				check("SBOw", err)
				if err != nil {
					continue
				}
				_, err = c.OperateWait(ctx, ref, p)
				check("OperateWait", err)
			}
		})
	}
	wg.Wait()
	close(stopFlip)
	flip.Wait()
	if overlap.Load() != 0 {
		t.Errorf("%d operations overlapped", overlap.Load())
	}
	if executed.Load() == 0 {
		t.Error("nothing executed")
	}
	c := controlClient(t, srv)
	waitFor(t, func() bool { return c.SelectWithValue(ctx, ref, posParams(250, true, 0)) == nil })
}
