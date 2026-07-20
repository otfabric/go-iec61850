// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/otfabric/go-mms"
)

// ctlNumCounter is a process-wide auto-incrementing counter for
// control sequence numbers. It is used when the caller does not
// provide an explicit CtlNum.
//
// This counter is shared across all clients and sessions in the
// process. It provides best-effort local uniqueness for convenience
// but is NOT a protocol-level or session-scoped guarantee. Under
// long runtimes the uint8 range wraps every 255 increments,
// meaning two operations 255 apart will share the same ctlNum.
//
// For enhanced-security control models (SBO-with-enhanced-security),
// IEC 61850-7-2 requires ctlNum to match between Select and
// Operate. The auto-counter satisfies this for simple single-client
// use but does NOT guarantee uniqueness across multiple clients
// or sessions. Production systems using enhanced security SHOULD
// set [OperateParams].CtlNum explicitly to maintain correct
// select/operate correlation.
var ctlNumCounter uint32

func nextCtlNum() uint8 {
	n := atomic.AddUint32(&ctlNumCounter, 1)
	return uint8(n)
}

// Operate performs a direct-operate control command on the specified
// data object.
//
// The ref identifies the controllable data object (e.g.
// "LD/LN.SPCSO1"). It must include LD and LN with a data-object
// path but must NOT include FC — the library writes to CO/Oper
// automatically.
//
// For SBO control models, use [Client.Select] or
// [Client.SelectWithValue] before calling Operate.
//
// If params.CtlNum is zero, an auto-incrementing sequence number
// is used. If params.Origin is nil, a default Origin with
// [OrCatRemoteControl] is used.
func (c *Client) Operate(ctx context.Context, ref Ref, params OperateParams) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if err := validateControlRef(ref); err != nil {
		return err
	}
	if params.CtlVal == nil {
		return fmt.Errorf("iec61850: operate %s: %w: nil CtlVal", ref.String(), ErrInvalidArgument)
	}
	if params.CtlNum == 0 {
		params.CtlNum = nextCtlNum()
	}

	operRef := controlSubRef(ref, "Oper")
	operVal := buildOper(params)

	if err := c.writeControlValue(ctx, operRef, operVal); err != nil {
		return &ControlError{
			Ref:       ref.String(),
			Operation: "operate",
			Wrapped:   err,
		}
	}

	c.logger.Debug("iec61850: operate", "ref", ref.String(), "ctlNum", params.CtlNum)
	return nil
}

// Select performs a select-before-operate (SBO) select command.
//
// This is used with [CtlModelSBONormal]. The server returns the
// selected object reference on success. The client must then call
// [Client.Operate] within the server's select timeout.
//
// For enhanced security SBO ([CtlModelSBOEnhanced]), use
// [Client.SelectWithValue] instead.
//
// # Interoperability note
//
// The SBO decode path assumes the server returns a VisibleString from
// a normal MMS read on the SBO subattribute. An empty string is
// interpreted as "select denied". This matches IEC 61850-8-1 § 22.2
// for normal-security SBO but behaviour may vary across devices.
// Validate against your target devices before relying on this in
// production.
func (c *Client) Select(ctx context.Context, ref Ref) (string, error) {
	if err := c.checkOpen(); err != nil {
		return "", err
	}
	if err := validateControlRef(ref); err != nil {
		return "", err
	}

	sboRef := controlSubRef(ref, "SBO")
	domainID, itemID, err := c.refToMMS(sboRef)
	if err != nil {
		return "", &ControlError{Ref: ref.String(), Operation: "select", Wrapped: err}
	}

	result, err := c.mmsClient.Read(ctx, mms.ReadRequest{
		DomainID: domainID,
		ItemID:   itemID,
	})
	if err != nil {
		return "", &ControlError{Ref: ref.String(), Operation: "select", Wrapped: err}
	}

	if result == nil || result.Value == nil {
		return "", &ControlError{
			Ref:       ref.String(),
			Operation: "select",
			Wrapped:   fmt.Errorf("empty response from server"),
		}
	}

	selected, ok := result.Value.VisibleString()
	if !ok {
		return "", &ControlError{
			Ref:       ref.String(),
			Operation: "select",
			Wrapped:   fmt.Errorf("unexpected SBO response type: %s", result.Value.Type()),
		}
	}
	if selected == "" {
		return "", &ControlError{
			Ref:       ref.String(),
			Operation: "select",
			Wrapped:   fmt.Errorf("server returned empty SBO string (select denied)"),
		}
	}

	c.logger.Debug("iec61850: select", "ref", ref.String(), "selected", selected)
	return selected, nil
}

// SelectWithValue performs a select-before-operate with enhanced
// security (SBOw) by writing the full Oper structure to the SBOw
// subattribute.
//
// This is used with [CtlModelSBOEnhanced]. On success the select
// is held by the server; the client must then call [Client.Operate]
// with the same parameters within the server's select timeout.
func (c *Client) SelectWithValue(ctx context.Context, ref Ref, params OperateParams) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if err := validateControlRef(ref); err != nil {
		return err
	}
	if params.CtlVal == nil {
		return fmt.Errorf("iec61850: select-with-value %s: %w: nil CtlVal", ref.String(), ErrInvalidArgument)
	}
	if params.CtlNum == 0 {
		params.CtlNum = nextCtlNum()
	}

	sbowRef := controlSubRef(ref, "SBOw")
	operVal := buildOper(params)

	if err := c.writeControlValue(ctx, sbowRef, operVal); err != nil {
		return &ControlError{
			Ref:       ref.String(),
			Operation: "select",
			Wrapped:   err,
		}
	}

	c.logger.Debug("iec61850: select-with-value", "ref", ref.String(), "ctlNum", params.CtlNum)
	return nil
}

// Cancel sends a cancel command for a previously selected control.
//
// The CancelParams should match the CtlVal, Origin, and CtlNum of
// the original Operate or SelectWithValue.
func (c *Client) Cancel(ctx context.Context, ref Ref, params CancelParams) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if err := validateControlRef(ref); err != nil {
		return err
	}
	if params.CtlVal == nil {
		return fmt.Errorf("iec61850: cancel %s: %w: nil CtlVal", ref.String(), ErrInvalidArgument)
	}

	cancelRef := controlSubRef(ref, "Cancel")
	cancelVal := buildCancel(params)

	if err := c.writeControlValue(ctx, cancelRef, cancelVal); err != nil {
		return &ControlError{
			Ref:       ref.String(),
			Operation: "cancel",
			Wrapped:   err,
		}
	}

	c.logger.Debug("iec61850: cancel", "ref", ref.String(), "ctlNum", params.CtlNum)
	return nil
}

// ReadCtlModel reads the ctlModel attribute of a controllable data
// object. This determines which control flow (direct, SBO, enhanced)
// should be used.
//
// The ref identifies the data object (e.g. "LD/LN.SPCSO1") without
// FC — the library reads from CF/ctlModel automatically.
func (c *Client) ReadCtlModel(ctx context.Context, ref Ref) (CtlModel, error) {
	if err := c.checkOpen(); err != nil {
		return 0, err
	}
	if err := validateControlRef(ref); err != nil {
		return 0, err
	}

	ctlModelRef := Ref{
		LD:   ref.LD,
		LN:   ref.LN,
		Path: append(append([]string(nil), ref.Path...), "ctlModel"),
		FC:   FCCF,
	}

	val, err := c.ReadRaw(ctx, ctlModelRef)
	if err != nil {
		return 0, fmt.Errorf("iec61850: read ctlModel %s: %w", ref.String(), err)
	}

	i, ok := val.Int32()
	if !ok {
		return 0, fmt.Errorf("iec61850: read ctlModel %s: unexpected type %s", ref.String(), val.Type())
	}

	return CtlModel(i), nil
}

// ReadLastApplError reads and decodes the LastApplError from the
// logical node containing the given control object.
//
// LastApplError is a structured attribute under the LN at FC=CO
// that provides the reason for the most recent control failure.
// Returns nil (no error value) if the attribute is not present
// or cannot be decoded.
func (c *Client) ReadLastApplError(ctx context.Context, ref Ref) (*LastApplError, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	laeRef := Ref{
		LD:   ref.LD,
		LN:   ref.LN,
		Path: []string{"LastApplError"},
		FC:   FCCO,
	}

	raw, err := c.ReadRaw(ctx, laeRef)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read LastApplError %s: %w", ref.String(), err)
	}

	return decodeLastApplError(raw)
}

// decodeLastApplError decodes an MMS structure into a LastApplError.
// LastApplError ::= SEQUENCE { CntrlObj, Error, Origin, AddCause }
//
// The decode is strict: each member must be present and have the
// expected type. Malformed payloads return an error rather than a
// partially filled struct, because a diagnostic API that silently
// accepts broken responses can make invalid data look valid.
func decodeLastApplError(v *mms.Value) (*LastApplError, error) {
	members, ok := v.Structure()
	if !ok || len(members) < 4 {
		return nil, fmt.Errorf("iec61850: LastApplError: expected structure with >=4 members, got %s", v.Type())
	}

	lae := &LastApplError{}

	s, ok := members[0].VisibleString()
	if !ok {
		return nil, fmt.Errorf("iec61850: LastApplError: CntrlObj: expected VisibleString, got %s", members[0].Type())
	}
	lae.CntrlObj = s

	errVal, ok := members[1].Int32()
	if !ok {
		return nil, fmt.Errorf("iec61850: LastApplError: Error: expected integer, got %s", members[1].Type())
	}
	lae.Error = int(errVal)

	originMembers, ok := members[2].Structure()
	if !ok || len(originMembers) < 2 {
		return nil, fmt.Errorf("iec61850: LastApplError: Origin: expected structure with >=2 members, got %s", members[2].Type())
	}
	cat, ok := originMembers[0].Int32()
	if !ok {
		return nil, fmt.Errorf("iec61850: LastApplError: Origin.OrCat: expected integer, got %s", originMembers[0].Type())
	}
	lae.Origin.OrCat = OrCat(cat)
	ident, ok := originMembers[1].OctetString()
	if !ok {
		return nil, fmt.Errorf("iec61850: LastApplError: Origin.OrIdent: expected OctetString, got %s", originMembers[1].Type())
	}
	lae.Origin.OrIdent = ident

	cause, ok := members[3].Int32()
	if !ok {
		return nil, fmt.Errorf("iec61850: LastApplError: AddCause: expected integer, got %s", members[3].Type())
	}
	lae.AddCause = AddCause(cause)

	return lae, nil
}

// validateControlRef checks that a ref is suitable for control
// operations: must have LD, LN, and at least one path component,
// and must NOT have FC pre-set (the library manages FC).
func validateControlRef(ref Ref) error {
	if ref.LD == "" {
		return fmt.Errorf("iec61850: control: %w: empty logical device", ErrInvalidArgument)
	}
	if ref.LN == "" {
		return fmt.Errorf("iec61850: control: %w: empty logical node", ErrInvalidArgument)
	}
	if len(ref.Path) == 0 {
		return fmt.Errorf("iec61850: control: %w: data object path required (e.g. LD/LN.SPCSO1)", ErrInvalidArgument)
	}
	if ref.FC != "" {
		return fmt.Errorf("iec61850: control: %w: FC must not be set — the library manages CO paths automatically", ErrInvalidArgument)
	}
	return nil
}

// controlSubRef creates a Ref for a control sub-attribute (Oper,
// SBO, SBOw, Cancel) under the given data object.
func controlSubRef(ref Ref, subAttr string) Ref {
	return Ref{
		LD:   ref.LD,
		LN:   ref.LN,
		Path: append(append([]string(nil), ref.Path...), subAttr),
		FC:   FCCO,
	}
}

// writeControlValue writes an MMS structure to a control sub-attribute.
func (c *Client) writeControlValue(ctx context.Context, ref Ref, value *mms.Value) error {
	domainID, itemID, err := c.refToMMS(ref)
	if err != nil {
		return err
	}

	_, err = c.mmsClient.Write(ctx, mms.WriteRequest{
		DomainID: domainID,
		ItemID:   itemID,
		Value:    value,
	})
	return err
}
