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

// SetWriteInterceptor installs a callback that is invoked by RCB
// and SGCB write handlers before committing to the store. This
// allows higher layers (e.g. [ReportEngine], [SettingGroupEngine])
// to intercept and validate writes from MMS clients.
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
	for _, obj := range ln.DataObjects {
		if err := registerDO(srv, vs, ldName, ln.Name, nil, &obj); err != nil {
			return err
		}
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

func registerDO(srv *mms.Server, vs *ValueStore, ldName, lnName string, parentPath []string, obj *DataObject) error {
	for _, child := range obj.Children {
		childPath := append(append([]string(nil), parentPath...), obj.Name)
		if err := registerDO(srv, vs, ldName, lnName, childPath, &child); err != nil {
			return err
		}
	}

	for _, attr := range obj.Attributes {
		attrPath := append(append([]string(nil), parentPath...), obj.Name)
		if err := registerDA(srv, vs, ldName, lnName, attrPath, &attr); err != nil {
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
		iv, err := parseInitialValue(attr.BType, attr.InitialValue)
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
		Write: func(_ context.Context, val *mms.Value) error {
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

	fields := []rcbField{
		{"RptID", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(rptID)},
		{"RptEna", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
		{"DatSet", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(rpt.DatSet)},
		{"ConfRev", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.ConfRev))},
		{"OptFlds", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 10}, encodeOptFieldsDef(rpt.OptFlds)},
		{"BufTm", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.BufTime))},
		{"SqNum", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(0)},
		{"TrgOps", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 6}, encodeTrgOpsDef(rpt.TrgOps)},
		{"IntgPd", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(rpt.IntgPd))},
		{"GI", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
	}

	if !rpt.Buffered {
		fields = append(fields, rcbField{
			"Resv", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false),
		})
	} else {
		fields = append(fields,
			rcbField{"PurgeBuf", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
			rcbField{"EntryID", mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 8}, mms.NewOctetString(make([]byte, 8))},
			rcbField{"TimeOfEntry", mms.TypeSpec{Type: mms.ValueTypeBinaryTime}, mms.NewBinaryTime(0)},
		)
	}

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
func parseInitialValue(btype, val string) (*mms.Value, error) {
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
		// Enum values are stored as plain integers without validation
		// against the referenced EnumType. A full validation pass in
		// FromSCL would require threading the EnumType index here.
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer value %q for %s: %w", val, btype, err)
		}
		return mms.NewInteger(i), nil

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
func encodeOptFieldsDef(o OptFieldsDef) *mms.Value {
	data := make([]byte, 2)
	setOptBit := func(bit int, v bool) {
		if v {
			byteIdx := bit / 8
			bitInByte := uint(7 - (bit % 8))
			data[byteIdx] |= 1 << bitInByte
		}
	}
	setOptBit(0, o.SeqNum)
	setOptBit(1, o.TimeStamp)
	setOptBit(2, o.ReasonCode)
	setOptBit(3, o.DataSet)
	setOptBit(4, o.DataRef)
	setOptBit(5, o.BufOvfl)
	setOptBit(6, o.EntryID)
	setOptBit(7, o.ConfigRef)
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
