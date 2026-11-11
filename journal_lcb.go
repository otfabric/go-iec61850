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

// Log control block (LCB) runtime: IEC 61850-7-2 §17.3, mapped by
// IEC 61850-8-1 §17.3.3 onto the "<LN>$LG$<name>" structure (Table 65)
// and MMS journals.

// lcbReasonTag is the journal variable tag of a reason code
// (IEC 61850-8-1 §17.3.3.4.4).
const lcbReasonTag = "ReasonCode"

// lcbRuntime is the state of one LCB. The store holds the attribute
// values clients read; the runtime caches what logging needs.
type lcbRuntime struct {
	mu sync.Mutex

	ldName, lnName string
	item           string // "<LN>$LG$<name>"

	logEna  bool
	journal journalKey // from LogRef; empty when LogRef is NULL
	datSet  string
	trgOps  TrgOps
	intgPd  uint32

	dsValid     bool
	memberKeys  []string // store key = DatRef "LD/LN$FC$DO$DA"
	memberNodes []*servermodel.MemberNode
	leafMembers map[string][]int
	prevValues  []*mms.Value

	intgStop chan struct{}
}

func (rt *lcbRuntime) key(attr string) string {
	return servermodel.StoreKey(rt.ldName, rt.item+"$"+attr)
}

// lcbError is a DataAccessError for a refused LCB write
// (IEC 61850-8-1 §17.3.4.3 maps a negative response with the
// ServiceError mapping of §8.1.3.4.4.2).
func lcbError(code mms.DataAccessErrorCode, rt *lcbRuntime, format string, args ...any) error {
	return fmt.Errorf("iec61850: LCB %s/%s: %s: %w", rt.ldName, rt.item, fmt.Sprintf(format, args...), &mms.DataAccessError{Code: code})
}

// parseLogRef splits a LogRef "<Domain>/<LNName>$<journal>"
// (IEC 61850-8-1 §17.3.3.3.2) into the MMS journal domain and item.
func parseLogRef(ref string) (journalKey, bool) {
	domain, item, ok := strings.Cut(ref, "/")
	if !ok || domain == "" || !strings.Contains(item, "$") {
		return journalKey{}, false
	}
	return journalKey{domain: domain, journal: item}, true
}

// registerLCBs creates the runtime of every LCB of the model, loads its
// configuration from the store, and starts the integrity timer of the
// enabled ones.
func (e *JournalEngine) registerLCBs() {
	for i := range e.model.LogicalDevices {
		ld := &e.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.Logs {
				rt := &lcbRuntime{ldName: ld.Name, lnName: ln.Name, item: servermodel.LCBItemID(ln.Name, ln.Logs[k].Name)}
				rt.mu.Lock()
				e.loadLocked(rt)
				if rt.logEna {
					e.startIntegrityLocked(rt)
				}
				rt.mu.Unlock()
				e.mu.Lock()
				e.lcbs[ld.Name+"/"+rt.item] = rt
				e.mu.Unlock()
			}
		}
	}
}

// loadLocked reads the LCB configuration from the store and resolves
// the data set. Caller holds rt.mu.
func (e *JournalEngine) loadLocked(rt *lcbRuntime) {
	if v := e.store.Get(rt.key("LogEna")); v != nil {
		rt.logEna, _ = v.Bool()
	}
	var journal journalKey
	if v := e.store.Get(rt.key("LogRef")); v != nil {
		s, _ := v.VisibleString()
		journal, _ = parseLogRef(s)
	}
	e.setJournalRef(rt, journal)
	rt.datSet = ""
	if v := e.store.Get(rt.key("DatSet")); v != nil {
		rt.datSet, _ = v.VisibleString()
	}
	if v := e.store.Get(rt.key("TrgOps")); v != nil {
		rt.trgOps = decodeTrgOps(v)
	}
	if v := e.store.Get(rt.key("IntgPd")); v != nil {
		rt.intgPd, _ = v.Uint32()
	}
	e.resolveLocked(rt)
}

// resolveLocked binds the data set members of rt. Caller holds rt.mu.
func (e *JournalEngine) resolveLocked(rt *lcbRuntime) {
	rt.dsValid = false
	rt.memberKeys, rt.memberNodes, rt.leafMembers, rt.prevValues = nil, nil, nil, nil
	if rt.datSet == "" {
		return // an empty DatSet means no data set is selected
	}
	ds, _ := e.model.FindDataSet(rt.ldName, rt.datSet)
	if ds == nil {
		return
	}
	rt.memberKeys = make([]string, len(ds.Members))
	rt.memberNodes = make([]*servermodel.MemberNode, len(ds.Members))
	rt.leafMembers = make(map[string][]int)
	for i, m := range ds.Members {
		ldInst := m.LDInst
		if ldInst == "" {
			ldInst = rt.ldName
		}
		rt.memberKeys[i] = servermodel.StoreKey(ldInst, m.LNName+"$"+m.FC+"$"+strings.ReplaceAll(m.DOPath, ".", "$"))
		keys := []string{rt.memberKeys[i]}
		if n, err := e.model.ResolveMember(rt.ldName, m); err == nil {
			rt.memberNodes[i] = n
			keys = n.Keys()
		}
		for _, k := range keys {
			rt.leafMembers[k] = append(rt.leafMembers[k], i)
		}
	}
	rt.dsValid = true
	rt.prevValues = e.memberValues(rt)
}

func (e *JournalEngine) memberValues(rt *lcbRuntime) []*mms.Value {
	vals := make([]*mms.Value, len(rt.memberKeys))
	for i, k := range rt.memberKeys {
		if n := rt.memberNodes[i]; n != nil {
			vals[i] = e.store.ReadMember(n)
		} else {
			vals[i] = e.store.Get(k)
		}
	}
	return vals
}

// notifyLCBs logs the members of every enabled LCB that a write of
// storeKey triggers: "the log shall be triggered by the same trigger
// conditions as defined for reporting" (IEC 61850-7-2 §17.3.4), and
// only while LogEna is TRUE.
func (e *JournalEngine) notifyLCBs(storeKey string, occTime time.Time) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, rt := range e.lcbs {
		rt.mu.Lock()
		e.detectLocked(rt, storeKey, occTime)
		rt.mu.Unlock()
	}
}

func (e *JournalEngine) detectLocked(rt *lcbRuntime, storeKey string, occTime time.Time) {
	members := rt.leafMembers[storeKey]
	if len(members) == 0 {
		return
	}
	cur := e.memberValues(rt)
	var vars []mms.JournalVariable
	for _, i := range members {
		reason := triggerReason(rt.trgOps, rt.memberNodes[i], storeKey, rt.prevValues[i], cur[i])
		rt.prevValues[i] = cur[i]
		if reason == 0 || !rt.logEna || rt.journal.journal == "" {
			continue
		}
		vars = append(vars, lcbEntryData(rt.memberKeys[i], cur[i], reason)...)
	}
	if len(vars) > 0 {
		e.addEntry(rt.journal, occTime, vars)
	}
}

// lcbEntryData is one EntryData: the data reference/value variable,
// then the reason code variable; the DatRef tag is an ObjectReference
// (IEC 61850-8-1 §17.3.3.4.4). Table 65 does not map OptFlds, so the
// reason code is always logged.
func lcbEntryData(ref string, v *mms.Value, reason ReasonCode) []mms.JournalVariable {
	if v == nil {
		return nil
	}
	return []mms.JournalVariable{{Tag: ref, Value: v}, {Tag: lcbReasonTag, Value: encodeReasonCode(reason)}}
}

// addEntry appends one journal entry (one MMS journal entry may hold
// several EntryData, IEC 61850-8-1 §17.3.3.4.3) and refreshes the status of the LCBs using that journal.
func (e *JournalEngine) addEntry(k journalKey, occTime time.Time, vars []mms.JournalVariable) []byte {
	id := e.provider.AddEntry(k.domain, k.journal, occTime, vars)
	e.refreshStatus(k)
	return id
}

// refreshStatus writes OldEntrTm, NewEntrTm, OldEnt and NewEnt of every
// LCB whose LogRef is k, so that they describe that log
// (IEC 61850-8-1 Table 65 a). An empty journal has equal zero times and
// NULL (all-zero) entry IDs (§17.3.3.3.4/5).
func (e *JournalEngine) refreshStatus(k journalKey) {
	oldID, newID, oldT, newT := e.provider.bounds(k.domain, k.journal)
	e.refMu.Lock()
	users := e.byJournal[k]
	e.refMu.Unlock()
	for _, rt := range users {
		e.storeStatus(rt, oldID, newID, oldT, newT)
	}
}

// setJournalRef records that rt writes to journal k (its LogRef). Caller
// holds rt.mu; byJournal has its own lock so that an entry added under
// one LCB's lock can refresh the status of the others.
func (e *JournalEngine) setJournalRef(rt *lcbRuntime, k journalKey) {
	e.refMu.Lock()
	defer e.refMu.Unlock()
	if old := rt.journal; old.journal != "" {
		users := e.byJournal[old]
		for i, u := range users {
			if u == rt {
				e.byJournal[old] = append(users[:i:i], users[i+1:]...)
				break
			}
		}
	}
	rt.journal = k
	if k.journal != "" {
		e.byJournal[k] = append(e.byJournal[k], rt)
	}
}

func (e *JournalEngine) storeStatus(rt *lcbRuntime, oldID, newID []byte, oldT, newT time.Time) {
	bt := func(t time.Time) *mms.Value {
		if t.IsZero() {
			return mms.NewBinaryTime(binaryTimeEpoch1984.UnixMilli())
		}
		return mms.NewBinaryTime(t.UTC().UnixMilli())
	}
	id := func(b []byte) *mms.Value {
		if b == nil {
			b = make([]byte, 8)
		}
		return mms.NewOctetString(b)
	}
	e.store.Set(rt.key("OldEntrTm"), bt(oldT))
	e.store.Set(rt.key("NewEntrTm"), bt(newT))
	e.store.Set(rt.key("OldEnt"), id(oldID))
	e.store.Set(rt.key("NewEnt"), id(newID))
}

// startIntegrityLocked starts the integrity log of rt: with TrgOps
// integrity and IntgPd > 0, all members are logged every IntgPd ms with
// reason integrity (IEC 61850-7-2 §17.3.4, §17.2.3.2.3.3). Caller holds
// rt.mu.
func (e *JournalEngine) startIntegrityLocked(rt *lcbRuntime) {
	e.stopIntegrityLocked(rt)
	if !rt.trgOps.Has(TrgOpIntegrity) || rt.intgPd == 0 || !rt.dsValid {
		return
	}
	stop := make(chan struct{})
	rt.intgStop = stop
	period := time.Duration(rt.intgPd) * time.Millisecond
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-e.stopCh:
				return
			case <-t.C:
				// The ticker paces IntgPd on the monotonic clock; the
				// entry is stamped by the server's Clock.
				now := clockOrSystem(e.clock).Now()
				e.mu.RLock()
				rt.mu.Lock()
				if rt.intgStop == stop {
					e.integrityLocked(rt, now)
				}
				rt.mu.Unlock()
				e.mu.RUnlock()
			}
		}
	}()
}

func (rt *lcbRuntime) stopIntegrityLocked() {
	if rt.intgStop != nil {
		close(rt.intgStop)
		rt.intgStop = nil
	}
}

func (e *JournalEngine) stopIntegrityLocked(rt *lcbRuntime) { rt.stopIntegrityLocked() }

func (e *JournalEngine) integrityLocked(rt *lcbRuntime, now time.Time) {
	if !rt.logEna || !rt.dsValid || rt.journal.journal == "" {
		return
	}
	cur := e.memberValues(rt)
	var vars []mms.JournalVariable
	for i, v := range cur {
		vars = append(vars, lcbEntryData(rt.memberKeys[i], v, ReasonIntegrity)...)
	}
	copy(rt.prevValues, cur)
	if len(vars) > 0 {
		e.addEntry(rt.journal, now, vars)
	}
}

// HandleLCBWrite applies a client write of LCB attribute attr
// (IEC 61850-8-1 §17.3.4.3 SetLCBValues). While LogEna is TRUE, writes
// to LogRef, DatSet, TrgOps and IntgPd are rejected; a LogRef naming no
// existing journal and a DatSet naming no existing named variable list
// are rejected too. The value is committed only when the write is
// accepted.
func (e *JournalEngine) HandleLCBWrite(_ context.Context, ldName, lcbItem, attr string, val *mms.Value) error {
	e.mu.RLock()
	rt := e.lcbs[ldName+"/"+lcbItem]
	e.mu.RUnlock()
	if rt == nil {
		return fmt.Errorf("iec61850: LCB %s/%s: %w", ldName, lcbItem, &mms.DataAccessError{Code: mms.DataAccessErrorObjectNonExistent})
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	switch attr {
	case "LogEna":
		ena, ok := val.Bool()
		if !ok {
			return lcbError(mms.DataAccessErrorTypeInconsistent, rt, "LogEna is not a boolean")
		}
		e.store.Set(rt.key(attr), val)
		if ena == rt.logEna {
			return nil
		}
		rt.logEna = ena
		if ena {
			// Evaluate triggers against the values from now on.
			rt.prevValues = e.memberValues(rt)
			e.startIntegrityLocked(rt)
		} else {
			rt.stopIntegrityLocked()
		}
		return nil
	case "LogRef", "DatSet", "TrgOps", "IntgPd":
	default:
		return lcbError(mms.DataAccessErrorObjectNonExistent, rt, "no attribute %s", attr)
	}

	if rt.logEna {
		return lcbError(mms.DataAccessErrorTemporarilyUnavailable, rt, "%s may not be written while LogEna=TRUE", attr)
	}
	switch attr {
	case "LogRef":
		s, _ := val.VisibleString()
		if s != "" {
			k, ok := parseLogRef(s)
			if !ok || !e.provider.hasJournal(k.domain, k.journal) {
				return lcbError(mms.DataAccessErrorObjectValueInvalid, rt, "LogRef %q names no journal", s)
			}
		}
	case "DatSet":
		s, _ := val.VisibleString()
		if s != "" {
			if ds, _ := e.model.FindDataSet(rt.ldName, s); ds == nil {
				return lcbError(mms.DataAccessErrorObjectValueInvalid, rt, "DatSet %q names no data set", s)
			}
		}
	}
	e.store.Set(rt.key(attr), val)
	e.loadLocked(rt)
	if rt.journal.journal != "" {
		oldID, newID, oldT, newT := e.provider.bounds(rt.journal.domain, rt.journal.journal)
		e.storeStatus(rt, oldID, newID, oldT, newT)
	} else {
		e.storeStatus(rt, nil, nil, time.Time{}, time.Time{})
	}
	return nil
}

// stop ends the integrity goroutines of all LCBs.
func (e *JournalEngine) stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
		e.mu.RLock()
		for _, rt := range e.lcbs {
			rt.mu.Lock()
			rt.stopIntegrityLocked()
			rt.mu.Unlock()
		}
		e.mu.RUnlock()
		e.wg.Wait()
	})
}
