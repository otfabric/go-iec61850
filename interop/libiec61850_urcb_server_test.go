//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2C-b:
//
//	libiec61850 IED client → go-iec61850 server (URCB reporting)
//
// The go-iec61850 server has the report engine enabled
// (startGoIEDServerWithReports). When a client writes a data attribute the
// write interceptor stores the new value and calls
// ReportEngine.NotifyValueChanged so any enabled dchg-triggered URCB can
// send an InformationReport.
//
// The libiec61850-ied-reporter binary exercises the full cycle:
//
//  1. Read current URCB attributes (get-rcb).
//  2. Install report handler, set RptEna=true (enable-rcb).
//  3. Write GGIO1.SPS1.stVal[ST] to !initial (write, triggers dchg).
//  4. Wait for the InformationReport (receive-report).
//  5. Set RptEna=false (disable-rcb).
//  6. Disconnect (conclude).
//
// Assertions:
//   - receive-report is present and ok=true.
//   - rptID matches "interop_urcb01".
//   - seqNum is non-zero.
//   - inclusion[0]=true (SPS1.stVal changed), inclusion[1]=false (Mod.stVal not changed).
//   - values[0] equals !fixVal.SPS1StVal.
//   - reasons[0] is "data-change".
package interop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// TestLibIECServer_URCB_DataChange runs libiec61850-ied-reporter against a
// go-iec61850 server with the report engine enabled and validates the
// full dchg report cycle.
func TestLibIECServer_URCB_DataChange(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ch := startIEDReporterAdapter(t, h.port, fixVal.SPS1StVal)

	results := collectReporterResults(t, ch)

	// 1. get-rcb must succeed.
	rcb, ok := findReporterOp(results, "get-rcb")
	if !ok {
		t.Error("get-rcb: not found in reporter output")
	} else if !rcb.OK {
		t.Errorf("get-rcb: ok=false, error=%q", rcb.Error)
	} else if rcb.RptID != "interop_urcb01" {
		t.Errorf("get-rcb: rptID want %q, got %q", "interop_urcb01", rcb.RptID)
	}

	// 2. enable-rcb must succeed.
	if en, ok := findReporterOp(results, "enable-rcb"); !ok {
		t.Error("enable-rcb: not found")
	} else if !en.OK {
		t.Errorf("enable-rcb: ok=false, error=%q", en.Error)
	}

	// 3. write must succeed.
	if wr, ok := findReporterOp(results, "write"); !ok {
		t.Error("write: not found")
	} else if !wr.OK {
		t.Errorf("write: ok=false, error=%q", wr.Error)
	}

	// 4. receive-report is the core assertion.
	rpt, ok := findReporterOp(results, "receive-report")
	if !ok {
		t.Fatal("receive-report: not found — reporter timed out waiting for dchg report")
	}
	if !rpt.OK {
		t.Fatalf("receive-report: ok=false, error=%q", rpt.Error)
	}

	if rpt.RptID != "interop_urcb01" {
		t.Errorf("receive-report rptID: want %q, got %q", "interop_urcb01", rpt.RptID)
	}
	if rpt.SeqNum == 0 {
		t.Error("receive-report seqNum: expected non-zero")
	}

	// Inclusion: [true, false] — only SPS1.stVal changed.
	if len(rpt.Inclusion) < 2 {
		t.Fatalf("inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] {
		t.Error("inclusion[0] (SPS1.stVal): want true (changed)")
	}
	if rpt.Inclusion[1] {
		t.Error("inclusion[1] (Mod.stVal): want false (not changed)")
	}

	// Values: first included member should be !fixVal.SPS1StVal.
	if len(rpt.Values) > 0 {
		var vals []json.RawMessage
		if err := json.Unmarshal(rpt.Values, &vals); err != nil {
			t.Fatalf("values: unmarshal: %v", err)
		}
		if len(vals) == 0 {
			t.Fatal("values array is empty")
		}
		var b bool
		if err := json.Unmarshal(vals[0], &b); err != nil {
			t.Fatalf("values[0]: expected bool, got %s", vals[0])
		}
		want := !fixVal.SPS1StVal
		if b != want {
			t.Errorf("values[0] (SPS1.stVal): want %v, got %v", want, b)
		}
	} else {
		t.Error("values: missing from receive-report")
	}

	// Reason codes: first included member must be "data-change".
	if len(rpt.Reasons) > 0 {
		if rpt.Reasons[0] != "data-change" {
			t.Errorf("reasons[0]: want %q, got %q", "data-change", rpt.Reasons[0])
		}
	} else {
		t.Error("reasons: missing from receive-report")
	}

	// 5. disable-rcb must succeed.
	if dis, ok := findReporterOp(results, "disable-rcb"); !ok {
		t.Error("disable-rcb: not found")
	} else if !dis.OK {
		t.Errorf("disable-rcb: ok=false, error=%q", dis.Error)
	}

	// 6. conclude must succeed.
	if con, ok := findReporterOp(results, "conclude"); !ok {
		t.Error("conclude: not found")
	} else if !con.OK {
		t.Errorf("conclude: ok=false, error=%q", con.Error)
	}
}

// TestLibIECServer_URCB_RejectedWriteNoReport verifies that enabling
// reports on the go-iec61850 server does not weaken write validation.
//
// A write that would be rejected by the server (wrong MMS type) must be
// rejected identically whether or not the report engine is active. The
// invariant is:
//
//	EnableReports() changes notification behavior only.
//	It must not expand which writes are accepted.
//
// Additionally, no spurious report should be delivered after a rejected write,
// and the client association must remain usable.
func TestLibIECServer_URCB_RejectedWriteNoReport(t *testing.T) {
	// The go-iec61850 server uses the SCL name directly ("urcb01"),
	// unlike the libiec61850 server which appends an instance suffix ("urcb0101").
	const serverUrcbID = "LLN0$RP$urcb01"

	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIED(t, ctx, fmt.Sprintf("localhost:%d", h.port))
	defer c.Close(ctx)

	sub, err := c.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD: urcbLD, RCBItemID: serverUrcbID,
		ReserveURCB: true, AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close()

	// GGIO1.SPS1.stVal[ST] is BOOLEAN; writing an INTEGER is a type mismatch.
	ref, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	writeErr := c.Write(ctx, ref, mms.NewInteger(99))
	if writeErr == nil {
		t.Error("expected type-mismatch error writing INTEGER to BOOLEAN attribute, got nil")
	} else {
		t.Logf("correctly rejected: %v", writeErr)
	}

	// No report expected: a rejected write must not trigger a dchg notification.
	noReportCtx, noReportCancel := context.WithTimeout(ctx, 2*time.Second)
	defer noReportCancel()
	select {
	case rpt, ok := <-sub.Reports():
		if ok {
			t.Errorf("spurious report after rejected write: RptID=%q SeqNum=%d",
				rpt.RptID, rpt.SeqNum)
		}
	case <-noReportCtx.Done():
		// correct: no report
	}

	// Association must remain usable after a rejected write.
	raw, readErr := c.GetReportControlBlock(ctx, urcbLD, serverUrcbID)
	if readErr != nil {
		t.Fatalf("GetReportControlBlock after rejected write: %v", readErr)
	}
	if !raw.RptEna {
		t.Error("RptEna should still be true after rejected write")
	}
}
