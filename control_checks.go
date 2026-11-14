// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// The check conditions of the control model (IEC 61850-7-2 20.2.1,
// 20.5.2.7). The Check service parameter asks for an interlocking check,
// an operative test applied in the state PerformTest (at SelectWithValue
// and at Operate), and for a synchrocheck, a dynamic test applied in the
// state WaitForExecution after the Operate passed its operative tests. A
// check the control object does not support is ignored and the command
// handled (20.5.2.7): a check runs only when its Check bit is set and the
// registration supplies it ([ControlHandler.CheckInterlock],
// [ControlHandler.Interlocking], [ControlHandler.CheckSynchro],
// [ControlHandler.Synchrocheck]).

// ProcessResponse models, for the default operate (no
// [ControlHandler.OnOperate]), the switching device behind a controllable
// status: only what IEC 61850 defines. It is opt-in; nil keeps the plain
// mirror of ctlVal into stVal.
type ProcessResponse struct {
	// TravelTime is the time the device takes to reach the commanded
	// position. For a DPC (stVal Dbpos), stVal shows intermediate-state
	// (00, IEC 61850-7-3 Dbpos) from the operate until the end position
	// is reached TravelTime later. With enhanced security the control
	// stays in WaitForChange meanwhile: the CommandTermination is
	// positive when the position is reached, negative with AddCause
	// time-limit-over when the CF operTimeout passes first, and stVal
	// then stays intermediate-state (IEC 61850-7-2 20.3.3 d). Zero
	// writes the end position at once. Ignored for a stVal that is not
	// Dbpos.
	TravelTime time.Duration

	// OpCnt names an operation counter, a data object with an integer
	// stVal (INS, such as XCBR OpCnt, IEC 61850-7-4): "LN.DO" in the
	// control's logical device or "LD/LN.DO". Its stVal is incremented
	// when an operation reaches its end position; it saturates at the
	// INT32 maximum. Empty counts nothing.
	OpCnt string

	// PositionReached refuses an Operate whose ctlVal is the position
	// stVal already shows, with AddCause position-reached (IEC 61850-7-2
	// Table 54), as an operative test.
	PositionReached bool
}

// synchroPoll is how often a synchrocheck waiting for synchronism reads
// the RSYN release.
const synchroPoll = 10 * time.Millisecond

// errCancelledInTest is the cause of a check that a Cancel stopped (IEC
// 61850-7-2 Figures 38 to 42: Cancel in PerformTest or WaitForExecution).
var errCancelledInTest = errors.New("cancelled during the control's tests")

// controlChecks are the resolved store keys of a registration's CILO,
// RSYN and operation counter bindings; empty when not bound.
type controlChecks struct {
	enaOpn, enaCls string // CILO EnaOpn.stVal, EnaCls.stVal
	rel            string // RSYN Rel.stVal
	opCnt          string // operation counter stVal
}

// stRef splits the data object reference objRef ("LN.DO" or
// "LD/LN.DO"; the LD defaults to ld) into LD, LN and the DO path joined
// with $.
func stRef(ld, objRef string) (ldName, ln, doPath string) {
	if l, rest, ok := strings.Cut(objRef, "/"); ok {
		ld, objRef = l, rest
	}
	ln, do, _ := strings.Cut(objRef, ".")
	return ld, ln, strings.ReplaceAll(do, ".", "$")
}

// stValKey is the store key of the ST stVal of the data object objRef
// (see [stRef]).
func stValKey(ld, objRef string) string {
	ld, ln, do := stRef(ld, objRef)
	return servermodel.StoreKey(ld, ln+"$ST$"+do+"$stVal")
}

// resolveChecks resolves and validates the bindings of a registration
// against the server's model: a CILO with BOOLEAN EnaOpn and EnaCls
// stVal (IEC 61850-7-4 CILO), an RSYN with a BOOLEAN Rel stVal (7-4
// RSYN), an operation counter with a signed integer stVal (INS).
func (s *Server) resolveChecks(ld string, h ControlHandler) (controlChecks, error) {
	var c controlChecks
	need := func(objRef, what string, bType func(string) bool) error {
		if s.model == nil {
			return nil
		}
		l, ln, do := stRef(ld, objRef)
		if d := s.modelDO(l, ln, do); d != nil {
			for _, a := range d.Attributes {
				if a.FC == "ST" && a.Name == "stVal" && bType(a.BType) {
					return nil
				}
			}
		}
		return fmt.Errorf("%w: %s: %s.stVal not in the model or of another type", ErrInvalidArgument, what, objRef)
	}
	isBool := func(b string) bool { return b == "BOOLEAN" }
	isInt := func(b string) bool { return strings.HasPrefix(b, "INT") && !strings.HasSuffix(b, "U") }
	if ln := h.Interlocking; ln != "" {
		c.enaOpn, c.enaCls = stValKey(ld, ln+".EnaOpn"), stValKey(ld, ln+".EnaCls")
		if err := need(ln+".EnaOpn", "Interlocking", isBool); err != nil {
			return c, err
		}
		if err := need(ln+".EnaCls", "Interlocking", isBool); err != nil {
			return c, err
		}
	}
	if ln := h.Synchrocheck; ln != "" {
		c.rel = stValKey(ld, ln+".Rel")
		if err := need(ln+".Rel", "Synchrocheck", isBool); err != nil {
			return c, err
		}
	}
	if p := h.Process; p != nil && p.OpCnt != "" {
		if !strings.Contains(p.OpCnt, ".") {
			return c, fmt.Errorf("%w: Process.OpCnt %q: want LN.DO", ErrInvalidArgument, p.OpCnt)
		}
		c.opCnt = stValKey(ld, p.OpCnt)
		if err := need(p.OpCnt, "Process.OpCnt", isInt); err != nil {
			return c, err
		}
	}
	return c, nil
}

// storeBool reads a BOOLEAN from the store; false when absent (an
// EnaOpn, EnaCls or Rel without a value does not release).
func (s *Server) storeBool(key string) bool {
	if s.store == nil {
		return false
	}
	v := s.store.Get(key)
	if v == nil {
		return false
	}
	b, _ := v.Bool()
	return b
}

// checkRefusal is the refusal of a failed check: the callback's
// [*ControlError] when it names an AddCause or the callback panicked,
// else a ControlError with cause.
func checkRefusal(req ControlRequest, cause AddCause, what string, err error) error {
	if errors.Is(err, errHandlerPanic) {
		return err
	}
	var ce *ControlError
	if errors.As(err, &ce) && ce.AddCause != AddCauseUnknown {
		return err
	}
	return newControlRefusal(req, req.Operation, cause, fmt.Errorf("%s: %w", what, err))
}

// performTests runs the tests of a SelectWithValue (operative) or an
// Operate (operative, then dynamic): IEC 61850-7-2 20.2.1, 20.3.3 d,
// Figures 38 to 42. abort, closed by a Cancel, stops a waiting
// synchrocheck.
func (s *Server) performTests(ctx context.Context, reg *controlRegistration, req ControlRequest, abort <-chan struct{}) error {
	if req.Check.Has(CheckInterlockCheck) {
		if err := s.interlockTest(ctx, reg, req); err != nil {
			return err
		}
	}
	if req.Operation != "operate" {
		return nil
	}
	if err := s.positionTest(reg, req); err != nil {
		return err
	}
	if req.Check.Has(CheckSynchroCheck) {
		return s.synchroTest(ctx, reg, req, abort)
	}
	return nil
}

// interlockTest is the interlocking check (IEC 61850-7-2 Table 54
// blocked-by-interlocking): the CILO binding, EnaCls.stVal for a close
// (ctlVal TRUE) and EnaOpn.stVal for an open (FALSE), IEC 61850-7-4
// CILO; then CheckInterlock. A ctlVal that is not BOOLEAN (no open or
// close) is not gated by the CILO.
func (s *Server) interlockTest(ctx context.Context, reg *controlRegistration, req ControlRequest) error {
	if reg.checks.enaOpn != "" && req.CtlVal != nil {
		if closeCmd, ok := req.CtlVal.Bool(); ok {
			key, name := reg.checks.enaOpn, "EnaOpn"
			if closeCmd {
				key, name = reg.checks.enaCls, "EnaCls"
			}
			if !s.storeBool(key) {
				return newControlRefusal(req, req.Operation, AddCauseBlockedByInterlocking,
					fmt.Errorf("interlocking: %s %s.stVal is FALSE", reg.handler.Interlocking, name))
			}
		}
	}
	if h := reg.handler.CheckInterlock; h != nil {
		if err := callControlHandler(s.cb, h, ctx, req); err != nil {
			return checkRefusal(req, AddCauseBlockedByInterlocking, "interlocking", err)
		}
	}
	return nil
}

// positionTest refuses an Operate to the position stVal shows (IEC
// 61850-7-2 Table 54 position-reached) when the registration's
// ProcessResponse asks for it.
func (s *Server) positionTest(reg *controlRegistration, req ControlRequest) error {
	p := reg.handler.Process
	if p == nil || !p.PositionReached || reg.handler.OnOperate != nil || req.CtlVal == nil || s.store == nil {
		return nil
	}
	want, ok := req.CtlVal.Bool()
	if !ok {
		return nil
	}
	cur := s.store.Get(servermodel.StoreKey(reg.ld, reg.ln+"$ST$"+reg.doPath+"$stVal"))
	if cur == nil {
		return nil
	}
	reached := false
	if b, isBool := cur.Bool(); isBool {
		reached = b == want
	} else if bits, isBits := cur.BitString(); isBits && len(bits) > 0 {
		reached = bits[0]&0xC0 == dbposBits(want)
	}
	if reached {
		return newControlRefusal(req, req.Operation, AddCausePositionReached, errors.New("already in the commanded position"))
	}
	return nil
}

// synchroTest is the synchrocheck, a dynamic test (IEC 61850-7-2 20.2.1,
// Table 54 blocked-by-synchrocheck: the time limit passed without
// synchronism): the RSYN binding waits for Rel.stVal TRUE (IEC 61850-7-4
// RSYN), then CheckSynchro runs; both are bounded by SynchroTimeout.
func (s *Server) synchroTest(ctx context.Context, reg *controlRegistration, req ControlRequest, abort <-chan struct{}) error {
	timeout := reg.handler.SynchroTimeout
	var cancel context.CancelCauseFunc
	ctx, cancel = context.WithCancelCause(ctx)
	defer cancel(nil)
	if timeout > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, timeout)
		defer stop()
	}
	if abort != nil {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			select {
			case <-abort:
				cancel(errCancelledInTest)
			case <-stop:
			}
		}()
	}
	fail := func(what string, err error) error {
		if errors.Is(context.Cause(ctx), errCancelledInTest) {
			return newControlRefusal(req, req.Operation, AddCauseAbortionByCancel, errCancelledInTest)
		}
		return checkRefusal(req, AddCauseBlockedBySynchrocheck, what, err)
	}
	if reg.checks.rel != "" && !s.storeBool(reg.checks.rel) {
		if timeout <= 0 {
			return fail("synchrocheck", fmt.Errorf("%s Rel.stVal is FALSE", reg.handler.Synchrocheck))
		}
		tick := time.NewTicker(synchroPoll)
		for !s.storeBool(reg.checks.rel) {
			select {
			case <-tick.C:
			case <-ctx.Done():
				tick.Stop()
				return fail("synchrocheck", fmt.Errorf("%s Rel.stVal FALSE until the time limit: %w", reg.handler.Synchrocheck, ctx.Err()))
			}
		}
		tick.Stop()
	}
	if h := reg.handler.CheckSynchro; h != nil {
		if err := callControlHandler(s.cb, h, ctx, req); err != nil {
			return fail("synchrocheck", err)
		}
	}
	return nil
}

// dbposBits is the Dbpos of a BOOLEAN position: on 10, off 01 (IEC
// 61850-8-1 8.1.2.1).
func dbposBits(on bool) byte {
	if on {
		return 0x80
	}
	return 0x40
}

// travels reports whether the default operate of req moves a DPC
// through intermediate-state (ProcessResponse.TravelTime).
func (s *Server) travels(reg *controlRegistration, req ControlRequest) bool {
	p := reg.handler.Process
	if p == nil || p.TravelTime <= 0 || reg.handler.OnOperate != nil || req.CtlVal == nil {
		return false
	}
	if _, ok := req.CtlVal.Bool(); !ok {
		return false
	}
	st := s.findAttr(reg, "ST", "stVal")
	return st != nil && st.BType == "Dbpos"
}

// completeTravel waits for the device to reach the commanded position,
// then writes it into stVal with t and counts the operation. A context
// that ends first leaves stVal intermediate-state.
func (s *Server) completeTravel(ctx context.Context, reg *controlRegistration, req ControlRequest) error {
	t := time.NewTimer(reg.handler.Process.TravelTime)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	on, _ := req.CtlVal.Bool()
	wctx := context.WithoutCancel(ctx)
	s.controlSetValue(wctx, servermodel.StoreKey(reg.ld, reg.ln+"$ST$"+reg.doPath+"$stVal"), DbposValue(on))
	if s.hasST(reg, "t") {
		s.controlSetValue(wctx, servermodel.StoreKey(reg.ld, reg.ln+"$ST$"+reg.doPath+"$t"), stampUTC(s.clock))
	}
	s.countOperation(wctx, reg)
	return nil
}

// countOperation increments the operation counter of the registration's
// ProcessResponse, saturating at the INT32 maximum.
func (s *Server) countOperation(ctx context.Context, reg *controlRegistration) {
	key := reg.checks.opCnt
	if key == "" || s.store == nil {
		return
	}
	reg.stMu.Lock()
	defer reg.stMu.Unlock()
	var n int64
	if v := s.store.Get(key); v != nil {
		n, _ = v.Int64()
	}
	if n < math.MaxInt32 {
		n++
	}
	s.controlSetValue(ctx, key, mms.NewInteger(n))
}

// executeFn is what runs in the execution phase of an operation: the
// travel of the ProcessResponse, then OnExecute; nil when neither.
func (s *Server) executeFn(reg *controlRegistration, req ControlRequest) func(context.Context, ControlRequest) error {
	travel := s.travels(reg, req)
	execute := reg.handler.OnExecute
	switch {
	case !travel:
		return execute
	case execute == nil:
		return func(ctx context.Context, r ControlRequest) error { return s.completeTravel(ctx, reg, r) }
	default:
		return func(ctx context.Context, r ControlRequest) error {
			if err := s.completeTravel(ctx, reg, r); err != nil {
				return err
			}
			return execute(ctx, r)
		}
	}
}
