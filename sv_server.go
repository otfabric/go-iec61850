// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/sv"
)

// ErrSV indicates a server-side Sampled Values configuration or
// runtime failure. Errors from [Server.EnableSV] wrap it.
var ErrSV = errors.New("iec61850: SV error")

// SVOptions configures [Server.EnableSV].
type SVOptions struct {
	// Transport sends the SV frames (required). Use [sv.Open] for a
	// network interface or a [sv.Hub] port for tests.
	Transport sv.Transport

	// StartDisabled leaves SvEna false for every MSVCB; clients (or
	// [SVEngine.SetEnabled]) enable publishing. By default every MSVCB
	// with a valid address and dataset publishes immediately.
	StartDisabled bool

	// SmpSynch is the smpSynch value sent (0 none, 1 local, 2 global,
	// 5-254 PTP grandmaster). Change it at runtime with
	// [sv.Publisher.SetSmpSynch].
	SmpSynch uint8

	// NominalFrequency is the power system frequency for SmpPerPeriod
	// (default 50 Hz).
	NominalFrequency float64

	// Logger receives SV diagnostics. Nil uses the server logger.
	Logger *slog.Logger

	// Metrics receives publisher counters. Nil disables metrics.
	Metrics sv.Metrics
}

// SVEngine publishes the multicast Sampled Values control blocks of a
// [Server]'s model.
//
// Every MSVCB (SCL SampledValueControl in LLN0 with an SMV address)
// gets an [sv.Publisher] that sends the current dataset values at
// SmpRate. When a dataset member changes through [Server.SetValue] or
// an MMS client write, the next samples carry the new value. For
// waveform data, take the publisher with [SVEngine.Publisher] and feed
// it directly ([sv.Publisher.UpdateSample]).
//
// The MSVCB attributes are served over MMS under the MS functional
// constraint: SvEna, MsvID, DatSet, SmpRate, OptFlds and SmpMod are
// writable (all but SvEna only while SvEna is false; a DatSet change
// increments ConfRev); ConfRev, DstAddress and noASDU are read-only.
// Unicast SV (USVCB) is not supported.
type SVEngine struct {
	s    *Server
	opts SVOptions
	log  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu    sync.RWMutex
	svcbs map[string]*svcbRuntime // key: store-key prefix "LD/LLN0$MS$name"
}

type svcbRuntime struct {
	e      *SVEngine
	ldName string
	lnName string
	def    servermodel.SvCBDef
	prefix string

	mu      sync.Mutex
	members []*servermodel.MemberNode
	layout  sv.Layout
	keys    map[string]bool
	pub     *sv.Publisher
	stop    context.CancelFunc
}

// EnableSV creates the SV engine and starts publishing. Call it after
// [NewServer]; [Server.Close] stops it.
func (s *Server) EnableSV(opts SVOptions) (*SVEngine, error) {
	if opts.Transport == nil {
		return nil, fmt.Errorf("%w: nil transport", ErrSV)
	}
	if s.svEngine != nil {
		return nil, fmt.Errorf("%w: already enabled", ErrSV)
	}
	if opts.Logger == nil {
		opts.Logger = s.logger
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &SVEngine{s: s, opts: opts, log: opts.Logger, ctx: ctx, cancel: cancel, svcbs: map[string]*svcbRuntime{}}
	for i := range s.model.LogicalDevices {
		ld := &s.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.SvCBs {
				rt := &svcbRuntime{e: e, ldName: ld.Name, lnName: ln.Name, def: ln.SvCBs[k]}
				rt.prefix = servermodel.StoreKey(ld.Name, servermodel.SvCBItemID(ln.Name, rt.def.Name))
				e.svcbs[rt.prefix] = rt
			}
		}
	}
	s.svEngine = e
	for _, rt := range e.sorted() {
		rt.mu.Lock()
		err := rt.resolveLocked()
		rt.mu.Unlock()
		if err != nil {
			e.log.Warn("iec61850: SvCB needs commissioning", "svcbRef", rt.prefix, "error", err)
			continue
		}
		if !opts.StartDisabled {
			if err := rt.setEnabled(true); err != nil {
				e.Stop()
				s.svEngine = nil
				return nil, fmt.Errorf("%w: %s: %w", ErrSV, rt.prefix, err)
			}
		}
	}
	e.log.Info("iec61850: SV engine enabled", "svcbs", len(e.svcbs))
	return e, nil
}

// SVEngine returns the SV engine, or nil if [Server.EnableSV] has not
// been called.
func (s *Server) SVEngine() *SVEngine { return s.svEngine }

func (e *SVEngine) sorted() []*svcbRuntime {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*svcbRuntime, 0, len(e.svcbs))
	for _, rt := range e.svcbs {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].prefix < out[j].prefix })
	return out
}

// ControlBlocks returns the references ("LD/LLN0$MS$name") of all
// MSVCBs.
func (e *SVEngine) ControlBlocks() []string {
	var out []string
	for _, rt := range e.sorted() {
		out = append(out, rt.prefix)
	}
	return out
}

func (e *SVEngine) lookup(ref string) (*svcbRuntime, error) {
	e.mu.RLock()
	rt := e.svcbs[ref]
	e.mu.RUnlock()
	if rt == nil {
		return nil, fmt.Errorf("%w: SvCB %q: %w", ErrSV, ref, ErrNotFound)
	}
	return rt, nil
}

// Publisher returns the publisher of an MSVCB ("LD/LLN0$MS$name"), or
// nil while it is disabled.
func (e *SVEngine) Publisher(ref string) (*sv.Publisher, error) {
	rt, err := e.lookup(ref)
	if err != nil {
		return nil, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.pub, nil
}

// SetEnabled sets SvEna of an MSVCB, exactly like an MMS client write.
func (e *SVEngine) SetEnabled(ref string, enabled bool) error {
	rt, err := e.lookup(ref)
	if err != nil {
		return err
	}
	return rt.setEnabled(enabled)
}

// Stop stops all publishers. It is idempotent; [Server.Close] calls it.
func (e *SVEngine) Stop() {
	e.cancel()
	e.wg.Wait()
}

func (rt *svcbRuntime) key(field string) string { return rt.prefix + "$" + field }

func (rt *svcbRuntime) get(field string) *mms.Value { return rt.e.s.store.Get(rt.key(field)) }

func (rt *svcbRuntime) str(field string) string {
	v, _ := rt.get(field).VisibleString()
	return v
}

func (rt *svcbRuntime) uint(field string) uint64 {
	v, _ := rt.get(field).Uint64()
	return v
}

// resolveLocked resolves the current DatSet to member layouts.
func (rt *svcbRuntime) resolveLocked() error {
	rt.members, rt.layout, rt.keys = nil, nil, nil
	if !rt.def.HasAddress {
		return errors.New("no SMV address")
	}
	ds := rt.str("DatSet")
	if ds == "" {
		return errors.New("empty DatSet")
	}
	def, _ := rt.e.s.model.FindDataSet(rt.ldName, ds)
	if def == nil {
		return fmt.Errorf("dataset %q not found", ds)
	}
	members := make([]*servermodel.MemberNode, len(def.Members))
	var layout sv.Layout
	keys := map[string]bool{}
	for i, m := range def.Members {
		n, err := rt.e.s.model.ResolveMember(rt.ldName, m)
		if err != nil {
			return err
		}
		members[i] = n
		for _, l := range n.Leaves() {
			k, ok := sv.KindOf(l.BType)
			if !ok {
				return fmt.Errorf("member %s: basic type %q cannot be sampled", l.Key, l.BType)
			}
			layout = append(layout, k)
			keys[l.Key] = true
		}
	}
	rt.members, rt.layout, rt.keys = members, layout, keys
	return nil
}

// values returns the current leaf values of the dataset, in layout
// order.
func (rt *svcbRuntime) values() []*mms.Value {
	out := make([]*mms.Value, 0, len(rt.layout))
	for _, m := range rt.members {
		for _, l := range m.Leaves() {
			out = append(out, rt.e.s.store.ReadMember(l))
		}
	}
	return out
}

// setEnabled implements SvEna.
func (rt *svcbRuntime) setEnabled(on bool) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !on {
		if rt.stop != nil {
			rt.stop()
			rt.stop = nil
		}
		if rt.pub != nil {
			_ = rt.pub.Close()
			rt.pub = nil
		}
		rt.e.s.store.Set(rt.key("SvEna"), mms.NewBoolean(false))
		rt.e.log.Info("iec61850: SvCB disabled", "svcbRef", rt.prefix)
		return nil
	}
	if rt.pub != nil {
		return nil
	}
	if rt.members == nil {
		return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	}
	if err := rt.e.ctx.Err(); err != nil {
		return err
	}
	opts, _ := servermodel.SvOptFldsFromValue(rt.get("OptFlds"))
	smpMod, _ := rt.get("SmpMod").Int64()
	cfg := sv.PublisherConfig{
		Dst:              sv.MAC(rt.def.DstAddr),
		AppID:            rt.def.AppID,
		SvID:             rt.str("MsvID"),
		ConfRev:          uint32(rt.uint("ConfRev")),
		SmpRate:          uint16(rt.uint("SmpRate")),
		SmpMod:           sv.SmpMod(smpMod),
		NoASDU:           int(rt.uint("noASDU")),
		NominalFrequency: rt.e.opts.NominalFrequency,
		SmpSynch:         rt.e.opts.SmpSynch,
		RefrTm:           opts.RefreshTime,
		SendSmpRate:      opts.SampleRate,
		SendSmpMod:       opts.SampleRate && smpMod != 0,
		Layout:           rt.layout,
		Logger:           rt.e.log,
		Metrics:          rt.e.opts.Metrics,
	}
	if opts.DataSet {
		cfg.DatSet = rt.str("DatSet")
	}
	if rt.def.HasVLAN {
		cfg.VLAN = &sv.VLAN{Priority: rt.def.VLANPriority, ID: rt.def.VLANID}
	}
	sample, err := rt.layout.Encode(nil, rt.values())
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrSV, rt.prefix, err)
	}
	cfg.Sample = sample
	pub, err := sv.NewPublisher(rt.e.opts.Transport, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(rt.e.ctx)
	rt.pub, rt.stop = pub, cancel
	rt.e.s.store.Set(rt.key("SvEna"), mms.NewBoolean(true))
	rt.e.wg.Add(1)
	go func() {
		defer rt.e.wg.Done()
		if err := pub.Run(ctx); err != nil {
			rt.e.log.Warn("iec61850: SV publisher stopped", "svcbRef", rt.prefix, "error", err)
		}
		_ = pub.Close()
	}()
	rt.e.log.Info("iec61850: SvCB enabled", "svcbRef", rt.prefix, "svID", cfg.SvID, "leaves", len(rt.layout))
	return nil
}

// notify refreshes the sample of every enabled MSVCB whose dataset
// contains storeKey.
func (e *SVEngine) notify(storeKey string) {
	if e.ctx.Err() != nil {
		return
	}
	for _, rt := range e.sorted() {
		rt.mu.Lock()
		if rt.pub != nil && rt.keys[storeKey] {
			if err := rt.pub.Update(rt.values()); err != nil {
				e.log.Warn("iec61850: SV sample update failed", "svcbRef", rt.prefix, "error", err)
			}
		}
		rt.mu.Unlock()
	}
}

// handleWrite processes an MMS client write to an MSVCB attribute.
// handled=false means the attribute is not an MSVCB attribute.
func (e *SVEngine) handleWrite(storeKey string, val *mms.Value) (bool, error) {
	i := strings.LastIndex(storeKey, "$")
	if i < 0 {
		return false, nil
	}
	e.mu.RLock()
	rt := e.svcbs[storeKey[:i]]
	e.mu.RUnlock()
	if rt == nil {
		return false, nil
	}
	field := storeKey[i+1:]
	invalid := &mms.DataAccessError{Code: mms.DataAccessErrorObjectValueInvalid}
	if field == "SvEna" {
		on, _ := val.Bool()
		return true, rt.setEnabled(on)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.pub != nil {
		return true, &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	}
	switch field {
	case "MsvID":
		if s, _ := val.VisibleString(); s == "" {
			return true, invalid
		}
	case "SmpRate":
		if r, _ := val.Uint64(); r == 0 {
			return true, invalid
		}
	case "SmpMod":
		if m, _ := val.Int64(); m < 0 || m > 2 {
			return true, invalid
		}
	case "OptFlds":
	case "DatSet":
		ds, _ := val.VisibleString()
		if def, _ := e.s.model.FindDataSet(rt.ldName, ds); def == nil {
			return true, invalid
		}
		if ds == rt.str("DatSet") {
			return true, nil
		}
		e.s.store.Set(storeKey, val)
		e.s.store.Set(rt.key("ConfRev"), mms.NewUnsigned(rt.uint("ConfRev")+1))
		if err := rt.resolveLocked(); err != nil {
			e.log.Warn("iec61850: SvCB DatSet unusable", "svcbRef", rt.prefix, "error", err)
		}
		return true, nil
	default:
		return false, nil
	}
	e.s.store.Set(storeKey, val)
	return true, nil
}
