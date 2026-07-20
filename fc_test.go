// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"testing"
)

func TestParseFC_Valid(t *testing.T) {
	for _, tc := range AllFunctionalConstraints() {
		t.Run(string(tc), func(t *testing.T) {
			fc, err := ParseFC(string(tc))
			if err != nil {
				t.Fatalf("ParseFC(%q) error: %v", tc, err)
			}
			if fc != tc {
				t.Fatalf("ParseFC(%q) = %q, want %q", tc, fc, tc)
			}
		})
	}
}

func TestParseFC_Invalid(t *testing.T) {
	cases := []string{
		"", "X", "st", "XX", "A", "ZZ", "STT", "S", "123",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			_, err := ParseFC(s)
			if err == nil {
				t.Fatalf("ParseFC(%q) expected error, got nil", s)
			}
			if !errors.Is(err, ErrInvalidFunctionalConstraint) {
				t.Fatalf("ParseFC(%q) error = %v, want ErrInvalidFunctionalConstraint", s, err)
			}
		})
	}
}

func TestFC_IsValid(t *testing.T) {
	if !FCST.IsValid() {
		t.Fatal("FCST.IsValid() = false")
	}
	if FunctionalConstraint("XX").IsValid() {
		t.Fatal("FC(XX).IsValid() = true")
	}
}

func TestFC_Description(t *testing.T) {
	if d := FCST.Description(); d != "Status" {
		t.Fatalf("FCST.Description() = %q, want %q", d, "Status")
	}
	if d := FCMX.Description(); d != "Measurands" {
		t.Fatalf("FCMX.Description() = %q, want %q", d, "Measurands")
	}
	if d := FunctionalConstraint("XX").Description(); d != "Unknown" {
		t.Fatalf("FC(XX).Description() = %q, want %q", d, "Unknown")
	}
}

func TestFC_String(t *testing.T) {
	if s := FCST.String(); s != "ST" {
		t.Fatalf("FCST.String() = %q, want %q", s, "ST")
	}
}

func TestAllFunctionalConstraints(t *testing.T) {
	all := AllFunctionalConstraints()
	if len(all) != 19 {
		t.Fatalf("AllFunctionalConstraints() has %d entries, want 19", len(all))
	}
	seen := make(map[FunctionalConstraint]bool)
	for _, fc := range all {
		if seen[fc] {
			t.Fatalf("duplicate FC: %s", fc)
		}
		seen[fc] = true
		if !fc.IsValid() {
			t.Fatalf("AllFunctionalConstraints contains invalid FC: %s", fc)
		}
	}
}
