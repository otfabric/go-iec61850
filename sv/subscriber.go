// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"
)

// DefaultTimeout is the default stream supervision timeout.
const DefaultTimeout = 100 * time.Millisecond

// DefaultEventQueue is the default event channel capacity.
const DefaultEventQueue = 256

// rateWindow is the window of the rate estimator.
const rateWindow = 500 * time.Millisecond

// SubscriberConfig configures a [Subscriber].
type SubscriberConfig struct {
	// Limits bounds the decoder (default [DefaultLimits]).
	Limits Limits

	// AcceptSimulation accepts frames with the simulation bit set. By
	// default they are dropped, as by a device not in simulation mode
	// (LPHD.Sim false).
	AcceptSimulation bool

	// SupervisionInterval is how often Run checks stream timeouts
	// (default 10 ms).
	SupervisionInterval time.Duration

	// OnDecodeError, when non-nil, is called for every frame with EtherType
	// 0x88BA that fails to decode.
	OnDecodeError func(raw []byte, err error)

	// Now is the clock (default time.Now).
	Now func() time.Time

	// Logger receives diagnostics; nil disables logging.
	Logger *slog.Logger

	// Metrics receives counters; nil disables metrics.
	Metrics Metrics
}

// SubscriptionConfig selects and supervises one or more SV streams.
// Zero-valued filters match anything.
type SubscriptionConfig struct {
	// Dst and Src filter on the Ethernet addresses.
	Dst, Src MAC

	// AppIDs filters on the APPID.
	AppIDs []uint16

	// SvID filters on the svID.
	SvID string

	// Validation: DatSet (when non-empty, checked against ASDUs that
	// carry datSet), ConfRev (when non-zero),
	// SmpRate/SmpMod (when SmpRate is non-zero, checked against ASDUs
	// that carry smpRate) and SampleSize (when non-zero; Layout.Size()
	// when a Layout is given). An ASDU failing validation produces an
	// EventInvalid and is not delivered.
	DatSet     string
	ConfRev    uint32
	SmpRate    uint16
	SmpMod     SmpMod
	SampleSize int
	Layout     Layout

	// NominalFrequency converts a SmpPerPeriod rate (default 50 Hz).
	NominalFrequency float64

	// SmpCntWrap is the expected smpCnt modulus. Zero derives it from
	// SmpRate (or from the smpRate field of the stream); without either
	// the subscriber assumes 65536 and learns the modulus from the first
	// wrap to 0.
	SmpCntWrap int

	// Timeout is the supervision timeout: a stream that sends nothing
	// for this long reports EventTimeout (default [DefaultTimeout]).
	Timeout time.Duration

	// OnASDU, when non-nil, is called synchronously for every accepted
	// ASDU on the receive goroutine. The [Sample] and the slices it
	// references are only valid during the call; copy what you keep.
	// This path does not allocate.
	OnASDU func(s *Sample)

	// SampleQueue, when positive and OnASDU is nil, delivers accepted
	// ASDUs on [Subscription.Samples] as owned copies (allocating per
	// ASDU). When the queue is full, samples are dropped and counted in
	// Stats.QueueDrops.
	SampleQueue int

	// EventQueue is the capacity of the event channel (default
	// [DefaultEventQueue]). Events are dropped when it is full.
	EventQueue int
}

// StreamKey identifies a stream within a subscription.
type StreamKey struct {
	Src   MAC
	AppID uint16
	SvID  string
}

// String formats the key for logs.
func (k StreamKey) String() string {
	return k.SvID + "@" + k.Src.String() + "/0x" + strconv.FormatUint(uint64(k.AppID), 16)
}

// Sample is one received ASDU with its stream context.
type Sample struct {
	Stream   StreamKey
	Received time.Time

	// Frame is the frame the ASDU came in; Index is the ASDU's position
	// in it.
	Frame *Frame
	Index int

	// ASDU is the decoded ASDU (ASDU.Sample holds the dataset).
	ASDU *ASDU

	// Lost is the number of samples missing just before this one
	// (smpCnt gap), 0 normally.
	Lost int
}

// Status is the supervision status of a stream.
type Status int

// Stream statuses.
const (
	// StatusOK: samples arrive within the timeout.
	StatusOK Status = iota + 1
	// StatusTimeout: no sample within the timeout.
	StatusTimeout
	// StatusInvalid: the last ASDU failed validation.
	StatusInvalid
)

// String returns the status name.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusTimeout:
		return "timeout"
	case StatusInvalid:
		return "invalid"
	default:
		return "Status(" + strconv.Itoa(int(s)) + ")"
	}
}

// EventKind classifies a subscription [Event].
type EventKind int

// Event kinds.
const (
	// EventStart: the first ASDU of a stream was received.
	EventStart EventKind = iota + 1
	// EventGap: smpCnt skipped samples (Event.Lost of them).
	EventGap
	// EventDuplicate: an smpCnt was received again.
	EventDuplicate
	// EventOutOfOrder: an smpCnt older than the last one arrived.
	EventOutOfOrder
	// EventTimeout: no ASDU within the supervision timeout.
	EventTimeout
	// EventRestored: ASDUs resumed after a timeout.
	EventRestored
	// EventSynch: smpSynch changed (Event.SmpSynch is the new value).
	EventSynch
	// EventConfRev: confRev changed (Event.ConfRev is the new value).
	EventConfRev
	// EventInvalid: an ASDU failed validation (Event.Err).
	EventInvalid
)

var eventNames = map[EventKind]string{
	EventStart: "start", EventGap: "gap", EventDuplicate: "duplicate", EventOutOfOrder: "out-of-order",
	EventTimeout: "timeout", EventRestored: "restored", EventSynch: "synch", EventConfRev: "confrev",
	EventInvalid: "invalid",
}

// String returns the event kind name.
func (k EventKind) String() string {
	if s, ok := eventNames[k]; ok {
		return s
	}
	return "EventKind(" + strconv.Itoa(int(k)) + ")"
}

// Event is a supervision event of a stream.
type Event struct {
	Kind   EventKind
	Stream StreamKey
	Time   time.Time

	// SmpCnt is the counter of the ASDU that caused the event (not set
	// for EventTimeout).
	SmpCnt uint16

	// Lost is the number of missing samples (EventGap).
	Lost int

	// SmpSynch and ConfRev carry the new values (EventSynch,
	// EventConfRev) and the current ones otherwise.
	SmpSynch uint8
	ConfRev  uint32

	// Err is the validation error (EventInvalid).
	Err error
}

// Stats are the counters of one stream.
type Stats struct {
	Stream StreamKey
	Status Status

	// Frames and ASDUs count what was received; Lost counts samples
	// missing from smpCnt gaps; Duplicates and OutOfOrder count repeated
	// and late counters; Invalid counts ASDUs that failed validation;
	// Timeouts counts timeout transitions; QueueDrops counts samples not
	// delivered because the sample queue was full.
	Frames, ASDUs, Lost, Duplicates, OutOfOrder, Invalid, Timeouts, QueueDrops uint64

	// LastSmpCnt, SmpSynch, ConfRev and Simulation are the values of the
	// last accepted ASDU.
	LastSmpCnt uint16
	SmpSynch   uint8
	ConfRev    uint32
	Simulation bool

	// SmpCntWrap is the smpCnt modulus in use (configured, derived from
	// smpRate or learned).
	SmpCntWrap int

	// Rate is the measured rate in ASDUs per second (over ~0.5 s).
	Rate float64

	// First and Last are the receive times of the first and last ASDU.
	First, Last time.Time
}

// Subscriber receives SV frames from a transport and dispatches them to
// subscriptions. Create it with [NewSubscriber], add subscriptions with
// [Subscriber.Subscribe] and call [Subscriber.Run]. Frames may instead
// be fed with [Subscriber.HandleFrame] (for example from an l2.Demux
// port read elsewhere).
type Subscriber struct {
	t       Transport
	cfg     SubscriberConfig
	logger  *slog.Logger
	metrics Metrics

	mu     sync.RWMutex
	subs   []*Subscription
	closed bool

	// Receive-goroutine state.
	frame Frame
	smp   Sample
}

// NewSubscriber returns a subscriber reading from t. t may be nil when
// frames are fed with HandleFrame only.
func NewSubscriber(t Transport, cfg SubscriberConfig) (*Subscriber, error) {
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits
	}
	if cfg.Limits.MaxASDUs < 0 || cfg.Limits.MaxSample < 0 {
		return nil, configErr("Limits", "negative limit")
	}
	if cfg.SupervisionInterval <= 0 {
		cfg.SupervisionInterval = 10 * time.Millisecond
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Subscriber{t: t, cfg: cfg, logger: orDiscard(cfg.Logger), metrics: orNop(cfg.Metrics)}, nil
}

// Subscription is a set of streams selected by a [SubscriptionConfig].
type Subscription struct {
	s       *Subscriber
	cfg     SubscriptionConfig
	events  chan Event
	samples chan Sample

	mu      sync.Mutex
	streams map[StreamKey]*stream
	closed  bool
}

type stream struct {
	st      Stats
	have    bool
	wrap    int // 0 = unknown (65536 assumed, learnable)
	learned bool
	winAt   time.Time
	winN    uint64
}

// Subscribe adds a subscription.
func (s *Subscriber) Subscribe(cfg SubscriptionConfig) (*Subscription, error) {
	if err := cfg.Layout.Validate(); err != nil {
		return nil, err
	}
	if len(cfg.Layout) > 0 {
		if cfg.SampleSize != 0 && cfg.SampleSize != cfg.Layout.Size() {
			return nil, configErr("SampleSize", "%d disagrees with Layout size %d", cfg.SampleSize, cfg.Layout.Size())
		}
		cfg.SampleSize = cfg.Layout.Size()
	}
	if cfg.SmpCntWrap < 0 || cfg.SmpCntWrap > 65536 {
		return nil, configErr("SmpCntWrap", "%d out of range", cfg.SmpCntWrap)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.EventQueue <= 0 {
		cfg.EventQueue = DefaultEventQueue
	}
	if cfg.NominalFrequency == 0 {
		cfg.NominalFrequency = DefaultNominalFrequency
	}
	sub := &Subscription{s: s, cfg: cfg, events: make(chan Event, cfg.EventQueue), streams: map[StreamKey]*stream{}}
	if cfg.OnASDU == nil && cfg.SampleQueue > 0 {
		sub.samples = make(chan Sample, cfg.SampleQueue)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	s.subs = append(s.subs, sub)
	return sub, nil
}

// Close stops Run and closes every subscription. It does not close the
// transport.
func (s *Subscriber) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	subs := s.subs
	s.subs = nil
	s.mu.Unlock()
	for _, sub := range subs {
		sub.shut()
	}
	return nil
}

// Run receives frames until ctx is done or the subscriber is closed
// (both return nil), or the transport fails. It also runs timeout
// supervision.
func (s *Subscriber) Run(ctx context.Context) error {
	if s.t == nil {
		return configErr("Transport", "nil transport; use HandleFrame")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.supervise(ctx)
	for {
		raw, err := s.t.Recv(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrClosed) || s.isClosed() {
				return nil //nolint:nilerr // cancellation and Close are a normal stop
			}
			return err
		}
		s.HandleFrame(raw)
		if s.isClosed() {
			return nil
		}
	}
}

func (s *Subscriber) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *Subscriber) supervise(ctx context.Context) {
	t := time.NewTicker(s.cfg.SupervisionInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.CheckTimeouts()
		}
	}
}

// CheckTimeouts runs one timeout check (Run calls it periodically).
func (s *Subscriber) CheckTimeouts() {
	now := s.cfg.Now()
	s.mu.RLock()
	subs := s.subs
	s.mu.RUnlock()
	for _, sub := range subs {
		sub.checkTimeouts(now)
	}
}

// HandleFrame decodes and dispatches one raw Ethernet frame. Frames of
// other EtherTypes are ignored. It must not be called concurrently with
// itself or with Run (the decoder reuses its storage).
func (s *Subscriber) HandleFrame(raw []byte) {
	now := s.cfg.Now()
	f := &s.frame
	if err := DecodeInto(f, raw, s.cfg.Limits); err != nil {
		if errors.Is(err, ErrNotSV) {
			return
		}
		s.metrics.FrameDropped("decode")
		s.logger.Debug("sv: decode failed", "error", err)
		if s.cfg.OnDecodeError != nil {
			s.cfg.OnDecodeError(raw, err)
		}
		return
	}
	if f.Simulation && !s.cfg.AcceptSimulation {
		s.metrics.FrameDropped("simulation")
		return
	}
	s.mu.RLock()
	subs := s.subs
	s.mu.RUnlock()
	matched := false
	for _, sub := range subs {
		if sub.process(f, now, &s.smp) {
			matched = true
		}
	}
	if !matched {
		s.metrics.FrameDropped("filtered")
	}
}

func (sub *Subscription) matchesFrame(f *Frame) bool {
	c := &sub.cfg
	if !c.Dst.IsZero() && c.Dst != f.Dst {
		return false
	}
	if !c.Src.IsZero() && c.Src != f.Src {
		return false
	}
	if len(c.AppIDs) > 0 && !slices.Contains(c.AppIDs, f.AppID) {
		return false
	}
	return true
}

// process handles the ASDUs of f that match; it reports whether any did.
func (sub *Subscription) process(f *Frame, now time.Time, smp *Sample) bool {
	if !sub.matchesFrame(f) {
		return false
	}
	matched := false
	for i := range f.PDU.ASDUs {
		a := &f.PDU.ASDUs[i]
		if sub.cfg.SvID != "" && a.SvID != sub.cfg.SvID {
			continue
		}
		matched = true
		sub.s.metrics.ASDUReceived(a.SvID)
		sub.handleASDU(f, i, a, now, smp)
	}
	return matched
}

func (sub *Subscription) handleASDU(f *Frame, idx int, a *ASDU, now time.Time, smp *Sample) {
	key := StreamKey{Src: f.Src, AppID: f.AppID, SvID: a.SvID}
	sub.mu.Lock()
	if sub.closed {
		sub.mu.Unlock()
		return
	}
	st := sub.streams[key]
	if st == nil {
		st = &stream{st: Stats{Stream: key, SmpCntWrap: 65536}}
		st.wrap = sub.cfg.SmpCntWrap
		sub.streams[key] = st
	}
	if idx == 0 || st.st.Frames == 0 {
		st.st.Frames++
	}
	st.st.ASDUs++
	if st.st.First.IsZero() {
		st.st.First = now
	}
	st.st.Last = now
	sub.updateRate(st, now)

	if err := sub.validate(a); err != nil {
		st.st.Invalid++
		st.st.Status = StatusInvalid
		ev := Event{Kind: EventInvalid, Stream: key, Time: now, SmpCnt: a.SmpCnt, SmpSynch: a.SmpSynch, ConfRev: a.ConfRev, Err: err}
		sub.mu.Unlock()
		sub.emit(ev)
		return
	}

	var evs [4]Event
	ne := 0
	base := Event{Stream: key, Time: now, SmpCnt: a.SmpCnt, SmpSynch: a.SmpSynch, ConfRev: a.ConfRev}
	if st.st.Status == StatusTimeout {
		ev := base
		ev.Kind = EventRestored
		evs[ne] = ev
		ne++
	}
	st.st.Status = StatusOK
	st.st.Simulation = f.Simulation
	if st.wrap == 0 && a.HasSmpRate && a.SmpRate > 0 {
		mod := SmpPerPeriod
		if a.HasSmpMod {
			mod = a.SmpMod
		}
		st.wrap = DefaultSmpCntWrap(SamplesPerSecond(a.SmpRate, mod, sub.cfg.NominalFrequency))
	}
	if st.wrap == 0 && sub.cfg.SmpRate > 0 {
		st.wrap = DefaultSmpCntWrap(SamplesPerSecond(sub.cfg.SmpRate, sub.cfg.SmpMod, sub.cfg.NominalFrequency))
	}
	lost := 0
	deliver := true
	if !st.have {
		st.have = true
		ev := base
		ev.Kind = EventStart
		evs[ne] = ev
		ne++
	} else {
		if a.SmpSynch != st.st.SmpSynch {
			ev := base
			ev.Kind = EventSynch
			evs[ne] = ev
			ne++
		}
		if a.ConfRev != st.st.ConfRev {
			ev := base
			ev.Kind = EventConfRev
			evs[ne] = ev
			ne++
		}
		kind, n := st.sequence(a.SmpCnt)
		switch kind {
		case EventGap:
			lost = n
			st.st.Lost += uint64(n)
			ev := base
			ev.Kind, ev.Lost = EventGap, n
			evs[ne] = ev
			ne++
		case EventDuplicate:
			st.st.Duplicates++
			deliver = false
			ev := base
			ev.Kind = EventDuplicate
			evs[ne] = ev
			ne++
		case EventOutOfOrder:
			st.st.OutOfOrder++
			deliver = false
			ev := base
			ev.Kind = EventOutOfOrder
			evs[ne] = ev
			ne++
		default: // in sequence
		}
	}
	if deliver {
		st.st.LastSmpCnt = a.SmpCnt
	}
	st.st.SmpSynch = a.SmpSynch
	st.st.ConfRev = a.ConfRev
	if st.wrap > 0 {
		st.st.SmpCntWrap = st.wrap
	}
	sub.mu.Unlock()

	for i := 0; i < ne; i++ {
		sub.emit(evs[i])
	}
	if !deliver {
		return
	}
	if sub.cfg.OnASDU != nil {
		*smp = Sample{Stream: key, Received: now, Frame: f, Index: idx, ASDU: a, Lost: lost}
		sub.cfg.OnASDU(smp)
		return
	}
	if sub.samples != nil {
		ac := *a
		ac.Sample = clone(a.Sample)
		fc := *f
		fc.PDU.ASDUs = nil
		fc.PDU.Security = clone(f.PDU.Security)
		fc.strs = nil
		if f.VLAN != nil {
			v := *f.VLAN // DecodeInto reuses the tag
			fc.VLAN = &v
		}
		sub.mu.Lock()
		if !sub.closed {
			select {
			case sub.samples <- Sample{Stream: key, Received: now, Frame: &fc, Index: idx, ASDU: &ac, Lost: lost}:
			default:
				st.st.QueueDrops++
			}
		}
		sub.mu.Unlock()
	}
}

// sequence classifies cnt against the last accepted counter.
func (st *stream) sequence(cnt uint16) (EventKind, int) {
	last := int(st.st.LastSmpCnt)
	c := int(cnt)
	w := st.wrap
	if w == 0 {
		w = 65536
		// Learn the modulus from the first wrap to 0.
		if c == 0 && last > 0 && last < 65535 && !st.learned {
			st.learned = true
			st.wrap = last + 1
			return 0, 0
		}
	}
	if c == last {
		return EventDuplicate, 0
	}
	if c >= w || last >= w {
		// Counter outside the expected modulus: resynchronize.
		return 0, 0
	}
	exp := (last + 1) % w
	d := (c - exp + w) % w
	switch {
	case d == 0:
		return 0, 0
	case d < w/2:
		return EventGap, d
	default:
		return EventOutOfOrder, 0
	}
}

func (sub *Subscription) updateRate(st *stream, now time.Time) {
	if st.winAt.IsZero() {
		st.winAt = now
	}
	st.winN++
	if el := now.Sub(st.winAt); el >= rateWindow {
		st.st.Rate = float64(st.winN) / el.Seconds()
		st.winAt, st.winN = now, 0
	}
}

func (sub *Subscription) validate(a *ASDU) error {
	c := &sub.cfg
	if c.DatSet != "" && a.DatSet != "" && a.DatSet != c.DatSet { // datSet is optional on the wire
		return &ValidationError{SvID: a.SvID, Field: "datSet", Want: c.DatSet, Got: a.DatSet, Err: ErrDatSetMismatch}
	}
	if c.ConfRev != 0 && a.ConfRev != c.ConfRev {
		return &ValidationError{SvID: a.SvID, Field: "confRev", Want: fmt.Sprint(c.ConfRev), Got: fmt.Sprint(a.ConfRev), Err: ErrConfRevMismatch}
	}
	if c.SmpRate != 0 && a.HasSmpRate && (a.SmpRate != c.SmpRate || (a.HasSmpMod && a.SmpMod != c.SmpMod)) {
		return &ValidationError{SvID: a.SvID, Field: "smpRate", Want: fmt.Sprintf("%d %v", c.SmpRate, c.SmpMod), Got: fmt.Sprintf("%d %v", a.SmpRate, a.SmpMod), Err: ErrSmpRateMismatch}
	}
	if c.SampleSize != 0 && len(a.Sample) != c.SampleSize {
		return &ValidationError{SvID: a.SvID, Field: "sample", Want: fmt.Sprint(c.SampleSize), Got: fmt.Sprint(len(a.Sample)), Err: ErrSampleSizeMismatch}
	}
	return nil
}

func (sub *Subscription) checkTimeouts(now time.Time) {
	sub.mu.Lock()
	var evs []Event
	for k, st := range sub.streams {
		if st.st.Status != StatusTimeout && now.Sub(st.st.Last) > sub.cfg.Timeout {
			st.st.Status = StatusTimeout
			st.st.Timeouts++
			st.st.Rate = 0
			st.winAt, st.winN = time.Time{}, 0
			evs = append(evs, Event{Kind: EventTimeout, Stream: k, Time: now, SmpSynch: st.st.SmpSynch, ConfRev: st.st.ConfRev})
		}
	}
	sub.mu.Unlock()
	for _, ev := range evs {
		sub.emit(ev)
	}
}

func (sub *Subscription) emit(ev Event) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.closed {
		return
	}
	// Non-blocking send under the lock, so shut cannot close the
	// channel concurrently.
	select {
	case sub.events <- ev:
		sub.s.metrics.Event(ev.Stream.SvID, ev.Kind, false)
	default:
		sub.s.metrics.Event(ev.Stream.SvID, ev.Kind, true)
	}
}

// Events returns the supervision event channel. It is closed when the
// subscription or the subscriber is closed.
func (sub *Subscription) Events() <-chan Event { return sub.events }

// Samples returns the sample channel (nil unless SampleQueue > 0 and
// OnASDU is nil). It is closed with the subscription.
func (sub *Subscription) Samples() <-chan Sample { return sub.samples }

// Stats returns the counters of every stream seen, sorted by key.
func (sub *Subscription) Stats() []Stats {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	out := make([]Stats, 0, len(sub.streams))
	for _, st := range sub.streams {
		out = append(out, st.st)
	}
	slices.SortFunc(out, func(a, b Stats) int {
		if c := compareStr(a.Stream.SvID, b.Stream.SvID); c != 0 {
			return c
		}
		if c := compareStr(a.Stream.Src.String(), b.Stream.Src.String()); c != 0 {
			return c
		}
		return int(a.Stream.AppID) - int(b.Stream.AppID)
	})
	return out
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Close removes the subscription and closes its channels.
func (sub *Subscription) Close() error {
	s := sub.s
	s.mu.Lock()
	if i := slices.Index(s.subs, sub); i >= 0 {
		s.subs = slices.Delete(slices.Clone(s.subs), i, i+1)
	}
	s.mu.Unlock()
	sub.shut()
	return nil
}

func (sub *Subscription) shut() {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.closed {
		return
	}
	sub.closed = true
	close(sub.events)
	if sub.samples != nil {
		close(sub.samples)
	}
}
