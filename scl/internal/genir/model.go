// Package genir provides the intermediate representation for the SCL
// code generator. The IR is version-neutral and independent of Go
// code emission.
package genir

// Schema is the top-level intermediate representation of a single
// SCL XSD bundle (e.g. 2007C5). It aggregates all definitions
// discovered across the included XSD files.
type Schema struct {
	Version   string // e.g. "v2007c5"
	Bundle    string // directory name of the source bundle
	Namespace string // target namespace

	SimpleTypes     []*SimpleType
	ComplexTypes    []*ComplexType
	Elements        []*Element
	AttributeGroups []*AttributeGroup
}

// SimpleType represents an xs:simpleType.
type SimpleType struct {
	Name         string
	Doc          string
	Base         string   // restriction base type (e.g. "xs:normalizedString")
	Enumerations []string // enum values when restriction has xs:enumeration
	UnionMembers []string // for xs:union memberTypes
	Patterns     []string // xs:pattern values
	MinLength    int
	MaxLength    int
}

// ComplexType represents an xs:complexType.
type ComplexType struct {
	Name     string
	Doc      string
	Abstract bool
	Mixed    bool

	// Content model: either direct children or via complexContent/simpleContent
	BaseType      string // extension/restriction base (empty if none)
	ContentKind   ContentKind
	Sequence      []*Element
	Choice        *ChoiceGroup
	Attributes    []*Attribute
	AttrGroupRefs []string // references to attributeGroup names
	HasAnyElement bool     // xs:any present
	HasAnyAttr    bool     // xs:anyAttribute present
}

// ContentKind describes how a complex type defines its content.
type ContentKind int

const (
	ContentDirect          ContentKind = iota // direct sequence/attributes
	ContentComplexExtend                      // complexContent + extension
	ContentSimpleExtend                       // simpleContent + extension
	ContentComplexRestrict                    // complexContent + restriction
	ContentSimpleRestrict                     // simpleContent + restriction
)

// Element represents an xs:element declaration.
type Element struct {
	Name      string
	Doc       string
	Type      string // type reference (e.g. "tIED")
	Ref       string // element ref (e.g. "IED") — mutually exclusive with Type
	MinOccurs int    // default 1
	MaxOccurs int    // -1 means unbounded

	// Inline complex type (when no type attribute is given)
	InlineComplex *ComplexType
}

// Attribute represents an xs:attribute declaration.
type Attribute struct {
	Name    string
	Type    string // type reference
	Use     string // "required", "optional", or ""
	Default string // default value
	Fixed   string // fixed value

	// Inline simple type for anonymous restrictions
	InlineSimple *SimpleType
}

// AttributeGroup represents an xs:attributeGroup definition.
type AttributeGroup struct {
	Name       string
	Attributes []*Attribute
}

// ChoiceGroup represents an xs:choice element.
type ChoiceGroup struct {
	Elements  []*Element
	MinOccurs int
	MaxOccurs int // -1 means unbounded
}
