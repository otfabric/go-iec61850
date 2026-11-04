// SPDX-License-Identifier: MIT

package servermodel

import (
	"testing"
)

// An Enum Val in SCL is the EnumVal literal (IEC 61850-6). A literal can
// be a number: ABB's baud-rate EnumType has ord 6 = "9600". The literal
// must win over reading the digits as an ordinal.
func TestParseInitialValue_EnumNumericLiteral(t *testing.T) {
	names := map[string]int{"300": 1, "9600": 6, "115200": 10}
	for _, tc := range []struct {
		val  string
		want int64
	}{
		{"9600", 6},    // literal, not ordinal 9600
		{"115200", 10}, // literal
		{"6", 6},       // no such literal: the ordinal
	} {
		v, err := parseInitialValue("Enum", tc.val, names)
		if err != nil {
			t.Fatalf("%q: %v", tc.val, err)
		}
		if got, _ := v.Int64(); got != tc.want {
			t.Errorf("%q = %d, want %d", tc.val, got, tc.want)
		}
	}
	if _, err := parseInitialValue("Enum", "fast", names); err == nil {
		t.Error("unknown non-numeric literal accepted")
	}
}

// Dbpos literals (IEC 61850-7-3) encode as the two bits DpCtlVal writes
// (IEC 61850-8-1): intermediate 00, off 01, on 10, bad 11.
func TestParseInitialValue_DbposLiterals(t *testing.T) {
	for val, want := range map[string]byte{
		"intermediate-state": 0x00, "off": 0x40, "on": 0x80, "bad-state": 0xc0,
	} {
		v, err := parseInitialValue("Dbpos", val, nil)
		if err != nil {
			t.Fatalf("%q: %v", val, err)
		}
		b, ok := v.BitString()
		if n, _ := v.BitStringLength(); !ok || n != 2 || len(b) != 1 || b[0] != want {
			t.Errorf("%q = %x (%d bits), want %02x", val, b, n, want)
		}
	}
	if _, err := parseInitialValue("Dbpos", "open", nil); err == nil {
		t.Error("unknown Dbpos literal accepted")
	}
	if _, err := parseInitialValue("Check", "on", nil); err == nil {
		t.Error("Check accepted a Dbpos literal")
	}
}

// ObjRef is VISIBLE STRING129 (IEC 61850-8-1).
func TestBTypeObjRef(t *testing.T) {
	ts, ok := btypeToMMS["ObjRef"]
	if !ok || ts.Size != 129 {
		t.Fatalf("ObjRef = %+v, %v", ts, ok)
	}
	v, err := parseInitialValue("ObjRef", "IED1LD0/LLN0.Mod", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := v.VisibleString(); s != "IED1LD0/LLN0.Mod" {
		t.Errorf("value = %q", s)
	}
}
