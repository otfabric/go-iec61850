// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// fakeClock is a manually advanced clock for subscriber tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// frameWith returns an encoded frame of one ASDU per counter.
func frameWith(t *testing.T, mod func(f *Frame), cnts ...uint16) []byte {
	t.Helper()
	f := &Frame{Dst: testDst, Src: testSrc, AppID: 0x4000}
	for _, c := range cnts {
		f.PDU.ASDUs = append(f.PDU.ASDUs, ASDU{SvID: "MU01", DatSet: "IED1MU/LLN0$PhsMeas1", SmpCnt: c, ConfRev: 1, SmpSynch: 1, Sample: leSample(int32(c))})
	}
	if mod != nil {
		mod(f)
	}
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func drainEvents(sub *Subscription) []Event {
	var out []Event
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestSubscriberSequence(t *testing.T) {
	clk := newFakeClock()
	s, err := NewSubscriber(nil, SubscriberConfig{Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	var got []uint16
	var lostSeen int
	sub, err := s.Subscribe(SubscriptionConfig{SvID: "MU01", SmpCntWrap: 10, OnASDU: func(smp *Sample) {
		got = append(got, smp.ASDU.SmpCnt)
		lostSeen += smp.Lost
		if smp.Stream.SvID != "MU01" || smp.Frame == nil {
			t.Error("sample context")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	feed := func(cnts ...uint16) {
		for _, c := range cnts {
			clk.Add(time.Millisecond)
			s.HandleFrame(frameWith(t, nil, c))
		}
	}
	feed(7, 8, 9, 0, 1) // wrap at 10
	if evs := kinds(drainEvents(sub)); len(evs) != 1 || evs[0] != EventStart {
		t.Fatalf("start: %v", evs)
	}
	feed(4) // gap: 2, 3
	feed(4) // duplicate
	feed(3) // out of order (late)
	feed(5)
	evs := drainEvents(sub)
	if k := kinds(evs); len(k) != 3 || k[0] != EventGap || k[1] != EventDuplicate || k[2] != EventOutOfOrder || evs[0].Lost != 2 {
		t.Fatalf("events: %v %+v", k, evs)
	}
	if len(got) != 7 || got[5] != 4 || got[6] != 5 || lostSeen != 2 {
		t.Fatalf("delivered %v lost %d", got, lostSeen)
	}
	st := sub.Stats()
	if len(st) != 1 || st[0].Lost != 2 || st[0].Duplicates != 1 || st[0].OutOfOrder != 1 || st[0].ASDUs != 9 || st[0].LastSmpCnt != 5 ||
		st[0].SmpCntWrap != 10 || st[0].Status != StatusOK || st[0].ConfRev != 1 || st[0].SmpSynch != 1 {
		t.Fatalf("stats %+v", st[0])
	}
}

func TestSubscriberWrapLearningAndRate(t *testing.T) {
	clk := newFakeClock()
	s, _ := NewSubscriber(nil, SubscriberConfig{Now: clk.Now})
	sub, _ := s.Subscribe(SubscriptionConfig{})
	// 4000 samples per second, wrapping at 4000; no smpRate on the wire.
	for i := range 4100 {
		clk.Add(250 * time.Microsecond)
		s.HandleFrame(frameWith(t, nil, uint16(i%4000)))
	}
	st := sub.Stats()[0]
	if st.Lost != 0 || st.OutOfOrder != 0 || st.SmpCntWrap != 4000 {
		t.Fatalf("learned wrap: %+v", st)
	}
	if st.Rate < 3900 || st.Rate > 4100 {
		t.Errorf("rate %.0f", st.Rate)
	}
	// smpRate on the wire derives the modulus (80/period @ 60 Hz = 4800).
	s2, _ := NewSubscriber(nil, SubscriberConfig{Now: clk.Now})
	sub2, _ := s2.Subscribe(SubscriptionConfig{NominalFrequency: 60})
	s2.HandleFrame(frameWith(t, func(f *Frame) {
		f.PDU.ASDUs[0].HasSmpRate, f.PDU.ASDUs[0].SmpRate = true, 80
		f.PDU.ASDUs[0].HasSmpMod, f.PDU.ASDUs[0].SmpMod = true, SmpPerPeriod
	}, 4799))
	s2.HandleFrame(frameWith(t, nil, 0))
	if st := sub2.Stats()[0]; st.SmpCntWrap != 4800 || st.Lost != 0 {
		t.Fatalf("derived wrap: %+v", st)
	}
	// Configured smpRate derives it too; a counter beyond it resyncs.
	s3, _ := NewSubscriber(nil, SubscriberConfig{Now: clk.Now})
	sub3, _ := s3.Subscribe(SubscriptionConfig{SmpRate: 4000, SmpMod: SmpPerSec})
	for _, c := range []uint16{3999, 0, 5000, 5001} {
		s3.HandleFrame(frameWith(t, nil, c))
	}
	if st := sub3.Stats()[0]; st.SmpCntWrap != 4000 || st.Lost != 0 {
		t.Fatalf("configured wrap: %+v", st)
	}
}

func TestSubscriberEventsAndValidation(t *testing.T) {
	clk := newFakeClock()
	var dropped atomic.Int32
	m := &countMetrics{}
	s, _ := NewSubscriber(nil, SubscriberConfig{Now: clk.Now, Metrics: m, OnDecodeError: func([]byte, error) { dropped.Add(1) }})
	sub, _ := s.Subscribe(SubscriptionConfig{
		Dst: testDst, Src: testSrc, AppIDs: []uint16{0x4000}, SvID: "MU01",
		DatSet: "IED1MU/LLN0$PhsMeas1", ConfRev: 1, SmpRate: 80, SmpMod: SmpPerPeriod, Layout: LELayout, Timeout: 50 * time.Millisecond,
	})
	s.HandleFrame(frameWith(t, nil, 0))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.PDU.ASDUs[0].SmpSynch = 2 }, 1))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.PDU.ASDUs[0].ConfRev = 2 }, 2))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.PDU.ASDUs[0].DatSet = "other" }, 3))
	s.HandleFrame(frameWith(t, func(f *Frame) {
		f.PDU.ASDUs[0].HasSmpRate, f.PDU.ASDUs[0].SmpRate = true, 256
	}, 4))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.PDU.ASDUs[0].Sample = []byte{1} }, 5))
	// Filters and drops.
	s.HandleFrame(frameWith(t, func(f *Frame) { f.Dst = MAC{1} }, 6))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.Src = MAC{2} }, 6))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.AppID = 0x4001 }, 6))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.PDU.ASDUs[0].SvID = "other" }, 6))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.Simulation = true }, 6))
	s.HandleFrame([]byte{1, 2, 3})
	s.HandleFrame(make([]byte, 60)) // EtherType 0: not SV, ignored silently

	evs := drainEvents(sub)
	// A configured confRev turns a change into an invalid ASDU, not EventConfRev.
	want := []EventKind{EventStart, EventSynch, EventInvalid, EventInvalid, EventInvalid, EventInvalid}
	if k := kinds(evs); len(k) != len(want) {
		t.Fatalf("events %v", k)
	} else {
		for i := range want {
			if k[i] != want[i] {
				t.Fatalf("events %v, want %v", k, want)
			}
		}
	}
	for i, sentinel := range []error{ErrConfRevMismatch, ErrDatSetMismatch, ErrSmpRateMismatch, ErrSampleSizeMismatch} {
		if err := evs[2+i].Err; !errors.Is(err, sentinel) || !errors.Is(err, ErrValidation) || err.Error() == "" {
			t.Errorf("invalid %d: %v", i, err)
		}
	}
	if st := sub.Stats()[0]; st.Invalid != 4 || st.Status != StatusInvalid {
		t.Errorf("stats %+v", st)
	}
	if dropped.Load() != 1 || m.dropped["decode"] != 1 || m.dropped["filtered"] != 4 || m.dropped["simulation"] != 1 {
		t.Errorf("drops %d %v", dropped.Load(), m.dropped)
	}

	// Timeout and restore.
	clk.Add(60 * time.Millisecond)
	s.CheckTimeouts()
	s.CheckTimeouts() // reported once
	s.HandleFrame(frameWith(t, nil, 7))
	k := kinds(drainEvents(sub))
	// smpSynch reverts to 1 and the invalid ASDUs 2..5 (plus 6) count as a gap.
	if len(k) != 4 || k[0] != EventTimeout || k[1] != EventRestored || k[2] != EventSynch || k[3] != EventGap {
		t.Fatalf("timeout events %v", k)
	}
	if st := sub.Stats()[0]; st.Timeouts != 1 || st.Status != StatusOK {
		t.Errorf("stats %+v", st)
	}
	// Without a configured confRev a change is reported as EventConfRev.
	sub2, _ := s.Subscribe(SubscriptionConfig{SvID: "MU01"})
	s.HandleFrame(frameWith(t, nil, 8))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.PDU.ASDUs[0].ConfRev = 5 }, 9))
	if evs := drainEvents(sub2); len(evs) != 2 || evs[1].Kind != EventConfRev || evs[1].ConfRev != 5 {
		t.Errorf("confrev events %+v", evs)
	}
	if m.events.Load() == 0 || m.asdus.Load() == 0 {
		t.Error("metrics not called")
	}
}

func TestSubscriberSimulationAndChannels(t *testing.T) {
	clk := newFakeClock()
	s, _ := NewSubscriber(nil, SubscriberConfig{Now: clk.Now, AcceptSimulation: true})
	sub, _ := s.Subscribe(SubscriptionConfig{SampleQueue: 2, EventQueue: 1})
	for c := range uint16(4) {
		s.HandleFrame(frameWith(t, func(f *Frame) { f.Simulation = true; f.PDU.Security = []byte{1} }, c, c+10))
	}
	smp := <-sub.Samples()
	if smp.ASDU.SmpCnt != 0 || !smp.Frame.Simulation || smp.Frame.PDU.Security[0] != 1 || len(smp.ASDU.Sample) != LESampleSize {
		t.Fatalf("sample %+v", smp)
	}
	st := sub.Stats()[0]
	if st.QueueDrops == 0 || !st.Simulation || st.Frames != 4 {
		t.Errorf("stats %+v", st)
	}
	// The event queue holds one event; the rest are dropped.
	if evs := drainEvents(sub); len(evs) != 1 {
		t.Errorf("events %d", len(evs))
	}
	_ = sub.Close()
	_ = sub.Close()
	if _, ok := <-sub.Events(); ok {
		t.Error("events not closed")
	}
	for range sub.Samples() {
	}
	s.HandleFrame(frameWith(t, nil, 9)) // no subscription left
	if _, err := s.Subscribe(SubscriptionConfig{SampleSize: 3, Layout: LELayout}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("size/layout mismatch: %v", err)
	}
	if _, err := s.Subscribe(SubscriptionConfig{Layout: Layout{KindInvalid}}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("layout: %v", err)
	}
	if _, err := s.Subscribe(SubscriptionConfig{SmpCntWrap: 70000}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("wrap: %v", err)
	}
	if _, err := NewSubscriber(nil, SubscriberConfig{Limits: Limits{MaxASDUs: -1}}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("limits: %v", err)
	}
	if err := s.Run(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("Run without transport: %v", err)
	}
	sub2, _ := s.Subscribe(SubscriptionConfig{})
	_ = s.Close()
	_ = s.Close()
	if _, ok := <-sub2.Events(); ok {
		t.Error("subscriber close did not close subscriptions")
	}
	if _, err := s.Subscribe(SubscriptionConfig{}); !errors.Is(err, ErrClosed) {
		t.Errorf("subscribe after close: %v", err)
	}
}

func TestStatsOrderAndNames(t *testing.T) {
	clk := newFakeClock()
	s, _ := NewSubscriber(nil, SubscriberConfig{Now: clk.Now})
	sub, _ := s.Subscribe(SubscriptionConfig{})
	for _, id := range []string{"b", "a"} {
		for _, app := range []uint16{0x4002, 0x4001} {
			s.HandleFrame(frameWith(t, func(f *Frame) { f.AppID = app; f.PDU.ASDUs[0].SvID = id }, 0))
		}
	}
	s.HandleFrame(frameWith(t, func(f *Frame) { f.Src = MAC{9}; f.PDU.ASDUs[0].SvID = "a" }, 0))
	st := sub.Stats()
	if len(st) != 5 || st[0].Stream.SvID != "a" || st[0].Stream.Src != testSrc || st[1].Stream.AppID != 0x4002 || st[2].Stream.Src != (MAC{9}) {
		t.Fatalf("order %+v", st)
	}
	if st[0].Stream.String() == "" {
		t.Error("key string")
	}
	for k := EventStart; k <= EventInvalid; k++ {
		if k.String() == "" || k.String()[0] == 'E' {
			t.Errorf("event %d name %q", k, k)
		}
	}
	if EventKind(99).String() != "EventKind(99)" || Status(9).String() != "Status(9)" ||
		StatusOK.String() != "ok" || StatusTimeout.String() != "timeout" || StatusInvalid.String() != "invalid" {
		t.Error("names")
	}
}

// countMetrics records metric calls.
type countMetrics struct {
	NopMetrics
	mu      sync.Mutex
	dropped map[string]int
	sent    atomic.Int64
	errs    atomic.Int64
	asdus   atomic.Int64
	events  atomic.Int64
}

func (m *countMetrics) FrameDropped(r string) {
	m.mu.Lock()
	if m.dropped == nil {
		m.dropped = map[string]int{}
	}
	m.dropped[r]++
	m.mu.Unlock()
}
func (m *countMetrics) FrameSent(string, int)         { m.sent.Add(1) }
func (m *countMetrics) SendError(string, error)       { m.errs.Add(1) }
func (m *countMetrics) ASDUReceived(string)           { m.asdus.Add(1) }
func (m *countMetrics) Event(string, EventKind, bool) { m.events.Add(1) }

func pubConfig() PublisherConfig {
	return PublisherConfig{
		Dst: testDst, AppID: 0x4000, SvID: "MU01", DatSet: "IED1MU/LLN0$PhsMeas1", ConfRev: 1,
		SmpRate: 4000, SmpMod: SmpPerSec, Layout: LELayout, SmpSynch: SmpSynchLocal,
	}
}

// runPair starts a publisher and a subscriber on a hub.
func runPair(t *testing.T, pcfg PublisherConfig, scfg SubscriptionConfig) (*Publisher, *Subscription, context.CancelFunc) {
	t.Helper()
	hub := NewHub()
	p, err := NewPublisher(hub.NewPort(MAC{}), pcfg)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
	sub, err := s.Subscribe(scfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = s.Run(ctx) }()
	go func() { defer wg.Done(); _ = p.Run(ctx) }()
	return p, sub, func() { cancel(); wg.Wait() }
}

func TestPublisherRateAndSequence(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var cnts []uint16
		p, sub, stop := runPair(t, pubConfig(), SubscriptionConfig{SvID: "MU01", Layout: LELayout, OnASDU: func(s *Sample) {
			mu.Lock()
			cnts = append(cnts, s.ASDU.SmpCnt)
			mu.Unlock()
		}})
		time.Sleep(300 * time.Millisecond) // fake time: 1200 sample periods
		synctest.Wait()                    // every sample sent is delivered
		stop()
		st := p.State()
		// In the bubble the clock moves only when every goroutine waits:
		// the publisher sends every sample period on time.
		if st.Samples < 1200 || st.Samples > 1201 || st.Frames != st.Samples || st.Rate != 4000 || st.Skipped != 0 {
			t.Fatalf("publisher state %+v", st)
		}
		mu.Lock()
		defer mu.Unlock()
		if uint64(len(cnts)) != st.Samples {
			t.Fatalf("received %d samples, %d sent", len(cnts), st.Samples)
		}
		for i := 1; i < len(cnts); i++ {
			if cnts[i] != (cnts[i-1]+1)%4000 && st.Skipped == 0 {
				t.Fatalf("smpCnt %d after %d", cnts[i], cnts[i-1])
			}
		}
		// A skip moves smpCnt on, so the subscriber counts it as lost at the
		// next frame. Samples skipped after the last frame sent (a loaded host,
		// just before stop) have no next frame: Lost is the gaps the subscriber
		// saw, at most what the publisher skipped.
		ss := sub.Stats()[0]
		gaps := uint64(0)
		for i := 1; i < len(cnts); i++ {
			gaps += uint64((int(cnts[i]) - int(cnts[i-1]) - 1 + 4000) % 4000)
		}
		if ss.Lost != gaps || ss.Lost > st.Skipped || ss.Duplicates != 0 || ss.OutOfOrder != 0 {
			t.Errorf("subscriber stats %+v (smpCnt gaps %d, publisher skipped %d)", ss, gaps, st.Skipped)
		}
	})
}

func TestPublisherBatchingWrapAndSource(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg := pubConfig()
		cfg.SmpRate, cfg.SmpMod, cfg.NoASDU, cfg.SmpCntWrap = 2000, SmpPerSec, 8, 10
		cfg.RefrTm, cfg.SendSmpRate, cfg.SendSmpMod = true, true, true
		cfg.GmIdentity = &[8]byte{1}
		cfg.Layout = nil
		cfg.Source = func(info SampleInfo, dst []byte) []byte {
			var s LESample
			s.Current[0].Value = int32(info.Index)
			return s.Append(dst)
		}
		var mu sync.Mutex
		var frames []int
		var lastIdx int32 = -1
		var mono = true
		_, sub, stop := runPair(t, cfg, SubscriptionConfig{SmpCntWrap: 10, OnASDU: func(s *Sample) {
			mu.Lock()
			defer mu.Unlock()
			if s.Index == 0 {
				frames = append(frames, len(s.Frame.PDU.ASDUs))
			}
			a := s.ASDU
			if !a.HasRefrTm || !a.HasSmpRate || a.SmpRate != 2000 || !a.HasSmpMod || !a.HasGmIdentity {
				t.Errorf("optional fields %+v", a)
			}
			var le LESample
			_ = DecodeLE(a.Sample, &le)
			if le.Current[0].Value != lastIdx+1 {
				mono = false
			}
			lastIdx = le.Current[0].Value
			if int(a.SmpCnt) != int(le.Current[0].Value)%10 {
				t.Errorf("smpCnt %d for index %d", a.SmpCnt, le.Current[0].Value)
			}
		}})
		time.Sleep(200 * time.Millisecond) // fake time: 50 frames of 8 ASDUs
		stop()
		mu.Lock()
		defer mu.Unlock()
		if len(frames) < 20 {
			t.Fatalf("frames %d", len(frames))
		}
		for _, n := range frames {
			if n != 8 {
				t.Fatalf("frame with %d ASDUs", n)
			}
		}
		if st := sub.Stats()[0]; st.Lost != 0 && mono {
			t.Errorf("lost %d with a monotonic source", st.Lost)
		}
	})
}

func TestPublisherUpdateEnableSynch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg := pubConfig()
		cfg.SmpRate = 1000
		cfg.Disabled = true
		var mu sync.Mutex
		var last LESample
		var synch uint8
		var n int
		p, _, stop := runPair(t, cfg, SubscriptionConfig{OnASDU: func(s *Sample) {
			mu.Lock()
			defer mu.Unlock()
			_ = DecodeLE(s.ASDU.Sample, &last)
			synch = s.ASDU.SmpSynch
			n++
		}})
		defer stop()
		time.Sleep(30 * time.Millisecond) // fake time: 30 sample periods
		mu.Lock()
		if n != 0 {
			t.Fatalf("disabled publisher sent %d", n)
		}
		mu.Unlock()
		if p.State().Enabled {
			t.Fatal("enabled")
		}

		var s LESample
		s.Current[2] = Point{Value: 42, Q: QualityTest}
		vals, _ := LELayout.Decode(s.Append(nil))
		if err := p.Update(vals); err != nil {
			t.Fatal(err)
		}
		p.SetSmpSynch(SmpSynchGlobal)
		p.SetSimulation(false)
		p.SetEnabled(true)
		waitFor(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return n > 5 && last.Current[2].Value == 42 && synch == SmpSynchGlobal
		})
		s.Current[2].Value = 43
		if err := p.UpdateSample(s.Append(nil)); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return last.Current[2].Value == 43
		})
		if err := p.UpdateSample([]byte{1}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("short sample: %v", err)
		}
		if err := p.Update([]*mms.Value{mms.NewInteger(1)}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("bad values: %v", err)
		}
		p.SetEnabled(false)
		synctest.Wait() // what was sent before has been delivered
		mu.Lock()
		before := n
		mu.Unlock()
		time.Sleep(30 * time.Millisecond) // fake time: 30 sample periods
		mu.Lock()
		after := n
		mu.Unlock()
		if after != before {
			t.Errorf("sent %d samples while disabled", after-before)
		}
		if st := p.State(); st.Enabled || st.SmpSynch != SmpSynchGlobal {
			t.Errorf("state %+v", st)
		}
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestPublisherConfigErrors(t *testing.T) {
	hub := NewHub()
	tr := hub.NewPort(MAC{})
	mod := []func(c *PublisherConfig){
		func(c *PublisherConfig) { c.SvID = "" },
		func(c *PublisherConfig) { c.SvID = "\x01" },
		func(c *PublisherConfig) { c.DatSet = "\x01" },
		func(c *PublisherConfig) { c.SvID = strings.Repeat("A", MaxIDLength+1) },
		func(c *PublisherConfig) { c.DatSet = strings.Repeat("A", MaxIDLength+1) },
		func(c *PublisherConfig) { c.SmpRate = 0 },
		func(c *PublisherConfig) { c.SmpMod = 9 },
		func(c *PublisherConfig) { c.NominalFrequency = -1 },
		func(c *PublisherConfig) { c.NoASDU = -1 },
		func(c *PublisherConfig) { c.VLAN = &VLAN{ID: 0x2000} },
		func(c *PublisherConfig) { c.Layout = Layout{Kind(99)} },
		func(c *PublisherConfig) { c.SmpCntWrap = 70000 },
		func(c *PublisherConfig) { c.Sample = []byte{1} },
	}
	for i, m := range mod {
		c := pubConfig()
		m(&c)
		if _, err := NewPublisher(tr, c); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: %v", i, err)
		}
	}
	if _, err := NewPublisher(nil, pubConfig()); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("nil transport: %v", err)
	}
	// A frame that cannot fit.
	c := pubConfig()
	c.Layout, c.Sample, c.NoASDU = nil, make([]byte, 400), 4
	if _, err := NewPublisher(tr, c); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("oversize: %v", err)
	}
	// Update without a layout.
	c = pubConfig()
	c.Layout, c.Sample = nil, []byte{1, 2}
	p, err := NewPublisher(tr, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Update(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("Update without layout: %v", err)
	}
	if p.Config().Src != tr.HardwareAddr() || p.Config().NoASDU != 1 || p.Config().NominalFrequency != DefaultNominalFrequency {
		t.Errorf("defaults %+v", p.Config())
	}
	// Run after Close and concurrent Run.
	_ = p.Close()
	_ = p.Close()
	if err := p.Run(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("Run after Close: %v", err)
	}
	p2, _ := NewPublisher(tr, pubConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p2.Run(ctx) }()
	waitFor(t, func() bool { return p2.State().Frames > 0 })
	if err := p2.Run(ctx); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("second Run: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestSamplesPerSecond(t *testing.T) {
	if SamplesPerSecond(80, SmpPerPeriod, 0) != 4000 || SamplesPerSecond(80, SmpPerPeriod, 60) != 4800 ||
		SamplesPerSecond(4000, SmpPerSec, 50) != 4000 || SamplesPerSecond(2, SecPerSmp, 50) != 0.5 {
		t.Error("rates")
	}
	if DefaultSmpCntWrap(4800) != 4800 || DefaultSmpCntWrap(0.5) != 65536 || DefaultSmpCntWrap(1e6) != 65536 || DefaultSmpCntWrap(4000.5) != 65536 {
		t.Error("wraps")
	}
}

// failTransport rejects every send.
type failTransport struct {
	Transport
	err error
}

func (f *failTransport) Send([]byte) error { return f.err }

func TestPublisherSendErrors(t *testing.T) {
	t.Parallel()
	m := &countMetrics{}
	hub := NewHub()
	cfg := pubConfig()
	cfg.Metrics = m
	p, _ := NewPublisher(&failTransport{Transport: hub.NewPort(MAC{}), err: errors.New("link down")}, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	waitFor(t, func() bool { return p.State().SendErrors > 3 })
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
	if m.errs.Load() == 0 {
		t.Error("SendError metric")
	}
	// ErrClosed from the transport stops Run.
	p2, _ := NewPublisher(&failTransport{Transport: hub.NewPort(MAC{}), err: ErrClosed}, pubConfig())
	if err := p2.Run(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("Run on closed transport: %v", err)
	}
}

// TestPublisherCatchUp drives the publisher with a clock that jumps ahead:
// it sends a burst, then skips the samples it cannot catch up with.
func TestPublisherCatchUp(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	hub := NewHub()
	rx := hub.NewPort(MAC{})
	cfg := pubConfig()
	cfg.Now = clk.Now
	cfg.MaxBurst = 4
	cfg.AlignSecond = true
	p, _ := NewPublisher(hub.NewPort(MAC{}), cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	// Advance the clock until the publisher falls behind a full burst.
	waitFor(t, func() bool { clk.Add(200 * time.Millisecond); return p.State().Skipped > 0 })
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
	st := p.State()
	if st.Frames < 4 || st.Late == 0 {
		t.Errorf("state %+v", st)
	}
	f, err := rx.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	g, err := Decode(f)
	if err != nil || g.PDU.ASDUs[0].SmpCnt != 0 {
		t.Errorf("first aligned sample: %v %+v", err, g)
	}
}

// TestSamplesChannelOwnsData checks that samples sent on the channel do
// not alias the subscriber's reused frame.
func TestSamplesChannelOwnsData(t *testing.T) {
	s, _ := NewSubscriber(nil, SubscriberConfig{})
	sub, _ := s.Subscribe(SubscriptionConfig{SampleQueue: 4})
	s.HandleFrame(frameWith(t, func(f *Frame) { f.VLAN = &VLAN{Priority: 4, ID: 1} }, 0))
	s.HandleFrame(frameWith(t, func(f *Frame) { f.VLAN = &VLAN{Priority: 6, ID: 2}; f.PDU.ASDUs[0].SvID = "MU02" }, 1))
	a, b := <-sub.Samples(), <-sub.Samples()
	if a.Frame.VLAN.ID != 1 || b.Frame.VLAN.ID != 2 || a.ASDU.SvID != "MU01" || a.ASDU.SmpCnt != 0 || Int32At(a.ASDU.Sample, 0) != 0 {
		t.Fatalf("first sample changed: %+v %+v", a.Frame.VLAN, a.ASDU)
	}
}

// TestSpec_9_2_Table14_SmpCntResetResync: 9-2 Table 14 smpCnt "shall be
// set to zero" by a synchronising pulse; the subscriber must not drop
// the samples that follow (SV-01).
func TestSpec_9_2_Table14_SmpCntResetResync(t *testing.T) {
	now := time.Unix(1000, 0)
	s, _ := NewSubscriber(nil, SubscriberConfig{Now: func() time.Time { return now }})
	delivered := 0
	sub, _ := s.Subscribe(SubscriptionConfig{SvID: "MU01", SmpCntWrap: 4000, OnASDU: func(*Sample) { delivered++ }})
	send := func(c uint16) {
		t.Helper()
		f := &Frame{Dst: MAC{1, 0x0c, 0xcd, 4, 0, 1}, AppID: 0x4000, PDU: PDU{ASDUs: []ASDU{{SvID: "MU01", SmpCnt: c, ConfRev: 1, Sample: make([]byte, 8)}}}}
		b, err := Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		s.HandleFrame(b)
		now = now.Add(250 * time.Microsecond)
	}
	for c := uint16(990); c <= 1000; c++ {
		send(c)
	}
	for c := uint16(0); c < 100; c++ {
		send(c)
	}
	if delivered != 111 {
		t.Errorf("delivered %d, want 111", delivered)
	}
	st := sub.Stats()[0]
	if st.OutOfOrder != 0 || st.Resyncs != 1 || st.LastSmpCnt != 99 {
		t.Errorf("stats %+v", st)
	}
	var resync int
	for _, e := range drainEvents(sub) {
		if e.Kind == EventResync {
			resync++
			if e.SmpCnt != 0 || e.Lost != 0 {
				t.Errorf("resync event %+v", e)
			}
		}
	}
	if resync != 1 {
		t.Errorf("resync events %d", resync)
	}

	// A genuine reorder (1000, 998) stays out of order, but a counter
	// that jumped back is followed after ResyncAfter frames.
	for _, c := range []uint16{1000, 998, 1001} {
		send(c)
	}
	if st := sub.Stats()[0]; st.OutOfOrder != 1 || st.LastSmpCnt != 1001 {
		t.Errorf("reorder stats %+v", st)
	}
	before := delivered
	for c := uint16(500); c < 510; c++ {
		send(c)
	}
	if got := delivered - before; got != 10-(ResyncAfter-1) {
		t.Errorf("after jump back delivered %d", got)
	}
	if st := sub.Stats()[0]; st.Resyncs != 2 || st.OutOfOrder != uint64(1+ResyncAfter-1) || st.LastSmpCnt != 509 {
		t.Errorf("jump stats %+v", st)
	}
	if EventResync.String() != "resync" {
		t.Error(EventResync.String())
	}
}

// TestSV_SimulationPerStream: with AcceptSimulation, once a simulated
// ASDU of a stream arrived the real ASDUs of that stream are ignored
// (7-4 LPHD.Sim, as for GOOSE), so the two counters are not
// interleaved (SV-07).
func TestSV_SimulationPerStream(t *testing.T) {
	now := time.Unix(1000, 0)
	m := &countMetrics{}
	s, _ := NewSubscriber(nil, SubscriberConfig{Now: func() time.Time { return now }, AcceptSimulation: true, Metrics: m})
	var got []uint16
	sub, _ := s.Subscribe(SubscriptionConfig{SvID: "MU01", SmpCntWrap: 4000, OnASDU: func(x *Sample) { got = append(got, x.ASDU.SmpCnt) }})
	send := func(c uint16, sim bool) {
		f := &Frame{Dst: MAC{1, 0x0c, 0xcd, 4, 0, 1}, AppID: 0x4000, Simulation: sim, PDU: PDU{ASDUs: []ASDU{{SvID: "MU01", SmpCnt: c, ConfRev: 1, Sample: make([]byte, 8)}}}}
		b, err := Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		s.HandleFrame(b)
	}
	send(0, false) // real stream before simulation starts
	for i := range uint16(10) {
		send(i+1, false)
		send(500+i, true)
	}
	if len(got) != 12 || got[0] != 0 || got[1] != 1 || got[2] != 500 || got[11] != 509 {
		t.Errorf("delivered %v", got)
	}
	st := sub.Stats()[0]
	if st.OutOfOrder != 0 || st.Lost != 498 || !st.Simulation {
		t.Errorf("stats %+v", st)
	}
	if m.dropped["simulation"] != 9 {
		t.Errorf("dropped %v", m.dropped)
	}
}
