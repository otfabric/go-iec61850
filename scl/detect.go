// SPDX-License-Identifier: MIT

package scl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SchemaVersion identifies a specific SCL schema version.
type SchemaVersion string

const (
	VersionUnknown SchemaVersion = ""
	Version17      SchemaVersion = "1.7"
	Version2007B   SchemaVersion = "2007B"
	Version2007B4  SchemaVersion = "2007B4"
	Version2007C5  SchemaVersion = "2007C5"
)

// Confidence indicates how certain the version detection is.
type Confidence string

const (
	ConfidenceHigh Confidence = "high"
	ConfidenceLow  Confidence = "low"
)

// VersionInfo holds the detected schema version information
// extracted from the root SCL element.
type VersionInfo struct {
	Schema     SchemaVersion
	Namespace  string
	Version    string // raw "version" attribute (e.g. "2007")
	Revision   string // raw "revision" attribute (e.g. "B")
	Release    string // raw "release" attribute (e.g. "4")
	ReleaseNum int    // parsed numeric release; -1 when absent, 0 when malformed

	Confidence       Confidence
	Reasons          []string
	VendorNamespaces []string
}

const iecNamespace = "http://www.iec.ch/61850/2003/SCL"

// DetectVersion sniffs the SCL schema version from raw XML data.
//
// It streams tokens until the first StartElement, verifies it is an
// SCL root, reads only the element's attributes (without calling
// DecodeElement), and stops immediately. The full document is never
// parsed.
func DetectVersion(data []byte) (VersionInfo, error) {
	return detectVersion(newDecoder(data))
}

func detectVersion(dec *xml.Decoder) (VersionInfo, error) {
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return VersionInfo{}, fmt.Errorf("scl: no SCL root element found")
		}
		if err != nil {
			return VersionInfo{}, fmt.Errorf("scl: no SCL root element found: %w", err)
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if se.Name.Local != "SCL" {
			return VersionInfo{}, fmt.Errorf("scl: root element is <%s>, not <SCL>", se.Name.Local)
		}

		vi := extractVersionInfo(se)
		return vi, nil
	}
}

// newDecoder returns an XML decoder for SCL data. Besides UTF-8 itself it
// accepts the declarations some tools write for UTF-8 ("utf8", "UTF8")
// and US-ASCII, a subset of it; any other encoding is refused with its
// name.
func newDecoder(data []byte) *xml.Decoder {
	return newDecoderNS(data, "")
}

// newDecoderNS is newDecoder with a default namespace for elements that
// declare none (xml.Decoder.DefaultSpace); "" keeps them unqualified.
func newDecoderNS(data []byte, defaultSpace string) *xml.Decoder {
	return newReaderDecoder(bytes.NewReader(data), defaultSpace)
}

// newReaderDecoder is newDecoderNS for a document read from r.
func newReaderDecoder(r io.Reader, defaultSpace string) *xml.Decoder {
	dec := xml.NewDecoder(r)
	dec.DefaultSpace = defaultSpace
	dec.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(strings.ReplaceAll(label, "-", "")) {
		case "utf8", "usascii", "ascii":
			return input, nil
		}
		return nil, fmt.Errorf("unsupported encoding %q", label)
	}
	return dec
}

// DetectFile detects the SCL schema version of a file, as
// [DetectVersion] does for data. It reads the file up to the root
// element only, and at most [DefaultMaxBytes] of it.
func DetectFile(path string) (VersionInfo, error) {
	f, err := os.Open(filepath.Clean(path)) // the caller names the file to read
	if err != nil {
		return VersionInfo{}, fmt.Errorf("scl: read %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	r := &readErr{r: io.LimitReader(f, DefaultMaxBytes)}
	vi, err := detectVersion(newReaderDecoder(r, ""))
	if err != nil && r.err != nil {
		return VersionInfo{}, fmt.Errorf("scl: read %q: %w", path, r.err)
	}
	return vi, err
}

// readErr passes on the reads of r and keeps the first error other than
// io.EOF, so that a read error is reported as one.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

func extractVersionInfo(se xml.StartElement) VersionInfo {
	vi := VersionInfo{
		Namespace:  se.Name.Space,
		ReleaseNum: -1,
	}

	var vendors []string

	for _, a := range se.Attr {
		switch a.Name.Local {
		case "version":
			vi.Version = a.Value
		case "revision":
			vi.Revision = a.Value
		case "release":
			vi.Release = a.Value
		}

		if isVendorNamespaceAttr(a) {
			vendors = append(vendors, a.Value)
		}
	}

	vi.VendorNamespaces = vendors

	if vi.Release != "" {
		n, err := strconv.Atoi(vi.Release)
		if err != nil {
			vi.ReleaseNum = 0
		} else {
			vi.ReleaseNum = n
		}
	}

	vi.Schema, vi.Confidence, vi.Reasons = classifyVersion(vi)
	return vi
}

func isVendorNamespaceAttr(a xml.Attr) bool {
	if a.Name.Space != "xmlns" {
		return false
	}
	if a.Value == iecNamespace {
		return false
	}
	if strings.HasPrefix(a.Value, "http://www.w3.org/") {
		return false
	}
	return true
}

func classifyVersion(vi VersionInfo) (SchemaVersion, Confidence, []string) {
	if vi.Namespace != iecNamespace && vi.Namespace != "" {
		return VersionUnknown, ConfidenceLow, []string{
			fmt.Sprintf("unexpected namespace %q", vi.Namespace),
		}
	}

	switch {
	case vi.Version == "" && vi.Revision == "" && vi.Release == "":
		if vi.Namespace == iecNamespace {
			return Version17, ConfidenceHigh, nil
		}
		return VersionUnknown, ConfidenceLow, []string{"no version attributes and no IEC namespace"}

	case vi.Version == "2007" && vi.Revision == "B" && vi.Release == "":
		return Version2007B, ConfidenceHigh, nil

	case vi.Version == "2007" && vi.Revision == "B" && vi.ReleaseNum == 4:
		return Version2007B4, ConfidenceHigh, nil

	case vi.Version == "2007" && vi.Revision == "C" && vi.ReleaseNum == 5:
		return Version2007C5, ConfidenceHigh, nil

	case vi.Release != "" && vi.ReleaseNum == 0:
		return VersionUnknown, ConfidenceLow, []string{
			fmt.Sprintf("malformed release %q (not a number)", vi.Release),
		}

	default:
		return VersionUnknown, ConfidenceLow, []string{
			fmt.Sprintf("unsupported tuple version=%q revision=%q release=%q", vi.Version, vi.Revision, vi.Release),
		}
	}
}

// xsiNamespace is the XML Schema instance namespace (xsi:type,
// xsi:schemaLocation).
const xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"

// decodeSCL decodes data into v with the attributes of the XML Schema
// instance namespace left out. encoding/xml binds an attribute to a field
// tagged "type,attr" whatever its namespace, so in <P type="IP"
// xsi:type="tP_IP">, as some engineering tools write it, the later
// xsi:type would replace the address type. No SCL binding reads an xsi
// attribute.
//
// Elements nested deeper than maxDepth, skipped ones included, end the
// decode with an error that wraps [ErrLimitExceeded].
func decodeSCL(data []byte, defaultSpace string, maxDepth int, v any) error {
	return xml.NewTokenDecoder(&dropXSI{d: newDecoderNS(data, defaultSpace), maxDepth: maxDepth}).Decode(v)
}

// dropXSI passes on the tokens of d without attributes in the XML Schema
// instance namespace, and counts the element depth: every token of the
// document passes here, also those of elements the decoder skips.
type dropXSI struct {
	d        *xml.Decoder
	depth    int
	maxDepth int
}

func (f *dropXSI) Token() (xml.Token, error) {
	t, err := f.d.Token()
	switch t.(type) {
	case xml.StartElement:
		f.depth++
		if f.depth > f.maxDepth {
			return nil, fmt.Errorf("%w: elements nested more than %d deep (ParseOptions.MaxDepth)", ErrLimitExceeded, f.maxDepth)
		}
	case xml.EndElement:
		f.depth--
	}
	if se, ok := t.(xml.StartElement); ok {
		kept := make([]xml.Attr, 0, len(se.Attr))
		for _, a := range se.Attr {
			if a.Name.Space != xsiNamespace {
				kept = append(kept, a)
			}
		}
		se.Attr = kept
		t = se
	}
	return t, err
}
