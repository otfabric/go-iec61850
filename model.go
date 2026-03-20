package iec61850

import "github.com/otfabric/go-mms"

// LogicalDevice represents a logical device discovered on the server.
// A logical device maps to an MMS domain.
type LogicalDevice struct {
	// Name is the logical device name (MMS domain ID).
	Name string
}

// LogicalNode represents a logical node within a logical device.
// In the MMS mapping, logical nodes appear as the first $-delimited
// segment of named variables within a domain.
type LogicalNode struct {
	// Name is the logical node name (e.g., "LLN0", "GGIO1").
	Name string

	// LD is the parent logical device name.
	LD string
}

// Ref returns the IEC 61850 object reference for this logical node.
func (ln LogicalNode) Ref() Ref {
	return Ref{LD: ln.LD, LN: ln.Name}
}

// DataObject represents a browsed model component, which may be a
// data object, sub-data object, or data attribute. It captures name,
// reference, and optional MMS type information.
type DataObject struct {
	// Name is the component name (e.g., "Mod", "stVal", "mag").
	Name string

	// Reference is the full IEC 61850 object reference.
	Reference Ref

	// FC is the functional constraint, when unambiguous. Empty during
	// browse/tree discovery where the same object may appear under
	// multiple FCs. Populated only when a single FC applies or when
	// explicitly set by the caller.
	FC FunctionalConstraint

	// Children contains sub-objects (nested DOs or DAs).
	// Populated only when the tree is built via [Client.Tree] or
	// explicit recursive browsing.
	Children []DataObject
}

// ModelNode represents a node in the IEC 61850 data model tree.
// Used by [Client.Tree] to represent the full model hierarchy.
type ModelNode struct {
	// Name is the local name of this node (LD name, LN name, or
	// path component name).
	Name string

	// Reference is the object reference for this node. During browse/
	// tree discovery the FC field is typically empty because the same
	// path may appear under multiple FCs. Use [Ref.WithFC],
	// [ModelNode.FC], or [ModelNode.FCs] to obtain an FC-qualified
	// reference suitable for read/write operations.
	Reference Ref

	// FC is the functional constraint when a single unambiguous FC
	// applies. Set only when exactly one FC is observed for this
	// node (e.g., via [TreeOptions.IncludeFCs] or caller-assigned).
	FC FunctionalConstraint

	// FCs contains all functional constraints observed for this
	// node. Populated when [TreeOptions.IncludeFCs] is true.
	// Multiple entries indicate the node appears under several FCs
	// (common for DOs containing attributes in ST, MX, CF, etc.).
	// Empty when FC annotation is not requested.
	FCs []FunctionalConstraint

	// Type contains MMS type information when available (from
	// GetVariableAccessAttributes). Nil when type info has not
	// been retrieved.
	Type *mms.TypeSpec

	// Children contains the child nodes in the hierarchy.
	Children []*ModelNode
}

// BrowseNode represents a generic browse child returned by
// [Client.ListChildren]. Unlike [DataObject], a BrowseNode does not
// imply that the node is a data object — it may be a sub-data object,
// data attribute, or any other child in the FC-merged browse view.
type BrowseNode struct {
	// Name is the local component name (e.g., "stVal", "mag").
	Name string

	// Reference is the full IEC 61850 object reference.
	Reference Ref
}

// MatchMode selects the pattern matching algorithm for [FindQuery].
type MatchMode int

const (
	// MatchGlob uses [path.Match] glob semantics (default). '*'
	// matches any characters within a single component, '?' matches
	// a single character.
	MatchGlob MatchMode = iota

	// MatchRegex uses Go [regexp] semantics. The pattern is compiled
	// with [regexp.Compile] and matched against the full object
	// reference string.
	MatchRegex
)

// FindQuery specifies search criteria for [Client.FindPaths].
type FindQuery struct {
	// Pattern is matched against the object reference (without FC
	// suffix). The matching algorithm is determined by [MatchMode].
	//
	// Glob examples (default):
	//   "*/LLN0*"           — all LLN0 nodes across all LDs
	//   "LD1/GGIO1.Ind*"    — all Ind objects under GGIO1
	//
	// Regex examples:
	//   ".*GGIO1\\.Ind[12]" — Ind1 and Ind2 under any GGIO1
	Pattern string

	// MatchMode selects the pattern matching algorithm. The default
	// ([MatchGlob]) preserves backward compatibility.
	MatchMode MatchMode

	// FC optionally filters results to a specific functional
	// constraint. When empty, all FCs are included.
	FC FunctionalConstraint

	// LDFilter, when non-empty, restricts the search to a single
	// logical device. This avoids fetching variable lists from
	// every LD, which can be expensive on servers with many domains.
	LDFilter string

	// MaxDepth limits traversal depth based on [Ref.Depth]. Zero means
	// unlimited. Depth values correspond to:
	//   1 = LD only
	//   2 = LN
	//   3 = first data object level (LD/LN.DO)
	//   4+ = deeper data attributes
	MaxDepth int
}
