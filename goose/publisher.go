// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
)

// Default retransmission parameters (IEC 61850-8-1 Annex C leaves the
// values to configuration; these match common SCL MinTime/MaxTime
// defaults).
const (
	// timeQuality is the TimeQuality of zero values (ZeroValue): no
	// flags, 10 bits of sub-second accuracy.
	timeQuality uint8 = 0x0a

	// DefaultTimeQuality is the TimeQuality octet of T when
	// PublisherConfig.TimeQuality is zero: ClockNotSynchronized (0x20,
	// 7-2 TimeQuality / 8-1 8.1.3.7) with 10 bits of sub-second
	// accuracy. The publisher cannot know whether the host clock is
	// synchronised, so it does not claim it.
	DefaultTimeQuality uint8 = 0x2a

	// MaxRefLen and MaxGoIDLen are the maximum gocbRef/datSet and goID
	// lengths (IEC 61850-8-1 18.1.2.5.2: VISIBLE-STRING up to 129 and
	// 65 octets).
	MaxRefLen  = 129
	MaxGoIDLen = 65

	// MaxRetransmission bounds MaxTime: the retransmission interval
	// stays below 60 s (IEC 61850-8-1 18.1.2.5.1 Figure 10 key 4).
	MaxRetransmission = 60 * time.Second

	DefaultMinTime = 10 * time.Millisecond
	DefaultMaxTime = 1000 * time.Millisecond
)

// PublisherConfig configures a [Publisher].
type PublisherConfig struct {
	// Dst is the destination address (required). IEC 61850-8-1
	// recommends 01-0C-CD-01-00-00..01-0C-CD-01-01-FF.
	Dst MAC

	// Src is the source address. Zero uses the transport address.
	Src MAC

	// VLAN, when non-nil, adds an 802.1Q tag.
	VLAN *VLAN

	// AppID is the GOOSE APPID.
	AppID uint16

	// GocbRef is the GoCB reference (required), e.g.
	// "IED1LD0/LLN0$GO$gcb1".
	GocbRef string

	// DatSet is the dataset reference, e.g. "IED1LD0/LLN0$ds1".
	// Required unless NdsCom is set.
	DatSet string

	// GoID is the GOOSE identifier, at most [MaxGoIDLen] octets. Empty
	// uses GocbRef (not length-checked).
	GoID string

	// ConfRev is the configuration revision.
	ConfRev uint32

	// MinTime is the interval of the first retransmission after a
	// state change. Subsequent intervals double until MaxTime.
	// Zero uses [DefaultMinTime].
	MinTime time.Duration

	// MaxTime is the heartbeat interval in the steady state. Zero uses
	// [DefaultMaxTime].
	MaxTime time.Duration

	// TimeAllowedToLive is sent in every message. Zero uses 2*MaxTime.
	// It must be at least 1 ms and greater than MaxTime (8-1 18.1.2.5.1
	// Figure 10 key 2: the retransmission time is less than the
	// timeAllowedToLive).
	TimeAllowedToLive time.Duration

	// TimeQuality is the TimeQuality octet of T. Zero uses
	// [DefaultTimeQuality] (clock not synchronised); set it (for
	// example 0x0a) when the host clock is known to be synchronised.
	TimeQuality uint8

	// FixedOffs selects the fixed-length encoding of IEC 61850-8-1
	// Annex A.3 (SCL fixedOffs="true", see [EncodePDUFixedLength]).
	FixedOffs bool

	// MemberTypes are the MMS types of the dataset members, used by
	// FixedOffs for the integer and float widths. Optional.
	MemberTypes []mms.TypeSpec

	// Test sets the goosePdu test/simulation field.
	Test bool

	// Simulation sets the Reserved1 simulation bit.
	Simulation bool

	// NdsCom sets the needs-commissioning flag.
	NdsCom bool

	// InitialStNum is the state number of the first message. Zero
	// uses 1.
	InitialStNum uint32

	// Values are the initial dataset values. When set, [Publisher.Run]
	// sends them immediately and later calls to [Publisher.Publish]
	// must keep the same number and types of members.
	Values []*mms.Value

	// Logger receives publisher diagnostics. Nil disables logging.
	Logger *slog.Logger

	// Metrics receives counters. Nil disables metrics.
	Metrics Metrics

	// Now returns the current time; nil uses [time.Now]. It paces the
	// retransmissions and, without Clock, stamps T. Intended for tests.
	Now func() time.Time

	// Clock, when non-nil, stamps every new state: it returns T and
	// its TimeQuality octet, which then replaces TimeQuality.
	// Retransmission timing keeps using Now, so a step of Clock does
	// not change the retransmission curve. The server's GOOSE engine
	// sets it from ServerOptions.Clock.
	Clock func() (t time.Time, quality uint8)
}

func (c *PublisherConfig) normalize() error {
	if c.Dst.IsZero() {
		return configErr("Dst", "destination address required")
	}
	if c.Dst[0]&0x01 == 0 {
		// 8-1 Table 12: DstAddress must be a multicast Ethernet
		// address.
		return configErr("Dst", "%s is not a multicast address", c.Dst)
	}
	if c.GocbRef == "" {
		return configErr("GocbRef", "required")
	}
	if len(c.GocbRef) > MaxRefLen || len(c.DatSet) > MaxRefLen {
		return configErr("GocbRef/DatSet", "longer than %d octets (8-1 18.1.2.5.2)", MaxRefLen)
	}
	if c.DatSet == "" && !c.NdsCom {
		return configErr("DatSet", "required unless NdsCom is set")
	}
	if c.VLAN != nil && (c.VLAN.Priority > 7 || c.VLAN.ID > 0x0fff) {
		return configErr("VLAN", "priority %d / id %d out of range", c.VLAN.Priority, c.VLAN.ID)
	}
	if c.MinTime < 0 || c.MaxTime < 0 || c.TimeAllowedToLive < 0 {
		return configErr("MinTime/MaxTime", "negative duration")
	}
	if c.MinTime == 0 {
		c.MinTime = DefaultMinTime
	}
	if c.MaxTime == 0 {
		c.MaxTime = DefaultMaxTime
	}
	if c.MinTime > c.MaxTime {
		return configErr("MinTime", "%v greater than MaxTime %v", c.MinTime, c.MaxTime)
	}
	if c.MaxTime >= MaxRetransmission {
		return configErr("MaxTime", "%v not less than %v (8-1 18.1.2.5.1)", c.MaxTime, MaxRetransmission)
	}
	if c.TimeAllowedToLive == 0 {
		c.TimeAllowedToLive = 2 * c.MaxTime
	}
	if c.TimeAllowedToLive < time.Millisecond {
		return configErr("TimeAllowedToLive", "%v below 1 ms (8-1 18.1.2.5.2: 1..2^32-1 ms)", c.TimeAllowedToLive)
	}
	if c.TimeAllowedToLive/time.Millisecond > 0xffffffff {
		return configErr("TimeAllowedToLive", "exceeds 2^32-1 ms")
	}
	if c.TimeAllowedToLive <= c.MaxTime {
		return configErr("TimeAllowedToLive", "%v not greater than MaxTime %v (8-1 18.1.2.5.1 Figure 10)", c.TimeAllowedToLive, c.MaxTime)
	}
	if len(c.GoID) > MaxGoIDLen {
		return configErr("GoID", "longer than %d octets (8-1 18.1.2.5.2)", MaxGoIDLen)
	}
	if c.GoID == "" {
		// The default GoID is the GoCB reference (8-1 Table 73), which
		// may be longer than 65 octets; it is sent as is.
		c.GoID = c.GocbRef
	}
	if c.TimeQuality == 0 {
		c.TimeQuality = DefaultTimeQuality
	}
	if c.InitialStNum == 0 {
		c.InitialStNum = 1
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return nil
}

// PublisherState is a snapshot of a publisher's sequence state.
type PublisherState struct {
	StNum, SqNum uint32
	T            time.Time // time of the last state change
	Enabled      bool
	Interval     time.Duration // current retransmission interval
	Sent         uint64        // frames sent
}

// Publisher sends one GOOSE control block's messages: a new state
// (stNum+1, sqNum 0) on every [Publisher.Publish] followed by
// retransmissions at MinTime, 2*MinTime, 4*MinTime ... up to the
// MaxTime heartbeat (IEC 61850-8-1 Annex C).
//
// A Publisher is safe for concurrent use. Publish sends synchronously
// for minimum latency; [Publisher.Run] drives retransmissions.
type Publisher struct {
	t       Transport
	logger  *slog.Logger
	metrics Metrics

	mu       sync.Mutex
	cfg      PublisherConfig
	values   []*mms.Value
	stNum    uint32
	sqNum    uint32
	tChange  time.Time
	tq       uint8 // TimeQuality of tChange
	interval time.Duration
	nextAt   time.Time
	enabled  bool
	started  bool // a first state has been sent
	sent     uint64
	buf      []byte
	frame    Frame
	fx       *fixedLength // non-nil for FixedOffs

	wake      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

// NewPublisher validates cfg and returns a publisher bound to t. It
// sends nothing until [Publisher.Run] or [Publisher.Publish] is called.
func NewPublisher(t Transport, cfg PublisherConfig) (*Publisher, error) {
	if t == nil {
		return nil, configErr("Transport", "nil transport")
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	if cfg.Src.IsZero() {
		cfg.Src = t.HardwareAddr()
	}
	logger := orDiscard(cfg.Logger).With("gocbRef", cfg.GocbRef)
	p := &Publisher{
		t:       t,
		logger:  logger,
		metrics: guardMetrics(cfg.Metrics, logger, nil),
		cfg:     cfg,
		stNum:   cfg.InitialStNum - 1,
		enabled: true,
		wake:    make(chan struct{}, 1),
		closed:  make(chan struct{}),
	}
	if cfg.FixedOffs {
		p.fx = &fixedLength{types: cfg.MemberTypes}
	}
	if cfg.Values != nil {
		p.values = cloneValues(cfg.Values)
		// Validate encodability up front.
		if _, err := p.fx.encodeAllData(p.values); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Config returns the effective configuration (defaults applied).
func (p *Publisher) Config() PublisherConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.cfg
	c.Values = cloneValues(p.values)
	return c
}

// State returns the current sequence state.
func (p *Publisher) State() PublisherState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PublisherState{StNum: p.stNum, SqNum: p.sqNum, T: p.tChange, Enabled: p.enabled, Interval: p.interval, Sent: p.sent}
}

// Publish starts a new state with the given dataset values: stNum is
// incremented (rolling over to 1), sqNum reset to 0, T set to now, and
// the message sent immediately. The retransmission curve restarts.
//
// The number of values and their types must match the previous state
// (or PublisherConfig.Values); otherwise Publish fails with
// [ErrInvalidValue] and nothing is sent. The values are deep-copied.
func (p *Publisher) Publish(values []*mms.Value) error {
	if err := p.checkOpen(); err != nil {
		return err
	}
	vals := cloneValues(values)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.values != nil {
		if len(vals) != len(p.values) {
			return errValue("got %d values, dataset has %d members", len(vals), len(p.values))
		}
		for i := range vals {
			if !SameShape(vals[i], p.values[i]) {
				return &EncodeError{Field: "values", Index: i, Err: errValue("type differs from dataset member")}
			}
		}
	}
	if _, err := p.fx.encodeAllData(vals); err != nil {
		return err
	}
	p.values = vals
	return p.newStateLocked()
}

// SetFlags changes the test, simulation and needs-commissioning flags.
// The change is sent as a new state.
func (p *Publisher) SetFlags(test, simulation, ndsCom bool) error {
	if err := p.checkOpen(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.Test == test && p.cfg.Simulation == simulation && p.cfg.NdsCom == ndsCom {
		return nil
	}
	p.cfg.Test, p.cfg.Simulation, p.cfg.NdsCom = test, simulation, ndsCom
	if !p.started {
		return nil
	}
	return p.newStateLocked()
}

// SetEnabled starts or stops transmission (GoEna). Re-enabling sends
// the current values as a new state with stNum 1: enabling GoEna
// restarts StNum at 1 (IEC 61850-7-2 18.2.3.6).
func (p *Publisher) SetEnabled(enabled bool) error {
	if err := p.checkOpen(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.enabled == enabled {
		return nil
	}
	p.enabled = enabled
	p.logger.Info("goose: publisher enabled changed", "enabled", enabled)
	if enabled && p.values != nil {
		p.stNum = 0 // newStateLocked makes it 1 (7-2 18.2.3.6)
		return p.newStateLocked()
	}
	p.signal()
	return nil
}

func (p *Publisher) newStateLocked() error {
	p.stNum++
	if p.stNum == 0 {
		p.stNum = 1
	}
	p.sqNum = 0
	now := p.cfg.Now()
	p.tChange, p.tq = now, p.cfg.TimeQuality
	if p.cfg.Clock != nil {
		p.tChange, p.tq = p.cfg.Clock()
	}
	p.started = true
	p.interval = p.cfg.MinTime
	p.nextAt = now.Add(p.interval)
	p.signal()
	if !p.enabled {
		return nil
	}
	return p.sendLocked(false)
}

func (p *Publisher) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Publisher) sendLocked(retrans bool) error {
	c := &p.cfg
	p.frame = Frame{
		Dst: c.Dst, Src: c.Src, VLAN: c.VLAN, AppID: c.AppID, Simulation: c.Simulation,
		PDU: PDU{
			GocbRef:           c.GocbRef,
			TimeAllowedToLive: talMillis(c.TimeAllowedToLive),
			DatSet:            c.DatSet,
			GoID:              c.GoID,
			T:                 p.tChange,
			TimeQuality:       p.tq,
			StNum:             p.stNum,
			SqNum:             p.sqNum,
			Test:              c.Test,
			ConfRev:           c.ConfRev,
			NdsCom:            c.NdsCom,
			AllData:           p.values,
		},
	}
	var err error
	p.buf, err = appendFrame(p.buf[:0], &p.frame, p.fx)
	if err != nil {
		return err
	}
	if err := p.t.Send(p.buf); err != nil {
		p.metrics.SendError(c.GocbRef, err)
		p.logger.Warn("goose: send failed", "stNum", p.stNum, "sqNum", p.sqNum, "error", err)
		return err
	}
	p.sent++
	p.metrics.FrameSent(c.GocbRef, retrans)
	if p.logger.Enabled(context.Background(), slog.LevelDebug) {
		p.logger.Debug("goose: sent", "stNum", p.stNum, "sqNum", p.sqNum, "retransmission", retrans)
	}
	return nil
}

// retransmit sends the next retransmission if it is due and returns the
// time of the following one (zero if nothing is scheduled).
func (p *Publisher) retransmit(now time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || !p.enabled {
		return time.Time{}
	}
	if now.Before(p.nextAt) {
		return p.nextAt
	}
	p.sqNum++
	if p.sqNum == 0 {
		p.sqNum = 1
	}
	p.interval *= 2
	if p.interval > p.cfg.MaxTime {
		p.interval = p.cfg.MaxTime
	}
	p.nextAt = now.Add(p.interval)
	_ = p.sendLocked(true) // errors are logged and counted; keep the heartbeat going
	return p.nextAt
}

// Run drives retransmissions until ctx is done or the publisher is
// closed; it then returns nil. If PublisherConfig.Values was set and no
// state has been published yet, Run first sends the initial state.
func (p *Publisher) Run(ctx context.Context) error {
	if err := p.checkOpen(); err != nil {
		return err
	}
	p.mu.Lock()
	if !p.started && p.values != nil {
		if err := p.newStateLocked(); err != nil {
			p.logger.Warn("goose: initial send failed", "error", err)
		}
	}
	p.mu.Unlock()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		next := p.retransmit(p.cfg.Now())
		wait := time.Hour
		if !next.IsZero() {
			wait = max(next.Sub(p.cfg.Now()), 0)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return nil
		case <-p.closed:
			return nil
		case <-p.wake:
		case <-timer.C:
		}
	}
}

// Close stops the publisher. Run returns and further calls fail with
// [ErrClosed]. The transport is not closed.
func (p *Publisher) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

func (p *Publisher) checkOpen() error {
	select {
	case <-p.closed:
		return ErrClosed
	default:
		return nil
	}
}

func cloneValues(vs []*mms.Value) []*mms.Value {
	if vs == nil {
		return nil
	}
	out := make([]*mms.Value, len(vs))
	for i, v := range vs {
		if v != nil {
			out[i] = v.Clone()
		}
	}
	return out
}

// SameShape reports whether a and b have the same MMS type and, for
// structures and arrays, recursively the same element shapes. Values
// are not compared.
func SameShape(a, b *mms.Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Type() {
	case mms.ValueTypeStructure:
		ea, _ := a.Structure()
		eb, _ := b.Structure()
		if len(ea) != len(eb) {
			return false
		}
		for i := range ea {
			if !SameShape(ea[i], eb[i]) {
				return false
			}
		}
	case mms.ValueTypeArray:
		ea, _ := a.ArrayElements()
		eb, _ := b.ArrayElements()
		if len(ea) != len(eb) {
			return false
		}
		for i := range ea {
			if !SameShape(ea[i], eb[i]) {
				return false
			}
		}
	default:
		// Scalars: equal types are the same shape.
	}
	return true
}

// talMillis converts TimeAllowedToLive to the goosePdu
// timeAllowedToLive milliseconds. normalize already rejects values
// above 2^32-1 ms; the clamp only makes that bound visible at the
// conversion and never changes an accepted value.
func talMillis(d time.Duration) uint32 {
	return uint32(min(max(d/time.Millisecond, 0), math.MaxUint32))
}
