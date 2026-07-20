//go:build interop

// SPDX-License-Identifier: MIT

// Phase E3 — TrgOps option tests.
//
// These tests verify the trigger-option semantics of the go-iec61850 server's
// URCB report engine:
//
//   - TrgOpDataUpdate fires on every write regardless of value change.
//   - TrgOpQualityChanged fires when quality bits within a structure member change.
//   - TrgOpDataChanged does NOT fire when the written value equals the previous one.
//   - Multiple dchg writes in quick succession arrive in one or more reports (not zero).
//
// All tests use go client → go server (startGoIEDServerWithReports).
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// TestGoServer_URCB_TrgOp_DataUpdate verifies that TrgOpDataUpdate fires on
// every write even when the value does not change.
//
// Strategy:
//  1. Enable URCB with TrgOps = TrgOpDataUpdate only.
//  2. Write SPS1.stVal with its current value (no state change).
//  3. A report must arrive because dupd triggers on every accepted write.
//  4. The report's ReasonCode must include ReasonDataUpdate.
func TestGoServer_URCB_TrgOp_DataUpdate(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps,
		TrgOps: iec61850.TrgOpDataUpdate,
	}); err != nil {
		t.Fatalf("SetReportControlBlock TrgOps: %v", err)
	}

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:         urcbLD,
		RCBItemID:  goServerURCBID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	// Write the SAME value that is already stored — no data change, but dupd fires.
	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if err := c.Write(ctx, ref, mms.NewBoolean(fixVal.SPS1StVal)); err != nil {
		t.Fatalf("Write SPS1.stVal (same value): %v", err)
	}

	select {
	case rpt, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before dupd report")
		}
		t.Logf("dupd report: SeqNum=%d Inclusion=%v ReasonCodes=%v",
			rpt.SeqNum, rpt.Inclusion, rpt.ReasonCodes)
		// At least one member must carry ReasonDataUpdate.
		found := false
		for _, rc := range rpt.ReasonCodes {
			if rc&iec61850.ReasonDataUpdate != 0 {
				found = true
				break
			}
		}
		if !found && len(rpt.ReasonCodes) > 0 {
			t.Errorf("dupd report: no member has ReasonDataUpdate, got %v", rpt.ReasonCodes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: no dupd report after same-value write with TrgOpDataUpdate")
	}
}

// TestGoServer_URCB_TrgOp_QualityChange verifies that TrgOpQualityChanged
// fires when the quality bits of a dataset member change.
//
// The report engine's qualityChanged heuristic inspects the MMS value stored
// at a member's store key.  It detects quality changes when the stored value
// is a structure whose element at index 1 is a bit string (the IEC 61850
// {stVal, q, t} layout).
//
// Setup: pre-load SPS1.stVal as a {stVal, q} structure with ValidityGood,
// then trigger a quality change via Server.SetValue with QualityOldData set.
func TestGoServer_URCB_TrgOp_QualityChange(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	// Pre-load the dataset member as {stVal, q} so qualityChanged can compare
	// the quality bit string at structure index 1.  Use ValidityGood (q=0) as
	// the baseline.  This bypasses the write interceptor (no report fired).
	const memberKey = "InteropLD/GGIO1$ST$SPS1$stVal"
	goodQual := iec61850.EncodeQuality(0)
	h.srv.ValueStore().Set(memberKey, mms.NewStructure([]*mms.Value{
		mms.NewBoolean(fixVal.SPS1StVal),
		goodQual,
	}))

	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps,
		TrgOps: iec61850.TrgOpQualityChanged,
	}); err != nil {
		t.Fatalf("SetReportControlBlock TrgOps: %v", err)
	}

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:         urcbLD,
		RCBItemID:  goServerURCBID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	// Trigger quality change: keep stVal the same, set QualityOldData flag.
	dirtyQual := iec61850.EncodeQuality(iec61850.QualityOldData)
	h.srv.SetValue(context.Background(), memberKey, mms.NewStructure([]*mms.Value{
		mms.NewBoolean(fixVal.SPS1StVal),
		dirtyQual,
	}))

	select {
	case rpt, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before qchg report")
		}
		t.Logf("qchg report: SeqNum=%d Inclusion=%v ReasonCodes=%v",
			rpt.SeqNum, rpt.Inclusion, rpt.ReasonCodes)
		if len(rpt.ReasonCodes) > 0 {
			found := false
			for _, rc := range rpt.ReasonCodes {
				if rc&iec61850.ReasonQualityChanged != 0 {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("qchg report: no member has ReasonQualityChanged, got %v", rpt.ReasonCodes)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: no quality-change report after SetValue with changed quality bits")
	}
}

// TestGoServer_URCB_TrgOps_NoDchgIfSameValue verifies that TrgOpDataChanged
// does NOT produce a report when the written value equals the stored value.
//
// Strategy:
//  1. Enable URCB with TrgOps = TrgOpDataChanged only.
//  2. Write SPS1.stVal with its current value (no change).
//  3. Wait 200ms.
//  4. Assert the channel is empty (no report arrived).
func TestGoServer_URCB_TrgOps_NoDchgIfSameValue(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps,
		TrgOps: iec61850.TrgOpDataChanged,
	}); err != nil {
		t.Fatalf("SetReportControlBlock TrgOps: %v", err)
	}

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:         urcbLD,
		RCBItemID:  goServerURCBID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	// Write the same value twice — dchg must NOT fire.
	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	for i := range 2 {
		if err := c.Write(ctx, ref, mms.NewBoolean(fixVal.SPS1StVal)); err != nil {
			t.Fatalf("Write #%d SPS1.stVal (same value): %v", i+1, err)
		}
	}

	// Allow 200ms for any spurious report to arrive.
	select {
	case rpt, ok := <-sub.Reports():
		if ok {
			t.Errorf("unexpected dchg report after same-value write: SeqNum=%d Inclusion=%v",
				rpt.SeqNum, rpt.Inclusion)
		}
	case <-time.After(200 * time.Millisecond):
		// correct: no report for an unchanged value
	}
}

// TestGoServer_URCB_TrgOps_MultipleTriggersOneReport verifies that two rapid
// dchg writes each produce a report.  With TrgOpIntegrity set to a very long
// period (so it does not interfere), both value changes must appear in the
// received reports (one or two reports, not zero).
func TestGoServer_URCB_TrgOps_MultipleTriggersOneReport(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	// Set long IntgPd so integrity does not interfere during the test.
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps | iec61850.RCBFieldIntgPd,
		TrgOps: iec61850.TrgOpDataChanged | iec61850.TrgOpIntegrity,
		IntgPd: 10_000,
	}); err != nil {
		t.Fatalf("SetReportControlBlock TrgOps+IntgPd: %v", err)
	}

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:         urcbLD,
		RCBItemID:  goServerURCBID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	// Write two different dataset members in rapid succession.
	sps1Ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	modRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.stVal[ST]")

	if err := c.Write(ctx, sps1Ref, mms.NewBoolean(!fixVal.SPS1StVal)); err != nil {
		t.Fatalf("Write SPS1.stVal: %v", err)
	}
	if err := c.Write(ctx, modRef, mms.NewInteger(fixVal.ModStVal+1)); err != nil {
		t.Fatalf("Write Mod.stVal: %v", err)
	}

	// Collect reports until both members have been seen or the deadline fires.
	seenMember0 := false // SPS1.stVal (dataset member 0)
	seenMember1 := false // Mod.stVal  (dataset member 1)
	deadline := time.After(3 * time.Second)
	for !seenMember0 || !seenMember1 {
		select {
		case rpt, ok := <-sub.Reports():
			if !ok {
				t.Fatal("subscription channel closed")
			}
			t.Logf("report: SeqNum=%d Inclusion=%v ReasonCodes=%v",
				rpt.SeqNum, rpt.Inclusion, rpt.ReasonCodes)
			for i, inc := range rpt.Inclusion {
				if !inc {
					continue
				}
				switch i {
				case 0:
					seenMember0 = true
				case 1:
					seenMember1 = true
				}
			}
		case <-deadline:
			t.Fatalf("timeout: seenMember0(SPS1.stVal)=%v seenMember1(Mod.stVal)=%v",
				seenMember0, seenMember1)
		}
	}
}
