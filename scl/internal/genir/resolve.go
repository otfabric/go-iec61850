// SPDX-License-Identifier: MIT

package genir

import (
	"fmt"
	"sort"
	"strings"
)

// Resolved is a Schema with all type references resolved and
// attribute groups expanded. It provides lookup maps for code
// generation.
type Resolved struct {
	*Schema

	SimpleTypeMap  map[string]*SimpleType
	ComplexTypeMap map[string]*ComplexType
	ElementMap     map[string]*Element
	AttrGroupMap   map[string]*AttributeGroup

	// TopElements are global element declarations (like SCL, IED, etc.)
	TopElements []*Element
}

// Resolve validates and resolves a parsed Schema, building lookup
// maps and checking for broken references.
func Resolve(s *Schema) (*Resolved, error) {
	r := &Resolved{
		Schema:         s,
		SimpleTypeMap:  make(map[string]*SimpleType, len(s.SimpleTypes)),
		ComplexTypeMap: make(map[string]*ComplexType, len(s.ComplexTypes)),
		ElementMap:     make(map[string]*Element, len(s.Elements)),
		AttrGroupMap:   make(map[string]*AttributeGroup, len(s.AttributeGroups)),
	}

	for _, st := range s.SimpleTypes {
		if st.Name == "" {
			continue
		}
		r.SimpleTypeMap[st.Name] = st
	}
	for _, ct := range s.ComplexTypes {
		if ct.Name == "" {
			continue
		}
		if _, dup := r.ComplexTypeMap[ct.Name]; dup {
			return nil, fmt.Errorf("duplicate complexType %q", ct.Name)
		}
		r.ComplexTypeMap[ct.Name] = ct
	}
	for _, el := range s.Elements {
		if el.Name == "" {
			continue
		}
		r.ElementMap[el.Name] = el
		r.TopElements = append(r.TopElements, el)
	}
	for _, ag := range s.AttributeGroups {
		r.AttrGroupMap[ag.Name] = ag
	}

	sort.Slice(r.TopElements, func(i, j int) bool {
		return r.TopElements[i].Name < r.TopElements[j].Name
	})

	if err := r.validate(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Resolved) validate() error {
	var errs []string

	for _, ct := range r.ComplexTypes {
		if ct.BaseType != "" && !isBuiltinXSD(ct.BaseType) {
			if _, ok := r.ComplexTypeMap[ct.BaseType]; !ok {
				if _, ok := r.SimpleTypeMap[ct.BaseType]; !ok {
					errs = append(errs, fmt.Sprintf("complexType %q: unresolved base type %q", ct.Name, ct.BaseType))
				}
			}
		}
		for _, attr := range ct.Attributes {
			if attr.Type != "" && !isBuiltinXSD(attr.Type) {
				if _, ok := r.SimpleTypeMap[attr.Type]; !ok {
					if _, ok := r.ComplexTypeMap[attr.Type]; !ok {
						errs = append(errs, fmt.Sprintf("complexType %q, attr %q: unresolved type %q", ct.Name, attr.Name, attr.Type))
					}
				}
			}
		}
		for _, ref := range ct.AttrGroupRefs {
			if _, ok := r.AttrGroupMap[ref]; !ok {
				errs = append(errs, fmt.Sprintf("complexType %q: unresolved attributeGroup %q", ct.Name, ref))
			}
		}
		for _, el := range ct.Sequence {
			if err := r.validateElementRef(ct.Name, el); err != "" {
				errs = append(errs, err)
			}
		}
		if ct.Choice != nil {
			for _, el := range ct.Choice.Elements {
				if err := r.validateElementRef(ct.Name, el); err != "" {
					errs = append(errs, err)
				}
			}
		}
	}

	for _, el := range r.Elements {
		if el.Type != "" && !isBuiltinXSD(el.Type) {
			if _, ok := r.ComplexTypeMap[el.Type]; !ok {
				if _, ok := r.SimpleTypeMap[el.Type]; !ok {
					errs = append(errs, fmt.Sprintf("top element %q: unresolved type %q", el.Name, el.Type))
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("schema validation errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

func (r *Resolved) validateElementRef(ctxType string, el *Element) string {
	if el.Ref != "" {
		if _, ok := r.ElementMap[el.Ref]; !ok {
			return fmt.Sprintf("complexType %q: unresolved element ref %q", ctxType, el.Ref)
		}
	}
	if el.Type != "" && !isBuiltinXSD(el.Type) {
		if _, ok := r.ComplexTypeMap[el.Type]; !ok {
			if _, ok := r.SimpleTypeMap[el.Type]; !ok {
				return fmt.Sprintf("complexType %q, element %q: unresolved type %q", ctxType, el.Name, el.Type)
			}
		}
	}
	return ""
}

// ExpandedAttributes returns all attributes for a complex type,
// including those inherited from attributeGroups.
func (r *Resolved) ExpandedAttributes(ct *ComplexType) []*Attribute {
	var attrs []*Attribute
	attrs = append(attrs, ct.Attributes...)
	for _, ref := range ct.AttrGroupRefs {
		if ag, ok := r.AttrGroupMap[ref]; ok {
			attrs = append(attrs, ag.Attributes...)
		}
	}
	return attrs
}

// BaseChain returns the inheritance chain for a complex type,
// from most derived to most base.
func (r *Resolved) BaseChain(ct *ComplexType) []*ComplexType {
	var chain []*ComplexType
	seen := make(map[string]bool)
	cur := ct
	for cur != nil && cur.BaseType != "" && !isBuiltinXSD(cur.BaseType) {
		if seen[cur.BaseType] {
			break
		}
		seen[cur.BaseType] = true
		base, ok := r.ComplexTypeMap[cur.BaseType]
		if !ok {
			break
		}
		chain = append(chain, base)
		cur = base
	}
	return chain
}

// AllAttributes returns all attributes for a type including
// inherited ones from the base chain, with attributeGroups expanded.
func (r *Resolved) AllAttributes(ct *ComplexType) []*Attribute {
	chain := r.BaseChain(ct)

	seen := make(map[string]bool)
	var result []*Attribute

	// Own attributes first (most derived)
	for _, a := range r.ExpandedAttributes(ct) {
		if !seen[a.Name] {
			seen[a.Name] = true
			result = append(result, a)
		}
	}

	// Then base attributes
	for _, base := range chain {
		for _, a := range r.ExpandedAttributes(base) {
			if !seen[a.Name] {
				seen[a.Name] = true
				result = append(result, a)
			}
		}
	}

	return result
}

// AllSequenceElements returns all child elements for a type
// including inherited ones from the base chain.
func (r *Resolved) AllSequenceElements(ct *ComplexType) []*Element {
	chain := r.BaseChain(ct)

	var result []*Element

	// Base elements first (from most base to derived)
	for i := len(chain) - 1; i >= 0; i-- {
		result = append(result, chain[i].Sequence...)
	}
	// Then own elements
	result = append(result, ct.Sequence...)

	return result
}

var builtinTypes = map[string]bool{
	"xs:string": true, "xs:normalizedString": true, "xs:token": true,
	"xs:Name": true, "xs:NCName": true, "xs:anyURI": true,
	"xs:boolean": true, "xs:integer": true, "xs:int": true,
	"xs:unsignedInt": true, "xs:unsignedByte": true, "xs:unsignedShort": true,
	"xs:short": true, "xs:long": true, "xs:unsignedLong": true,
	"xs:decimal": true, "xs:float": true, "xs:double": true,
	"xs:dateTime": true, "xs:date": true, "xs:time": true,
	"xs:duration": true, "xs:language": true,
	"xs:positiveInteger": true, "xs:nonNegativeInteger": true,
	"xs:hexBinary": true, "xs:base64Binary": true,
	"xs:NMTOKEN": true, "xs:ID": true,
}

func isBuiltinXSD(name string) bool {
	return builtinTypes[name]
}

// IsEnumType returns true if the simple type defines an enumeration.
func IsEnumType(st *SimpleType) bool {
	return len(st.Enumerations) > 0
}
