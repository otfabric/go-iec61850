// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/goid"
	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// Server is an IEC 61850 MMS server built from an SCL data model.
//
// # Stability
//
// The server API is stable as of v1.0.0. It supports variable
// registration, control operations (direct normal, SBO normal, and SBOw
// enhanced security with connection-scoped ownership enforcement),
// runtime report delivery (data-change, quality-change, integrity-period,
// GI triggers, BRCB replay on re-enable), setting groups (active/edit
// selection, confirmation, handler callbacks), and journal services
// (runtime log entry generation with in-memory storage).
//
// # Known limitations
//
//   - SCL array/count attributes are parsed but not expanded during
//     server model registration. DA elements with count > 1 are
//     registered as scalar variables.
//   - Enum BTypes are mapped to generic integers (INT8) without
//     stronger enum-type semantics.
//   - Dataset and report cross-references are validated only within
//     the same logical node. Cross-LN dataset references are not
//     supported.
//   - Runtime report engine sends URCBs to the enabling connection
//     and broadcasts BRCBs to all connections. Multi-client URCB
//     arbitration and segmented report generation are not yet
//     implemented.
//   - BRCB buffering is in-memory with configurable depth. There is
//     no persistent buffer storage or replay-on-reconnect.
//   - Control handlers support SBO ownership enforcement and
//     enhanced-security ctlNum verification, but interlocking,
//     synchrocheck, and command termination are application
//     responsibility via callbacks.
//
// # Architecture
//
// The Server wraps a [mms.Server] and populates its variable registry
// from an IEC 61850 data model derived from an SCL file. Data
// attribute values are stored in a thread-safe [ValueStore] that backs
// the MMS read/write callbacks. Call [Server.Close] for orderly
// shutdown of runtime engines when the server is no longer needed.
type Server struct {
	logger *slog.Logger
	model  *servermodel.Model
	store  *servermodel.ValueStore
	mms    *mms.Server

	controlMu sync.RWMutex
	// controlLife orders control-model value writes (stSeld, the default
	// operate) before Close; controlClosed is set by Close.
	controlLife   sync.RWMutex
	controlClosed bool
	controls      map[string]*controlRegistration

	// enginesClosed is set by Close: JournalEngine and SettingGroupEngine
	// return nil from then on.
	enginesClosed atomic.Bool

	reportEngine       *ReportEngine
	reportMaxPDU       int // MMS MaxPDUSize bounding one InformationReport
	newReportBuffer    func(ldName, rcbItemID string) ReportBuffer
	sgEngine           *SettingGroupEngine
	journalEngine      *JournalEngine
	mmsJournalProvider *MemoryJournalProvider
	gooseEngine        *GOOSEEngine
	svEngine           *SVEngine

	hasFileProvider bool
	hasIdentity     bool
	onConnect       func(ConnectionEvent)
	onDisconnect    func(ConnectionEvent)

	// hookListeners are the listeners of the hook-routing accept loop,
	// closed by Shutdown (mms.Server only tracks its own listeners).
	// conns are the connection goroutines that loop started and that have
	// not returned; Shutdown waits for them. No goroutine is added once
	// shutdown is set. All three are guarded by lnMu.
	lnMu          sync.Mutex
	hookListeners map[mms.TransportListener]struct{}
	shutdown      bool
	conns         map[*servedConn]struct{}
	// downCh is closed when shutdown is set; it ends the accept backoff
	// of ListenAndServe. Guarded by lnMu, created on first use.
	downCh chan struct{}

	// maxConns bounds conns (ServerOptions.MaxConnections, default
	// filled in).
	maxConns int

	// Counter of Stats.
	connsRefused atomic.Uint64

	// cb calls application code under a recover and counts the calls
	// that did not return normally (ServerStats.CallbacksRecovered).
	cb *callbackGuard

	// bg tracks background sends (CommandTermination) stopped by Close.
	bg backgroundTasks

	// clock stamps protocol values (ServerOptions.Clock); nil is
	// SystemClock. Timers never use it.
	clock Clock
}

// servedConn is one connection goroutine of [Server.ListenAndServe].
type servedConn struct {
	// goroutine identifies it, so that a Shutdown called from a hook that
	// runs on it (OnConnect, OnDisconnect) does not wait for itself.
	goroutine atomic.Uint64
	done      chan struct{} // closed when the goroutine returns
}

// ServerOptions configures the IEC 61850 server.
//
// The server calls application code under a recover: OnConnect and
// OnDisconnect, the FileProvider, Authenticate, Metrics and a
// JournalProvider set in MMS, the [ControlHandler] and
// [SettingGroupHandler] callbacks, and the code a variable read or write
// reaches (the report buffers, the Clock). A call that does not return
// normally is logged with its stack at error level and counted in
// [ServerStats.CallbacksRecovered], and the request is answered with an
// error: object-access-denied for a variable read or write, a service
// error for a file or journal service, a rejection of the association
// for Authenticate and a refusal for a control service. A connection
// hook or a Metrics call loses that event. The connection, the other
// associations and the process go on. Code registered directly on
// [Server.MMS] is called by go-mms as it is.
type ServerOptions struct {
	// Logger, when non-nil, enables structured logging. When
	// [ServerOptions.MMS].Logger is also nil, this logger is passed
	// through to the underlying [mms.Server]. ISO transport logging
	// ([iso.WithLogger]) is independent and is not set from here.
	Logger *slog.Logger

	// Identity, when non-nil, registers the MMS Identify handler so
	// that clients can query vendor/model/revision. Without this,
	// Identify requests are rejected with service-unsupported.
	Identity *ServerIdentity

	// FileProvider, when non-nil, enables MMS file services
	// (FileOpen, FileRead, FileClose, FileDelete, FileDirectory).
	// This is a convenience field; it is equivalent to setting
	// MMS.FileProvider.
	FileProvider mms.FileProvider

	// Authenticate, when non-nil, is called during association
	// establishment to accept or reject peers. This is a convenience
	// field; it is equivalent to setting MMS.Authenticate.
	Authenticate mms.Authenticator

	// OnConnect, when non-nil, is called when a new transport
	// connection is accepted, before the MMS association handshake.
	// This fires even if the association subsequently fails.
	OnConnect func(ConnectionEvent)

	// OnDisconnect, when non-nil, is called when a transport
	// connection's [Server.Serve] call returns (the connection is
	// fully closed regardless of how it ended).
	OnDisconnect func(ConnectionEvent)

	// AllowReadOnlyFCWrites, when true, lets MMS clients write data
	// attributes of the read-only functional constraints: status (ST),
	// measurands (MX), setting-group values (SG), service responses
	// (SR), operate-received (OR) and extended definitions (EX). IEC
	// 61850-7-2 makes them read-only to clients, and by default a write
	// is answered with object-access-denied; the application changes
	// them with [Server.SetValue]. Set it only for a simulator or a test
	// fixture whose peers drive status values over MMS. It is not
	// conformant and lets any associated client change process data.
	AllowReadOnlyFCWrites bool

	// AllowEmptyReportDatSet serves report control blocks that have no data
	// set (SCL datSet absent or empty), which SCL and IEC 61850-7-2 allow:
	// a client writes DatSet before it enables the RCB. By default such a
	// model is refused, because the RCB cannot report as configured.
	// Enabling an RCB while its DatSet is still empty is refused either way.
	AllowEmptyReportDatSet bool

	// NewReportBuffer, when non-nil, returns the buffer of each buffered
	// report control block (ldName is the logical device, rcbItemID the
	// MMS item such as "LLN0$BR$brcb01"); [Server.EnableReports] calls it
	// once per BRCB. A nil result, or a nil NewReportBuffer, uses
	// [NewMemoryReportBuffer] with 1000 entries and no byte limit. URCBs
	// always use the in-memory queue.
	NewReportBuffer func(ldName, rcbItemID string) ReportBuffer

	// Metrics, when non-nil, receives the MMS request and connection
	// events of the server (see [mms.Metrics]). It is a convenience field
	// equivalent to MMS.Metrics, which takes precedence when both are set.
	Metrics mms.Metrics

	// MMS holds the underlying MMS server options. Fields set here
	// take precedence over convenience fields above (FileProvider,
	// Authenticate) when both are set.
	MMS mms.ServerOptions

	// Clock stamps the protocol times the server sets: the `t` the
	// default operate writes (also at the end of a ProcessResponse
	// travel), report TimeStamp and BRCB TimeOfEntry,
	// journal entry times, SGCB LActTm, and GOOSE T and SV RefrTm
	// (with their TimeQuality). Nil is [SystemClock]: the host time,
	// "clock not synchronised". Timers (sboTimeout, operTm, IntgPd,
	// BufTm, ResvTms) run on the monotonic clock and ignore it; see
	// [Clock].
	Clock Clock

	// MaxConnections bounds the connections [Server.ListenAndServe]
	// serves at once, over all its listeners; zero or less means
	// [DefaultMaxConnections]. A connection accepted beyond it is closed
	// at once, before OnConnect and the association, and counted in
	// [ServerStats.ConnectionsRefused]. Connections given to
	// [Server.Serve] by the caller are not counted.
	MaxConnections int
}

// DefaultMaxConnections is the default [ServerOptions.MaxConnections].
const DefaultMaxConnections = 1024

// ServerStats counts connections and callbacks of a [Server]; see
// [Server.Stats].
type ServerStats struct {
	// Connections is the number of connections [Server.ListenAndServe]
	// serves now.
	Connections int

	// ConnectionsRefused counts the connections closed at once because
	// [ServerOptions.MaxConnections] were served.
	ConnectionsRefused uint64

	// CallbacksRecovered counts the calls of application code (hooks,
	// providers, handlers, metrics) that did not return normally (see
	// [ServerOptions]).
	CallbacksRecovered uint64
}

// Stats returns the counters of the server. It is safe to call from any
// goroutine.
func (s *Server) Stats() ServerStats {
	s.lnMu.Lock()
	n := len(s.conns)
	s.lnMu.Unlock()
	return ServerStats{
		Connections:        n,
		ConnectionsRefused: s.connsRefused.Load(),
		CallbacksRecovered: s.cb.count(),
	}
}

// NewServer creates an IEC 61850 server from the given data model.
//
// The model is typically built via [NewServerModelFromSCL]. After
// construction, call [Server.Serve] or [Server.ListenAndServe] to
// accept client connections.
func NewServer(model *servermodel.Model, opts ServerOptions) (*Server, error) {
	if model == nil {
		return nil, fmt.Errorf("iec61850: nil server model")
	}

	if errs := model.ValidateWith(servermodel.ValidateOptions{AllowEmptyReportDatSet: opts.AllowEmptyReportDatSet}); len(errs) > 0 {
		return nil, fmt.Errorf("iec61850: model validation failed (%d errors): %w", len(errs), errors.Join(errs...))
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(discardHandler{})
	}

	mmsOpts := opts.MMS
	if mmsOpts.Logger == nil {
		mmsOpts.Logger = logger
	}

	if opts.FileProvider != nil && mmsOpts.FileProvider == nil {
		mmsOpts.FileProvider = opts.FileProvider
	}
	if opts.Metrics != nil && mmsOpts.Metrics == nil {
		mmsOpts.Metrics = opts.Metrics
	}
	if opts.Authenticate != nil && mmsOpts.Authenticate == nil {
		mmsOpts.Authenticate = opts.Authenticate
	}

	var mjp *MemoryJournalProvider
	if jp, ok := mmsOpts.JournalProvider.(*MemoryJournalProvider); ok {
		mjp = jp
	} else if mmsOpts.JournalProvider == nil && modelHasLogs(model) {
		mjp = NewMemoryJournalProvider()
		mmsOpts.JournalProvider = mjp
	}

	// Application code that go-mms calls on its goroutines runs under a
	// recover (see ServerOptions).
	cb := &callbackGuard{logger: logger}
	if mmsOpts.FileProvider != nil {
		mmsOpts.FileProvider = &guardedFileProvider{p: mmsOpts.FileProvider, g: cb}
	}
	if mmsOpts.Authenticate != nil {
		mmsOpts.Authenticate = cb.authenticator(mmsOpts.Authenticate)
	}
	mmsOpts.Metrics = guardMetrics(mmsOpts.Metrics, cb)
	if mmsOpts.JournalProvider != nil && mjp == nil {
		mmsOpts.JournalProvider = guardJournal(mmsOpts.JournalProvider, cb)
	}

	mmsSrv := mms.NewServer(mmsOpts)

	vs, err := servermodel.RegisterModel(guardedRegistrar{srv: mmsSrv, g: cb}, model, servermodel.NewValueStoreWithOptions(servermodel.ValueStoreOptions{
		AllowReadOnlyFCWrites: opts.AllowReadOnlyFCWrites,
	}))
	if err != nil {
		return nil, fmt.Errorf("iec61850: register model: %w", err)
	}

	s := &Server{
		logger:             logger,
		model:              model,
		store:              vs,
		mms:                mmsSrv,
		controls:           make(map[string]*controlRegistration),
		mmsJournalProvider: mjp,
		hasFileProvider:    mmsOpts.FileProvider != nil,
		onConnect:          opts.OnConnect,
		onDisconnect:       opts.OnDisconnect,
		reportMaxPDU:       mmsOpts.MMS.MaxPDUSize,
		clock:              clockOrSystem(opts.Clock),
		newReportBuffer:    opts.NewReportBuffer,
		maxConns:           opts.MaxConnections,
		cb:                 cb,
	}
	if s.maxConns <= 0 {
		s.maxConns = DefaultMaxConnections
	}

	if opts.Identity != nil {
		s.HandleIdentify(*opts.Identity)
	}
	s.HandleStatus()
	s.installWriteInterceptor()
	vs.SetWriteGuard(s.guardControlWrite)

	logger.Info("iec61850: server created",
		"logical_devices", len(model.LogicalDevices))

	return s, nil
}

// Model returns the server's data model. The returned pointer is
// shared with the server internals. The caller must not modify the
// model after server creation — doing so may corrupt the MMS
// variable registry and cause undefined runtime behaviour.
func (s *Server) Model() *servermodel.Model {
	return s.model
}

// ValueStore returns the server's value store for direct value
// manipulation (e.g., updating data attribute values from process
// inputs).
//
// When reports are enabled, prefer [Server.SetValue] which
// automatically triggers change-detection and report generation.
func (s *Server) ValueStore() *servermodel.ValueStore {
	return s.store
}

// SetValue updates a data attribute value in the store and notifies
// the report and GOOSE engines (if enabled) of the change. The storeKey follows
// the format "ldName/itemID" (see [servermodel.StoreKey]).
//
// This is the primary way to inject process values into a running
// server and have them trigger reports automatically.
func (s *Server) SetValue(ctx context.Context, storeKey string, val *mms.Value) {
	s.store.Set(storeKey, val)
	if s.reportEngine != nil {
		s.reportEngine.NotifyValueChanged(ctx, storeKey)
	}
	if s.journalEngine != nil {
		s.journalEngine.LogValueWrite(ctx, storeKey, clockOrSystem(s.clock).Now())
	}
	if s.gooseEngine != nil {
		s.gooseEngine.notify(storeKey)
	}
	if s.svEngine != nil {
		s.svEngine.notify(storeKey)
	}
}

// ReportEngine returns the report engine, or nil if reports have not
// been enabled via [Server.EnableReports].
func (s *Server) ReportEngine() *ReportEngine {
	return s.reportEngine
}

// installWriteInterceptor sets up the ValueStore write interceptor used for
// two distinct purposes (see [servermodel.ValueStore.SetWriteInterceptor]):
//
//   - RCB / SGCB subfield writes: the interceptor is called as a dispatcher
//     that can reject or reroute the write before (SGCB) or after (RCB) the
//     value is committed.
//
//   - Regular DA writes: the interceptor is called as a post-write hook after
//     the DA Write handler has already stored the new value.  Returning
//     (true, nil) signals that report notification was dispatched; it does NOT
//     roll back the store update.
//
// No recursion hazard: the interceptor is only triggered by MMS client Write
// callbacks registered during model registration, not by [ValueStore.Set].
// Runtime engines that call store.Set internally (e.g. SqNum updates, SGCB
// sync) bypass the interceptor and write directly to the store.
func (s *Server) installWriteInterceptor() {
	s.store.SetWriteInterceptor(func(ctx context.Context, storeKey string, val *mms.Value) (bool, error) {
		parts := strings.SplitN(storeKey, "/", 2)
		if len(parts) != 2 {
			return false, nil
		}
		ldName := parts[0]
		itemID := parts[1]

		if s.reportEngine != nil {
			if rcbItemID, subfield, ok := parseRCBStoreKey(itemID); ok {
				sc := mms.ServerConnFromContext(ctx)
				err := s.reportEngine.HandleRCBWrite(ctx, ldName, rcbItemID, subfield, val, sc)
				return true, err
			}
		}

		if s.journalEngine != nil {
			if lcbItem, attr, ok := servermodel.ParseLCBItem(itemID); ok {
				return true, s.journalEngine.HandleLCBWrite(ctx, ldName, lcbItem, attr, val)
			}
		}

		if s.sgEngine != nil {
			if subfield, ok := parseSGCBStoreKey(itemID); ok {
				err := s.sgEngine.HandleSGCBWrite(ctx, ldName, subfield, val)
				return true, err
			}
		}

		if s.gooseEngine != nil && strings.Contains(itemID, "$GO$") {
			if handled, err := s.gooseEngine.handleWrite(storeKey, val); handled {
				return true, err
			}
		}

		if s.svEngine != nil && strings.Contains(itemID, "$MS$") {
			if handled, err := s.svEngine.handleWrite(storeKey, val); handled {
				return true, err
			}
		}

		// CO functional-constraint write — dispatch to the control handler.
		// Store key format: "lnName$CO$doPath...$subAttr"
		// e.g. "GGIO1$CO$SPCSO1$Oper" or "GGIO1$CO$SPCSO1$Oper$ctlVal"
		if lnName, path, subAttr, ok := parseCOStoreKey(itemID); ok {
			err := s.handleControlWrite(ctx, ldName, lnName, path, subAttr, val)
			return true, err
		}

		// Regular data-attribute write by a connected MMS client.
		// The DA Write handler has already stored the value via vs.Set
		// before calling the interceptor, so only the notification is
		// needed here. The engine compares prevValues with the new store
		// contents to suppress reports for unchanged values.
		if s.gooseEngine != nil {
			s.gooseEngine.notify(storeKey)
		}
		if s.svEngine != nil {
			s.svEngine.notify(storeKey)
		}
		// A client write is logged like a local one (IEC 61850-7-2
		// §17.3.4: the log is triggered as reporting is).
		if s.journalEngine != nil {
			s.journalEngine.LogValueWrite(ctx, storeKey, clockOrSystem(s.clock).Now())
		}
		if s.reportEngine != nil {
			s.reportEngine.NotifyValueChanged(ctx, storeKey)
			return true, nil
		}

		return false, nil
	})
}

// parseCOStoreKey checks if an MMS item ID is a CO functional-constraint write.
// CO item IDs follow the pattern "LNName$CO$path...$subAttr"
// e.g. "GGIO1$CO$SPCSO1$Oper" or "GGIO1$CO$SPCSO1$Cancel".
//
// Returns (lnName, fullPath, subAttr, true) where fullPath includes the DO
// name and all sub-path segments, and subAttr is the last segment (the control
// service identifier: Oper, SBO, SBOw, or Cancel).
func parseCOStoreKey(itemID string) (lnName string, path []string, subAttr string, ok bool) {
	before, after0, ok := strings.Cut(itemID, "$CO$")
	if !ok {
		return "", nil, "", false
	}
	lnName = before
	after := after0 // everything after "$CO$"
	if after == "" {
		return "", nil, "", false
	}
	segs := strings.Split(after, "$")
	if len(segs) < 2 {
		return "", nil, "", false
	}
	// Require the last segment to be a recognised control service identifier.
	last := segs[len(segs)-1]
	switch last {
	case "Oper", "SBO", "SBOw", "Cancel":
		// OK
	default:
		// Sub-BDA write (e.g. Oper$ctlVal) — dispatch via parent Oper level.
		return "", nil, "", false
	}
	return lnName, segs, last, true
}

// parseRCBStoreKey checks if an MMS item ID is an RCB subfield write.
//
// This is a heuristic string parser that relies on the $RP$ / $BR$
// convention used by [servermodel.RegisterModel]. It works for all
// standard IEC 61850 RCB naming but would need updating if a
// non-standard naming scheme were used. A structural approach using
// a set of registered RCB names would be more robust but is not
// necessary while the naming convention is controlled internally.
func parseRCBStoreKey(itemID string) (rcbItemID, subfield string, ok bool) {
	i := strings.Index(itemID, "$RP$")
	if i < 0 {
		i = strings.Index(itemID, "$BR$")
	}
	if i < 0 {
		return "", "", false
	}

	afterPrefix := itemID[i+4:]
	j := strings.Index(afterPrefix, "$")
	if j < 0 {
		return "", "", false
	}

	rcbItemID = itemID[:i+4+j]
	subfield = afterPrefix[j+1:]
	return rcbItemID, subfield, true
}

// parseSGCBStoreKey checks if an MMS item ID is an SGCB subfield
// write. SGCB item IDs follow the pattern "LNName$SP$SGCB$subfield".
func parseSGCBStoreKey(itemID string) (subfield string, ok bool) {
	const sgcbMarker = "$SP$SGCB$"
	_, after, ok := strings.Cut(itemID, sgcbMarker)
	if !ok {
		return "", false
	}
	return after, true
}

// Close performs an orderly shutdown of the server's runtime engines.
// It stops pending CommandTermination sends, the report engine
// (integrity timers, delivery goroutines), the GOOSE engine (publishers)
// and the journal and setting group engines; [Server.JournalEngine] and
// [Server.SettingGroupEngine] return nil afterwards. Close is idempotent,
// safe to call multiple times and safe concurrently with requests of open
// associations and with [Server.SetValue].
//
// Note: Close does not close the underlying MMS server or any active
// connections. Use context cancellation to stop [Server.Serve] or
// [Server.ListenAndServe].
func (s *Server) Close() {
	s.bg.stop()
	s.stopControls()
	if s.reportEngine != nil {
		s.reportEngine.Stop()
	}
	if s.gooseEngine != nil {
		s.gooseEngine.Stop()
	}
	if s.svEngine != nil {
		s.svEngine.Stop()
	}
	if s.journalEngine != nil {
		s.journalEngine.stop()
	}
	if s.sgEngine != nil {
		s.sgEngine.stop()
	}
	// The accessors report the journal and setting group engines as gone.
	// The fields stay: requests of open associations still read them, and
	// clearing them raced with those reads.
	s.enginesClosed.Store(true)
	s.logger.Info("iec61850: server closed")
}

// Shutdown stops the server gracefully: it closes the listeners of
// [Server.ListenAndServe], lets in-flight requests finish, concludes every
// association (see [mms.Server.Shutdown]), waits for the connection
// goroutines of ListenAndServe and then stops the report and GOOSE engines
// like [Server.Close]. When ctx ends first, the remaining connections are
// aborted and ctx's error is returned.
// ListenAndServe then returns [mms.ErrServerClosed].
//
// When Shutdown returns nil, every connection goroutine that
// ListenAndServe started has returned: its [Server.Serve] has released
// what the association held and its OnDisconnect hook has run. When ctx
// ends while such a goroutine still runs (an OnDisconnect that has not
// returned), Shutdown stops waiting, stops the engines and returns ctx's
// error; a later Shutdown waits again. Connections the caller serves with
// [Server.Serve] on goroutines of its own are the caller's to wait for.
//
// Shutdown may be called from OnConnect or OnDisconnect: it does not wait
// for the connection goroutine it is called on.
func (s *Server) Shutdown(ctx context.Context) error {
	s.lnMu.Lock()
	if !s.shutdown {
		s.shutdown = true
		if s.downCh != nil {
			close(s.downCh)
		}
	}
	for ln := range s.hookListeners {
		_ = ln.Close()
	}
	s.lnMu.Unlock()
	err := s.mms.Shutdown(ctx)
	if werr := s.waitServedConns(ctx); err == nil {
		err = werr
	}
	s.Close()
	return err
}

// waitServedConns waits until the connection goroutines of ListenAndServe
// have returned, except the one it is called on, or until ctx ends, when
// it returns ctx's error.
func (s *Server) waitServedConns(ctx context.Context) error {
	self := goid.ID()
	s.lnMu.Lock()
	pending := make([]*servedConn, 0, len(s.conns))
	for sc := range s.conns {
		if sc.goroutine.Load() != self {
			pending = append(pending, sc)
		}
	}
	s.lnMu.Unlock()
	for _, sc := range pending {
		select {
		case <-sc.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// MMS returns the underlying MMS server for advanced configuration
// (e.g., registering Identify/Status handlers or a FileProvider).
func (s *Server) MMS() *mms.Server {
	return s.mms
}

// Serve handles a single MMS transport connection. This blocks until
// the connection is closed or the context is cancelled.
//
// If [ServerOptions.OnConnect] or [ServerOptions.OnDisconnect] are
// configured, OnConnect is called when the transport is accepted
// (before the MMS association handshake completes), and OnDisconnect
// is called when the Serve call returns (after the connection is
// fully closed). This means OnConnect fires even if the association
// subsequently fails during authentication or handshake.
func (s *Server) Serve(ctx context.Context, conn mms.Transport) error {
	if s.onConnect != nil {
		s.cb.hook("OnConnect", s.onConnect, ConnectionEvent{})
	}
	// The gate orders reports after the response to the request that
	// triggered them (see respGate).
	gate := newRespGate()
	ctx = context.WithValue(ctx, respGateKey{}, gate)
	err := s.mms.Serve(ctx, &gatedTransport{Transport: conn, gate: gate})
	// IEC 61850-7-2 Table 47: the association is gone, so are its
	// selections (other paths release them on the next control service).
	s.releaseLostSelections()
	// The association has ended: release the report control blocks it
	// owned (IEC 61850-7-2 §17.2.2.5 Figure 24, §17.2.4.4).
	if s.reportEngine != nil {
		s.reportEngine.reapClosedConns()
	}
	if s.onDisconnect != nil {
		s.cb.hook("OnDisconnect", s.onDisconnect, ConnectionEvent{})
	}
	return err
}

// ListenAndServe accepts connections on the given listener and serves
// each in a new goroutine. This blocks until the context is cancelled
// or the listener is closed.
//
// Each accepted connection is handled by [Server.Serve] in its own
// goroutine, which ensures that connection lifecycle hooks are invoked.
// At most [ServerOptions.MaxConnections] are served at once; a connection
// beyond that is closed at once. A temporary accept error is retried
// after a pause that starts at 5 ms and doubles up to 1 s.
//
// ListenAndServe returns after the connection goroutines it started have
// returned (their OnDisconnect included): after ctx ends, which ends
// their associations; after [Server.Shutdown], which concludes them; and
// after an accept error that is not temporary, when it first ends the
// connections it serves.
func (s *Server) ListenAndServe(ctx context.Context, ln mms.TransportListener) error {
	return s.listenAndServeWithHooks(ctx, ln)
}

// Accept backoff of ListenAndServe after a temporary accept error, as
// net/http and go-mms transport/iso do.
const (
	acceptBackoffMin = 5 * time.Millisecond
	acceptBackoffMax = time.Second
)

// listenAndServeWithHooks implements the accept loop: every connection
// goes through [Server.Serve], so connection hooks are invoked and
// reports are ordered after responses.
func (s *Server) listenAndServeWithHooks(ctx context.Context, ln mms.TransportListener) error {
	s.lnMu.Lock()
	if s.shutdown {
		s.lnMu.Unlock()
		_ = ln.Close()
		return mms.ErrServerClosed
	}
	if s.hookListeners == nil {
		s.hookListeners = make(map[mms.TransportListener]struct{})
	}
	s.hookListeners[ln] = struct{}{}
	if s.downCh == nil {
		s.downCh = make(chan struct{})
	}
	down := s.downCh
	s.lnMu.Unlock()

	// The connections of this call: served with connCtx, cancelled when
	// the listener fails, and waited for before returning.
	connCtx, cancelConns := context.WithCancel(ctx)
	var served sync.WaitGroup
	defer func() {
		s.lnMu.Lock()
		delete(s.hookListeners, ln)
		s.lnMu.Unlock()
		_ = ln.Close()
		served.Wait()
		cancelConns()
	}()

	var backoff time.Duration
	for {
		transport, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.lnMu.Lock()
			isDown := s.shutdown
			s.lnMu.Unlock()
			if isDown {
				return mms.ErrServerClosed
			}
			if isTemporaryAcceptError(err) {
				backoff = min(max(2*backoff, acceptBackoffMin), acceptBackoffMax)
				s.logger.Warn("iec61850: temporary accept error, retrying", "error", err, "backoff", backoff)
				t := time.NewTimer(backoff)
				select {
				case <-t.C:
				case <-ctx.Done():
				case <-down:
				}
				t.Stop()
				continue
			}
			s.logger.Error("iec61850: accept error", "error", err)
			// The listener has failed: end the connections it served.
			cancelConns()
			return err
		}
		backoff = 0
		// The goroutine is registered before it starts, and none starts
		// once Shutdown has begun: Shutdown waits for all of them.
		sc := &servedConn{done: make(chan struct{})}
		s.lnMu.Lock()
		if s.shutdown {
			s.lnMu.Unlock()
			_ = transport.Close()
			return mms.ErrServerClosed
		}
		if len(s.conns) >= s.maxConns {
			s.lnMu.Unlock()
			_ = transport.Close()
			if s.connsRefused.Add(1) == 1 {
				s.logger.Warn("iec61850: connection refused: MaxConnections served", "max", s.maxConns)
			} else {
				s.logger.Debug("iec61850: connection refused: MaxConnections served", "max", s.maxConns)
			}
			continue
		}
		if s.conns == nil {
			s.conns = make(map[*servedConn]struct{})
		}
		s.conns[sc] = struct{}{}
		s.lnMu.Unlock()
		served.Add(1)
		go func() {
			defer served.Done()
			sc.goroutine.Store(goid.ID())
			defer func() {
				s.lnMu.Lock()
				delete(s.conns, sc)
				s.lnMu.Unlock()
				close(sc.done)
			}()
			defer func() { _ = transport.Close() }()
			if serveErr := s.Serve(connCtx, transport); serveErr != nil {
				s.logger.Debug("iec61850: connection closed", "error", serveErr)
			}
		}()
	}
}

// NewServerModelFromSCL builds a server data model from an SCL
// configuration. The iedName selects the IED; apName selects the
// AccessPoint (empty uses the first AccessPoint with a Server).
//
// Each logical device is served as the MMS domain IEC 61850-8-1 names it,
// as the other IEC 61850 stacks do: LDevice@ldName when the SCL has one,
// otherwise the IED name followed by LDevice@inst ("InteropIEDInteropLD").
// Data set members, report, GOOSE and SV control block references, and
// the keys of [Server.SetValue] use that name. Before v1.7.0 the domain
// was LDevice@inst alone; [NewServerModelFromSCLWithOptions] with
// [ServerModelOptions.InstanceDomainNames] keeps that.
//
// The expansion of the type templates stops at
// [scl.DefaultMaxExpandedNodes] data objects and attributes with an error
// that wraps [ErrLimitExceeded] and [scl.ErrLimitExceeded];
// [ServerModelOptions.MaxExpandedNodes] sets another limit.
//
// This is a convenience wrapper around [servermodel.FromSCL].
func NewServerModelFromSCL(s *scl.SCL, iedName, apName string) (*servermodel.Model, error) {
	return NewServerModelFromSCLWithOptions(s, iedName, apName, ServerModelOptions{})
}

// ServerModelOptions control [NewServerModelFromSCLWithOptions].
type ServerModelOptions struct {
	// InstanceDomainNames names each logical device (MMS domain) by its
	// LDevice@inst alone ("InteropLD"), as go-iec61850 did before v1.7.0,
	// instead of its IEC 61850-8-1 name.
	InstanceDomainNames bool

	// MaxExpandedNodes is the number of data objects and data
	// attributes (DO, SDO, DA and BDA, each counted once per place in
	// the model it is expanded to) the expansion of the type templates
	// may produce; zero or less means [scl.DefaultMaxExpandedNodes].
	// Beyond it the model is not built and the error wraps
	// [ErrLimitExceeded] and [scl.ErrLimitExceeded].
	MaxExpandedNodes int
}

// NewServerModelFromSCLWithOptions is [NewServerModelFromSCL] with
// options.
func NewServerModelFromSCLWithOptions(s *scl.SCL, iedName, apName string, o ServerModelOptions) (*servermodel.Model, error) {
	m, err := servermodel.FromSCLWithOptions(s, iedName, apName, servermodel.Options{
		InstanceDomainNames: o.InstanceDomainNames,
		MaxExpandedNodes:    o.MaxExpandedNodes,
	})
	if err != nil && errors.Is(err, scl.ErrLimitExceeded) {
		return nil, modelLimitError{err}
	}
	return m, err
}

// modelLimitError is the error of a model whose expansion went beyond
// its limit: it reads as the servermodel error and matches both
// [ErrLimitExceeded] and [scl.ErrLimitExceeded].
type modelLimitError struct{ err error }

func (e modelLimitError) Error() string   { return e.err.Error() }
func (e modelLimitError) Unwrap() []error { return []error{ErrLimitExceeded, e.err} }

// isTemporaryAcceptError reports whether an Accept error is temporary, as
// mms.Server.ListenAndServe does.
func isTemporaryAcceptError(err error) bool {
	type temporary interface {
		error
		Temporary() bool
	}
	t, ok := errors.AsType[temporary](err)
	return ok && t.Temporary()
}
