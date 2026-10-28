// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// brcbReplayModel is testReportSCL with EntryID and DataSet optional
// fields enabled on the BRCB so replayed reports carry their entry IDs.
func brcbReplayModel() *servermodel.Model {
	m := testReportSCL()
	rpt := &m.LogicalDevices[0].LogicalNodes[0].Reports[0]
	rpt.OptFlds.EntryID = true
	rpt.OptFlds.DataSet = true
	return m
}

// bufferBRCBEntries enables brcb01 without a connection, records n data
// changes into its buffer and disables it again, leaving the buffer for a
// later client to retrieve.
func bufferBRCBEntries(t *testing.T, srv *Server, engine *ReportEngine, n int) {
	t.Helper()
	ctx := context.Background()
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	key := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	for i := 1; i <= n; i++ {
		srv.SetValue(ctx, key, mms.NewInteger(int64(i*10)))
	}
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(false)); err != nil {
		t.Fatalf("disable: %v", err)
	}
}

func recvReport(t *testing.T, sub *ReportSubscription) *ReportIndication {
	t.Helper()
	select {
	case r := <-sub.Reports():
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for replayed report")
		return nil
	}
}

func TestBRCB_ReplaysBufferedEntriesToEnablingClient(t *testing.T) {
	srv, err := NewServer(brcbReplayModel(), ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	engine := srv.EnableReports()
	defer engine.Stop()

	bufferBRCBEntries(t, srv, engine, 3)

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub, err := c.SubscribeReport(ctx, "rpt_brcb01", SubscribeReportOptions{
		LD:         "LD1",
		RCBItemID:  "LLN0$BR$brcb01",
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	for i := 1; i <= 3; i++ {
		r := recvReport(t, sub)
		if r.RptID != "rpt_brcb01" {
			t.Errorf("#%d RptID = %q", i, r.RptID)
		}
		if r.SeqNum != uint32(i) {
			t.Errorf("#%d SeqNum = %d, want %d", i, r.SeqNum, i)
		}
		if got := decodeEntryID(r.EntryID); got != uint64(i) {
			t.Errorf("#%d EntryID = %d, want %d", i, got, i)
		}
		if r.DatSet != "LD1/LLN0$dsTest" {
			t.Errorf("#%d DatSet = %q", i, r.DatSet)
		}
		if len(r.Values) != 1 {
			t.Fatalf("#%d values = %d, want 1", i, len(r.Values))
		}
		if v, err := r.Values[0].Int64(); err != nil || v != int64(i*10) {
			t.Errorf("#%d value = %d (%v), want %d", i, v, err, i*10)
		}
		if len(r.ReasonCodes) != 1 || r.ReasonCodes[0]&ReasonDataChanged == 0 {
			t.Errorf("#%d reasons = %v, want data-change", i, r.ReasonCodes)
		}
	}
}

func TestBRCB_ReplayResumesAfterClientEntryID(t *testing.T) {
	srv, err := NewServer(brcbReplayModel(), ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	engine := srv.EnableReports()
	defer engine.Stop()

	bufferBRCBEntries(t, srv, engine, 3)

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	// "I already have entry 2": only entry 3 must be replayed.
	if err := c.SetReportControlBlock(ctx, "LD1", "LLN0$BR$brcb01", RCBUpdate{
		Fields:  RCBFieldEntryID,
		EntryID: encodeEntryID(2),
	}); err != nil {
		t.Fatalf("write EntryID: %v", err)
	}
	sub, err := c.SubscribeReport(ctx, "rpt_brcb01", SubscribeReportOptions{
		LD:         "LD1",
		RCBItemID:  "LLN0$BR$brcb01",
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	r := recvReport(t, sub)
	if got := decodeEntryID(r.EntryID); got != 3 {
		t.Fatalf("first replayed EntryID = %d, want 3", got)
	}
	if v, err := r.Values[0].Int64(); err != nil || v != 30 {
		t.Errorf("value = %d (%v), want 30", v, err)
	}
	// Replay numbering restarts at 1 for the filtered set.
	if r.SeqNum != 1 {
		t.Errorf("SeqNum = %d, want 1", r.SeqNum)
	}
}

func TestBRCB_ResumeAtLatestEntrySkipsReplay(t *testing.T) {
	srv, err := NewServer(brcbReplayModel(), ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	engine := srv.EnableReports()
	defer engine.Stop()

	bufferBRCBEntries(t, srv, engine, 2)

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	if err := c.SetReportControlBlock(ctx, "LD1", "LLN0$BR$brcb01", RCBUpdate{
		Fields:  RCBFieldEntryID | RCBFieldRptEna,
		EntryID: encodeEntryID(2),
		RptEna:  true,
	}); err != nil {
		t.Fatalf("enable with EntryID: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	enabled, qLen := rt.enabled, len(rt.bufQueue)
	rt.mu.Unlock()
	if !enabled {
		t.Error("BRCB should be enabled even when nothing needs replaying")
	}
	if qLen != 2 {
		t.Errorf("buffer should be retained, got %d entries", qLen)
	}

	// A fresh change after enabling is still delivered live.
	sub, err := c.SubscribeReport(ctx, "rpt_brcb01", SubscribeReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	srv.SetValue(ctx, servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal"), mms.NewInteger(99))
	r := recvReport(t, sub)
	if got := decodeEntryID(r.EntryID); got != 3 {
		t.Errorf("live EntryID = %d, want 3", got)
	}
	if v, _ := r.Values[0].Int64(); v != 99 {
		t.Errorf("live value = %d, want 99", v)
	}
}
