// SPDX-License-Identifier: MIT

package scl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v17 "github.com/otfabric/go-iec61850/scl/internal/raw/v17"
	v2007b "github.com/otfabric/go-iec61850/scl/internal/raw/v2007b"
	v2007b4 "github.com/otfabric/go-iec61850/scl/internal/raw/v2007b4"
	v2007c5 "github.com/otfabric/go-iec61850/scl/internal/raw/v2007c5"
)

// ParseBytes parses raw SCL XML bytes and returns a [Result]
// containing the normalized model, version info, document kind,
// and any diagnostics.
//
// It detects the schema version from the root element, dispatches
// to the correct versioned raw type for unmarshalling, and converts
// the result into the normalized [SCL] model.
func ParseBytes(data []byte, opts ParseOptions) (*Result, error) {
	vi, err := DetectVersion(data)
	if err != nil {
		return nil, err
	}

	kind := opts.Kind
	if kind == "" {
		kind = KindUnknown
	}

	originalNamespace := vi.Namespace
	defaultSpace := ""
	var nsDiag []Diagnostic
	if opts.AssumeSCLNamespace && vi.Namespace == "" {
		defaultSpace = iecNamespace
		vi.Namespace = iecNamespace
		vi.Schema, vi.Confidence, vi.Reasons = classifyVersion(vi)
		nsDiag = []Diagnostic{{
			Severity: DiagWarning,
			Code:     "namespace-assumed",
			Path:     "SCL",
			Message:  fmt.Sprintf("the SCL root element has no namespace; read as %s (the schema requires it)", iecNamespace),
		}}
	}

	result := &Result{
		Version: vi,
		Kind:    kind,
	}

	doc, diags, err := decodeAndConvert(data, vi, defaultSpace)
	if err != nil {
		return nil, err
	}
	diags = append(nsDiag, diags...)
	doc.Metadata = &DocumentMetadata{
		Version:           vi,
		Kind:              kind,
		OriginalNamespace: originalNamespace,
		VendorNamespaces:  vi.VendorNamespaces,
		HasHeader:         rootHasChild(data, "Header"),
	}
	result.Document = doc
	result.Diagnostics = diags

	if opts.ValidateSemantic {
		result.Diagnostics = append(result.Diagnostics, Validate(doc)...)
		result.Diagnostics = append(result.Diagnostics, ValidateKind(doc, kind)...)
	}

	if opts.MaxDiagnostics > 0 && len(result.Diagnostics) > opts.MaxDiagnostics {
		result.Diagnostics = result.Diagnostics[:opts.MaxDiagnostics]
	}

	if opts.Strict && result.HasErrors() {
		return result, fmt.Errorf("scl: strict mode: %d error(s)", countErrors(result.Diagnostics))
	}

	return result, nil
}

// ParseFileOpts reads and parses an SCL file, using the file
// extension to infer [DocumentKind] when not specified in opts.
func ParseFileOpts(path string, opts ParseOptions) (*Result, error) {
	data, err := os.ReadFile(filepath.Clean(path)) // the caller names the file to read
	if err != nil {
		return nil, fmt.Errorf("scl: read %q: %w", path, err)
	}

	if opts.Kind == "" {
		opts.Kind = kindFromExtension(path)
	}

	return ParseBytes(data, opts)
}

// --- Compatibility wrappers ---

// Parse reads an SCL XML document from r and returns the parsed model.
// For richer output, use [ParseBytes].
func Parse(r io.Reader) (*SCL, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("scl: read: %w", err)
	}
	result, err := ParseBytes(data, ParseOptions{})
	if err != nil {
		return nil, err
	}
	return result.Document, nil
}

// ParseFile reads and parses an SCL XML file from the given path.
// For richer output, use [ParseFileOpts].
func ParseFile(path string) (*SCL, error) {
	result, err := ParseFileOpts(path, ParseOptions{})
	if err != nil {
		return nil, err
	}
	return result.Document, nil
}

// ParseWithOptions is a compatibility wrapper around [ParseBytes].
//
// Deprecated: Use [ParseBytes] directly.
func ParseWithOptions(r io.Reader, opts ParseOptions) (*Result, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("scl: read: %w", err)
	}
	return ParseBytes(data, opts)
}

// ParseFileWithOptions is a compatibility wrapper around [ParseFileOpts].
//
// Deprecated: Use [ParseFileOpts] directly.
func ParseFileWithOptions(path string, opts ParseOptions) (*Result, error) {
	return ParseFileOpts(path, opts)
}

// --- Internal dispatch ---

func decodeAndConvert(data []byte, vi VersionInfo, defaultSpace string) (*SCL, []Diagnostic, error) {
	switch vi.Schema {
	case Version2007C5:
		var raw v2007c5.SCL
		if err := decodeSCL(data, defaultSpace, &raw); err != nil {
			return nil, nil, fmt.Errorf("scl: unmarshal v2007c5: %w", err)
		}
		return convertV2007C5(&raw)

	case Version2007B4:
		var raw v2007b4.SCL
		if err := decodeSCL(data, defaultSpace, &raw); err != nil {
			return nil, nil, fmt.Errorf("scl: unmarshal v2007b4: %w", err)
		}
		return convertV2007B4(&raw)

	case Version2007B:
		var raw v2007b.SCL
		if err := decodeSCL(data, defaultSpace, &raw); err != nil {
			return nil, nil, fmt.Errorf("scl: unmarshal v2007b: %w", err)
		}
		return convertV2007B(&raw)

	case Version17:
		var raw v17.SCL
		if err := decodeSCL(data, defaultSpace, &raw); err != nil {
			return nil, nil, fmt.Errorf("scl: unmarshal v17: %w", err)
		}
		return convertV17(&raw)

	default:
		fb, ok := fallbackSchema(vi)
		if !ok {
			return nil, nil, fmt.Errorf("scl: unsupported schema version (version=%q revision=%q release=%q)", vi.Version, vi.Revision, vi.Release)
		}
		alt := vi
		alt.Schema = fb
		doc, diags, err := decodeAndConvert(data, alt, defaultSpace)
		if err != nil {
			return nil, nil, err
		}
		warn := Diagnostic{
			Severity: DiagWarning,
			Code:     "schema-fallback",
			Path:     "SCL",
			Message: fmt.Sprintf("schema version=%q revision=%q release=%q is not a published SCL schema this library knows; parsed with the %s schema, elements it does not define are ignored",
				vi.Version, vi.Revision, vi.Release, fb),
		}
		return doc, append([]Diagnostic{warn}, diags...), nil
	}
}

// rootHasChild reports whether the root element of data has a direct
// child element with the given local name. It stops at the first match
// and ignores everything deeper than the root's children.
func rootHasChild(data []byte, name string) bool {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	depth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && t.Name.Local == name {
				return true
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				return false
			}
		}
	}
}

// fallbackSchema chooses the schema bindings for an SCL document in the
// IEC namespace whose version tuple is not one of the published schemas
// (IEC 61850-6 fixes version to 2007, with revision B release 4 or
// revision C release 5 for the schemas in specs/): the closest
// revision of version 2007, or the newest schema for any other version.
// A foreign namespace or a malformed release is not parsed.
func fallbackSchema(vi VersionInfo) (SchemaVersion, bool) {
	if vi.Namespace != iecNamespace || (vi.Release != "" && vi.ReleaseNum == 0) {
		return VersionUnknown, false
	}
	if vi.Version != "2007" {
		return Version2007C5, true
	}
	switch {
	case vi.Revision < "B":
		return Version2007B, true
	case vi.Revision == "B":
		return Version2007B4, true
	default:
		return Version2007C5, true
	}
}

// KindFromPath infers the [DocumentKind] from a file path's extension.
func KindFromPath(path string) DocumentKind {
	return kindFromExtension(path)
}

func kindFromExtension(path string) DocumentKind {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".scd":
		return KindSCD
	case ".cid":
		return KindCID
	case ".icd":
		return KindICD
	case ".iid":
		return KindIID
	case ".ssd":
		return KindSSD
	default:
		return KindUnknown
	}
}

func countErrors(diags []Diagnostic) int {
	n := 0
	for _, d := range diags {
		if d.Severity == DiagError {
			n++
		}
	}
	return n
}

// parseBool interprets an SCL boolean attribute. It normalises
// case and surrounding whitespace, accepting "true"/"false" (and
// variants such as "True", " TRUE "). An empty string is treated as
// false. Anything else returns an error.
func parseBool(s string) (bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "true":
		return true, nil
	case "", "false":
		return false, nil
	default:
		return false, fmt.Errorf("scl: invalid boolean value %q", s)
	}
}
