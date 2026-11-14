// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-mms"
)

// DataAccessError codes returned for RCB writes, per the IEC 61850-8-1
// §8.1.3.4.4.2 write rules and Table 23:
//
//   - reserved by another client, or not writable in the current state
//     (RptEna=TRUE): temporarily-unavailable (instance-locked-by-other-client);
//   - read-only attribute: object-access-denied (access-violation);
//   - wrong type or size: type-inconsistent (type-conflict);
//   - bad value (unknown data set, unknown EntryID, RptEna with a NULL
//     DatSet, a non-zero IntgPd below ServerOptions.MinIntegrityPeriod):
//     object-value-invalid (parameter-value-inconsistent);
//   - attribute not present in the RCB: object-non-existent.
const (
	rcbErrLocked         = mms.DataAccessErrorTemporarilyUnavailable
	rcbErrReadOnly       = mms.DataAccessErrorObjectAccessDenied
	rcbErrType           = mms.DataAccessErrorTypeInconsistent
	rcbErrValue          = mms.DataAccessErrorObjectValueInvalid
	rcbErrNonExistentAtt = mms.DataAccessErrorObjectNonExistent
)

// rcbError returns an error carrying the DataAccessError code that go-mms
// sends on the wire.
func rcbError(code mms.DataAccessErrorCode, rcb, format string, args ...any) error {
	return fmt.Errorf("iec61850: RCB %s: %s: %w", rcb, fmt.Sprintf(format, args...), &mms.DataAccessError{Code: code})
}

// binaryTimeZero is the TimeOfEntry exposed while no entry is buffered
// (a local issue per IEC 61850-7-2 §17.2.2.16): the BINARY-TIME epoch.
func binaryTimeZero() *mms.Value {
	return mms.NewBinaryTime(binaryTimeEpoch1984.UnixMilli())
}

// binaryTimeEpoch1984 is the MMS TimeOfDay origin (ISO 9506-2: 1984-01-01).
var binaryTimeEpoch1984 = time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC)

// HandleRCBWrite is called when a client writes to an RCB subfield. It
// validates the write against the IEC 61850-7-2 §17.2 / IEC 61850-8-1
// §17.1 rules and, when accepted, commits the value to the store and
// triggers the runtime behaviour (enable, disable, GI, purge, resync).
// A rejected write leaves the store unchanged and returns an error
// wrapping an [mms.DataAccessError].
//
// The optional conn identifies the MMS association that issued the write;
// it becomes the owner and the delivery target of the RCB. A call without
// conn is a local (server application) action: it is not subject to the
// reservation of another client, and an RCB enabled that way buffers or
// discards its reports but delivers none.
func (re *ReportEngine) HandleRCBWrite(ctx context.Context, ldName, rcbItemID, subfield string, val *mms.Value, conn ...*mms.ServerConn) error {
	key := ldName + "/" + rcbItemID

	re.mu.RLock()
	rt, ok := re.rcbs[key]
	re.mu.RUnlock()
	if !ok {
		return nil
	}

	var sc *mms.ServerConn
	if len(conn) > 0 {
		sc = conn[0]
	}
	if val == nil {
		return rcbError(rcbErrType, rcbItemID, "%s: nil value", subfield)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Ownership: a reserved control block accepts attribute writes from
	// its owner only (IEC 61850-7-2 §17.2.1; URCB §17.2.4.5). An owner
	// whose association is gone no longer holds the reservation.
	if sc != nil && rt.owner != nil && rt.owner != sc {
		if re.isConnActive(rt.owner) {
			return rcbError(rcbErrLocked, rcbItemID, "%s: reserved by another client", subfield)
		}
		re.disableLocked(rt)
		re.releaseLocked(rt)
	}

	switch subfield {
	case "RptEna":
		b, ok := val.Bool()
		if !ok {
			return rcbError(rcbErrType, rcbItemID, "RptEna: expected boolean")
		}
		if b {
			return re.enableLocked(rt, sc, respGateFrom(ctx))
		}
		re.disableLocked(rt)
		// The reservation made implicitly by RptEna=TRUE ends with it
		// (§17.2.4.5); a BRCB without ResvTms is reserved only while
		// enabled (Table 37 c3). An explicit Resv=TRUE stays.
		if rt.buffered || !rt.explicitResv {
			re.releaseLocked(rt)
		}
		re.store.Set(rt.sk("RptEna"), mms.NewBoolean(false))
		return nil

	case "GI":
		return re.writeGILocked(rt, val)

	case "Resv":
		if rt.buffered {
			return rcbError(rcbErrNonExistentAtt, rcbItemID, "Resv not applicable to BRCB")
		}
		b, ok := val.Bool()
		if !ok {
			return rcbError(rcbErrType, rcbItemID, "Resv: expected boolean")
		}
		// IEC 61850-8-1 Table 63: Resv may not be written when RptEna=TRUE.
		if rt.enabled {
			return rcbError(rcbErrLocked, rcbItemID, "Resv: RptEna is TRUE")
		}
		if b {
			rt.owner = sc
			rt.reserved = true
			rt.explicitResv = true
			re.store.Set(rt.sk("Resv"), mms.NewBoolean(true))
		} else {
			re.releaseLocked(rt)
		}
		return nil

	case "PurgeBuf":
		if !rt.buffered {
			return rcbError(rcbErrNonExistentAtt, rcbItemID, "PurgeBuf not applicable to URCB")
		}
		b, ok := val.Bool()
		if !ok {
			return rcbError(rcbErrType, rcbItemID, "PurgeBuf: expected boolean")
		}
		// IEC 61850-8-1 Table 61: may not be written when RptEna=TRUE.
		if rt.enabled {
			return rcbError(rcbErrLocked, rcbItemID, "PurgeBuf: RptEna is TRUE")
		}
		if b {
			re.purgeLocked(rt)
		}
		// §17.2.2.14: "this attribute shall be automatically set to FALSE".
		re.store.Set(rt.sk("PurgeBuf"), mms.NewBoolean(false))
		return nil

	case "EntryID":
		return re.writeEntryIDLocked(rt, val)

	case "RptID", "DatSet", "OptFlds", "BufTm", "TrgOps", "IntgPd":
		// IEC 61850-7-2 Table 37 c1/c2, §17.2.4.4; IEC 61850-8-1 Tables
		// 61 and 63: "May not be written when RptEna=TRUE".
		if rt.enabled {
			return rcbError(rcbErrLocked, rcbItemID, "%s: RptEna is TRUE", subfield)
		}
		return re.writeConfigLocked(rt, subfield, val)

	case "ConfRev", "SqNum", "TimeOfEntry", "Owner":
		// IEC 61850-8-1 Table 61/63 "May not be written"; §8.1.3.4.4.2.
		return rcbError(rcbErrReadOnly, rcbItemID, "%s is read-only", subfield)

	default:
		return rcbError(rcbErrNonExistentAtt, rcbItemID, "unknown attribute %q", subfield)
	}
}

// enableLocked handles RptEna=TRUE; gate is the respGate of sc, if any.
// Caller holds rt.mu.
func (re *ReportEngine) enableLocked(rt *rcbRuntime, sc *mms.ServerConn, gate *respGate) error {
	// §17.2.2.10: a write of RptEna=TRUE that is not a FALSE→TRUE
	// transition changes nothing (SqNum is not reset).
	if rt.enabled {
		return nil
	}
	// IEC 61850-8-1 §17.1.2/§17.1.3: enabling RptEna fails while DatSet
	// is empty; Figure 24: no transition from
	// disabled with a NULL or invalid DatSet.
	if rt.datSet == "" {
		return rcbError(rcbErrValue, rt.rcbItemID, "RptEna: DatSet is NULL")
	}
	if !rt.dsValid {
		if err := re.resolveDataset(rt); err != nil {
			return rcbError(rcbErrValue, rt.rcbItemID, "RptEna: %v", err)
		}
		if rt.buffered {
			rt.prevValues = re.readMemberValues(rt)
		}
	}

	// Reservation: RptEna=TRUE reserves the URCB implicitly (§17.2.4.5)
	// and the BRCB for the enabling client (Table 37 c3).
	if rt.owner == nil {
		rt.owner = sc
	}
	if !rt.buffered && !rt.reserved {
		rt.reserved = true
		re.store.Set(rt.sk("Resv"), mms.NewBoolean(true))
	}

	rt.enabled = true
	rt.enableConn = sc
	// §17.2.2.10 Figure 27: SqNum is set to 0 on the FALSE→TRUE transition.
	rt.seqNum = 0
	re.store.Set(rt.sk("SqNum"), mms.NewUnsigned(0))

	if rt.buffered {
		// §17.2.2.15: from disabled, reporting starts with the oldest
		// entry; from resync, after the entry the client set.
		oldestPred := rt.entryID
		if o, ok := rt.buf.Oldest(); ok {
			oldestPred = o.ID - 1
		}
		switch {
		case rt.resync && rt.hasEntryLocked(rt.resyncID):
			rt.lastSentID = rt.resyncID
			rt.pendingOvfl = false
		case rt.resync:
			// The resync entry was discarded in the meantime.
			rt.lastSentID = oldestPred
			rt.pendingOvfl = true
		default:
			// §17.2.3.2.2.8: BufOvfl=TRUE in the first report after the
			// transition from disabled to enabled when entries may have
			// been lost (some were discarded since start-up or a purge).
			rt.lastSentID = oldestPred
			rt.pendingOvfl = rt.discarded
		}
		rt.resync = false
	} else {
		rt.prevValues = re.readMemberValues(rt)
		rt.buf.Purge()
		rt.giPending, rt.giSent = 0, nil
		rt.lastSentID = rt.entryID
		re.restartIntegrityLocked(rt)
	}
	if rt.buffered && rt.intgStop == nil {
		re.restartIntegrityLocked(rt)
	}

	re.store.Set(rt.sk("RptEna"), mms.NewBoolean(true))

	if sc != nil {
		rt.sendGate = gate
		re.startSenderLocked(rt, sc)
	}

	re.logger.Info("iec61850: RCB enabled",
		"rcb", rt.rcbItemID, "rptID", rt.rptID, "datSet", rt.datSet, "hasConn", sc != nil)
	return nil
}

// writeGILocked handles a GI write (IEC 61850-7-2 §17.2.2.13 Figure 28).
func (re *ReportEngine) writeGILocked(rt *rcbRuntime, val *mms.Value) error {
	b, ok := val.Bool()
	if !ok {
		return rcbError(rcbErrType, rt.rcbItemID, "GI: expected boolean")
	}
	// GI=FALSE: accepted, and a GI already in progress continues.
	if !b {
		re.store.Set(rt.sk("GI"), mms.NewBoolean(false))
		return nil
	}
	// "NO GI ALLOWED": GI.response– and GI set to FALSE.
	if !rt.enabled {
		re.store.Set(rt.sk("GI"), mms.NewBoolean(false))
		return rcbError(rcbErrLocked, rt.rcbItemID, "GI: RptEna is FALSE")
	}
	// After initiation GI is set back to FALSE by the RCB.
	re.store.Set(rt.sk("GI"), mms.NewBoolean(false))
	// With TrgOps.GI FALSE, setting GI to TRUE is accepted but produces no
	// GI report.
	if !rt.trgOps.Has(TrgOpGI) {
		return nil
	}
	re.queueGILocked(rt)
	return nil
}

// writeEntryIDLocked handles an EntryID write (BRCB resync,
// IEC 61850-7-2 §17.2.2.5 Figure 24).
func (re *ReportEngine) writeEntryIDLocked(rt *rcbRuntime, val *mms.Value) error {
	if !rt.buffered {
		return rcbError(rcbErrNonExistentAtt, rt.rcbItemID, "EntryID not applicable to URCB")
	}
	if rt.enabled {
		return rcbError(rcbErrLocked, rt.rcbItemID, "EntryID: RptEna is TRUE")
	}
	bs, ok := val.OctetString()
	// IEC 61850-8-1 §8.1.3.5: EntryID is OCTET STRING (SIZE(8)); a value of
	// another size is type-inconsistent (§8.1.3.4.4.2).
	if !ok || len(bs) != 8 {
		return rcbError(rcbErrType, rt.rcbItemID, "EntryID: expected an 8-octet string")
	}
	id := decodeEntryID(bs)
	if id == 0 {
		// EntryID zero asks for a resync to the oldest queued entry;
		// resync → disabled.
		rt.resync = false
		re.store.Set(rt.sk("EntryID"), mms.NewOctetString(append([]byte(nil), bs...)))
		return nil
	}
	if !rt.hasEntryLocked(id) {
		// An unknown EntryID is answered with parameter-value-
		// inappropriate and the state becomes (stays) disabled.
		rt.resync = false
		return rcbError(rcbErrValue, rt.rcbItemID, "EntryID %d is not in the buffer", id)
	}
	rt.resync = true
	rt.resyncID = id
	re.store.Set(rt.sk("EntryID"), mms.NewOctetString(append([]byte(nil), bs...)))
	return nil
}

// writeConfigLocked handles a write of a configuration attribute while
// the RCB is disabled.
func (re *ReportEngine) writeConfigLocked(rt *rcbRuntime, subfield string, val *mms.Value) error {
	changed := false
	switch subfield {
	case "RptID":
		s, ok := val.VisibleString()
		if !ok {
			return rcbError(rcbErrType, rt.rcbItemID, "RptID: expected visible string")
		}
		changed = s != storedString(re.store.Get(rt.sk("RptID")))
		re.store.Set(rt.sk("RptID"), val)

	case "DatSet":
		s, ok := val.VisibleString()
		if !ok {
			return rcbError(rcbErrType, rt.rcbItemID, "DatSet: expected visible string")
		}
		// IEC 61850-7-2 §17.2.2.6, IEC 61850-8-1 §17.1.2: a DataSet that
		// does not exist fails unless NULL, and the value stays unchanged.
		if s != "" && re.findDataset(rt.ldName, s) == nil {
			return rcbError(rcbErrValue, rt.rcbItemID, "DatSet: data set %q does not exist", s)
		}
		if s != rt.datSet {
			changed = true
			re.store.Set(rt.sk("DatSet"), val)
			// §17.2.2.7: ConfRev counts successful DatSet changes by
			// SetxRCBValues; the value 0 is reserved.
			rt.confRev++
			if rt.confRev == 0 {
				rt.confRev = 1
			}
			re.store.Set(rt.sk("ConfRev"), mms.NewUnsigned(uint64(rt.confRev)))
		}

	case "OptFlds":
		o, err := decodeOptFldsStrict(val)
		if err != nil {
			return rcbError(rcbErrType, rt.rcbItemID, "OptFlds: %v", err)
		}
		re.store.Set(rt.sk("OptFlds"), encodeOptFlds(maskRCBOptFlds(o, rt.buffered)))

	case "BufTm", "IntgPd":
		u, ok := val.Uint32()
		if !ok {
			return rcbError(rcbErrType, rt.rcbItemID, "%s: expected unsigned", subfield)
		}
		// ServerOptions.MinIntegrityPeriod: a non-zero IntgPd below the
		// minimum is a bad value, object-value-invalid
		// (parameter-value-inconsistent, IEC 61850-8-1 Table 23).
		if subfield == "IntgPd" && u != 0 && u < re.minIntgPd {
			return rcbError(rcbErrValue, rt.rcbItemID, "IntgPd %d ms is below the minimum %d ms", u, re.minIntgPd)
		}
		prev, havePrev := storedUint32(re.store.Get(rt.sk(subfield)))
		changed = !havePrev || prev != u
		re.store.Set(rt.sk(subfield), mms.NewUnsigned(uint64(u)))

	case "TrgOps":
		t, err := decodeTrgOpsStrict(val)
		if err != nil {
			return rcbError(rcbErrType, rt.rcbItemID, "TrgOps: %v", err)
		}
		changed = t != rt.trgOps
		re.store.Set(rt.sk("TrgOps"), val)
	}

	re.loadConfigLocked(rt)

	if !changed {
		return nil
	}
	if subfield == "DatSet" {
		if err := re.resolveDataset(rt); err == nil && rt.buffered {
			rt.prevValues = re.readMemberValues(rt)
		}
	}
	// IEC 61850-8-1 §17.1.2: changing RptID, DatSet, BufTm, TrgOps,
	// IntgPd or PurgeBuf purges the buffer (IEC 61850-7-2 Table 37 c1).
	if rt.buffered {
		re.purgeLocked(rt)
	}
	if rt.buffered {
		re.restartIntegrityLocked(rt)
	}
	return nil
}

// maskRCBOptFlds removes the OptFlds bits the server decides itself: a
// URCB never reports BufOvfl and EntryID and reads them back FALSE
// (IEC 61850-7-2 §17.2.5.2; IEC 61850-8-1 Table 63), and the segmentation
// bit is reserved in the RCB and set in a report only when the report is
// segmented (IEC 61850-8-1 Table 62, §17.2.1).
func maskRCBOptFlds(o OptFlds, buffered bool) OptFlds {
	o &^= OptFldSegmentation
	if !buffered {
		o &^= OptFldBufOvfl | OptFldEntryID
	}
	return o
}

// purgeLocked discards all buffered entries, including those not yet
// sent (IEC 61850-7-2 §17.2.2.14). EntryIDs keep counting, so an EntryID
// from before the purge is unknown afterwards. Caller holds rt.mu.
func (re *ReportEngine) purgeLocked(rt *rcbRuntime) {
	rt.buf.Purge()
	rt.giPending, rt.giSent = 0, nil
	rt.lastSentID = rt.entryID
	rt.pendingOvfl = false
	rt.discarded = false
	rt.resync = false
	rt.stopAggLocked()
	re.store.Set(rt.sk("EntryID"), mms.NewOctetString(make([]byte, 8)))
	re.store.Set(rt.sk("TimeOfEntry"), binaryTimeZero())
}

// hasEntryLocked reports whether the entry with the given id is in the
// buffer.
func (rt *rcbRuntime) hasEntryLocked(id uint64) bool {
	_, ok := rt.buf.Get(id)
	return ok
}

// publishNewestEntryLocked exposes the newest buffered entry in EntryID
// and TimeOfEntry, which a disabled BRCB reads back (§17.2.2.15,
// §17.2.2.16).
func (re *ReportEngine) publishNewestEntryLocked(rt *rcbRuntime) {
	e, ok := rt.buf.Newest()
	if !ok {
		re.store.Set(rt.sk("EntryID"), mms.NewOctetString(make([]byte, 8)))
		re.store.Set(rt.sk("TimeOfEntry"), binaryTimeZero())
		return
	}
	re.store.Set(rt.sk("EntryID"), mms.NewOctetString(encodeEntryID(e.ID)))
	re.store.Set(rt.sk("TimeOfEntry"), mms.NewBinaryTime(e.TimeOfEntry.UnixMilli()))
}

// storedString returns the visible-string content of v, "" otherwise.
func storedString(v *mms.Value) string {
	if v == nil {
		return ""
	}
	s, _ := v.VisibleString()
	return s
}

// storedUint32 returns the unsigned content of v.
func storedUint32(v *mms.Value) (uint32, bool) {
	if v == nil {
		return 0, false
	}
	return v.Uint32()
}
