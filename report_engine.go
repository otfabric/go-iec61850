// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"slices"
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
// # Ownership and delivery
//
// An RCB instance is owned by one client at a time and only that client
// receives its reports (IEC 61850-7-2 §17.2.1). A URCB is reserved by
// Resv=TRUE or implicitly by RptEna=TRUE (§17.2.4.5); a BRCB is reserved
// by the client that enables it (Table 37 c3). While reserved, writes by
// other clients are answered with temporarily-unavailable (IEC 61850-8-1
// §8.1.3.4.4.2). When the owning association is lost the RCB is
// disabled and released (§17.2.2.5 Figure 24, §17.2.4.4).
//
// Each enabled RCB has one delivery goroutine that formats and sends its
// entries in creation order (§17.2.3.2.3.5), so buffered (replayed) and
// live reports never overtake each other and SqNum is contiguous.
//
// A BRCB buffers entries in all states (§17.2.3.2.3.6) from the moment
// its DatSet references an existing data set.
type ReportEngine struct {
	logger *slog.Logger
	store  *servermodel.ValueStore
	mmsSrv *mms.Server
	model  *servermodel.Model

	// maxPDU bounds the encoded size of one InformationReport; longer
	// reports are segmented (IEC 61850-7-2 §17.2.3.2.2.5).
	maxPDU int

	mu   sync.RWMutex
	rcbs map[string]*rcbRuntime // keyed by "ldName/rcbItemID"

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// life ends when Stop is called; every background report send
	// derives its context from it (see sendContext).
	life     context.Context
	stopLife context.CancelFunc
}

// rcbRuntime holds the live state for a single report control block.
// All fields are guarded by mu.
type rcbRuntime struct {
	mu sync.Mutex

	ldName    string
	lnName    string
	rcbItemID string
	buffered  bool

	// Configuration, kept equal to the store attributes: loaded at
	// start-up and updated on every accepted write.
	rptID   string
	datSet  string
	confRev uint32
	optFlds OptFlds
	trgOps  TrgOps
	bufTm   uint32
	intgPd  uint32

	// dsValid is true when datSet resolved to a data set of the model.
	dsValid bool

	// Ownership (IEC 61850-7-2 §17.2.1, §17.2.4.5, Table 37 c3).
	owner        *mms.ServerConn // reserving client; nil when free or local
	reserved     bool            // URCB Resv attribute
	explicitResv bool            // URCB: reserved by Resv=TRUE (not only by RptEna)

	// Runtime state.
	enabled    bool
	enableConn *mms.ServerConn // delivery target; nil for a local enable
	resync     bool            // BRCB resync state (Figure 24)
	resyncID   uint64          // BRCB: EntryID set by the client in resync
	seqNum     uint32          // SqNum of the next report

	// Dataset member resolution cache.
	memberKeys  []string                  // store keys for dataset members
	memberVars  []mms.ObjectName          // MMS variable names for report encoding
	memberNodes []*servermodel.MemberNode // value layout per member; nil if unresolved
	leafMembers map[string][]int          // store key of a leaf -> members containing it

	// Previous values for change detection.
	prevValues []*mms.Value

	// Entry queue, in creation order. A BRCB keeps transmitted entries
	// (up to bufMax) for a later resync; a URCB drops an entry once it
	// is sent.
	bufQueue    []*bufferedEntry
	bufMax      int
	entryID     uint64 // id of the newest entry created
	lastSentID  uint64 // id of the last entry formatted and queued for transmission
	pendingOvfl bool   // BufOvfl=TRUE goes into the next report
	discarded   bool   // an entry was discarded since start-up or the last purge

	// BufTm aggregation (IEC 61850-7-2 §17.2.2.9).
	agg      *bufferedEntry
	aggTimer *time.Timer
	aggGen   uint64

	// Integrity timer goroutine; closed to stop it.
	intgStop chan struct{}

	// Delivery goroutine of the current enable; sendStop is closed on
	// disable, wake signals new entries.
	sendStop chan struct{}
	wake     chan struct{}
}

// bufferedEntry holds a single report entry waiting for, or kept after,
// delivery.
type bufferedEntry struct {
	id          uint64
	entryID     []byte
	timeOfEntry time.Time
	values      []*mms.Value
	reasons     []ReasonCode
	inclusion   []bool

	// gi marks a general-interrogation entry: it is sent once in the
	// enable session that requested it and never replayed (§17.2.2.13,
	// §17.2.3.2.3.6 buffers dchg/qchg/dupd/integrity only).
	gi   bool
	sent bool
}

// defaultRCBBufMax is the default entry capacity of a BRCB buffer.
const defaultRCBBufMax = 1000

// reportDispatchDelay defers the first report after a quiet period so
// that the Write response that triggered it reaches the client first;
// libiec61850 discards a report received while it waits for that
// response. Reports keep their order regardless (one sender per RCB).
var reportDispatchDelay = 100 * time.Millisecond

// connReapInterval is how often the engine checks for owners whose
// association has ended (besides the check after every Server.Serve).
var connReapInterval = time.Second

// EnableReports creates and starts the [ReportEngine] for this server.
// It scans all RCBs in the model, creates runtime state for each, starts
// buffering for every BRCB whose DatSet references an existing data set
// (IEC 61850-7-2 §17.2.2.5 Figure 24 power-up), and begins monitoring
// for GI/integrity triggers.
//
// Call this after [NewServer] and before [Server.Serve] or
// [Server.ListenAndServe].
func (s *Server) EnableReports() *ReportEngine {
	engine := &ReportEngine{
		logger: s.logger,
		store:  s.store,
		mmsSrv: s.mms,
		model:  s.model,
		maxPDU: s.reportMaxPDU,
		rcbs:   make(map[string]*rcbRuntime),
		stopCh: make(chan struct{}),
	}
	if engine.maxPDU <= 0 {
		engine.maxPDU = defaultReportMaxPDU
	}
	engine.life, engine.stopLife = context.WithCancel(context.Background())

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

	engine.wg.Add(1)
	go engine.reapLoop()

	s.reportEngine = engine
	s.installWriteInterceptor()
	s.logger.Info("iec61850: report engine enabled", "rcbs", len(engine.rcbs))
	return engine
}

// Stop shuts down the report engine, stopping all integrity timers
// and delivery goroutines and disabling all RCBs.
func (re *ReportEngine) Stop() {
	re.stopOnce.Do(func() {
		if re.stopLife != nil {
			re.stopLife()
		}
		close(re.stopCh)
		re.mu.RLock()
		for _, rt := range re.rcbs {
			rt.mu.Lock()
			re.disableLocked(rt)
			rt.stopIntegrityLocked()
			rt.stopAggLocked()
			rt.mu.Unlock()
		}
		re.mu.RUnlock()
		re.wg.Wait()
	})
}

// stopped reports whether Stop has been called.
func (re *ReportEngine) stopped() bool {
	select {
	case <-re.stopCh:
		return true
	default:
		return false
	}
}

// lifeContext returns the engine's lifetime context, which ends on Stop.
// An engine not created by EnableReports never stops on its own.
func (re *ReportEngine) lifeContext() context.Context {
	if re.life != nil {
		return re.life
	}
	return context.Background()
}

// sendContext returns the context for one background report send: it
// ends when the engine stops or after unsolicitedSendTimeout, so a peer
// that stops reading cannot hold a delivery goroutine (and Stop) forever.
func (re *ReportEngine) sendContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(re.lifeContext(), unsolicitedSendTimeout)
}

// send delivers one InformationReport within sendContext.
func (re *ReportEngine) send(conn *mms.ServerConn, req *mms.InformationReportRequest) error {
	ctx, cancel := re.sendContext()
	defer cancel()
	return conn.SendInformationReport(ctx, req)
}

// isConnActive reports whether conn is still registered with the MMS server.
// A nil conn is never considered active.
func (re *ReportEngine) isConnActive(conn *mms.ServerConn) bool {
	if conn == nil || re.mmsSrv == nil {
		return false
	}
	return slices.Contains(re.mmsSrv.Connections(), conn)
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
		bufMax:    defaultRCBBufMax,
		wake:      make(chan struct{}, 1),
	}

	rt.mu.Lock()
	re.loadConfigLocked(rt)
	if rt.buffered {
		// IEC 61850-7-2 §17.2.2.5 (Figure 24): upon power-up buffering
		// starts if DatSet references an existing data set.
		if re.resolveDataset(rt) == nil {
			rt.prevValues = re.readMemberValues(rt)
		}
		re.restartIntegrityLocked(rt)
	}
	rt.mu.Unlock()

	re.rcbs[key] = rt
}

// sk returns the store key of RCB attribute suffix.
func (rt *rcbRuntime) sk(suffix string) string {
	return servermodel.StoreKey(rt.ldName, rt.rcbItemID+"$"+suffix)
}

// loadConfigLocked reads the RCB configuration attributes from the store.
func (re *ReportEngine) loadConfigLocked(rt *rcbRuntime) {
	rt.rptID = ""
	if v := re.store.Get(rt.sk("RptID")); v != nil {
		if s, ok := v.VisibleString(); ok {
			rt.rptID = s
		}
	}
	if rt.rptID == "" {
		// IEC 61850-8-1 §17.2.1: a NULL RptID reports the RCB reference.
		rt.rptID = rt.ldName + "/" + rt.rcbItemID
	}
	rt.datSet = ""
	if v := re.store.Get(rt.sk("DatSet")); v != nil {
		if s, ok := v.VisibleString(); ok {
			rt.datSet = s
		}
	}
	if v := re.store.Get(rt.sk("ConfRev")); v != nil {
		if u, ok := v.Uint32(); ok {
			rt.confRev = u
		}
	}
	if v := re.store.Get(rt.sk("OptFlds")); v != nil {
		rt.optFlds = decodeOptFlds(v)
	}
	if v := re.store.Get(rt.sk("TrgOps")); v != nil {
		rt.trgOps = decodeTrgOps(v)
	}
	if v := re.store.Get(rt.sk("BufTm")); v != nil {
		if u, ok := v.Uint32(); ok {
			rt.bufTm = u
		}
	}
	if v := re.store.Get(rt.sk("IntgPd")); v != nil {
		if u, ok := v.Uint32(); ok {
			rt.intgPd = u
		}
	}
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

// resolveDataset maps the DatSet string to ValueStore keys and MMS
// ObjectNames for each member and sets rt.dsValid. Caller holds rt.mu.
func (re *ReportEngine) resolveDataset(rt *rcbRuntime) error {
	rt.dsValid = false
	rt.memberKeys, rt.memberVars, rt.memberNodes, rt.leafMembers, rt.prevValues = nil, nil, nil, nil, nil
	if rt.datSet == "" {
		return fmt.Errorf("empty DatSet")
	}
	ds := re.findDataset(rt.ldName, rt.datSet)
	if ds == nil {
		return fmt.Errorf("dataset %q not found", rt.datSet)
	}

	rt.memberKeys = make([]string, len(ds.Members))
	rt.memberVars = make([]mms.ObjectName, len(ds.Members))
	rt.memberNodes = make([]*servermodel.MemberNode, len(ds.Members))
	rt.leafMembers = make(map[string][]int)

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
		// A member may name a data object or a constructed attribute
		// (FCD): its value is the structure of all attributes of its FC
		// below it, and a change of any of them concerns the member.
		// A member the model cannot resolve keeps the exact-key lookup.
		keys := []string{rt.memberKeys[i]}
		if n, err := re.model.ResolveMember(rt.ldName, m); err == nil {
			rt.memberNodes[i] = n
			keys = n.Keys()
		}
		for _, k := range keys {
			rt.leafMembers[k] = append(rt.leafMembers[k], i)
		}
	}
	rt.dsValid = true
	return nil
}

// findDataset returns the data set named by ref (local name or
// "LD/LN$name") in logical device ldName, or nil.
func (re *ReportEngine) findDataset(ldName, ref string) *servermodel.DataSetDef {
	for i := range re.model.LogicalDevices {
		ld := &re.model.LogicalDevices[i]
		if ld.Name != ldName {
			continue
		}
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.DataSets {
				dsRef := ldName + "/" + ln.Name + "$" + ln.DataSets[k].Name
				if dsRef == ref || ln.DataSets[k].Name == ref {
					return &ln.DataSets[k]
				}
			}
		}
	}
	return nil
}

// readMemberValues reads current values for all dataset members.
func (re *ReportEngine) readMemberValues(rt *rcbRuntime) []*mms.Value {
	values := make([]*mms.Value, len(rt.memberKeys))
	for i, key := range rt.memberKeys {
		if n := rt.memberNodes[i]; n != nil {
			values[i] = re.store.ReadMember(n)
			continue
		}
		values[i] = re.store.Get(key)
	}
	return values
}

// monitoringLocked reports whether the RCB records entries now: a BRCB
// in every state once its data set is valid (§17.2.3.2.3.6), a URCB
// only while enabled.
func (rt *rcbRuntime) monitoringLocked() bool {
	if !rt.dsValid {
		return false
	}
	return rt.enabled || rt.buffered
}

// disableLocked stops reporting (RptEna=FALSE): the delivery goroutine
// ends, unsent GI entries are dropped (§17.2.2.13 Figure 28: no GI
// outside the enabled state) and a URCB forgets its queue. A BRCB keeps
// buffering. Caller holds rt.mu.
func (re *ReportEngine) disableLocked(rt *rcbRuntime) {
	rt.resync = false
	if !rt.enabled {
		return
	}
	rt.enabled = false
	rt.enableConn = nil
	if rt.sendStop != nil {
		close(rt.sendStop)
		rt.sendStop = nil
	}
	if rt.buffered {
		rt.bufQueue = slices.DeleteFunc(rt.bufQueue, func(e *bufferedEntry) bool { return e.gi && !e.sent })
		re.publishNewestEntryLocked(rt)
	} else {
		rt.bufQueue = nil
		rt.stopIntegrityLocked()
		rt.stopAggLocked()
	}
	re.store.Set(rt.sk("RptEna"), mms.NewBoolean(false))
}

// releaseLocked frees the reservation of rt. Caller holds rt.mu.
func (re *ReportEngine) releaseLocked(rt *rcbRuntime) {
	rt.owner = nil
	rt.explicitResv = false
	if !rt.buffered && rt.reserved {
		rt.reserved = false
		re.store.Set(rt.sk("Resv"), mms.NewBoolean(false))
	}
}

// connLost handles the end of an association: every RCB it owns or
// reports to is disabled and released (IEC 61850-7-2 §17.2.2.5 Figure 24
// "association lost", §17.2.4.4, Figure 27). BRCBs keep buffering, so the
// reconnecting client can resynchronise.
func (re *ReportEngine) connLost(conn *mms.ServerConn) {
	if conn == nil {
		return
	}
	re.mu.RLock()
	defer re.mu.RUnlock()
	for _, rt := range re.rcbs {
		rt.mu.Lock()
		if rt.owner == conn || rt.enableConn == conn {
			re.disableLocked(rt)
			re.releaseLocked(rt)
			re.logger.Info("iec61850: RCB released after association loss", "rcb", rt.rcbItemID)
		}
		rt.mu.Unlock()
	}
}

// reapClosedConns releases every RCB whose owner is no longer an active
// association.
func (re *ReportEngine) reapClosedConns() {
	if re.mmsSrv == nil {
		return
	}
	active := re.mmsSrv.Connections()
	var gone []*mms.ServerConn
	re.mu.RLock()
	for _, rt := range re.rcbs {
		rt.mu.Lock()
		for _, c := range []*mms.ServerConn{rt.owner, rt.enableConn} {
			if c != nil && !slices.Contains(active, c) && !slices.Contains(gone, c) {
				gone = append(gone, c)
			}
		}
		rt.mu.Unlock()
	}
	re.mu.RUnlock()
	for _, c := range gone {
		re.connLost(c)
	}
}

// reapLoop periodically releases RCBs of lost associations, for servers
// whose connections are not served through Server.Serve.
func (re *ReportEngine) reapLoop() {
	defer re.wg.Done()
	t := time.NewTicker(connReapInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			re.reapClosedConns()
		case <-re.stopCh:
			return
		}
	}
}

// encodeEntryID encodes a uint64 as an 8-byte big-endian entry ID.
//
// Entry IDs are monotonically increasing counters, starting at 1 for the
// first entry after server startup. The server never generates ID 0
// (IEC 61850-7-2 §17.2.2.15: all zeros is reserved for an empty buffer).
//
// The 8-byte big-endian encoding matches the IEC 61850-8-1 §8.1.3.5
// EntryID (OCTET STRING (SIZE(8))).
//
// Entry IDs are purely in-memory and reset to 1 on server restart. They
// are NOT persistent across process restarts. A purge discards entries
// but IDs keep counting, so an EntryID from before the purge is unknown
// afterwards and a resync to it is refused (§17.2.2.5 Figure 24).
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
		am, aok := a.ArrayElements()
		bm, bok := b.ArrayElements()
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
// qualityChanged is a heuristic quality-change detector, used for data
// set members whose layout the model cannot resolve. It looks at
// position 1 which matches the standard {stVal, q, t} layout used by
// most CDCs (SPS, DPS, INS, MV, etc.).
//
// A missing value (never set) on one side is a quality change only when
// the other side carries a quality.
func qualityChanged(prev, curr *mms.Value) bool {
	if prev == nil || curr == nil {
		if prev == nil {
			return hasQuality(curr)
		}
		return hasQuality(prev)
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

// hasQuality reports whether v is a quality bit-string or a structure
// with one at position 1 (the layout qualityChanged inspects).
func hasQuality(v *mms.Value) bool {
	if v == nil {
		return false
	}
	if v.Type() == mms.ValueTypeBitString {
		return true
	}
	m, ok := v.Structure()
	return ok && len(m) >= 2 && m[1] != nil && m[1].Type() == mms.ValueTypeBitString
}

// decodeTrgOps and decodeOptFlds are defined in report.go.
