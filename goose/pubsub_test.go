// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

var testDst = MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01}

func pubConfig() PublisherConfig {
	return PublisherConfig{
		Dst: testDst, AppID: 0x1000, GocbRef: "LD/LLN0$GO$gcb1", DatSet: "LD/LLN0$ds1",
		GoID: "gcb1", ConfRev: 1, MinTime: 5 * time.Millisecond, MaxTime: 40 * time.Millisecond,
		Values: []*mms.Value{mms.NewBoolean(false), mms.NewInteger(1)},
	}
}

// recvFrames collects decoded frames from port until ctx is done.
func recvFrames(ctx context.Context, t *testing.T, port Transport) <-chan *Frame {
	out := make(chan *Frame, 1024)
	go func() {
		defer close(out)
		for {
			raw, err := port.Recv(ctx)
			if err != nil {
				return
			}
			f, err := Decode(raw)
			if err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			out <- f
		}
	}()
	return out
}

func nextFrame(t *testing.T, ch <-chan *Frame) *Frame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for frame")
		return nil
	}
}

// stampTransport records the send time of every frame.
type stampTransport struct {
	Transport
	mu    sync.Mutex
	times []time.Time
}

func (s *stampTransport) Send(b []byte) error {
	s.mu.Lock()
	s.times = append(s.times, time.Now())
	s.mu.Unlock()
	return s.Transport.Send(b)
}

func TestPublisherRetransmissionCurve(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	tx := &stampTransport{Transport: hub.NewPort(MAC{})}
	rx := hub.NewPort(MAC{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := recvFrames(ctx, t, rx)
	p, err := NewPublisher(tx, pubConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	for i := 0; i < 7; i++ {
		f := nextFrame(t, frames)
		if f.PDU.StNum != 1 || f.PDU.SqNum != uint32(i) {
			t.Fatalf("frame %d: stNum=%d sqNum=%d", i, f.PDU.StNum, f.PDU.SqNum)
		}
		if f.Src != tx.HardwareAddr() || f.PDU.TimeAllowedToLive != 80 || f.PDU.GoID != "gcb1" {
			t.Fatalf("frame %d: %+v", i, f)
		}
	}
	// Intervals: 5, 10, 20, 40, 40, 40 ms (timers only fire late, so the
	// lower bound is strict and the upper bound loose for CI).
	tx.mu.Lock()
	times := append([]time.Time(nil), tx.times...)
	tx.mu.Unlock()
	want := []time.Duration{5, 10, 20, 40, 40, 40}
	for i, w := range want {
		got := times[i+1].Sub(times[i])
		if got < w*time.Millisecond-time.Millisecond || got > w*time.Millisecond*4+20*time.Millisecond {
			t.Errorf("interval %d = %v, want ~%dms", i, got, w)
		}
	}

	if err := p.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewInteger(2)}); err != nil {
		t.Fatal(err)
	}
	for {
		f := nextFrame(t, frames)
		if f.PDU.StNum == 2 {
			if f.PDU.SqNum != 0 || !f.PDU.AllData[0].Equal(mms.NewBoolean(true)) {
				t.Fatalf("new state frame: %+v", f.PDU)
			}
			break
		}
	}
	st := p.State()
	if st.StNum != 2 || !st.Enabled || st.Sent < 8 {
		t.Errorf("state = %+v", st)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPublisherValidation(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	port := hub.NewPort(MAC{})
	bad := []func(c *PublisherConfig){
		func(c *PublisherConfig) { c.Dst = MAC{} },
		func(c *PublisherConfig) { c.GocbRef = "" },
		func(c *PublisherConfig) { c.DatSet = "" },
		func(c *PublisherConfig) { c.VLAN = &VLAN{ID: 5000} },
		func(c *PublisherConfig) { c.MinTime = -1 },
		func(c *PublisherConfig) { c.MinTime = time.Second; c.MaxTime = time.Millisecond },
		func(c *PublisherConfig) { c.TimeAllowedToLive = 1 << 62 },
	}
	for i, mod := range bad {
		c := pubConfig()
		mod(&c)
		if _, err := NewPublisher(port, c); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
	if _, err := NewPublisher(nil, pubConfig()); !errors.Is(err, ErrInvalidConfig) {
		t.Error(err)
	}
	c := pubConfig()
	c.Values = []*mms.Value{mms.NewReal(1)}
	if _, err := NewPublisher(port, c); !errors.Is(err, ErrInvalidValue) {
		t.Error(err)
	}
	// NdsCom allows an empty DatSet; defaults applied.
	c = pubConfig()
	c.DatSet, c.NdsCom, c.GoID, c.MinTime, c.MaxTime = "", true, "", 0, 0
	p, err := NewPublisher(port, c)
	if err != nil {
		t.Fatal(err)
	}
	eff := p.Config()
	if eff.GoID != c.GocbRef || eff.MinTime != DefaultMinTime || eff.MaxTime != DefaultMaxTime || eff.TimeAllowedToLive != 2*DefaultMaxTime || eff.Src != port.HardwareAddr() {
		t.Errorf("defaults: %+v", eff)
	}
}

func TestPublisherPublishErrorsAndFlags(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	tx, rx := hub.NewPort(MAC{}), hub.NewPort(MAC{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := recvFrames(ctx, t, rx)
	c := pubConfig()
	c.Values = nil
	c.MaxTime = time.Hour
	c.MinTime = time.Hour
	p, err := NewPublisher(tx, c)
	if err != nil {
		t.Fatal(err)
	}
	// Flags before the first state are stored silently.
	if err := p.SetFlags(true, false, false); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish([]*mms.Value{mms.NewBoolean(true)}); err != nil {
		t.Fatal(err)
	}
	if f := nextFrame(t, frames); f.PDU.StNum != 1 || !f.PDU.Test {
		t.Fatalf("first: %+v", f.PDU)
	}
	if err := p.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewBoolean(true)}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("count: %v", err)
	}
	if err := p.Publish([]*mms.Value{mms.NewInteger(1)}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("type: %v", err)
	}
	if err := p.Publish([]*mms.Value{nil}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("nil: %v", err)
	}
	if err := p.SetFlags(true, true, true); err != nil {
		t.Fatal(err)
	}
	if f := nextFrame(t, frames); f.PDU.StNum != 2 || !f.Simulation || !f.PDU.NdsCom {
		t.Fatalf("flags: %+v", f)
	}
	if err := p.SetFlags(true, true, true); err != nil { // no change, no frame
		t.Fatal(err)
	}
	if err := p.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := p.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish([]*mms.Value{mms.NewBoolean(false)}); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-frames:
		t.Fatalf("frame while disabled: %+v", f.PDU)
	case <-time.After(30 * time.Millisecond):
	}
	if err := p.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if f := nextFrame(t, frames); f.PDU.StNum != 4 || !f.PDU.AllData[0].Equal(mms.NewBoolean(false)) {
		t.Fatalf("re-enable: %+v", f.PDU)
	}
	_ = p.Close()
	_ = p.Close()
	for name, err := range map[string]error{
		"publish": p.Publish(nil), "flags": p.SetFlags(false, false, false),
		"enable": p.SetEnabled(false), "run": p.Run(ctx),
	} {
		if !errors.Is(err, ErrClosed) {
			t.Errorf("%s after close: %v", name, err)
		}
	}
}

type failingTransport struct {
	Transport
	fail bool
	mu   sync.Mutex
	n    int
}

func (f *failingTransport) Send(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.fail {
		return errors.New("link down")
	}
	return nil
}

type countingMetrics struct {
	NopMetrics
	mu                         sync.Mutex
	sent, retrans, errs, recvd int
	dropped                    map[string]int
	events                     map[EventKind]int
	droppedEvents              int
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{dropped: map[string]int{}, events: map[EventKind]int{}}
}

func (m *countingMetrics) FrameSent(_ string, r bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent++
	if r {
		m.retrans++
	}
}
func (m *countingMetrics) SendError(string, error) { m.mu.Lock(); m.errs++; m.mu.Unlock() }
func (m *countingMetrics) FrameReceived(string)    { m.mu.Lock(); m.recvd++; m.mu.Unlock() }
func (m *countingMetrics) FrameDropped(r string)   { m.mu.Lock(); m.dropped[r]++; m.mu.Unlock() }
func (m *countingMetrics) Event(_ string, k EventKind, d bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[k]++
	if d {
		m.droppedEvents++
	}
}

func TestPublisherSendErrorsKeepHeartbeat(t *testing.T) {
	t.Parallel()
	ft := &failingTransport{Transport: NewHub().NewPort(MAC{}), fail: true}
	m := newCountingMetrics()
	c := pubConfig()
	c.Metrics = m
	p, err := NewPublisher(ft, c)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)
	ft.mu.Lock()
	n := ft.n
	ft.mu.Unlock()
	if n < 3 {
		t.Errorf("only %d send attempts", n)
	}
	if err := p.Publish(c.Values); err == nil {
		t.Error("Publish did not report send error")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errs < 3 || m.sent != 0 {
		t.Errorf("metrics: %+v", m)
	}
}

// subHarness feeds frames directly into a subscriber.
type subHarness struct {
	t   *testing.T
	s   *Subscriber
	sub *Subscription
	now time.Time
	mu  sync.Mutex
}

func newSubHarness(t *testing.T, cfg SubscriptionConfig, scfg SubscriberConfig) *subHarness {
	h := &subHarness{t: t, now: time.Unix(1000, 0)}
	scfg.Now = func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now }
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), scfg)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Subscribe(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.s, h.sub = s, sub
	t.Cleanup(func() { _ = s.Close() })
	return h
}

func (h *subHarness) feed(mod func(f *Frame)) {
	h.t.Helper()
	f := &Frame{
		Dst: testDst, Src: MAC{2, 0, 0, 0, 0, 9}, AppID: 0x1000,
		PDU: PDU{GocbRef: "LD/LLN0$GO$gcb1", DatSet: "LD/LLN0$ds1", GoID: "gcb1", TimeAllowedToLive: 60000,
			T: time.Unix(0, 0), StNum: 1, ConfRev: 1, AllData: []*mms.Value{mms.NewBoolean(true)}},
	}
	if mod != nil {
		mod(f)
	}
	h.s.HandleFrame(mustEncode(h.t, f))
}

func (h *subHarness) seq(st, sq uint32) {
	h.feed(func(f *Frame) { f.PDU.StNum, f.PDU.SqNum = st, sq })
}

// drain returns the kinds of all queued events.
func (h *subHarness) drain() []Event {
	var out []Event
	for {
		select {
		case ev := <-h.sub.Events():
			out = append(out, ev)
		default:
			return out
		}
	}
}

func kinds(evs []Event) []EventKind {
	k := make([]EventKind, len(evs))
	for i, e := range evs {
		k[i] = e.Kind
	}
	return k
}

func eqKinds(a []EventKind, b ...EventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSubscriberSequenceSupervision(t *testing.T) {
	t.Parallel()
	h := newSubHarness(t, SubscriptionConfig{GocbRef: "LD/LLN0$GO$gcb1", ConfRev: 1, DatSet: "LD/LLN0$ds1", GoID: "gcb1",
		Members: []mms.TypeSpec{{Type: mms.ValueTypeBoolean}}}, SubscriberConfig{})

	steps := []struct {
		st, sq uint32
		want   []EventKind
	}{
		{1, 0, []EventKind{EventSupervision, EventMessage}},
		{1, 1, []EventKind{EventMessage}},
		{1, 1, []EventKind{EventDuplicate}},
		{1, 3, []EventKind{EventSqNumGap, EventMessage}},
		{1, 2, []EventKind{EventOutOfOrder}},
		{2, 0, []EventKind{EventMessage}},
		{5, 0, []EventKind{EventStNumGap, EventMessage}},
		{6, 2, []EventKind{EventSqNumGap, EventMessage}},
		{4, 0, []EventKind{EventOutOfOrder}},
		{1, 0, []EventKind{EventRestart, EventMessage}},
		{0x80000000, 0, []EventKind{EventStNumGap, EventMessage}},
		{0xffffffff, 0, []EventKind{EventStNumGap, EventMessage}},
		{0xffffffff, 0x7fffffff, []EventKind{EventSqNumGap, EventMessage}},
		{0xffffffff, 0xfffffffe, []EventKind{EventSqNumGap, EventMessage}},
		{0xffffffff, 0xffffffff, []EventKind{EventMessage}},
		{0xffffffff, 1, []EventKind{EventMessage}}, // sqNum rollover to 1
		{1, 0, []EventKind{EventMessage}},          // stNum rollover to 1
	}
	for i, s := range steps {
		h.seq(s.st, s.sq)
		evs := h.drain()
		if !eqKinds(kinds(evs), s.want...) {
			t.Fatalf("step %d (%d,%d): events %v, want %v", i, s.st, s.sq, kinds(evs), s.want)
		}
	}
	sup := h.sub.Supervision()
	if len(sup) != 1 {
		t.Fatalf("streams = %d", len(sup))
	}
	s := sup[0]
	if s.Status != StatusOK || !s.St || s.LastStNum != 1 || s.LastSqNum != 0 || s.Duplicates != 1 || s.OutOfOrder != 2 ||
		s.StNumGaps != 3 || s.SqNumGaps != 4 || s.Restarts != 1 || s.ConfRevNum != 1 || s.RxConfRevNum != 1 {
		t.Errorf("supervision = %+v", s)
	}
	if s.Stream.GocbRef != "LD/LLN0$GO$gcb1" || s.Stream.AppID != 0x1000 {
		t.Errorf("stream = %+v", s.Stream)
	}
}

func TestSubscriberValidation(t *testing.T) {
	t.Parallel()
	h := newSubHarness(t, SubscriptionConfig{ConfRev: 1, DatSet: "LD/LLN0$ds1", GoID: "gcb1",
		Members: []mms.TypeSpec{{Type: mms.ValueTypeBoolean}}}, SubscriberConfig{})
	cases := []struct {
		mod  func(f *Frame)
		want error
		st   Status
	}{
		{func(f *Frame) { f.PDU.ConfRev = 2 }, ErrConfRevMismatch, StatusInvalid},
		{func(f *Frame) { f.PDU.DatSet = "x" }, ErrDatSetMismatch, StatusInvalid},
		{func(f *Frame) { f.PDU.GoID = "x" }, ErrGoIDMismatch, StatusInvalid},
		{func(f *Frame) { f.PDU.NumDatSetEntries = 3 }, ErrMemberMismatch, StatusInvalid},
		{func(f *Frame) { f.PDU.AllData = []*mms.Value{mms.NewInteger(1)} }, ErrMemberMismatch, StatusInvalid},
		{func(f *Frame) { f.PDU.AllData = nil }, ErrMemberMismatch, StatusInvalid},
		{func(f *Frame) { f.PDU.NdsCom = true }, ErrNeedsCommissioning, StatusNeedsCommissioning},
	}
	for i, c := range cases {
		h.feed(c.mod)
		evs := h.drain()
		var inv *Event
		for j := range evs {
			if evs[j].Kind == EventInvalid {
				inv = &evs[j]
			}
		}
		if inv == nil || !errors.Is(inv.Err, c.want) || !errors.Is(inv.Err, ErrValidation) {
			t.Fatalf("case %d: events %v", i, evs)
		}
		if got := h.sub.Supervision()[0]; got.Status != c.st || got.St || got.LastErr == nil {
			t.Errorf("case %d: status %v", i, got.Status)
		}
	}
	// A valid frame recovers.
	h.seq(1, 0)
	evs := h.drain()
	if !eqKinds(kinds(evs), EventSupervision, EventMessage) || evs[0].State.Status != StatusOK {
		t.Fatalf("recovery: %v", kinds(evs))
	}
	if h.sub.Supervision()[0].Invalid != uint64(len(cases)) {
		t.Errorf("invalid counter %d", h.sub.Supervision()[0].Invalid)
	}
}

func TestSubscriberMatchesType(t *testing.T) {
	ts := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: []mms.TypeSpecElement{
		{Name: "q", Type: mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		{Name: "a", Type: mms.TypeSpec{Type: mms.ValueTypeArray, Count: 2, Element: &mms.TypeSpec{Type: mms.ValueTypeInteger}}},
	}}
	good := mms.NewStructure([]*mms.Value{mms.NewBitStringWithLength([]byte{0, 0}, 13), mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)})})
	if !MatchesType(good, ts) {
		t.Error("good value rejected")
	}
	bad := []*mms.Value{
		nil,
		mms.NewBoolean(true),
		mms.NewStructure(nil),
		mms.NewStructure([]*mms.Value{mms.NewBitStringWithLength([]byte{0}, 2), mms.NewArray(nil)}),
		mms.NewStructure([]*mms.Value{mms.NewBitStringWithLength([]byte{0, 0}, 13), mms.NewArray([]*mms.Value{mms.NewInteger(1)})}),
		mms.NewStructure([]*mms.Value{mms.NewBitStringWithLength([]byte{0, 0}, 13), mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)})}),
	}
	for i, v := range bad {
		if MatchesType(v, ts) {
			t.Errorf("bad %d accepted", i)
		}
	}
	if typeSpecString(ts) == "" || typeSpecString(mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}) == "" || typeSpecString(mms.TypeSpec{Type: mms.ValueTypeBoolean}) == "" {
		t.Error("typeSpecString")
	}
	if !SameShape(good, good.Clone()) || SameShape(good, mms.NewStructure(nil)) || SameShape(nil, good) || !SameShape(nil, nil) {
		t.Error("SameShape")
	}
	arr := mms.NewArray([]*mms.Value{mms.NewInteger(1)})
	if SameShape(arr, mms.NewArray(nil)) || SameShape(arr, mms.NewArray([]*mms.Value{mms.NewBoolean(true)})) || !SameShape(arr, arr) {
		t.Error("SameShape array")
	}
	if SameShape(good, mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewInteger(1)})) {
		t.Error("SameShape struct elems")
	}
}

func TestSubscriberFiltersAndSimulation(t *testing.T) {
	t.Parallel()
	m := newCountingMetrics()
	h := newSubHarness(t, SubscriptionConfig{Dst: testDst, AppIDs: []uint16{0x1000}, Src: MAC{2, 0, 0, 0, 0, 9}, GocbRef: "LD/LLN0$GO$gcb1"}, SubscriberConfig{Metrics: m})
	h.feed(func(f *Frame) { f.Dst = Broadcast })
	h.feed(func(f *Frame) { f.AppID = 2 })
	h.feed(func(f *Frame) { f.Src = MAC{2} })
	h.feed(func(f *Frame) { f.PDU.GocbRef = "x" })
	h.feed(func(f *Frame) { f.Simulation = true })
	if evs := h.drain(); len(evs) != 0 {
		t.Fatalf("filtered frames produced %v", kinds(evs))
	}
	m.mu.Lock()
	if m.dropped["filtered"] != 4 || m.dropped["simulation"] != 1 {
		t.Errorf("dropped = %v", m.dropped)
	}
	m.mu.Unlock()

	// Non-GOOSE and undecodable frames.
	var decodeErrs int
	h.s.cfg.OnDecodeError = func([]byte, error) { decodeErrs++ }
	h.s.HandleFrame(make([]byte, 60))                                                              // EtherType 0: ignored
	h.s.HandleFrame([]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 0x88, 0xb8, 0, 1, 0, 9, 0, 0, 0, 0}) // bad
	if decodeErrs != 1 {
		t.Errorf("decode errors = %d", decodeErrs)
	}

	// Simulation mode: simulated frames accepted, then real ones ignored.
	h2 := newSubHarness(t, SubscriptionConfig{AcceptSimulation: true}, SubscriberConfig{})
	h2.seq(1, 0)
	h2.feed(func(f *Frame) { f.Simulation = true; f.PDU.StNum = 2 })
	h2.seq(3, 0)
	evs := h2.drain()
	if !eqKinds(kinds(evs), EventSupervision, EventMessage, EventMessage) {
		t.Fatalf("simulation events %v", kinds(evs))
	}
	if s := h2.sub.Supervision()[0]; !s.SimSt || s.LastStNum != 2 {
		t.Errorf("sim supervision %+v", s)
	}
}

func TestSubscriberOnlyStateChangesAndOverflow(t *testing.T) {
	t.Parallel()
	m := newCountingMetrics()
	h := newSubHarness(t, SubscriptionConfig{OnlyStateChanges: true, QueueSize: 2}, SubscriberConfig{Metrics: m})
	h.seq(1, 0) // supervision + message: fills queue
	h.seq(1, 1) // retransmission: no event
	h.seq(2, 0) // dropped (queue full)
	if s := h.sub.Supervision()[0]; s.DroppedEvents != 1 || s.Messages != 3 || s.StateChanges != 2 {
		t.Errorf("supervision %+v", s)
	}
	m.mu.Lock()
	if m.droppedEvents != 1 {
		t.Errorf("metrics dropped events = %d", m.droppedEvents)
	}
	m.mu.Unlock()
	if _, err := h.s.Subscribe(SubscriptionConfig{QueueSize: -1}); !errors.Is(err, ErrInvalidConfig) {
		t.Error(err)
	}
}

func TestSubscriberTimeoutAndRecovery(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var got []Event
	h := newSubHarness(t, SubscriptionConfig{Handler: func(e Event) { mu.Lock(); got = append(got, e); mu.Unlock() }}, SubscriberConfig{})
	h.feed(func(f *Frame) { f.PDU.TimeAllowedToLive = 20 })
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 4
	})
	h.seq(2, 0)
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 6
	})
	mu.Lock()
	k := kinds(got)
	mu.Unlock()
	if !eqKinds(k, EventSupervision, EventMessage, EventTimeout, EventSupervision, EventSupervision, EventMessage) {
		t.Fatalf("events %v", k)
	}
	if got[3].State.Status != StatusTimeout || got[3].Frame != nil || got[4].State.Status != StatusOK {
		t.Errorf("statuses %v %v", got[3].State.Status, got[4].State.Status)
	}
	if s := h.sub.Supervision()[0]; s.Timeouts != 1 {
		t.Errorf("timeouts %d", s.Timeouts)
	}
	// TAL 0 disables supervision timer.
	h.feed(func(f *Frame) { f.PDU.StNum = 3; f.PDU.TimeAllowedToLive = 0 })
	time.Sleep(30 * time.Millisecond)
	if s := h.sub.Supervision()[0]; s.Timeouts != 1 || s.Status != StatusOK {
		t.Errorf("after TAL 0: %+v", s)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestSubscriberLifecycle(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	port := hub.NewPort(MAC{})
	if _, err := NewSubscriber(nil, SubscriberConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Error(err)
	}
	s, err := NewSubscriber(port, SubscriberConfig{Limits: Limits{MaxDepth: -1}})
	if err != nil {
		t.Fatal(err)
	}
	sub1, _ := s.Subscribe(SubscriptionConfig{})
	sub2, _ := s.Subscribe(SubscriptionConfig{GocbRef: "LD/LLN0$GO$gcb1"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	pub, _ := NewPublisher(hub.NewPort(MAC{}), pubConfig())
	go func() { _ = pub.Run(ctx) }()
	for _, sub := range []*Subscription{sub1, sub2} {
		select {
		case ev := <-sub.Events():
			if ev.Kind != EventSupervision {
				t.Errorf("first event %v", ev.Kind)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no event")
		}
	}
	if err := sub1.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-drainUntilClosed(sub1.Events()); ok {
		t.Error("events channel not closed")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	_ = s.Close()
	if _, err := s.Subscribe(SubscriptionConfig{}); !errors.Is(err, ErrClosed) {
		t.Error(err)
	}
	// Run returns nil when the transport closes, and the transport's
	// error otherwise.
	_ = port.Close()
	if err := s.Run(context.Background()); err != nil {
		t.Error(err)
	}
	s2, _ := NewSubscriber(errTransport{}, SubscriberConfig{})
	if err := s2.Run(context.Background()); err == nil || errors.Is(err, ErrClosed) {
		t.Error(err)
	}
}

type errTransport struct{ Transport }

func (errTransport) Recv(context.Context) ([]byte, error) { return nil, errors.New("boom") }

func drainUntilClosed(ch <-chan Event) <-chan Event {
	out := make(chan Event)
	go func() {
		for range ch {
		}
		close(out)
	}()
	return out
}

func TestEventAndStatusStrings(t *testing.T) {
	for k := EventMessage; k <= EventSupervision; k++ {
		if k.String() == "" {
			t.Error(k)
		}
	}
	if EventKind(99).String() != "EventKind(99)" {
		t.Error(EventKind(99).String())
	}
	for s, want := range map[Status]string{StatusWaiting: "waiting", StatusOK: "ok", StatusTimeout: "timeout", StatusInvalid: "invalid", StatusNeedsCommissioning: "ndscom", Status(9): "Status(9)"} {
		if s.String() != want {
			t.Errorf("%d: %s", s, s.String())
		}
	}
}
