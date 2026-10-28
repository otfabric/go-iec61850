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
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// ErrGOOSE indicates a server-side GOOSE configuration or runtime
// failure. Errors from [Server.EnableGOOSE] wrap it.
var ErrGOOSE = errors.New("iec61850: GOOSE error")

// GOOSEOptions configures [Server.EnableGOOSE].
type GOOSEOptions struct {
	// Transport sends the GOOSE frames (required). Use [goose.Open] for
	// a network interface or a [goose.Hub] port for tests.
	Transport goose.Transport

	// StartDisabled leaves GoEna false for every GoCB; clients (or
	// [GOOSEEngine.SetEnabled]) enable publishing. By default every GoCB
	// with a valid address and dataset publishes immediately.
	StartDisabled bool

	// Logger receives GOOSE diagnostics. Nil uses the server logger.
	Logger *slog.Logger

	// Metrics receives publisher counters. Nil disables metrics.
	Metrics goose.Metrics
}

// GOOSEEngine publishes the GOOSE control blocks of a [Server]'s model.
//
// Every GoCB (SCL GSEControl in LLN0 with a GSE address) gets a
// [goose.Publisher]. When a dataset member changes through
// [Server.SetValue] or an MMS client write, the engine publishes a new
// state if the member values differ from the last published ones.
//
// The GoCB attributes are served over MMS under the GO functional
// constraint: GoEna, GoID and DatSet are writable (GoID and DatSet only
// while GoEna is false; a DatSet change increments ConfRev), all other
// attributes are read-only.
type GOOSEEngine struct {
	s       *Server
	t       goose.Transport
	logger  *slog.Logger
	metrics goose.Metrics

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu    sync.RWMutex
	gocbs map[string]*gocbRuntime // key: store-key prefix "LD/LLN0$GO$name"
}

type gocbRuntime struct {
	e      *GOOSEEngine
	ldName string
	lnName string
	def    servermodel.GoCBDef
	prefix string // store key prefix "LD/LLN0$GO$name"

	mu      sync.Mutex
	members []*servermodel.MemberNode
	keys    map[string]bool
	pub     *goose.Publisher
	stop    context.CancelFunc
	last    []*mms.Value
}

// EnableGOOSE creates the GOOSE engine and starts publishing. Call it
// after [NewServer]; [Server.Close] stops it.
func (s *Server) EnableGOOSE(opts GOOSEOptions) (*GOOSEEngine, error) {
	if opts.Transport == nil {
		return nil, fmt.Errorf("%w: nil transport", ErrGOOSE)
	}
	if s.gooseEngine != nil {
		return nil, fmt.Errorf("%w: already enabled", ErrGOOSE)
	}
	logger := opts.Logger
	if logger == nil {
		logger = s.logger
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &GOOSEEngine{
		s: s, t: opts.Transport, logger: logger, metrics: opts.Metrics,
		ctx: ctx, cancel: cancel, gocbs: map[string]*gocbRuntime{},
	}
	for i := range s.model.LogicalDevices {
		ld := &s.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.GoCBs {
				rt := &gocbRuntime{e: e, ldName: ld.Name, lnName: ln.Name, def: ln.GoCBs[k]}
				rt.prefix = servermodel.StoreKey(ld.Name, servermodel.GoCBItemID(ln.Name, rt.def.Name))
				e.gocbs[rt.prefix] = rt
			}
		}
	}
	s.gooseEngine = e
	for _, rt := range e.sorted() {
		rt.mu.Lock()
		err := rt.resolveLocked()
		if err != nil {
			logger.Warn("iec61850: GoCB needs commissioning", "gocbRef", rt.ref(), "error", err)
		}
		rt.mu.Unlock()
		if err == nil && !opts.StartDisabled {
			if err := rt.setEnabled(true); err != nil {
				e.Stop()
				return nil, fmt.Errorf("%w: %s: %w", ErrGOOSE, rt.ref(), err)
			}
		}
	}
	logger.Info("iec61850: GOOSE engine enabled", "gocbs", len(e.gocbs))
	return e, nil
}

// GOOSEEngine returns the GOOSE engine, or nil if [Server.EnableGOOSE]
// has not been called.
func (s *Server) GOOSEEngine() *GOOSEEngine { return s.gooseEngine }

func (e *GOOSEEngine) sorted() []*gocbRuntime {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*gocbRuntime, 0, len(e.gocbs))
	for _, rt := range e.gocbs {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].prefix < out[j].prefix })
	return out
}

// ControlBlocks returns the references ("LD/LLN0$GO$name") of all GoCBs.
func (e *GOOSEEngine) ControlBlocks() []string {
	var out []string
	for _, rt := range e.sorted() {
		out = append(out, rt.ref())
	}
	return out
}

func (e *GOOSEEngine) lookup(ref string) (*gocbRuntime, error) {
	e.mu.RLock()
	rt := e.gocbs[ref]
	e.mu.RUnlock()
	if rt == nil {
		return nil, fmt.Errorf("%w: GoCB %q: %w", ErrGOOSE, ref, ErrNotFound)
	}
	return rt, nil
}

// Publisher returns the publisher of a GoCB ("LD/LLN0$GO$name"), or nil
// while the GoCB has never been enabled.
func (e *GOOSEEngine) Publisher(ref string) (*goose.Publisher, error) {
	rt, err := e.lookup(ref)
	if err != nil {
		return nil, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.pub, nil
}

// SetEnabled sets GoEna of a GoCB, exactly like an MMS client write.
func (e *GOOSEEngine) SetEnabled(ref string, enabled bool) error {
	rt, err := e.lookup(ref)
	if err != nil {
		return err
	}
	return rt.setEnabled(enabled)
}

// Stop stops all publishers. It is idempotent; [Server.Close] calls it.
func (e *GOOSEEngine) Stop() {
	e.cancel()
	e.wg.Wait()
}

func (rt *gocbRuntime) ref() string { return rt.prefix }

func (rt *gocbRuntime) key(field string) string { return rt.prefix + "$" + field }

func (rt *gocbRuntime) get(field string) *mms.Value { return rt.e.s.store.Get(rt.key(field)) }

func (rt *gocbRuntime) str(field string) string {
	v, _ := rt.get(field).VisibleString()
	return v
}

func (rt *gocbRuntime) boolean(field string) bool {
	v, _ := rt.get(field).Bool()
	return v
}

// resolveLocked resolves the current DatSet to member layouts and sets
// NdsCom accordingly.
func (rt *gocbRuntime) resolveLocked() error {
	err := func() error {
		if !rt.def.HasAddress {
			return errors.New("no GSE address")
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
		keys := map[string]bool{}
		for i, m := range def.Members {
			n, err := rt.e.s.model.ResolveMember(rt.ldName, m)
			if err != nil {
				return err
			}
			members[i] = n
			for _, k := range n.Keys() {
				keys[k] = true
			}
		}
		rt.members, rt.keys = members, keys
		return nil
	}()
	if err != nil {
		rt.members, rt.keys = nil, nil
	}
	rt.e.s.store.Set(rt.key("NdsCom"), mms.NewBoolean(err != nil))
	return err
}

func (rt *gocbRuntime) readValues() []*mms.Value {
	vals := make([]*mms.Value, len(rt.members))
	for i, n := range rt.members {
		vals[i] = rt.e.s.store.ReadMember(n)
	}
	return vals
}

// setEnabled implements GoEna.
func (rt *gocbRuntime) setEnabled(on bool) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if on == rt.boolean("GoEna") && (rt.pub != nil) == on {
		return nil
	}
	if !on {
		if rt.stop != nil {
			rt.stop()
			rt.stop = nil
		}
		if rt.pub != nil {
			_ = rt.pub.Close()
			rt.pub = nil
		}
		rt.e.s.store.Set(rt.key("GoEna"), mms.NewBoolean(false))
		rt.e.logger.Info("iec61850: GoCB disabled", "gocbRef", rt.ref())
		return nil
	}
	if rt.boolean("NdsCom") || rt.members == nil {
		return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	}
	if err := rt.e.ctx.Err(); err != nil {
		return err
	}
	cfg := goose.PublisherConfig{
		Dst:     goose.MAC(rt.def.DstAddr),
		AppID:   rt.def.AppID,
		GocbRef: rt.ref(),
		DatSet:  rt.str("DatSet"),
		GoID:    rt.str("GoID"),
		MinTime: time.Duration(rt.def.MinTime) * time.Millisecond,
		MaxTime: time.Duration(rt.def.MaxTime) * time.Millisecond,
		Values:  rt.readValues(),
		Logger:  rt.e.logger,
		Metrics: rt.e.metrics,
	}
	if cfg.MinTime > cfg.MaxTime && cfg.MaxTime > 0 {
		cfg.MinTime = cfg.MaxTime
	}
	if cr, ok := rt.get("ConfRev").Uint64(); ok {
		cfg.ConfRev = uint32(cr)
	}
	if rt.def.HasVLAN {
		cfg.VLAN = &goose.VLAN{Priority: rt.def.VLANPriority, ID: rt.def.VLANID}
	}
	pub, err := goose.NewPublisher(rt.e.t, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(rt.e.ctx)
	rt.pub, rt.stop, rt.last = pub, cancel, cfg.Values
	rt.e.s.store.Set(rt.key("GoEna"), mms.NewBoolean(true))
	rt.e.wg.Add(1)
	go func() {
		defer rt.e.wg.Done()
		_ = pub.Run(ctx)
		_ = pub.Close()
	}()
	rt.e.logger.Info("iec61850: GoCB enabled", "gocbRef", rt.ref(), "members", len(rt.members))
	return nil
}

// notify publishes a new state for every GoCB whose dataset contains
// storeKey and whose member values changed.
func (e *GOOSEEngine) notify(storeKey string) {
	if e.ctx.Err() != nil {
		return
	}
	e.mu.RLock()
	rts := make([]*gocbRuntime, 0, len(e.gocbs))
	for _, rt := range e.gocbs {
		rts = append(rts, rt)
	}
	e.mu.RUnlock()
	for _, rt := range rts {
		rt.mu.Lock()
		if rt.pub == nil || !rt.keys[storeKey] {
			rt.mu.Unlock()
			continue
		}
		vals := rt.readValues()
		changed := false
		for i := range vals {
			if !vals[i].Equal(rt.last[i]) {
				changed = true
				break
			}
		}
		if changed {
			if err := rt.pub.Publish(vals); err != nil {
				e.logger.Warn("iec61850: GOOSE publish failed", "gocbRef", rt.ref(), "error", err)
			} else {
				rt.last = vals
			}
		}
		rt.mu.Unlock()
	}
}

// handleWrite processes an MMS client write to a GoCB attribute.
// handled=false means the attribute is not a GoCB attribute.
func (e *GOOSEEngine) handleWrite(storeKey string, val *mms.Value) (bool, error) {
	i := strings.LastIndex(storeKey, "$")
	if i < 0 {
		return false, nil
	}
	e.mu.RLock()
	rt := e.gocbs[storeKey[:i]]
	e.mu.RUnlock()
	if rt == nil {
		return false, nil
	}
	field := storeKey[i+1:]
	switch field {
	case "GoEna":
		on, _ := val.Bool()
		return true, rt.setEnabled(on)
	case "GoID", "DatSet":
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.boolean("GoEna") {
			return true, &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
		}
		if field == "GoID" {
			e.s.store.Set(storeKey, val)
			return true, nil
		}
		ds, _ := val.VisibleString()
		if def, _ := e.s.model.FindDataSet(rt.ldName, ds); def == nil {
			return true, &mms.DataAccessError{Code: mms.DataAccessErrorObjectValueInvalid}
		}
		if ds == rt.str("DatSet") {
			return true, nil
		}
		e.s.store.Set(storeKey, val)
		cr, _ := rt.get("ConfRev").Uint64()
		e.s.store.Set(rt.key("ConfRev"), mms.NewUnsigned(cr+1))
		if err := rt.resolveLocked(); err != nil {
			e.logger.Warn("iec61850: GoCB DatSet unusable", "gocbRef", rt.ref(), "error", err)
		}
		return true, nil
	}
	return false, nil
}
