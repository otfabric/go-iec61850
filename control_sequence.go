// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"strings"

	"github.com/otfabric/go-mms"
)

// Client-side control sequence state: the ctlNum of a SelectWithValue
// reused by the Operate and Cancel of the same sequence, the Oper layout
// of each control and the last LastApplError received per control.

// controlSeqKey identifies a control object (a validated control ref,
// without FC) for the sequence state.
func controlSeqKey(ref Ref) string { return ref.LD + "/" + ref.LN + "." + strings.Join(ref.Path, ".") }

// sequenceCtlNum returns the ctlNum of an Operate: n when given (non-zero
// or set), else that of the control's last SelectWithValue (IEC 61850-7-2
// 20.5.2.4: one control sequence, one ctlNum; libiec61850's client also
// keeps the select's ctlNum for the operate), else the next counter
// value.
func (c *Client) sequenceCtlNum(ref Ref, n uint8, set bool) uint8 {
	if n != 0 || set {
		return n
	}
	if sel, ok := c.selectCtlNum(ref); ok {
		return sel
	}
	return nextCtlNum()
}

func (c *Client) rememberCtlNum(ref Ref, n uint8) {
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	if c.ctlNums == nil {
		c.ctlNums = make(map[string]uint8)
	}
	c.ctlNums[controlSeqKey(ref)] = n
}

func (c *Client) selectCtlNum(ref Ref) (uint8, bool) {
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	n, ok := c.ctlNums[controlSeqKey(ref)]
	return n, ok
}

func (c *Client) forgetCtlNum(ref Ref) {
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	delete(c.ctlNums, controlSeqKey(ref))
}

// operTmLayout is what the client learned of a control structure's type.
type operTmLayout uint8

const (
	operTmUnknown operTmLayout = iota // the type could not be read
	operTmAbsent
	operTmPresent
)

// controlHasOperTm reports whether the control structure sub (Oper, SBOw,
// Cancel) has the operTm member: the object supports TimeActivatedOperate
// (IEC 61850-7-2 20.5.2.8), and every command then carries operTm, zero
// for an immediate one (IEC 61850-8-1 20.10.2.2). The type is read once
// per structure with GetVariableAccessAttributes, as libiec61850's
// client does; when it cannot be read, fallback (operTm requested)
// decides.
func (c *Client) controlHasOperTm(ctx context.Context, sub Ref, fallback bool) bool {
	domainID, itemID, err := c.refToMMS(sub)
	if err != nil {
		return fallback
	}
	key := string(domainID) + "/" + string(itemID)
	c.ctlMu.Lock()
	layout, ok := c.ctlLayouts[key]
	c.ctlMu.Unlock()
	if !ok {
		layout = operTmUnknown
		attrs, err := c.mmsClient.GetVariableAccessAttributes(ctx, mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: domainID, ItemID: itemID})
		switch {
		case err == nil:
			layout = operTmLayoutOf(attrs.TypeSpec)
		case ctx.Err() != nil:
			return fallback // not cached: the next command asks again
		default:
			c.logger.Debug("iec61850: control type not readable; operTm sent only when requested", "ref", key, "error", err)
		}
		c.ctlMu.Lock()
		if c.ctlLayouts == nil {
			c.ctlLayouts = make(map[string]operTmLayout)
		}
		c.ctlLayouts[key] = layout
		c.ctlMu.Unlock()
	}
	switch layout {
	case operTmPresent:
		return true
	case operTmAbsent:
		return false
	}
	return fallback
}

// operTmLayoutOf finds operTm in an Oper/SBOw/Cancel type: by name, or,
// for a type without names, as a UtcTime second member.
func operTmLayoutOf(ts mms.TypeSpec) operTmLayout {
	if ts.Type != mms.ValueTypeStructure || len(ts.Elements) < 2 {
		return operTmUnknown
	}
	for _, e := range ts.Elements {
		if e.Name == "operTm" {
			return operTmPresent
		}
	}
	if ts.Elements[0].Name == "" && ts.Elements[1].Type.Type == mms.ValueTypeUTCTime {
		return operTmPresent
	}
	return operTmAbsent
}

// laeEntry is a LastApplError received, with its arrival order.
type laeEntry struct {
	lae *LastApplError
	seq uint64
}

// recordLastApplErrorLocked keeps the LastApplError for
// [Client.ReadLastApplError]. Must be called with c.ctlMu held.
func (c *Client) recordLastApplErrorLocked(key string, lae *LastApplError) {
	if key == "" || lae == nil {
		return
	}
	if c.ctlLAE == nil {
		c.ctlLAE = make(map[string]laeEntry)
	}
	c.ctlLAESeq++
	c.ctlLAE[key] = laeEntry{lae: lae, seq: c.ctlLAESeq}
}

// cachedLastApplError returns the last LastApplError received for the
// control ref, or for any control of its LN when ref has no data object.
func (c *Client) cachedLastApplError(ref Ref) *LastApplError {
	domainID, itemID, err := c.refToMMS(Ref{LD: ref.LD, LN: ref.LN, Path: ref.Path, FC: FCCO})
	if err != nil {
		return nil
	}
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	if len(ref.Path) > 0 {
		if e, ok := c.ctlLAE[controlKey(string(domainID), string(itemID))]; ok {
			return e.lae
		}
		return nil
	}
	prefix := string(domainID) + "/" + string(itemID) + "$"
	var best laeEntry
	for k, e := range c.ctlLAE {
		if strings.HasPrefix(k, prefix) && e.seq > best.seq {
			best = e
		}
	}
	return best.lae
}
