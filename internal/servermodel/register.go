package servermodel

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
)

// btypeToMMS maps IEC 61850 basic types to MMS TypeSpec values.
var btypeToMMS = map[string]mms.TypeSpec{
	"BOOLEAN":      {Type: mms.ValueTypeBoolean},
	"INT8":         {Type: mms.ValueTypeInteger, Size: 8},
	"INT16":        {Type: mms.ValueTypeInteger, Size: 16},
	"INT32":        {Type: mms.ValueTypeInteger, Size: 32},
	"INT64":        {Type: mms.ValueTypeInteger, Size: 64},
	"INT128":       {Type: mms.ValueTypeInteger, Size: 128},
	"INT8U":        {Type: mms.ValueTypeUnsigned, Size: 8},
	"INT16U":       {Type: mms.ValueTypeUnsigned, Size: 16},
	"INT32U":       {Type: mms.ValueTypeUnsigned, Size: 32},
	"FLOAT32":      {Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
	"FLOAT64":      {Type: mms.ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11},
	"Enum":         {Type: mms.ValueTypeInteger, Size: 8},
	"Dbpos":        {Type: mms.ValueTypeBitString, Size: 2},
	"Tcmd":         {Type: mms.ValueTypeInteger, Size: 8},
	"Quality":      {Type: mms.ValueTypeBitString, Size: 13},
	"Timestamp":    {Type: mms.ValueTypeUTCTime},
	"VisString32":  {Type: mms.ValueTypeVisibleString, Size: 32},
	"VisString64":  {Type: mms.ValueTypeVisibleString, Size: 64},
	"VisString65":  {Type: mms.ValueTypeVisibleString, Size: 65},
	"VisString129": {Type: mms.ValueTypeVisibleString, Size: 129},
	"VisString255": {Type: mms.ValueTypeVisibleString, Size: 255},
	"Unicode255":   {Type: mms.ValueTypeMmsString, Size: 255},
	"Octet6":       {Type: mms.ValueTypeOctetString, Size: 6},
	"Octet16":      {Type: mms.ValueTypeOctetString, Size: 16},
	"Octet64":      {Type: mms.ValueTypeOctetString, Size: 64},
	"Check":        {Type: mms.ValueTypeBitString, Size: 2},
	"OptFlds":      {Type: mms.ValueTypeBitString, Size: 10},
	"TrgOps":       {Type: mms.ValueTypeBitString, Size: 6},
	"EntryTime":    {Type: mms.ValueTypeBinaryTime},
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

	interceptorMu sync.RWMutex
	interceptor   WriteInterceptor
}

// ValueStoreOptions configures [ValueStore] behavior.
type ValueStoreOptions struct {
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
		values:        make(map[string]*mms.Value),
		defensiveCopy: opts.DefensiveCopy,
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

	if ln.SettingGroup != nil {
		if err := registerSGCB(srv, vs, ldName, ln.Name, ln.SettingGroup); err != nil {
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
				return subTS.DefaultValue(), nil
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
			return ts0.DefaultValue(), nil
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
	if err := srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(ldName),
			ItemID: mms.ItemID(itemID),
		},
		TypeSpec: elem.Type,
		Read:     read,
		Write: func(ctx context.Context, val *mms.Value) error {
			// Store the compound value so the interceptor can read it if needed.
			vs.Set(storeKey, val)
			if handled, err := vs.callInterceptor(ctx, storeKey, val); handled || err != nil {
				return err
			}
			return nil
		},
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
				valid := false
				for _, ord := range attr.EnumValues {
					if int(i) == ord {
						valid = true
						break
					}
				}
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
		Read: func(_ context.Context) (*mms.Value, error) {
			val := vs.Get(storeKey)
			if val != nil {
				return val, nil
			}
			return ts.DefaultValue(), nil
		},
		Write: func(ctx context.Context, val *mms.Value) error {
			if val == nil {
				return fmt.Errorf("nil value for %s", storeKey)
			}
			if val.Type() != ts.Type {
				return fmt.Errorf("type mismatch for %s: got %v, want %v",
					storeKey, val.Type(), ts.Type)
			}
			if err := validateWriteSize(val, ts); err != nil {
				return fmt.Errorf("write validation for %s: %w", storeKey, err)
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

	return srv.RegisterVariable(v)
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
		rcbField{"OptFlds", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 10}, encodeOptFieldsDef(rpt.OptFlds)},
		rcbField{"BufTm", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.BufTime))},
		rcbField{"SqNum", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(0)},
		rcbField{"TrgOps", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 6}, encodeTrgOpsDef(rpt.TrgOps)},
		rcbField{"IntgPd", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.IntgPd))},
		rcbField{"GI", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
	)
	if rpt.Buffered {
		fields = append(fields,
			rcbField{"PurgeBuf", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
			rcbField{"EntryID", mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 8}, mms.NewOctetString(make([]byte, 8))},
			rcbField{"TimeOfEntry", mms.TypeSpec{Type: mms.ValueTypeBinaryTime}, mms.NewBinaryTime(0)},
		)
	}
	return fields
}

// registerRCB registers an RCB structure variable and its individual
// subfield variables with the MMS server.
//
// Supported subfields: RptID, RptEna, DatSet, ConfRev, OptFlds,
// BufTm, SqNum, TrgOps, IntgPd, GI.
// URCB-specific: Resv.
// BRCB-specific: PurgeBuf, EntryID, TimeOfEntry.
//
// Not currently implemented: Owner, ResvTms decode/use, runtime
// RptEna sequencing validation, URCB/BRCB field-type restrictions.
// The write path accepts any structurally matching value; semantic
// checks (e.g., "RptEna must be disabled before changing DatSet")
// are not enforced.
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
				return subTS.DefaultValue(), nil
			},
			Write: func(ctx context.Context, val *mms.Value) error {
				vs.Set(subSK, val)
				if handled, err := vs.callInterceptor(ctx, subSK, val); handled || err != nil {
					return err
				}
				return nil
			},
		}); err != nil {
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
					v = f.typeSpec.DefaultValue()
				}
				elems[i] = v
			}
			return mms.NewStructure(elems), nil
		},
		Write: func(_ context.Context, val *mms.Value) error {
			elems, ok := val.Structure()
			if !ok || len(elems) != len(fields) {
				return fmt.Errorf("RCB write: expected structure with %d elements", len(fields))
			}
			for i, f := range fields {
				if elems[i] == nil {
					return fmt.Errorf("RCB write: nil element at position %d (%s)", i, f.suffix)
				}
				if elems[i].Type() != f.typeSpec.Type {
					return fmt.Errorf("RCB write: type mismatch for %s: got %v, want %v",
						f.suffix, elems[i].Type(), f.typeSpec.Type)
				}
				if err := validateWriteSize(elems[i], f.typeSpec); err != nil {
					return fmt.Errorf("RCB write %s: %w", f.suffix, err)
				}
			}
			for i, f := range fields {
				subKey := StoreKey(ldName, rcbItemID+"$"+f.suffix)
				vs.Set(subKey, elems[i])
			}
			return nil
		},
	})
}

func registerSGCB(srv *mms.Server, vs *ValueStore, ldName, lnName string, sg *SettingGroupDef) error {
	sgcbItemID := lnName + "$SP$SGCB"

	fields := []rcbField{
		{"NumOfSGs", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}, mms.NewUnsigned(uint64(sg.NumOfSGs))},
		{"ActSG", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}, mms.NewUnsigned(uint64(sg.ActSG))},
		{"EditSG", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}, mms.NewUnsigned(0)},
		{"CnfEdit", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
		{"LActTm", mms.TypeSpec{Type: mms.ValueTypeUTCTime}, mms.NewUTCTime(time.Time{})},
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
				return subTS.DefaultValue(), nil
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
					v = f.typeSpec.DefaultValue()
				}
				elems[i] = v
			}
			return mms.NewStructure(elems), nil
		},
		Write: func(_ context.Context, val *mms.Value) error {
			elems, ok := val.Structure()
			if !ok || len(elems) != len(fields) {
				return fmt.Errorf("SGCB write: expected structure with %d elements", len(fields))
			}
			for i, f := range fields {
				subKey := StoreKey(ldName, sgcbItemID+"$"+f.suffix)
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

	case "INT8", "INT16", "INT32", "INT64", "INT128", "Tcmd":
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer value %q for %s: %w", val, btype, err)
		}
		return mms.NewInteger(i), nil

	case "Enum":
		// Accept numeric ordinal strings ("1") or SCL enum value names
		// ("direct-with-normal-security"). Name lookup requires enumNames.
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return mms.NewInteger(i), nil
		}
		if enumNames != nil {
			if ord, ok := enumNames[val]; ok {
				return mms.NewInteger(int64(ord)), nil
			}
		}
		return nil, fmt.Errorf("invalid integer value %q for %s: not a valid ordinal or enum name", val, btype)

	case "INT8U", "INT16U", "INT32U":
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

	case "VisString32", "VisString64", "VisString65", "VisString129", "VisString255":
		return mms.NewVisibleString(val), nil

	case "Unicode255":
		return mms.NewMmsString(val), nil

	case "Quality":
		return parseInitialBitString(val, 13)

	case "Dbpos", "Check":
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
		return mms.NewBinaryTime(0), nil

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
	for i := 0; i < len(data); i++ {
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
	for bit := 0; bit < 6; bit++ {
		if raw&(1<<uint(bit)) != 0 {
			bitInByte := uint(7 - (bit % 8))
			data[0] |= 1 << bitInByte
		}
	}
	return mms.NewBitStringWithLength(data, 6)
}
