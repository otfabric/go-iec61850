//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover the control model's InformationReports in the
// go client → peer server direction (IEC 61850-8-1): the CommandTermination
// after an enhanced-security operate, and the LastApplError a server sends
// before refusing a control write.

package interop

import (
	"context"
	"errors"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// TestLibIECClient_Control_SBOwTermination: SelectWithValue then
// OperateWait on SPCSO3 (SBO with enhanced security); the peer ends the
// operate with a positive CommandTermination.
func TestLibIECClient_Control_SBOwTermination(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := h.dial(t, ctx)
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	params := iec61850.OperateParams{CtlVal: iec61850.BoolCtlVal(true), CtlNum: 21}
	if err := client.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}
	wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
	defer wcancel()
	term, err := client.OperateWait(wctx, ref, params)
	if err != nil {
		t.Fatalf("OperateWait: %v", err)
	}
	if !term.Positive() || term.Oper == nil {
		t.Fatalf("CommandTermination = %+v", term)
	}
	oper, ok := term.Oper.Structure()
	if !ok || len(oper) == 0 {
		t.Fatalf("Oper = %v", term.Oper)
	}
	if b, _ := oper[0].Bool(); !b {
		t.Errorf("Oper.ctlVal = %v, want true", oper[0])
	}
}

// TestLibIECClient_Control_OperateNotSelected: an operate of SPCSO3
// without a select is refused; the peer reports why in a LastApplError
// (AddCause object-not-selected) that the ControlError carries.
func TestLibIECClient_Control_OperateNotSelected(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := h.dial(t, ctx)
	defer client.Close(ctx) //nolint:errcheck

	ref, err := iec61850.ParseRef(sbow3Ref)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	err = client.Operate(ctx, ref, iec61850.OperateParams{CtlVal: iec61850.BoolCtlVal(true), CtlNum: 22})
	var ce *iec61850.ControlError
	if !errors.As(err, &ce) {
		t.Fatalf("Operate without select = %v, want a ControlError", err)
	}
	t.Logf("refused: %v", err)
	if ce.LastApplError == nil {
		if peer().name == "iec61850-rust" {
			// Observed 2026-11-09 on the pinned image: the write is refused
			// (ObjectAccessDenied) with no LastApplError report.
			t.Skipf("iec61850-rust sends no LastApplError with a refused operate: %v", err)
		}
		t.Fatalf("no LastApplError with the refusal: %v", err)
	}
	lae := ce.LastApplError
	t.Logf("LastApplError: %+v", *lae)
	if lae.AddCause != iec61850.AddCauseObjectNotSelected || ce.AddCause != lae.AddCause {
		t.Errorf("AddCause = %s, want object-not-selected", lae.AddCause)
	}
	if lae.CtlNum != 22 {
		t.Errorf("CtlNum = %d, want 22", lae.CtlNum)
	}
	if want := "$CO$SPCSO3$Oper"; len(lae.CntrlObj) < len(want) || lae.CntrlObj[len(lae.CntrlObj)-len(want):] != want {
		t.Errorf("CntrlObj = %q", lae.CntrlObj)
	}
}
