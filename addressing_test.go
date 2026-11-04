// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-mms"
)

func TestParseSelector(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []byte
		err  bool
	}{
		{in: "0001", want: []byte{0, 1}},
		{in: "00000001", want: []byte{0, 0, 0, 1}},
		{in: "01", want: []byte{1}},
		{in: "0x0001", want: []byte{0, 1}},
		{in: " 00ff ", want: []byte{0, 0xff}},
		{in: "", want: []byte{}},
		{in: "1", err: true},
		{in: "zz", err: true},
	} {
		got, err := ParseSelector(tc.in)
		if tc.err {
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("ParseSelector(%q) err = %v, want ErrInvalidArgument", tc.in, err)
			}
			continue
		}
		if err != nil || got == nil || !bytes.Equal(got, tc.want) {
			t.Errorf("ParseSelector(%q) = %#v, %v; want %#v", tc.in, got, err, tc.want)
		}
	}
}

func TestParseAPTitle(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want mms.APTitle
		err  bool
	}{
		{in: "1,3,9999,23", want: mms.APTitle{1, 3, 9999, 23}},
		{in: "1.1.1.999.1", want: mms.APTitle{1, 1, 1, 999, 1}},
		{in: "\"1 3 9999 23\"", want: mms.APTitle{1, 3, 9999, 23}},
		{in: "", want: mms.APTitle{}},
		{in: "1", err: true},
		{in: "1,x", err: true},
		{in: "1,-2", err: true},
	} {
		got, err := ParseAPTitle(tc.in)
		if tc.err {
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("ParseAPTitle(%q) err = %v, want ErrInvalidArgument", tc.in, err)
			}
			continue
		}
		if err != nil || got == nil || !got.Equal(tc.want) {
			t.Errorf("ParseAPTitle(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestApplySCLAddress(t *testing.T) {
	// The ConnectedAP example of IEC 61850-8-1 Annex (SCL Communication).
	ap := scl.ConnectedAP{IEDName: "D1Q1SB4", APName: "S1", Address: []scl.P{
		{Type: "IP", Value: "10.0.0.11"},
		{Type: PTypeOSIAPTitle, Value: "1,3,9999,23"},
		{Type: PTypeOSIAEQualifier, Value: "23"},
		{Type: PTypeOSITSEL, Value: "00000001"},
		{Type: PTypeOSIPSEL, Value: "01"},
		{Type: PTypeOSISSEL, Value: "01"},
	}}
	var opts mms.DialOptions
	if err := ApplySCLAddress(&opts, ap); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opts.Transport.RemoteTSelector, []byte{0, 0, 0, 1}) ||
		!bytes.Equal(opts.ISO.RemoteSSelector, []byte{1}) || !bytes.Equal(opts.ISO.RemotePSelector, []byte{1}) {
		t.Errorf("selectors T=%x S=%x P=%x", opts.Transport.RemoteTSelector, opts.ISO.RemoteSSelector, opts.ISO.RemotePSelector)
	}
	if !opts.ISO.RemoteAPTitle.Equal(mms.APTitle{1, 3, 9999, 23}) || opts.ISO.RemoteAEQualifier != 23 {
		t.Errorf("AP-title %v / %d", opts.ISO.RemoteAPTitle, opts.ISO.RemoteAEQualifier)
	}
	if opts.Transport.LocalTSelector != nil || opts.ISO.LocalAPTitle != nil {
		t.Error("local addressing must stay at the defaults")
	}

	// Absent P-types leave the defaults (nil) in place.
	var empty mms.DialOptions
	if err := ApplySCLAddress(&empty, scl.ConnectedAP{Address: []scl.P{{Type: "IP", Value: "10.0.0.1"}}}); err != nil {
		t.Fatal(err)
	}
	if empty.Transport.RemoteTSelector != nil || empty.ISO.RemoteAPTitle != nil {
		t.Error("absent P-types changed the options")
	}

	for _, p := range []scl.P{
		{Type: PTypeOSITSEL, Value: "1"},
		{Type: PTypeOSISSEL, Value: "q1"},
		{Type: PTypeOSIPSEL, Value: "123"},
		{Type: PTypeOSIAPTitle, Value: "1,a"},
		{Type: PTypeOSIAEQualifier, Value: "x"},
	} {
		err := ApplySCLAddress(&mms.DialOptions{}, scl.ConnectedAP{IEDName: "I", APName: "S1", Address: []scl.P{p}})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s=%q: err = %v, want ErrInvalidArgument", p.Type, p.Value, err)
		}
	}
	if err := ApplySCLAddress(nil, ap); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil opts: err = %v", err)
	}
}
