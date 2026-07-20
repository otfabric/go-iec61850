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
	mms "github.com/otfabric/go-mms"
)

const (
	brcbLD     = "InteropLD"
	brcbItemID = "LLN0$BR$brcb01"
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
		if rpt.SeqNum == 0 {
			t.Error("expected non-zero SeqNum in BRCB report")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BRCB GI report not received within timeout")
	}
}

// ---------------------------------------------------------------------------
// TestGoServer_BRCB_Replay
// ---------------------------------------------------------------------------

// TestGoServer_BRCB_Replay verifies that buffered entries are replayed when
// the BRCB is re-enabled after being disabled. The sequence:
//  1. Enable BRCB, trigger GI → report received and buffered.
//  2. Disable BRCB.
//  3. Re-enable BRCB → buffered entry is replayed to the client.
func TestGoServer_BRCB_Replay(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	// Step 1: enable BRCB and buffer one entry via GI.
	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:            brcbLD,
		RCBItemID:     brcbItemID,
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}

	var firstEntryID []byte
	select {
	case rpt := <-sub.Reports():
		t.Logf("step 1 report: seqNum=%d entryID=%x", rpt.SeqNum, rpt.EntryID)
		firstEntryID = rpt.EntryID
	case <-time.After(5 * time.Second):
		t.Fatal("step 1: GI report not received")
	}

	// Step 2: disable BRCB.
	if err := sub.Close(); err != nil {
		t.Fatalf("sub.Close: %v", err)
	}

	// Step 3: re-enable BRCB and wait for the replay.
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
		t.Logf("replay report: seqNum=%d entryID=%x", rpt.SeqNum, rpt.EntryID)
		if len(rpt.EntryID) == 0 {
			t.Error("replayed report should have non-empty EntryID")
		}
		// The replayed entry should have the same EntryID as the original.
		if len(firstEntryID) > 0 && len(rpt.EntryID) > 0 {
			if string(rpt.EntryID) != string(firstEntryID) {
				t.Errorf("replayed EntryID: want %x, got %x", firstEntryID, rpt.EntryID)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replay report not received after re-enabling BRCB")
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

	// Step 1: enable BRCB, buffer one entry.
	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:            brcbLD,
		RCBItemID:     brcbItemID,
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}

	select {
	case rpt := <-sub.Reports():
		t.Logf("step 1 report: seqNum=%d entryID=%x", rpt.SeqNum, rpt.EntryID)
	case <-time.After(5 * time.Second):
		t.Fatal("step 1: GI report not received")
	}

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

// TestGoServer_BRCB_Overflow fills the BRCB buffer beyond its capacity and
// verifies that the BufOvfl flag is set in the resulting report.
//
// We shrink the buffer to 2 entries via SetRCBBufMax so we only need to
// generate 3 reports to trigger an overflow.
func TestGoServer_BRCB_Overflow(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	// Enable BRCB.
	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close()

	// Shrink the buffer to 2 so we can overflow with just 3 reports.
	re := h.srv.ReportEngine()
	if re == nil {
		t.Skip("report engine not available")
	}
	if !re.SetRCBBufMax(brcbLD, brcbItemID, 2) {
		t.Fatalf("BRCB runtime not found: %s/%s", brcbLD, brcbItemID)
	}

	// Drain any pending reports before generating overflow traffic.
	time.Sleep(200 * time.Millisecond)
drainLoop:
	for {
		select {
		case <-sub.Reports():
		default:
			break drainLoop
		}
	}

	// Generate 4 value changes via SetValue (which notifies the report engine).
	// The 3rd change overflows the buffer of 2.
	writeCtx := context.Background()
	for i := 0; i < 4; i++ {
		h.srv.SetValue(writeCtx, "InteropLD/GGIO1$ST$SPS1$stVal",
			mms.NewBoolean(i%2 == 0))
		time.Sleep(50 * time.Millisecond)
	}

	// Collect reports and look for BufOvfl=true.
	deadline := time.After(5 * time.Second)
	sawOverflow := false
	for !sawOverflow {
		select {
		case rpt, open := <-sub.Reports():
			if !open {
				t.Fatal("subscription channel closed")
			}
			t.Logf("report: seqNum=%d bufOvfl=%v", rpt.SeqNum, rpt.BufOvfl)
			if rpt.BufOvfl {
				sawOverflow = true
			}
		case <-deadline:
			t.Error("no report with BufOvfl=true received after filling buffer")
			return
		}
	}
}
