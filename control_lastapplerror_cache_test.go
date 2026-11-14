// SPDX-License-Identifier: MIT

package iec61850

import (
	"fmt"
	"testing"

	"github.com/otfabric/go-mms"
)

// laeReport is the InformationReport of a LastApplError for cntrlObj.
func laeReport(cntrlObj string, ctlNum uint8, cause AddCause) *mms.InformationReportIndication {
	return &mms.InformationReportIndication{
		Variables: []mms.ObjectName{{Scope: mms.ObjectScopeVMD, ItemID: "LastApplError"}},
		Values:    []*mms.Value{laeValue(cntrlObj, mms.NewUnsigned(uint64(ctlNum)), int64(cause))},
	}
}

// The client keeps one LastApplError per control it addressed. A
// LastApplError that names another control is handled (it is a control
// report) and not kept, so the number of entries is the number of
// addressed controls whatever the number of distinct CntrlObj received.
// What ReadLastApplError returns for an addressed control is unchanged.
func TestLastApplErrorCache_OnlyAddressedControls(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	lnRef := Ref{LD: ref.LD, LN: ref.LN}

	// The client addresses one control: an Operate without a selection is
	// refused and the server reports the LastApplError.
	refusal(t, "Oper unselected", c.Operate(ctx, ref, boolParams(5)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
	if lae, err := c.ReadLastApplError(ctx, ref); err != nil || lae == nil || lae.CtlNum != 5 || lae.AddCause != AddCauseObjectNotSelected {
		t.Fatalf("ReadLastApplError of the addressed control = %+v, %v", lae, err)
	}

	for _, n := range []int{0, 1, 2, 1000} {
		t.Run(fmt.Sprintf("%d other controls", n), func(t *testing.T) {
			for i := range n {
				obj := fmt.Sprintf("%s/GGIO1$CO$Other%d$Oper", ctlLD, i)
				if !c.handleControlReport(laeReport(obj, 1, AddCauseBlockedByProcess)) {
					t.Fatalf("LastApplError for %s not handled as a control report", obj)
				}
			}
			c.ctlMu.Lock()
			entries := len(c.ctlLAE)
			c.ctlMu.Unlock()
			if entries != 1 {
				t.Errorf("%d cache entries after %d LastApplError for controls not addressed, want 1", entries, n)
			}
			// The addressed control's entry is still the one received for it,
			// also for the LN-level query.
			for _, r := range []Ref{ref, lnRef} {
				if lae, err := c.ReadLastApplError(ctx, r); err != nil || lae == nil || lae.CtlNum != 5 {
					t.Errorf("ReadLastApplError(%s) = %+v, %v; want the addressed control's, ctlNum 5", r, lae, err)
				}
			}
			if lae := c.cachedLastApplError(Ref{LD: ref.LD, LN: ref.LN, Path: []string{"Other0"}}); lae != nil {
				t.Errorf("a control that was not addressed has a cached LastApplError: %+v", lae)
			}
		})
	}

	// A later LastApplError for the addressed control replaces its entry.
	c.handleControlReport(laeReport(ctlLD+"/GGIO1$CO$SPCSO3$Oper", 6, AddCauseBlockedByInterlocking))
	if lae, err := c.ReadLastApplError(ctx, ref); err != nil || lae == nil || lae.CtlNum != 6 || lae.AddCause != AddCauseBlockedByInterlocking {
		t.Errorf("ReadLastApplError after a second LastApplError = %+v, %v; want ctlNum 6", lae, err)
	}
}

// Every control service addresses its control: Select (a Read of SBO),
// SelectWithValue, Operate, OperateWait and Cancel.
func TestLastApplErrorCache_ServicesAddressTheControl(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	ctx := waitCtx(t)
	sbo := mustParseRef(t, ctlSBO)
	sbow := mustParseRef(t, ctlSBOw)

	if _, err := c.Select(ctx, sbo); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if err := c.SelectWithValue(ctx, sbow, boolParams(1)); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}
	for i, tc := range []struct {
		ref Ref
		obj string
	}{
		{sbo, ctlLD + "/GGIO1$CO$SPCSO2$Oper"},
		{sbow, ctlLD + "/GGIO1$CO$SPCSO3$SBOw"},
	} {
		c.handleControlReport(laeReport(tc.obj, uint8(20+i), AddCauseBlockedByProcess))
		if lae := c.cachedLastApplError(tc.ref); lae == nil || lae.CtlNum != uint8(20+i) {
			t.Errorf("%s: cached LastApplError = %+v, want ctlNum %d", tc.ref, lae, 20+i)
		}
	}
}
