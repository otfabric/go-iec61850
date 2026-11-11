// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"slices"
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
	now := time.Now()

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

// recordEventLocked records a dchg/qchg/dupd notification, aggregating
// notifications for BufTm (IEC 61850-7-2 §17.2.2.9). Caller holds rt.mu.
func (re *ReportEngine) recordEventLocked(rt *rcbRuntime, inclusion []bool, reasons []ReasonCode, values []*mms.Value, now time.Time) {
	if rt.bufTm == 0 {
		// "The default value of 0 ... each internal notification shall
		// cause the BRCB to send a single report."
		re.appendEntryLocked(rt, &bufferedEntry{
			timeOfEntry: now,
			values:      values,
			reasons:     reasons,
			inclusion:   inclusion,
		})
		return
	}
	// "In the case where a second internal notification of the same
	// member of a data-set has occurred prior to the expiration of BufTm,
	// the BRCB shall [for ST, may for MX] behave as if BufTm has expired
	// and immediately send the report, restart the timer ... and process
	// the second notification."
	if rt.agg != nil {
		for i, in := range inclusion {
			if in && rt.agg.inclusion[i] {
				re.flushAggLocked(rt)
				break
			}
		}
	}
	if rt.agg == nil {
		n := len(inclusion)
		rt.agg = &bufferedEntry{
			timeOfEntry: now,
			values:      make([]*mms.Value, n),
			reasons:     make([]ReasonCode, n),
			inclusion:   make([]bool, n),
		}
		rt.aggGen++
		gen := rt.aggGen
		// "Upon receipt of the first set of internal notification ... the
		// BRCB shall start a timer of the duration buffer time."
		rt.aggTimer = time.AfterFunc(time.Duration(rt.bufTm)*time.Millisecond, func() {
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
		rt.agg.inclusion[i] = true
		rt.agg.values[i] = values[i]
		rt.agg.reasons[i] |= reasons[i]
	}
}

// flushAggLocked turns the pending BufTm aggregate into an entry.
func (re *ReportEngine) flushAggLocked(rt *rcbRuntime) {
	e := rt.agg
	rt.stopAggLocked()
	if e != nil {
		re.appendEntryLocked(rt, e)
	}
}

// stopAggLocked discards the pending BufTm aggregate and its timer.
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
	rt.bufQueue = slices.DeleteFunc(rt.bufQueue, func(e *bufferedEntry) bool { return e.gi && !e.sent })
	re.appendEntryLocked(rt, re.snapshotEntryLocked(rt, ReasonGI, true))
}

// snapshotEntryLocked builds an entry with every member included.
func (re *ReportEngine) snapshotEntryLocked(rt *rcbRuntime, reason ReasonCode, gi bool) *bufferedEntry {
	n := len(rt.memberKeys)
	inclusion := make([]bool, n)
	reasons := make([]ReasonCode, n)
	for i := range inclusion {
		inclusion[i] = true
		reasons[i] = reason
	}
	return &bufferedEntry{
		timeOfEntry: time.Now(),
		values:      re.readMemberValues(rt),
		reasons:     reasons,
		inclusion:   inclusion,
		gi:          gi,
	}
}

// appendEntryLocked assigns the next EntryID to e and appends it to the
// queue, discarding the oldest entry when the buffer is full
// (IEC 61850-7-2 §17.2.3.2.2.8). Caller holds rt.mu.
func (re *ReportEngine) appendEntryLocked(rt *rcbRuntime, e *bufferedEntry) {
	rt.entryID++
	e.id = rt.entryID
	e.entryID = encodeEntryID(e.id)

	limit := max(rt.bufMax, 1)
	for len(rt.bufQueue) >= limit {
		d := rt.bufQueue[0]
		rt.bufQueue = rt.bufQueue[1:]
		rt.discarded = true
		// "If one of the entries discarded causes the report handler to
		// move the pointer to the next entry for transmission, the
		// implementation shall indicate BufOvfl=TRUE in the next entry
		// that is formatted and transmitted only." Discarding an entry
		// already transmitted does not (Figure 30).
		if rt.enabled && d.id > rt.lastSentID && !d.sent {
			rt.pendingOvfl = true
		}
		re.logger.Debug("iec61850: report buffer full, oldest entry discarded",
			"rcb", rt.rcbItemID, "entry", d.id)
	}
	rt.bufQueue = append(rt.bufQueue, e)

	if rt.buffered && !rt.enabled && !rt.resync {
		re.publishNewestEntryLocked(rt)
	}
	if rt.enabled && rt.sendStop != nil {
		select {
		case rt.wake <- struct{}{}:
		default:
		}
	}
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
			if rt.monitoringLocked() {
				// "BufTm shall have no effect when this change issues a
				// report": pending events go first, then the integrity.
				re.flushAggLocked(rt)
				re.appendEntryLocked(rt, re.snapshotEntryLocked(rt, ReasonIntegrity, false))
			}
			rt.mu.Unlock()
		case <-stop:
			return
		case <-re.stopCh:
			return
		}
	}
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
	go re.senderLoop(rt, conn, stop, wake)
}

// pauseUntil waits for d and reports true, or false as soon as stop is
// closed or the engine stops.
func (re *ReportEngine) pauseUntil(d time.Duration, stop <-chan struct{}) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
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
func (re *ReportEngine) senderLoop(rt *rcbRuntime, conn *mms.ServerConn, stop, wake chan struct{}) {
	defer re.wg.Done()
	for {
		select {
		case <-stop:
			return
		case <-re.stopCh:
			return
		case <-wake:
		}
		if !re.pauseUntil(reportDispatchDelay, stop) {
			return
		}
		for {
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
		i := slices.IndexFunc(rt.bufQueue, func(e *bufferedEntry) bool {
			return e.id > rt.lastSentID && !(e.gi && e.sent)
		})
		if i < 0 {
			return nil, false
		}
		e := rt.bufQueue[i]
		rt.lastSentID = e.id
		if e.gi {
			e.sent = true
		}
		if !rt.buffered {
			// A URCB has no buffer: once formatted the entry is gone.
			rt.bufQueue = rt.bufQueue[i+1:]
		}

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
			timeOfEntry: e.timeOfEntry,
			inclusion:   e.inclusion,
			dataRefs:    rt.memberKeys,
			reasons:     e.reasons,
			values:      e.values,
		}
		if rt.buffered {
			p.entryID = e.entryID
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
			re.store.Set(rt.sk("EntryID"), mms.NewOctetString(append([]byte(nil), e.entryID...)))
			re.store.Set(rt.sk("TimeOfEntry"), mms.NewBinaryTime(e.timeOfEntry.UnixMilli()))
		}
		if segments == nil {
			re.logger.Warn("iec61850: report member exceeds the PDU size, report dropped",
				"rcb", rt.rcbItemID, "maxPDU", re.maxPDU)
			continue
		}
		return segments, true
	}
}
