// SPDX-License-Identifier: MIT

package genir

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// xsdSchema is the raw XML representation of an xs:schema element.
type xsdSchema struct {
	XMLName         xml.Name         `xml:"schema"`
	TargetNamespace string           `xml:"targetNamespace,attr"`
	Version         string           `xml:"version,attr"`
	Includes        []xsdInclude     `xml:"include"`
	SimpleTypes     []xsdSimpleType  `xml:"simpleType"`
	ComplexTypes    []xsdComplexType `xml:"complexType"`
	Elements        []xsdElement     `xml:"element"`
	AttrGroups      []xsdAttrGroup   `xml:"attributeGroup"`
}

type xsdInclude struct {
	SchemaLocation string `xml:"schemaLocation,attr"`
}

type xsdAnnotation struct {
	Documentation []xsdDocumentation `xml:"documentation"`
}

type xsdDocumentation struct {
	Value string `xml:",chardata"`
}

type xsdSimpleType struct {
	Name        string          `xml:"name,attr"`
	Annotation  xsdAnnotation   `xml:"annotation"`
	Restriction *xsdRestriction `xml:"restriction"`
	Union       *xsdUnion       `xml:"union"`
}

type xsdRestriction struct {
	Base         string           `xml:"base,attr"`
	Enumerations []xsdEnumeration `xml:"enumeration"`
	Patterns     []xsdPattern     `xml:"pattern"`
	MinLength    *xsdFacet        `xml:"minLength"`
	MaxLength    *xsdFacet        `xml:"maxLength"`
	MinExclusive *xsdFacet        `xml:"minExclusive"`

	// For simpleContent/complexContent restrictions
	Attributes []xsdAttribute `xml:"attribute"`
	Sequence   *xsdSequence   `xml:"sequence"`
}

type xsdEnumeration struct {
	Value string `xml:"value,attr"`
}

type xsdPattern struct {
	Value string `xml:"value,attr"`
}

type xsdFacet struct {
	Value string `xml:"value,attr"`
}

type xsdUnion struct {
	MemberTypes string `xml:"memberTypes,attr"`
}

type xsdComplexType struct {
	Name           string             `xml:"name,attr"`
	Abstract       string             `xml:"abstract,attr"`
	Mixed          string             `xml:"mixed,attr"`
	Annotation     xsdAnnotation      `xml:"annotation"`
	Sequence       *xsdSequence       `xml:"sequence"`
	All            *xsdSequence       `xml:"all"`
	Choice         *xsdChoice         `xml:"choice"`
	ComplexContent *xsdComplexContent `xml:"complexContent"`
	SimpleContent  *xsdSimpleContent  `xml:"simpleContent"`
	Attributes     []xsdAttribute     `xml:"attribute"`
	AttrGroupRefs  []xsdAttrGroupRef  `xml:"attributeGroup"`
	AnyAttr        *xsdAny            `xml:"anyAttribute"`
}

type xsdSequence struct {
	MinOccurs string        `xml:"minOccurs,attr"`
	MaxOccurs string        `xml:"maxOccurs,attr"`
	Elements  []xsdElement  `xml:"element"`
	Choices   []xsdChoice   `xml:"choice"`
	Anys      []xsdAny      `xml:"any"`
	Sequences []xsdSequence `xml:"sequence"`
}

type xsdChoice struct {
	MinOccurs string       `xml:"minOccurs,attr"`
	MaxOccurs string       `xml:"maxOccurs,attr"`
	Elements  []xsdElement `xml:"element"`
}

type xsdElement struct {
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	Ref         string          `xml:"ref,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	Annotation  xsdAnnotation   `xml:"annotation"`
	ComplexType *xsdComplexType `xml:"complexType"`
	Unique      []xsdIdentity   `xml:"unique"`
	Key         []xsdIdentity   `xml:"key"`
	Keyref      []xsdIdentity   `xml:"keyref"`
}

type xsdIdentity struct {
	Name string `xml:"name,attr"`
}

type xsdAttribute struct {
	Name       string         `xml:"name,attr"`
	Type       string         `xml:"type,attr"`
	Use        string         `xml:"use,attr"`
	Default    string         `xml:"default,attr"`
	Fixed      string         `xml:"fixed,attr"`
	SimpleType *xsdSimpleType `xml:"simpleType"`
}

type xsdAttrGroup struct {
	Name       string         `xml:"name,attr"`
	Ref        string         `xml:"ref,attr"`
	Attributes []xsdAttribute `xml:"attribute"`
}

type xsdAttrGroupRef = xsdAttrGroup

type xsdComplexContent struct {
	Extension   *xsdExtension   `xml:"extension"`
	Restriction *xsdRestriction `xml:"restriction"`
}

type xsdSimpleContent struct {
	Extension   *xsdExtension   `xml:"extension"`
	Restriction *xsdRestriction `xml:"restriction"`
}

type xsdExtension struct {
	Base          string            `xml:"base,attr"`
	Sequence      *xsdSequence      `xml:"sequence"`
	Choice        *xsdChoice        `xml:"choice"`
	Attributes    []xsdAttribute    `xml:"attribute"`
	AttrGroupRefs []xsdAttrGroupRef `xml:"attributeGroup"`
	AnyAttr       *xsdAny           `xml:"anyAttribute"`
}

type xsdAny struct {
	Namespace       string `xml:"namespace,attr"`
	ProcessContents string `xml:"processContents,attr"`
	MinOccurs       string `xml:"minOccurs,attr"`
	MaxOccurs       string `xml:"maxOccurs,attr"`
}

// ParseBundle parses an SCL XSD bundle starting from SCL.xsd in the
// given directory. It resolves all xs:include references and returns
// a unified Schema IR.
func ParseBundle(dir string, version string) (*Schema, error) {
	schema := &Schema{
		Version: version,
		Bundle:  filepath.Base(dir),
	}

	loaded := make(map[string]bool)
	if err := loadXSD(dir, "SCL.xsd", schema, loaded); err != nil {
		return nil, fmt.Errorf("parse bundle %s: %w", dir, err)
	}

	return schema, nil
}

func loadXSD(dir, filename string, schema *Schema, loaded map[string]bool) error {
	if loaded[filename] {
		return nil
	}
	loaded[filename] = true

	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var raw xsdSchema
	if err := xml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	if schema.Namespace == "" {
		schema.Namespace = raw.TargetNamespace
	}

	for _, inc := range raw.Includes {
		loc := inc.SchemaLocation
		if isIgnoredFile(loc) {
			continue
		}
		if err := loadXSD(dir, loc, schema, loaded); err != nil {
			return err
		}
	}

	for i := range raw.SimpleTypes {
		st := convertSimpleType(&raw.SimpleTypes[i])
		schema.SimpleTypes = append(schema.SimpleTypes, st)
	}

	for i := range raw.ComplexTypes {
		ct := convertComplexType(&raw.ComplexTypes[i])
		schema.ComplexTypes = append(schema.ComplexTypes, ct)
	}

	for i := range raw.Elements {
		el := convertElement(&raw.Elements[i])
		schema.Elements = append(schema.Elements, el)
	}

	for i := range raw.AttrGroups {
		ag := convertAttrGroup(&raw.AttrGroups[i])
		if ag != nil {
			schema.AttributeGroups = append(schema.AttributeGroups, ag)
		}
	}

	return nil
}

func isIgnoredFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "ieccopyright") ||
		strings.Contains(lower, "iecmanifest")
}

func convertSimpleType(raw *xsdSimpleType) *SimpleType {
	st := &SimpleType{
		Name: raw.Name,
		Doc:  extractDoc(raw.Annotation),
	}

	if raw.Union != nil {
		members := strings.Fields(raw.Union.MemberTypes)
		for i, m := range members {
			members[i] = stripNSPrefix(m)
		}
		st.UnionMembers = members
	}

	if raw.Restriction != nil {
		st.Base = stripNSPrefix(raw.Restriction.Base)
		for _, e := range raw.Restriction.Enumerations {
			st.Enumerations = append(st.Enumerations, e.Value)
		}
		for _, p := range raw.Restriction.Patterns {
			st.Patterns = append(st.Patterns, p.Value)
		}
		if raw.Restriction.MinLength != nil {
			st.MinLength, _ = strconv.Atoi(raw.Restriction.MinLength.Value)
		}
		if raw.Restriction.MaxLength != nil {
			st.MaxLength, _ = strconv.Atoi(raw.Restriction.MaxLength.Value)
		}
	}

	return st
}

func convertComplexType(raw *xsdComplexType) *ComplexType {
	ct := &ComplexType{
		Name:     raw.Name,
		Doc:      extractDoc(raw.Annotation),
		Abstract: raw.Abstract == "true",
		Mixed:    raw.Mixed == "true",
	}

	if raw.ComplexContent != nil {
		if ext := raw.ComplexContent.Extension; ext != nil {
			ct.ContentKind = ContentComplexExtend
			ct.BaseType = stripNSPrefix(ext.Base)
			appendExtensionContent(ct, ext)
		} else if rest := raw.ComplexContent.Restriction; rest != nil {
			ct.ContentKind = ContentComplexRestrict
			ct.BaseType = stripNSPrefix(rest.Base)
			if rest.Sequence != nil {
				convertSequenceElements(rest.Sequence, ct)
			}
			for i := range rest.Attributes {
				ct.Attributes = append(ct.Attributes, convertAttribute(&rest.Attributes[i]))
			}
		}
		return ct
	}

	if raw.SimpleContent != nil {
		if ext := raw.SimpleContent.Extension; ext != nil {
			ct.ContentKind = ContentSimpleExtend
			ct.BaseType = stripNSPrefix(ext.Base)
			for i := range ext.Attributes {
				ct.Attributes = append(ct.Attributes, convertAttribute(&ext.Attributes[i]))
			}
			for _, agr := range ext.AttrGroupRefs {
				ref := stripNSPrefix(agr.Ref)
				if ref != "" {
					ct.AttrGroupRefs = append(ct.AttrGroupRefs, ref)
				}
			}
		} else if rest := raw.SimpleContent.Restriction; rest != nil {
			ct.ContentKind = ContentSimpleRestrict
			ct.BaseType = stripNSPrefix(rest.Base)
			for i := range rest.Attributes {
				ct.Attributes = append(ct.Attributes, convertAttribute(&rest.Attributes[i]))
			}
		}
		return ct
	}

	ct.ContentKind = ContentDirect
	if raw.Sequence != nil {
		convertSequenceElements(raw.Sequence, ct)
	}
	if raw.All != nil {
		convertSequenceElements(raw.All, ct)
	}
	if raw.Choice != nil {
		ct.Choice = convertChoice(raw.Choice)
	}
	for i := range raw.Attributes {
		ct.Attributes = append(ct.Attributes, convertAttribute(&raw.Attributes[i]))
	}
	for _, agr := range raw.AttrGroupRefs {
		ref := stripNSPrefix(agr.Ref)
		if ref != "" {
			ct.AttrGroupRefs = append(ct.AttrGroupRefs, ref)
		}
	}
	if raw.AnyAttr != nil {
		ct.HasAnyAttr = true
	}

	return ct
}

func appendExtensionContent(ct *ComplexType, ext *xsdExtension) {
	if ext.Sequence != nil {
		convertSequenceElements(ext.Sequence, ct)
	}
	if ext.Choice != nil {
		ct.Choice = convertChoice(ext.Choice)
	}
	for i := range ext.Attributes {
		ct.Attributes = append(ct.Attributes, convertAttribute(&ext.Attributes[i]))
	}
	for _, agr := range ext.AttrGroupRefs {
		ref := stripNSPrefix(agr.Ref)
		if ref != "" {
			ct.AttrGroupRefs = append(ct.AttrGroupRefs, ref)
		}
	}
	if ext.AnyAttr != nil {
		ct.HasAnyAttr = true
	}
}

func convertSequenceElements(seq *xsdSequence, ct *ComplexType) {
	for _, a := range seq.Anys {
		_ = a
		ct.HasAnyElement = true
	}
	for i := range seq.Elements {
		ct.Sequence = append(ct.Sequence, convertElement(&seq.Elements[i]))
	}
	for i := range seq.Choices {
		ch := convertChoice(&seq.Choices[i])
		if ct.Choice == nil {
			ct.Choice = ch
		} else {
			ct.Choice.Elements = append(ct.Choice.Elements, ch.Elements...)
		}
	}
	for i := range seq.Sequences {
		convertSequenceElements(&seq.Sequences[i], ct)
	}
}

func convertElement(raw *xsdElement) *Element {
	el := &Element{
		Name:      raw.Name,
		Type:      stripNSPrefix(raw.Type),
		Ref:       stripNSPrefix(raw.Ref),
		Doc:       extractDoc(raw.Annotation),
		MinOccurs: 1,
		MaxOccurs: 1,
	}

	if raw.MinOccurs != "" {
		el.MinOccurs, _ = strconv.Atoi(raw.MinOccurs)
	}
	if raw.MaxOccurs != "" {
		if raw.MaxOccurs == "unbounded" {
			el.MaxOccurs = -1
		} else {
			el.MaxOccurs, _ = strconv.Atoi(raw.MaxOccurs)
		}
	}

	if raw.ComplexType != nil {
		el.InlineComplex = convertComplexType(raw.ComplexType)
	}

	return el
}

func convertAttribute(raw *xsdAttribute) *Attribute {
	attr := &Attribute{
		Name:    raw.Name,
		Type:    stripNSPrefix(raw.Type),
		Use:     raw.Use,
		Default: raw.Default,
		Fixed:   raw.Fixed,
	}
	if raw.SimpleType != nil {
		attr.InlineSimple = convertSimpleType(raw.SimpleType)
	}
	return attr
}

func convertAttrGroup(raw *xsdAttrGroup) *AttributeGroup {
	if raw.Name == "" {
		return nil
	}
	ag := &AttributeGroup{Name: raw.Name}
	for i := range raw.Attributes {
		ag.Attributes = append(ag.Attributes, convertAttribute(&raw.Attributes[i]))
	}
	return ag
}

func convertChoice(raw *xsdChoice) *ChoiceGroup {
	cg := &ChoiceGroup{
		MinOccurs: 0,
		MaxOccurs: 1,
	}
	if raw.MinOccurs != "" {
		cg.MinOccurs, _ = strconv.Atoi(raw.MinOccurs)
	}
	if raw.MaxOccurs != "" {
		if raw.MaxOccurs == "unbounded" {
			cg.MaxOccurs = -1
		} else {
			cg.MaxOccurs, _ = strconv.Atoi(raw.MaxOccurs)
		}
	}
	for i := range raw.Elements {
		cg.Elements = append(cg.Elements, convertElement(&raw.Elements[i]))
	}
	return cg
}

// stripNSPrefix removes the namespace prefix from a qualified name.
// e.g. "scl:tIED" → "tIED", "xs:string" stays "xs:string" since
// xs: is the XSD namespace we keep for builtin detection.
func stripNSPrefix(name string) string {
	if name == "" {
		return name
	}
	if strings.HasPrefix(name, "xs:") {
		return name
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func extractDoc(ann xsdAnnotation) string {
	var parts []string
	for _, d := range ann.Documentation {
		s := strings.TrimSpace(d.Value)
		if s != "" && !strings.Contains(s, "COPYRIGHT") {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}
