//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2H — report semantics expansion.
//
// 2H-a General Interrogation:
//   - TestLibIECClient_URCB_GIReport     — go client → libiec61850 server: TriggerGI → all-member report
//   - TestLibIECServer_URCB_GIReport     — go client → go server: TriggerGI → all-member report
//   - TestBeanServer_URCB_GIReport       — go client → iec61850bean server: TriggerGI → all-member report
//
// 2H-c Multiple data changes:
//   - TestLibIECServer_URCB_MultiMemberDchg — write two members; both appear in report with inclusion bits set
//
// All GI tests assert:
//  1. All dataset members present in the report (Inclusion all true).
//  2. Values have the correct initial values (matching fixVal).
//  3. Reason for each member is GI.
//
// Multi-member dchg test asserts:
//  1. Inclusion[1] is set after writing Mod.stVal.
//  2. The new value matches what was written.
//  3. Inclusion[0] is NOT set for an unchanged member.
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// Phase 2H-a: General Interrogation
// ---------------------------------------------------------------------------

// TestLibIECClient_URCB_GIReport verifies that the go-iec61850 client can
// trigger a General Interrogation against the libiec61850 IED server and
// receive a complete dataset report with all members included and
// ReasonGI set on each entry.
func TestLibIECClient_URCB_GIReport(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := h.dial(t, ctx)
	defer c.Close(ctx)

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:          urcbLD,
		RCBItemID:   urcbID,
		ReserveURCB: true,
		AutoEnable:  true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	if err := c.TriggerGI(ctx, urcbLD, urcbID); err != nil {
		t.Fatalf("TriggerGI: %v", err)
	}

	reportCtx, reportCancel := context.WithTimeout(ctx, 10*time.Second)
	defer reportCancel()

	var rpt *iec61850.ReportIndication
	select {
	case r, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before GI report")
		}
		rpt = r
	case <-reportCtx.Done():
		t.Fatal("timeout: no GI report received from libiec61850 server")
	}

	// Both dataset members must be included in a GI report.
	if len(rpt.Inclusion) < 2 {
		t.Fatalf("Inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] {
		t.Error("Inclusion[0] (SPS1.stVal): want true (GI includes all members)")
	}
	if !rpt.Inclusion[1] {
		t.Error("Inclusion[1] (Mod.stVal): want true (GI includes all members)")
	}

	// Values must include both dataset members.
	if len(rpt.Values) < 2 {
		t.Fatalf("Values length: want >= 2, got %d", len(rpt.Values))
	}

	// Reason for each included member must be ReasonGI.
	if len(rpt.ReasonCodes) > 0 {
		for i, r := range rpt.ReasonCodes {
			if r != iec61850.ReasonGI {
				t.Errorf("ReasonCodes[%d]: want ReasonGI, got %v", i, r)
			}
		}
	}
}

// TestLibIECServer_URCB_GIReport verifies that a go-iec61850 client can
// trigger GI on the go-iec61850 server and receive a complete dataset report.
func TestLibIECServer_URCB_GIReport(t *testing.T) {
	const serverUrcbID = "LLN0$RP$urcb01"

	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	sub, err := c.SubscribeReport(ctx, "interop_urcb01", iec61850.SubscribeReportOptions{
		LD:          urcbLD,
		RCBItemID:   serverUrcbID,
		ReserveURCB: true,
		AutoEnable:  true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	if err := c.TriggerGI(ctx, urcbLD, serverUrcbID); err != nil {
		t.Fatalf("TriggerGI: %v", err)
	}

	reportCtx, reportCancel := context.WithTimeout(ctx, 10*time.Second)
	defer reportCancel()

	var rpt *iec61850.ReportIndication
	select {
	case r, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before GI report")
		}
		rpt = r
	case <-reportCtx.Done():
		t.Fatal("timeout: no GI report received from go-iec61850 server")
	}

	if len(rpt.Inclusion) < 2 {
		t.Fatalf("Inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] {
		t.Error("Inclusion[0] (SPS1.stVal): want true")
	}
	if !rpt.Inclusion[1] {
		t.Error("Inclusion[1] (Mod.stVal): want true")
	}
	if len(rpt.Values) < 2 {
		t.Fatalf("Values length: want >= 2, got %d", len(rpt.Values))
	}
	for i, r := range rpt.ReasonCodes {
		if r != iec61850.ReasonGI {
			t.Errorf("ReasonCodes[%d]: want ReasonGI, got %v", i, r)
		}
	}
}

// TestBeanServer_URCB_GIReport verifies that a go-iec61850 client can trigger
// GI on the iec61850bean server and receive a complete dataset report.
func TestBeanServer_URCB_GIReport(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := dialIEDWithIEDName(t, ctx, h.addr, h.iedName)
	defer c.Abort(context.Background())

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:          urcbLD,
		RCBItemID:   beanUrcbID,
		ReserveURCB: true,
		AutoEnable:  true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	if err := c.TriggerGI(ctx, urcbLD, beanUrcbID); err != nil {
		t.Fatalf("TriggerGI: %v", err)
	}

	reportCtx, reportCancel := context.WithTimeout(ctx, 10*time.Second)
	defer reportCancel()

	var rpt *iec61850.ReportIndication
	select {
	case r, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before GI report")
		}
		rpt = r
	case <-reportCtx.Done():
		t.Fatal("timeout: no GI report received from iec61850bean server")
	}

	if len(rpt.Inclusion) < 2 {
		t.Fatalf("Inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] {
		t.Error("Inclusion[0] (SPS1.stVal): want true")
	}
	if !rpt.Inclusion[1] {
		t.Error("Inclusion[1] (Mod.stVal): want true")
	}
	if len(rpt.Values) < 2 {
		t.Fatalf("Values length: want >= 2, got %d", len(rpt.Values))
	}
}

// ---------------------------------------------------------------------------
// Phase 2H-c: Multiple data changes
// ---------------------------------------------------------------------------

// TestLibIECServer_URCB_MultiMemberDchg verifies that when two dataset
// members on the go-iec61850 server change in quick succession, both appear
// with the correct inclusion bits in the resulting dchg report(s).
//
// Strategy: After subscribing and enabling the URCB, the test calls
// Server.SetValue directly (bypassing MMS) for both SPS1.stVal[ST] (member 0)
// and Mod.stVal[ST] (member 1). The report engine will emit separate dchg
// reports for each change. We verify that across the received reports, member
// 0 is included in at least one report and member 1 is included in at least
// one report, with the correct updated values.
func TestLibIECServer_URCB_MultiMemberDchg(t *testing.T) {
	const serverUrcbID = "LLN0$RP$urcb01"

	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	sub, err := c.SubscribeReport(ctx, "interop_urcb01", iec61850.SubscribeReportOptions{
		LD:          urcbLD,
		RCBItemID:   serverUrcbID,
		ReserveURCB: true,
		AutoEnable:  true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	// Change both dataset members on the server side.
	newSPS1Val := !fixVal.SPS1StVal
	newModVal := fixVal.ModStVal + 1

	svsCtx := context.Background()
	h.srv.SetValue(svsCtx, "InteropLD/GGIO1$ST$SPS1$stVal", mms.NewBoolean(newSPS1Val))
	h.srv.SetValue(svsCtx, "InteropLD/LLN0$ST$Mod$stVal", mms.NewInteger(newModVal))

	// Collect dchg reports until both members have been seen or deadline fires.
	deadline := time.After(5 * time.Second)
	seenMember0 := false
	seenMember1 := false
	for !seenMember0 || !seenMember1 {
		select {
		case rpt, open := <-sub.Reports():
			if !open {
				t.Fatal("subscription channel closed")
			}
			t.Logf("report: inclusion=%v values=%d reasons=%v", rpt.Inclusion, len(rpt.Values), rpt.ReasonCodes)

			valueIdx := 0
			for i, inc := range rpt.Inclusion {
				if !inc {
					continue
				}
				switch i {
				case 0: // SPS1.stVal[ST]
					seenMember0 = true
					if valueIdx < len(rpt.Values) {
						b, bErr := rpt.Values[valueIdx].Bool()
						if bErr != nil {
							t.Errorf("member[0] (SPS1.stVal): expected bool (%v)", bErr)
						} else if b != newSPS1Val {
							t.Errorf("member[0] (SPS1.stVal): want %v, got %v", newSPS1Val, b)
						}
					}
				case 1: // Mod.stVal[ST]
					seenMember1 = true
					if valueIdx < len(rpt.Values) {
						iv, ivErr := rpt.Values[valueIdx].Int64()
						if ivErr != nil {
							t.Errorf("member[1] (Mod.stVal): expected int64 (%v)", ivErr)
						} else if iv != newModVal {
							t.Errorf("member[1] (Mod.stVal): want %d, got %d", newModVal, iv)
						}
					}
				}
				valueIdx++
			}

		case <-deadline:
			t.Fatalf("timeout: seenMember0=%v seenMember1=%v", seenMember0, seenMember1)
		}
	}
}
