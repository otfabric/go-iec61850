//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover BRCB EntryID resume behaviour.
//
// Scenario: a client connects, enables a BRCB, receives some reports, then
// disconnects. On reconnect, the client writes the last-known EntryID before
// enabling the BRCB. The server must replay only buffered entries that arrive
// after that EntryID, not entries the client already received.
//
// Tests:
//   - TestGoServer_BRCB_EntryID_Resume: client resumes from last known EntryID
//     and only receives entries produced after that point.
//   - TestGoServer_BRCB_EntryID_ZeroResume: when EntryID is all-zeros the
//     server replays the full buffer (no filtering).
//   - TestGoServer_BRCB_EntryID_ResumeFromFuture: when the written EntryID is
//     beyond any buffered entry, no replay is delivered.
package interop

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// drainAll drains all buffered reports from sub with a short deadline.
// It is used to flush stale entries before the primary assertion.
func drainAll(sub *iec61850.ReportSubscription, deadline time.Duration) []*iec61850.ReportIndication {
	var reports []*iec61850.ReportIndication
	t := time.NewTimer(deadline)
	defer t.Stop()
	for {
		select {
		case r := <-sub.Reports():
			reports = append(reports, r)
			// Reset the timer: keep draining until quiet.
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(deadline)
		case <-t.C:
			return reports
		}
	}
}

// TestGoServer_BRCB_EntryID_Resume verifies that writing a non-zero EntryID
// before enabling the BRCB causes the server to skip entries the client
// already received.
func TestGoServer_BRCB_EntryID_Resume(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx) //nolint:errcheck

	// ── Phase 1: enable BRCB and buffer two entries ──────────────────────────

	// First entry: GI
	sub1, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:            brcbLD,
		RCBItemID:     brcbItemID,
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport 1: %v", err)
	}

	var entry1ID []byte
	select {
	case rpt := <-sub1.Reports():
		t.Logf("entry1: seqNum=%d entryID=%x", rpt.SeqNum, rpt.EntryID)
		entry1ID = append([]byte(nil), rpt.EntryID...)
	case <-time.After(5 * time.Second):
		t.Fatal("entry 1 not received")
	}

	// Second entry: GI again.
	if err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldGI,
		GI:     true,
	}); err != nil {
		t.Fatalf("GI write: %v", err)
	}

	var entry2ID []byte
	select {
	case rpt := <-sub1.Reports():
		t.Logf("entry2: seqNum=%d entryID=%x", rpt.SeqNum, rpt.EntryID)
		entry2ID = append([]byte(nil), rpt.EntryID...)
	case <-time.After(5 * time.Second):
		t.Fatal("entry 2 not received")
	}

	if bytes.Equal(entry1ID, entry2ID) {
		t.Errorf("expected different EntryIDs for two distinct entries; both are %x", entry1ID)
	}

	// ── Phase 2: disable BRCB (entries stay buffered) ────────────────────────
	if err := sub1.Close(); err != nil {
		t.Fatalf("sub1.Close: %v", err)
	}

	// ── Phase 3: resume — write entry1ID before re-enabling ──────────────────
	// The server should replay only entry2 (the one after entry1ID).
	if err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields:  iec61850.RCBFieldEntryID,
		EntryID: entry1ID,
	}); err != nil {
		t.Fatalf("SetReportControlBlock EntryID: %v", err)
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

	replayed := drainAll(sub2, 3*time.Second)
	if len(replayed) == 0 {
		t.Fatal("expected at least one replayed entry after resume, got none")
	}
	// The only replayed entry should be entry2.
	if len(replayed) != 1 {
		t.Errorf("expected exactly 1 replayed entry, got %d", len(replayed))
	}
	if got := replayed[0].EntryID; !bytes.Equal(got, entry2ID) {
		t.Errorf("replayed EntryID = %x, want %x", got, entry2ID)
	}
}

// TestGoServer_BRCB_EntryID_ZeroResume verifies that when the written
// EntryID is all-zeros the full buffer is replayed.
func TestGoServer_BRCB_EntryID_ZeroResume(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx) //nolint:errcheck

	// Buffer one entry.
	sub1, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:            brcbLD,
		RCBItemID:     brcbItemID,
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	select {
	case <-sub1.Reports():
	case <-time.After(5 * time.Second):
		t.Fatal("initial GI not received")
	}
	if err := sub1.Close(); err != nil {
		t.Fatalf("sub1.Close: %v", err)
	}

	// Write zero EntryID (no filtering) and re-enable.
	if err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields:  iec61850.RCBFieldEntryID,
		EntryID: make([]byte, 8),
	}); err != nil {
		t.Fatalf("SetReportControlBlock EntryID=0: %v", err)
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

	replayed := drainAll(sub2, 3*time.Second)
	if len(replayed) == 0 {
		t.Fatal("expected full replay with zero EntryID, got none")
	}
}

// TestGoServer_BRCB_EntryID_ResumeFromFuture verifies that when the written
// EntryID is beyond any buffered entry, no replay is delivered.
func TestGoServer_BRCB_EntryID_ResumeFromFuture(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx) //nolint:errcheck

	// Buffer one entry.
	sub1, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:            brcbLD,
		RCBItemID:     brcbItemID,
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	select {
	case rpt := <-sub1.Reports():
		t.Logf("buffered entry: entryID=%x", rpt.EntryID)
	case <-time.After(5 * time.Second):
		t.Fatal("initial GI not received")
	}
	if err := sub1.Close(); err != nil {
		t.Fatalf("sub1.Close: %v", err)
	}

	// Write a very large EntryID (far in the future).
	futureID := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields:  iec61850.RCBFieldEntryID,
		EntryID: futureID,
	}); err != nil {
		t.Fatalf("SetReportControlBlock EntryID=max: %v", err)
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

	// Nothing should arrive.
	select {
	case rpt := <-sub2.Reports():
		t.Errorf("expected no replay with future EntryID, got report entryID=%x", rpt.EntryID)
	case <-time.After(2 * time.Second):
		// Expected: no replay.
	}
}
