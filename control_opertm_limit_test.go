// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// An Operate whose operTm is further ahead than MaxOperDelay (24 hours by
// default) is refused with AddCause time-limit-over; up to the limit it is
// accepted. A refused operate leaves the control as it was: another
// operate is accepted at once. The times are the fake time of the
// synctest bubble, so the server sees the operTm exactly as sent (UTCTime
// keeps whole milliseconds here).
func TestControl_MaxOperDelay(t *testing.T) {
	cases := []struct {
		name     string
		maxDelay time.Duration
		ahead    time.Duration
		refused  bool
	}{
		{"default, 1 ms", 0, time.Millisecond, false},
		{"default, limit-1s", 0, DefaultMaxOperDelay - time.Second, false},
		{"default, limit", 0, DefaultMaxOperDelay, false},
		{"default, limit+1ms", 0, DefaultMaxOperDelay + time.Millisecond, true},
		{"default, a year", 0, 365 * 24 * time.Hour, true},
		{"1h, limit", time.Hour, time.Hour, false},
		{"1h, limit+1ms", time.Hour, time.Hour + time.Millisecond, true},
		{"1h, 59 min", time.Hour, 59 * time.Minute, false},
		{"negative is the default, limit", -1, DefaultMaxOperDelay, false},
		{"negative is the default, limit+1ms", -1, DefaultMaxOperDelay + time.Millisecond, true},
	}
	for _, model := range []CtlModel{CtlModelDirectNormal, CtlModelSBOEnhanced} {
		for _, tc := range cases {
			t.Run(model.String()+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					srv := specControlServer(t, specCF{}, func(s *Server) {
						mustRegister(t, s, "GGIO1.SPCTm", model, ControlHandler{MaxOperDelay: tc.maxDelay})
					})
					c := controlClient(t, srv)
					ctx := waitCtx(t)
					ref := mustParseRef(t, ctlTm)
					now := time.Now().Truncate(time.Second)
					time.Sleep(time.Until(now.Add(time.Second))) // fake time: a whole second
					params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1, OperTm: time.Now().Add(tc.ahead)}
					if model.IsSBO() {
						if err := c.SelectWithValue(ctx, ref, params); err != nil {
							t.Fatalf("SBOw: %v", err)
						}
					}
					err := c.Operate(ctx, ref, params)
					if !tc.refused {
						if err != nil {
							t.Fatalf("Operate with operTm %v ahead: %v", tc.ahead, err)
						}
						if ok, err := srv.CancelTimeActivatedOperate(ctlLD, "GGIO1.SPCTm"); !ok || err != nil {
							t.Fatalf("CancelTimeActivatedOperate = %v, %v; want the accepted operate waiting", ok, err)
						}
						return
					}
					refusal(t, "Operate beyond MaxOperDelay", err, AddCauseTimeLimit, mms.DataAccessErrorObjectAccessDenied)
					if ok, _ := srv.CancelTimeActivatedOperate(ctlLD, "GGIO1.SPCTm"); ok {
						t.Error("a refused operate waits for its operTm")
					}
					// The control is as before: an immediate operate (with
					// the selection, for SBOw) is accepted.
					now2 := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1}
					if err := c.Operate(ctx, ref, now2); err != nil {
						t.Errorf("Operate after the refusal: %v", err)
					}
				})
			})
		}
	}
}

// CancelTimeActivatedOperate ends a time activated operate that waits for
// operTm: it never operates, the goroutine that waited has ended, and the
// control is Unselected, so another client operates it at once. With no
// operate waiting it reports false; a control that is not registered is
// ErrNotFound.
func TestServer_CancelPendingActivation(t *testing.T) {
	for _, model := range []CtlModel{CtlModelDirectNormal, CtlModelSBOEnhanced} {
		t.Run(model.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
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
				a, b := controlClient(t, srv), controlClient(t, srv)
				ctx := waitCtx(t)
				ref := mustParseRef(t, ctlTm)

				if _, err := srv.CancelTimeActivatedOperate(ctlLD, "GGIO1.NoSuch"); !errors.Is(err, ErrNotFound) {
					t.Errorf("CancelTimeActivatedOperate of an unregistered control = %v, want ErrNotFound", err)
				}
				if ok, err := srv.CancelTimeActivatedOperate(ctlLD, "GGIO1.SPCTm"); ok || err != nil {
					t.Errorf("CancelTimeActivatedOperate with nothing waiting = %v, %v; want false, nil", ok, err)
				}

				operTm := time.Now().Add(time.Hour)
				pending := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1, OperTm: operTm}
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

				ok, err := srv.CancelTimeActivatedOperate(ctlLD, "GGIO1.SPCTm")
				if !ok || err != nil {
					t.Fatalf("CancelTimeActivatedOperate = %v, %v; want true, nil", ok, err)
				}
				synctest.Wait()
				// The goroutine that waited for operTm has ended.
				idle := make(chan struct{})
				go func() { srv.bg.wg.Wait(); close(idle) }()
				synctest.Wait()
				select {
				case <-idle:
				default:
					t.Fatal("the cancelled operate still waits for its operTm")
				}
				if ok, _ := srv.CancelTimeActivatedOperate(ctlLD, "GGIO1.SPCTm"); ok {
					t.Error("second CancelTimeActivatedOperate = true, want false")
				}

				if model.IsSBO() {
					stSeld := Ref{LD: ref.LD, LN: ref.LN, Path: []string{"SPCTm", "stSeld"}, FC: FCST}
					if v, err := b.ReadRaw(ctx, stSeld); err != nil {
						t.Errorf("read stSeld: %v", err)
					} else if sel, _ := v.Bool(); sel {
						t.Error("stSeld is TRUE after the cancel")
					}
					if err := b.SelectWithValue(ctx, ref, boolParams(2)); err != nil {
						t.Fatalf("B SBOw after the cancel: %v", err)
					}
				}
				if err := b.Operate(ctx, ref, boolParams(2)); err != nil {
					t.Fatalf("B Operate after the cancel: %v", err)
				}

				time.Sleep(time.Until(operTm) + time.Minute) // fake time: beyond A's operTm
				synctest.Wait()
				mu.Lock()
				defer mu.Unlock()
				if len(operated) != 1 || operated[0] != 2 {
					t.Errorf("operated %v, want only B's [2]", operated)
				}
			})
		})
	}
}
