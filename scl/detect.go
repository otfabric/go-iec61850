// SPDX-License-Identifier: MIT

package scl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
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
	dec := xml.NewDecoder(bytes.NewReader(data))

	for {
		tok, err := dec.Token()
		if err != nil {
			return VersionInfo{}, fmt.Errorf("scl: no SCL root element found")
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

// DetectFile is a convenience wrapper that reads a file and detects
// its SCL schema version.
func DetectFile(path string) (VersionInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return VersionInfo{}, fmt.Errorf("scl: read %q: %w", path, err)
	}
	return DetectVersion(data)
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
