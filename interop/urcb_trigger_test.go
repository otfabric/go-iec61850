//go:build interop

// SPDX-License-Identifier: MIT

// Phase E3 — TrgOps option tests.
//
// These tests verify the trigger-option semantics of the go-iec61850 server's
// URCB report engine:
//
//   - The trigger options refer to the TrgOp of the data attribute in its CDC
//     (IEC 61850-7-2 §17.2.2.11): the fixture's SPS.stVal has dchg only, so
//     TrgOpDataUpdate and TrgOpQualityChanged produce no report for it.
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

// TestGoServer_URCB_TrgOp_DataUpdate verifies that TrgOpDataUpdate does not
// report a write of SPS1.stVal: "dchg, qchg, dupd ... refer to the TrgOp of
// the DataAttribute" (IEC 61850-7-2 §17.2.2.11), and SPS.stVal has the
// TrgOp dchg, not dupd (IEC 61850-7-3; fixture DOType SPS_Type).
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

	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if err := c.Write(ctx, ref, mms.NewBoolean(!fixVal.SPS1StVal)); err != nil {
		t.Fatalf("Write SPS1.stVal: %v", err)
	}

	select {
	case rpt := <-sub.Reports():
		t.Errorf("unexpected report for an attribute without TrgOp dupd: SeqNum=%d ReasonCodes=%v", rpt.SeqNum, rpt.ReasonCodes)
	case <-time.After(1 * time.Second):
	}
}

// TestGoServer_URCB_TrgOp_QualityChange verifies that TrgOpQualityChanged
// does not report a data change of SPS1.stVal: its TrgOp is dchg
// (IEC 61850-7-2 §17.2.2.11); the quality q is a separate attribute and
// not a member of dsInterop.
func TestGoServer_URCB_TrgOp_QualityChange(t *testing.T) {
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

	h.srv.SetValue(context.Background(), goLD+"/GGIO1$ST$SPS1$stVal", mms.NewBoolean(!fixVal.SPS1StVal))
	h.srv.SetValue(context.Background(), goLD+"/GGIO1$ST$SPS1$q", iec61850.EncodeQuality(iec61850.QualityOldData))

	select {
	case rpt := <-sub.Reports():
		t.Errorf("unexpected report: SeqNum=%d ReasonCodes=%v", rpt.SeqNum, rpt.ReasonCodes)
	case <-time.After(1 * time.Second):
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
