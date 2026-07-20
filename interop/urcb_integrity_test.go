//go:build interop

// SPDX-License-Identifier: MIT

// Phase E1 — URCB integrity period tests.
//
// These tests verify that the go-iec61850 server and the libiec61850 server
// correctly generate periodic integrity reports when IntgPd is configured
// and TrgOpIntegrity is enabled on the URCB.
//
// Go→Go tests configure the RCB directly via SetReportControlBlock before
// subscribing.  LibIEC tests attempt the same configuration on the external
// server; the test is skipped if the server rejects the write (e.g. because
// the ICD marks TrgOps/IntgPd as fixed).
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// goServerURCBID is the MMS named-variable ID for the URCB on the
// go-iec61850 server.  The go server uses the bare SCL name without the
// libiec61850 instance-number suffix ("01").
const goServerURCBID = "LLN0$RP$urcb01"

// TestGoServer_URCB_IntegrityPeriod verifies that the go-iec61850 server
// delivers periodic integrity reports when IntgPd=500ms and TrgOps includes
// TrgOpIntegrity.  It expects at least 2 reports within 3 seconds and
// validates that:
//   - Every dataset member is included (all Inclusion bits set).
//   - Every member's ReasonCode includes ReasonIntegrity.
//   - SeqNum increases monotonically across reports.
func TestGoServer_URCB_IntegrityPeriod(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	// Reserve, then configure TrgOps=integrity and IntgPd=500ms before enabling.
	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps | iec61850.RCBFieldIntgPd,
		TrgOps: iec61850.TrgOpIntegrity,
		IntgPd: 500,
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

	// Collect at least 2 integrity reports within 3s.
	var reports []*iec61850.ReportIndication
	deadline := time.After(3 * time.Second)
	for len(reports) < 2 {
		select {
		case rpt, ok := <-sub.Reports():
			if !ok {
				t.Fatal("subscription channel closed before receiving 2 integrity reports")
			}
			t.Logf("integrity report %d: SeqNum=%d Inclusion=%v ReasonCodes=%v",
				len(reports)+1, rpt.SeqNum, rpt.Inclusion, rpt.ReasonCodes)
			reports = append(reports, rpt)
		case <-deadline:
			t.Fatalf("timeout: received %d of 2 expected integrity reports", len(reports))
		}
	}

	// Disable RCB.
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldRptEna,
		RptEna: false,
	}); err != nil {
		t.Logf("disable RCB: %v (non-fatal)", err)
	}

	// Validate the collected reports.
	var prevSeq uint32
	for i, rpt := range reports {
		// Integrity reports must include every dataset member.
		for j, inc := range rpt.Inclusion {
			if !inc {
				t.Errorf("report[%d]: Inclusion[%d]=false, want true (integrity includes all members)", i, j)
			}
		}
		// Each included member must carry ReasonIntegrity.
		for j, rc := range rpt.ReasonCodes {
			if rc&iec61850.ReasonIntegrity == 0 {
				t.Errorf("report[%d]: ReasonCodes[%d]=%v, want ReasonIntegrity set", i, j, rc)
			}
		}
		// SeqNum must increase monotonically.
		if i > 0 && rpt.SeqNum <= prevSeq {
			t.Errorf("report[%d]: SeqNum=%d not greater than previous %d (not monotonic)",
				i, rpt.SeqNum, prevSeq)
		}
		prevSeq = rpt.SeqNum
	}
}

// TestGoServer_URCB_IntegrityCoexistsWithDchg verifies that data-change and
// integrity triggers coexist correctly.  After enabling with both flags, a
// data write causes a dchg report; the integrity timer fires independently
// without cancelling or suppressing either trigger.
func TestGoServer_URCB_IntegrityCoexistsWithDchg(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps | iec61850.RCBFieldIntgPd,
		TrgOps: iec61850.TrgOpIntegrity | iec61850.TrgOpDataChanged,
		IntgPd: 800,
	}); err != nil {
		t.Fatalf("SetReportControlBlock: %v", err)
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

	// Write SPS1.stVal to a new value to trigger a dchg report.
	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if err := c.Write(ctx, ref, mms.NewBoolean(!fixVal.SPS1StVal)); err != nil {
		t.Fatalf("Write SPS1.stVal: %v", err)
	}

	// Wait up to 3s to observe at least one dchg report and one integrity report.
	seenDchg := false
	seenIntg := false
	deadline := time.After(3 * time.Second)
	for !seenDchg || !seenIntg {
		select {
		case rpt, ok := <-sub.Reports():
			if !ok {
				t.Fatal("subscription channel closed")
			}
			t.Logf("report: SeqNum=%d ReasonCodes=%v", rpt.SeqNum, rpt.ReasonCodes)
			for _, rc := range rpt.ReasonCodes {
				if rc&iec61850.ReasonDataChanged != 0 {
					seenDchg = true
				}
				if rc&iec61850.ReasonIntegrity != 0 {
					seenIntg = true
				}
			}
			// If OptFlds does not include ReasonCode, attribute the first
			// report after the write to dchg and subsequent ones to integrity.
			if len(rpt.ReasonCodes) == 0 {
				if !seenDchg {
					seenDchg = true
				} else {
					seenIntg = true
				}
			}
		case <-deadline:
			t.Fatalf("timeout: seenDchg=%v seenIntg=%v", seenDchg, seenIntg)
		}
	}
}

// TestGoServer_URCB_IntegrityDisableStopsTimer verifies that disabling the
// RCB stops the integrity timer.  After writing RptEna=false no further
// reports must arrive for at least 700ms (more than two integrity periods).
func TestGoServer_URCB_IntegrityDisableStopsTimer(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps | iec61850.RCBFieldIntgPd,
		TrgOps: iec61850.TrgOpIntegrity,
		IntgPd: 300,
	}); err != nil {
		t.Fatalf("SetReportControlBlock: %v", err)
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

	// Wait for at least one integrity report to confirm the timer is running.
	select {
	case rpt, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before first integrity report")
		}
		t.Logf("first integrity report: SeqNum=%d", rpt.SeqNum)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first integrity report before disable")
	}

	// Disable the RCB.
	if err := c.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldRptEna,
		RptEna: false,
	}); err != nil {
		t.Fatalf("disable RCB: %v", err)
	}

	// Drain any reports that were already in-flight at the moment of disable.
	time.Sleep(50 * time.Millisecond)
drain:
	for {
		select {
		case <-sub.Reports():
		default:
			break drain
		}
	}

	// No further reports must arrive for 700ms (> 2 integrity periods of 300ms).
	select {
	case rpt, ok := <-sub.Reports():
		if ok {
			t.Errorf("unexpected report after RptEna=false: SeqNum=%d", rpt.SeqNum)
		}
	case <-time.After(700 * time.Millisecond):
		// correct: timer stopped
	}
}

// TestLibIECServer_URCB_IntegrityPeriod verifies that the libiec61850 server
// delivers at least one integrity report when IntgPd=1000ms and TrgOps is
// set to integrity.
//
// The libiec61850 fixture ICD defines TrgOps with period=false.  If the
// server rejects the TrgOps or IntgPd write (because those attributes are
// marked fixed by ReportSettings), the test is skipped rather than failed.
func TestLibIECServer_URCB_IntegrityPeriod(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := h.dial(t, ctx)
	defer c.Close(ctx)

	// Attempt to configure integrity on the libiec61850 URCB.
	if err := c.SetReportControlBlock(ctx, urcbLD, urcbID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldTrgOps | iec61850.RCBFieldIntgPd,
		TrgOps: iec61850.TrgOpIntegrity,
		IntgPd: 1000,
	}); err != nil {
		t.Skipf("libiec61850 server rejected TrgOps/IntgPd write "+
			"(ICD may mark them fixed via ReportSettings): %v", err)
	}

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

	// Wait up to 4s for at least one integrity report.
	select {
	case rpt, ok := <-sub.Reports():
		if !ok {
			t.Fatal("subscription channel closed before integrity report")
		}
		t.Logf("integrity report: SeqNum=%d ReasonCodes=%v", rpt.SeqNum, rpt.ReasonCodes)
		for i, rc := range rpt.ReasonCodes {
			if rc&iec61850.ReasonIntegrity == 0 {
				t.Errorf("ReasonCodes[%d]=%v: want ReasonIntegrity set", i, rc)
			}
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timeout: no integrity report received from libiec61850 server within 4s")
	}

	if err := c.SetReportControlBlock(ctx, urcbLD, urcbID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldRptEna,
		RptEna: false,
	}); err != nil {
		t.Logf("disable RCB: %v (non-fatal)", err)
	}
}
