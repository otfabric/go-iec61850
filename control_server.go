// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// callControlHandler invokes fn, recovering from any panic and converting it
// to an error so that a misbehaving application callback cannot crash the server.
func callControlHandler(fn func(context.Context, ControlRequest) error, ctx context.Context, req ControlRequest) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &ControlError{
				Ref:       req.Ref,
				Operation: req.Operation,
				Wrapped:   fmt.Errorf("panic in ControlHandler callback: %v", r),
			}
		}
	}()
	return fn(ctx, req)
}

// ControlHandler defines the server-side callbacks for a controllable
// data object. Implement the methods relevant to the control model;
// unset (nil) handlers use default accept/reject behavior.
//
// All handler methods receive a context from the MMS layer and a
// [ControlRequest] describing the command.
type ControlHandler struct {
	// SelectTimeout overrides [DefaultSelectTimeout] for this control
	// object. The select reservation expires this long after a
	// successful Select or SelectWithValue. A zero value means use
	// [DefaultSelectTimeout].
	SelectTimeout time.Duration

	// OnSelect is called when a client issues a Select (SBO) or
	// SelectWithValue (SBOw) request. Return nil to accept the
	// select, or an error (preferably wrapping [ErrSelectFailed]
	// with an [AddCause]) to deny it.
	//
	// If nil, selects are accepted unconditionally.
	OnSelect func(ctx context.Context, req ControlRequest) error

	// OnOperate is called when a client issues an Operate request.
	// Return nil to accept the operate, or an error (preferably
	// wrapping [ErrOperateFailed] with an [AddCause]) to deny it.
	//
	// If nil, operates are accepted unconditionally and the value
	// is written to the ValueStore.
	OnOperate func(ctx context.Context, req ControlRequest) error

	// OnCancel is called when a client issues a Cancel request.
	// Return nil to accept the cancellation, or an error to deny.
	//
	// If nil, cancels are accepted unconditionally.
	OnCancel func(ctx context.Context, req ControlRequest) error
}

// ControlRequest contains the decoded parameters from a client
// control command (Operate, Select, or Cancel).
type ControlRequest struct {
	// Ref is the IEC 61850 object reference of the controlled object.
	Ref string

	// Operation is "operate", "select", or "cancel".
	Operation string

	// CtlVal is the control value.
	CtlVal *mms.Value

	// Origin is the command originator.
	Origin Origin

	// CtlNum is the control sequence number.
	CtlNum uint8

	// OperTm is the scheduled operation time (zero for immediate).
	OperTm time.Time

	// Test indicates whether this is a test command.
	Test bool

	// Check contains synchrocheck/interlockCheck bits.
	Check CheckConditions
}

// controlRegistration holds a registered control handler and its
// associated SBO state for a single controllable data object.
//
// For SBO enhanced (SBOw), ownership is tracked by serialized
// originator identity (OrCat:OrIdent) AND by connection pointer so
// that two clients with the same origin identity can be distinguished.
// For SBO normal, ownership is tracked solely by the MMS ServerConn
// pointer, since the SBO select is a Read request that carries no
// origin information.
type controlRegistration struct {
	handler       ControlHandler
	ctlModel      CtlModel
	selectTimeout time.Duration // effective per-control timeout

	mu           sync.Mutex
	selectOwner  string          // "orCat:orIdent" — originator for SBOw enhanced
	selectConn   *mms.ServerConn // connection identity for SBO normal (and SBOw contention)
	selectTime   time.Time
	selectCtlNum uint8
	selectCtlVal *mms.Value // SBOw: ctlVal from SelectWithValue, checked on Operate
}

// DefaultSelectTimeout is the default SBO select timeout applied when
// no per-registration timeout is configured.
//
// Uses wall-clock time via [time.Since]. On systems where the wall
// clock can jump (NTP corrections, suspend/resume), the effective
// timeout may differ from the intended duration. For
// safety-critical applications, consider shorter timeouts and
// monitoring clock skew.
const DefaultSelectTimeout = 30 * time.Second

// RegisterControl registers a [ControlHandler] for the controllable
// data object identified by ldName and doRef (e.g. "GGIO1.SPCSO1").
// The ctlModel determines the expected control flow.
//
// The handler callbacks are invoked from within the MMS write path
// when a client writes to the Oper, SBO, SBOw, or Cancel subattributes.
func (s *Server) RegisterControl(ldName, doRef string, ctlModel CtlModel, handler ControlHandler) error {
	if ldName == "" || doRef == "" {
		return fmt.Errorf("iec61850: register control: %w: empty ldName or doRef", ErrInvalidArgument)
	}
	if !ctlModel.IsControllable() {
		return fmt.Errorf("iec61850: register control %s/%s: %w: status-only model", ldName, doRef, ErrNotControllable)
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	key := ldName + "/" + doRef
	if _, exists := s.controls[key]; exists {
		return fmt.Errorf("iec61850: register control: %w: already registered: %s", ErrInvalidArgument, key)
	}

	reg := &controlRegistration{
		handler:       handler,
		ctlModel:      ctlModel,
		selectTimeout: handler.SelectTimeout,
	}
	if reg.selectTimeout <= 0 {
		reg.selectTimeout = DefaultSelectTimeout
	}
	s.controls[key] = reg

	// For SBO normal models, override the SBO[CO] variable's Read handler
	// so that a client Read to SBO[CO] processes the select and returns
	// the control reference string.
	if ctlModel == CtlModelSBONormal {
		s.installSBONormalReadHandler(ldName, doRef, reg)
	}

	s.logger.Debug("iec61850: control registered",
		"ld", ldName, "do", doRef, "ctlModel", ctlModel)

	return nil
}

// handleControlWrite is the server-side dispatch for control writes.
// It parses the MMS structure, looks up the handler, and executes
// the appropriate control flow.
func (s *Server) handleControlWrite(ctx context.Context, ldName, lnName string, path []string, subAttr string, val *mms.Value) error { //nolint:unparam // ldName varies at runtime; tests only exercise one LD
	doPath := pathWithoutSuffix(path, subAttr)
	doRef := lnName + "." + joinDotPath(doPath)
	key := ldName + "/" + doRef

	s.controlMu.RLock()
	reg, ok := s.controls[key]
	s.controlMu.RUnlock()

	if !ok {
		return nil
	}

	ref := ldName + "/" + doRef
	req, err := decodeControlRequest(ref, subAttr, val)
	if err != nil {
		return fmt.Errorf("iec61850: control %s: decode: %w", ref, err)
	}

	switch subAttr {
	case "Oper":
		return s.executeOperate(ctx, reg, req)
	case "SBO":
		return s.executeSelect(ctx, reg, req)
	case "SBOw":
		return s.executeSelectWithValue(ctx, reg, req)
	case "Cancel":
		return s.executeCancel(ctx, reg, req)
	default:
		return nil
	}
}

func (s *Server) executeOperate(ctx context.Context, reg *controlRegistration, req ControlRequest) error {
	if reg.ctlModel.IsSBO() {
		reg.mu.Lock()

		if reg.ctlModel == CtlModelSBONormal {
			// SBO normal: check for active select granted via either the
			// Read handler (selectConn) or the Write-to-SBO path (selectOwner).
			sc := mms.ServerConnFromContext(ctx)
			conn := reg.selectConn
			owner := reg.selectOwner
			selectTime := reg.selectTime

			if conn == nil && owner == "" {
				// No select active.
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: no active SBO select", "ref", req.Ref)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			if time.Since(selectTime) > reg.selectTimeout {
				reg.selectConn = nil
				reg.selectOwner = ""
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: SBO select timed out", "ref", req.Ref)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			// Ownership check: if select was via Read, enforce connection identity;
			// if via Write, enforce origin identity.
			if conn != nil && sc != nil && conn != sc {
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: SBO connection mismatch", "ref", req.Ref)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			if conn == nil && owner != "" {
				// Write-path select: enforce origin identity.
				ownerKey := fmt.Sprintf("%d:%x", req.Origin.OrCat, req.Origin.OrIdent)
				if owner != ownerKey {
					reg.mu.Unlock()
					s.logger.Warn("iec61850: operate denied: SBO origin mismatch",
						"ref", req.Ref, "selectOwner", owner, "operateOwner", ownerKey)
					return &ControlError{
						Ref:       req.Ref,
						Operation: "operate",
						AddCause:  AddCauseSelectFailed,
						Wrapped:   ErrOperateFailed,
					}
				}
			}
			reg.selectConn = nil
			reg.selectOwner = ""
			reg.mu.Unlock()
		} else {
			// SBO enhanced: ownership is by origin identity AND connection.
			sc := mms.ServerConnFromContext(ctx)
			ownerKey := fmt.Sprintf("%d:%x", req.Origin.OrCat, req.Origin.OrIdent)
			currentOwner := reg.selectOwner
			currentConn := reg.selectConn
			selectTime := reg.selectTime
			selectCtlNum := reg.selectCtlNum
			selectCtlVal := reg.selectCtlVal

			if currentOwner == "" || time.Since(selectTime) > reg.selectTimeout {
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: no active select", "ref", req.Ref)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			if currentOwner != ownerKey {
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: owner mismatch",
					"ref", req.Ref, "selectOwner", currentOwner, "operateOwner", ownerKey)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			// Enforce connection identity when both owners share the same origin.
			if currentConn != nil && sc != nil && currentConn != sc {
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: SBOw connection mismatch", "ref", req.Ref)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			if reg.ctlModel.IsEnhanced() && selectCtlNum != req.CtlNum {
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: ctlNum mismatch",
					"ref", req.Ref, "selectCtlNum", selectCtlNum, "operateCtlNum", req.CtlNum)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseSelectFailed,
					Wrapped:   ErrOperateFailed,
				}
			}
			// Enforce ctlVal match between SelectWithValue and Operate.
			if reg.ctlModel.IsEnhanced() && selectCtlVal != nil && !mmsValuesEqual(selectCtlVal, req.CtlVal) {
				reg.mu.Unlock()
				s.logger.Warn("iec61850: operate denied: ctlVal mismatch",
					"ref", req.Ref)
				return &ControlError{
					Ref:       req.Ref,
					Operation: "operate",
					AddCause:  AddCauseParameterChange,
					Wrapped:   ErrOperateFailed,
				}
			}
			reg.selectOwner = ""
			reg.selectConn = nil
			reg.selectCtlVal = nil
			reg.mu.Unlock()
		}
	}

	if reg.handler.OnOperate != nil {
		if err := callControlHandler(reg.handler.OnOperate, ctx, req); err != nil {
			return err
		}
		return nil
	}

	// Default fallback (demo-grade): when no OnOperate handler is
	// registered, write CtlVal directly to the ValueStore's stVal.
	// This is a convenience for simple SPC/DPC demos and tests.
	// Real applications should provide an explicit OnOperate handler
	// that implements proper CDC-aware command processing,
	// interlocking, and application-specific validation.
	if req.CtlVal != nil && s.store != nil {
		storeKey := s.controlStoreKey(req.Ref)
		if storeKey != "" {
			s.SetValue(ctx, storeKey, req.CtlVal)
		}
	}
	return nil
}

func (s *Server) executeSelect(ctx context.Context, reg *controlRegistration, req ControlRequest) error {
	if !reg.ctlModel.IsSBO() {
		return &ControlError{
			Ref:       req.Ref,
			Operation: "select",
			Wrapped:   fmt.Errorf("ctlModel %s does not support SBO", reg.ctlModel),
		}
	}

	if reg.handler.OnSelect != nil {
		if err := callControlHandler(reg.handler.OnSelect, ctx, req); err != nil {
			return err
		}
	}

	sc := mms.ServerConnFromContext(ctx)
	ownerKey := fmt.Sprintf("%d:%x", req.Origin.OrCat, req.Origin.OrIdent)

	reg.mu.Lock()
	// Check for existing active select by a different connection.
	// For SBOw, two clients sharing the same origin key are distinguished
	// by connection pointer. If the existing owner is still connected and it's
	// not the same connection, deny the select.
	if reg.selectConn != nil && reg.selectConn != sc {
		if s.isConnActive(reg.selectConn) && time.Since(reg.selectTime) <= reg.selectTimeout {
			reg.mu.Unlock()
			s.logger.Warn("iec61850: select denied: existing active selection by another connection",
				"ref", req.Ref)
			return &ControlError{
				Ref:       req.Ref,
				Operation: "select",
				AddCause:  AddCauseSelectFailed,
				Wrapped:   ErrSelectFailed,
			}
		}
		// Previous owner disconnected or timed out — allow the new select.
	}
	reg.selectOwner = ownerKey
	reg.selectConn = sc
	reg.selectTime = time.Now()
	reg.selectCtlNum = req.CtlNum
	reg.selectCtlVal = req.CtlVal
	reg.mu.Unlock()

	return nil
}

func (s *Server) executeSelectWithValue(ctx context.Context, reg *controlRegistration, req ControlRequest) error {
	if !reg.ctlModel.IsEnhanced() {
		return &ControlError{
			Ref:       req.Ref,
			Operation: "select",
			Wrapped:   fmt.Errorf("ctlModel %s does not support SBOw (use SBO)", reg.ctlModel),
		}
	}
	return s.executeSelect(ctx, reg, req)
}

func (s *Server) executeCancel(ctx context.Context, reg *controlRegistration, req ControlRequest) error {
	sc := mms.ServerConnFromContext(ctx)
	ownerKey := fmt.Sprintf("%d:%x", req.Origin.OrCat, req.Origin.OrIdent)

	reg.mu.Lock()
	// Enforce ownership: only the connection that holds the selection may cancel it.
	// For SBO normal: conn identity; for SBOw: origin + connection identity.
	if reg.ctlModel == CtlModelSBONormal {
		if reg.selectConn != nil && reg.selectConn != sc {
			reg.mu.Unlock()
			s.logger.Warn("iec61850: cancel denied: not the selection owner (SBO normal)",
				"ref", req.Ref)
			return &ControlError{
				Ref:       req.Ref,
				Operation: "cancel",
				AddCause:  AddCauseSelectFailed,
				Wrapped:   ErrSelectFailed,
			}
		}
	} else if reg.ctlModel.IsEnhanced() {
		if reg.selectOwner != "" && reg.selectOwner != ownerKey {
			reg.mu.Unlock()
			s.logger.Warn("iec61850: cancel denied: origin mismatch (SBOw)",
				"ref", req.Ref, "owner", reg.selectOwner, "cancel", ownerKey)
			return &ControlError{
				Ref:       req.Ref,
				Operation: "cancel",
				AddCause:  AddCauseSelectFailed,
				Wrapped:   ErrSelectFailed,
			}
		}
		if reg.selectConn != nil && sc != nil && reg.selectConn != sc {
			reg.mu.Unlock()
			s.logger.Warn("iec61850: cancel denied: connection mismatch (SBOw)",
				"ref", req.Ref)
			return &ControlError{
				Ref:       req.Ref,
				Operation: "cancel",
				AddCause:  AddCauseSelectFailed,
				Wrapped:   ErrSelectFailed,
			}
		}
	}
	reg.mu.Unlock()

	if reg.handler.OnCancel != nil {
		if err := callControlHandler(reg.handler.OnCancel, ctx, req); err != nil {
			return err
		}
	}

	reg.mu.Lock()
	reg.selectOwner = ""
	reg.selectConn = nil
	reg.selectCtlVal = nil
	reg.mu.Unlock()

	return nil
}

// decodeControlRequest decodes an MMS structure into a ControlRequest.
func decodeControlRequest(ref, operation string, val *mms.Value) (ControlRequest, error) {
	req := ControlRequest{
		Ref:       ref,
		Operation: operation,
	}

	// SBO normal: the client writes a VisibleString (the select reference)
	// to the SBO attribute. There is no structure to decode; the CtlVal
	// carries the raw value for context if needed.
	if operation == "SBO" {
		req.CtlVal = val
		return req, nil
	}

	members, ok := val.Structure()
	if !ok {
		return ControlRequest{}, fmt.Errorf("expected structure, got %s", val.Type())
	}

	switch operation {
	case "Oper", "SBOw":
		req.Operation = "operate"
		if operation == "SBOw" {
			req.Operation = "select"
		}
		if len(members) < 5 {
			return req, fmt.Errorf("oper structure: need >=5 members, got %d", len(members))
		}
		req.CtlVal = members[0]

		// Determine whether operTm (BinaryTime or UTCTime) is present at index 1.
		// If members[1] is a structure → it is origin (no operTm); origin is at index 1.
		// If members[1] is a time value → it is operTm; origin is at index 2.
		originIdx := 1
		if _, isStruct := members[1].Structure(); !isStruct {
			// members[1] is operTm — skip it
			originIdx = 2
		}
		if originIdx < len(members) {
			if originMembers, ok := members[originIdx].Structure(); ok && len(originMembers) >= 2 {
				if cat, ok := originMembers[0].Int32(); ok {
					req.Origin.OrCat = OrCat(cat)
				}
				if ident, ok := originMembers[1].OctetString(); ok {
					req.Origin.OrIdent = ident
				}
			}
		}
		// ctlNum is at originIdx+1 (when present in model)
		ctlNumIdx := originIdx + 1
		if ctlNumIdx < len(members) {
			if n, ok := members[ctlNumIdx].Uint32(); ok {
				req.CtlNum = uint8(n)
			}
		}
		// T (timestamp) is at ctlNumIdx+1 — skip it.
		// Test is at ctlNumIdx+2, Check at ctlNumIdx+3.
		testIdx := ctlNumIdx + 2
		checkIdx := testIdx + 1
		if testIdx < len(members) {
			if b, ok := members[testIdx].Bool(); ok {
				req.Test = b
			}
		}
		if checkIdx < len(members) {
			if bits, ok := members[checkIdx].BitString(); ok && len(bits) > 0 {
				req.Check = CheckConditions(bits[0] >> 6)
			}
		}

	case "Cancel":
		if len(members) < 5 {
			return req, fmt.Errorf("Cancel structure: need >=5 members, got %d", len(members))
		}
		req.CtlVal = members[0]
		// Same operTm-optional logic as Oper: if members[1] is a structure it is
		// origin (no operTm); if it is a time value, operTm is present.
		originIdx := 1
		if _, isStruct := members[1].Structure(); !isStruct {
			originIdx = 2
		}
		if originIdx < len(members) {
			if originMembers, ok := members[originIdx].Structure(); ok && len(originMembers) >= 2 {
				if cat, ok := originMembers[0].Int32(); ok {
					req.Origin.OrCat = OrCat(cat)
				}
				if ident, ok := originMembers[1].OctetString(); ok {
					req.Origin.OrIdent = ident
				}
			}
		}
		ctlNumIdx := originIdx + 1
		if ctlNumIdx < len(members) {
			if n, ok := members[ctlNumIdx].Uint32(); ok {
				req.CtlNum = uint8(n)
			}
		}

	case "SBO":
		// Unreachable: SBO is handled before the Structure check above.
	}

	return req, nil
}

// installSBONormalReadHandler overrides the Read function of the SBO[CO]
// variable for a SBO-normal controllable data object. When a client reads
// SBO[CO], this handler processes the select request: it grants the select
// to the requesting MMS connection if the control is not already held, and
// returns the control reference string. An empty string is returned when the
// select is denied (already selected by another connection).
//
// IEC 61850-7-2 §20.3: for ctlModel=2 (sbo-with-normal-security), the
// client selects by reading the SBO attribute; the server responds with the
// control object reference on success or an empty string on denial.
func (s *Server) installSBONormalReadHandler(ldName, doRef string, reg *controlRegistration) {
	if s.mms == nil {
		return // unit-test stub servers have no underlying MMS server
	}
	// Build the MMS itemID for the SBO leaf: LNName$CO$DOName$SBO
	// doRef is "GGIO1.SPCSO1" → lnName="GGIO1", doPath=["SPCSO1"]
	dot := strings.Index(doRef, ".")
	if dot < 0 {
		s.logger.Warn("iec61850: installSBOReadHandler: invalid doRef", "doRef", doRef)
		return
	}
	lnName := doRef[:dot]
	doPath := strings.ReplaceAll(doRef[dot+1:], ".", "$")
	sboItemID := lnName + "$CO$" + doPath + "$SBO"
	ctlRef := ldName + "/" + doRef

	readFn := func(ctx context.Context) (*mms.Value, error) {
		sc := mms.ServerConnFromContext(ctx)
		reg.mu.Lock()
		defer reg.mu.Unlock()

		now := time.Now()
		// Deny if already selected by a different active connection.
		if reg.selectConn != nil && reg.selectConn != sc {
			if s.isConnActive(reg.selectConn) && now.Sub(reg.selectTime) <= reg.selectTimeout {
				return mms.NewVisibleString(""), nil
			}
			// Previous owner disconnected or timed out — release the selection.
			reg.selectConn = nil
			reg.selectOwner = ""
		}
		// Grant select to this connection.
		reg.selectConn = sc
		reg.selectTime = now
		s.logger.Debug("iec61850: SBO normal select granted", "ref", ctlRef)
		return mms.NewVisibleString(ctlRef), nil
	}

	if err := s.mms.SetVariableRead(ldName, sboItemID, readFn); err != nil {
		s.logger.Warn("iec61850: installSBOReadHandler: variable not found",
			"domain", ldName, "itemID", sboItemID, "err", err)
	}
}

// pathWithoutSuffix returns a new slice with the last element removed
// if it equals suffix. The returned slice never aliases the input.
func pathWithoutSuffix(path []string, suffix string) []string {
	n := len(path)
	if n > 0 && path[n-1] == suffix {
		n--
	}
	out := make([]string, n)
	copy(out, path[:n])
	return out
}

// joinDotPath joins path components with ".".
func joinDotPath(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "."
		}
		result += p
	}
	return result
}

// controlStoreKey derives a ValueStore key for a controllable DO's
// stVal from its control reference (e.g. "LD1/LLN0.SPCSO1" →
// "LD1/LLN0$ST$SPCSO1$stVal"). Returns empty if the format is
// unrecognized.
//
// This is a convenience fallback for simple SPC/DPC-style CDCs where
// the status value is always at ....$ST$...$stVal. For CDCs with
// different status attribute paths, applications should implement
// the stVal update in their [ControlHandler.OnOperate] callback
// instead of relying on this default write.
func (s *Server) controlStoreKey(ref string) string {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	ldName := parts[0]
	doRef := parts[1] // e.g. "LLN0.SPCSO1"
	dotParts := strings.SplitN(doRef, ".", 2)
	if len(dotParts) != 2 {
		return ""
	}
	lnName := dotParts[0]
	doPath := strings.ReplaceAll(dotParts[1], ".", "$")
	itemID := lnName + "$ST$" + doPath + "$stVal"
	return servermodel.StoreKey(ldName, itemID)
}

// isConnActive reports whether conn is still in the set of active MMS connections.
func (s *Server) isConnActive(conn *mms.ServerConn) bool {
	if conn == nil || s.mms == nil {
		return false
	}
	for _, c := range s.mms.Connections() {
		if c == conn {
			return true
		}
	}
	return false
}

// mmsValuesEqual returns true if a and b are both non-nil and encode to
// the same canonical string. Used for ctlVal matching in SBOw.
func mmsValuesEqual(a, b *mms.Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.String() == b.String()
}
