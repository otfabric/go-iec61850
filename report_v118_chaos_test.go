// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// Chaos tests for the BRCB buffer and BufTm of v1.18.0 (IEC 61850-7-2
// §17.2.2.9, §17.2.3.2.2.8, §17.2.2.15). Run with -race.

// TestChaos_BRCBOverflowAcrossReconnect: a client receives a BRCB with an
// eight-entry buffer, loses its association at a random point, and while
// it is away a seeded number of data changes (often more than the buffer
// holds) are buffered; it reconnects, resynchronises with the last
// EntryID it received and enables again. Five rounds per seed.
//
// Invariants: the client sees every EntryID at most once and in
// increasing order; each entry carries the value of its data change; an
// entry is missing only after the buffer overflowed, and then the first
// report after the gap has BufOvfl set; a resync to a discarded entry is
// refused and the first report after it has BufOvfl set; nothing leaks.
func TestChaos_BRCBOverflowAcrossReconnect(t *testing.T) {
	for _, seed := range []int64{1, 2, 3} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			checkGoroutines(t, func() { brcbOverflowRounds(t, seed) })
		})
	}
}

func brcbOverflowRounds(t *testing.T, seed int64) {
	const bufEntries = 8
	m := brcbReplayModel()
	m.LogicalDevices[0].LogicalNodes[0].Reports[0].OptFlds.BufOvfl = true
	srv, err := NewServer(m, ServerOptions{
		NewReportBuffer: func(string, string) ReportBuffer { return NewMemoryReportBuffer(bufEntries, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()
	rng := rand.New(rand.NewSource(seed))
	key := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	var written uint64 // entry n carries the value 10n
	write := func(k int) {
		for range k {
			written++
			srv.SetValue(ctx, key, mms.NewInteger(int64(written*10)))
		}
	}
	var last uint64 // newest EntryID the client received
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("seed %d, last EntryID %d: %s", seed, last, fmt.Sprintf(format, args...))
	}

	// The first association enables the BRCB, so buffering starts.
	write(0)
	for round := range 5 {
		c := dialLoopback(t, ctx, srv)
		resynced := false
		for deadline := time.Now().Add(5 * time.Second); last > 0; {
			err := c.SetReportControlBlock(ctx, "LD1", "LLN0$BR$brcb01", RCBUpdate{Fields: RCBFieldEntryID, EntryID: encodeEntryID(last)})
			// Temporarily unavailable: the previous association's release
			// is still running.
			if dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable || time.Now().After(deadline) {
				resynced = err == nil
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		var sub *ReportSubscription
		deadline := time.Now().Add(5 * time.Second)
		for {
			// The previous association's release may still be running.
			sub, err = c.SubscribeReport(ctx, "rpt_brcb01", SubscribeReportOptions{LD: "LD1", RCBItemID: "LLN0$BR$brcb01", AutoEnable: true, QueueSize: 1024})
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				fail("round %d: enable: %v", round, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if round == 0 {
			write(3)
		}
		// Live changes while the client receives, then a drop once it has
		// them all.
		live := rng.Intn(6)
		want := written + uint64(live)
		var wg sync.WaitGroup
		wg.Go(func() { write(live) })
		first := true
		for last < want {
			r := recvReport(t, sub)
			id := decodeEntryID(r.EntryID)
			if id <= last {
				fail("round %d: EntryID %d after %d", round, id, last)
			}
			if v, err := r.Values[0].Int64(); err != nil || v != int64(id*10) {
				fail("round %d: entry %d carries %d (%v)", round, id, v, err)
			}
			// An overflow can also happen while the client receives: the
			// first report after any gap has BufOvfl.
			if id > last+1 && !r.BufOvfl {
				fail("round %d: entries %d..%d lost without BufOvfl (first %v, resynced %v)", round, last+1, id-1, first, resynced)
			}
			// A refused resync: the entry was discarded, so entries may have
			// been lost and the first report says so.
			if first && last > 0 && !resynced && !r.BufOvfl {
				fail("round %d: resync to %d refused, first report %d without BufOvfl", round, last, id)
			}
			first = false
			last = id
		}
		wg.Wait()
		dropAssociation(c)
		// Away: often more changes than the buffer holds.
		write(rng.Intn(3 * bufEntries))
	}
}

// TestChaos_BufTmUnderLoad: four writers change the data set member of a
// BRCB with BufTm 20 ms as fast as they can while a client receives.
// Invariants (§17.2.2.9): a second change of a member already waiting in
// BufTm sends the waiting report first, so no change is lost or merged:
// the client receives every written value exactly once, in the order the
// store took them, and the buffer (1000 entries) does not overflow.
func TestChaos_BufTmUnderLoad(t *testing.T) {
	checkGoroutines(t, func() {
		m := brcbReplayModel()
		m.LogicalDevices[0].LogicalNodes[0].Reports[0].BufTime = 20
		m.LogicalDevices[0].LogicalNodes[0].Reports[0].OptFlds.BufOvfl = true
		srv, err := NewServer(m, ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer srv.Close()
		engine := srv.EnableReports()
		defer engine.Stop()
		ctx := context.Background()
		c := dialLoopback(t, ctx, srv)
		sub, err := c.SubscribeReport(ctx, "rpt_brcb01", SubscribeReportOptions{LD: "LD1", RCBItemID: "LLN0$BR$brcb01", AutoEnable: true, QueueSize: 1024})
		if err != nil {
			t.Fatal(err)
		}
		defer dropAssociation(c)
		defer func() { _ = sub.Close() }()

		key := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
		const writers, changes = 4, 150
		var wg sync.WaitGroup
		var mu sync.Mutex
		var order []int64
		for w := range writers {
			wg.Go(func() {
				for i := range changes {
					v := int64(w*changes + i + 1)
					mu.Lock() // order is the order the store took the values
					srv.SetValue(ctx, key, mms.NewInteger(v))
					order = append(order, v)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		for i, want := range order {
			r := recvReport(t, sub)
			if r.BufOvfl {
				t.Fatalf("report %d: BufOvfl", i)
			}
			if len(r.Values) != 1 {
				t.Fatalf("report %d with %d values", i, len(r.Values))
			}
			if v, _ := r.Values[0].Int64(); v != want {
				t.Fatalf("report %d carries %d, want %d", i, v, want)
			}
		}
	})
}
