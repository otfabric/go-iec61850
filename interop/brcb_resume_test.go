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
//   - TestGoServer_BRCB_EntryID_ResumeFromFuture: an EntryID that is not in
//     the buffer is refused (IEC 61850-7-2 §17.2.2.15, Figure 24).
//
// The buffered entries are data changes: a BRCB buffers dchg, qchg, dupd
// and integrity entries, not general-interrogation reports
// (IEC 61850-7-2 §17.2.3.2.3.6).
package interop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// toggleSPS1 inverts GGIO1.SPS1.stVal n times: n data-change entries in
// the fixture's BRCB.
func toggleSPS1(h *goIEDServer, n int) {
	key := goLD + "/GGIO1$ST$SPS1$stVal"
	for range n {
		cur, _ := h.srv.ValueStore().Get(key).Bool()
		h.srv.SetValue(context.Background(), key, mms.NewBoolean(!cur))
	}
}

// recvReportT waits for one report.
func recvReportT(t *testing.T, sub *iec61850.ReportSubscription, what string) *iec61850.ReportIndication {
	t.Helper()
	select {
	case rpt := <-sub.Reports():
		t.Logf("%s: seqNum=%d entryID=%x bufOvfl=%v", what, rpt.SeqNum, rpt.EntryID, rpt.BufOvfl)
		return rpt
	case <-time.After(5 * time.Second):
		t.Fatalf("%s not received", what)
		return nil
	}
}

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

	// ── Phase 1: enable BRCB and receive two data-change entries ─────────────
	sub1, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport 1: %v", err)
	}
	toggleSPS1(h, 2)
	entry1ID := append([]byte(nil), recvReportT(t, sub1, "entry1").EntryID...)
	entry2ID := append([]byte(nil), recvReportT(t, sub1, "entry2").EntryID...)
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
	if len(replayed) != 1 {
		t.Fatalf("expected exactly 1 replayed entry, got %d", len(replayed))
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

	// Buffer one entry while the BRCB is disabled (7-2 §17.2.3.2.3.6).
	toggleSPS1(h, 1)

	// Write zero EntryID (no resync) and enable.
	if err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields:  iec61850.RCBFieldEntryID,
		EntryID: make([]byte, 8),
	}); err != nil {
		t.Fatalf("SetReportControlBlock EntryID=0: %v", err)
	}

	sub, err := c.SubscribeReport(ctx, brcbRptID, iec61850.SubscribeReportOptions{
		LD:         brcbLD,
		RCBItemID:  brcbItemID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close()

	replayed := drainAll(sub, 3*time.Second)
	if len(replayed) != 1 {
		t.Fatalf("expected the buffered entry with zero EntryID, got %d", len(replayed))
	}
}

// TestGoServer_BRCB_EntryID_ResumeFromFuture verifies that an EntryID that
// is not in the buffer is refused: the resync needs an entry the BRCB holds
// (IEC 61850-7-2 §17.2.2.15, Figure 24; 8-1 parameter-value-inappropriate →
// object-value-invalid).
func TestGoServer_BRCB_EntryID_ResumeFromFuture(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx) //nolint:errcheck

	toggleSPS1(h, 1)

	futureID := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	err := c.SetReportControlBlock(ctx, brcbLD, brcbItemID, iec61850.RCBUpdate{
		Fields:  iec61850.RCBFieldEntryID,
		EntryID: futureID,
	})
	var dae *mms.DataAccessError
	if !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectValueInvalid {
		t.Fatalf("SetReportControlBlock EntryID=max: %v, want object-value-invalid", err)
	}
}
