// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// ReportEngine is the server-side runtime report engine. It monitors
// value changes in the [servermodel.ValueStore] and generates
// IEC 61850 InformationReports to connected clients based on RCB
// configuration (trigger options, optional fields, dataset bindings).
//
// The engine is created by [Server.EnableReports] and runs until the
// server is shut down or [ReportEngine.Stop] is called.
//
// # Interoperability note
//
// The report encoding follows IEC 61850-8-1 field ordering (RptID,
// OptFlds, optional fields by flag order, SubSeqNum, MoreSegments,
// inclusion bitmap, values, reason codes). However, IEC 61850 report
// encoding is unforgiving — small ordering or type mismatches can
// cause decode failures on the receiving side. The encoding has been
// validated in loopback tests with the go-iec61850 client decoder
// but should be verified against real devices or libiec61850 before
// being considered production-ready.
type ReportEngine struct {
	logger *slog.Logger
	store  *servermodel.ValueStore
	mmsSrv *mms.Server
	model  *servermodel.Model

	mu   sync.RWMutex
	rcbs map[string]*rcbRuntime // keyed by "ldName/rcbItemID"

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// rcbRuntime holds the live state for a single report control block.
type rcbRuntime struct {
	mu sync.Mutex

	ldName    string
	lnName    string
	rcbItemID string
	buffered  bool

	// Configuration (read from store on enable).
	rptID   string
	datSet  string
	confRev uint32
	optFlds OptFlds
	trgOps  TrgOps
	bufTm   uint32
	intgPd  uint32

	// Runtime state.
	enabled     bool
	reserved    bool // URCB only
	reserveConn *mms.ServerConn
	enableConn  *mms.ServerConn // connection that enabled this RCB (URCB delivery target)
	seqNum      uint32
	bufOvfl     bool   // set when a buffered entry was dropped due to overflow
	entryID     uint64 // BRCB: auto-incrementing entry ID

	// Dataset member resolution cache.
	memberKeys []string         // store keys for dataset members
	memberVars []mms.ObjectName // MMS variable names for report encoding

	// Previous values for change detection.
	prevValues []*mms.Value

	// Integrity timer.
	intgTicker *time.Ticker
	intgStop   chan struct{}

	// Buffered report queue (BRCB only).
	bufQueue []*bufferedEntry
	bufMax   int
}

// bufferedEntry holds a single buffered report waiting for delivery.
type bufferedEntry struct {
	entryID     []byte
	timeOfEntry time.Time
	values      []*mms.Value
	reasons     []ReasonCode
	inclusion   []bool
}

// EnableReports creates and starts the [ReportEngine] for this server.
// It scans all RCBs in the model, creates runtime state for each, and
// begins monitoring for GI/integrity triggers.
//
// Call this after [NewServer] and before [Server.Serve] or
// [Server.ListenAndServe].
func (s *Server) EnableReports() *ReportEngine {
	engine := &ReportEngine{
		logger: s.logger,
		store:  s.store,
		mmsSrv: s.mms,
		model:  s.model,
		rcbs:   make(map[string]*rcbRuntime),
		stopCh: make(chan struct{}),
	}

	for i := range s.model.LogicalDevices {
		ld := &s.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.Reports {
				rpt := &ln.Reports[k]
				engine.registerRCBRuntime(ld.Name, ln.Name, rpt)
			}
		}
	}

	s.reportEngine = engine
	s.installWriteInterceptor()
	s.logger.Info("iec61850: report engine enabled", "rcbs", len(engine.rcbs))
	return engine
}

// Stop shuts down the report engine, stopping all integrity timers
// and disabling all RCBs.
func (re *ReportEngine) Stop() {
	re.stopOnce.Do(func() {
		close(re.stopCh)
		re.mu.RLock()
		for _, rt := range re.rcbs {
			rt.disable()
		}
		re.mu.RUnlock()
		re.wg.Wait()
	})
}

// isConnActive reports whether conn is still registered with the MMS server.
// A nil conn is never considered active.
func (re *ReportEngine) isConnActive(conn *mms.ServerConn) bool {
	if conn == nil || re.mmsSrv == nil {
		return false
	}
	for _, c := range re.mmsSrv.Connections() {
		if c == conn {
			return true
		}
	}
	return false
}

func (re *ReportEngine) registerRCBRuntime(ldName, lnName string, rpt *servermodel.ReportDef) {
	prefix := "RP"
	if rpt.Buffered {
		prefix = "BR"
	}
	rcbItemID := lnName + "$" + prefix + "$" + rpt.Name
	key := ldName + "/" + rcbItemID

	rt := &rcbRuntime{
		ldName:    ldName,
		lnName:    lnName,
		rcbItemID: rcbItemID,
		buffered:  rpt.Buffered,
		bufMax:    1000, // default buffer size for BRCB
	}

	re.rcbs[key] = rt
}

// HandleRCBWrite is called when a client writes to an RCB subfield.
// It intercepts RptEna, GI, Resv, and PurgeBuf writes to trigger
// the appropriate runtime behavior. The optional conn parameter
// identifies the MMS connection that issued the write, used for
// URCB connection-scoped delivery.
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

	switch subfield {
	case "RptEna":
		b, ok := val.Bool()
		if !ok {
			return fmt.Errorf("iec61850: RptEna: expected boolean")
		}
		if b {
			// For URCB: enforce that only the reservation owner can enable.
			if !rt.buffered {
				rt.mu.Lock()
				reserved := rt.reserved
				reserveConn := rt.reserveConn
				rt.mu.Unlock()
				if reserved && reserveConn != nil && reserveConn != sc {
					return &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
				}
			}
			return re.enableRCB(ctx, rt, sc)
		}
		rt.disable()
		// BRCB: clear the server-written EntryID from the store so that a
		// subsequent re-enable without a client-written EntryID results in a
		// full replay rather than accidentally filtering entries by the stale
		// value last written when the entry was buffered.
		if rt.buffered {
			skFn := func(suffix string) string {
				return servermodel.StoreKey(rt.ldName, rt.rcbItemID+"$"+suffix)
			}
			re.store.Set(skFn("EntryID"), mms.NewOctetString(make([]byte, 8)))
		}
		return nil

	case "GI":
		b, ok := val.Bool()
		if !ok {
			return fmt.Errorf("iec61850: GI: expected boolean")
		}
		if b {
			re.triggerGI(ctx, rt)
		}
		return nil

	case "Resv":
		if rt.buffered {
			return fmt.Errorf("iec61850: Resv not applicable to BRCB")
		}
		b, ok := val.Bool()
		if !ok {
			return fmt.Errorf("iec61850: Resv: expected boolean")
		}
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if b {
			// Reject if already reserved by a different active connection.
			if rt.reserved && rt.reserveConn != nil && rt.reserveConn != sc {
				if re.isConnActive(rt.reserveConn) {
					return &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
				}
				// Previous owner disconnected — take over the reservation.
			}
			rt.reserved = true
			rt.reserveConn = sc
		} else {
			// Only the reservation owner may clear it.
			if rt.reserved && rt.reserveConn != nil && sc != nil && rt.reserveConn != sc {
				return &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
			}
			rt.reserved = false
			rt.reserveConn = nil
		}
		return nil

	case "PurgeBuf":
		if !rt.buffered {
			return nil
		}
		b, ok := val.Bool()
		if !ok {
			return nil
		}
		if b {
			rt.mu.Lock()
			rt.bufQueue = nil
			rt.mu.Unlock()
		}
		return nil

	default:
		re.logger.Debug("iec61850: RCB write to unhandled subfield",
			"rcb", key, "subfield", subfield)
		return nil
	}
}

// enableRCB reads current RCB configuration from the store, resolves
// the dataset, and starts integrity/change monitoring. The optional
// conn is stored as the delivery target for URCBs.
func (re *ReportEngine) enableRCB(_ context.Context, rt *rcbRuntime, conn *mms.ServerConn) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.enabled {
		return nil
	}

	sk := func(suffix string) string {
		return servermodel.StoreKey(rt.ldName, rt.rcbItemID+"$"+suffix)
	}

	if v := re.store.Get(sk("RptID")); v != nil {
		if s, ok := v.VisibleString(); ok {
			rt.rptID = s
		}
	}
	if rt.rptID == "" {
		rt.rptID = rt.ldName + "/" + rt.rcbItemID
	}

	if v := re.store.Get(sk("DatSet")); v != nil {
		if s, ok := v.VisibleString(); ok {
			rt.datSet = s
		}
	}

	if v := re.store.Get(sk("ConfRev")); v != nil {
		if u, ok := v.Uint32(); ok {
			rt.confRev = u
		}
	}

	if v := re.store.Get(sk("OptFlds")); v != nil {
		rt.optFlds = decodeOptFlds(v)
	}

	if v := re.store.Get(sk("TrgOps")); v != nil {
		rt.trgOps = decodeTrgOps(v)
	}

	if v := re.store.Get(sk("BufTm")); v != nil {
		if u, ok := v.Uint32(); ok {
			rt.bufTm = u
		}
	}

	if v := re.store.Get(sk("IntgPd")); v != nil {
		if u, ok := v.Uint32(); ok {
			rt.intgPd = u
		}
	}

	if err := re.resolveDataset(rt); err != nil {
		return fmt.Errorf("iec61850: enable RCB %s: %w", rt.rcbItemID, err)
	}

	rt.prevValues = re.readMemberValues(rt)
	rt.enabled = true
	rt.enableConn = conn
	rt.bufOvfl = false

	re.logger.Debug("iec61850: enableRCB", "rcb", rt.rcbItemID, "hasConn", conn != nil,
		"datSet", rt.datSet, "memberKeys", rt.memberKeys)

	// Invariant: store.Set calls here bypass the write interceptor.
	// They go directly to ValueStore.Set, not through MMS write
	// callbacks. This is safe and intentional — do not route these
	// through the interceptor path or recursion will occur.
	re.store.Set(sk("RptEna"), mms.NewBoolean(true))

	// BRCB: replay buffered entries to the enabling connection so the
	// client receives entries it missed while disconnected.
	if rt.buffered && conn != nil && len(rt.bufQueue) > 0 {
		// Read the resume EntryID that was (optionally) written by the client
		// before enabling. If non-zero, only entries after that point are sent.
		var resumeID uint64
		if v := re.store.Get(sk("EntryID")); v != nil {
			if bs, ok := v.OctetString(); ok {
				resumeID = decodeEntryID(bs)
			}
		}
		entries := filterEntriesAfter(rt.bufQueue, resumeID)
		if len(entries) == 0 {
			// Nothing to replay; skip starting the goroutine.
			goto startIntegrity
		}
		replayEntries := make([]*bufferedEntry, len(entries))
		copy(replayEntries, entries)
		rptID := rt.rptID
		optFlds := rt.optFlds
		confRev := rt.confRev
		datSet := rt.datSet
		rcbItemID := rt.rcbItemID
		re.wg.Add(1)
		go func() {
			defer re.wg.Done()
			re.replayBRCBEntries(rcbItemID, rptID, optFlds, confRev, datSet, replayEntries, conn)
		}()
	}

startIntegrity:

	// Start integrity timer if configured.
	if rt.intgPd > 0 && rt.trgOps.Has(TrgOpIntegrity) {
		rt.intgStop = make(chan struct{})
		rt.intgTicker = time.NewTicker(time.Duration(rt.intgPd) * time.Millisecond)
		tickC := rt.intgTicker.C
		stopC := rt.intgStop
		re.wg.Add(1)
		go re.integrityLoop(rt, tickC, stopC)
	}

	re.logger.Info("iec61850: RCB enabled",
		"rcb", rt.rcbItemID, "rptID", rt.rptID, "datSet", rt.datSet)

	return nil
}

// SetRCBBufMax sets the maximum buffer capacity for the named BRCB. Returns
// false if the RCB is not found. Intended for testing; use with caution in
// production code.
func (re *ReportEngine) SetRCBBufMax(ldName, rcbItemID string, max int) bool {
	key := ldName + "/" + rcbItemID
	re.mu.RLock()
	rt, ok := re.rcbs[key]
	re.mu.RUnlock()
	if !ok {
		return false
	}
	rt.mu.Lock()
	rt.bufMax = max
	rt.mu.Unlock()
	return true
}

// replayBRCBEntries delivers buffered BRCB entries to the given connection.
// It is run in a background goroutine and stops early if delivery fails.
func (re *ReportEngine) replayBRCBEntries(
	rcbItemID, rptID string,
	optFlds OptFlds, confRev uint32, datSet string,
	entries []*bufferedEntry,
	conn *mms.ServerConn,
) {
	re.logger.Debug("iec61850: replaying BRCB entries",
		"rcb", rcbItemID, "count", len(entries))

	// Small delay so the client's RptEna write response arrives before the
	// replayed reports, giving the client time to register its subscription
	// channel before the first replayed report is delivered.
	time.Sleep(150 * time.Millisecond)

	for i, entry := range entries {
		reportValues := encodeReportValuesEx(reportParams{
			rptID:     rptID,
			optFlds:   optFlds,
			seqNum:    uint32(i + 1),
			confRev:   confRev,
			datSet:    datSet,
			entryID:   entry.entryID,
			inclusion: entry.inclusion,
			reasons:   entry.reasons,
			values:    entry.values,
		})

		req := &mms.InformationReportRequest{
			ListName: &mms.ObjectName{
				Scope:  mms.ObjectScopeVMD,
				ItemID: "RPT",
			},
			Values: reportValues,
		}

		if err := conn.SendInformationReport(context.Background(), req); err != nil {
			re.logger.Debug("iec61850: BRCB replay send failed",
				"rcb", rcbItemID, "entry", i, "error", err)
			return
		}
	}

	re.logger.Debug("iec61850: BRCB replay complete", "rcb", rcbItemID, "entries", len(entries))
}

// disable stops the RCB and clears runtime state.
func (rt *rcbRuntime) disable() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if !rt.enabled {
		return
	}
	rt.enabled = false

	if rt.intgTicker != nil {
		rt.intgTicker.Stop()
		close(rt.intgStop)
		rt.intgTicker = nil
	}
}

// resolveDataset maps the DatSet string to ValueStore keys and MMS
// ObjectNames for each member.
func (re *ReportEngine) resolveDataset(rt *rcbRuntime) error {
	if rt.datSet == "" {
		return fmt.Errorf("empty DatSet")
	}

	var ds *servermodel.DataSetDef
	for i := range re.model.LogicalDevices {
		ld := &re.model.LogicalDevices[i]
		if ld.Name != rt.ldName {
			continue
		}
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.DataSets {
				dsRef := rt.ldName + "/" + ln.Name + "$" + ln.DataSets[k].Name
				if dsRef == rt.datSet || ln.DataSets[k].Name == rt.datSet {
					ds = &ln.DataSets[k]
					break
				}
			}
			if ds != nil {
				break
			}
		}
		break
	}

	if ds == nil {
		return fmt.Errorf("dataset %q not found", rt.datSet)
	}

	rt.memberKeys = make([]string, len(ds.Members))
	rt.memberVars = make([]mms.ObjectName, len(ds.Members))

	for i, m := range ds.Members {
		ldInst := m.LDInst
		if ldInst == "" {
			ldInst = rt.ldName
		}
		doPath := strings.ReplaceAll(m.DOPath, ".", "$")
		itemID := m.LNName + "$" + m.FC + "$" + doPath
		rt.memberKeys[i] = servermodel.StoreKey(ldInst, itemID)
		rt.memberVars[i] = mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldInst),
			ItemID: mms.ItemID(itemID),
		}
	}

	return nil
}

// readMemberValues reads current values for all dataset members.
func (re *ReportEngine) readMemberValues(rt *rcbRuntime) []*mms.Value {
	values := make([]*mms.Value, len(rt.memberKeys))
	for i, key := range rt.memberKeys {
		values[i] = re.store.Get(key)
	}
	return values
}

// NotifyValueChanged is called when a data attribute value is changed
// in the ValueStore. It checks all enabled RCBs for relevant triggers
// and generates reports as needed.
func (re *ReportEngine) NotifyValueChanged(ctx context.Context, storeKey string) {
	select {
	case <-re.stopCh:
		return
	default:
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	re.logger.Debug("iec61850: NotifyValueChanged", "storeKey", storeKey, "rcbs", len(re.rcbs))

	for _, rt := range re.rcbs {
		rt.mu.Lock()
		if !rt.enabled {
			rt.mu.Unlock()
			continue
		}

		memberIdx := -1
		for i, k := range rt.memberKeys {
			if k == storeKey {
				memberIdx = i
				break
			}
		}
		if memberIdx < 0 {
			rt.mu.Unlock()
			continue
		}

		newVal := re.store.Get(storeKey)
		prevVal := rt.prevValues[memberIdx]

		var reason ReasonCode
		if rt.trgOps.Has(TrgOpDataChanged) && !valuesEqual(prevVal, newVal) {
			reason |= ReasonDataChanged
		}
		if rt.trgOps.Has(TrgOpQualityChanged) && qualityChanged(prevVal, newVal) {
			reason |= ReasonQualityChanged
		}
		if rt.trgOps.Has(TrgOpDataUpdate) {
			reason |= ReasonDataUpdate
		}

		if reason == 0 {
			rt.mu.Unlock()
			continue
		}

		rt.prevValues[memberIdx] = newVal

		inclusion := make([]bool, len(rt.memberKeys))
		reasons := make([]ReasonCode, len(rt.memberKeys))
		inclusion[memberIdx] = true
		reasons[memberIdx] = reason

		currentValues := re.readMemberValues(rt)
		rt.seqNum++
		seqNum := rt.seqNum

		rt.mu.Unlock()

		re.sendReport(ctx, rt, seqNum, inclusion, reasons, currentValues)
	}
}

// triggerGI generates a General Interrogation report for the given RCB.
func (re *ReportEngine) triggerGI(ctx context.Context, rt *rcbRuntime) {
	rt.mu.Lock()
	if !rt.enabled || !rt.trgOps.Has(TrgOpGI) {
		rt.mu.Unlock()
		return
	}

	inclusion := make([]bool, len(rt.memberKeys))
	reasons := make([]ReasonCode, len(rt.memberKeys))
	for i := range inclusion {
		inclusion[i] = true
		reasons[i] = ReasonGI
	}

	currentValues := re.readMemberValues(rt)
	rt.seqNum++
	seqNum := rt.seqNum

	rt.mu.Unlock()

	re.sendReport(ctx, rt, seqNum, inclusion, reasons, currentValues)

	// Reset GI flag.
	sk := servermodel.StoreKey(rt.ldName, rt.rcbItemID+"$GI")
	re.store.Set(sk, mms.NewBoolean(false))
}

// integrityLoop runs the periodic integrity report timer.
// tickC and stopC are captured from the rcbRuntime at goroutine start
// to avoid data races with disable().
func (re *ReportEngine) integrityLoop(rt *rcbRuntime, tickC <-chan time.Time, stopC <-chan struct{}) {
	defer re.wg.Done()

	for {
		select {
		case <-tickC:
			rt.mu.Lock()
			if !rt.enabled {
				rt.mu.Unlock()
				return
			}

			inclusion := make([]bool, len(rt.memberKeys))
			reasons := make([]ReasonCode, len(rt.memberKeys))
			for i := range inclusion {
				inclusion[i] = true
				reasons[i] = ReasonIntegrity
			}

			currentValues := re.readMemberValues(rt)
			rt.seqNum++
			seqNum := rt.seqNum
			rt.mu.Unlock()

			re.sendReport(context.Background(), rt, seqNum, inclusion, reasons, currentValues)

		case <-stopC:
			return
		case <-re.stopCh:
			return
		}
	}
}

// sendReport builds and delivers an IEC 61850 InformationReport.
// For URCBs, the report is sent only to the connection that enabled
// the RCB. For BRCBs, the report is broadcast to all connections.
func (re *ReportEngine) sendReport(ctx context.Context, rt *rcbRuntime, seqNum uint32, inclusion []bool, reasons []ReasonCode, values []*mms.Value) {
	rt.mu.Lock()
	rptID := rt.rptID
	optFlds := rt.optFlds
	confRev := rt.confRev
	datSet := rt.datSet
	buffered := rt.buffered
	enableConn := rt.enableConn
	bufOvfl := rt.bufOvfl
	rt.mu.Unlock()

	var entryIDBytes []byte

	if buffered {
		rt.mu.Lock()
		rt.entryID++
		entryIDBytes = encodeEntryID(rt.entryID)
		entry := &bufferedEntry{
			entryID:     entryIDBytes,
			timeOfEntry: time.Now(),
			values:      values,
			reasons:     reasons,
			inclusion:   inclusion,
		}
		if len(rt.bufQueue) >= rt.bufMax {
			rt.bufQueue = rt.bufQueue[1:]
			rt.bufOvfl = true
			bufOvfl = true
			re.logger.Debug("iec61850: BRCB buffer overflow, oldest entry dropped",
				"rcb", rt.rcbItemID)
		}
		rt.bufQueue = append(rt.bufQueue, entry)

		sk := func(suffix string) string {
			return servermodel.StoreKey(rt.ldName, rt.rcbItemID+"$"+suffix)
		}
		re.store.Set(sk("EntryID"), mms.NewOctetString(entryIDBytes))
		re.store.Set(sk("TimeOfEntry"), mms.NewBinaryTime(entry.timeOfEntry.UnixMilli()))
		re.store.Set(sk("SqNum"), mms.NewUnsigned(uint64(seqNum)))
		rt.mu.Unlock()
	} else {
		sk := servermodel.StoreKey(rt.ldName, rt.rcbItemID+"$SqNum")
		re.store.Set(sk, mms.NewUnsigned(uint64(seqNum)))
	}

	reportValues := encodeReportValuesEx(reportParams{
		rptID:     rptID,
		optFlds:   optFlds,
		seqNum:    seqNum,
		confRev:   confRev,
		datSet:    datSet,
		bufOvfl:   bufOvfl,
		entryID:   entryIDBytes,
		inclusion: inclusion,
		reasons:   reasons,
		values:    values,
	})

	req := &mms.InformationReportRequest{
		// IEC 61850-8-1 / libiec61850 compatibility: IEC 61850 reports must
		// use VMD-specific variableListName "RPT" (not domain-specific).
		// libiec61850 client ignores domain-specific InformationReports
		// and only processes VMD-specific ones with name "RPT".
		// The go-iec61850 client matches by RptID in the values list,
		// so both clients receive correctly regardless of the list name.
		ListName: &mms.ObjectName{
			Scope:  mms.ObjectScopeVMD,
			ItemID: "RPT",
		},
		Values: reportValues,
	}

	// Connection-aware delivery: URCBs send only to the enabling
	// connection; BRCBs broadcast to all.
	//
	// Delivery is done in a background goroutine so the report is sent
	// AFTER the current confirmed-service response (e.g. the Write response
	// that triggered dchg). Sending the UnconfirmedPDU inside the Write
	// handler — before the Write ConfirmedResponse — causes some clients
	// (e.g. libiec61850) to receive the report while still waiting for the
	// write confirmation, which can cause the report to be discarded.
	if !buffered && enableConn != nil {
		re.wg.Add(1)
		go func() {
			defer re.wg.Done()
			// Small yield to let the current confirmed response be sent first.
			time.Sleep(100 * time.Millisecond)
			re.logger.Info("iec61850: sending URCB report", "rcb", rt.rcbItemID)

			if err := enableConn.SendInformationReport(context.Background(), req); err != nil {
				re.logger.Warn("iec61850: URCB report send to enableConn failed",
					"rcb", rt.rcbItemID, "error", err)
				fmt.Fprintf(os.Stderr, "[DBG] send failed: %v\n", err)
			} else {
				re.logger.Info("iec61850: URCB report sent", "rcb", rt.rcbItemID)
				fmt.Fprintf(os.Stderr, "[DBG] send OK nValues=%d\n", len(req.Values))
			}
		}()
		return
	}

	conns := re.mmsSrv.Connections()
	for _, conn := range conns {
		c := conn
		re.wg.Add(1)
		go func() {
			defer re.wg.Done()
			time.Sleep(100 * time.Millisecond)
			if err := c.SendInformationReport(context.Background(), req); err != nil {
				re.logger.Debug("iec61850: report send failed",
					"rcb", rt.rcbItemID, "error", err)
			}
		}()
	}
}

// reportParams holds all parameters needed to encode a report.
type reportParams struct {
	rptID     string
	optFlds   OptFlds
	seqNum    uint32
	confRev   uint32
	datSet    string
	bufOvfl   bool
	entryID   []byte // nil for URCB
	inclusion []bool
	reasons   []ReasonCode
	values    []*mms.Value
}

// encodeReportValues builds the flat MMS value list for an IEC 61850
// InformationReport following the standard field order:
//
//	RptID, OptFlds, [SeqNum], [TimeStamp], [DatSet], [BufOvfl],
//	[EntryID], [ConfRev], SubSeqNum, MoreSegments, Inclusion,
//	[DataRef...], Values..., [ReasonCode...]
func encodeReportValues(
	rptID string,
	optFlds OptFlds,
	seqNum, confRev uint32,
	datSet string,
	inclusion []bool,
	reasons []ReasonCode,
	values []*mms.Value,
) []*mms.Value {
	return encodeReportValuesEx(reportParams{
		rptID:     rptID,
		optFlds:   optFlds,
		seqNum:    seqNum,
		confRev:   confRev,
		datSet:    datSet,
		inclusion: inclusion,
		reasons:   reasons,
		values:    values,
	})
}

func encodeReportValuesEx(p reportParams) []*mms.Value {
	var result []*mms.Value

	result = append(result, mms.NewVisibleString(p.rptID))
	result = append(result, encodeOptFlds(p.optFlds))

	if p.optFlds.Has(OptFldSeqNum) {
		result = append(result, mms.NewUnsigned(uint64(p.seqNum)))
	}

	if p.optFlds.Has(OptFldTimeStamp) {
		result = append(result, mms.NewBinaryTime(time.Now().UTC().UnixMilli()))
	}

	if p.optFlds.Has(OptFldDataSet) {
		result = append(result, mms.NewVisibleString(p.datSet))
	}

	if p.optFlds.Has(OptFldBufOvfl) {
		result = append(result, mms.NewBoolean(p.bufOvfl))
	}

	if p.optFlds.Has(OptFldEntryID) {
		eid := p.entryID
		if eid == nil {
			eid = make([]byte, 8)
		}
		result = append(result, mms.NewOctetString(eid))
	}

	if p.optFlds.Has(OptFldConfRev) {
		result = append(result, mms.NewUnsigned(uint64(p.confRev)))
	}

	// SubSeqNum + MoreSegments (only present when segmentation is enabled).
	if p.optFlds.Has(OptFldSegmentation) {
		result = append(result, mms.NewUnsigned(0))
		result = append(result, mms.NewBoolean(false))
	}

	inclusionBits := encodeInclusion(p.inclusion)
	result = append(result, inclusionBits)

	for i, incl := range p.inclusion {
		if incl && i < len(p.values) {
			v := p.values[i]
			if v == nil {
				v = mms.NewBoolean(false)
			}
			result = append(result, v)
		}
	}

	if p.optFlds.Has(OptFldReasonCode) {
		for i, incl := range p.inclusion {
			if incl && i < len(p.reasons) {
				result = append(result, encodeReasonCode(p.reasons[i]))
			}
		}
	}

	return result
}

// encodeInclusion encodes the inclusion bitmap as an MMS bitstring.
func encodeInclusion(inclusion []bool) *mms.Value {
	bitLen := len(inclusion)
	byteLen := (bitLen + 7) / 8
	bits := make([]byte, byteLen)
	for i, incl := range inclusion {
		if incl {
			bits[i/8] |= 0x80 >> (uint(i) % 8)
		}
	}
	return mms.NewBitStringWithLength(bits, bitLen)
}

// encodeReasonCode encodes a ReasonCode as a 7-bit bitstring.
func encodeReasonCode(r ReasonCode) *mms.Value {
	b := byte(0)
	if r&ReasonDataChanged != 0 {
		b |= 0x40
	}
	if r&ReasonQualityChanged != 0 {
		b |= 0x20
	}
	if r&ReasonDataUpdate != 0 {
		b |= 0x10
	}
	if r&ReasonIntegrity != 0 {
		b |= 0x08
	}
	if r&ReasonGI != 0 {
		b |= 0x04
	}
	return mms.NewBitStringWithLength([]byte{b}, 7)
}

// encodeEntryID encodes a uint64 as an 8-byte big-endian entry ID.
//
// Entry IDs are monotonically increasing counters, starting at 1 for the
// first buffered entry after server startup. The server never generates
// ID 0; a stored EntryID of all-zero bytes is therefore the neutral value
// that means "no resume point — replay the full buffer."
//
// The 8-byte big-endian encoding matches the IEC 61850-7-2 OctetString8
// format expected for the EntryID attribute.
//
// Entry IDs are purely in-memory and reset to 1 on server restart. They
// are NOT persistent across process restarts and are NOT stable across
// server reboots. Clients should not cache EntryIDs across reconnects to
// a different server process.
//
// Calling PurgeBuf=true invalidates all existing EntryIDs; subsequent
// re-enables start replay from entry 1.
func encodeEntryID(id uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, id)
	return b
}

// decodeEntryID decodes an 8-byte big-endian entry ID into a uint64.
// Returns 0 for nil or short slices (treated as "no resume point").
func decodeEntryID(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b[:8])
}

// filterEntriesAfter returns only the subset of entries whose entryID
// is strictly greater than resumeID. When resumeID is 0, all entries
// are returned (no filtering).
//
// Resume semantics:
//   - The supplied EntryID is treated as EXCLUSIVE. The caller receives
//     entries that were produced AFTER the entry with the given ID.
//   - An unknown or future EntryID (greater than any buffered ID) yields
//     an empty result — the client effectively says "I have everything up
//     to a point beyond the current buffer."
//   - A zero EntryID means "I have nothing; replay the full buffer."
//   - These semantics follow the IEC 61850-7-2 description where the
//     client writes the EntryID of the last received entry before
//     enabling the BRCB, so the server skips that entry and all earlier
//     entries in the replay.
//   - When the server-side buffer has overflowed (entries were dropped)
//     since the supplied EntryID, some entries that would have followed
//     it may no longer be present. The BufOvfl flag in the next report
//     will be set to notify the client of the gap.
func filterEntriesAfter(entries []*bufferedEntry, resumeID uint64) []*bufferedEntry {
	if resumeID == 0 {
		return entries
	}
	var result []*bufferedEntry
	for _, e := range entries {
		if decodeEntryID(e.entryID) > resumeID {
			result = append(result, e)
		}
	}
	return result
}

// valuesEqual performs a recursive equality check between two MMS
// values. Returns true if both are nil, or if they have the same
// type and identical content. Supports all MMS value types including
// structures, arrays, UTC time, and binary time.
func valuesEqual(a, b *mms.Value) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Type() != b.Type() {
		return false
	}

	switch a.Type() {
	case mms.ValueTypeBoolean:
		ab, _ := a.Bool()
		bb, _ := b.Bool()
		return ab == bb
	case mms.ValueTypeInteger:
		ai, _ := a.Int64()
		bi, _ := b.Int64()
		return ai == bi
	case mms.ValueTypeUnsigned:
		au, _ := a.Uint64()
		bu, _ := b.Uint64()
		return au == bu
	case mms.ValueTypeFloat:
		af, _ := a.Float64()
		bf, _ := b.Float64()
		return af == bf
	case mms.ValueTypeVisibleString:
		as, _ := a.VisibleString()
		bs, _ := b.VisibleString()
		return as == bs
	case mms.ValueTypeMmsString:
		as, _ := a.MmsString()
		bs, _ := b.MmsString()
		return as == bs
	case mms.ValueTypeBitString:
		ab, _ := a.BitString()
		bb, _ := b.BitString()
		return bytesEqual(ab, bb)
	case mms.ValueTypeOctetString:
		ao, _ := a.OctetString()
		bo, _ := b.OctetString()
		return bytesEqual(ao, bo)
	case mms.ValueTypeUTCTime:
		at, _ := a.UTCTime()
		bt, _ := b.UTCTime()
		return at.Equal(bt)
	case mms.ValueTypeBinaryTime:
		am, _ := a.BinaryTime()
		bm, _ := b.BinaryTime()
		return am == bm
	case mms.ValueTypeStructure:
		am, aok := a.Structure()
		bm, bok := b.Structure()
		if !aok || !bok || len(am) != len(bm) {
			return false
		}
		for i := range am {
			if !valuesEqual(am[i], bm[i]) {
				return false
			}
		}
		return true
	case mms.ValueTypeArray:
		am, aok := a.Structure() // arrays use Structure() accessor
		bm, bok := b.Structure()
		if !aok || !bok || len(am) != len(bm) {
			return false
		}
		for i := range am {
			if !valuesEqual(am[i], bm[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// qualityChanged checks if quality bits changed between two MMS
// values. It handles three cases:
//   - Direct bitstring (the value is a Quality bitstring).
//   - Structure containing a "q" member at a well-known position.
//     IEC 61850 data attributes typically have stVal at [0] and q
//     at [1] within a structure.
//   - Falls back to full value comparison if the type is bitstring.
//
// qualityChanged is a heuristic quality-change detector. It checks
// for bit-string changes in the quality attribute of common IEC 61850
// CDC structures. For structures, it looks at position 1 which
// matches the standard {stVal, q, t} layout used by most CDCs (SPS,
// DPS, INS, MV, etc.). This is a pragmatic approximation, not a
// semantically complete quality comparison for all possible CDC
// structures.
func qualityChanged(prev, curr *mms.Value) bool {
	if prev == nil || curr == nil {
		return prev != curr
	}

	if prev.Type() == mms.ValueTypeBitString && curr.Type() == mms.ValueTypeBitString {
		return !valuesEqual(prev, curr)
	}

	if prev.Type() == mms.ValueTypeStructure && curr.Type() == mms.ValueTypeStructure {
		pm, pok := prev.Structure()
		cm, cok := curr.Structure()
		if pok && cok && len(pm) >= 2 && len(cm) >= 2 {
			if pm[1] != nil && cm[1] != nil &&
				pm[1].Type() == mms.ValueTypeBitString &&
				cm[1].Type() == mms.ValueTypeBitString {
				return !valuesEqual(pm[1], cm[1])
			}
		}
	}

	return false
}

// decodeTrgOps and decodeOptFlds are defined in report.go.
