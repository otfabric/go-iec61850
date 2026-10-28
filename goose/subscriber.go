// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
)

// EventKind classifies subscriber events.
type EventKind int

const (
	// EventMessage is an accepted, valid message. Event.NewState
	// reports whether it starts a new state (stNum changed).
	EventMessage EventKind = iota + 1

	// EventTimeout reports that TimeAllowedToLive elapsed without a
	// message.
	EventTimeout

	// EventStNumGap reports that one or more state changes were missed
	// (stNum advanced by more than one). The message itself is also
	// delivered as EventMessage.
	EventStNumGap

	// EventSqNumGap reports missed retransmissions within a state (or a
	// new state that did not start at sqNum 0). The message is also
	// delivered.
	EventSqNumGap

	// EventDuplicate reports a repeated stNum/sqNum pair. The message
	// is not delivered.
	EventDuplicate

	// EventOutOfOrder reports a message older than the last accepted
	// one. The message is not delivered.
	EventOutOfOrder

	// EventRestart reports that the publisher restarted (stNum went
	// back to 0 or 1). The message is also delivered.
	EventRestart

	// EventInvalid reports a message that failed validation; Event.Err
	// is a [*ValidationError]. The message is not delivered.
	EventInvalid

	// EventSupervision reports a change of Event.State.Status.
	EventSupervision
)

var eventKindNames = map[EventKind]string{
	EventMessage: "message", EventTimeout: "timeout", EventStNumGap: "stnum-gap",
	EventSqNumGap: "sqnum-gap", EventDuplicate: "duplicate", EventOutOfOrder: "out-of-order",
	EventRestart: "restart", EventInvalid: "invalid", EventSupervision: "supervision",
}

func (k EventKind) String() string {
	if s, ok := eventKindNames[k]; ok {
		return s
	}
	return "EventKind(" + strconv.Itoa(int(k)) + ")"
}

// Status is the supervision status of a GOOSE stream.
type Status int

const (
	// StatusWaiting means no message has been received yet.
	StatusWaiting Status = iota

	// StatusOK means valid messages are received within
	// TimeAllowedToLive.
	StatusOK

	// StatusTimeout means TimeAllowedToLive elapsed without a message.
	StatusTimeout

	// StatusInvalid means the last message failed validation (confRev,
	// datSet, goID or member layout).
	StatusInvalid

	// StatusNeedsCommissioning means the last message had ndsCom set.
	StatusNeedsCommissioning
)

func (s Status) String() string {
	switch s {
	case StatusWaiting:
		return "waiting"
	case StatusOK:
		return "ok"
	case StatusTimeout:
		return "timeout"
	case StatusInvalid:
		return "invalid"
	case StatusNeedsCommissioning:
		return "ndscom"
	default:
		return "Status(" + strconv.Itoa(int(s)) + ")"
	}
}

// StreamKey identifies one received GOOSE stream.
type StreamKey struct {
	Src     MAC
	AppID   uint16
	GocbRef string
}

// Supervision is the supervision state of one stream, modelled on the
// LGOS logical node (IEC 61850-7-4).
type Supervision struct {
	Stream StreamKey
	Status Status

	// St is true while the subscription is valid (LGOS.St).
	St bool

	// SimSt is true when simulated messages are being accepted
	// (LGOS.SimSt).
	SimSt bool

	// NdsCom mirrors the last received ndsCom flag (LGOS.NdsCom).
	NdsCom bool

	// LastStNum and LastSqNum are the sequence numbers of the last
	// accepted message (LGOS.LastStNum).
	LastStNum, LastSqNum uint32

	// ConfRevNum is the expected configuration revision
	// (LGOS.ConfRevNum); RxConfRevNum is the last received one
	// (LGOS.RxConfRevNum).
	ConfRevNum, RxConfRevNum uint32

	// LastRx is the time of the last received message and
	// TimeAllowedToLive the TAL it carried.
	LastRx            time.Time
	TimeAllowedToLive time.Duration

	// LastErr is the last validation error, if any.
	LastErr error

	// Counters.
	Messages, StateChanges, Duplicates, OutOfOrder, StNumGaps, SqNumGaps,
	Restarts, Timeouts, Invalid, DroppedEvents uint64
}

// Event is delivered to a subscription's handler or channel.
type Event struct {
	Kind EventKind

	// Received is the local receive time (or the expiry time for
	// EventTimeout).
	Received time.Time

	// Frame is the received frame; nil for EventTimeout and for
	// EventSupervision events caused by a timeout.
	Frame *Frame

	// NewState is true for an EventMessage that starts a new state.
	NewState bool

	// PrevStNum and PrevSqNum are the sequence numbers of the previously
	// accepted message.
	PrevStNum, PrevSqNum uint32

	// State is the supervision snapshot after processing.
	State Supervision

	// Err is the validation error for EventInvalid.
	Err error
}

// SubscriptionConfig selects and validates GOOSE messages.
//
// Filters with a zero value match everything; a subscription without
// any filter receives every GOOSE frame (monitor mode). Validation
// fields with a zero value are not checked.
type SubscriptionConfig struct {
	// Dst filters on the destination address.
	Dst MAC

	// AppIDs filters on any of the listed APPIDs.
	AppIDs []uint16

	// GocbRef filters on the GoCB reference.
	GocbRef string

	// Src filters on the source address.
	Src MAC

	// DatSet, GoID and ConfRev are the expected values.
	DatSet  string
	GoID    string
	ConfRev uint32

	// Members, when non-nil, is the expected dataset layout: the
	// number of entries and the MMS type of each.
	Members []mms.TypeSpec

	// AcceptSimulation corresponds to LPHD.Sim: when false, frames with
	// the simulation bit are ignored; when true they are accepted and,
	// once one is seen on a stream, non-simulated frames of that
	// stream are ignored.
	AcceptSimulation bool

	// OnlyStateChanges delivers EventMessage only for new states;
	// retransmissions still feed supervision.
	OnlyStateChanges bool

	// Handler, when non-nil, is called for every event on a dedicated
	// goroutine, in order. Otherwise events are read from
	// [Subscription.Events].
	Handler func(Event)

	// QueueSize bounds the event queue (default 256). When full, events
	// are dropped and counted in Supervision.DroppedEvents.
	QueueSize int
}

// SubscriberConfig configures a [Subscriber].
type SubscriberConfig struct {
	// Limits bounds decoding (zero uses [DefaultLimits]).
	Limits Limits

	// Logger receives diagnostics. Nil disables logging.
	Logger *slog.Logger

	// Metrics receives counters. Nil disables metrics.
	Metrics Metrics

	// OnDecodeError, when non-nil, is called with frames that are not
	// valid GOOSE (non-GOOSE EtherTypes are ignored silently).
	OnDecodeError func(raw []byte, err error)

	// Now returns the current time; nil uses [time.Now].
	Now func() time.Time
}

// Subscriber receives GOOSE frames from a transport and dispatches them
// to subscriptions.
type Subscriber struct {
	t       Transport
	cfg     SubscriberConfig
	logger  *slog.Logger
	metrics Metrics

	mu     sync.RWMutex
	subs   []*Subscription
	closed bool
}

// NewSubscriber returns a subscriber reading from t. Call
// [Subscriber.Run] to start receiving.
func NewSubscriber(t Transport, cfg SubscriberConfig) (*Subscriber, error) {
	if t == nil {
		return nil, configErr("Transport", "nil transport")
	}
	if cfg.Limits.MaxDepth <= 0 {
		cfg.Limits.MaxDepth = DefaultLimits.MaxDepth
	}
	if cfg.Limits.MaxElements <= 0 {
		cfg.Limits.MaxElements = DefaultLimits.MaxElements
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Subscriber{t: t, cfg: cfg, logger: orDiscard(cfg.Logger), metrics: orNop(cfg.Metrics)}, nil
}

// Subscribe adds a subscription.
func (s *Subscriber) Subscribe(cfg SubscriptionConfig) (*Subscription, error) {
	if cfg.QueueSize < 0 {
		return nil, configErr("QueueSize", "negative")
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 256
	}
	cfg.AppIDs = slices.Clone(cfg.AppIDs)
	cfg.Members = slices.Clone(cfg.Members)
	sub := &Subscription{
		s:       s,
		cfg:     cfg,
		streams: map[StreamKey]*stream{},
		events:  make(chan Event, cfg.QueueSize),
		done:    make(chan struct{}),
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	s.subs = append(s.subs, sub)
	s.mu.Unlock()
	if cfg.Handler != nil {
		sub.wg.Add(1)
		go func() {
			defer sub.wg.Done()
			for ev := range sub.events {
				cfg.Handler(ev)
			}
		}()
	}
	s.logger.Info("goose: subscription created", "gocbRef", cfg.GocbRef, "dst", cfg.Dst.String(), "appIDs", cfg.AppIDs)
	return sub, nil
}

// Run receives frames until ctx is done (returning nil), the subscriber
// is closed (returning nil) or the transport fails.
func (s *Subscriber) Run(ctx context.Context) error {
	for {
		raw, err := s.t.Recv(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrClosed) {
				return nil //nolint:nilerr // cancellation and Close are a normal stop
			}
			return err
		}
		s.HandleFrame(raw)
	}
}

// HandleFrame decodes and dispatches one raw Ethernet frame. Run calls
// it for every received frame; it is exported for callers that own the
// receive loop.
func (s *Subscriber) HandleFrame(raw []byte) {
	now := s.cfg.Now()
	f, err := DecodeWithLimits(raw, s.cfg.Limits)
	if err != nil {
		if errors.Is(err, ErrNotGOOSE) {
			return
		}
		s.metrics.FrameDropped("decode")
		s.logger.Debug("goose: decode failed", "error", err)
		if s.cfg.OnDecodeError != nil {
			s.cfg.OnDecodeError(raw, err)
		}
		return
	}
	s.mu.RLock()
	subs := s.subs
	s.mu.RUnlock()
	matched := false
	for _, sub := range subs {
		if sub.matches(f) {
			matched = true
			sub.process(f, now)
		}
	}
	if matched {
		s.metrics.FrameReceived(f.PDU.GocbRef)
	} else {
		s.metrics.FrameDropped("filtered")
	}
}

// Close closes all subscriptions. The transport is not closed; Run
// returns when the transport is closed or its context is cancelled.
func (s *Subscriber) Close() error {
	s.mu.Lock()
	subs := s.subs
	s.subs = nil
	s.closed = true
	s.mu.Unlock()
	for _, sub := range subs {
		sub.close()
	}
	return nil
}

func (s *Subscriber) remove(sub *Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = slices.DeleteFunc(slices.Clone(s.subs), func(x *Subscription) bool { return x == sub })
}

// Subscription is one set of filters with per-stream supervision.
type Subscription struct {
	s   *Subscriber
	cfg SubscriptionConfig

	mu      sync.Mutex
	streams map[StreamKey]*stream
	closed  bool

	events    chan Event
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

type stream struct {
	sup      Supervision
	have     bool // a message has been accepted
	timer    *time.Timer
	gen      uint64
	simSeen  bool
	lastT    time.Time // T of the last accepted message
	lastSent Status    // last status reported via EventSupervision
}

// Events returns the event channel. It is closed by
// [Subscription.Close]. When a Handler is configured the channel is
// drained internally and must not be read.
func (sub *Subscription) Events() <-chan Event { return sub.events }

// Supervision returns a snapshot of every stream seen so far, ordered by
// gocbRef then source address.
func (sub *Subscription) Supervision() []Supervision {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	out := make([]Supervision, 0, len(sub.streams))
	for _, st := range sub.streams {
		out = append(out, st.sup)
	}
	slices.SortFunc(out, func(a, b Supervision) int {
		if a.Stream.GocbRef != b.Stream.GocbRef {
			if a.Stream.GocbRef < b.Stream.GocbRef {
				return -1
			}
			return 1
		}
		return slices.Compare(a.Stream.Src[:], b.Stream.Src[:])
	})
	return out
}

// Close removes the subscription, stops its timers and closes the event
// channel after pending events have been handed to the Handler.
func (sub *Subscription) Close() error {
	sub.s.remove(sub)
	sub.close()
	return nil
}

func (sub *Subscription) close() {
	sub.closeOnce.Do(func() {
		sub.mu.Lock()
		sub.closed = true
		for _, st := range sub.streams {
			if st.timer != nil {
				st.timer.Stop()
			}
		}
		close(sub.events)
		sub.mu.Unlock()
		close(sub.done)
		sub.wg.Wait()
	})
}

func (sub *Subscription) matches(f *Frame) bool {
	c := &sub.cfg
	if !c.Dst.IsZero() && f.Dst != c.Dst {
		return false
	}
	if !c.Src.IsZero() && f.Src != c.Src {
		return false
	}
	if len(c.AppIDs) > 0 && !slices.Contains(c.AppIDs, f.AppID) {
		return false
	}
	if c.GocbRef != "" && f.PDU.GocbRef != c.GocbRef {
		return false
	}
	return true
}

// emit queues an event; the caller holds sub.mu.
func (sub *Subscription) emit(st *stream, ev Event) {
	if sub.closed {
		return
	}
	ev.State = st.sup
	select {
	case sub.events <- ev:
		sub.s.metrics.Event(st.sup.Stream.GocbRef, ev.Kind, false)
	default:
		st.sup.DroppedEvents++
		sub.s.metrics.Event(st.sup.Stream.GocbRef, ev.Kind, true)
		sub.s.logger.Warn("goose: event queue full, event dropped", "gocbRef", st.sup.Stream.GocbRef, "kind", ev.Kind.String())
	}
}

// setStatus updates the status and emits EventSupervision on change.
func (sub *Subscription) setStatus(st *stream, s Status, now time.Time, f *Frame) {
	st.sup.Status = s
	st.sup.St = s == StatusOK
	if s != st.lastSent {
		st.lastSent = s
		sub.emit(st, Event{Kind: EventSupervision, Received: now, Frame: f})
	}
}

func (sub *Subscription) process(f *Frame, now time.Time) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.closed {
		return
	}
	key := StreamKey{Src: f.Src, AppID: f.AppID, GocbRef: f.PDU.GocbRef}
	st := sub.streams[key]
	if st == nil {
		st = &stream{sup: Supervision{Stream: key, ConfRevNum: sub.cfg.ConfRev}}
		sub.streams[key] = st
	}

	// Simulation handling (LPHD.Sim / LGOS.SimSt).
	if f.Simulation && !sub.cfg.AcceptSimulation {
		sub.s.metrics.FrameDropped("simulation")
		return
	}
	if f.Simulation {
		st.simSeen = true
		st.sup.SimSt = true
	} else if st.simSeen && sub.cfg.AcceptSimulation {
		sub.s.metrics.FrameDropped("simulation")
		return
	}

	p := &f.PDU
	st.sup.LastRx = now
	st.sup.TimeAllowedToLive = time.Duration(p.TimeAllowedToLive) * time.Millisecond
	st.sup.RxConfRevNum = p.ConfRev
	st.sup.NdsCom = p.NdsCom
	sub.armTimer(key, st)

	if verr := sub.validate(p); verr != nil {
		st.sup.Invalid++
		st.sup.LastErr = verr
		status := StatusInvalid
		if errors.Is(verr, ErrNeedsCommissioning) {
			status = StatusNeedsCommissioning
		}
		sub.emit(st, Event{Kind: EventInvalid, Received: now, Frame: f, Err: verr, PrevStNum: st.sup.LastStNum, PrevSqNum: st.sup.LastSqNum})
		sub.setStatus(st, status, now, f)
		return
	}
	st.sup.LastErr = nil

	prevSt, prevSq := st.sup.LastStNum, st.sup.LastSqNum
	base := Event{Received: now, Frame: f, PrevStNum: prevSt, PrevSqNum: prevSq}
	newState := !st.have
	var extra []EventKind
	if st.have {
		switch {
		case p.StNum == prevSt:
			switch {
			case p.SqNum == prevSq:
				st.sup.Duplicates++
				sub.emit(st, withKind(base, EventDuplicate))
				sub.setStatus(st, StatusOK, now, f)
				return
			case seqAfter(p.SqNum, prevSq):
				if p.SqNum != nextSeq(prevSq) {
					st.sup.SqNumGaps++
					extra = append(extra, EventSqNumGap)
				}
			default:
				st.sup.OutOfOrder++
				sub.emit(st, withKind(base, EventOutOfOrder))
				sub.setStatus(st, StatusOK, now, f)
				return
			}
		case p.StNum == nextSeq(prevSt):
			newState = true
			if p.SqNum != 0 {
				st.sup.SqNumGaps++
				extra = append(extra, EventSqNumGap)
			}
		case p.StNum <= 1 && prevSt > 1 && !p.T.Before(st.lastT):
			// A reset to stNum 0/1 with a T no older than the last
			// state is a restart; an older T is a late frame.
			st.sup.Restarts++
			extra = append(extra, EventRestart)
			newState = true
		case seqAfter(p.StNum, prevSt):
			newState = true
			if p.StNum != nextSeq(prevSt) {
				st.sup.StNumGaps++
				extra = append(extra, EventStNumGap)
			}
			if p.SqNum != 0 {
				st.sup.SqNumGaps++
				extra = append(extra, EventSqNumGap)
			}
		default:
			st.sup.OutOfOrder++
			sub.emit(st, withKind(base, EventOutOfOrder))
			sub.setStatus(st, StatusOK, now, f)
			return
		}
	}
	st.have = true
	st.lastT = p.T
	st.sup.LastStNum, st.sup.LastSqNum = p.StNum, p.SqNum
	st.sup.Messages++
	if newState {
		st.sup.StateChanges++
	}
	sub.setStatus(st, StatusOK, now, f)
	for _, k := range extra {
		sub.emit(st, withKind(base, k))
	}
	if newState || !sub.cfg.OnlyStateChanges {
		ev := withKind(base, EventMessage)
		ev.NewState = newState
		sub.emit(st, ev)
	}
}

func withKind(e Event, k EventKind) Event {
	e.Kind = k
	return e
}

// seqAfter reports whether a follows b in a 32-bit sequence space where
// 0 is only used before the first rollover (rollover goes to 1).
func seqAfter(a, b uint32) bool {
	return a != b && a-b < 1<<31
}

func nextSeq(x uint32) uint32 {
	if x == 0xffffffff {
		return 1
	}
	return x + 1
}

func (sub *Subscription) validate(p *PDU) error {
	c := &sub.cfg
	ve := func(field, want, got string, err error) error {
		return &ValidationError{GocbRef: p.GocbRef, Field: field, Want: want, Got: got, Err: err}
	}
	if p.NdsCom {
		return ve("ndsCom", "false", "true", ErrNeedsCommissioning)
	}
	if c.ConfRev != 0 && p.ConfRev != c.ConfRev {
		return ve("confRev", strconv.FormatUint(uint64(c.ConfRev), 10), strconv.FormatUint(uint64(p.ConfRev), 10), ErrConfRevMismatch)
	}
	if c.DatSet != "" && p.DatSet != c.DatSet {
		return ve("datSet", c.DatSet, p.DatSet, ErrDatSetMismatch)
	}
	if c.GoID != "" && p.GoID != c.GoID {
		return ve("goID", c.GoID, p.GoID, ErrGoIDMismatch)
	}
	if int(p.NumDatSetEntries) != len(p.AllData) {
		return ve("numDatSetEntries", strconv.Itoa(len(p.AllData)), strconv.FormatUint(uint64(p.NumDatSetEntries), 10), ErrMemberMismatch)
	}
	if c.Members != nil {
		if len(c.Members) != len(p.AllData) {
			return ve("allData", fmt.Sprintf("%d members", len(c.Members)), fmt.Sprintf("%d members", len(p.AllData)), ErrMemberMismatch)
		}
		for i, ts := range c.Members {
			if !MatchesType(p.AllData[i], ts) {
				return ve(fmt.Sprintf("allData[%d]", i), typeSpecString(ts), p.AllData[i].Type().String(), ErrMemberMismatch)
			}
		}
	}
	return nil
}

func (sub *Subscription) armTimer(key StreamKey, st *stream) {
	st.gen++
	gen := st.gen
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	tal := st.sup.TimeAllowedToLive
	if tal <= 0 {
		return
	}
	st.timer = time.AfterFunc(tal, func() { sub.expire(key, gen) })
}

func (sub *Subscription) expire(key StreamKey, gen uint64) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	st := sub.streams[key]
	if sub.closed || st == nil || st.gen != gen {
		return
	}
	now := sub.s.cfg.Now()
	st.sup.Timeouts++
	st.timer = nil
	sub.emit(st, Event{Kind: EventTimeout, Received: now, PrevStNum: st.sup.LastStNum, PrevSqNum: st.sup.LastSqNum})
	sub.setStatus(st, StatusTimeout, now, nil)
	sub.s.logger.Warn("goose: timeAllowedToLive expired", "gocbRef", key.GocbRef, "src", key.Src.String())
}

// MatchesType reports whether v conforms to the MMS type ts. Sizes are
// checked for bit strings with a fixed size; integer widths are not
// enforced, and floats of either width match FLOAT32/FLOAT64.
func MatchesType(v *mms.Value, ts mms.TypeSpec) bool {
	if v == nil {
		return false
	}
	if v.Type() != ts.Type {
		return false
	}
	switch ts.Type {
	case mms.ValueTypeStructure:
		elems, _ := v.Structure()
		if len(elems) != len(ts.Elements) {
			return false
		}
		for i, e := range ts.Elements {
			if !MatchesType(elems[i], e.Type) {
				return false
			}
		}
	case mms.ValueTypeArray:
		elems, _ := v.ArrayElements()
		if ts.Count > 0 && len(elems) != ts.Count {
			return false
		}
		if ts.Element != nil {
			for _, e := range elems {
				if !MatchesType(e, *ts.Element) {
					return false
				}
			}
		}
	case mms.ValueTypeBitString:
		n, _ := v.BitStringLength()
		if ts.Size > 0 && n != ts.Size {
			return false
		}
	default:
		// Scalars: the type check above is sufficient.
	}
	return true
}

func typeSpecString(ts mms.TypeSpec) string {
	if ts.Type == mms.ValueTypeStructure {
		return fmt.Sprintf("Structure(%d)", len(ts.Elements))
	}
	if ts.Size > 0 {
		return fmt.Sprintf("%v(%d)", ts.Type, ts.Size)
	}
	return ts.Type.String()
}
