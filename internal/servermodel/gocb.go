// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// GoCBDef defines a GOOSE control block (SCL GSEControl plus the GSE
// address of its ConnectedAP).
type GoCBDef struct {
	// Name is the control block name (e.g. "gcb01").
	Name string

	// GoID is the GOOSE identifier (SCL GSEControl appID). Empty means
	// the GoCB reference is used.
	GoID string

	// DatSet is the dataset name (local to the LN).
	DatSet string

	// ConfRev is the configuration revision.
	ConfRev uint32

	// FixedOffs mirrors the SCL fixedOffs attribute.
	FixedOffs bool

	// HasAddress is true when a valid GSE address was found.
	HasAddress bool

	// HasVLAN is true when the address specifies VLAN-ID or
	// VLAN-PRIORITY (frames are then 802.1Q tagged).
	HasVLAN bool

	// DstAddr, AppID, VLANID and VLANPriority are the GSE address.
	DstAddr      [6]byte
	AppID        uint16
	VLANID       uint16
	VLANPriority uint8

	// MinTime and MaxTime are the retransmission parameters in ms
	// (0 when not configured).
	MinTime, MaxTime uint32
}

// GSEAddress is a parsed SCL GSE (or SMV) Address element.
type GSEAddress struct {
	Dst          [6]byte
	AppID        uint16
	VLANID       uint16
	VLANPriority uint8
	HasVLAN      bool
}

// ParseGSEAddress parses the P elements of an SCL GSE Address:
// MAC-Address (required, "01-0C-CD-01-00-01"), APPID (required, 4 hex
// digits), VLAN-ID (3 hex digits) and VLAN-PRIORITY (0-7).
func ParseGSEAddress(ps []scl.P) (GSEAddress, error) {
	var a GSEAddress
	var haveMAC, haveAppID bool
	for _, p := range ps {
		v := strings.TrimSpace(p.Value)
		switch p.Type {
		case "MAC-Address":
			parts := strings.FieldsFunc(v, func(r rune) bool { return r == '-' || r == ':' })
			if len(parts) != 6 {
				return a, fmt.Errorf("MAC-Address %q: want 6 octets", v)
			}
			for i, s := range parts {
				b, err := strconv.ParseUint(s, 16, 8)
				if err != nil {
					return a, fmt.Errorf("MAC-Address %q: %w", v, err)
				}
				a.Dst[i] = byte(b)
			}
			haveMAC = true
		case "APPID":
			x, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(v), "0x"), 16, 16)
			if err != nil {
				return a, fmt.Errorf("APPID %q: %w", v, err)
			}
			a.AppID = uint16(x)
			haveAppID = true
		case "VLAN-ID":
			x, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(v), "0x"), 16, 16)
			if err != nil || x > 0xfff {
				return a, fmt.Errorf("VLAN-ID %q: invalid", v)
			}
			a.VLANID = uint16(x)
			a.HasVLAN = true
		case "VLAN-PRIORITY":
			x, err := strconv.ParseUint(v, 10, 8)
			if err != nil || x > 7 {
				return a, fmt.Errorf("VLAN-PRIORITY %q: invalid", v)
			}
			a.VLANPriority = uint8(x)
			a.HasVLAN = true
		}
	}
	if !haveMAC {
		return a, fmt.Errorf("missing MAC-Address")
	}
	if !haveAppID {
		return a, fmt.Errorf("missing APPID")
	}
	return a, nil
}

// parseMillis parses an SCL MinTime/MaxTime value (milliseconds,
// possibly with a fraction).
func parseMillis(s string) (uint32, error) {
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 || f > math.MaxUint32 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return uint32(math.Round(f)), nil
}

// convertGoCB builds a GoCBDef from an SCL GSEControl and its GSE
// address (nil when the Communication section has none). Address
// problems are reported as warnings and leave HasAddress false.
func convertGoCB(gc *scl.GSEControl, gse *scl.GSEAddress, where string, warnings *[]string) GoCBDef {
	def := GoCBDef{Name: gc.Name, GoID: gc.AppID, DatSet: gc.DatSet, ConfRev: gc.ConfRev, FixedOffs: gc.FixedOffs}
	warn := func(format string, args ...any) {
		if warnings != nil {
			*warnings = append(*warnings, where+": "+fmt.Sprintf(format, args...))
		}
	}
	if gse == nil {
		warn("GSEControl %q has no GSE address in the Communication section", gc.Name)
		return def
	}
	addr, err := ParseGSEAddress(gse.Address)
	if err != nil {
		warn("GSE %q: %v", gc.Name, err)
		return def
	}
	def.HasAddress = true
	def.DstAddr, def.AppID, def.VLANID, def.VLANPriority, def.HasVLAN = addr.Dst, addr.AppID, addr.VLANID, addr.VLANPriority, addr.HasVLAN
	if def.MinTime, err = parseMillis(gse.MinTime); err != nil {
		warn("GSE %q MinTime: %v", gc.Name, err)
	}
	if def.MaxTime, err = parseMillis(gse.MaxTime); err != nil {
		warn("GSE %q MaxTime: %v", gc.Name, err)
	}
	return def
}

// GoCB MMS field layout (IEC 61850-8-1 GoCB): GoEna, GoID, DatSet,
// ConfRev, NdsCom, DstAddress{Addr, PRIORITY, VID, APPID}, MinTime,
// MaxTime, FixedOffs.
var dstAddressTS = mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: []mms.TypeSpecElement{
	{Name: "Addr", Type: mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 6}},
	{Name: "PRIORITY", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}},
	{Name: "VID", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
	{Name: "APPID", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
}}

// GoCBWritableFields lists the GoCB attributes a client may write. All
// other attributes are read-only (object-access-denied).
var GoCBWritableFields = map[string]bool{"GoEna": true, "GoID": true, "DatSet": true}

// GoCBItemID returns the MMS item ID of a GoCB ("LLN0$GO$gcb01").
func GoCBItemID(lnName, cbName string) string { return lnName + "$GO$" + cbName }

// QualifiedDatSet returns the domain-qualified dataset reference used in
// control blocks ("LD/LLN0$ds1").
func QualifiedDatSet(ldName, lnName, ds string) string {
	if ds == "" || strings.Contains(ds, "/") {
		return ds
	}
	return ldName + "/" + lnName + "$" + ds
}

func buildGoCBFields(g *GoCBDef, ldName, lnName string) []rcbField {
	goID := g.GoID
	if goID == "" {
		goID = ldName + "/" + GoCBItemID(lnName, g.Name)
	}
	dst := mms.NewStructure([]*mms.Value{
		mms.NewOctetString(g.DstAddr[:]),
		mms.NewUnsigned(uint64(g.VLANPriority)),
		mms.NewUnsigned(uint64(g.VLANID)),
		mms.NewUnsigned(uint64(g.AppID)),
	})
	return []rcbField{
		{"GoEna", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
		{"GoID", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(goID)},
		{"DatSet", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(QualifiedDatSet(ldName, lnName, g.DatSet))},
		{"ConfRev", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(g.ConfRev))},
		{"NdsCom", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(!g.HasAddress)},
		{"DstAddress", dstAddressTS, dst},
		{"MinTime", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(g.MinTime))},
		{"MaxTime", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(g.MaxTime))},
		{"FixedOffs", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(g.FixedOffs)},
	}
}

// goCBTypeSpec returns the GoCB structure TypeSpec and a read function
// over the store.
func goCBStruct(vs *ValueStore, g *GoCBDef, ldName, lnName string) (mms.TypeSpec, func(context.Context) (*mms.Value, error)) {
	fields := buildGoCBFields(g, ldName, lnName)
	elems := make([]mms.TypeSpecElement, len(fields))
	keys := make([]string, len(fields))
	for i, f := range fields {
		elems[i] = mms.TypeSpecElement{Name: f.suffix, Type: f.typeSpec}
		keys[i] = StoreKey(ldName, GoCBItemID(lnName, g.Name)+"$"+f.suffix)
	}
	read := func(context.Context) (*mms.Value, error) {
		vals := make([]*mms.Value, len(fields))
		for i, f := range fields {
			if v := vs.Get(keys[i]); v != nil {
				vals[i] = v
			} else {
				vals[i] = defaultMMSValue(f.typeSpec)
			}
		}
		return mms.NewStructure(vals), nil
	}
	return mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: elems}, read
}

// registerGoCB registers the GoCB structure and its attributes. Writes
// to GoEna, GoID and DatSet go through the write interceptor (which may
// reject them); the value is stored only when the interceptor accepts
// or is absent. Other attributes and whole-structure writes are
// rejected with object-access-denied.
func registerGoCB(srv *mms.Server, vs *ValueStore, ldName, lnName string, g *GoCBDef) error {
	cbItem := GoCBItemID(lnName, g.Name)
	denied := &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
	for _, f := range buildGoCBFields(g, ldName, lnName) {
		item := cbItem + "$" + f.suffix
		sk := StoreKey(ldName, item)
		vs.Set(sk, f.initial)
		ts := f.typeSpec
		v := mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(ldName), ItemID: mms.ItemID(item)},
			TypeSpec: ts,
			Read: func(context.Context) (*mms.Value, error) {
				if val := vs.Get(sk); val != nil {
					return val, nil
				}
				return defaultMMSValue(ts), nil
			},
		}
		if GoCBWritableFields[f.suffix] {
			v.Write = func(ctx context.Context, val *mms.Value) error {
				if val == nil || val.Type() != ts.Type {
					return &mms.DataAccessError{Code: mms.DataAccessErrorTypeInconsistent}
				}
				if err := validateWriteSize(val, ts); err != nil {
					return &mms.DataAccessError{Code: mms.DataAccessErrorObjectValueInvalid}
				}
				if handled, err := vs.callInterceptor(ctx, sk, val); handled || err != nil {
					return err
				}
				vs.Set(sk, val)
				return nil
			}
		} else {
			v.Write = func(context.Context, *mms.Value) error { return denied }
		}
		if err := srv.RegisterVariable(v); err != nil {
			return fmt.Errorf("register GoCB attribute %s: %w", item, err)
		}
		if f.suffix == "DstAddress" {
			for i, e := range dstAddressTS.Elements {
				sub := item + "$" + e.Name
				idx := i
				if err := srv.RegisterVariable(mms.Variable{
					Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(ldName), ItemID: mms.ItemID(sub)},
					TypeSpec: e.Type,
					Read: func(context.Context) (*mms.Value, error) {
						if v := vs.Get(sk); v != nil {
							if elems, ok := v.Structure(); ok && idx < len(elems) {
								return elems[idx], nil
							}
						}
						return defaultMMSValue(dstAddressTS.Elements[idx].Type), nil
					},
					Write: func(context.Context, *mms.Value) error { return denied },
				}); err != nil {
					return fmt.Errorf("register GoCB attribute %s: %w", sub, err)
				}
			}
		}
	}
	ts, read := goCBStruct(vs, g, ldName, lnName)
	return srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(ldName), ItemID: mms.ItemID(cbItem)},
		TypeSpec: ts,
		Read:     read,
		Write:    func(context.Context, *mms.Value) error { return denied },
	})
}

// registerGOGroup registers the "LN$GO" FC-group variable and returns
// its TypeSpec element and read function for the LN structure. ok is
// false when the LN has no GoCBs.
func registerGOGroup(srv *mms.Server, vs *ValueStore, ldName string, ln *LogicalNode) (mms.TypeSpecElement, func(context.Context) (*mms.Value, error), bool, error) {
	if len(ln.GoCBs) == 0 {
		return mms.TypeSpecElement{}, nil, false, nil
	}
	elems := make([]mms.TypeSpecElement, len(ln.GoCBs))
	reads := make([]func(context.Context) (*mms.Value, error), len(ln.GoCBs))
	for i := range ln.GoCBs {
		ts, read := goCBStruct(vs, &ln.GoCBs[i], ldName, ln.Name)
		elems[i] = mms.TypeSpecElement{Name: ln.GoCBs[i].Name, Type: ts}
		reads[i] = read
	}
	ts := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: elems}
	read := makeStructRead(reads)
	item := ln.Name + "$GO"
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(ldName), ItemID: mms.ItemID(item)},
		TypeSpec: ts,
		Read:     read,
	}); err != nil {
		return mms.TypeSpecElement{}, nil, false, fmt.Errorf("register GO FC group %s: %w", item, err)
	}
	return mms.TypeSpecElement{Name: "GO", Type: ts}, read, true, nil
}

// MemberNode is the resolved value layout of a dataset member: a leaf
// (Key set) or a structure of children.
type MemberNode struct {
	Name     string
	Key      string       // ValueStore key, leaves only
	Type     mms.TypeSpec // full type of this node
	Children []*MemberNode
}

// Keys returns the store keys of all leaves under n, in order.
func (n *MemberNode) Keys() []string {
	if n.Key != "" {
		return []string{n.Key}
	}
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Keys()...)
	}
	return out
}

// ReadMember assembles the current value of a dataset member from the
// store, substituting type defaults for missing leaves.
func (vs *ValueStore) ReadMember(n *MemberNode) *mms.Value {
	if n.Key != "" {
		if v := vs.Get(n.Key); v != nil {
			return v
		}
		return defaultMMSValue(n.Type)
	}
	vals := make([]*mms.Value, len(n.Children))
	for i, c := range n.Children {
		vals[i] = vs.ReadMember(c)
	}
	return mms.NewStructure(vals)
}

// FindDataSet returns the dataset with the given name (local, or
// "LD/LN$name" qualified) in LD ldName, and the LN holding it.
func (m *Model) FindDataSet(ldName, ref string) (*DataSetDef, *LogicalNode) {
	for i := range m.LogicalDevices {
		ld := &m.LogicalDevices[i]
		if ld.Name != ldName {
			continue
		}
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.DataSets {
				ds := &ln.DataSets[k]
				if ds.Name == ref || QualifiedDatSet(ldName, ln.Name, ds.Name) == ref {
					return ds, ln
				}
			}
		}
	}
	return nil, nil
}

// ResolveMember resolves a dataset member (in LD ldName) to its value
// layout. The member may reference a data object (all attributes of its
// FC, recursively) or a data attribute (leaf or constructed).
func (m *Model) ResolveMember(ldName string, mem DataSetMemberDef) (*MemberNode, error) {
	ldInst := mem.LDInst
	if ldInst == "" {
		ldInst = ldName
	}
	var ln *LogicalNode
	for i := range m.LogicalDevices {
		if m.LogicalDevices[i].Name != ldInst {
			continue
		}
		for j := range m.LogicalDevices[i].LogicalNodes {
			if m.LogicalDevices[i].LogicalNodes[j].Name == mem.LNName {
				ln = &m.LogicalDevices[i].LogicalNodes[j]
			}
		}
	}
	ref := ldInst + "/" + mem.LNName + "." + mem.DOPath + " [" + mem.FC + "]"
	if ln == nil {
		return nil, fmt.Errorf("member %s: logical node not found", ref)
	}
	segs := strings.Split(mem.DOPath, ".")
	base := ldInst + "/" + mem.LNName + "$" + mem.FC
	var obj *DataObject
	for i := range ln.DataObjects {
		if ln.DataObjects[i].Name == segs[0] {
			obj = &ln.DataObjects[i]
		}
	}
	if obj == nil {
		return nil, fmt.Errorf("member %s: data object %q not found", ref, segs[0])
	}
	path := []string{segs[0]}
	rest := segs[1:]
	// Descend through sub-data objects.
	for len(rest) > 0 {
		var child *DataObject
		for i := range obj.Children {
			if obj.Children[i].Name == rest[0] {
				child = &obj.Children[i]
			}
		}
		if child == nil {
			break
		}
		obj = child
		path = append(path, rest[0])
		rest = rest[1:]
	}
	if len(rest) == 0 {
		n := doNode(obj, base, path, mem.FC)
		if n == nil {
			return nil, fmt.Errorf("member %s: no attributes with FC %s", ref, mem.FC)
		}
		return n, nil
	}
	// Descend into attributes.
	var attr *DataAttribute
	for i := range obj.Attributes {
		a := &obj.Attributes[i]
		if a.Name == rest[0] && (a.FC == mem.FC || a.FC == "") {
			attr = a
		}
	}
	for _, s := range rest[1:] {
		if attr == nil {
			break
		}
		path = append(path, attr.Name)
		var next *DataAttribute
		for i := range attr.Children {
			if attr.Children[i].Name == s {
				next = &attr.Children[i]
			}
		}
		attr = next
	}
	if attr == nil {
		return nil, fmt.Errorf("member %s: attribute not found", ref)
	}
	return attrNode(attr, base, path)
}

func doNode(obj *DataObject, base string, path []string, fc string) *MemberNode {
	n := &MemberNode{Name: obj.Name}
	var elems []mms.TypeSpecElement
	for i := range obj.Attributes {
		a := &obj.Attributes[i]
		if a.FC != fc && a.FC != "" {
			continue
		}
		c, err := attrNode(a, base, path)
		if err != nil {
			continue
		}
		n.Children = append(n.Children, c)
		elems = append(elems, mms.TypeSpecElement{Name: c.Name, Type: c.Type})
	}
	for i := range obj.Children {
		sub := &obj.Children[i]
		c := doNode(sub, base, append(append([]string(nil), path...), sub.Name), fc)
		if c == nil {
			continue
		}
		n.Children = append(n.Children, c)
		elems = append(elems, mms.TypeSpecElement{Name: c.Name, Type: c.Type})
	}
	if len(n.Children) == 0 {
		return nil
	}
	n.Type = mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: elems}
	return n
}

func attrNode(a *DataAttribute, base string, parent []string) (*MemberNode, error) {
	path := append(append([]string(nil), parent...), a.Name)
	if len(a.Children) == 0 {
		ts, ok := btypeToMMS[a.BType]
		if !ok {
			return nil, fmt.Errorf("unsupported BType %q", a.BType)
		}
		return &MemberNode{Name: a.Name, Key: base + "$" + strings.Join(path, "$"), Type: ts}, nil
	}
	n := &MemberNode{Name: a.Name}
	var elems []mms.TypeSpecElement
	for i := range a.Children {
		c, err := attrNode(&a.Children[i], base, path)
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, c)
		elems = append(elems, mms.TypeSpecElement{Name: c.Name, Type: c.Type})
	}
	n.Type = mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: elems}
	return n, nil
}
