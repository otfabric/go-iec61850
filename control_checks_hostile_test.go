// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// TestChaos_ControlChecksHostileOper: a client writes seeded malformed
// Oper structures to a direct-control DPC whose interlocking and
// synchrocheck always refuse (CheckInterlock, CheckSynchro, and a CILO
// that enables nothing): Check bit strings of every length with every
// bit pattern, Check of other types, members of the wrong type, missing
// and extra members, and ctlVal, origin, ctlNum and T of other types.
//
// Invariants (IEC 61850-7-2 20.2.1, 20.5.2.7, Table 54; 8-1 Table 85):
// no operation runs with a check bit set, so a malformed Check cannot
// bypass the checks; every refusal is a DataAccessError or a control
// refusal with an AddCause; the association keeps working, and a correct
// Oper without checks still operates.
func TestChaos_ControlChecksHostileOper(t *testing.T) {
	var executed, withChecks atomic.Int32
	srv := checksServer(t, checksCF{}, CtlModelDirectNormal, ControlHandler{
		Interlocking:   "CILO1",
		Synchrocheck:   "RSYN1",
		SynchroTimeout: 5 * time.Millisecond,
		CheckInterlock: func(context.Context, ControlRequest) error { return errors.New("blocked") },
		CheckSynchro:   func(context.Context, ControlRequest) error { return errors.New("not in sync") },
		OnOperate: func(_ context.Context, req ControlRequest) error {
			executed.Add(1)
			if req.Check != 0 {
				withChecks.Add(1)
			}
			return nil
		},
	})
	setStVal(srv, "CILO1.EnaOpn", mms.NewBoolean(false))
	setStVal(srv, "CILO1.EnaCls", mms.NewBoolean(false))
	setStVal(srv, "RSYN1.Rel", mms.NewBoolean(false))
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ld, item := mms.DomainID(ctlLD), mms.ItemID("XCBR1$CO$Pos$Oper")

	hostile := func(r *rand.Rand, n uint8) *mms.Value {
		p := posParams(n, r.IntN(2) == 0, CheckConditions(r.IntN(4)))
		m, _ := encodeOper(p, true).Structure()
		m = append([]*mms.Value(nil), m...)
		// Check (the last member): any length, any pattern, or another type.
		switch r.IntN(4) {
		case 0:
			bits := r.IntN(17)
			b := make([]byte, (bits+7)/8)
			for i := range b {
				b[i] = byte(r.IntN(256)) // padding bits included
			}
			m[len(m)-1] = mms.NewBitStringWithLength(b, bits)
		case 1:
			m[len(m)-1] = []*mms.Value{mms.NewInteger(3), mms.NewBoolean(true), mms.NewOctetString([]byte{0xc0}), mms.NewStructure(nil)}[r.IntN(4)]
		}
		// Another member of the wrong type.
		if r.IntN(3) == 0 {
			m[r.IntN(len(m)-1)] = []*mms.Value{mms.NewInteger(-1), mms.NewVisibleString("x"), mms.NewBitStringWithLength([]byte{0xff}, 8), mms.NewUnsigned(300)}[r.IntN(4)]
		}
		// A member missing or one too many.
		switch r.IntN(6) {
		case 0:
			i := r.IntN(len(m))
			m = append(m[:i], m[i+1:]...)
		case 1:
			m = append(m, mms.NewBoolean(true))
		}
		return mms.NewStructure(m)
	}

	r := rand.New(rand.NewPCG(7, 61850))
	for i := range 400 {
		v := hostile(r, uint8(i))
		_, err := c.mmsClient.Write(ctx, mms.WriteRequest{DomainID: ld, ItemID: item, Value: v})
		if err == nil {
			continue
		}
		var dae *mms.DataAccessError
		if !errors.As(err, &dae) {
			t.Fatalf("write %d (%v): %v is not a data access error", i, v, err)
		}
	}
	if n := withChecks.Load(); n != 0 {
		t.Fatalf("%d operations ran with a check bit set although every check refuses", n)
	}
	before := executed.Load()
	if err := c.Operate(ctx, mustParseRef(t, ctlPos), posParams(250, true, 0)); err != nil {
		t.Fatalf("correct Oper after the hostile ones: %v", err)
	}
	if executed.Load() != before+1 {
		t.Fatal("correct Oper did not operate")
	}
	var ce *ControlError
	if err := c.Operate(ctx, mustParseRef(t, ctlPos), posParams(251, true, CheckInterlockCheck)); !errors.As(err, &ce) || ce.AddCause != AddCauseBlockedByInterlocking {
		t.Fatalf("Oper with interlock-check: %v, want blocked-by-interlocking", err)
	}
}
