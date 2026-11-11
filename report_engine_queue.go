// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// NotifyValueChanged is called when a data attribute value is changed
// in the ValueStore. It checks every monitoring RCB (enabled URCBs, and
// BRCBs in all states, IEC 61850-7-2 §17.2.3.2.3.6) for relevant
// triggers and records report entries as needed.
func (re *ReportEngine) NotifyValueChanged(_ context.Context, storeKey string) {
	if re.stopped() {
		return
	}
	now := re.clk.Now()

	re.mu.RLock()
	defer re.mu.RUnlock()

	for _, rt := range re.rcbs {
		rt.mu.Lock()
		if rt.monitoringLocked() {
			re.detectChangeLocked(rt, storeKey, now)
		}
		rt.mu.Unlock()
	}
}

// detectChangeLocked evaluates the trigger options of rt for a change of
// storeKey and records an entry when a member qualifies (§17.2.3.2.3.2).
func (re *ReportEngine) detectChangeLocked(rt *rcbRuntime, storeKey string, now time.Time) {
	members := rt.leafMembers[storeKey]
	if len(members) == 0 {
		return
	}

	currentValues := re.readMemberValues(rt)
	inclusion := make([]bool, len(rt.memberKeys))
	reasons := make([]ReasonCode, len(rt.memberKeys))
	included := false
	for _, memberIdx := range members {
		newVal := currentValues[memberIdx]
		prevVal := rt.prevValues[memberIdx]

		reason := re.memberReason(rt, memberIdx, storeKey, prevVal, newVal)
		rt.prevValues[memberIdx] = newVal
		if reason == 0 {
			continue
		}
		inclusion[memberIdx] = true
		reasons[memberIdx] = reason
		included = true
	}
	if !included {
		return
	}
	re.recordEventLocked(rt, inclusion, reasons, currentValues, now)
}

// memberReason returns the reason for inclusion of member idx after a
// write of storeKey, or 0.
func (re *ReportEngine) memberReason(rt *rcbRuntime, idx int, storeKey string, prevVal, newVal *mms.Value) ReasonCode {
	return triggerReason(rt.trgOps, rt.memberNodes[idx], storeKey, prevVal, newVal)
}

// triggerReason evaluates the trigger options trg of a report or log
// control block for a write of storeKey that changed a data set member
// (layout n, may be nil) from prevVal to newVal.
//
// "dchg, qchg, dupd ... refer to the TrgOp of the DataAttribute" in the
// CDC (IEC 61850-7-2 §17.2.2.11, §17.2.3.2.3.2): when the model carries
// the attribute trigger options (from SCL DA dchg/qchg/dupd), the reason
// is that of the written attribute: data-change when a dchg attribute
// changed, quality-change when a qchg attribute changed, data-update
// when a dupd attribute was written. A change of an attribute without a
// TrgOp (such as t) is no internal notification (7-2 Figure 32).
//
// A model without attribute trigger options (built in Go without them)
// falls back to comparing the member value: any change is
// data-change, a change of a Quality is quality-change, and every write
// is data-update.
func triggerReason(trg TrgOps, n *servermodel.MemberNode, storeKey string, prevVal, newVal *mms.Value) ReasonCode {
	if n != nil && n.HasTrgOps() {
		leaf, newLeaf, ok := n.Leaf(newVal, storeKey)
		if !ok {
			return 0
		}
		_, prevLeaf, _ := n.Leaf(prevVal, storeKey)
		changed := !valuesEqual(prevLeaf, newLeaf)
		var reason ReasonCode
		if leaf.Dchg && changed && trg.Has(TrgOpDataChanged) {
			reason |= ReasonDataChanged
		}
		if leaf.Qchg && changed && trg.Has(TrgOpQualityChanged) {
			reason |= ReasonQualityChanged
		}
		if leaf.Dupd && trg.Has(TrgOpDataUpdate) {
			reason |= ReasonDataUpdate
		}
		return reason
	}
	var reason ReasonCode
	if trg.Has(TrgOpDataChanged) && !valuesEqual(prevVal, newVal) {
		reason |= ReasonDataChanged
	}
	if trg.Has(TrgOpQualityChanged) && qualityChanged(prevVal, newVal) {
		reason |= ReasonQualityChanged
	}
	if trg.Has(TrgOpDataUpdate) {
		reason |= ReasonDataUpdate
	}
	return reason
}

// recordEventLocked records a dchg/qchg/dupd notification, combining
// the notifications of one buffer time into one entry
// (IEC 61850-7-2 §17.2.2.9, Figure 26). Caller holds rt.mu.
//
//   - BufTm = 0: every internal notification is its own entry.
//   - The first notification starts the BufTm timer; notifications of
//     other members before it expires join the pending entry, which is
//     queued when the timer expires.
//   - A second notification of a member already in the pending entry
//     queues the pending entry at once, restarts the timer and starts the
//     next entry with the new notification. The standard requires this
//     for FC=ST and allows it for FC=MX (the alternative, replacing the
//     value in the pending entry, is not used), so it applies to every
//     member.
//
// The entry's TimeOfEntry is the time of its first notification
// (§17.2.2.16).
func (re *ReportEngine) recordEventLocked(rt *rcbRuntime, inclusion []bool, reasons []ReasonCode, values []*mms.Value, now time.Time) {
	if rt.bufTm == 0 {
		re.appendEntryLocked(rt, ReportEntry{
			TimeOfEntry: now,
			Values:      values,
			Reasons:     reasons,
			Inclusion:   inclusion,
		})
		return
	}
	if rt.agg != nil {
		for i, in := range inclusion {
			if in && rt.agg.Inclusion[i] {
				re.flushAggLocked(rt)
				break
			}
		}
	}
	if rt.agg == nil {
		n := len(inclusion)
		rt.agg = &ReportEntry{
			TimeOfEntry: now,
			Values:      make([]*mms.Value, n),
			Reasons:     make([]ReasonCode, n),
			Inclusion:   make([]bool, n),
		}
		rt.aggGen++
		gen := rt.aggGen
		rt.aggTimer = re.clk.AfterFunc(time.Duration(rt.bufTm)*time.Millisecond, func() {
			rt.mu.Lock()
			defer rt.mu.Unlock()
			if rt.aggGen == gen && rt.agg != nil && !re.stopped() {
				re.flushAggLocked(rt)
			}
		})
	}
	for i, in := range inclusion {
		if !in {
			continue
		}
		rt.agg.Inclusion[i] = true
		rt.agg.Values[i] = values[i]
		rt.agg.Reasons[i] |= reasons[i]
	}
}

// flushAggLocked queues the pending BufTm entry, if any, as if BufTm had
// expired.
func (re *ReportEngine) flushAggLocked(rt *rcbRuntime) {
	e := rt.agg
	rt.stopAggLocked()
	if e != nil {
		re.appendEntryLocked(rt, *e)
	}
}

// stopAggLocked discards the pending BufTm entry and its timer.
func (rt *rcbRuntime) stopAggLocked() {
	if rt.aggTimer != nil {
		rt.aggTimer.Stop()
		rt.aggTimer = nil
	}
	rt.agg = nil
	rt.aggGen++
}

// queueGILocked queues a general-interrogation entry with the current
// values of all members. A GI that is queued but not yet sent is deleted
// first (§17.2.2.13 Figure 28 "Delete pending GI information"). Pending
// events are queued before it ("Prior events reported").
func (re *ReportEngine) queueGILocked(rt *rcbRuntime) {
	re.flushAggLocked(rt)
	rt.dropPendingGILocked()
	if id := re.appendEntryLocked(rt, re.snapshotEntryLocked(rt, ReasonGI, true)); id != 0 {
		rt.giPending = id
	}
}

// snapshotEntryLocked builds an entry with every member included.
func (re *ReportEngine) snapshotEntryLocked(rt *rcbRuntime, reason ReasonCode, gi bool) ReportEntry {
	n := len(rt.memberKeys)
	inclusion := make([]bool, n)
	reasons := make([]ReasonCode, n)
	for i := range inclusion {
		inclusion[i] = true
		reasons[i] = reason
	}
	return ReportEntry{
		TimeOfEntry: re.clk.Now(),
		Values:      re.readMemberValues(rt),
		Reasons:     reasons,
		Inclusion:   inclusion,
		GI:          gi,
	}
}

// appendEntryLocked assigns the next EntryID to e and appends it to the
// buffer, which discards its oldest entries when it is full
// (IEC 61850-7-2 §17.2.3.2.2.8). It returns the EntryID, or 0 when the
// buffer could not keep e. Caller holds rt.mu.
func (re *ReportEngine) appendEntryLocked(rt *rcbRuntime, e ReportEntry) uint64 {
	rt.entryID++
	e.ID = rt.entryID
	kept := true
	for _, d := range rt.buf.Append(e) {
		if d.ID == e.ID {
			kept = false
		}
		re.discardedLocked(rt, d)
	}

	if rt.buffered && !rt.enabled && !rt.resync {
		re.publishNewestEntryLocked(rt)
	}
	if rt.enabled && rt.sendStop != nil {
		select {
		case rt.wake <- struct{}{}:
		default:
		}
	}
	if !kept {
		return 0
	}
	return e.ID
}

// discardedLocked accounts for an entry the buffer discarded.
func (re *ReportEngine) discardedLocked(rt *rcbRuntime, d ReportEntry) {
	rt.discarded = true
	// Discarding an entry that was still waiting to be sent moves the
	// send pointer, so only the next transmitted entry carries
	// BufOvfl=TRUE. Discarding an entry already transmitted does not
	// (Figure 30).
	if rt.enabled && d.ID > rt.lastSentID && (!d.GI || !rt.giWasSentLocked(d.ID)) {
		rt.pendingOvfl = true
	}
	delete(rt.giSent, d.ID)
	if rt.giPending == d.ID {
		rt.giPending = 0
	}
	re.logger.Debug("iec61850: report buffer full, oldest entry discarded",
		"rcb", rt.rcbItemID, "entry", d.ID)
}

// restartIntegrityLocked (re)starts the integrity timer for the current
// configuration: integrity reports are issued when TrgOps.integrity is
// set and IntgPd > 0 (IEC 61850-7-2 §17.2.3.2.3.3), and a BRCB buffers
// them in all states (§17.2.3.2.3.6).
func (re *ReportEngine) restartIntegrityLocked(rt *rcbRuntime) {
	rt.stopIntegrityLocked()
	if re.stopped() || !rt.monitoringLocked() || rt.intgPd == 0 || !rt.trgOps.Has(TrgOpIntegrity) {
		return
	}
	stop := make(chan struct{})
	rt.intgStop = stop
	period := time.Duration(rt.intgPd) * time.Millisecond
	re.wg.Add(1)
	go re.integrityLoop(rt, period, stop)
}

// stopIntegrityLocked stops the integrity timer goroutine, if any.
func (rt *rcbRuntime) stopIntegrityLocked() {
	if rt.intgStop != nil {
		close(rt.intgStop)
		rt.intgStop = nil
	}
}

// integrityLoop runs the periodic integrity timer of one configuration.
func (re *ReportEngine) integrityLoop(rt *rcbRuntime, period time.Duration, stop chan struct{}) {
	defer re.wg.Done()
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			rt.mu.Lock()
			if rt.intgStop != stop {
				rt.mu.Unlock()
				return
			}
			re.integrityLocked(rt)
			rt.mu.Unlock()
		case <-stop:
			return
		case <-re.stopCh:
			return
		}
	}
}

// integrityLocked handles the expiry of the integrity period: an entry
// with all members (§17.2.3.2.3.3). BufTm has no effect on it
// (§17.2.2.12); the pending BufTm entry is queued first, so the events
// keep their time order (§17.2.3.2.3.5).
func (re *ReportEngine) integrityLocked(rt *rcbRuntime) {
	if !rt.monitoringLocked() {
		return
	}
	re.flushAggLocked(rt)
	re.appendEntryLocked(rt, re.snapshotEntryLocked(rt, ReasonIntegrity, false))
}

// startSenderLocked starts the delivery goroutine of a new enable
// session. Caller holds rt.mu.
func (re *ReportEngine) startSenderLocked(rt *rcbRuntime, conn *mms.ServerConn) {
	stop := make(chan struct{})
	wake := make(chan struct{}, 1)
	rt.sendStop = stop
	rt.wake = wake
	wake <- struct{}{} // deliver what is already buffered
	re.wg.Add(1)
	go re.senderLoop(rt, conn, rt.sendGate, stop, wake)
}

// awaitGate waits until the receiving association has no response
// pending (see respGate), at most gateTimeout. It reports false as soon as
// stop is closed or the engine stops.
func (re *ReportEngine) awaitGate(g *respGate, stop <-chan struct{}) bool {
	idle := g.idleCh()
	select {
	case <-idle:
		return true
	default:
	}
	t := time.NewTimer(gateTimeout)
	defer t.Stop()
	select {
	case <-idle:
		return true
	case <-t.C:
		return true
	case <-stop:
		return false
	case <-re.lifeContext().Done():
		return false
	}
}

// senderLoop formats and sends the entries of one enable session in
// order. It is the only sender of the RCB, so reports reach the client in
// the time sequence order of their entries (IEC 61850-7-2
// §17.2.3.2.3.5) and buffered entries are sent before newer ones
// (§17.2.3.2.3.6).
//
// A report triggered by a request of the receiving association waits
// until that request's response is sent (respGate); other reports are
// sent at once.
func (re *ReportEngine) senderLoop(rt *rcbRuntime, conn *mms.ServerConn, gate *respGate, stop, wake chan struct{}) {
	defer re.wg.Done()
	for {
		select {
		case <-stop:
			return
		case <-re.stopCh:
			return
		case <-wake:
		}
		for {
			if !re.awaitGate(gate, stop) {
				return
			}
			segments, ok := re.nextReport(rt, stop)
			if !ok {
				break
			}
			for _, vals := range segments {
				req := &mms.InformationReportRequest{
					// IEC 61850-8-1 §17.2.1: the variableListName is the
					// VMD-specific "RPT".
					ListName: &mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: "RPT"},
					Values:   vals,
				}
				if err := re.send(conn, req); err != nil {
					if errors.Is(err, mms.ErrServerConnClosed) || !re.isConnActive(conn) {
						re.connLost(conn)
						return
					}
					re.logger.Warn("iec61850: report send failed", "rcb", rt.rcbItemID, "error", err)
					break
				}
			}
		}
	}
}

// nextReport formats the next entry of the session for transmission and
// returns its InformationReport value lists (more than one when
// segmented); ok is false when there is nothing to send.
func (re *ReportEngine) nextReport(rt *rcbRuntime, stop chan struct{}) ([][]*mms.Value, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for {
		if rt.sendStop != stop || !rt.enabled {
			return nil, false
		}
		e, ok := rt.buf.After(rt.lastSentID)
		for ok && e.GI && rt.giWasSentLocked(e.ID) {
			e, ok = rt.buf.After(e.ID)
		}
		if !ok {
			return nil, false
		}
		rt.lastSentID = e.ID
		if e.GI {
			rt.markGISentLocked(e.ID)
		}
		if !rt.buffered {
			// A URCB has no buffer: once formatted the entry is gone.
			rt.buf.Remove(e.ID)
			delete(rt.giSent, e.ID)
		}
		entryID := encodeEntryID(e.ID)

		bufOvfl := false
		if rt.buffered && rt.pendingOvfl {
			bufOvfl = true
			rt.pendingOvfl = false
		}

		p := reportParams{
			rptID:       rt.rptID,
			optFlds:     maskRCBOptFlds(rt.optFlds, rt.buffered),
			seqNum:      rt.seqNum,
			confRev:     rt.confRev,
			datSet:      rt.datSet,
			bufOvfl:     bufOvfl,
			timeOfEntry: e.TimeOfEntry,
			inclusion:   e.Inclusion,
			dataRefs:    rt.memberKeys,
			reasons:     e.Reasons,
			values:      e.Values,
		}
		if rt.buffered {
			p.entryID = entryID
		}
		segments := formatReport(p, re.maxPDU)

		// §17.2.2.10: SqNum increments once the report is formatted and
		// queued; INT8U for a URCB (§17.2.5.2), INT16U for a BRCB, rolling
		// over to 0 (Figure 27).
		mod := uint32(1 << 16)
		if !rt.buffered {
			mod = 1 << 8
		}
		rt.seqNum = (rt.seqNum + 1) % mod
		re.store.Set(rt.sk("SqNum"), mms.NewUnsigned(uint64(rt.seqNum)))
		if rt.buffered {
			// §17.2.2.15: while enabled EntryID is the last one formatted
			// and queued for transmission; §17.2.2.16 its TimeOfEntry.
			re.store.Set(rt.sk("EntryID"), mms.NewOctetString(entryID))
			re.store.Set(rt.sk("TimeOfEntry"), mms.NewBinaryTime(e.TimeOfEntry.UnixMilli()))
		}
		if segments == nil {
			re.logger.Warn("iec61850: report member exceeds the PDU size, report dropped",
				"rcb", rt.rcbItemID, "maxPDU", re.maxPDU)
			continue
		}
		return segments, true
	}
}
