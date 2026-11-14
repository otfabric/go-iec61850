// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// servedClient connects a new client to srv and returns it with a channel
// that is closed when the server's Serve of its association has returned.
func servedClient(t *testing.T, srv *Server) (*Client, <-chan struct{}) {
	t.Helper()
	sctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT, serverT := loopbackPair(t)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(sctx, serverT)
	}()
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()); _ = mc.Close(context.Background()) })
	return c, served
}

// A time activated operate that waits for operTm belongs to the
// association that sent it. When that association ends, the server aborts
// it and the control returns to Unselected: it never operates, another
// client can use the control at once, and the goroutine that waited for
// operTm has ended. IEC 61850-7-2 Table 47 defines this for a selection;
// for the pending activation it is this server's choice
// (KNOWN_LIMITATIONS.md).
func TestSpec_7_2_Table47_AssociationLossWhileWaitingForOperTm(t *testing.T) {
	for _, model := range []CtlModel{CtlModelDirectNormal, CtlModelSBOEnhanced} {
		t.Run(model.String(), func(t *testing.T) {
			var mu sync.Mutex
			var operated []uint8
			srv := specControlServer(t, specCF{}, func(s *Server) {
				mustRegister(t, s, "GGIO1.SPCTm", model, ControlHandler{OnOperate: func(_ context.Context, req ControlRequest) error {
					mu.Lock()
					defer mu.Unlock()
					operated = append(operated, req.CtlNum)
					return nil
				}})
			})
			a, aServed := servedClient(t, srv)
			b := controlClient(t, srv)
			ctx := waitCtx(t)
			ref := mustParseRef(t, ctlTm)

			// A schedules an operate for an operTm the test never reaches.
			pending := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1, OperTm: time.Now().Add(time.Hour)}
			if model.IsSBO() {
				if err := a.SelectWithValue(ctx, ref, pending); err != nil {
					t.Fatalf("A SBOw: %v", err)
				}
			}
			if err := a.Operate(ctx, ref, pending); err != nil {
				t.Fatalf("A time activated Operate: %v", err)
			}
			refusal(t, "B Oper while A's operate waits", b.Operate(ctx, ref, boolParams(2)),
				AddCauseCommandAlreadyExec, mms.DataAccessErrorTemporarilyUnavailable)

			// A's association ends; its Serve has returned.
			if err := a.mmsClient.Close(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-aServed:
			case <-ctx.Done():
				t.Fatal("Serve of A's association did not return")
			}

			// The goroutine that waited for operTm has ended: nothing else
			// runs in the background of this server now.
			idle := make(chan struct{})
			go func() {
				srv.bg.wg.Wait()
				close(idle)
			}()
			select {
			case <-idle:
			case <-ctx.Done():
				t.Fatal("the pending time activated operate is still waiting after its association ended")
			}

			// The control is Unselected: B operates it at once.
			now := OperateParams{CtlVal: BoolCtlVal(false), CtlNum: 2}
			if model.IsSBO() {
				stSeld := Ref{LD: ref.LD, LN: ref.LN, Path: []string{"SPCTm", "stSeld"}, FC: FCST}
				if v, err := b.ReadRaw(ctx, stSeld); err != nil {
					t.Errorf("read stSeld: %v", err)
				} else if sel, _ := v.Bool(); sel {
					t.Error("stSeld is TRUE after A's association ended")
				}
				if err := b.SelectWithValue(ctx, ref, now); err != nil {
					t.Fatalf("B SBOw after A's association ended: %v", err)
				}
				if term, err := b.OperateWait(ctx, ref, now); err != nil || !term.Positive() {
					t.Fatalf("B OperateWait after A's association ended = %+v, %v", term, err)
				}
			} else if err := b.Operate(ctx, ref, now); err != nil {
				t.Fatalf("B Operate after A's association ended: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(operated) != 1 || operated[0] != 2 {
				t.Errorf("operated ctlNums = %v, want only B's 2", operated)
			}
		})
	}
}

// The release at the next control service (a connection given to
// mms.Server directly, whose end the Server does not see): a time
// activated operate whose association is gone is aborted when another
// service addresses the control.
func TestControl_ExpireReleasesPendingOperTmOfLostAssociation(t *testing.T) {
	srv := specControlServer(t, specCF{}, func(s *Server) {
		mustRegister(t, s, "GGIO1.SPCTm", CtlModelDirectNormal, ControlHandler{})
	})
	reg := srv.controls[ctlLD+"/GGIO1.SPCTm"]
	gone := &mms.ServerConn{} // not among the server's connections
	abort := make(chan struct{})

	reg.mu.Lock()
	reg.state = ctlWaitActivation
	reg.sel = controlSelection{conn: gone}
	reg.abort = abort
	released := srv.expireLocked(reg, time.Now())
	state, pending := reg.state, reg.abort
	reg.mu.Unlock()

	if !released || state != ctlUnselected || pending != nil {
		t.Errorf("expireLocked = %v, state %v, abort %v; want released, Unselected, nil", released, state, pending)
	}
	select {
	case <-abort:
	default:
		t.Error("the abort channel of the pending operate is not closed")
	}

	// A control in execution is not released by the loss of its association.
	reg.mu.Lock()
	reg.state = ctlExecuting
	reg.sel = controlSelection{conn: gone}
	released = srv.expireLocked(reg, time.Now())
	state = reg.state
	reg.mu.Unlock()
	if released || state != ctlExecuting {
		t.Errorf("executing: expireLocked = %v, state %v; want not released, Executing", released, state)
	}
}
