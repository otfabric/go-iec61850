// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"math"
	"strings"
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

	// OnConfirmEdit is called when a client confirms an edit session,
	// before the engine copies the edit buffer into the group's value
	// bank. A non-volatile store (IEC 61850-8-1 §16.2.4) persists the
	// values here (read them with [SettingGroupEngine.SettingGroupValue]
	// after the call returns, or from the FC=SE attributes now). Return a
	// non-nil error to reject the confirmation.
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

	// The edit reservation (IEC 61850-7-2 16.3.3.1): the client that
	// selected editSG, and the timer of ResvTms (16.2.2.8). owner is nil
	// for a selection without an MMS association (server side).
	owner    *mms.ServerConn
	resvGen  uint64
	resvTime *time.Timer

	// The setting group values (IEC 61850-7-2 §16.1, Figure 22): one
	// bank per group with a value for every setting of the LD.
	// banks[g-1][i] is the value of settings[i] in group g. For a setting
	// with an FC=SG attribute, the active group's values are the FC=SG
	// attributes in the store (the "active buffer"), and its bank entry
	// is refreshed from them when another group becomes active.
	settings []servermodel.Setting
	index    map[string]int // SG and SE store key -> settings index
	banks    [][]*mms.Value
}

// SettingGroupEngine manages server-side setting group control
// blocks (IEC 61850-7-2 §16, IEC 61850-8-1 §16). It intercepts SGCB
// writes (ActSG, EditSG, CnfEdit), enforces the setting group lifecycle
// and the edit reservation, and keeps one value bank per setting group:
//
//   - FC=SG attributes hold the active group's values;
//   - SelectActiveSG (ActSG) loads the selected group's bank into them;
//   - SelectEditSG (EditSG) copies the group's values into the FC=SE
//     attributes, the edit buffer, which SetEditSGValue writes;
//   - ConfirmEditSGValues (CnfEdit) copies the edit buffer into the
//     group's bank, and into the FC=SG attributes when the group is the
//     active one; SelectEditSG 0 discards it.
//
// The banks start with the SCL values per group (Val sGroup, IEC
// 61850-6) or the attribute's value, and are kept in memory; see
// [SettingGroupEngine.SetSettingGroupValue] to restore persisted values.
// A change of an active setting goes through [Server.SetValue], so data
// change reports, logs and GOOSE follow it.
type SettingGroupEngine struct {
	mu     sync.RWMutex
	sgcbs  map[string]*sgcbRuntime // key: ldName
	store  *servermodel.ValueStore
	logger interface{ Debug(string, ...any) }

	// setLive changes an active (FC=SG) setting with notification of
	// reports, logs and GOOSE; nil stores without notification.
	setLive func(ctx context.Context, key string, val *mms.Value)

	// connActive reports whether an MMS association is still open; nil
	// treats every owner as connected.
	connActive func(*mms.ServerConn) bool

	// clock stamps LActTm (the server's Clock); nil is SystemClock.
	clock Clock
}

// newSettingGroupEngine creates a new engine backed by the given store.
func newSettingGroupEngine(store *servermodel.ValueStore, logger interface{ Debug(string, ...any) }) *SettingGroupEngine {
	return &SettingGroupEngine{
		sgcbs:  make(map[string]*sgcbRuntime),
		store:  store,
		logger: logger,
	}
}

// registerSGCB registers an SGCB runtime for the given LD with the
// setting leaves of the LD, and builds the value banks: a group's value
// is its value in the model, else the model's value for all groups, else
// the setting's current value in the store. The active group's values
// are then loaded into the FC=SG attributes.
func (e *SettingGroupEngine) registerSGCB(ldName string, sg *servermodel.SettingGroupDef, handler SettingGroupHandler, settings []servermodel.Setting) {
	actSG := sg.ActSG
	if actSG == 0 || actSG > sg.NumOfSGs {
		actSG = 1
	}

	rt := &sgcbRuntime{
		ldName:   ldName,
		numOfSGs: sg.NumOfSGs,
		actSG:    actSG,
		editSG:   0,
		resvTms:  sg.ResvTms,
		handler:  handler,
		settings: settings,
		index:    make(map[string]int, 2*len(settings)),
		banks:    make([][]*mms.Value, sg.NumOfSGs),
	}
	for g := range rt.banks {
		rt.banks[g] = make([]*mms.Value, len(settings))
	}
	for i, st := range settings {
		if st.SGKey != "" {
			rt.index[st.SGKey] = i
		}
		if st.SEKey != "" {
			rt.index[st.SEKey] = i
		}
		base := e.stored(st.SGKey, st)
		if st.SGKey == "" {
			base = e.stored(st.SEKey, st)
		}
		for g := range rt.banks {
			v := base
			if gv, ok := st.Groups[uint8(g+1)]; ok {
				v = gv
			} else if gv, ok := st.Groups[0]; ok {
				v = gv
			}
			rt.banks[g][i] = v
		}
		// The server is not serving yet: load without notification.
		if st.SGKey != "" {
			e.store.Set(st.SGKey, rt.banks[actSG-1][i])
		}
		if st.SEKey != "" {
			e.store.Set(st.SEKey, rt.banks[actSG-1][i])
		}
	}

	e.mu.Lock()
	e.sgcbs[ldName] = rt
	e.mu.Unlock()
}

// stored returns the store value under key, or the zero value of the
// setting's type.
func (e *SettingGroupEngine) stored(key string, st servermodel.Setting) *mms.Value {
	if key != "" {
		if v := e.store.Get(key); v != nil {
			return v
		}
	}
	return servermodel.DefaultValue(st.Type)
}

// groupValueLocked returns the value of settings[i] in group g: the
// FC=SG attribute for the active group, the bank otherwise. Must be
// called with rt.mu held.
func (e *SettingGroupEngine) groupValueLocked(rt *sgcbRuntime, g uint8, i int) *mms.Value {
	if st := rt.settings[i]; g == rt.actSG && st.SGKey != "" {
		return e.stored(st.SGKey, st)
	}
	return rt.banks[g-1][i]
}

// setActiveLocked changes the FC=SG attribute of settings[i] to v when
// it differs, notifying reports, logs and GOOSE. Must be called with
// rt.mu held.
func (e *SettingGroupEngine) setActiveLocked(ctx context.Context, rt *sgcbRuntime, i int, v *mms.Value) {
	st := rt.settings[i]
	if st.SGKey == "" {
		rt.banks[rt.actSG-1][i] = v
		return
	}
	if cur := e.store.Get(st.SGKey); cur != nil && cur.Equal(v) {
		return
	}
	if e.setLive != nil {
		e.setLive(ctx, st.SGKey, v)
		return
	}
	e.store.Set(st.SGKey, v)
}

// snapshotLocked returns the store values of the settings' FC=SG
// (se false) or FC=SE (se true) attributes, so values a handler callback
// sets can be told apart. Must be called with rt.mu held.
func (e *SettingGroupEngine) snapshotLocked(rt *sgcbRuntime, se bool) []*mms.Value {
	snap := make([]*mms.Value, len(rt.settings))
	for i, st := range rt.settings {
		key := st.SGKey
		if se {
			key = st.SEKey
		}
		if key != "" {
			snap[i] = e.store.Get(key)
		}
	}
	return snap
}

// changedSince reports whether the store value under key differs from
// the snapshot value before.
func (e *SettingGroupEngine) changedSince(key string, before *mms.Value) bool {
	if key == "" {
		return false
	}
	cur := e.store.Get(key)
	if cur == nil || before == nil {
		return cur != before
	}
	return !cur.Equal(before)
}

// HandleSGCBWrite intercepts writes to SGCB subfields and enforces
// setting group semantics. It returns nil when the write is accepted,
// and otherwise an error that carries the MMS DataAccessError of IEC
// 61850-8-1 8.1.3.4.4.2 / Table 23 (16.2.1, 16.2.2, 16.2.4):
// object-value-invalid for a group number out of range,
// type-inconsistent for a value of the wrong type, object-access-denied
// for a read-only member, temporarily-unavailable while another client
// holds the edit reservation or when no edit is selected. An error from
// a [SettingGroupHandler] callback keeps the code it carries (see
// [ErrWriteAccessDenied] and the other Err* write sentinels), and is
// object-access-denied otherwise.
//
// Supported subfields: ActSG, EditSG, CnfEdit. The client is the MMS
// association in ctx ([mms.ServerConnFromContext]).
func (e *SettingGroupEngine) HandleSGCBWrite(ctx context.Context, ldName, subfield string, val *mms.Value) error {
	e.mu.RLock()
	rt, ok := e.sgcbs[ldName]
	e.mu.RUnlock()
	if !ok {
		return asWriteError(fmt.Errorf("iec61850: SGCB write: unknown LD %q", ldName), mms.DataAccessErrorObjectNonExistent)
	}

	var err error
	switch subfield {
	case "ActSG":
		err = e.handleActSGWrite(ctx, rt, val)
	case "EditSG":
		err = e.handleEditSGWrite(ctx, rt, val)
	case "CnfEdit":
		err = e.handleCnfEditWrite(ctx, rt, val)
	default:
		// NumOfSG, LActTm, ResvTms are read-only (IEC 61850-7-2 Table 36;
		// IEC 61850-8-1 8.1.3.4.4.2: object-access-denied).
		err = fmt.Errorf("iec61850: SGCB write: read-only subfield %q: %w", subfield, ErrWriteAccessDenied)
	}
	return asWriteError(err, mms.DataAccessErrorObjectAccessDenied)
}

// sgcbGroup decodes a written ActSG/EditSG: INT8U (IEC 61850-7-2 clause
// 16). Another MMS type is type-inconsistent; a value above 255 is
// refused instead of truncated, which would select a different group
// (256 -> 0, 257 -> 1).
func sgcbGroup(name string, val *mms.Value) (uint8, error) {
	if val == nil || val.Type() != mms.ValueTypeUnsigned {
		return 0, fmt.Errorf("iec61850: %s write: expected unsigned: %w", name, ErrWriteTypeInconsistent)
	}
	u, _ := val.Uint64()
	if u > math.MaxUint8 {
		return 0, fmt.Errorf("iec61850: %s write: group %d out of range: %w", name, u, ErrWriteTypeInconsistent)
	}
	return uint8(u), nil
}

func (e *SettingGroupEngine) handleActSGWrite(ctx context.Context, rt *sgcbRuntime, val *mms.Value) error {
	newSG, err := sgcbGroup("ActSG", val)
	if err != nil {
		return err
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// IEC 61850-7-2 16.3.2: ActSG 1..NumOfSG; out of range is
	// parameter-value-inconsistent -> object-value-invalid (8-1 Table 23).
	if newSG == 0 || newSG > rt.numOfSGs {
		return fmt.Errorf("iec61850: ActSG write: group %d out of range [1, %d]: %w", newSG, rt.numOfSGs, ErrWriteValueInvalid)
	}

	var before []*mms.Value
	if rt.handler.OnActiveSGChanged != nil {
		before = e.snapshotLocked(rt, false)
		if err := rt.handler.OnActiveSGChanged(ctx, rt.ldName, newSG); err != nil {
			return fmt.Errorf("iec61850: ActSG write rejected: %w", err)
		}
	}
	e.activateLocked(ctx, rt, newSG, before)
	e.syncSGCBToStore(rt, true)

	e.logger.Debug("iec61850: active SG changed", "ld", rt.ldName, "actSG", newSG)
	return nil
}

// activateLocked makes newSG the active group (IEC 61850-7-2 16.2.2.4,
// Figure 22): the active values are kept in the old group's bank and the
// new group's bank is loaded into the FC=SG attributes. before is the
// snapshot of the FC=SG attributes before the OnActiveSGChanged callback,
// or nil: a value the callback set is the new group's value (how
// applications kept their own groups before v1.18.0). Must be called
// with rt.mu held.
func (e *SettingGroupEngine) activateLocked(ctx context.Context, rt *sgcbRuntime, newSG uint8, before []*mms.Value) {
	old := rt.actSG
	for i, st := range rt.settings {
		if st.SGKey == "" {
			continue
		}
		cur := e.stored(st.SGKey, st)
		if before == nil || !e.changedSince(st.SGKey, before[i]) {
			rt.banks[old-1][i] = cur
			continue
		}
		if before[i] != nil {
			rt.banks[old-1][i] = before[i]
		}
		rt.banks[newSG-1][i] = cur
	}
	rt.actSG = newSG
	for i := range rt.settings {
		e.setActiveLocked(ctx, rt, i, rt.banks[newSG-1][i])
	}
}

// liveOwnerLocked reports the edit reservation's owner, releasing the
// reservation first when the owner's association was lost (IEC 61850-7-2
// 16.3.3.1). Must be called with rt.mu held.
func (e *SettingGroupEngine) liveOwnerLocked(rt *sgcbRuntime) *mms.ServerConn {
	if rt.editSG != 0 && rt.owner != nil && e.connActive != nil && !e.connActive(rt.owner) {
		e.logger.Debug("iec61850: edit SG released: association lost", "ld", rt.ldName)
		e.releaseEditLocked(rt)
	}
	return rt.owner
}

// releaseEditLocked ends the edit reservation. Must be called with rt.mu
// held.
func (e *SettingGroupEngine) releaseEditLocked(rt *sgcbRuntime) {
	rt.editSG = 0
	rt.owner = nil
	rt.resvGen++
	if rt.resvTime != nil {
		rt.resvTime.Stop()
		rt.resvTime = nil
	}
	e.syncSGCBToStore(rt, false)
}

// reservedByOtherLocked reports whether another client holds the edit
// reservation. Must be called with rt.mu held.
func (e *SettingGroupEngine) reservedByOtherLocked(rt *sgcbRuntime, sc *mms.ServerConn) bool {
	owner := e.liveOwnerLocked(rt)
	return rt.editSG != 0 && owner != nil && owner != sc
}

func (e *SettingGroupEngine) handleEditSGWrite(ctx context.Context, rt *sgcbRuntime, val *mms.Value) error {
	editSG, err := sgcbGroup("EditSG", val)
	if err != nil {
		return err
	}
	sc := mms.ServerConnFromContext(ctx)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// The reservation belongs to one client (IEC 61850-7-2 16.3.3.1);
	// another client's SelectEditSG, including 0, is
	// instance-locked-by-other-client -> temporarily-unavailable (8-1
	// 8.1.3.4.4.2).
	if e.reservedByOtherLocked(rt, sc) {
		return fmt.Errorf("iec61850: EditSG write: edit of group %d reserved by another client: %w", rt.editSG, ErrWriteTemporarilyUnavailable)
	}

	if editSG == 0 {
		// SelectEditSG with 0 cancels the edit (7-2 16.3.3.1).
		e.releaseEditLocked(rt)
		e.logger.Debug("iec61850: edit SG released", "ld", rt.ldName)
		return nil
	}

	if editSG > rt.numOfSGs {
		return fmt.Errorf("iec61850: EditSG write: group %d out of range [1, %d]: %w", editSG, rt.numOfSGs, ErrWriteValueInvalid)
	}

	var before []*mms.Value
	if rt.handler.OnEditSGSelected != nil {
		before = e.snapshotLocked(rt, true)
		if err := rt.handler.OnEditSGSelected(ctx, rt.ldName, editSG); err != nil {
			return fmt.Errorf("iec61850: EditSG write rejected: %w", err)
		}
	}

	// SelectEditSG copies the group's values into the edit buffer, FC=SE
	// (IEC 61850-7-2 16.2.2.5, 16.3.3.1). A value the callback set stays.
	for i, st := range rt.settings {
		if st.SEKey == "" || before != nil && e.changedSince(st.SEKey, before[i]) {
			continue
		}
		e.store.Set(st.SEKey, e.groupValueLocked(rt, editSG, i))
	}

	rt.editSG = editSG
	rt.owner = sc
	e.armReservationLocked(rt)
	e.syncSGCBToStore(rt, false)

	e.logger.Debug("iec61850: edit SG selected", "ld", rt.ldName, "editSG", editSG)
	return nil
}

// armReservationLocked (re)starts the ResvTms timer of the reservation
// (IEC 61850-7-2 16.2.2.8, 16.3.3.1: the reservation ends when no
// ConfirmEditSGValues arrives within ResvTms seconds of SelectEditSG).
// ResvTms 0 means no timeout. Must be called with rt.mu held.
func (e *SettingGroupEngine) armReservationLocked(rt *sgcbRuntime) {
	rt.resvGen++
	if rt.resvTime != nil {
		rt.resvTime.Stop()
		rt.resvTime = nil
	}
	if rt.resvTms == 0 {
		return
	}
	gen := rt.resvGen
	rt.resvTime = time.AfterFunc(time.Duration(rt.resvTms)*time.Second, func() {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.resvGen != gen || rt.editSG == 0 {
			return
		}
		e.logger.Debug("iec61850: edit SG released: ResvTms expired", "ld", rt.ldName)
		e.releaseEditLocked(rt)
	})
}

func (e *SettingGroupEngine) handleCnfEditWrite(ctx context.Context, rt *sgcbRuntime, val *mms.Value) error {
	if val == nil || val.Type() != mms.ValueTypeBoolean {
		return fmt.Errorf("iec61850: CnfEdit write: expected boolean: %w", ErrWriteTypeInconsistent)
	}
	b, _ := val.Bool()
	if !b {
		return nil
	}
	sc := mms.ServerConnFromContext(ctx)

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// ConfirmEditSGValues needs a selected edit group, reserved by this
	// client; otherwise writability depends on another object (EditSG):
	// temporarily-unavailable (IEC 61850-8-1 8.1.3.4.4.2).
	if e.reservedByOtherLocked(rt, sc) {
		return fmt.Errorf("iec61850: CnfEdit write: edit reserved by another client: %w", ErrWriteTemporarilyUnavailable)
	}
	if rt.editSG == 0 {
		return fmt.Errorf("iec61850: CnfEdit write: no edit session active: %w", ErrWriteTemporarilyUnavailable)
	}

	if rt.handler.OnConfirmEdit != nil {
		if err := rt.handler.OnConfirmEdit(ctx, rt.ldName, rt.editSG); err != nil {
			return fmt.Errorf("iec61850: CnfEdit write rejected: %w", err)
		}
	}

	// The edit buffer overwrites the group's values (IEC 61850-7-2
	// 16.2.2.5, 16.3.5.1); an edit of the active group re-activates it
	// with the new values.
	confirmedSG := rt.editSG
	for i, st := range rt.settings {
		if st.SEKey == "" {
			continue
		}
		v := e.stored(st.SEKey, st)
		if confirmedSG == rt.actSG {
			e.setActiveLocked(ctx, rt, i, v)
		} else {
			rt.banks[confirmedSG-1][i] = v
		}
	}
	e.releaseEditLocked(rt)

	e.logger.Debug("iec61850: edit confirmed", "ld", rt.ldName, "sg", confirmedSG)
	return nil
}

// checkEditAccess is the [servermodel.EditGuard] of FC=SE attributes:
// with no edit group selected, SetEditSGValue and GetEditSGValue get
// Response- (IEC 61850-7-2 16.2.2.5), temporarily-unavailable since the
// access depends on another object (IEC 61850-8-1 8.1.3.4.4.2). A write
// by a client other than the one holding the reservation is refused the
// same way (7-2 16.3.3.1).
func (e *SettingGroupEngine) checkEditAccess(ctx context.Context, storeKey string, write bool) error {
	ld, _, ok := strings.Cut(storeKey, "/")
	if !ok {
		return nil
	}
	e.mu.RLock()
	rt, ok := e.sgcbs[ld]
	e.mu.RUnlock()
	if !ok {
		return nil
	}
	sc := mms.ServerConnFromContext(ctx)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	owner := e.liveOwnerLocked(rt)
	if rt.editSG == 0 {
		return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	}
	if write && owner != nil && owner != sc {
		return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
	}
	return nil
}

// stop ends the ResvTms timers.
func (e *SettingGroupEngine) stop() {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, rt := range e.sgcbs {
		rt.mu.Lock()
		rt.resvGen++
		if rt.resvTime != nil {
			rt.resvTime.Stop()
			rt.resvTime = nil
		}
		rt.mu.Unlock()
	}
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
		e.store.Set(prefix+"$LActTm", stampUTC(e.clock))
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
	engine.clock = s.clock

	for i := range s.model.LogicalDevices {
		ld := &s.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			if ln.SettingGroup != nil {
				settings, err := servermodel.Settings(ld)
				if err != nil {
					// The model passed validation; a per-group value that
					// does not parse leaves the groups at the current value.
					s.logger.Warn("iec61850: setting group values ignored", "ld", ld.Name, "error", err)
					settings, _ = servermodel.Settings(withoutGroupValues(ld))
				}
				engine.registerSGCB(ld.Name, ln.SettingGroup, handler, settings)
			}
		}
	}

	engine.connActive = func(sc *mms.ServerConn) bool { return s.mms == nil || s.isConnActive(sc) }
	engine.setLive = s.SetValue
	s.sgEngine = engine
	s.store.SetEditGuard(engine.checkEditAccess)
	s.installWriteInterceptor()
}

// SettingGroupEngine returns the setting group engine, or nil if
// setting groups have not been enabled via [Server.EnableSettingGroups].
func (s *Server) SettingGroupEngine() *SettingGroupEngine {
	if s.enginesClosed.Load() {
		return nil
	}
	return s.sgEngine
}

// ChangeActiveSettingGroup programmatically changes the active
// setting group for the given LD. This is for server-side use
// (e.g., operator override). The handler's OnActiveSGChanged
// callback is invoked.
func (s *Server) ChangeActiveSettingGroup(ctx context.Context, ld string, sg uint8) error {
	e := s.SettingGroupEngine()
	if e == nil {
		return fmt.Errorf("iec61850: setting groups not enabled")
	}
	return e.HandleSGCBWrite(ctx, ld, "ActSG", mms.NewUnsigned(uint64(sg)))
}

// withoutGroupValues returns a copy of ld without per-group values.
func withoutGroupValues(ld *servermodel.LogicalDevice) *servermodel.LogicalDevice {
	cp := *ld
	cp.LogicalNodes = make([]servermodel.LogicalNode, len(ld.LogicalNodes))
	var strip func(a []servermodel.DataAttribute) []servermodel.DataAttribute
	strip = func(a []servermodel.DataAttribute) []servermodel.DataAttribute {
		out := make([]servermodel.DataAttribute, len(a))
		for i := range a {
			out[i] = a[i]
			out[i].GroupValues = nil
			out[i].Children = strip(a[i].Children)
		}
		return out
	}
	var stripDO func(o []servermodel.DataObject) []servermodel.DataObject
	stripDO = func(o []servermodel.DataObject) []servermodel.DataObject {
		out := make([]servermodel.DataObject, len(o))
		for i := range o {
			out[i] = o[i]
			out[i].Attributes = strip(o[i].Attributes)
			out[i].Children = stripDO(o[i].Children)
		}
		return out
	}
	for i := range ld.LogicalNodes {
		cp.LogicalNodes[i] = ld.LogicalNodes[i]
		cp.LogicalNodes[i].DataObjects = stripDO(ld.LogicalNodes[i].DataObjects)
	}
	return &cp
}

// settingRuntime returns the SGCB runtime and setting index of the
// setting under store key (its FC=SG or FC=SE attribute).
func (e *SettingGroupEngine) settingRuntime(key string) (*sgcbRuntime, int, error) {
	ld, _, ok := strings.Cut(key, "/")
	if !ok {
		return nil, 0, fmt.Errorf("iec61850: setting %q: %w: not a store key", key, ErrInvalidArgument)
	}
	e.mu.RLock()
	rt, ok := e.sgcbs[ld]
	e.mu.RUnlock()
	if !ok {
		return nil, 0, fmt.Errorf("iec61850: setting %q: %w: no SGCB in LD %q", key, ErrNotFound, ld)
	}
	i, ok := rt.index[key]
	if !ok {
		return nil, 0, fmt.Errorf("iec61850: setting %q: %w: not an FC=SG or FC=SE attribute", key, ErrNotFound)
	}
	return rt, i, nil
}

// checkGroup validates a group number (numOfSGs is immutable).
func checkGroup(rt *sgcbRuntime, key string, group uint8) error {
	if group == 0 || group > rt.numOfSGs {
		return fmt.Errorf("iec61850: setting %q: %w: group %d out of range [1, %d]", key, ErrInvalidArgument, group, rt.numOfSGs)
	}
	return nil
}

// SettingKeys returns the settings of the setting groups of ld (every
// FC=SG and FC=SE leaf of the LD, IEC 61850-7-2 §16.1), one store key per
// setting: its FC=SG attribute, or FC=SE when the model has no FC=SG one.
// Nil when ld has no SGCB.
func (e *SettingGroupEngine) SettingKeys(ld string) []string {
	e.mu.RLock()
	rt, ok := e.sgcbs[ld]
	e.mu.RUnlock()
	if !ok {
		return nil
	}
	keys := make([]string, len(rt.settings))
	for i, st := range rt.settings {
		keys[i] = st.SGKey
		if keys[i] == "" {
			keys[i] = st.SEKey
		}
	}
	return keys
}

// SettingGroupValue returns the value of a setting in setting group
// group (1..NumOfSG). key is the store key of the setting's FC=SG or
// FC=SE attribute (see [Server.SetValue] and [SettingGroupEngine.SettingKeys]). For the
// active group this is the FC=SG value; an edit not yet confirmed is not
// included (IEC 61850-7-2 16.2.2.5). It returns [ErrNotFound] for a key
// that is not a setting of an LD with an SGCB and [ErrInvalidArgument]
// for a group out of range.
func (e *SettingGroupEngine) SettingGroupValue(key string, group uint8) (*mms.Value, error) {
	rt, i, err := e.settingRuntime(key)
	if err != nil {
		return nil, err
	}
	if err := checkGroup(rt, key, group); err != nil {
		return nil, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return e.groupValueLocked(rt, group, i), nil
}

// SetSettingGroupValue sets the value of a setting in setting group
// group (1..NumOfSG), for example to restore values the application
// persisted (IEC 61850-8-1 §16.1: the groups are non-volatile). key is
// as for [SettingGroupEngine.SettingGroupValue]. For the active group
// the FC=SG attribute changes, with data change reports, logs and
// GOOSE. An edit buffer
// already loaded for that group is not changed; the next SelectEditSG
// loads the new value. It returns [ErrTypeMismatch] when val does not
// match the setting's type.
func (e *SettingGroupEngine) SetSettingGroupValue(ctx context.Context, key string, group uint8, val *mms.Value) error {
	rt, i, err := e.settingRuntime(key)
	if err != nil {
		return err
	}
	if err := checkGroup(rt, key, group); err != nil {
		return err
	}
	if err := servermodel.CheckWriteValue(val, rt.settings[i].Type); err != nil {
		return fmt.Errorf("iec61850: setting %q: %w: %w", key, ErrTypeMismatch, err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if group == rt.actSG {
		e.setActiveLocked(ctx, rt, i, val)
		return nil
	}
	rt.banks[group-1][i] = val
	return nil
}
