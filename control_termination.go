// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/otfabric/go-mms"
)

// ErrCommandTerminationNegative is wrapped in the [ControlError] that
// [Client.OperateWait] returns when the server ends an accepted operate
// with a negative CommandTermination.
var ErrCommandTerminationNegative = errors.New("iec61850: negative command termination")

// CommandTermination is the outcome a server reports after it executed
// an operate of a control with enhanced security (IEC 61850-8-1,
// CommandTermination): an InformationReport of the Oper attribute,
// preceded by a LastApplError when the operation failed.
type CommandTermination struct {
	// Ref is the controlled object, as passed to [Client.OperateWait].
	Ref string

	// Oper is the Oper value the server reported.
	Oper *mms.Value

	// LastApplError is set for a negative termination.
	LastApplError *LastApplError
}

// Positive reports whether the operation completed successfully.
func (t *CommandTermination) Positive() bool { return t.LastApplError == nil }

// controlKey identifies a control object across its sub-attributes:
// the MMS domain and the item up to the data object, without the
// trailing Oper, SBOw, SBO or Cancel.
func controlKey(domain, item string) string {
	for _, sub := range []string{"$Oper", "$SBOw", "$SBO", "$Cancel"} {
		if strings.HasSuffix(item, sub) {
			item = strings.TrimSuffix(item, sub)
			break
		}
	}
	return domain + "/" + item
}

// controlKeyOf returns the key of a LastApplError's CntrlObj
// ("domain/item"), or "" when it is not of that form.
func controlKeyOf(cntrlObj string) string {
	domain, item, ok := strings.Cut(strings.TrimRight(cntrlObj, "\x00 "), "/")
	if !ok || domain == "" || item == "" {
		return ""
	}
	return controlKey(domain, item)
}

// lastApplErrorWatch collects the LastApplError the server reports for
// one control write in flight.
type lastApplErrorWatch struct {
	key string
	lae *LastApplError // guarded by Client.ctlMu
	c   *Client
}

func (w *lastApplErrorWatch) result() *LastApplError {
	w.c.ctlMu.Lock()
	defer w.c.ctlMu.Unlock()
	return w.lae
}

func (c *Client) watchLastApplError(key string) *lastApplErrorWatch {
	w := &lastApplErrorWatch{key: key, c: c}
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	if c.ctlWrites == nil {
		c.ctlWrites = make(map[*lastApplErrorWatch]struct{})
	}
	c.ctlWrites[w] = struct{}{}
	return w
}

func (c *Client) unwatchLastApplError(w *lastApplErrorWatch) {
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	delete(c.ctlWrites, w)
}

// terminationWaiter waits for the CommandTermination of one operate.
type terminationWaiter struct {
	ref string
	ch  chan *CommandTermination // buffered 1; closed when the client closes
}

func (c *Client) addTerminationWaiter(key, ref string) (*terminationWaiter, error) {
	w := &terminationWaiter{ref: ref, ch: make(chan *CommandTermination, 1)}
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	if c.ctlClosed {
		return nil, ErrClosed
	}
	if c.ctlWaiters == nil {
		c.ctlWaiters = make(map[string][]*terminationWaiter)
	}
	c.ctlWaiters[key] = append(c.ctlWaiters[key], w)
	return w, nil
}

func (c *Client) removeTerminationWaiter(key string, w *terminationWaiter) {
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	ws := c.ctlWaiters[key]
	for i := range ws {
		if ws[i] == w {
			c.ctlWaiters[key] = append(ws[:i:i], ws[i+1:]...)
			break
		}
	}
	if len(c.ctlWaiters[key]) == 0 {
		delete(c.ctlWaiters, key)
	}
}

// closeControlWaiters ends every OperateWait in progress with
// [ErrClosed]; called when the client closes.
func (c *Client) closeControlWaiters() {
	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	c.ctlClosed = true
	for _, ws := range c.ctlWaiters {
		for _, w := range ws {
			close(w.ch)
		}
	}
	c.ctlWaiters = nil
}

// handleControlReport handles the InformationReports of the control
// model (IEC 61850-8-1): a LastApplError alone, reported before the
// negative response to a control write, and a CommandTermination
// (the Oper attribute, preceded by a LastApplError when negative). It
// reports whether the indication was one of them.
func (c *Client) handleControlReport(report *mms.InformationReportIndication) bool {
	if report.ListName != nil || len(report.Variables) == 0 || len(report.Variables) != len(report.Values) {
		return false
	}
	var lae *LastApplError
	sawLAE, operIdx := false, -1
	for i, v := range report.Variables {
		switch {
		case v.Scope == mms.ObjectScopeVMD && v.ItemID == "LastApplError":
			sawLAE = true
			decoded, err := decodeLastApplError(report.Values[i])
			if err != nil {
				c.logger.Warn("iec61850: undecodable LastApplError", "error", err)
				continue
			}
			lae = decoded
		case v.Scope == mms.ObjectScopeDomain && strings.HasSuffix(string(v.ItemID), "$Oper"):
			operIdx = i
		}
	}
	if !sawLAE && operIdx < 0 {
		return false
	}

	c.ctlMu.Lock()
	defer c.ctlMu.Unlock()
	if operIdx < 0 {
		key := controlKeyOf(cntrlObjOf(lae))
		for w := range c.ctlWrites {
			if w.key == key {
				w.lae = lae
			}
		}
		c.logger.Debug("iec61850: LastApplError", "cntrlObj", cntrlObjOf(lae))
		return true
	}

	v := report.Variables[operIdx]
	key := controlKey(string(v.Domain), string(v.ItemID))
	ws := c.ctlWaiters[key]
	if len(ws) == 0 {
		c.logger.Debug("iec61850: CommandTermination without a waiter", "oper", key)
		return true
	}
	w := ws[0]
	c.ctlWaiters[key] = ws[1:]
	if len(c.ctlWaiters[key]) == 0 {
		delete(c.ctlWaiters, key)
	}
	if sawLAE && lae == nil {
		// Negative, but the LastApplError could not be decoded.
		lae = &LastApplError{CntrlObj: key}
	}
	w.ch <- &CommandTermination{Ref: w.ref, Oper: report.Values[operIdx], LastApplError: lae}
	return true
}

func cntrlObjOf(e *LastApplError) string {
	if e == nil {
		return ""
	}
	return e.CntrlObj
}

// OperateWait operates a control with enhanced security
// ([CtlModelDirectEnhanced] or [CtlModelSBOEnhanced], after
// [Client.SelectWithValue]) and waits for its CommandTermination.
//
// A refused operate returns the [ControlError] of [Client.Operate],
// carrying the server's LastApplError when it sent one. A negative
// termination returns the termination and a [ControlError] wrapping
// [ErrCommandTerminationNegative] with the LastApplError's AddCause. The
// wait ends with ctx: servers send no CommandTermination for controls
// with normal security, so do not use OperateWait for them.
func (c *Client) OperateWait(ctx context.Context, ref Ref, params OperateParams) (*CommandTermination, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if err := validateControlRef(ref); err != nil {
		return nil, err
	}
	domainID, itemID, err := c.refToMMS(controlSubRef(ref, "Oper"))
	if err != nil {
		return nil, &ControlError{Ref: ref.String(), Operation: "operate", Wrapped: err}
	}
	c.initReportDispatch()
	key := controlKey(string(domainID), string(itemID))
	w, err := c.addTerminationWaiter(key, ref.String())
	if err != nil {
		return nil, err
	}
	defer c.removeTerminationWaiter(key, w)

	if err := c.Operate(ctx, ref, params); err != nil {
		return nil, err
	}

	select {
	case t, ok := <-w.ch:
		if !ok {
			return nil, ErrClosed
		}
		if !t.Positive() {
			return t, &ControlError{
				Ref:           ref.String(),
				Operation:     "operate",
				AddCause:      t.LastApplError.AddCause,
				LastApplError: t.LastApplError,
				Wrapped:       ErrCommandTerminationNegative,
			}
		}
		c.logger.Debug("iec61850: command termination", "ref", ref.String())
		return t, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("iec61850: operate %s: waiting for CommandTermination: %w", ref.String(), ctx.Err())
	case <-c.mmsClient.Done():
		return nil, fmt.Errorf("iec61850: operate %s: waiting for CommandTermination: %w", ref.String(), ErrClosed)
	}
}
