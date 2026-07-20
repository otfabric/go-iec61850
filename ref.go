// SPDX-License-Identifier: MIT

package iec61850

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-mms"
)

// Ref is a parsed IEC 61850 object reference.
//
// An IEC 61850 object reference identifies a node in the data model
// hierarchy: LogicalDevice / LogicalNode . DataObject . DataAttribute.
// An optional functional constraint can be appended in brackets.
//
// Format: LD/LN.DO.DA[FC]
//
// A Ref may represent different levels of the hierarchy:
//
//   - LD-only (LN empty, no path) — identifies a logical device for
//     browsing and GetNameList operations. Not all operations accept
//     LD-only refs; read/write require at least LN + FC.
//   - LN-level (LN set, no path) — identifies a logical node.
//   - Object-level (LN set, path present) — identifies a data object
//     or data attribute.
//
// Examples:
//
//	simpleIOGenericIO/GGIO1.Ind1.stVal[ST]
//	simpleIOGenericIO/GGIO1.Ind1.stVal
//	simpleIOGenericIO/LLN0
type Ref struct {
	// LD is the logical device name.
	LD string
	// LN is the logical node name (e.g., "LLN0", "GGIO1").
	LN string
	// Path contains the data object / data attribute path components.
	// Empty when the reference points to a logical node.
	Path []string
	// FC is the functional constraint. Empty when not specified.
	FC FunctionalConstraint
}

// ParseRef parses an IEC 61850 object reference string.
//
// Accepted formats:
//
//	LD/LN.DO.DA[FC]   — full reference with functional constraint
//	LD/LN.DO.DA        — reference without FC
//	LD/LN              — logical node reference
//	LD                 — logical device reference (LN will be empty)
//
// Returns a [*ReferenceError] wrapping [ErrInvalidReference] on
// malformed input.
//
// The parser accepts references of any reasonable length. Use
// [ParseRefStrict] to enforce the conservative 129-character limit
// inspired by IEC 61850-7-2 VisibleString129.
//
// Unknown FC values (not in the standard set) are accepted. Use
// [StrictnessOptions.RejectUnknownFC] at call sites or
// [FunctionalConstraint.IsValid] for semantic FC validation.
func ParseRef(s string) (Ref, error) {
	if s == "" {
		return Ref{}, &ReferenceError{Input: s, Reason: "empty reference"}
	}

	openCount := strings.Count(s, "[")
	closeCount := strings.Count(s, "]")
	if openCount != closeCount {
		return Ref{}, &ReferenceError{Input: s, Reason: "mismatched brackets"}
	}
	if openCount > 1 {
		return Ref{}, &ReferenceError{Input: s, Reason: "multiple bracket pairs"}
	}

	var fc FunctionalConstraint
	work := s

	if idx := strings.Index(work, "["); idx >= 0 {
		end := strings.Index(work, "]")
		if end < 0 || end != len(work)-1 {
			return Ref{}, &ReferenceError{Input: s, Reason: "malformed functional constraint brackets"}
		}
		fcStr := work[idx+1 : end]
		if len(fcStr) != 2 {
			return Ref{}, &ReferenceError{Input: s, Reason: fmt.Sprintf("functional constraint must be 2 characters, got %q", fcStr)}
		}
		fc = FunctionalConstraint(fcStr)
		work = work[:idx]
	}

	slashIdx := strings.Index(work, "/")

	if slashIdx < 0 {
		if strings.Contains(work, ".") {
			return Ref{}, &ReferenceError{Input: s, Reason: "missing logical device separator '/'"}
		}
		ref := Ref{LD: work, FC: fc}
		if err := ref.Validate(); err != nil {
			return Ref{}, err
		}
		return ref, nil
	}

	ld := work[:slashIdx]
	if ld == "" {
		return Ref{}, &ReferenceError{Input: s, Reason: "empty logical device name"}
	}
	if len(ld) > 64 {
		return Ref{}, &ReferenceError{Input: s, Reason: "logical device name exceeds 64 characters"}
	}

	rest := work[slashIdx+1:]
	if rest == "" {
		return Ref{}, &ReferenceError{Input: s, Reason: "empty content after '/'"}
	}

	dotIdx := strings.Index(rest, ".")
	if dotIdx < 0 {
		ref := Ref{LD: ld, LN: rest, FC: fc}
		if err := ref.Validate(); err != nil {
			return Ref{}, err
		}
		return ref, nil
	}

	ln := rest[:dotIdx]
	if ln == "" {
		return Ref{}, &ReferenceError{Input: s, Reason: "empty logical node name"}
	}

	pathStr := rest[dotIdx+1:]
	if pathStr == "" {
		return Ref{}, &ReferenceError{Input: s, Reason: "empty path after logical node"}
	}

	parts := strings.Split(pathStr, ".")

	ref := Ref{LD: ld, LN: ln, Path: parts, FC: fc}
	if err := ref.Validate(); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// ParseRefStrict is like [ParseRef] but enforces a maximum reference
// length of 129 characters, inspired by the IEC 61850-7-2 MMS
// VisibleString129 type for ObjectReference attributes.
func ParseRefStrict(s string) (Ref, error) {
	if len(s) > 129 {
		return Ref{}, &ReferenceError{Input: s, Reason: "exceeds maximum length (129)"}
	}
	return ParseRef(s)
}

// Validate checks the structural invariants of the Ref.
//
// Rules:
//   - LD must be non-empty
//   - if LN is empty, Path must also be empty
//   - if LN is empty, FC must also be empty (FC requires at least LN)
//   - no empty path components
//   - if FC is non-empty, it must be exactly 2 characters (length check
//     only — unknown FC values are accepted; use [FunctionalConstraint.IsValid]
//     for semantic validation)
//   - components must not contain separators (/, ., $, [, ])
func (r Ref) Validate() error {
	if r.LD == "" {
		return &ReferenceError{Input: r.String(), Reason: "empty logical device name"}
	}
	if err := validateComponent(r.LD, "logical device"); err != nil {
		return &ReferenceError{Input: r.String(), Reason: err.Error()}
	}
	if r.LN == "" && len(r.Path) > 0 {
		return &ReferenceError{Input: r.String(), Reason: "path present without logical node"}
	}
	if r.FC != "" && r.LN == "" {
		return &ReferenceError{Input: r.String(), Reason: "functional constraint requires logical node"}
	}
	if r.LN != "" {
		if err := validateComponent(r.LN, "logical node"); err != nil {
			return &ReferenceError{Input: r.String(), Reason: err.Error()}
		}
	}
	for i, p := range r.Path {
		if p == "" {
			return &ReferenceError{Input: r.String(), Reason: fmt.Sprintf("empty path component at position %d", i)}
		}
		if err := validateComponent(p, fmt.Sprintf("path component [%d]", i)); err != nil {
			return &ReferenceError{Input: r.String(), Reason: err.Error()}
		}
	}
	if r.FC != "" && len(r.FC) != 2 {
		return &ReferenceError{Input: r.String(), Reason: fmt.Sprintf("functional constraint must be 2 characters, got %q", r.FC)}
	}
	return nil
}

// validateComponent rejects names containing IEC 61850 / MMS separators.
func validateComponent(name, label string) error {
	if strings.ContainsAny(name, "/.$[]") {
		return fmt.Errorf("%s %q contains illegal separator character", label, name)
	}
	return nil
}

// String returns the canonical IEC 61850 object reference string.
func (r Ref) String() string {
	var b strings.Builder
	b.WriteString(r.LD)

	if r.LN != "" {
		b.WriteByte('/')
		b.WriteString(r.LN)
		if len(r.Path) > 0 {
			b.WriteByte('.')
			b.WriteString(strings.Join(r.Path, "."))
		}
	}

	if r.FC != "" {
		b.WriteByte('[')
		b.WriteString(string(r.FC))
		b.WriteByte(']')
	}

	return b.String()
}

// ObjectReference returns the reference formatted without the FC
// bracket suffix.
func (r Ref) ObjectReference() string {
	noFC := r
	noFC.FC = ""
	return noFC.String()
}

// WithFC returns a copy of r with the functional constraint set to fc.
func (r Ref) WithFC(fc FunctionalConstraint) Ref {
	r.FC = fc
	return r
}

// Parent returns the parent reference. For a DA reference, this returns
// the parent DO or LN. Returns false if r is already at the top level
// (LD-only).
//
// The functional constraint is preserved when the returned parent still
// refers to a logical node or object path. When the parent is LD-only,
// the FC is dropped because LD[FC] is not a meaningful IEC 61850
// reference shape.
func (r Ref) Parent() (Ref, bool) {
	if len(r.Path) > 0 {
		return Ref{
			LD:   r.LD,
			LN:   r.LN,
			Path: r.Path[:len(r.Path)-1],
			FC:   r.FC,
		}, true
	}
	if r.LN != "" {
		return Ref{LD: r.LD}, true
	}
	return Ref{}, false
}

// Child returns a new reference with name appended to the path.
// Returns an error if r has no logical node (LD-only), if name is
// empty, or if name contains separator characters (/, ., $, [, ]).
func (r Ref) Child(name string) (Ref, error) {
	if r.LN == "" {
		return Ref{}, &ReferenceError{Input: r.String(), Reason: "cannot add child path without logical node"}
	}
	if name == "" {
		return Ref{}, &ReferenceError{Input: r.String(), Reason: "empty child name"}
	}
	if strings.ContainsAny(name, "/.$[]") {
		return Ref{}, &ReferenceError{Input: r.String(), Reason: fmt.Sprintf("child name %q contains illegal separator character", name)}
	}
	path := make([]string, len(r.Path), len(r.Path)+1)
	copy(path, r.Path)
	path = append(path, name)
	return Ref{LD: r.LD, LN: r.LN, Path: path, FC: r.FC}, nil
}

// IsLD reports whether the reference points to a logical device only.
func (r Ref) IsLD() bool {
	return r.LD != "" && r.LN == "" && len(r.Path) == 0
}

// IsLN reports whether the reference points to a logical node (no path).
func (r Ref) IsLN() bool {
	return r.LN != "" && len(r.Path) == 0
}

// IsObject reports whether the reference points to a data object or
// data attribute (LN set with at least one path component).
func (r Ref) IsObject() bool {
	return r.LN != "" && len(r.Path) > 0
}

// HasPath reports whether the reference includes a data object or data
// attribute path.
func (r Ref) HasPath() bool {
	return len(r.Path) > 0
}

// Depth returns the depth of the reference in the hierarchy.
//
//   - 0: zero-value ref (empty LD)
//   - 1: LD only
//   - 2: LD/LN
//   - 3+: LD/LN + path components (depth = 2 + len(Path))
func (r Ref) Depth() int {
	if r.LD == "" {
		return 0
	}
	if r.LN == "" {
		return 1
	}
	return 2 + len(r.Path)
}

// MMS domain/item-ID translation.
//
// IEC 61850 maps to MMS as:
//   - MMS domain = logical device name
//   - MMS item ID = LN$FC$path (with $ separating components)
//
// Example: LD/LN.DO.DA[ST] → domain="LD", itemID="LN$ST$DO$DA"

// ToMMS converts the reference to MMS domain and item ID.
//
// Calls [Ref.Validate] before conversion. Requires a valid FC when
// the reference includes a path.
//
// LN-level refs without FC (e.g., Ref{LD:"LD1", LN:"LLN0"}) produce
// a bare item ID of "LLN0". This is used for GetNameList and other
// browse-oriented MMS operations. It is not the normal read/write
// mapping — MMS read/write at the LN level is a special IEC 61850
// convention that returns the complete named-variable subtree.
// Normal attribute access requires FC-qualified refs.
//
// Examples:
//
//	Ref{LD:"LD1", LN:"LLN0"}                                     → domain="LD1", itemID="LLN0"
//	Ref{LD:"LD1", LN:"LLN0", Path:[]string{"Mod","stVal"}, FC:FCST} → domain="LD1", itemID="LLN0$ST$Mod$stVal"
func (r Ref) ToMMS() (domain mms.DomainID, itemID mms.ItemID, err error) {
	if err := r.Validate(); err != nil {
		return "", "", err
	}

	domain = mms.DomainID(r.LD)

	if r.LN == "" {
		return domain, "", nil
	}

	if len(r.Path) == 0 && r.FC == "" {
		return domain, mms.ItemID(r.LN), nil
	}

	if r.FC == "" {
		return "", "", &ReferenceError{
			Input:  r.String(),
			Reason: "cannot convert to MMS item ID: functional constraint required when path is present",
		}
	}

	var b strings.Builder
	b.WriteString(r.LN)
	b.WriteByte('$')
	b.WriteString(string(r.FC))
	for _, p := range r.Path {
		b.WriteByte('$')
		b.WriteString(p)
	}

	return domain, mms.ItemID(b.String()), nil
}

// RefFromMMS constructs a Ref from an MMS domain ID and item ID.
//
// The item ID is expected to follow the IEC 61850 MMS mapping convention:
// LN$FC$path$components. The FC is extracted from the second $-delimited
// segment.
//
// Unknown FCs are accepted and stored as-is. This matches the library's
// "accept by default, reject via strictness" philosophy. Use
// [StrictnessOptions.RejectUnknownFC] at call sites to enforce stricter
// validation.
//
// Returns an error for empty domain or empty segments within the item ID.
func RefFromMMS(domain mms.DomainID, itemID mms.ItemID) (Ref, error) {
	ld := string(domain)
	if ld == "" {
		return Ref{}, &ReferenceError{Input: string(itemID), Reason: "empty MMS domain"}
	}

	item := string(itemID)
	if item == "" {
		return Ref{LD: ld}, nil
	}

	parts := strings.Split(item, "$")

	ln := parts[0]
	if ln == "" {
		return Ref{}, &ReferenceError{Input: item, Reason: "empty logical node in MMS item ID"}
	}

	if len(parts) == 1 {
		return Ref{LD: ld, LN: ln}, nil
	}

	fcStr := parts[1]
	if fcStr == "" {
		return Ref{}, &ReferenceError{Input: item, Reason: "empty functional constraint in MMS item ID"}
	}
	fc := FunctionalConstraint(fcStr)

	var path []string
	if len(parts) > 2 {
		path = parts[2:]
		for i, p := range path {
			if p == "" {
				return Ref{}, &ReferenceError{Input: item, Reason: fmt.Sprintf("empty path segment at position %d in MMS item ID", i)}
			}
		}
	}

	return Ref{LD: ld, LN: ln, Path: path, FC: fc}, nil
}
