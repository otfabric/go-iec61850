// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// Spec-verification tests of buffer time (IEC 61850-7-2 §17.2.2.9,
// Figure 26) and its interplay with integrity (§17.2.2.12,
// §17.2.3.2.3.3) and general interrogation (§17.2.2.13 Figure 28,
// §17.2.3.2.3.4). The BufTm timer runs on a fake clock, so every
// assertion is about the instant an entry is created, not a sleep.

// fakeClock is a manual reportClock and Clock: timers fire in Advance,
// in the caller's goroutine, in deadline order, and TimeOfEntry stamps
// read the same manual time.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Quality is the TimeQuality of SystemClock.
func (c *fakeClock) Quality() TimeQuality { return systemClockQuality }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) reportTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	active := !t.stopped && !t.fired
	t.stopped = true
	return active
}

// Advance moves the clock by d and runs every timer that is due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.at.After(c.now) {
			t.fired = true
			due = append(due, t)
		}
	}
	c.timers = slices.DeleteFunc(c.timers, func(t *fakeTimer) bool { return t.stopped || t.fired })
	c.mu.Unlock()
	slices.SortFunc(due, func(a, b *fakeTimer) int { return a.at.Compare(b.at) })
	for _, t := range due {
		t.f()
	}
}

// active returns the number of timers that have neither fired nor been
// stopped.
func (c *fakeClock) active() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// bufTmFixture is a spec server whose BRCB monitors dsTwo {Mod.stVal,
// Beh.stVal} with the given BufTm on a fake clock. The BRCB stays
// disabled: it buffers in every state (§17.2.3.2.3.6), so the buffer
// shows each entry the moment it is created.
type bufTmFixture struct {
	t      *testing.T
	srv    *Server
	engine *ReportEngine
	rt     *rcbRuntime
	clk    *fakeClock
}

func newBufTmFixture(t *testing.T, bufTm uint32) *bufTmFixture {
	t.Helper()
	srv, engine := newSpecServer(t)
	clk := newFakeClock()
	engine.clk = clk
	engine.clock = clk
	ctx := context.Background()
	for _, w := range []struct {
		attr string
		v    *mms.Value
	}{
		{"DatSet", mms.NewVisibleString("LD1/LLN0$dsTwo")},
		{"BufTm", mms.NewUnsigned(uint64(bufTm))},
	} {
		if err := engine.HandleRCBWrite(ctx, "LD1", specBRCB, w.attr, w.v); err != nil {
			t.Fatalf("write %s: %v", w.attr, err)
		}
	}
	return &bufTmFixture{t: t, srv: srv, engine: engine, rt: engine.rcbs["LD1/"+specBRCB], clk: clk}
}

func (f *bufTmFixture) set(key string, v int64) {
	f.srv.SetValue(context.Background(), key, mms.NewInteger(v))
}

func (f *bufTmFixture) entries() []ReportEntry {
	f.rt.mu.Lock()
	defer f.rt.mu.Unlock()
	return f.rt.buf.(*MemoryReportBuffer).Entries()
}

// wantEntries checks the buffer: one map per entry from member index to
// the reported value.
func (f *bufTmFixture) wantEntries(when string, want ...map[int]int64) []ReportEntry {
	f.t.Helper()
	got := f.entries()
	if len(got) != len(want) {
		f.t.Fatalf("%s: %d entries, want %d", when, len(got), len(want))
	}
	for k, e := range got {
		for i, in := range e.Inclusion {
			v, ok := want[k][i]
			if in != ok {
				f.t.Fatalf("%s: entry %d inclusion = %v, want members %v", when, k, e.Inclusion, want[k])
			}
			if !in {
				continue
			}
			if n, _ := e.Values[i].Int64(); n != v {
				f.t.Errorf("%s: entry %d member %d = %v, want %d", when, k, i, e.Values[i], v)
			}
			if e.Reasons[i]&ReasonDataChanged == 0 {
				f.t.Errorf("%s: entry %d member %d reason = %v, want dchg", when, k, i, e.Reasons[i])
			}
		}
	}
	return got
}

// TestSpec_7_2_17_2_2_9_FirstNotificationStartsBufTm: the first
// internal notification starts the timer; notifications of other members
// within BufTm are combined into one report when it expires, with the
// TimeOfEntry of the first notification (§17.2.2.16).
func TestSpec_7_2_17_2_2_9_FirstNotificationStartsBufTm(t *testing.T) {
	f := newBufTmFixture(t, 100)
	t0 := f.clk.Now()
	f.set(specModKey, 1)
	if f.clk.active() != 1 {
		t.Fatalf("%d timers after the first notification, want 1", f.clk.active())
	}
	f.clk.Advance(60 * time.Millisecond)
	f.set(specBehKey, 2)
	f.clk.Advance(39 * time.Millisecond)
	f.wantEntries("before BufTm expires")

	f.clk.Advance(time.Millisecond)
	got := f.wantEntries("at BufTm", map[int]int64{0: 1, 1: 2})
	if !got[0].TimeOfEntry.Equal(t0) {
		t.Errorf("TimeOfEntry = %v, want the first notification %v", got[0].TimeOfEntry, t0)
	}
	if f.clk.active() != 0 {
		t.Errorf("%d timers left after expiry", f.clk.active())
	}
}

// TestSpec_7_2_17_2_2_9_NextNotificationRestartsTimer: the notification
// after the expiry starts a new timer of the full BufTm; the timer does
// not run periodically.
func TestSpec_7_2_17_2_2_9_NextNotificationRestartsTimer(t *testing.T) {
	f := newBufTmFixture(t, 100)
	f.set(specModKey, 1)
	f.clk.Advance(100 * time.Millisecond)
	f.wantEntries("first period", map[int]int64{0: 1})

	f.clk.Advance(150 * time.Millisecond) // quiet: no timer runs
	if f.clk.active() != 0 {
		t.Fatalf("%d timers while quiet", f.clk.active())
	}
	f.set(specBehKey, 2)
	f.clk.Advance(99 * time.Millisecond)
	f.wantEntries("second period running", map[int]int64{0: 1})
	f.clk.Advance(time.Millisecond)
	f.wantEntries("second period", map[int]int64{0: 1}, map[int]int64{1: 2})
}

// TestSpec_7_2_17_2_2_9_SecondNotificationSameMember: a second
// notification of a member already in the pending report sends that
// report at once, restarts the timer with BufTm and starts the next
// report with the new notification (mandatory for FC=ST).
func TestSpec_7_2_17_2_2_9_SecondNotificationSameMember(t *testing.T) {
	f := newBufTmFixture(t, 100)
	f.set(specModKey, 1)
	f.clk.Advance(10 * time.Millisecond)
	f.set(specBehKey, 2)
	f.clk.Advance(30 * time.Millisecond)
	f.set(specModKey, 3) // t = 40 ms: Mod is already pending
	f.wantEntries("second Mod notification", map[int]int64{0: 1, 1: 2})

	// The first timer (due at 100 ms) no longer applies.
	f.clk.Advance(60 * time.Millisecond)
	f.wantEntries("old deadline", map[int]int64{0: 1, 1: 2})
	if f.clk.active() != 1 {
		t.Fatalf("%d timers, want the restarted one", f.clk.active())
	}
	f.clk.Advance(40 * time.Millisecond) // t = 140 ms = 40 + BufTm
	f.wantEntries("restarted timer", map[int]int64{0: 1, 1: 2}, map[int]int64{0: 3})
}

// TestSpec_7_2_17_2_2_9_RepeatedNotificationsAllReported: changes of the
// same member are communicated in consecutive reports and none is lost
// (§17.2.2.9 NOTE 2).
func TestSpec_7_2_17_2_2_9_RepeatedNotificationsAllReported(t *testing.T) {
	f := newBufTmFixture(t, 1000)
	for v := range int64(5) {
		f.set(specModKey, v+1)
	}
	f.clk.Advance(time.Second)
	f.wantEntries("five changes",
		map[int]int64{0: 1}, map[int]int64{0: 2}, map[int]int64{0: 3}, map[int]int64{0: 4}, map[int]int64{0: 5})
}

// TestSpec_7_2_17_2_2_9_BufTmZeroNoBuffering: BufTm = 0 means the buffer
// time is not used: each internal notification is its own report, at
// once, with only the member that produced it (§17.2.3.2.2.9 b).
func TestSpec_7_2_17_2_2_9_BufTmZeroNoBuffering(t *testing.T) {
	f := newBufTmFixture(t, 0)
	f.set(specModKey, 1)
	f.set(specBehKey, 2)
	f.wantEntries("BufTm 0", map[int]int64{0: 1}, map[int]int64{1: 2})
	if f.clk.active() != 0 {
		t.Errorf("%d timers with BufTm 0", f.clk.active())
	}
}

// TestSpec_7_2_17_2_2_9_ReasonsCombined: a member whose notification
// carries several reasons keeps them all in the combined report, and an
// unchanged write produces no notification.
func TestSpec_7_2_17_2_2_9_ReasonsCombined(t *testing.T) {
	f := newBufTmFixture(t, 100)
	ctx := context.Background()
	if err := f.engine.HandleRCBWrite(ctx, "LD1", specBRCB, "TrgOps", encodeTrgOps(TrgOpDataChanged|TrgOpDataUpdate)); err != nil {
		t.Fatal(err)
	}
	f.set(specModKey, 1)
	f.set(specBehKey, 0) // unchanged value: data-update only
	f.clk.Advance(100 * time.Millisecond)
	got := f.entries()
	if len(got) != 1 {
		t.Fatalf("%d entries, want 1", len(got))
	}
	if r := got[0].Reasons[0]; r != ReasonDataChanged|ReasonDataUpdate {
		t.Errorf("Mod reason = %v, want dchg|dupd", r)
	}
	if r := got[0].Reasons[1]; r != ReasonDataUpdate {
		t.Errorf("Beh reason = %v, want dupd", r)
	}
}

// TestSpec_7_2_17_2_2_12_IntegrityNotDelayedByBufTm: BufTm has no effect
// on an integrity report; events pending in the buffer time are reported
// first, so the reports keep the time order of their entries
// (§17.2.3.2.3.5).
func TestSpec_7_2_17_2_2_12_IntegrityNotDelayedByBufTm(t *testing.T) {
	f := newBufTmFixture(t, 100)
	f.set(specModKey, 1)
	f.clk.Advance(20 * time.Millisecond)

	f.rt.mu.Lock()
	f.engine.integrityLocked(f.rt) // integrity period expired at 20 ms
	f.rt.mu.Unlock()

	got := f.entries()
	if len(got) != 2 {
		t.Fatalf("%d entries, want the pending event and the integrity", len(got))
	}
	if !got[0].Inclusion[0] || got[0].Inclusion[1] || got[0].Reasons[0] != ReasonDataChanged {
		t.Errorf("first entry = %v %v, want the dchg of Mod", got[0].Inclusion, got[0].Reasons)
	}
	if got[1].Reasons[0] != ReasonIntegrity || got[1].Reasons[1] != ReasonIntegrity {
		t.Errorf("second entry reasons = %v, want integrity for all", got[1].Reasons)
	}
	// The integrity ended the buffer period: the timer is gone and the
	// next notification starts a new one.
	f.clk.Advance(80 * time.Millisecond)
	if n := len(f.entries()); n != 2 {
		t.Errorf("%d entries after the old deadline, want 2", n)
	}
}

// TestSpec_7_2_17_2_2_13_GIAfterPendingEvents: a GI request queues the
// events pending in the buffer time before the GI report (Figure 28
// "Prior events reported"); BufTm does not delay the GI.
func TestSpec_7_2_17_2_2_13_GIAfterPendingEvents(t *testing.T) {
	f := newBufTmFixture(t, 100)
	ctx := context.Background()
	if err := f.engine.HandleRCBWrite(ctx, "LD1", specBRCB, "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	f.set(specModKey, 1)
	if err := f.engine.HandleRCBWrite(ctx, "LD1", specBRCB, "GI", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	got := f.entries()
	if len(got) != 2 || got[0].GI || !got[1].GI {
		t.Fatalf("entries = %+v, want the pending event then the GI", got)
	}
	if f.clk.active() != 0 {
		t.Errorf("%d timers after GI", f.clk.active())
	}
	// A second GI before the first is sent replaces it (Figure 28
	// "Delete pending GI information").
	if err := f.engine.HandleRCBWrite(ctx, "LD1", specBRCB, "GI", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	got = f.entries()
	if len(got) != 2 || !got[1].GI || got[1].ID != 3 {
		t.Fatalf("after a second GI: %d entries, newest GI=%v ID=%d; want 2, the new GI with ID 3", len(got), got[len(got)-1].GI, got[len(got)-1].ID)
	}
	// The GI is not buffered: disabling removes it (§17.2.3.2.3.6).
	if err := f.engine.HandleRCBWrite(ctx, "LD1", specBRCB, "RptEna", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	if got := f.entries(); len(got) != 1 || got[0].GI {
		t.Errorf("after disable: %+v, want only the event", got)
	}
}

// TestSpec_8_1_17_1_2_BufTmChangeDropsPendingReport: a SetBRCBValues that
// changes BufTm purges the buffer (7-2 §17.2.2.9, 8-1 §17.1.2), which
// includes the report pending in the buffer time.
func TestSpec_8_1_17_1_2_BufTmChangeDropsPendingReport(t *testing.T) {
	f := newBufTmFixture(t, 100)
	f.set(specModKey, 1)
	if err := f.engine.HandleRCBWrite(context.Background(), "LD1", specBRCB, "BufTm", mms.NewUnsigned(200)); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(time.Second)
	f.wantEntries("after the BufTm change")
	f.set(specBehKey, 2)
	f.clk.Advance(199 * time.Millisecond)
	f.wantEntries("new BufTm running")
	f.clk.Advance(time.Millisecond)
	f.wantEntries("new BufTm", map[int]int64{1: 2})
}

// TestSpec_7_2_17_2_2_9_EngineStopDropsTimer: a BufTm timer that fires
// after Stop records nothing.
func TestSpec_7_2_17_2_2_9_EngineStopDropsTimer(t *testing.T) {
	f := newBufTmFixture(t, 100)
	f.set(specModKey, 1)
	f.engine.Stop()
	f.clk.Advance(time.Second)
	f.wantEntries("after Stop")
}

// TestSpec_7_2_17_2_3_2_2_5_BufTmReportSegmented: a report that combines
// more members than one PDU holds within BufTm is sent as subreports and
// reassembled (8-1 §17.2.1).
func TestSpec_7_2_17_2_3_2_2_5_BufTmReportSegmented(t *testing.T) {
	const n = 150
	srv, err := NewServer(segmentationModel(n), ServerOptions{MMS: mms.ServerOptions{MMS: mms.ServerMMSOptions{MaxPDUSize: 2000}}})
	if err != nil {
		t.Fatal(err)
	}
	engine := srv.EnableReports()
	clk := newFakeClock()
	engine.clk = clk
	engine.clock = clk
	defer srv.Close()
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldBufTm, BufTm: 500}); err != nil {
		t.Fatal(err)
	}
	sub := subscribeRCB(t, c, "big", specBRCB, true)
	for i := range n {
		srv.SetValue(ctx, servermodel.StoreKey("LD1", fmt.Sprintf("GGIO1$ST$Ind%d$stVal", i+1)), mms.NewBoolean(true))
	}
	expectNoReport(t, sub, 50*time.Millisecond)
	clk.Advance(500 * time.Millisecond)
	r := recvReport(t, sub)
	if len(r.Values) != n || slices.Contains(r.Inclusion, false) {
		t.Fatalf("assembled report: %d values, inclusion %v; want all %d members", len(r.Values), r.Inclusion, n)
	}
	for i, rc := range r.ReasonCodes {
		if rc != ReasonDataChanged {
			t.Fatalf("member %d reason = %v", i, rc)
		}
	}
	expectNoReport(t, sub, 100*time.Millisecond)
}
