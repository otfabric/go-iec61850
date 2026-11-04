// SPDX-License-Identifier: MIT

package iec61850

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-mms"
)

// SCL P-types of a ConnectedAP address that name the ISO addressing of an
// MMS server (IEC 61850-8-1 Table 132).
const (
	PTypeOSITSEL        = "OSI-TSEL"
	PTypeOSISSEL        = "OSI-SSEL"
	PTypeOSIPSEL        = "OSI-PSEL"
	PTypeOSIAPTitle     = "OSI-AP-Title"
	PTypeOSIAEQualifier = "OSI-AE-Qualifier"
)

// ParseSelector parses an OSI transport, session or presentation selector
// written in hex, as in SCL and on IED configuration screens: "0001",
// "00000001". An optional "0x" prefix is accepted. An empty string returns
// a non-nil empty selector, which the dial options read as "omit the
// parameter". Errors match [ErrInvalidArgument].
func ParseSelector(s string) ([]byte, error) {
	h := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	if h == "" {
		return []byte{}, nil
	}
	if len(h)%2 != 0 {
		return nil, fmt.Errorf("%w: selector %q: odd number of hex digits (write 01, not 1)", ErrInvalidArgument, s)
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("%w: selector %q: not hex", ErrInvalidArgument, s)
	}
	return b, nil
}

// ParseAPTitle parses an ACSE AP-title written as an object identifier
// with commas (the SCL OSI-AP-Title form, IEC 61850-8-1 Table 132:
// "1,3,9999,23"), dots or spaces between the arcs. An empty string returns
// a non-nil empty AP-title, which the dial options read as "omit".
// Errors match [ErrInvalidArgument].
func ParseAPTitle(s string) (mms.APTitle, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '.' || r == ' ' || r == '"' })
	if len(fields) == 0 {
		return mms.APTitle{}, nil
	}
	if len(fields) < 2 {
		return nil, fmt.Errorf("%w: AP-title %q: an object identifier has at least two arcs", ErrInvalidArgument, s)
	}
	title := make(mms.APTitle, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%w: AP-title %q: arc %q is not a non-negative integer", ErrInvalidArgument, s, f)
		}
		title[i] = n
	}
	return title, nil
}

// ApplySCLAddress sets the remote (called) ISO addressing of opts from the
// Address of an SCL ConnectedAP, the values an IED is configured with:
// OSI-TSEL, OSI-SSEL, OSI-PSEL, OSI-AP-Title and OSI-AE-Qualifier. A P-type
// the address does not carry leaves opts unchanged, so the go-mms default
// applies. The local (calling) addressing is not taken from SCL. Errors
// match [ErrInvalidArgument] and name the P-type.
func ApplySCLAddress(opts *mms.DialOptions, ap scl.ConnectedAP) error {
	if opts == nil {
		return fmt.Errorf("%w: nil DialOptions", ErrInvalidArgument)
	}
	for _, p := range ap.Address {
		var err error
		switch p.Type {
		case PTypeOSITSEL:
			opts.Transport.RemoteTSelector, err = ParseSelector(p.Value)
		case PTypeOSISSEL:
			opts.ISO.RemoteSSelector, err = ParseSelector(p.Value)
		case PTypeOSIPSEL:
			opts.ISO.RemotePSelector, err = ParseSelector(p.Value)
		case PTypeOSIAPTitle:
			opts.ISO.RemoteAPTitle, err = ParseAPTitle(p.Value)
		case PTypeOSIAEQualifier:
			opts.ISO.RemoteAEQualifier, err = strconv.Atoi(strings.TrimSpace(p.Value))
			if err != nil {
				err = fmt.Errorf("%w: %q is not an integer", ErrInvalidArgument, p.Value)
			}
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("ConnectedAP %s/%s %s: %w", ap.IEDName, ap.APName, p.Type, err)
		}
	}
	return nil
}
