// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// SettingGroupHandler provides application callbacks for setting
// group operations. All callbacks are optional; nil callbacks accept
// the operation unconditionally.
type SettingGroupHandler struct {
	// OnActiveSGChanged is called when a client selects a new active
	// setting group. Return a non-nil error to reject the activation.
	// The newSG is 1-based.
	OnActiveSGChanged func(ctx context.Context, ld string, newSG uint8) error

	// OnEditSGSelected is called when a client selects a group for
	// editing. Return a non-nil error to reject the selection (e.g.,
	// if another edit session is in progress from a different source).
	OnEditSGSelected func(ctx context.Context, ld string, editSG uint8) error

	// OnConfirmEdit is called when a client confirms an edit session.
	// The application should persist or apply the edited values.
	// Return a non-nil error to reject the confirmation.
	OnConfirmEdit func(ctx context.Context, ld string, editSG uint8) error
}

// sgcbRuntime holds the live state for one SGCB.
type sgcbRuntime struct {
	mu       sync.Mutex
	ldName   string
	numOfSGs uint8
	actSG    uint8
	editSG   uint8
	resvTms  uint16
	handler  SettingGroupHandler
}

// SettingGroupEngine manages server-side setting group control
// blocks. It intercepts SGCB writes (ActSG, EditSG, CnfEdit) and
// enforces the setting group lifecycle.
//
// # Interoperability note
//
// This implementation covers the core setting group workflow
// (active selection, edit reservation, confirm/commit). Reservation
// timeouts, multi-connection ownership tracking, and persistent
// setting group storage are not yet implemented. These are
// documented in KNOWN_LIMITATIONS.md.
type SettingGroupEngine struct {
	mu     sync.RWMutex
	sgcbs  map[string]*sgcbRuntime // key: ldName
	store  *servermodel.ValueStore
	logger interface{ Debug(string, ...any) }
}

// newSettingGroupEngine creates a new engine backed by the given store.
func newSettingGroupEngine(store *servermodel.ValueStore, logger interface{ Debug(string, ...any) }) *SettingGroupEngine {
	return &SettingGroupEngine{
		sgcbs:  make(map[string]*sgcbRuntime),
		store:  store,
		logger: logger,
	}
}

// registerSGCB registers an SGCB runtime for the given LD.
func (e *SettingGroupEngine) registerSGCB(ldName string, sg *servermodel.SettingGroupDef, handler SettingGroupHandler) {
	actSG := sg.ActSG
	if actSG == 0 {
		actSG = 1
	}

	rt := &sgcbRuntime{
		ldName:   ldName,
		numOfSGs: sg.NumOfSGs,
		actSG:    actSG,
		editSG:   0,
		resvTms:  sg.ResvTms,
		handler:  handler,
	}

	e.mu.Lock()
	e.sgcbs[ldName] = rt
	e.mu.Unlock()
}

// HandleSGCBWrite intercepts writes to SGCB subfields and enforces
// setting group semantics. Returns nil if the write is handled
// (accepted or rejected with an MMS error); returns an error to
// propagate to the MMS layer as a write failure.
//
// Supported subfields: ActSG, EditSG, CnfEdit.
func (e *SettingGroupEngine) HandleSGCBWrite(ctx context.Context, ldName, subfield string, val *mms.Value) error {
	e.mu.RLock()
	rt, ok := e.sgcbs[ldName]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("iec61850: SGCB write: unknown LD %q", ldName)
	}

	switch subfield {
	case "ActSG":
		return e.handleActSGWrite(ctx, rt, val)
	case "EditSG":
		return e.handleEditSGWrite(ctx, rt, val)
	case "CnfEdit":
		return e.handleCnfEditWrite(ctx, rt, val)
	default:
		return fmt.Errorf("iec61850: SGCB write: read-only subfield %q", subfield)
	}
}

func (e *SettingGroupEngine) handleActSGWrite(ctx context.Context, rt *sgcbRuntime, val *mms.Value) error {
	u, ok := val.Uint32()
	if !ok {
		return fmt.Errorf("iec61850: ActSG write: expected unsigned")
	}
	newSG := uint8(u)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if newSG == 0 || newSG > rt.numOfSGs {
		return fmt.Errorf("iec61850: ActSG write: group %d out of range [1, %d]", newSG, rt.numOfSGs)
	}

	if rt.handler.OnActiveSGChanged != nil {
		if err := rt.handler.OnActiveSGChanged(ctx, rt.ldName, newSG); err != nil {
			return fmt.Errorf("iec61850: ActSG write rejected: %w", err)
		}
	}

	rt.actSG = newSG
	e.syncSGCBToStore(rt, true)

	e.logger.Debug("iec61850: active SG changed", "ld", rt.ldName, "actSG", newSG)
	return nil
}

func (e *SettingGroupEngine) handleEditSGWrite(ctx context.Context, rt *sgcbRuntime, val *mms.Value) error {
	u, ok := val.Uint32()
	if !ok {
		return fmt.Errorf("iec61850: EditSG write: expected unsigned")
	}
	editSG := uint8(u)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if editSG == 0 {
		rt.editSG = 0
		e.syncSGCBToStore(rt, false)
		e.logger.Debug("iec61850: edit SG released", "ld", rt.ldName)
		return nil
	}

	if editSG > rt.numOfSGs {
		return fmt.Errorf("iec61850: EditSG write: group %d out of range [1, %d]", editSG, rt.numOfSGs)
	}

	if rt.editSG != 0 && rt.editSG != editSG {
		return fmt.Errorf("iec61850: EditSG write: edit session already active for group %d", rt.editSG)
	}

	if rt.handler.OnEditSGSelected != nil {
		if err := rt.handler.OnEditSGSelected(ctx, rt.ldName, editSG); err != nil {
			return fmt.Errorf("iec61850: EditSG write rejected: %w", err)
		}
	}

	rt.editSG = editSG
	e.syncSGCBToStore(rt, false)

	e.logger.Debug("iec61850: edit SG selected", "ld", rt.ldName, "editSG", editSG)
	return nil
}

func (e *SettingGroupEngine) handleCnfEditWrite(ctx context.Context, rt *sgcbRuntime, val *mms.Value) error {
	b, ok := val.Bool()
	if !ok {
		return fmt.Errorf("iec61850: CnfEdit write: expected boolean")
	}
	if !b {
		return nil
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.editSG == 0 {
		return fmt.Errorf("iec61850: CnfEdit write: no edit session active")
	}

	if rt.handler.OnConfirmEdit != nil {
		if err := rt.handler.OnConfirmEdit(ctx, rt.ldName, rt.editSG); err != nil {
			return fmt.Errorf("iec61850: CnfEdit write rejected: %w", err)
		}
	}

	confirmedSG := rt.editSG
	rt.editSG = 0
	e.syncSGCBToStore(rt, false)

	e.logger.Debug("iec61850: edit confirmed", "ld", rt.ldName, "sg", confirmedSG)
	return nil
}

// syncSGCBToStore writes the runtime SGCB state back to the ValueStore.
// Must be called with rt.mu held.
//
// NumOfSGs and ResvTms are immutable after model creation and are not
// updated here — they are set once during registerSGCB.
//
// LActTm (last activation time) is only updated when updateLActTm is
// true, which should only be on ActSG changes, not on edit operations.
func (e *SettingGroupEngine) syncSGCBToStore(rt *sgcbRuntime, updateLActTm bool) {
	// Invariant: these store.Set calls go directly to ValueStore.Set
	// and do NOT trigger the write interceptor. Do not change this
	// to route through MMS write callbacks.
	prefix := servermodel.StoreKey(rt.ldName, "LLN0$SP$SGCB")
	e.store.Set(prefix+"$ActSG", mms.NewUnsigned(uint64(rt.actSG)))
	e.store.Set(prefix+"$EditSG", mms.NewUnsigned(uint64(rt.editSG)))
	e.store.Set(prefix+"$CnfEdit", mms.NewBoolean(false))
	if updateLActTm {
		e.store.Set(prefix+"$LActTm", mms.NewUTCTime(time.Now()))
	}
}

// GetActiveSettingGroup returns the currently active setting group
// for the given LD. Returns 0 if no SGCB is registered.
func (e *SettingGroupEngine) GetActiveSettingGroup(ldName string) uint8 {
	e.mu.RLock()
	rt, ok := e.sgcbs[ldName]
	e.mu.RUnlock()
	if !ok {
		return 0
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.actSG
}

// GetEditSettingGroup returns the setting group currently under
// edit for the given LD. Returns 0 if no edit session is active.
func (e *SettingGroupEngine) GetEditSettingGroup(ldName string) uint8 {
	e.mu.RLock()
	rt, ok := e.sgcbs[ldName]
	e.mu.RUnlock()
	if !ok {
		return 0
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.editSG
}

// EnableSettingGroups initialises the setting group engine for the
// server. It scans the server model for SGCB definitions and
// registers them with the provided handler.
//
// This should be called after [NewServer] and before accepting
// connections.
func (s *Server) EnableSettingGroups(handler SettingGroupHandler) {
	engine := newSettingGroupEngine(s.store, s.logger)

	for i := range s.model.LogicalDevices {
		ld := &s.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			if ln.SettingGroup != nil {
				engine.registerSGCB(ld.Name, ln.SettingGroup, handler)
			}
		}
	}

	s.sgEngine = engine
	s.installWriteInterceptor()
}

// SettingGroupEngine returns the setting group engine, or nil if
// setting groups have not been enabled via [Server.EnableSettingGroups].
func (s *Server) SettingGroupEngine() *SettingGroupEngine {
	return s.sgEngine
}

// ChangeActiveSettingGroup programmatically changes the active
// setting group for the given LD. This is for server-side use
// (e.g., operator override). The handler's OnActiveSGChanged
// callback is invoked.
func (s *Server) ChangeActiveSettingGroup(ctx context.Context, ld string, sg uint8) error {
	if s.sgEngine == nil {
		return fmt.Errorf("iec61850: setting groups not enabled")
	}
	return s.sgEngine.HandleSGCBWrite(ctx, ld, "ActSG", mms.NewUnsigned(uint64(sg)))
}
