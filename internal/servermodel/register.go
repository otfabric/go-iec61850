// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/otfabric/go-mms"
)

// binaryTimeEpoch is the MMS TimeOfDay origin (1984-01-01). go-mms v1.1.0
// rejects BinaryTime values outside 1984-01-01…2163-06-05, so Unix 0
// (1970) is no longer a valid empty TimeOfEntry / EntryTime.
var binaryTimeEpoch = time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC)

func emptyBinaryTime() *mms.Value {
	return mms.NewBinaryTime(binaryTimeEpoch.UnixMilli())
}

func defaultMMSValue(ts mms.TypeSpec) *mms.Value {
	if ts.Type == mms.ValueTypeBinaryTime {
		return emptyBinaryTime()
	}
	return ts.DefaultValue()
}

// btypeToMMS maps IEC 61850 basic types to MMS TypeSpec values.
var btypeToMMS = map[string]mms.TypeSpec{
	"BOOLEAN": {Type: mms.ValueTypeBoolean},
	"INT8":    {Type: mms.ValueTypeInteger, Size: 8},
	"INT16":   {Type: mms.ValueTypeInteger, Size: 16},
	"INT24":   {Type: mms.ValueTypeInteger, Size: 24},
	"INT32":   {Type: mms.ValueTypeInteger, Size: 32},
	"INT64":   {Type: mms.ValueTypeInteger, Size: 64},
	"INT128":  {Type: mms.ValueTypeInteger, Size: 128},
	"INT8U":   {Type: mms.ValueTypeUnsigned, Size: 8},
	"INT16U":  {Type: mms.ValueTypeUnsigned, Size: 16},
	"INT24U":  {Type: mms.ValueTypeUnsigned, Size: 24},
	"INT32U":  {Type: mms.ValueTypeUnsigned, Size: 32},
	"FLOAT32": {Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
	"FLOAT64": {Type: mms.ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11},
	"Enum":    {Type: mms.ValueTypeInteger, Size: 8},
	"Dbpos":   {Type: mms.ValueTypeBitString, Size: 2},
	// Tcmd is a CODED ENUM (stop | lower | higher | reserved, IEC 61850-7-3
	// BSC/BAC ctlVal); IEC 61850-8-1 8.1.2.3 maps a CODED ENUM to a
	// bit-string sized for its largest value: BIT STRING(2). Before
	// v1.17.0 it was served as INT8 Integer.
	"Tcmd":         {Type: mms.ValueTypeBitString, Size: 2},
	"Quality":      {Type: mms.ValueTypeBitString, Size: 13},
	"Timestamp":    {Type: mms.ValueTypeUTCTime},
	"VisString32":  {Type: mms.ValueTypeVisibleString, Size: 32},
	"VisString64":  {Type: mms.ValueTypeVisibleString, Size: 64},
	"VisString65":  {Type: mms.ValueTypeVisibleString, Size: 65},
	"VisString129": {Type: mms.ValueTypeVisibleString, Size: 129},
	// ObjectReference is mapped to VISIBLE STRING129 (IEC 61850-8-1, ObjRef).
	"ObjRef":       {Type: mms.ValueTypeVisibleString, Size: 129},
	"VisString255": {Type: mms.ValueTypeVisibleString, Size: 255},
	"Unicode255":   {Type: mms.ValueTypeMmsString, Size: 255},
	"Octet6":       {Type: mms.ValueTypeOctetString, Size: 6},
	"Octet16":      {Type: mms.ValueTypeOctetString, Size: 16},
	"Octet64":      {Type: mms.ValueTypeOctetString, Size: 64},
	"Check":        {Type: mms.ValueTypeBitString, Size: 2},
	"OptFlds":      {Type: mms.ValueTypeBitString, Size: 10},
	"TrgOps":       {Type: mms.ValueTypeBitString, Size: 6},
	"EntryTime":    {Type: mms.ValueTypeBinaryTime},
	// The remaining basic types of IEC 61850-6 (tPredefinedBasicTypeEnum)
	// that a model may use for service-tracking and control-block
	// attributes. EntryID is an 8-octet OCTET STRING (IEC 61850-8-1
	// 8.1.3.5); SvOptFlds is the 5-bit OptFlds of an MSVCB (IEC 61850-9-2);
	// Currency is the ISO 4217 three-character code (IEC 61850-7-3);
	// PhyComAddr is {Addr, PRIORITY, VID, APPID} (IEC 61850-8-1, GoCB and
	// MSVCB DstAddress).
	"EntryID":   {Type: mms.ValueTypeOctetString, Size: 8},
	"SvOptFlds": {Type: mms.ValueTypeBitString, Size: 5},
	"Currency":  {Type: mms.ValueTypeVisibleString, Size: 3},
	"PhyComAddr": {Type: mms.ValueTypeStructure, Elements: []mms.TypeSpecElement{
		{Name: "Addr", Type: mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 6}},
		{Name: "PRIORITY", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}},
		{Name: "VID", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
		{Name: "APPID", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
	}},
}

// StoreKey builds a domain-qualified key for the [ValueStore].
// Format: "ldName/itemID".
func StoreKey(ldName, itemID string) string {
	return ldName + "/" + itemID
}

// ValueStore provides thread-safe storage for server data attribute
// values. Each attribute is keyed by its domain-qualified MMS
// identity: "ldName/itemID" (e.g. "LD1/LLN0$ST$Mod$stVal").
//
// By default, values are stored and returned as raw *mms.Value
// pointers without copying. Callers that need to mutate a retrieved
// value must copy it first. This aliasing policy keeps the hot path
// allocation-free.
//
// For safer usage in tests or complex server callbacks, enable
// DefensiveCopy mode via [ValueStoreOptions]. When enabled, Get
// returns a shallow copy (struct-level) and Set stores a shallow
// copy, reducing aliasing bugs at the cost of allocation. Note:
// this does not deep-copy slice fields (e.g., OctetString, BitString,
// Structure elements) — for those, callers must still copy manually.
// WriteInterceptor is called by RCB and SGCB write callbacks before
// writing to the store. If it returns (true, nil) the write was
// handled by the interceptor and the normal store write is skipped.
// If it returns (true, err) the write is rejected. If it returns
// (false, nil) the normal store write proceeds.
type WriteInterceptor func(ctx context.Context, storeKey string, val *mms.Value) (handled bool, err error)

type ValueStore struct {
	mu            sync.RWMutex
	values        map[string]*mms.Value
	defensiveCopy bool
	// allowReadOnlyFCWrites lets clients write read-only FCs; see
	// ValueStoreOptions.AllowReadOnlyFCWrites.
	allowReadOnlyFCWrites bool

	interceptorMu sync.RWMutex
	interceptor   WriteInterceptor
	editGuard     EditGuard
	writeGuard    WriteGuard
}

// WriteGuard decides whether a client may write the data attribute under
// storeKey; a non-nil error refuses the write before the store changes.
type WriteGuard func(ctx context.Context, storeKey string) error

// EditGuard decides whether a client may read (write=false) or write the
// FC=SE (edit setting group) attribute under storeKey; a non-nil error
// refuses the access before the store is read or changed.
type EditGuard func(ctx context.Context, storeKey string, write bool) error

// ValueStoreOptions configures [ValueStore] behavior.
type ValueStoreOptions struct {
	// AllowReadOnlyFCWrites, when true, lets clients write data
	// attributes of the read-only functional constraints (ST, MX, SG,
	// SR, OR, EX) when the model is registered with this store. Only
	// for simulators and test fixtures; see clientWritableFC.
	AllowReadOnlyFCWrites bool

	// DefensiveCopy, when true, causes Get and Set to perform a
	// shallow struct-level copy (*mms.Value is copied by value, not
	// by pointer). This prevents the most common aliasing bug where
	// a caller mutates a value after Set or uses a returned value
	// as mutable.
	//
	// IMPORTANT: this is a SHALLOW copy only. Slice fields inside
	// mms.Value (OctetString bytes, BitString bytes, Structure
	// element pointers, Array element pointers) still share the
	// underlying memory. Callers who mutate slice contents must
	// copy those slices manually regardless of this setting. This
	// option is a safety guardrail, not a full isolation guarantee.
	DefensiveCopy bool
}

// NewValueStore creates a new empty value store.
func NewValueStore() *ValueStore {
	return &ValueStore{values: make(map[string]*mms.Value)}
}

// NewValueStoreWithOptions creates a value store with the given
// options.
func NewValueStoreWithOptions(opts ValueStoreOptions) *ValueStore {
	return &ValueStore{
		values:                make(map[string]*mms.Value),
		defensiveCopy:         opts.DefensiveCopy,
		allowReadOnlyFCWrites: opts.AllowReadOnlyFCWrites,
	}
}

// Get retrieves a value by domain-qualified key (see [StoreKey]).
//
// When defensive-copy mode is disabled (the default), the returned
// pointer aliases the stored value — treat it as read-only; copy
// before mutating. When defensive-copy mode is enabled, a shallow
// copy is returned as a safety guardrail.
func (vs *ValueStore) Get(key string) *mms.Value {
	vs.mu.RLock()
	v := vs.values[key]
	vs.mu.RUnlock()
	if vs.defensiveCopy && v != nil {
		cp := *v
		return &cp
	}
	return v
}

// Set stores a value by domain-qualified key (see [StoreKey]).
//
// When defensive-copy mode is disabled (the default), the stored
// pointer is not copied — after calling Set, the caller must not
// mutate v. When defensive-copy mode is enabled, a shallow copy
// is stored instead.
func (vs *ValueStore) Set(key string, v *mms.Value) {
	if vs.defensiveCopy && v != nil {
		cp := *v
		v = &cp
	}
	vs.mu.Lock()
	vs.values[key] = v
	vs.mu.Unlock()
}

// SetWriteInterceptor installs a callback that serves two distinct roles
// depending on the attribute kind being written:
//
// Pre-commit dispatcher (RCB / SGCB subfields)
//
// For RCB subfields the handler stores the value first and then calls the
// interceptor; for SGCB subfields the interceptor is called first and the
// handler stores only when the interceptor returns handled=false.  Returning
// (true, err) from the interceptor causes the Write function to return err
// immediately, which can be used to reject the MMS write before it completes.
//
// Post-write notification hook (regular data attributes)
//
// For normal DAs the Write function validates the write, stores the new value
// via [ValueStore.Set], and only then calls the interceptor.  In this role
// the interceptor cannot prevent the write — the value is already in the store
// before the call.  It is used purely for notification (e.g. to trigger
// report-engine dchg evaluation after a client writes a dataset member).
// A write of a constructed attribute (other than a CO control service)
// stores and notifies its leaves one by one, each under its leaf key.
//
// Because of this split role, do NOT assume that every MMS client write flows
// through the interceptor before reaching the store.  Regular DA writes are
// already committed when the interceptor runs.
//
// The interceptor is checked at call time, so it may be set after
// [RegisterModel] returns.
func (vs *ValueStore) SetWriteInterceptor(fn WriteInterceptor) {
	vs.interceptorMu.Lock()
	vs.interceptor = fn
	vs.interceptorMu.Unlock()
}

// SetEditGuard installs the [EditGuard] consulted for every client read
// and write of an FC=SE attribute (IEC 61850-7-2 16.2.2.5: with no edit
// group selected, SetEditSGValue and GetEditSGValue get Response-).
func (vs *ValueStore) SetEditGuard(fn EditGuard) {
	vs.interceptorMu.Lock()
	vs.editGuard = fn
	vs.interceptorMu.Unlock()
}

// SetWriteGuard installs the [WriteGuard] consulted before every client
// write of a leaf data attribute is stored.
func (vs *ValueStore) SetWriteGuard(fn WriteGuard) {
	vs.interceptorMu.Lock()
	vs.writeGuard = fn
	vs.interceptorMu.Unlock()
}

// checkWrite runs the write guard.
func (vs *ValueStore) checkWrite(ctx context.Context, key string) error {
	vs.interceptorMu.RLock()
	fn := vs.writeGuard
	vs.interceptorMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, key)
}

// checkEdit runs the edit guard for an FC=SE access.
func (vs *ValueStore) checkEdit(ctx context.Context, fc, key string, write bool) error {
	if fc != "SE" {
		return nil
	}
	vs.interceptorMu.RLock()
	fn := vs.editGuard
	vs.interceptorMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, key, write)
}

func (vs *ValueStore) callInterceptor(ctx context.Context, key string, val *mms.Value) (bool, error) {
	vs.interceptorMu.RLock()
	fn := vs.interceptor
	vs.interceptorMu.RUnlock()
	if fn != nil {
		return fn(ctx, key, val)
	}
	return false, nil
}

// CallInterceptorForTest exposes the write interceptor for testing.
func (vs *ValueStore) CallInterceptorForTest(ctx context.Context, key string, val *mms.Value) (bool, error) {
	return vs.callInterceptor(ctx, key, val)
}

// RegisterModel registers all logical devices, variables, datasets,
// and report control blocks from the server [Model] with the MMS
// [mms.Server]. The [ValueStore] is used as the backing store for
// variable read/write callbacks.
//
// Returns the [ValueStore] used, which can also be provided pre-populated.
//
// Note: DA elements with Count > 1 in the SCL source are registered
// as scalar variables (array semantics are not expanded). This is a
// known limitation documented in the Server type.
func RegisterModel(srv *mms.Server, m *Model, vs *ValueStore) (*ValueStore, error) {
	if vs == nil {
		vs = NewValueStore()
	}

	for i := range m.LogicalDevices {
		ld := &m.LogicalDevices[i]
		if err := srv.RegisterDomain(ld.Name); err != nil {
			return nil, fmt.Errorf("register domain %q: %w", ld.Name, err)
		}

		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			if err := registerLN(srv, vs, ld.Name, ln); err != nil {
				return nil, fmt.Errorf("register LD %q LN %q: %w", ld.Name, ln.Name, err)
			}
		}
	}

	return vs, nil
}

func registerLN(srv *mms.Server, vs *ValueStore, ldName string, ln *LogicalNode) error {
	// Register the full variable hierarchy (LN, FC-group, DO, compound DA levels)
	// before leaf DAs so the ordering in GetNameList matches libiec61850.
	if err := registerLNHierarchy(srv, vs, ldName, ln); err != nil {
		return err
	}

	for _, ds := range ln.DataSets {
		if err := registerDataSet(srv, ldName, ln.Name, &ds); err != nil {
			return err
		}
	}

	for _, rpt := range ln.Reports {
		if err := registerRCB(srv, vs, ldName, ln.Name, &rpt); err != nil {
			return err
		}
	}

	for i := range ln.Logs {
		if err := registerLCB(srv, vs, ldName, ln.Name, &ln.Logs[i]); err != nil {
			return err
		}
	}

	if ln.SettingGroup != nil {
		if err := registerSGCB(srv, vs, ldName, ln.Name, ln.SettingGroup); err != nil {
			return err
		}
	}

	for i := range ln.GoCBs {
		if err := registerGoCB(srv, vs, ldName, ln.Name, &ln.GoCBs[i]); err != nil {
			return err
		}
	}

	for i := range ln.SvCBs {
		if err := registerSvCB(srv, vs, ldName, ln.Name, &ln.SvCBs[i]); err != nil {
			return err
		}
	}

	return nil
}

// registerLNHierarchy registers the full IEC 61850-8-1 variable name hierarchy
// for a LogicalNode. It emits variables in the order expected by libiec61850:
// LN bare name → FC-group → DO-level → compound DA intermediates → leaf DAs.
//
// This allows clients like iec61850bean to use GetNameList(namedVariable) to
// discover LN names (items without '$'), and then call GetVariableAccessAttributes
// on each LN name to obtain the complete nested model tree.
func registerLNHierarchy(srv *mms.Server, vs *ValueStore, ldName string, ln *LogicalNode) error {
	// ---- Phase 1: collect DO→FC→elem mappings ----
	type elemEntry struct {
		ts   mms.TypeSpec
		read func(context.Context) (*mms.Value, error)
	}
	// fcDOs[fc][doName] = structure of DAs for that (FC, DO) pair
	fcDOs := map[string]map[string]elemEntry{}
	fcOrder := []string{}
	doOrders := map[string][]string{} // fc → ordered DO names

	for i := range ln.DataObjects {
		obj := &ln.DataObjects[i]
		doFCs := collectFCsInDO(obj)
		for _, fc := range doFCs {
			elems, reads, err := buildDOElemsForFC(vs, ldName, ln.Name, nil, obj, fc)
			if err != nil {
				return fmt.Errorf("LD %s LN %s DO %s FC %s: %w", ldName, ln.Name, obj.Name, fc, err)
			}
			if len(elems) == 0 {
				continue
			}
			if fcDOs[fc] == nil {
				fcDOs[fc] = map[string]elemEntry{}
				fcOrder = append(fcOrder, fc)
				doOrders[fc] = nil
			}
			doTS := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: elems}
			doReads := reads
			doRead := makeStructRead(doReads)
			fcDOs[fc][obj.Name] = elemEntry{ts: doTS, read: doRead}
			doOrders[fc] = append(doOrders[fc], obj.Name)
		}
	}

	// ---- Phase 2: register LN variable (bare name, no $) ----
	lnElems := make([]mms.TypeSpecElement, 0, len(fcOrder))
	lnReads := make([]func(context.Context) (*mms.Value, error), 0, len(fcOrder))

	for _, fc := range fcOrder {
		doMap := fcDOs[fc]
		doNames := doOrders[fc]

		fcElems := make([]mms.TypeSpecElement, 0, len(doNames))
		fcReads := make([]func(context.Context) (*mms.Value, error), 0, len(doNames))

		for _, doName := range doNames {
			entry := doMap[doName]
			fcElems = append(fcElems, mms.TypeSpecElement{Name: doName, Type: entry.ts})
			fcReads = append(fcReads, entry.read)
		}

		fcTS := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: fcElems}
		fcRead := makeStructRead(fcReads)

		lnElems = append(lnElems, mms.TypeSpecElement{Name: fc, Type: fcTS})
		lnReads = append(lnReads, fcRead)

		// Register FC-group variable: "lnName$FC"
		fcItemID := ln.Name + "$" + fc
		if err := srv.RegisterVariable(mms.Variable{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(ldName),
				ItemID: mms.ItemID(fcItemID),
			},
			TypeSpec: fcTS,
			Read:     fcRead,
		}); err != nil {
			return fmt.Errorf("register FC group %s: %w", fcItemID, err)
		}

		// Register DO-level variables: "lnName$FC$DOname"
		for _, doName := range doNames {
			entry := doMap[doName]
			doItemID := ln.Name + "$" + fc + "$" + doName
			doRead := entry.read
			if err := srv.RegisterVariable(mms.Variable{
				Name: mms.ObjectName{
					Scope:  mms.ObjectScopeDomain,
					Domain: mms.DomainID(ldName),
					ItemID: mms.ItemID(doItemID),
				},
				TypeSpec: entry.ts,
				Read:     doRead,
			}); err != nil {
				return fmt.Errorf("register DO %s: %w", doItemID, err)
			}
		}

		// Register compound DA intermediates and leaf DAs
		for i := range ln.DataObjects {
			obj := &ln.DataObjects[i]
			if err := registerDOWithFC(srv, vs, ldName, ln.Name, nil, obj, fc); err != nil {
				return err
			}
		}
	}

	// ---- Phase 2b: add RP/BR FC groups from Reports into the LN TypeSpec ----
	// IEC 61850-8-1 requires the LN variable to expose its URCB/BRCB blocks under
	// "RP" and "BR" FC groups. Clients like iec61850bean discover RCBs by calling
	// GetVariableAccessAttributes on the bare LN name and parsing the structure.
	type rcbGroupEntry struct {
		ts   mms.TypeSpec
		read func(context.Context) (*mms.Value, error)
	}
	rcbGroups := map[string][]rcbGroupEntry{} // "RP" or "BR" → list of RCB entries
	rcbGroupOrder := map[string][]string{}    // "RP"/"BR" → ordered RCB names

	for i := range ln.Reports {
		rpt := &ln.Reports[i]
		prefix := "RP"
		if rpt.Buffered {
			prefix = "BR"
		}
		rptID := rpt.RptID
		if rptID == "" {
			rptID = ldName + "/" + ln.Name + "$" + prefix + "$" + rpt.Name
		}
		fields := buildRCBFields(rpt, rptID, ldName, ln.Name)
		rcbElems := make([]mms.TypeSpecElement, len(fields))
		rcbFieldReads := make([]func(context.Context) (*mms.Value, error), len(fields))
		for j, f := range fields {
			rcbElems[j] = mms.TypeSpecElement{Name: f.suffix, Type: f.typeSpec}
			// Read from the value store using the same key as registerRCB.
			subItemID := ln.Name + "$" + prefix + "$" + rpt.Name + "$" + f.suffix
			subSK := StoreKey(ldName, subItemID)
			subTS := f.typeSpec
			rcbFieldReads[j] = func(_ context.Context) (*mms.Value, error) {
				v := vs.Get(subSK)
				if v != nil {
					return v, nil
				}
				return defaultMMSValue(subTS), nil
			}
		}
		rcbTS := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: rcbElems}
		rcbRead := makeStructRead(rcbFieldReads)
		rcbGroups[prefix] = append(rcbGroups[prefix], rcbGroupEntry{ts: rcbTS, read: rcbRead})
		rcbGroupOrder[prefix] = append(rcbGroupOrder[prefix], rpt.Name)
	}

	// Append RP then BR groups to the LN elements (in a deterministic order).
	for _, prefix := range []string{"RP", "BR"} {
		entries := rcbGroups[prefix]
		names := rcbGroupOrder[prefix]
		if len(entries) == 0 {
			continue
		}
		rcbFCElems := make([]mms.TypeSpecElement, len(entries))
		rcbFCReads := make([]func(context.Context) (*mms.Value, error), len(entries))
		for i, e := range entries {
			rcbFCElems[i] = mms.TypeSpecElement{Name: names[i], Type: e.ts}
			rcbFCReads[i] = e.read
		}
		rcbFCTS := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: rcbFCElems}
		rcbFCRead := makeStructRead(rcbFCReads)
		lnElems = append(lnElems, mms.TypeSpecElement{Name: prefix, Type: rcbFCTS})
		lnReads = append(lnReads, rcbFCRead)

		// Register the FC-group variable "lnName$RP" or "lnName$BR"
		// so GetNameList also exposes it as a named variable.
		fcItemID := ln.Name + "$" + prefix
		if err := srv.RegisterVariable(mms.Variable{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(ldName),
				ItemID: mms.ItemID(fcItemID),
			},
			TypeSpec: rcbFCTS,
			Read:     rcbFCRead,
		}); err != nil {
			return fmt.Errorf("register RCB FC group %s: %w", fcItemID, err)
		}
	}

	// ---- Phase 2b': LG FC group (log control blocks, IEC 61850-8-1
	// §17.3.3.2) ----
	if elem, read, ok, err := registerLGGroup(srv, vs, ldName, ln); err != nil {
		return err
	} else if ok {
		lnElems = append(lnElems, elem)
		lnReads = append(lnReads, read)
	}

	// ---- Phase 2c: GO FC group (GOOSE control blocks) ----
	if elem, read, ok, err := registerGOGroup(srv, vs, ldName, ln); err != nil {
		return err
	} else if ok {
		lnElems = append(lnElems, elem)
		lnReads = append(lnReads, read)
	}

	// ---- Phase 2d: MS FC group (multicast SV control blocks) ----
	if elem, read, ok, err := registerMSGroup(srv, vs, ldName, ln); err != nil {
		return err
	} else if ok {
		lnElems = append(lnElems, elem)
		lnReads = append(lnReads, read)
	}

	// IEC 61850-8-1 Figure 4: the FC components of the LN in the
	// recommended order (SRV-07); unknown FCs keep their order after them.
	order := make([]int, len(lnElems))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return fcComponentRank(lnElems[a].Name) - fcComponentRank(lnElems[b].Name)
	})
	sortedElems := make([]mms.TypeSpecElement, len(order))
	sortedReads := make([]func(context.Context) (*mms.Value, error), len(order))
	for i, j := range order {
		sortedElems[i], sortedReads[i] = lnElems[j], lnReads[j]
	}
	lnElems, lnReads = sortedElems, sortedReads

	// Register LN variable (bare name)
	lnTS := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: lnElems}
	lnRead := makeStructRead(lnReads)
	if err := srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(ln.Name),
		},
		TypeSpec: lnTS,
		Read:     lnRead,
	}); err != nil {
		return fmt.Errorf("register LN %s: %w", ln.Name, err)
	}

	return nil
}

// fcComponentOrder is the recommended order of the FC components of a
// logical node (IEC 61850-8-1 Figure 4).
var fcComponentOrder = []string{"MX", "ST", "CO", "CF", "DC", "SP", "SG", "RP", "LG", "BR", "GO", "GS", "SV", "SE", "MS", "US", "EX", "SR", "OR", "BL"}

func fcComponentRank(fc string) int {
	if i := slices.Index(fcComponentOrder, fc); i >= 0 {
		return i
	}
	return len(fcComponentOrder)
}

// makeStructRead returns a read function that reads all child values and
// returns them as a single MMS structure value.
func makeStructRead(reads []func(context.Context) (*mms.Value, error)) func(context.Context) (*mms.Value, error) {
	return func(ctx context.Context) (*mms.Value, error) {
		vals := make([]*mms.Value, len(reads))
		for i, fn := range reads {
			v, err := fn(ctx)
			if err != nil {
				return nil, err
			}
			vals[i] = v
		}
		return mms.NewStructure(vals), nil
	}
}

// collectFCsInDO returns the unique FCs present in a DataObject (recursively),
// preserving first-seen order.
func collectFCsInDO(obj *DataObject) []string {
	seen := map[string]bool{}
	order := []string{}
	collectFCsInDOHelper(obj, seen, &order)
	return order
}

func collectFCsInDOHelper(obj *DataObject, seen map[string]bool, order *[]string) {
	for _, attr := range obj.Attributes {
		if !seen[attr.FC] {
			seen[attr.FC] = true
			*order = append(*order, attr.FC)
		}
	}
	for i := range obj.Children {
		collectFCsInDOHelper(&obj.Children[i], seen, order)
	}
}

// buildDOElemsForFC builds TypeSpecElements and read functions for all DAs
// (and compound DA structures) of obj that have the given FC.
// parentPath is the path of ancestor DO names (nil for top-level DOs).
func buildDOElemsForFC(vs *ValueStore, ldName, lnName string, parentPath []string, obj *DataObject, fc string) ([]mms.TypeSpecElement, []func(context.Context) (*mms.Value, error), error) {
	attrPath := append(append([]string(nil), parentPath...), obj.Name)

	var elems []mms.TypeSpecElement
	var reads []func(context.Context) (*mms.Value, error)

	// Direct DAs with this FC
	for i := range obj.Attributes {
		attr := &obj.Attributes[i]
		effectiveFC := attr.FC
		if effectiveFC == "" {
			effectiveFC = fc
		}
		if effectiveFC != fc {
			continue
		}
		elem, read, err := buildAttrElemAndRead(vs, ldName, lnName, attrPath, attr)
		if err != nil {
			return nil, nil, err
		}
		elems = append(elems, elem)
		reads = append(reads, read)
	}

	// Sub-DOs
	for i := range obj.Children {
		child := &obj.Children[i]
		childElems, childReads, err := buildDOElemsForFC(vs, ldName, lnName, attrPath, child, fc)
		if err != nil {
			return nil, nil, err
		}
		if len(childElems) > 0 {
			childTS := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: childElems}
			childReadFns := childReads
			childRead := makeStructRead(childReadFns)
			elems = append(elems, mms.TypeSpecElement{Name: child.Name, Type: childTS})
			reads = append(reads, childRead)
		}
	}

	return elems, reads, nil
}

// buildAttrElemAndRead builds a TypeSpecElement and read function for a
// DataAttribute (leaf or compound).
func buildAttrElemAndRead(vs *ValueStore, ldName, lnName string, path []string, attr *DataAttribute) (mms.TypeSpecElement, func(context.Context) (*mms.Value, error), error) {
	if len(attr.Children) == 0 {
		ts, ok := btypeToMMS[attr.BType]
		if !ok {
			return mms.TypeSpecElement{}, nil, fmt.Errorf("unsupported BType %q for %s/%s", attr.BType, ldName, lnName)
		}
		fullPath := append(append([]string(nil), path...), attr.Name)
		itemID := lnName + "$" + attr.FC + "$" + strings.Join(fullPath, "$")
		sk := StoreKey(ldName, itemID)
		ts0 := ts
		read := func(_ context.Context) (*mms.Value, error) {
			if v := vs.Get(sk); v != nil {
				return v, nil
			}
			return defaultMMSValue(ts0), nil
		}
		return mms.TypeSpecElement{Name: attr.Name, Type: ts}, read, nil
	}

	childPath := append(append([]string(nil), path...), attr.Name)
	childElems := make([]mms.TypeSpecElement, 0, len(attr.Children))
	childReads := make([]func(context.Context) (*mms.Value, error), 0, len(attr.Children))

	for i := range attr.Children {
		child := &attr.Children[i]
		eff := *child
		if eff.FC == "" {
			eff.FC = attr.FC
		}
		elem, read, err := buildAttrElemAndRead(vs, ldName, lnName, childPath, &eff)
		if err != nil {
			return mms.TypeSpecElement{}, nil, err
		}
		childElems = append(childElems, elem)
		childReads = append(childReads, read)
	}

	ts := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: childElems}
	childReadFns := childReads
	read := makeStructRead(childReadFns)
	return mms.TypeSpecElement{Name: attr.Name, Type: ts}, read, nil
}

// registerDOWithFC registers compound DA intermediates and leaf DAs for a
// DataObject restricted to a single FC. This mirrors registerDO but only
// emits items whose FC matches the given constraint.
func registerDOWithFC(srv *mms.Server, vs *ValueStore, ldName, lnName string, parentPath []string, obj *DataObject, fc string) error {
	attrPath := append(append([]string(nil), parentPath...), obj.Name)

	// Sub-DOs first
	for i := range obj.Children {
		if err := registerDOWithFC(srv, vs, ldName, lnName, attrPath, &obj.Children[i], fc); err != nil {
			return err
		}
	}

	// Attributes with this FC
	for i := range obj.Attributes {
		attr := &obj.Attributes[i]
		effectiveFC := attr.FC
		if effectiveFC == "" {
			effectiveFC = fc
		}
		if effectiveFC != fc {
			continue
		}
		if err := registerDAWithFC(srv, vs, ldName, lnName, attrPath, attr); err != nil {
			return err
		}
	}
	return nil
}

// registerDAWithFC registers a DataAttribute (and its intermediate compound
// levels) with the MMS server. For compound DAs (with children) it first
// registers the compound variable, then recurses for the children.
func registerDAWithFC(srv *mms.Server, vs *ValueStore, ldName, lnName string, path []string, attr *DataAttribute) error {
	if len(attr.Children) == 0 {
		// Leaf DA — use the existing registerDA helper.
		return registerDA(srv, vs, ldName, lnName, path, attr)
	}

	// Compound DA: register the compound variable first.
	childPath := append(append([]string(nil), path...), attr.Name)
	elem, read, err := buildAttrElemAndRead(vs, ldName, lnName, path, attr)
	if err != nil {
		return err
	}
	fullPath := append(append([]string(nil), path...), attr.Name)
	itemID := lnName + "$" + attr.FC + "$" + strings.Join(fullPath, "$")
	storeKey := StoreKey(ldName, itemID)
	var write func(context.Context, *mms.Value) error
	compoundTS := elem.Type
	switch {
	case attr.FC == "CO":
		// Control services are writes of the whole Oper, SBOw or Cancel
		// structure (IEC 61850-8-1 20.6.3, 20.7.3, 20.8.3); a component of
		// them (Oper$origin) is not writable (SRV-03). The control model
		// decides before the value is kept.
		if isControlServiceAttr(attr.Name) {
			write = func(ctx context.Context, val *mms.Value) error {
				if err := CheckWriteValue(val, compoundTS); err != nil {
					return fmt.Errorf("write %s: %w", storeKey, err)
				}
				if _, err := vs.callInterceptor(ctx, storeKey, val); err != nil {
					return err
				}
				vs.Set(storeKey, val)
				return nil
			}
		}
	case vs.allowReadOnlyFCWrites || clientWritableFC(attr.FC):
		// A write of a constructed attribute sets its leaves, as reads of
		// it and of its leaves are built from them; SetEditSGValue of an
		// FC=SE attribute (IEC 61850-7-2 16.3.4.2.1, an FCDA such as
		// StrVal.setMag) confirms the edit buffer leaf by leaf. Before
		// v1.18.0 the value was kept under the compound key, where no read
		// found it.
		fc := attr.FC
		write = func(ctx context.Context, val *mms.Value) error {
			if err := vs.checkEdit(ctx, fc, storeKey, true); err != nil {
				return err
			}
			// The value must match the declared structure (IEC 61850-8-1
			// 8.1.3.4.4.2): before v1.17.0 any value was stored.
			if err := CheckWriteValue(val, compoundTS); err != nil {
				return fmt.Errorf("write %s: %w", storeKey, err)
			}
			leaves, err := vs.compoundLeaves(ctx, nil, storeKey, attr, compoundTS, val)
			if err != nil {
				return fmt.Errorf("write %s: %w", storeKey, err)
			}
			// Each leaf is stored and then notified, as a leaf write is:
			// the report, GOOSE, SV and journal engines index data set
			// members by leaf key, and the report engine judges a
			// trigger option by the change of the notified leaf against
			// the member value at the previous notification. Storing all
			// leaves first would let a leaf without dchg hide the change
			// of the next one.
			var first error
			for _, l := range leaves {
				vs.Set(l.key, l.val)
				if _, err := vs.callInterceptor(ctx, l.key, l.val); err != nil && first == nil {
					first = err
				}
			}
			return first
		}
	}
	if attr.FC == "SE" {
		baseRead, fc := read, attr.FC
		read = func(ctx context.Context) (*mms.Value, error) {
			if err := vs.checkEdit(ctx, fc, storeKey, false); err != nil {
				return nil, err
			}
			return baseRead(ctx)
		}
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(itemID),
		},
		TypeSpec: elem.Type,
		Read:     read,
		Write:    write,
	}); err != nil {
		return fmt.Errorf("register compound DA %s: %w", itemID, err)
	}

	// Then recurse for children.
	for i := range attr.Children {
		child := &attr.Children[i]
		eff := *child
		if eff.FC == "" {
			eff.FC = attr.FC
		}
		if err := registerDAWithFC(srv, vs, ldName, lnName, childPath, &eff); err != nil {
			return err
		}
	}
	return nil
}

func registerDA(srv *mms.Server, vs *ValueStore, ldName, lnName string, path []string, attr *DataAttribute) error {
	if len(attr.Children) > 0 {
		childPath := append(append([]string(nil), path...), attr.Name)
		for _, child := range attr.Children {
			childWithFC := child
			if childWithFC.FC == "" {
				childWithFC.FC = attr.FC
			}
			if err := registerDA(srv, vs, ldName, lnName, childPath, &childWithFC); err != nil {
				return err
			}
		}
		return nil
	}

	fullPath := append(append([]string(nil), path...), attr.Name)
	itemID := lnName + "$" + attr.FC + "$" + strings.Join(fullPath, "$")

	typeSpec, ok := btypeToMMS[attr.BType]
	if !ok {
		return fmt.Errorf("unsupported BType %q for %s/%s", attr.BType, ldName, itemID)
	}
	storeKey := StoreKey(ldName, itemID)

	if attr.InitialValue != "" {
		iv, err := parseInitialValue(attr.BType, attr.InitialValue, attr.EnumNames)
		if err != nil {
			return fmt.Errorf("initial value for %s/%s: %w", ldName, itemID, err)
		}
		if attr.BType == "Enum" && len(attr.EnumValues) > 0 {
			if i, ok := iv.Int64(); ok {
				valid := slices.Contains(attr.EnumValues, int(i))
				if !valid {
					return fmt.Errorf("initial value for %s/%s: enum ordinal %d not in EnumType", ldName, itemID, i)
				}
			}
		}
		vs.Set(storeKey, iv)
	}

	ts := typeSpec
	v := mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(itemID),
		},
		TypeSpec: typeSpec,
		Read: func(ctx context.Context) (*mms.Value, error) {
			if err := vs.checkEdit(ctx, attr.FC, storeKey, false); err != nil {
				return nil, err
			}
			val := vs.Get(storeKey)
			if val != nil {
				return val, nil
			}
			return defaultMMSValue(ts), nil
		},
		Write: func(ctx context.Context, val *mms.Value) error {
			if err := vs.checkEdit(ctx, attr.FC, storeKey, true); err != nil {
				return err
			}
			// A refused write carries the DataAccessError of IEC 61850-8-1
			// Table 23 (type-inconsistent, object-value-invalid); before
			// v1.17.0 every refusal became temporarily-unavailable.
			if err := CheckWriteValue(val, ts); err != nil {
				return fmt.Errorf("write %s: %w", storeKey, err)
			}
			if err := checkEnumValue(val, attr); err != nil {
				return fmt.Errorf("write %s: %w", storeKey, err)
			}
			if err := vs.checkWrite(ctx, storeKey); err != nil {
				return err
			}
			vs.Set(storeKey, val)
			// Notify any installed interceptor (e.g. the ReportEngine) that
			// a client has successfully written this data attribute. The
			// value is already in the store at this point so interceptors
			// read the new value when building reports.
			if handled, err := vs.callInterceptor(ctx, storeKey, val); handled || err != nil {
				return err
			}
			return nil
		},
	}
	if !vs.allowReadOnlyFCWrites && !clientWritableFC(attr.FC) || attr.FC == "CO" {
		// go-mms answers object-access-denied. A CO leaf is a component of
		// Oper/SBOw/Cancel, or the read-only SBO (IEC 61850-8-1 20.5: Select
		// is a Read of SBO); writing it bypassed the control model.
		v.Write = nil
	}

	return srv.RegisterVariable(v)
}

// leafWrite is one leaf of a client write of a constructed attribute.
type leafWrite struct {
	key string
	val *mms.Value
}

// compoundLeaves appends to out the leaves of val, a value of the
// constructed attribute attr (type ts) under key, already checked against
// ts. Each leaf passes the checks of a leaf write (enumeration range,
// write guard) before any is stored, so a refused leaf refuses the whole
// write.
func (vs *ValueStore) compoundLeaves(ctx context.Context, out []leafWrite, key string, attr *DataAttribute, ts mms.TypeSpec, val *mms.Value) ([]leafWrite, error) {
	if len(attr.Children) == 0 {
		if err := checkEnumValue(val, attr); err != nil {
			return nil, err
		}
		if err := vs.checkWrite(ctx, key); err != nil {
			return nil, err
		}
		return append(out, leafWrite{key: key, val: val}), nil
	}
	members, _ := val.Structure()
	for i := range attr.Children {
		if i >= len(members) || i >= len(ts.Elements) {
			break
		}
		var err error
		if out, err = vs.compoundLeaves(ctx, out, key+"$"+attr.Children[i].Name, &attr.Children[i], ts.Elements[i].Type, members[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// isControlServiceAttr reports whether name is a control service
// attribute written as a whole (IEC 61850-8-1 20.6-20.8).
func isControlServiceAttr(name string) bool {
	return name == "Oper" || name == "SBOw" || name == "Cancel"
}

// clientWritableFC reports whether a client may write data attributes of
// functional constraint fc with SetDataValues. Status (ST), measurands
// (MX), setting-group values (SG, edited through SE and the SGCB),
// service responses (SR), operate-received (OR) and extended definitions
// (EX) are read-only to clients (IEC 61850-7-2, functional constraints);
// the server application changes them with Server.SetValue. Before this
// check any client could overwrite, for example, a breaker's Pos.stVal.
func clientWritableFC(fc string) bool {
	switch fc {
	case "ST", "MX", "SG", "SR", "OR", "EX":
		return false
	}
	return true
}

func registerDataSet(srv *mms.Server, ldName, lnName string, ds *DataSetDef) error {
	nvlName := lnName + "$" + ds.Name
	vars := make([]mms.VariableSpec, len(ds.Members))

	for i, m := range ds.Members {
		memberLD := ldName
		if m.LDInst != "" {
			memberLD = m.LDInst
		}

		itemID := m.LNName + "$" + m.FC + "$" + strings.ReplaceAll(m.DOPath, ".", "$")

		vars[i] = mms.VariableSpec{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(memberLD),
				ItemID: mms.ItemID(itemID),
			},
		}
	}

	return srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(nvlName),
		},
		Variables: vars,
	})
}

// rcbField describes a single RCB subfield for registration.
type rcbField struct {
	suffix   string
	typeSpec mms.TypeSpec
	initial  *mms.Value
}

// buildRCBFields returns the ordered list of rcbField descriptors for a
// URCB (Buffered=false) or BRCB (Buffered=true).  Used both by
// registerRCB (to register MMS variables) and by registerLNHierarchy (to
// include the RCB TypeSpec in the LN-level variable so that clients like
// iec61850bean can discover RCBs via GetVariableAccessAttributes on the LN).
func buildRCBFields(rpt *ReportDef, rptID, ldName, lnName string) []rcbField {
	// Per IEC 61850-8-1, the DatSet attribute in an RCB must be a
	// domain-qualified reference of the form "domain/LNName$dsName".
	// If the SCL only supplies the local dataset name, qualify it here.
	datSet := rpt.DatSet
	if datSet != "" && !strings.Contains(datSet, "/") {
		datSet = ldName + "/" + lnName + "$" + datSet
	}
	fields := []rcbField{
		{"RptID", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(rptID)},
		{"RptEna", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
	}
	if !rpt.Buffered {
		fields = append(fields, rcbField{
			"Resv", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false),
		})
	}
	fields = append(fields,
		rcbField{"DatSet", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(datSet)},
		rcbField{"ConfRev", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.ConfRev))},
		rcbField{"OptFlds", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 10}, encodeOptFieldsDef(rcbOptFields(rpt))},
		rcbField{"BufTm", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.BufTime))},
		// IEC 61850-7-2 Table 37 (BRCB SqNum INT16U) and §17.2.4.1 (URCB
		// SqNum INT8U).
		rcbField{"SqNum", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: sqNumBits(rpt.Buffered)}, mms.NewUnsigned(0)},
		rcbField{"TrgOps", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 6}, encodeTrgOpsDef(rpt.TrgOps)},
		rcbField{"IntgPd", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.IntgPd))},
		rcbField{"GI", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
	)
	if rpt.Buffered {
		fields = append(fields,
			rcbField{"PurgeBuf", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
			rcbField{"EntryID", mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 8}, mms.NewOctetString(make([]byte, 8))},
			rcbField{"TimeOfEntry", mms.TypeSpec{Type: mms.ValueTypeBinaryTime}, emptyBinaryTime()},
		)
	}
	return fields
}

// sqNumBits is the SqNum size of a BRCB (INT16U) or URCB (INT8U).
func sqNumBits(buffered bool) int {
	if buffered {
		return 16
	}
	return 8
}

// rcbOptFields returns the OptFlds a client reads back: a URCB never
// reports buffer-overflow or entryID, so both read back as false
// (IEC 61850-8-1 Table 63).
func rcbOptFields(rpt *ReportDef) OptFieldsDef {
	o := rpt.OptFlds
	if !rpt.Buffered {
		o.BufOvfl = false
		o.EntryID = false
	}
	return o
}

// rcbReadOnly are the RCB attributes a client may not write
// (IEC 61850-8-1 Table 61/63 "May not be written"; IEC 61850-7-2 Table 37
// "r"). go-mms answers a write with object-access-denied (8-1
// §8.1.3.4.4.2).
var rcbReadOnly = map[string]bool{"ConfRev": true, "SqNum": true, "TimeOfEntry": true, "Owner": true}

// rcbWriteErr is the DataAccessError for a write whose type or size does
// not match the RCB attribute (IEC 61850-8-1 §8.1.3.4.4.2, Table 23
// type-conflict).
func rcbWriteErr(item string, err error) error {
	return fmt.Errorf("RCB write %s: %w: %w", item, err, &mms.DataAccessError{Code: mms.DataAccessErrorTypeInconsistent})
}

// registerRCB registers an RCB structure variable and its individual
// subfield variables with the MMS server.
//
// Supported subfields: RptID, RptEna, DatSet, ConfRev, OptFlds,
// BufTm, SqNum, TrgOps, IntgPd, GI.
// URCB-specific: Resv.
// BRCB-specific: PurgeBuf, EntryID, TimeOfEntry.
// Not modelled: Owner and ResvTms (both optional, IEC 61850-7-2 Table 37
// c3/c4).
//
// ConfRev, SqNum and TimeOfEntry are read-only. A write of a writable
// subfield is type- and size-checked here and then handed to the write
// interceptor (the report engine), which applies the IEC 61850-7-2 §17.2
// rules and commits the value; without an interceptor the value is
// stored as written. A write of the whole RCB structure is refused with
// object-access-denied, because the structure has read-only components
// (IEC 61850-8-1 §8.1.3.4.4.2).
func registerRCB(srv *mms.Server, vs *ValueStore, ldName, lnName string, rpt *ReportDef) error {
	prefix := "RP"
	if rpt.Buffered {
		prefix = "BR"
	}
	rcbItemID := lnName + "$" + prefix + "$" + rpt.Name

	rptID := rpt.RptID
	if rptID == "" {
		rptID = ldName + "/" + rcbItemID
	}

	fields := buildRCBFields(rpt, rptID, ldName, lnName)

	structElems := make([]mms.TypeSpecElement, len(fields))
	for i, f := range fields {
		structElems[i] = mms.TypeSpecElement{Name: f.suffix, Type: f.typeSpec}

		subItemID := rcbItemID + "$" + f.suffix
		sk := StoreKey(ldName, subItemID)
		vs.Set(sk, f.initial)

		subSK := sk
		subTS := f.typeSpec
		v := mms.Variable{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(ldName),
				ItemID: mms.ItemID(subItemID),
			},
			TypeSpec: f.typeSpec,
			Read: func(_ context.Context) (*mms.Value, error) {
				val := vs.Get(subSK)
				if val != nil {
					return val, nil
				}
				return defaultMMSValue(subTS), nil
			},
			Write: func(ctx context.Context, val *mms.Value) error {
				if val == nil || val.Type() != subTS.Type {
					return rcbWriteErr(subItemID, fmt.Errorf("type mismatch"))
				}
				if err := validateWriteSize(val, subTS); err != nil {
					return rcbWriteErr(subItemID, err)
				}
				// The interceptor validates and commits the write; a
				// rejection leaves the store unchanged.
				if handled, err := vs.callInterceptor(ctx, subSK, val); err != nil || handled {
					return err
				}
				vs.Set(subSK, val)
				return nil
			},
		}
		if rcbReadOnly[f.suffix] {
			v.Write = nil // go-mms answers object-access-denied
		}
		if err := srv.RegisterVariable(v); err != nil {
			return fmt.Errorf("register RCB subfield %s: %w", subItemID, err)
		}
	}

	rcbTypeSpec := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: structElems}

	return srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(rcbItemID),
		},
		TypeSpec: rcbTypeSpec,
		Read: func(_ context.Context) (*mms.Value, error) {
			elems := make([]*mms.Value, len(fields))
			for i, f := range fields {
				subKey := StoreKey(ldName, rcbItemID+"$"+f.suffix)
				v := vs.Get(subKey)
				if v == nil {
					v = defaultMMSValue(f.typeSpec)
				}
				elems[i] = v
			}
			return mms.NewStructure(elems), nil
		},
		// IEC 61850-8-1 §8.1.3.4.4.2: a write that covers any read-only
		// part is answered with object-access-denied; the RCB
		// structure contains ConfRev, SqNum (and TimeOfEntry). Clients
		// write the attributes one by one (SetBRCBValues, 8-1 §17.2.3).
		Write: func(context.Context, *mms.Value) error {
			return &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
		},
	})
}

// sameSGCBMember compares a written SGCB member with the current one. A
// UtcTime (LActTm) that went over the wire keeps only a 24-bit fraction
// of a second (IEC 61850-8-1 8.1.3.7), so times within a millisecond, of
// any TimeQuality, are the same.
func sameSGCBMember(cur, written *mms.Value) bool {
	if ct, ok := cur.UTCTime(); ok {
		wt, ok := written.UTCTime()
		if !ok {
			return false
		}
		d := ct.Sub(wt)
		return d > -time.Millisecond && d < time.Millisecond
	}
	return cur.Equal(written)
}

// sgcbWritable are the SGCB attributes a client may write.
var sgcbWritable = map[string]bool{"ActSG": true, "EditSG": true, "CnfEdit": true}

func registerSGCB(srv *mms.Server, vs *ValueStore, ldName, lnName string, sg *SettingGroupDef) error {
	sgcbItemID := lnName + "$SP$SGCB"

	fields := []rcbField{
		// The component is NumOfSG (IEC 61850-8-1 Table 60, IEC 61850-7-2
		// Table 36); before v1.17.0 it was served as NumOfSGs.
		{"NumOfSG", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}, mms.NewUnsigned(uint64(sg.NumOfSGs))},
		{"ActSG", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}, mms.NewUnsigned(uint64(sg.ActSG))},
		{"EditSG", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}, mms.NewUnsigned(0)},
		{"CnfEdit", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
		{"LActTm", mms.TypeSpec{Type: mms.ValueTypeUTCTime}, mms.NewUTCTime(time.Unix(0, 0).UTC())},
		{"ResvTms", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}, mms.NewUnsigned(uint64(sg.ResvTms))},
	}

	structElems := make([]mms.TypeSpecElement, len(fields))
	for i, f := range fields {
		structElems[i] = mms.TypeSpecElement{Name: f.suffix, Type: f.typeSpec}

		subItemID := sgcbItemID + "$" + f.suffix
		sk := StoreKey(ldName, subItemID)
		vs.Set(sk, f.initial)

		subSK := sk
		subTS := f.typeSpec
		if err := srv.RegisterVariable(mms.Variable{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(ldName),
				ItemID: mms.ItemID(subItemID),
			},
			TypeSpec: f.typeSpec,
			Read: func(_ context.Context) (*mms.Value, error) {
				val := vs.Get(subSK)
				if val != nil {
					return val, nil
				}
				return defaultMMSValue(subTS), nil
			},
			Write: func(ctx context.Context, val *mms.Value) error {
				if handled, err := vs.callInterceptor(ctx, subSK, val); handled || err != nil {
					return err
				}
				vs.Set(subSK, val)
				return nil
			},
		}); err != nil {
			return fmt.Errorf("register SGCB subfield %s: %w", subItemID, err)
		}
	}

	sgcbTypeSpec := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: structElems}

	return srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(sgcbItemID),
		},
		TypeSpec: sgcbTypeSpec,
		Read: func(_ context.Context) (*mms.Value, error) {
			elems := make([]*mms.Value, len(fields))
			for i, f := range fields {
				subKey := StoreKey(ldName, sgcbItemID+"$"+f.suffix)
				v := vs.Get(subKey)
				if v == nil {
					v = defaultMMSValue(f.typeSpec)
				}
				elems[i] = v
			}
			return mms.NewStructure(elems), nil
		},
		// A write of the whole SGCB, which is also how a write with
		// alternate access to one component arrives, goes through the
		// interceptor for every changed writable subfield (ActSG,
		// EditSG, CnfEdit), in the order of the structure, as a write
		// of that subfield would. NumOfSG, LActTm and ResvTms are
		// read-only (IEC 61850-7-2) and keep their values.
		Write: func(ctx context.Context, val *mms.Value) error {
			elems, ok := val.Structure()
			if !ok || len(elems) != len(fields) {
				return writeRefused(mms.DataAccessErrorTypeInconsistent, "SGCB write: expected structure with %d elements", len(fields))
			}
			// Changing a read-only member is object-access-denied (IEC
			// 61850-8-1 8.1.3.4.4.2: a read-only sub-component).
			changed := false
			for i, f := range fields {
				cur := vs.Get(StoreKey(ldName, sgcbItemID+"$"+f.suffix))
				if cur == nil || sameSGCBMember(cur, elems[i]) {
					continue
				}
				if !sgcbWritable[f.suffix] {
					return writeRefused(mms.DataAccessErrorObjectAccessDenied, "SGCB write: %s is read-only", f.suffix)
				}
				changed = true
			}
			for i, f := range fields {
				if !sgcbWritable[f.suffix] {
					continue
				}
				subKey := StoreKey(ldName, sgcbItemID+"$"+f.suffix)
				// An unchanged EditSG still goes to the interceptor when
				// nothing else changed: a SelectEditSG of the group already
				// selected, which only the reservation holder may issue
				// (IEC 61850-7-2 16.3.3.1).
				if cur := vs.Get(subKey); cur != nil && cur.Equal(elems[i]) && (changed || f.suffix != "EditSG") {
					continue
				}
				if handled, err := vs.callInterceptor(ctx, subKey, elems[i]); err != nil {
					return err
				} else if handled {
					continue
				}
				vs.Set(subKey, elems[i])
			}
			return nil
		},
	})
}

// validateWriteSize performs lightweight size/range validation for
// typed writes against the declared TypeSpec. This catches obviously
// wrong values (e.g., a 32-byte octet string written to an Octet6
// field) without being overly restrictive.
func validateWriteSize(val *mms.Value, ts mms.TypeSpec) error {
	switch ts.Type {
	case mms.ValueTypeVisibleString:
		if ts.Size > 0 {
			if s, ok := val.VisibleString(); ok && len(s) > ts.Size {
				return fmt.Errorf("visible string length %d exceeds max %d", len(s), ts.Size)
			}
		}
	case mms.ValueTypeMmsString:
		// The size of an MMS string (Unicode255) counts characters.
		if ts.Size > 0 {
			if s, ok := val.MmsString(); ok {
				if n := utf8.RuneCountInString(s); n > ts.Size {
					return fmt.Errorf("MMS string length %d characters exceeds max %d", n, ts.Size)
				}
			}
		}
	case mms.ValueTypeOctetString:
		if ts.Size > 0 {
			if bs, ok := val.OctetString(); ok && len(bs) > ts.Size {
				return fmt.Errorf("octet string length %d exceeds max %d", len(bs), ts.Size)
			}
		}
	case mms.ValueTypeBitString:
		if ts.Size > 0 {
			if bl, ok := val.BitStringLength(); ok && bl != ts.Size {
				return fmt.Errorf("bit string length %d != expected %d", bl, ts.Size)
			}
		}
	default:
	}
	return nil
}

// parseInitialValue converts an SCL InitialValue string to an [mms.Value]
// for the given basic type. Returns an error for unsupported or invalid
// conversions rather than silently discarding the value.
//
// enumNames, when non-nil, maps SCL enumeration value names to their
// ordinals and is used to resolve Enum values specified as strings
// (e.g. "direct-with-normal-security") instead of ordinals.
func parseInitialValue(btype, val string, enumNames map[string]int) (*mms.Value, error) {
	switch btype {
	case "BOOLEAN":
		switch strings.ToLower(val) {
		case "true", "1":
			return mms.NewBoolean(true), nil
		case "false", "0":
			return mms.NewBoolean(false), nil
		default:
			return nil, fmt.Errorf("invalid BOOLEAN value %q", val)
		}

	case "INT8", "INT16", "INT24", "INT32", "INT64", "INT128":
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer value %q for %s: %w", val, btype, err)
		}
		return mms.NewInteger(i), nil

	case "Enum":
		// The SCL value of an enumerated attribute is the EnumVal literal
		// (IEC 61850-6, Val of an Enum DA), e.g. "direct-with-normal-security".
		// Literals can be numbers ("9600" of a baud-rate EnumType whose
		// ordinal is 6), so the literal is looked up first; a numeric string
		// that is no literal is taken as the ordinal.
		if ord, ok := enumNames[val]; ok {
			return mms.NewInteger(int64(ord)), nil
		}
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return mms.NewInteger(i), nil
		}
		return nil, fmt.Errorf("invalid integer value %q for %s: not a valid ordinal or enum name", val, btype)

	case "INT8U", "INT16U", "INT24U", "INT32U":
		u, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid unsigned value %q for %s: %w", val, btype, err)
		}
		return mms.NewUnsigned(u), nil

	case "FLOAT32", "FLOAT64":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float value %q for %s: %w", val, btype, err)
		}
		return mms.NewFloat(f), nil

	case "VisString32", "VisString64", "VisString65", "VisString129", "VisString255", "ObjRef", "Currency":
		return mms.NewVisibleString(val), nil

	case "Unicode255":
		return mms.NewMmsString(val), nil

	case "Quality":
		return parseInitialBitString(val, 13)

	case "Dbpos":
		// SCL gives a double point as its literal (IEC 61850-7-3 DPS
		// stVal); on the wire it is BIT STRING(2), intermediate-state 00,
		// off 01, on 10, bad-state 11 (IEC 61850-8-1), as DbposValue writes.
		if b, ok := dbposLiterals[val]; ok {
			return mms.NewBitStringWithLength([]byte{b}, 2), nil
		}
		return parseInitialBitString(val, 2)

	case "Tcmd":
		// CODED ENUM stop(0) | lower(1) | higher(2) | reserved(3) (IEC
		// 61850-7-3 BSC), encoded as the unsigned value in BIT STRING(2),
		// first bit in the high bit of the octet (IEC 61850-8-1 8.1.2.3).
		if b, ok := tcmdLiterals[val]; ok {
			return mms.NewBitStringWithLength([]byte{b}, 2), nil
		}
		u, err := strconv.ParseUint(val, 10, 8)
		if err != nil || u > 3 {
			return nil, fmt.Errorf("invalid Tcmd value %q", val)
		}
		return mms.NewBitStringWithLength([]byte{byte(u) << 6}, 2), nil

	case "SvOptFlds":
		return parseInitialBitString(val, 5)

	case "EntryID":
		return parseInitialOctetString(val, 8)

	case "Check":
		return parseInitialBitString(val, 2)

	case "OptFlds":
		return parseInitialBitString(val, 10)

	case "TrgOps":
		return parseInitialBitString(val, 6)

	case "Timestamp":
		if val != "" {
			return nil, fmt.Errorf("timestamp initial value parsing not yet supported (got %q); use zero default", val)
		}
		return mms.NewUTCTime(time.Time{}), nil

	case "EntryTime":
		return emptyBinaryTime(), nil

	case "Octet6":
		return parseInitialOctetString(val, 6)

	case "Octet16":
		return parseInitialOctetString(val, 16)

	case "Octet64":
		return parseInitialOctetString(val, 64)

	default:
		return nil, fmt.Errorf("unsupported BType %q for initial value", btype)
	}
}

// tcmdLiterals maps the Tcmd literals to their two-bit encoding, first
// bit in the high bit of the octet.
var tcmdLiterals = map[string]byte{
	"stop":     0x00,
	"lower":    0x40,
	"higher":   0x80,
	"reserved": 0xc0,
}

// dbposLiterals maps the Dbpos literals to their two-bit encoding,
// first bit in the high bit of the octet.
var dbposLiterals = map[string]byte{
	"intermediate-state": 0x00,
	"off":                0x40,
	"on":                 0x80,
	"bad-state":          0xc0,
}

// parseInitialBitString parses an integer string into a bit string of
// the specified bit length. The integer value populates the bits in
// MSB-first order.
func parseInitialBitString(val string, bits int) (*mms.Value, error) {
	u, err := strconv.ParseUint(val, 0, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid bit string value %q: %w", val, err)
	}
	byteLen := (bits + 7) / 8
	data := make([]byte, byteLen)
	for i := 0; i < bits && i < 64; i++ {
		if u&(1<<uint(i)) != 0 {
			byteIdx := i / 8
			bitInByte := uint(7 - (i % 8))
			data[byteIdx] |= 1 << bitInByte
		}
	}
	return mms.NewBitStringWithLength(data, bits), nil
}

// parseInitialOctetString parses a hex string (or decimal integer) into
// an octet string of the specified max size. For simplicity, a decimal
// integer is zero-padded into the first bytes; a hex string (0x prefix)
// is decoded directly.
func parseInitialOctetString(val string, maxSize int) (*mms.Value, error) {
	if strings.HasPrefix(val, "0x") || strings.HasPrefix(val, "0X") {
		data, err := hexDecode(val[2:])
		if err != nil {
			return nil, fmt.Errorf("invalid octet string hex %q: %w", val, err)
		}
		if len(data) > maxSize {
			return nil, fmt.Errorf("octet string %q exceeds max size %d", val, maxSize)
		}
		return mms.NewOctetString(data), nil
	}
	// Treat as a decimal integer and encode as big-endian bytes.
	u, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid octet string value %q: %w", val, err)
	}
	data := make([]byte, maxSize)
	for i := maxSize - 1; i >= 0 && u > 0; i-- {
		data[i] = byte(u & 0xFF)
		u >>= 8
	}
	return mms.NewOctetString(data), nil
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		s = "0" + s
	}
	data := make([]byte, len(s)/2)
	for i := range data {
		hi, err := hexNibble(s[i*2])
		if err != nil {
			return nil, err
		}
		lo, err := hexNibble(s[i*2+1])
		if err != nil {
			return nil, err
		}
		data[i] = hi<<4 | lo
	}
	return data, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex character %q", c)
	}
}

// encodeOptFieldsDef converts an [OptFieldsDef] to an MMS BitString value
// matching the IEC 61850 OptFlds encoding.
//
// IEC 61850 OptFlds BIT STRING (10 bits): bit 0 is reserved; bits 1-9 carry
// the actual flags in this order: seqNum, timeStamp, reasonCode, dataSet,
// dataRef, bufOvfl, entryID, confRev, segmentation.
func encodeOptFieldsDef(o OptFieldsDef) *mms.Value {
	data := make([]byte, 2)
	setOptBit := func(bit int, v bool) {
		if v {
			byteIdx := bit / 8
			bitInByte := uint(7 - (bit % 8))
			data[byteIdx] |= 1 << bitInByte
		}
	}
	setOptBit(1, o.SeqNum)
	setOptBit(2, o.TimeStamp)
	setOptBit(3, o.ReasonCode)
	setOptBit(4, o.DataSet)
	setOptBit(5, o.DataRef)
	setOptBit(6, o.BufOvfl)
	setOptBit(7, o.EntryID)
	setOptBit(8, o.ConfigRef)
	return mms.NewBitStringWithLength(data, 10)
}

// encodeTrgOpsDef converts a [TrgOpsDef] to an MMS BitString value
// matching the IEC 61850 TrgOps encoding (bit 0 reserved, logical
// flags start at bit 1).
func encodeTrgOpsDef(t TrgOpsDef) *mms.Value {
	var raw uint8
	if t.Dchg {
		raw |= 1 << 1
	}
	if t.Qchg {
		raw |= 1 << 2
	}
	if t.Dupd {
		raw |= 1 << 3
	}
	if t.Period {
		raw |= 1 << 4
	}
	if t.GI {
		raw |= 1 << 5
	}
	data := []byte{0}
	for bit := range 6 {
		if raw&(1<<uint(bit)) != 0 {
			bitInByte := uint(7 - (bit % 8))
			data[0] |= 1 << bitInByte
		}
	}
	return mms.NewBitStringWithLength(data, 6)
}
