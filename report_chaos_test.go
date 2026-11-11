// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// Chaos and race tests for the per-association report engine
// (IEC 61850-7-2 §17.2.1, §17.2.4.4/5, §17.2.3.2.3.6). Run with -race.

// checkGoroutines fails t when, after f and a settle period, more
// goroutines run than before.
func checkGoroutines(t *testing.T, f func()) {
	t.Helper()
	before := runtime.NumGoroutine()
	f()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= before {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("goroutine leak: %d before, %d after\n%s", before, n, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestChaos_URCBReservationRace: of 8 clients enabling one URCB at once,
// exactly one reserves it; the others get temporarily-unavailable
// (7-2 §17.2.4.5, 8-1 §8.1.3.4.4.2) (RPT-03).
func TestChaos_URCBReservationRace(t *testing.T) {
	checkGoroutines(t, func() {
		srv, err := NewServer(specModel(), ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		srv.EnableReports()
		ctx := context.Background()
		const n = 8
		clients := make([]*Client, n)
		for i := range clients {
			clients[i] = dialLoopback(t, ctx, srv)
		}
		var ok, busy atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, c := range clients {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				err := rawWrite(c, specURCB+"$RptEna", mms.NewBoolean(true))
				switch {
				case err == nil:
					ok.Add(1)
				case dataAccessCode(err) == mms.DataAccessErrorTemporarilyUnavailable:
					busy.Add(1)
				default:
					t.Errorf("enable: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if ok.Load() != 1 || busy.Load() != n-1 {
			t.Errorf("enabled by %d clients, refused for %d; want 1 and %d", ok.Load(), busy.Load(), n-1)
		}
		for _, c := range clients {
			dropAssociation(c)
		}
		srv.Close()
	})
}

// TestChaos_DisconnectDuringReporting: clients that drop while reports
// flow release their RCBs, and the next client can take over
// (7-2 §17.2.4.4, Figure 24) (RPT-02).
func TestChaos_DisconnectDuringReporting(t *testing.T) {
	checkGoroutines(t, func() {
		srv, err := NewServer(specModel(), ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		engine := srv.EnableReports()
		ctx := context.Background()

		stop := make(chan struct{})
		var prod sync.WaitGroup
		prod.Add(1)
		go func() {
			defer prod.Done()
			for i := int64(0); ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				srv.SetValue(ctx, specModKey, mms.NewInteger(i))
				time.Sleep(time.Millisecond)
			}
		}()

		for round := range 5 {
			c := dialLoopback(t, ctx, srv)
			for _, rcb := range []string{specURCB, specBRCB} {
				rptID := "rpt_urcb01"
				if rcb == specBRCB {
					rptID = "rpt_brcb01"
				}
				sub := subscribeRCB(t, c, rptID, rcb, true)
				recvReport(t, sub)
			}
			dropAssociation(c)
			for _, rcb := range []string{specURCB, specBRCB} {
				waitUntil(t, "release", func() bool { return rcbOwner(engine, rcb) == nil })
			}
			_ = round
		}
		close(stop)
		prod.Wait()
		srv.Close()
	})
}

// TestChaos_ReplayRacingLiveReports: entries created while a BRCB
// replays its buffer are delivered after the buffered ones, with
// contiguous SqNum and increasing EntryID (7-2 §17.2.3.2.3.5/6,
// §17.2.2.10) (RPT-09, RPT-10).
func TestChaos_ReplayRacingLiveReports(t *testing.T) {
	checkGoroutines(t, func() {
		srv, err := NewServer(specModel(), ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		srv.EnableReports()
		ctx := context.Background()
		const buffered, live = 200, 200
		for i := range buffered {
			srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i+1)))
		}
		c := dialLoopback(t, ctx, srv)
		sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, false)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := range live {
				srv.SetValue(ctx, specModKey, mms.NewInteger(int64(buffered+i+1)))
			}
		}()
		if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldRptEna, RptEna: true}); err != nil {
			t.Fatal(err)
		}
		<-done
		var lastID uint64
		for i := range buffered + live {
			r := recvReport(t, sub)
			if r.SeqNum != uint32(i) {
				t.Fatalf("report %d SqNum = %d", i, r.SeqNum)
			}
			id := decodeEntryID(r.EntryID)
			if id <= lastID {
				t.Fatalf("report %d EntryID %d after %d", i, id, lastID)
			}
			lastID = id
			if v, _ := r.Values[0].Int64(); v != int64(id) {
				t.Fatalf("report %d value %d for EntryID %d", i, v, id)
			}
		}
		dropAssociation(c)
		srv.Close()
	})
}

// TestChaos_EngineStopWhileSending: stopping the server while reports
// are queued ends every engine goroutine.
func TestChaos_EngineStopWhileSending(t *testing.T) {
	checkGoroutines(t, func() {
		srv, err := NewServer(specModel(), ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		srv.EnableReports()
		ctx := context.Background()
		c := dialLoopback(t, ctx, srv)
		subscribeRCB(t, c, "rpt_urcb01", specURCB, true)
		subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
		for i := range 100 {
			srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
		}
		srv.Close()
		dropAssociation(c)
	})
}
