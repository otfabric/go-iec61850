// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"

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
	controls  map[string]*controlRegistration

	reportEngine       *ReportEngine
	sgEngine           *SettingGroupEngine
	journalEngine      *JournalEngine
	mmsJournalProvider *MemoryJournalProvider

	hasFileProvider bool
	hasIdentity     bool
	onConnect       func(ConnectionEvent)
	onDisconnect    func(ConnectionEvent)
}

// ServerOptions configures the IEC 61850 server.
type ServerOptions struct {
	// Logger, when non-nil, enables structured logging.
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

	// MMS holds the underlying MMS server options. Fields set here
	// take precedence over convenience fields above (FileProvider,
	// Authenticate) when both are set.
	MMS mms.ServerOptions
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

	if errs := model.Validate(); len(errs) > 0 {
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

	mmsSrv := mms.NewServer(mmsOpts)

	vs, err := servermodel.RegisterModel(mmsSrv, model, nil)
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
	}

	if opts.Identity != nil {
		s.HandleIdentify(*opts.Identity)
	}
	s.HandleStatus()
	s.installWriteInterceptor()

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
// the report engine (if enabled) of the change. The storeKey follows
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
		s.journalEngine.LogValueWrite(ctx, storeKey, time.Now())
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

		if s.sgEngine != nil {
			if subfield, ok := parseSGCBStoreKey(itemID); ok {
				err := s.sgEngine.HandleSGCBWrite(ctx, ldName, subfield, val)
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
	i := strings.Index(itemID, "$CO$")
	if i < 0 {
		return "", nil, "", false
	}
	lnName = itemID[:i]
	after := itemID[i+4:] // everything after "$CO$"
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
	i := strings.Index(itemID, sgcbMarker)
	if i < 0 {
		return "", false
	}
	return itemID[i+len(sgcbMarker):], true
}

// Close performs an orderly shutdown of the server's runtime engines.
// It stops the report engine (integrity timers, delivery goroutines)
// and clears references to the journal and setting group engines.
// Close is idempotent and safe to call multiple times.
//
// Note: Close does not close the underlying MMS server or any active
// connections. Use context cancellation to stop [Server.Serve] or
// [Server.ListenAndServe]. The journal and setting group engines are
// stateless (no background goroutines), so they only need reference
// clearing for garbage collection.
func (s *Server) Close() {
	if s.reportEngine != nil {
		s.reportEngine.Stop()
	}
	s.journalEngine = nil
	s.sgEngine = nil
	s.logger.Info("iec61850: server closed")
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
		s.onConnect(ConnectionEvent{})
	}
	err := s.mms.Serve(ctx, conn)
	if s.onDisconnect != nil {
		s.onDisconnect(ConnectionEvent{})
	}
	return err
}

// ListenAndServe accepts connections on the given listener and serves
// each in a new goroutine. This blocks until the context is cancelled
// or the listener is closed.
//
// Each accepted connection is handled by [Server.Serve] in its own
// goroutine, which ensures that connection lifecycle hooks are invoked.
func (s *Server) ListenAndServe(ctx context.Context, ln mms.TransportListener) error {
	if s.onConnect == nil && s.onDisconnect == nil {
		return s.mms.ListenAndServe(ctx, ln)
	}
	return s.listenAndServeWithHooks(ctx, ln)
}

// listenAndServeWithHooks implements a custom accept loop that routes
// through [Server.Serve] so connection hooks are invoked.
func (s *Server) listenAndServeWithHooks(ctx context.Context, ln mms.TransportListener) error {
	defer func() { _ = ln.Close() }()

	for {
		transport, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logger.Error("iec61850: accept error", "error", err)
			return err
		}
		go func() {
			defer func() { _ = transport.Close() }()
			if serveErr := s.Serve(ctx, transport); serveErr != nil {
				s.logger.Debug("iec61850: connection closed", "error", serveErr)
			}
		}()
	}
}

// NewServerModelFromSCL builds a server data model from an SCL
// configuration. The iedName selects the IED; apName selects the
// AccessPoint (empty uses the first AccessPoint with a Server).
//
// This is a convenience wrapper around [servermodel.FromSCL].
func NewServerModelFromSCL(s *scl.SCL, iedName, apName string) (*servermodel.Model, error) {
	return servermodel.FromSCL(s, iedName, apName)
}
