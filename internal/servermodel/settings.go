// SPDX-License-Identifier: MIT

package servermodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/otfabric/go-mms"
)

// Setting is one leaf data attribute of a setting group (IEC 61850-7-2
// §16.1): the value of the active group is under FC=SG, the edit buffer
// under FC=SE. The SCL can declare either or both (IEC 61850-6 Table 47).
type Setting struct {
	// SGKey and SEKey are the store keys (see [StoreKey]) of the leaf
	// under FC=SG and FC=SE; "" when the model has no such attribute.
	SGKey, SEKey string

	// Type is the MMS type of the leaf.
	Type mms.TypeSpec

	// Groups are the values per setting group from the model
	// ([DataAttribute.GroupValues]); key 0 is the value of groups not
	// listed. Nil when the model gives none.
	Groups map[uint8]*mms.Value
}

// Settings returns the setting leaves of ld, ordered by store key. A
// setting group spans the logical device (IEC 61850-7-2 §16.1), so every
// FC=SG and FC=SE leaf of every LN is one. A per-group value that does
// not parse is an error.
func Settings(ld *LogicalDevice) ([]Setting, error) {
	byPath := map[string]*Setting{}
	var walkErr error

	var leaf func(lnName string, path []string, a *DataAttribute, fc string)
	leaf = func(lnName string, path []string, a *DataAttribute, fc string) {
		if a.FC != "" {
			fc = a.FC
		}
		if fc != "SG" && fc != "SE" {
			return
		}
		p := append(append([]string(nil), path...), a.Name)
		if len(a.Children) > 0 {
			for i := range a.Children {
				leaf(lnName, p, &a.Children[i], fc)
			}
			return
		}
		ts, ok := btypeToMMS[a.BType]
		if !ok {
			return
		}
		rest := strings.Join(p, "$")
		id := lnName + "$" + rest
		s := byPath[id]
		if s == nil {
			s = &Setting{Type: ts}
			byPath[id] = s
		}
		key := StoreKey(ld.Name, lnName+"$"+fc+"$"+rest)
		if fc == "SG" {
			s.SGKey = key
		} else {
			s.SEKey = key
		}
		// The SG attribute's values win over the SE twin's.
		if a.GroupValues == nil || (s.Groups != nil && fc == "SE") {
			return
		}
		groups := make(map[uint8]*mms.Value, len(a.GroupValues))
		for g, text := range a.GroupValues {
			v := defaultMMSValue(ts)
			if text != "" {
				var err error
				if v, err = parseInitialValue(a.BType, text, a.EnumNames); err != nil {
					if walkErr == nil {
						walkErr = fmt.Errorf("setting %s group %d: %w", key, g, err)
					}
					return
				}
			}
			groups[g] = v
		}
		s.Groups = groups
	}
	var walkDO func(lnName string, path []string, o *DataObject)
	walkDO = func(lnName string, path []string, o *DataObject) {
		p := append(append([]string(nil), path...), o.Name)
		for i := range o.Children {
			walkDO(lnName, p, &o.Children[i])
		}
		for i := range o.Attributes {
			leaf(lnName, p, &o.Attributes[i], "")
		}
	}
	for i := range ld.LogicalNodes {
		ln := &ld.LogicalNodes[i]
		for j := range ln.DataObjects {
			walkDO(ln.Name, nil, &ln.DataObjects[j])
		}
	}
	if walkErr != nil {
		return nil, walkErr
	}

	ids := make([]string, 0, len(byPath))
	for id := range byPath {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Setting, len(ids))
	for i, id := range ids {
		out[i] = *byPath[id]
	}
	return out, nil
}

// DefaultValue returns the zero value of the MMS type ts, as served for
// an attribute without a value.
func DefaultValue(ts mms.TypeSpec) *mms.Value { return defaultMMSValue(ts) }

// setLeaves stores the leaves of a constructed value val of type ts
// under key (key$member...). val has been checked against ts.
func setLeaves(vs *ValueStore, key string, ts mms.TypeSpec, val *mms.Value) {
	if ts.Type != mms.ValueTypeStructure {
		vs.Set(key, val)
		return
	}
	members, _ := val.Structure()
	for i, e := range ts.Elements {
		if i < len(members) {
			setLeaves(vs, key+"$"+e.Name, e.Type, members[i])
		}
	}
}
