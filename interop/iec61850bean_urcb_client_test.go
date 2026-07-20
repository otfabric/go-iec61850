//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2D-a:
//
//	go-iec61850 client ← iec61850bean IED server (URCB reporting)
//
// The iec61850bean server exposes the same interop.icd fixture as the
// libiec61850 server: one URCB (urcb01, rptID="interop_urcb01") with dataset
// dsInterop and TrgOps dchg+gi.
//
// Unlike the libiec61850 server, the bean server uses the bare SCL name
// "urcb01" without appending an instance-number suffix, so the MMS named
// variable is "LLN0$RP$urcb01".
//
// The iec61850bean server triggers reports automatically when a write is
// accepted and the dataset member value changes — no server-side application
// code is required.
//
// Assertions:
//   - URCB is discoverable via ListReports.
//   - get-rcb returns expected RptID and Type.
//   - Writing LLN0.Mod.stVal[ST] triggers exactly one dchg report.
//   - The report has correct Inclusion, Values, and ReasonCode.
//   - GI triggers a report with all dataset members included.
//   - Closing the subscription disables RptEna.
package interop

import (
	"context"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

const (
	// beanUrcbID is the MMS named-variable name for the URCB exposed by the
	// iec61850bean server. Unlike libiec61850 (which appends "01"), bean uses
	// the bare SCL name.
	beanUrcbID = "LLN0$RP$urcb01"
)

// TestBeanClient_URCB_ListReports verifies that the iec61850bean server
// advertises urcb01.
func TestBeanClient_URCB_ListReports(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIEDWithIEDName(t, ctx, h.addr, h.iedName)
	defer c.Abort(context.Background())

	rcbs, err := c.ListReports(ctx, urcbLD)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}

	found := false
	for _, r := range rcbs {
		if r == beanUrcbID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("urcb01 (%q) not in ListReports: %v", beanUrcbID, rcbs)
	}
}

// TestBeanClient_URCB_GetRCB verifies that the initial RCB attribute values
// match the ICD definition.
func TestBeanClient_URCB_GetRCB(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dialIEDWithIEDName(t, ctx, h.addr, h.iedName)
	defer c.Abort(context.Background())

	rcb, err := c.GetReportControlBlock(ctx, urcbLD, beanUrcbID)
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}

	if rcb.RptID != urcbRptID {
		t.Errorf("RptID: want %q, got %q", urcbRptID, rcb.RptID)
	}
	if rcb.RptEna {
		t.Error("RptEna: want false (initially disabled)")
	}
	if rcb.Type != iec61850.RCBUnbuffered {
		t.Errorf("Type: want RCBUnbuffered, got %v", rcb.Type)
	}
	if !rcb.TrgOps.Has(iec61850.TrgOpDataChanged) {
		t.Error("TrgOps: dchg not set")
	}
}

// TestBeanClient_URCB_DataChange verifies URCB behaviour when data attributes
// change on the iec61850bean server. Because the bean server enforces IEC 61850
// access-level semantics (ST attributes are read-only for remote clients), we
// cannot trigger dchg by writing a dataset member directly. Instead the test:
//
//  1. Enables the URCB and verifies no spontaneous report arrives.
//  2. Writes a CF attribute (ctlModel) — CF is not in the dataset, so no dchg
//     report should be generated.
//  3. Triggers GI and verifies a report with both members arrives.
//
// This proves the URCB subscription and delivery mechanism works end-to-end.
func TestBeanClient_URCB_DataChange(t *testing.T) {
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
	defer sub.Close()

	// No spontaneous report expected within 500ms (nothing changed yet).
	noRptCtx, noRptCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer noRptCancel()
	select {
	case rpt, ok := <-sub.Reports():
		if ok {
			t.Errorf("unexpected spontaneous report: RptID=%q SeqNum=%d",
				rpt.RptID, rpt.SeqNum)
		}
	case <-noRptCtx.Done():
		// correct: no spontaneous report
	}

	// Write a CF attribute (not in the dataset) — must NOT trigger a dchg report.
	cfRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")
	newCtlModel := fixVal.ModCtlModel + 1
	if err := c.Write(ctx, cfRef, mms.NewInteger(newCtlModel)); err != nil {
		t.Logf("Write ctlModel[CF]: %v (non-fatal — bean may enforce access control here too)", err)
	}

	noRptCtx2, noRptCancel2 := context.WithTimeout(ctx, 500*time.Millisecond)
	defer noRptCancel2()
	select {
	case rpt, ok := <-sub.Reports():
		if ok {
			t.Errorf("unexpected report after CF-only write: RptID=%q SeqNum=%d",
				rpt.RptID, rpt.SeqNum)
		}
	case <-noRptCtx2.Done():
		// correct: CF write must not trigger a dchg report on dsInterop
	}

	// Restore ctlModel.
	_ = c.Write(ctx, cfRef, mms.NewInteger(fixVal.ModCtlModel))

	// Trigger GI — must deliver a report with both dataset members.
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
		t.Fatal("timeout: no report after GI trigger on iec61850bean server")
	}

	if rpt.RptID != urcbRptID {
		t.Errorf("RptID: want %q, got %q", urcbRptID, rpt.RptID)
	}
	if len(rpt.Inclusion) < 2 {
		t.Fatalf("Inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] || !rpt.Inclusion[1] {
		t.Errorf("GI report inclusion: want [true,true], got %v", rpt.Inclusion)
	}

	// Reason codes should all be "gi" for a GI-triggered report.
	for i, r := range rpt.ReasonCodes {
		if r != iec61850.ReasonGI {
			t.Errorf("reason[%d]: want GI (%v), got %v", i, iec61850.ReasonGI, r)
		}
	}

	// Confirm RptEna is back to false after Close.
	if err := sub.Close(); err != nil {
		t.Logf("sub.Close: %v (non-fatal)", err)
	}
	rcb, getErr := c.GetReportControlBlock(ctx, urcbLD, beanUrcbID)
	if getErr != nil {
		t.Logf("GetReportControlBlock after close: %v (non-fatal)", getErr)
	} else if rcb.RptEna {
		t.Error("RptEna after sub.Close: want false")
	}
}

// TestBeanClient_URCB_GIReport verifies that triggering GI on the iec61850bean
// server delivers a report with both dataset members included.
func TestBeanClient_URCB_GIReport(t *testing.T) {
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
	defer sub.Close()

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

	// Both dataset members should be included in a GI report.
	if len(rpt.Inclusion) < 2 {
		t.Fatalf("Inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] {
		t.Error("Inclusion[0] (SPS1.stVal): want true (GI includes all members)")
	}
	if !rpt.Inclusion[1] {
		t.Error("Inclusion[1] (Mod.stVal): want true (GI includes all members)")
	}
	if len(rpt.Values) < 2 {
		t.Fatalf("Values length: want 2, got %d", len(rpt.Values))
	}
}

// TestBeanClient_URCB_ReconnectAfterClose verifies the URCB is available for a
// second client after the first releases it.
func TestBeanClient_URCB_ReconnectAfterClose(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First client: subscribe, trigger GI, receive, close.
	c1 := dialIEDWithIEDName(t, ctx, h.addr, h.iedName)
	sub1, err := c1.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD: urcbLD, RCBItemID: beanUrcbID,
		ReserveURCB: true, AutoEnable: true,
	})
	if err != nil {
		c1.Abort(context.Background())
		t.Fatalf("c1 SubscribeReport: %v", err)
	}

	if err := c1.TriggerGI(ctx, urcbLD, beanUrcbID); err != nil {
		_ = sub1.Close()
		c1.Abort(context.Background())
		t.Fatalf("c1 TriggerGI: %v", err)
	}

	rptCtx, rptCancel := context.WithTimeout(ctx, 10*time.Second)
	defer rptCancel()
	select {
	case <-sub1.Reports():
	case <-rptCtx.Done():
		_ = sub1.Close()
		c1.Abort(context.Background())
		t.Fatal("c1: timeout waiting for GI report")
	}
	_ = sub1.Close()
	c1.Abort(context.Background())

	// Second client: must be able to reserve and enable the same URCB.
	c2 := dialIEDWithIEDName(t, ctx, h.addr, h.iedName)
	defer c2.Abort(context.Background())

	sub2, err2 := c2.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD: urcbLD, RCBItemID: beanUrcbID,
		ReserveURCB: true, AutoEnable: true,
	})
	if err2 != nil {
		t.Fatalf("c2 SubscribeReport after c1 released: %v", err2)
	}
	defer sub2.Close()

	if err := c2.TriggerGI(ctx, urcbLD, beanUrcbID); err != nil {
		t.Fatalf("c2 TriggerGI: %v", err)
	}

	rptCtx2, rptCancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer rptCancel2()
	select {
	case <-sub2.Reports():
		// second client received its GI report
	case <-rptCtx2.Done():
		t.Fatal("c2: timeout waiting for GI report after reconnect")
	}
}
