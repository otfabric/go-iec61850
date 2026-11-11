//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase F — BRCB (Buffered Report Control Block).
//
// The ICD fixture defines brcb01 (buffered, same dsInterop dataset as urcb01).
// All tests are go-to-go (startGoIEDServerWithReports + go-iec61850 client)
// so they run without Docker adapters.
//
// Tests:
//   - TestGoServer_BRCB_EntryID: enable BRCB, trigger GI, verify non-empty EntryID.
//   - TestGoServer_BRCB_Replay: enable, buffer an entry, disable, re-enable, verify replay.
//   - TestGoServer_BRCB_Purge: enable, buffer, disable, PurgeBuf, re-enable, no replay.
//   - TestGoServer_BRCB_Overflow: fill buffer past bufMax, verify BufOvfl flag in report.
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

const (
	brcbLD     = "InteropLD"
	brcbItemID = "LLN0$BR$brcb0101" // indexed SCL brcb01, first instance
	brcbRptID  = "interop_brcb01"
)

// ---------------------------------------------------------------------------
// TestGoServer_BRCB_EntryID
// ---------------------------------------------------------------------------

// TestGoServer_BRCB_EntryID enables the BRCB, triggers a GI, and verifies
// the received report contains a non-empty EntryID.
func TestGoServer_BRCB_EntryID(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:            brcbLD,
		RCBItemID:     brcbItemID,
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close()

	select {
	case rpt := <-sub.Reports():
		t.Logf("BRCB report: seqNum=%d entryID=%x optFlds=%s",
			rpt.SeqNum, rpt.EntryID, rpt.OptFlds)
		if len(rpt.EntryID) == 0 {
			t.Error("expected non-empty EntryID in BRCB report")
		}
		// SqNum starts at 0 after enable (IEC 61850-7-2 §17.2.2.10).
	case <-time.After(5 * time.Second):
		t.Fatal("BRCB GI report not received within timeout")
	}
}

// ---------------------------------------------------------------------------
// TestGoServer_BRCB_Replay
// ---------------------------------------------------------------------------

// TestGoServer_BRCB_Replay verifies that buffered entries are replayed when
// the BRCB is re-enabled after being disabled. The sequence:
//  1. Enable BRCB, change a member → data-change report received.
//  2. Disable BRCB.
//  3. Re-enable BRCB → the buffered entry is sent again (no EntryID
//     resync: the whole buffer, IEC 61850-7-2 Figure 24).
//
// A GI report is not used: GI reports are not buffered
// (IEC 61850-7-2 §17.2.3.2.3.6).
func TestGoServer_BRCB_Replay(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	toggleSPS1(h, 1)
	firstEntryID := recvReportT(t, sub, "step 1 report").EntryID

	if err := sub.Close(); err != nil {
		t.Fatalf("sub.Close: %v", err)
	}

	sub2, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport 2: %v", err)
	}
	defer sub2.Close()

	rpt := recvReportT(t, sub2, "replay report")
	if string(rpt.EntryID) != string(firstEntryID) {
		t.Errorf("replayed EntryID: want %x, got %x", firstEntryID, rpt.EntryID)
	}
	if rpt.SeqNum != 0 {
		t.Errorf("replayed SeqNum = %d, want 0 after the enable (7-2 §17.2.2.10)", rpt.SeqNum)
	}
}

// ---------------------------------------------------------------------------
// TestGoServer_BRCB_Purge
// ---------------------------------------------------------------------------

// TestGoServer_BRCB_Purge verifies that writing PurgeBuf=true clears the
// buffered entries. After purging, re-enabling the BRCB should not trigger
// any replay.
func TestGoServer_BRCB_Purge(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	// Step 1: enable BRCB, buffer one data-change entry.
	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	toggleSPS1(h, 1)
	recvReportT(t, sub, "step 1 report")

	// Step 2: disable BRCB.
	if err := sub.Close(); err != nil {
		t.Fatalf("sub.Close: %v", err)
	}

	// Step 3: purge the buffer.
	if err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields:   iec61850.RCBFieldPurgeBuf,
		PurgeBuf: true,
	}); err != nil {
		t.Fatalf("SetReportControlBlock PurgeBuf: %v", err)
	}

	// Step 4: re-enable BRCB — no replay expected.
	sub2, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport 2: %v", err)
	}
	defer sub2.Close()

	select {
	case rpt := <-sub2.Reports():
		t.Errorf("unexpected replay after PurgeBuf: seqNum=%d entryID=%x",
			rpt.SeqNum, rpt.EntryID)
	case <-time.After(2 * time.Second):
		// Correct: no replay after purge.
	}
}

// ---------------------------------------------------------------------------
// TestGoServer_BRCB_Overflow
// ---------------------------------------------------------------------------

// TestGoServer_BRCB_Overflow fills the BRCB buffer beyond its capacity
// while the BRCB is disabled and verifies that the first report after the
// enable has BufOvfl=true and the next one BufOvfl=false
// (IEC 61850-7-2 §17.2.3.2.2.8: entries not yet sent were discarded).
//
// We shrink the buffer to 2 entries via SetRCBBufMax so we only need to
// generate 4 changes to discard two.
func TestGoServer_BRCB_Overflow(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	re := h.srv.ReportEngine()
	if re == nil {
		t.Skip("report engine not available")
	}
	if !re.SetRCBBufMax(goLD, brcbItemID, 2) {
		t.Fatalf("BRCB runtime not found: %s/%s", brcbLD, brcbItemID)
	}
	toggleSPS1(h, 4)

	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close()

	if rpt := recvReportT(t, sub, "first report"); !rpt.BufOvfl {
		t.Error("first report after the overflow: BufOvfl=false, want true")
	}
	if rpt := recvReportT(t, sub, "second report"); rpt.BufOvfl {
		t.Error("second report: BufOvfl=true, want false")
	}
}
