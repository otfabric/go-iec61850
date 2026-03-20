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

// ControlHandler defines the server-side callbacks for a controllable
// data object. Implement the methods relevant to the control model;
// unset (nil) handlers use default accept/reject behavior.
//
// All handler methods receive a context from the MMS layer and a
// [ControlRequest] describing the command.
type ControlHandler struct {
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
// SBO ownership is tracked by serialized originator identity
// (OrCat:OrIdent), not by connection or session. This means two
// clients sharing the same Origin bytes can interfere with each
// other's SBO state. When the MMS layer exposes connection identity
// in write callbacks, ownership should be bound to
// (connection + origin) for stronger multi-client isolation.
type controlRegistration struct {
	handler  ControlHandler
	ctlModel CtlModel

	mu           sync.Mutex
	selectOwner  string // "orCat:orIdent" — originator that holds the select
	selectTime   time.Time
	selectCtlNum uint8
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
		handler:  handler,
		ctlModel: ctlModel,
	}
	s.controls[key] = reg

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
		ownerKey := fmt.Sprintf("%d:%x", req.Origin.OrCat, req.Origin.OrIdent)
		currentOwner := reg.selectOwner
		selectTime := reg.selectTime
		selectCtlNum := reg.selectCtlNum

		if currentOwner == "" || time.Since(selectTime) > DefaultSelectTimeout {
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
		reg.selectOwner = ""
		reg.mu.Unlock()
	}

	if reg.handler.OnOperate != nil {
		if err := reg.handler.OnOperate(ctx, req); err != nil {
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
		if err := reg.handler.OnSelect(ctx, req); err != nil {
			return err
		}
	}

	reg.mu.Lock()
	reg.selectOwner = fmt.Sprintf("%d:%x", req.Origin.OrCat, req.Origin.OrIdent)
	reg.selectTime = time.Now()
	reg.selectCtlNum = req.CtlNum
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
	if reg.handler.OnCancel != nil {
		if err := reg.handler.OnCancel(ctx, req); err != nil {
			return err
		}
	}

	reg.mu.Lock()
	reg.selectOwner = ""
	reg.mu.Unlock()

	return nil
}

// decodeControlRequest decodes an MMS structure into a ControlRequest.
func decodeControlRequest(ref, operation string, val *mms.Value) (ControlRequest, error) {
	members, ok := val.Structure()
	if !ok {
		return ControlRequest{}, fmt.Errorf("expected structure, got %s", val.Type())
	}

	req := ControlRequest{
		Ref:       ref,
		Operation: operation,
	}

	switch operation {
	case "Oper", "SBOw":
		req.Operation = "operate"
		if operation == "SBOw" {
			req.Operation = "select"
		}
		if len(members) < 7 {
			return req, fmt.Errorf("oper structure: need >=7 members, got %d", len(members))
		}
		req.CtlVal = members[0]
		if originMembers, ok := members[2].Structure(); ok && len(originMembers) >= 2 {
			if cat, ok := originMembers[0].Int32(); ok {
				req.Origin.OrCat = OrCat(cat)
			}
			if ident, ok := originMembers[1].OctetString(); ok {
				req.Origin.OrIdent = ident
			}
		}
		if n, ok := members[3].Uint32(); ok {
			req.CtlNum = uint8(n)
		}
		if b, ok := members[5].Bool(); ok {
			req.Test = b
		}
		if bits, ok := members[6].BitString(); ok && len(bits) > 0 {
			req.Check = CheckConditions(bits[0] >> 6)
		}

	case "Cancel":
		if len(members) < 5 {
			return req, fmt.Errorf("Cancel structure: need >=5 members, got %d", len(members))
		}
		req.CtlVal = members[0]
		if originMembers, ok := members[2].Structure(); ok && len(originMembers) >= 2 {
			if cat, ok := originMembers[0].Int32(); ok {
				req.Origin.OrCat = OrCat(cat)
			}
			if ident, ok := originMembers[1].OctetString(); ok {
				req.Origin.OrIdent = ident
			}
		}
		if n, ok := members[3].Uint32(); ok {
			req.CtlNum = uint8(n)
		}

	case "SBO":
		req.CtlVal = val
	}

	return req, nil
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
