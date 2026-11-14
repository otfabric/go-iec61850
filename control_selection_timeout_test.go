// SPDX-License-Identifier: MIT

package iec61850

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// IEC 61850-7-2 20.2.2 a, Table 47: the select timeout of a selection is
// the one in force when it was made. A CF sboTimeout write by another
// association (CF stays writable) applies to later selections only, so it
// neither ends the current selection early nor lets that association
// select over it.
func TestSpec_7_2_SelectionTimeoutFixedAtSelect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := waitCtx(t)
		srv := specControlServer(t, specCF{sboTimeoutMs: 30000}, func(s *Server) {
			mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{})
		})
		a, b := controlClient(t, srv), controlClient(t, srv)
		ref := mustParseRef(t, ctlTm)
		cf := Ref{LD: ref.LD, LN: ref.LN, Path: []string{"SPCTm", "sboTimeout"}, FC: FCCF}

		if err := a.SelectWithValue(ctx, ref, boolParams(1)); err != nil {
			t.Fatal(err)
		}
		if err := b.Write(ctx, cf, mms.NewUnsigned(1)); err != nil {
			t.Fatalf("B write CF sboTimeout: %v", err)
		}
		time.Sleep(50 * time.Millisecond) // fake time: beyond the new 1 ms
		synctest.Wait()

		refusal(t, "B SBOw", b.SelectWithValue(ctx, ref, boolParams(2)), AddCauseObjectAlreadySelected, mms.DataAccessErrorTemporarilyUnavailable)
		if term, err := a.OperateWait(ctx, ref, boolParams(1)); err != nil || !term.Positive() {
			t.Fatalf("A OperateWait = %+v, %v", term, err)
		}

		// The written value governs the next selection.
		if err := b.SelectWithValue(ctx, ref, boolParams(2)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		refusal(t, "B Oper after the new sboTimeout", b.Operate(ctx, ref, boolParams(2)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
	})
}
