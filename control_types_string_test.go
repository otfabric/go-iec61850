// SPDX-License-Identifier: MIT

package iec61850

import "testing"

func TestOrCatAndAddCauseString(t *testing.T) {
	orCats := []OrCat{
		OrCatNotSupported, OrCatBayControl, OrCatStationControl, OrCatRemoteControl,
		OrCatAutomaticBay, OrCatAutomaticStation, OrCatAutomaticRemote,
		OrCatMaintenance, OrCatProcess, OrCat(99),
	}
	for _, c := range orCats {
		if c.String() == "" {
			t.Fatalf("empty string for %d", c)
		}
	}
	if OrCat(99).String() != "orCat(99)" {
		t.Fatalf("%q", OrCat(99).String())
	}

	causes := []AddCause{
		AddCauseUnknown, AddCauseNotSupported, AddCauseBlocked, AddCauseSelectFailed,
		AddCauseInvalidPosition, AddCausePositionReached, AddCauseParameterChange,
		AddCauseStepLimit, AddCauseBlockedBySwitch, AddCauseBlockedByInterlocking,
		AddCauseBlockedBySynchrocheck, AddCauseCommandAlreadyExec, AddCauseBlockedByHealth,
		AddCause1of1, AddCauseAbort, AddCauseTimeLimit, AddCauseBlockedByMode,
		AddCauseBlockedByProcess, AddCause(99),
	}
	for _, c := range causes {
		if c.String() == "" {
			t.Fatalf("empty string for %d", c)
		}
	}
	if got := AddCause(99).String(); got != "addCause(99)" {
		t.Fatalf("%q", got)
	}
}
