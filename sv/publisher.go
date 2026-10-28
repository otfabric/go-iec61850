// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-mms"
)

// DefaultNominalFrequency is the nominal power system frequency used to
// convert a SmpPerPeriod sample rate into samples per second.
const DefaultNominalFrequency = 50.0

// DefaultMaxBurst is the default number of frames a late publisher sends
// back-to-back to catch up with its schedule before it skips samples.
const DefaultMaxBurst = 16

// PublisherConfig configures a [Publisher].
type PublisherConfig struct {
	// Dst is the destination address (usually in 01-0C-CD-04-00-00 to
	// 01-0C-CD-04-01-FF). Src defaults to the transport address.
	Dst, Src MAC

	// VLAN, when non-nil, tags frames with 802.1Q.
	VLAN *VLAN

	// AppID is the APPID (SV range 0x4000-0x7FFF recommended).
	AppID uint16

	// Simulation sets the Reserved1 simulation bit.
	Simulation bool

	// SvID is the MsvID/UsvID (required).
	SvID string

	// DatSet is sent in every ASDU when non-empty (OptFlds dataSet).
	DatSet string

	// ConfRev is the configuration revision.
	ConfRev uint32

	// SmpRate and SmpMod define the sample rate (required, SmpRate > 0):
	// samples per nominal period (SmpPerPeriod, with NominalFrequency),
	// samples per second (SmpPerSec) or seconds per sample (SecPerSmp).
	SmpRate uint16
	SmpMod  SmpMod

	// NominalFrequency is the power system frequency in Hz for
	// SmpPerPeriod (default [DefaultNominalFrequency]).
	NominalFrequency float64

	// NoASDU is the number of samples per frame (default 1).
	NoASDU int

	// SmpSynch is the initial synchronization state.
	SmpSynch uint8

	// Optional ASDU fields (the OptFlds of the SVCB): RefrTm sends the
	// sample time as refrTm, SendSmpRate sends smpRate, SendSmpMod sends
	// smpMod, and GmIdentity, when non-nil, is sent as gmIdentity.
	RefrTm      bool
	SendSmpRate bool
	SendSmpMod  bool
	GmIdentity  *[8]byte

	// SmpCntWrap is the smpCnt modulus. Zero means the number of samples
	// per second when that is a whole number of at most 65536 (so that,
	// with AlignSecond, smpCnt 0 falls on the second), else 65536.
	SmpCntWrap int

	// AlignSecond starts the schedule at the next full second (of Now)
	// with smpCnt 0, as synchronized merging units do.
	AlignSecond bool

	// Layout describes the sample for [Publisher.Update]. Optional when
	// samples are given as bytes ([Publisher.UpdateSample], Sample or
	// Source).
	Layout Layout

	// Sample is the initial sample. Nil means a zero sample of
	// Layout.Size() octets.
	Sample []byte

	// Source, when non-nil, produces every sample instead of the last
	// value given to Update: it appends the sample for info to dst and
	// returns it. It runs on the publisher goroutine and must not block.
	Source func(info SampleInfo, dst []byte) []byte

	// Disabled starts the publisher with SvEna false.
	Disabled bool

	// MaxBurst bounds catch-up after a late wake-up (default
	// [DefaultMaxBurst] frames); samples beyond it are skipped.
	MaxBurst int

	// Now is the clock (default time.Now). Tests may substitute it.
	Now func() time.Time

	// Logger receives diagnostics; nil disables logging.
	Logger *slog.Logger

	// Metrics receives counters; nil disables metrics.
	Metrics Metrics
}

// SampleInfo describes the sample a [PublisherConfig].Source produces.
type SampleInfo struct {
	// Index counts samples since Run started (0-based, not wrapped).
	Index uint64

	// SmpCnt is the sample counter sent with the sample.
	SmpCnt uint16

	// Time is the scheduled sampling instant.
	Time time.Time
}

// PublisherState is a snapshot of a publisher.
type PublisherState struct {
	Enabled  bool
	SmpSynch uint8

	// NextSmpCnt is the counter of the next sample.
	NextSmpCnt uint16

	// Samples and Frames count what was sent; Skipped counts samples
	// dropped to catch up after the publisher fell behind; Late counts
	// frames sent after their scheduled time plus one sample period;
	// SendErrors counts frames the transport rejected.
	Samples, Frames, Skipped, Late, SendErrors uint64

	// Rate is the configured rate in samples per second.
	Rate float64
}

// Publisher sends a Sampled Values stream at its configured rate.
// Create it with [NewPublisher], then call [Publisher.Run]. Update,
// UpdateSample and the Set methods are safe for concurrent use.
type Publisher struct {
	t       Transport
	cfg     PublisherConfig
	period  float64 // ns per sample
	wrap    int
	logger  *slog.Logger
	metrics Metrics

	mu       sync.Mutex
	sample   []byte
	spare    []byte
	enabled  bool
	smpSynch uint8
	sim      bool
	nextCnt  uint16
	wake     chan struct{}

	// Scratch storage of the sending goroutine.
	buf   []byte
	cur   []byte
	src   []byte
	asdus []ASDU
	offs  []int

	closed  chan struct{}
	closeMu sync.Once
	running atomic.Bool

	samples, frames, skipped, late, sendErrs atomic.Uint64
}

// NewPublisher validates cfg and returns a publisher on t.
func NewPublisher(t Transport, cfg PublisherConfig) (*Publisher, error) {
	if t == nil {
		return nil, configErr("Transport", "nil transport")
	}
	if cfg.SvID == "" || !isVisible(cfg.SvID) {
		return nil, configErr("SvID", "required VisibleString")
	}
	if !isVisible(cfg.DatSet) {
		return nil, configErr("DatSet", "must be a VisibleString")
	}
	if cfg.SmpRate == 0 {
		return nil, configErr("SmpRate", "must be positive")
	}
	if cfg.SmpMod > SecPerSmp {
		return nil, configErr("SmpMod", "unknown sample mode %d", cfg.SmpMod)
	}
	if cfg.NominalFrequency == 0 {
		cfg.NominalFrequency = DefaultNominalFrequency
	}
	if cfg.NominalFrequency < 0 || math.IsNaN(cfg.NominalFrequency) || math.IsInf(cfg.NominalFrequency, 0) {
		return nil, configErr("NominalFrequency", "invalid %v", cfg.NominalFrequency)
	}
	if cfg.NoASDU == 0 {
		cfg.NoASDU = 1
	}
	if cfg.NoASDU < 0 || cfg.NoASDU > 0xffff {
		return nil, configErr("NoASDU", "%d out of range", cfg.NoASDU)
	}
	if cfg.VLAN != nil {
		if _, ok := cfg.VLAN.TCI(); !ok {
			return nil, configErr("VLAN", "priority %d / id %d out of range", cfg.VLAN.Priority, cfg.VLAN.ID)
		}
	}
	if err := cfg.Layout.Validate(); err != nil {
		return nil, err
	}
	if cfg.MaxBurst <= 0 {
		cfg.MaxBurst = DefaultMaxBurst
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Src.IsZero() {
		cfg.Src = t.HardwareAddr()
	}
	rate := SamplesPerSecond(cfg.SmpRate, cfg.SmpMod, cfg.NominalFrequency)
	wrap := cfg.SmpCntWrap
	if wrap == 0 {
		wrap = DefaultSmpCntWrap(rate)
	}
	if wrap < 1 || wrap > 65536 {
		return nil, configErr("SmpCntWrap", "%d out of range 1..65536", wrap)
	}
	sample := cfg.Sample
	if sample == nil {
		sample = make([]byte, cfg.Layout.Size())
	} else {
		sample = clone(sample)
	}
	if len(cfg.Layout) > 0 && len(sample) != cfg.Layout.Size() {
		return nil, configErr("Sample", "%d octets, layout needs %d", len(sample), cfg.Layout.Size())
	}
	p := &Publisher{
		t: t, cfg: cfg, period: 1e9 / rate, wrap: wrap,
		logger:  orDiscard(cfg.Logger).With("svID", cfg.SvID),
		metrics: orNop(cfg.Metrics),
		sample:  sample, enabled: !cfg.Disabled, smpSynch: cfg.SmpSynch, sim: cfg.Simulation,
		wake: make(chan struct{}, 1), closed: make(chan struct{}),
	}
	p.asdus = make([]ASDU, cfg.NoASDU)
	p.offs = make([]int, cfg.NoASDU+1)
	p.buf = make([]byte, 0, MaxPayload+32)
	// Check that a frame fits before Run (a Source is not called here).
	p.cur = append(p.cur[:0], sample...)
	src := p.cfg.Source
	p.cfg.Source = nil
	_, err := p.encodeAt(nil, 0, time.Unix(0, 0), 0, cfg.SmpSynch, cfg.Simulation)
	p.cfg.Source = src
	if err != nil {
		return nil, err
	}
	return p, nil
}

// SamplesPerSecond converts a sample rate to samples per second.
func SamplesPerSecond(smpRate uint16, mod SmpMod, nominalFrequency float64) float64 {
	switch mod {
	case SmpPerSec:
		return float64(smpRate)
	case SecPerSmp:
		return 1 / float64(smpRate)
	default:
		if nominalFrequency == 0 {
			nominalFrequency = DefaultNominalFrequency
		}
		return float64(smpRate) * nominalFrequency
	}
}

// DefaultSmpCntWrap returns the smpCnt modulus for a rate in samples per
// second: the rate when it is a whole number in 1..65536, else 65536.
func DefaultSmpCntWrap(rate float64) int {
	if r := math.Round(rate); r >= 1 && r <= 65536 && math.Abs(r-rate) < 1e-9 {
		return int(r)
	}
	return 65536
}

// Config returns the effective configuration.
func (p *Publisher) Config() PublisherConfig { return p.cfg }

// Update sets the sample from one MMS value per Layout leaf. The new
// sample is sent from the next scheduled sample on.
func (p *Publisher) Update(values []*mms.Value) error {
	if len(p.cfg.Layout) == 0 {
		return configErr("Layout", "Update needs a Layout; use UpdateSample")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	b, err := p.cfg.Layout.Encode(p.spare[:0], values)
	if err != nil {
		return err
	}
	p.sample, p.spare = b, p.sample
	return nil
}

// UpdateSample sets the encoded sample. With a Layout, its size must
// match. The slice is copied.
func (p *Publisher) UpdateSample(sample []byte) error {
	if len(p.cfg.Layout) > 0 && len(sample) != p.cfg.Layout.Size() {
		return errValue("sample of %d octets, layout needs %d", len(sample), p.cfg.Layout.Size())
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spare = append(p.spare[:0], sample...)
	p.sample, p.spare = p.spare, p.sample
	return nil
}

// SetEnabled starts (SvEna true) or stops sending. When enabled again
// the schedule restarts at the current time and smpCnt continues.
func (p *Publisher) SetEnabled(enabled bool) {
	p.mu.Lock()
	p.enabled = enabled
	p.mu.Unlock()
	p.poke()
}

// SetSmpSynch sets the synchronization state sent from the next frame.
func (p *Publisher) SetSmpSynch(s uint8) {
	p.mu.Lock()
	p.smpSynch = s
	p.mu.Unlock()
}

// SetSimulation sets the simulation bit from the next frame.
func (p *Publisher) SetSimulation(sim bool) {
	p.mu.Lock()
	p.sim = sim
	p.mu.Unlock()
}

// State returns a snapshot of the publisher.
func (p *Publisher) State() PublisherState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PublisherState{
		Enabled: p.enabled, SmpSynch: p.smpSynch, NextSmpCnt: p.nextCnt,
		Samples: p.samples.Load(), Frames: p.frames.Load(), Skipped: p.skipped.Load(),
		Late: p.late.Load(), SendErrors: p.sendErrs.Load(), Rate: 1e9 / p.period,
	}
}

func (p *Publisher) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Close stops Run. It does not close the transport.
func (p *Publisher) Close() error {
	p.closeMu.Do(func() { close(p.closed) })
	return nil
}

// Run sends the stream until ctx is done (returning nil) or the
// publisher is closed (returning nil). Transport errors are counted and
// logged but do not stop Run, except [ErrClosed] from the transport,
// which is returned. Run may be called only once at a time.
func (p *Publisher) Run(ctx context.Context) error {
	if !p.running.CompareAndSwap(false, true) {
		return configErr("Run", "already running")
	}
	defer p.running.Store(false)
	select {
	case <-p.closed:
		return ErrClosed
	default:
	}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	for {
		// Wait until enabled.
		for !p.isEnabled() {
			select {
			case <-ctx.Done():
				return nil
			case <-p.closed:
				return nil
			case <-p.wake:
			}
		}
		t0 := p.cfg.Now()
		if p.cfg.AlignSecond {
			t0 = t0.Truncate(time.Second).Add(time.Second)
			p.mu.Lock()
			p.nextCnt = 0
			p.mu.Unlock()
		}
		var k uint64 // next sample index relative to t0
		for p.isEnabled() {
			due := p.at(t0, k+uint64(p.cfg.NoASDU)-1)
			if d := due.Sub(p.cfg.Now()); d > 0 {
				timer.Reset(d)
				select {
				case <-ctx.Done():
					return nil
				case <-p.closed:
					return nil
				case <-p.wake:
					timer.Stop() // Go 1.23+ timers: no stale value to drain
					continue
				case <-timer.C:
				}
			} else {
				// Behind schedule: still honour ctx and Close.
				select {
				case <-ctx.Done():
					return nil
				case <-p.closed:
					return nil
				default:
				}
			}
			now := p.cfg.Now()
			for n := 0; n < p.cfg.MaxBurst; n++ {
				last := k + uint64(p.cfg.NoASDU) - 1
				if p.at(t0, last).After(now) {
					break
				}
				if now.Sub(p.at(t0, last)) > time.Duration(p.period) {
					p.late.Add(1)
				}
				if err := p.sendFrame(t0, k); errors.Is(err, ErrClosed) {
					return err
				}
				k += uint64(p.cfg.NoASDU)
			}
			// Still behind after a full burst: skip to the present.
			if behind := p.index(t0, now); behind > k+uint64(p.cfg.NoASDU) {
				skip := behind - k
				skip -= skip % uint64(p.cfg.NoASDU)
				p.skipped.Add(skip)
				p.mu.Lock()
				p.nextCnt = uint16((uint64(p.nextCnt) + skip) % uint64(p.wrap))
				p.mu.Unlock()
				k += skip
				p.logger.Warn("sv: publisher behind schedule, samples skipped", "skipped", skip)
			}
		}
	}
}

func (p *Publisher) isEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled
}

// at returns the scheduled time of sample k.
func (p *Publisher) at(t0 time.Time, k uint64) time.Time {
	return t0.Add(time.Duration(math.Round(float64(k) * p.period)))
}

// index returns the index of the last sample due at now.
func (p *Publisher) index(t0, now time.Time) uint64 {
	d := now.Sub(t0)
	if d < 0 {
		return 0
	}
	return uint64(float64(d) / p.period)
}

func (p *Publisher) sendFrame(t0 time.Time, k uint64) error {
	p.mu.Lock()
	cnt := p.nextCnt
	p.nextCnt = uint16((uint64(cnt) + uint64(p.cfg.NoASDU)) % uint64(p.wrap))
	if p.cfg.Source == nil {
		p.cur = append(p.cur[:0], p.sample...)
	}
	synch, sim := p.smpSynch, p.sim
	p.mu.Unlock()

	var err error
	p.buf, err = p.encodeAt(p.buf[:0], cnt, t0, k, synch, sim)
	if err != nil {
		p.sendErrs.Add(1)
		p.metrics.SendError(p.cfg.SvID, err)
		p.logger.Warn("sv: encode failed", "error", err)
		return err
	}
	if err := p.t.Send(p.buf); err != nil {
		p.sendErrs.Add(1)
		p.metrics.SendError(p.cfg.SvID, err)
		p.logger.Debug("sv: send failed", "error", err)
		return err
	}
	p.frames.Add(1)
	p.samples.Add(uint64(p.cfg.NoASDU))
	p.metrics.FrameSent(p.cfg.SvID, p.cfg.NoASDU)
	return nil
}

// encodeAt encodes the frame of samples k..k+NoASDU-1 (smpCnt from cnt)
// into dst, reusing the publisher's scratch storage. With a Source the
// samples are produced into p.src; otherwise every ASDU carries p.cur.
// Only one goroutine may call it at a time (Run, or NewPublisher before
// Run).
func (p *Publisher) encodeAt(dst []byte, cnt uint16, t0 time.Time, k uint64, synch uint8, sim bool) ([]byte, error) {
	n := p.cfg.NoASDU
	f := Frame{Dst: p.cfg.Dst, Src: p.cfg.Src, VLAN: p.cfg.VLAN, AppID: p.cfg.AppID, Simulation: sim}
	f.PDU.ASDUs = p.asdus[:n]
	if p.cfg.Source != nil {
		p.src = p.src[:0]
	}
	for i := 0; i < n; i++ {
		ti := p.at(t0, k+uint64(i))
		c := uint16((uint64(cnt) + uint64(i)) % uint64(p.wrap))
		a := &f.PDU.ASDUs[i]
		*a = ASDU{SvID: p.cfg.SvID, DatSet: p.cfg.DatSet, SmpCnt: c, ConfRev: p.cfg.ConfRev, SmpSynch: synch, Sample: p.cur}
		if p.cfg.RefrTm {
			a.RefrTm, a.HasRefrTm = ti, true
		}
		if p.cfg.SendSmpRate {
			a.SmpRate, a.HasSmpRate = p.cfg.SmpRate, true
		}
		if p.cfg.SendSmpMod {
			a.SmpMod, a.HasSmpMod = p.cfg.SmpMod, true
		}
		if p.cfg.GmIdentity != nil {
			a.GmIdentity, a.HasGmIdentity = *p.cfg.GmIdentity, true
		}
		if p.cfg.Source != nil {
			p.offs[i] = len(p.src)
			p.src = p.cfg.Source(SampleInfo{Index: k + uint64(i), SmpCnt: c, Time: ti}, p.src)
		}
	}
	if p.cfg.Source != nil {
		// Slice only after all appends: p.src may have been reallocated.
		p.offs[n] = len(p.src)
		for i := 0; i < n; i++ {
			f.PDU.ASDUs[i].Sample = p.src[p.offs[i]:p.offs[i+1]]
		}
	}
	return AppendFrame(dst, &f)
}
