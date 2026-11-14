// SPDX-License-Identifier: MIT

package iec61850

import (
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func testOper(withOperTm bool, ctlNum *mms.Value) *mms.Value {
	m := []*mms.Value{mms.NewBoolean(true)}
	if withOperTm {
		m = append(m, mms.NewUTCTime(time.Unix(0, 0).UTC()))
	}
	m = append(m, mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString([]byte{1})}), ctlNum,
		mms.NewUTCTime(time.Unix(0, 0).UTC()), mms.NewBoolean(false), mms.NewBitStringWithLength([]byte{0}, 2))
	return mms.NewStructure(m)
}

func TestOperCtlNum(t *testing.T) {
	for name, tc := range map[string]struct {
		oper *mms.Value
		want uint8
		ok   bool
	}{
		"without operTm":   {testOper(false, mms.NewUnsigned(7)), 7, true},
		"with operTm":      {testOper(true, mms.NewUnsigned(9)), 9, true},
		"nil":              {nil, 0, false},
		"not a structure":  {mms.NewBoolean(true), 0, false},
		"too short":        {mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewBoolean(true)}), 0, false},
		"nil member":       {mms.NewStructure([]*mms.Value{mms.NewBoolean(true), nil, mms.NewUnsigned(1)}), 0, false},
		"no origin":        {mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewBoolean(true), mms.NewUnsigned(1)}), 0, false},
		"ctlNum signed":    {testOper(false, mms.NewInteger(1)), 0, false},
		"ctlNum too large": {testOper(false, mms.NewUnsigned(256)), 0, false},
		"origin last": {mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewUTCTime(time.Unix(0, 0)),
			mms.NewStructure(nil)}), 0, false},
	} {
		got, ok := operCtlNum(tc.oper)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: operCtlNum = %d, %v; want %d, %v", name, got, ok, tc.want, tc.ok)
		}
	}
}

// IEC 61850-8-1 20.9.2: with two OperateWait of one control in flight, a
// CommandTermination goes to the waiter whose operate carried its
// ctlNum; one without a known ctlNum goes to the oldest waiter.
func TestHandleControlReport_TerminationByCtlNum(t *testing.T) {
	c := &Client{logger: discardLogger()}
	oper := mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "GGIO1$CO$SPCSO1$Oper"}
	key := controlKey("LD", "GGIO1$CO$SPCSO1$Oper")
	first, _ := c.addTerminationWaiter(key, "LD/GGIO1.SPCSO1", 1)
	second, _ := c.addTerminationWaiter(key, "LD/GGIO1.SPCSO1", 2)
	third, _ := c.addTerminationWaiter(key, "LD/GGIO1.SPCSO1", 3)

	c.handleControlReport(&mms.InformationReportIndication{
		Variables: []mms.ObjectName{oper},
		Values:    []*mms.Value{testOper(false, mms.NewUnsigned(2))},
	})
	if len(second.ch) != 1 || len(first.ch) != 0 || len(third.ch) != 0 {
		t.Fatalf("ctlNum 2 served first=%d second=%d third=%d", len(first.ch), len(second.ch), len(third.ch))
	}
	// An unknown ctlNum and an Oper without one go to the oldest waiter.
	c.handleControlReport(&mms.InformationReportIndication{
		Variables: []mms.ObjectName{oper},
		Values:    []*mms.Value{testOper(false, mms.NewUnsigned(42))},
	})
	if len(first.ch) != 1 || len(third.ch) != 0 {
		t.Fatalf("unknown ctlNum served first=%d third=%d", len(first.ch), len(third.ch))
	}
	c.handleControlReport(&mms.InformationReportIndication{
		Variables: []mms.ObjectName{oper},
		Values:    []*mms.Value{mms.NewBoolean(true)},
	})
	if len(third.ch) != 1 {
		t.Fatal("Oper without ctlNum not served to the remaining waiter")
	}
	if len(c.ctlWaiters) != 0 {
		t.Errorf("waiters left: %v", c.ctlWaiters)
	}
}
