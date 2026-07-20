// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestParseRef_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  Ref
	}{
		{
			input: "simpleIOGenericIO/GGIO1.Ind1.stVal[ST]",
			want: Ref{
				LD:   "simpleIOGenericIO",
				LN:   "GGIO1",
				Path: []string{"Ind1", "stVal"},
				FC:   FCST,
			},
		},
		{
			input: "LD1/LLN0.NamPlt.vendor[DC]",
			want: Ref{
				LD:   "LD1",
				LN:   "LLN0",
				Path: []string{"NamPlt", "vendor"},
				FC:   FCDC,
			},
		},
		{
			input: "LD1/MMXU1.TotW.mag.f[MX]",
			want: Ref{
				LD:   "LD1",
				LN:   "MMXU1",
				Path: []string{"TotW", "mag", "f"},
				FC:   FCMX,
			},
		},
		{
			input: "LD1/LLN0",
			want:  Ref{LD: "LD1", LN: "LLN0"},
		},
		{
			input: "LD1/GGIO1.Ind1.stVal",
			want:  Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}},
		},
		{
			input: "LD1",
			want:  Ref{LD: "LD1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseRef(tc.input)
			if err != nil {
				t.Fatalf("ParseRef(%q) error: %v", tc.input, err)
			}
			if got.LD != tc.want.LD {
				t.Errorf("LD = %q, want %q", got.LD, tc.want.LD)
			}
			if got.LN != tc.want.LN {
				t.Errorf("LN = %q, want %q", got.LN, tc.want.LN)
			}
			if len(got.Path) != len(tc.want.Path) {
				t.Errorf("Path len = %d, want %d", len(got.Path), len(tc.want.Path))
			} else {
				for i := range got.Path {
					if got.Path[i] != tc.want.Path[i] {
						t.Errorf("Path[%d] = %q, want %q", i, got.Path[i], tc.want.Path[i])
					}
				}
			}
			if got.FC != tc.want.FC {
				t.Errorf("FC = %q, want %q", got.FC, tc.want.FC)
			}
		})
	}
}

func TestParseRef_Invalid(t *testing.T) {
	tests := []struct {
		input  string
		reason string
	}{
		{"", "empty"},
		{"LD1/", "empty content after"},
		{"LD1/LN.", "empty path"},
		{"LD1/LN..DO", "empty path component"},
		{"LD1/.DO", "empty logical node"},
		{"ref[XX]", "invalid functional constraint"},
		{"ref[S]", "must be 2 characters"},
		{"ref[ST", "malformed brackets"},
		{"ref[ST]extra", "malformed brackets"},
		{"LN.DO.DA", "missing logical device separator"},
		// Stray bracket cases (feedback #6a).
		{"LD1/LN]", "mismatched"},
		{"LD1/LN]ST[", "mismatched"},
		{"LD1/LN[ST][MX]", "multiple bracket pairs"},
		// FC on LD-only is not valid (feedback round 2, #4).
		{"LD1[ST]", "FC requires LN"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := ParseRef(tc.input)
			if err == nil {
				t.Fatalf("ParseRef(%q) expected error, got nil", tc.input)
			}
			if !errors.Is(err, ErrInvalidReference) {
				var refErr *ReferenceError
				if !errors.As(err, &refErr) {
					t.Fatalf("ParseRef(%q) error type = %T, want *ReferenceError", tc.input, err)
				}
			}
		})
	}
}

func TestRef_String_Roundtrip(t *testing.T) {
	tests := []string{
		"simpleIOGenericIO/GGIO1.Ind1.stVal[ST]",
		"LD1/LLN0",
		"LD1/MMXU1.TotW.mag.f[MX]",
		"LD1",
	}

	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			ref, err := ParseRef(s)
			if err != nil {
				t.Fatalf("ParseRef(%q) error: %v", s, err)
			}
			got := ref.String()
			if got != s {
				t.Errorf("String() = %q, want %q", got, s)
			}
		})
	}
}

func TestRef_ObjectReference(t *testing.T) {
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST}
	want := "LD1/GGIO1.Ind1.stVal"
	got := ref.ObjectReference()
	if got != want {
		t.Errorf("ObjectReference() = %q, want %q", got, want)
	}
}

func TestRef_WithFC(t *testing.T) {
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1"}}
	got := ref.WithFC(FCST)
	if got.FC != FCST {
		t.Errorf("WithFC(ST).FC = %q, want ST", got.FC)
	}
	if ref.FC != "" {
		t.Error("WithFC modified original")
	}
}

func TestRef_Parent(t *testing.T) {
	ref := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST}

	p1, ok := ref.Parent()
	if !ok {
		t.Fatal("Parent() returned false")
	}
	if p1.String() != "LD1/GGIO1.Ind1[ST]" {
		t.Errorf("Parent() = %q, want LD1/GGIO1.Ind1[ST]", p1.String())
	}
	if p1.FC != FCST {
		t.Error("Parent() should preserve FC")
	}

	p2, ok := p1.Parent()
	if !ok {
		t.Fatal("Parent() returned false")
	}
	if p2.String() != "LD1/GGIO1[ST]" {
		t.Errorf("Parent() = %q, want LD1/GGIO1[ST]", p2.String())
	}

	p3, ok := p2.Parent()
	if !ok {
		t.Fatal("Parent() returned false")
	}
	if p3.String() != "LD1" {
		t.Errorf("Parent() = %q, want LD1", p3.String())
	}
	if p3.FC != "" {
		t.Errorf("Parent() to LD-only should drop FC, got %q", p3.FC)
	}

	_, ok = p3.Parent()
	if ok {
		t.Error("Parent() of LD-only ref should return false")
	}
}

func TestRef_Child(t *testing.T) {
	ref := Ref{LD: "LD1", LN: "GGIO1"}
	child, err := ref.Child("Ind1")
	if err != nil {
		t.Fatalf("Child(Ind1) error: %v", err)
	}
	if child.String() != "LD1/GGIO1.Ind1" {
		t.Errorf("Child(Ind1) = %q", child.String())
	}
	grandchild, err := child.Child("stVal")
	if err != nil {
		t.Fatalf("Child(stVal) error: %v", err)
	}
	if grandchild.String() != "LD1/GGIO1.Ind1.stVal" {
		t.Errorf("Child(stVal) = %q", grandchild.String())
	}
	if len(ref.Path) != 0 {
		t.Error("Child modified original")
	}
}

func TestRef_Child_Invalid(t *testing.T) {
	ref := Ref{LD: "LD1", LN: "GGIO1"}

	_, err := ref.Child("")
	if err == nil {
		t.Fatal("Child(\"\") expected error")
	}

	invalids := []string{"A.B", "A$B", "A[B]", "A/B"}
	for _, name := range invalids {
		_, err = ref.Child(name)
		if err == nil {
			t.Errorf("Child(%q) expected error", name)
		}
	}

	ldOnly := Ref{LD: "LD1"}
	_, err = ldOnly.Child("Do")
	if err == nil {
		t.Fatal("Child on LD-only ref expected error")
	}
}

func TestRef_IsLD_IsLN_HasPath(t *testing.T) {
	ld := Ref{LD: "LD1"}
	if !ld.IsLD() {
		t.Error("IsLD")
	}
	if ld.IsLN() {
		t.Error("IsLN should be false for LD-only")
	}

	ln := Ref{LD: "LD1", LN: "LLN0"}
	if ln.IsLD() {
		t.Error("IsLD should be false for LN")
	}
	if !ln.IsLN() {
		t.Error("IsLN")
	}

	da := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1"}}
	if da.IsLD() || da.IsLN() {
		t.Error("should not be LD or LN")
	}
	if !da.HasPath() {
		t.Error("HasPath")
	}
}

func TestRef_IsObject(t *testing.T) {
	ld := Ref{LD: "LD1"}
	if ld.IsObject() {
		t.Error("LD-only should not be IsObject")
	}
	ln := Ref{LD: "LD1", LN: "LLN0"}
	if ln.IsObject() {
		t.Error("LN-only should not be IsObject")
	}
	obj := Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1"}}
	if !obj.IsObject() {
		t.Error("LN+path should be IsObject")
	}
}

func TestRef_Depth(t *testing.T) {
	tests := []struct {
		ref  Ref
		want int
	}{
		{Ref{}, 0},
		{Ref{LD: "LD1"}, 1},
		{Ref{LD: "LD1", LN: "LLN0"}, 2},
		{Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1"}}, 3},
		{Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}}, 4},
	}
	for _, tc := range tests {
		got := tc.ref.Depth()
		if got != tc.want {
			t.Errorf("Ref(%s).Depth() = %d, want %d", tc.ref.String(), got, tc.want)
		}
	}
}

func TestRef_Validate(t *testing.T) {
	valid := []Ref{
		{LD: "LD1"},
		{LD: "LD1", LN: "LLN0"},
		{LD: "LD1", LN: "LLN0", Path: []string{"Ind1"}, FC: FCST},
		{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCMX},
		{LD: "LD1", LN: "LLN0", FC: FCST},
	}
	for _, r := range valid {
		if err := r.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", r.String(), err)
		}
	}

	invalid := []struct {
		ref    Ref
		reason string
	}{
		{Ref{}, "empty LD"},
		{Ref{LD: "LD1", Path: []string{"x"}}, "path without LN"},
		{Ref{LD: "LD1", LN: "LN", Path: []string{"x", ""}}, "empty path component"},
		{Ref{LD: "LD1", LN: "LN", FC: FunctionalConstraint("XYZ")}, "FC wrong length"},
		{Ref{LD: "LD.1"}, "LD with separator"},
		{Ref{LD: "LD1", LN: "LN/0"}, "LN with separator"},
		{Ref{LD: "LD1", LN: "LN", Path: []string{"a$b"}}, "path with separator"},
		{Ref{LD: "LD1", FC: FCST}, "FC on LD-only"},
	}
	for _, tc := range invalid {
		if err := tc.ref.Validate(); err == nil {
			t.Errorf("Validate(%q) expected error (%s), got nil", tc.ref.String(), tc.reason)
		}
	}
}

func TestRef_ToMMS(t *testing.T) {
	tests := []struct {
		ref      Ref
		domain   mms.DomainID
		itemID   mms.ItemID
		hasError bool
	}{
		{
			ref:    Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
			domain: "LD1",
			itemID: "GGIO1$ST$Ind1$stVal",
		},
		{
			ref:    Ref{LD: "LD1", LN: "LLN0", Path: []string{"NamPlt", "vendor"}, FC: FCDC},
			domain: "LD1",
			itemID: "LLN0$DC$NamPlt$vendor",
		},
		{
			ref:    Ref{LD: "LD1", LN: "LLN0"},
			domain: "LD1",
			itemID: "LLN0",
		},
		{
			ref:    Ref{LD: "LD1"},
			domain: "LD1",
			itemID: "",
		},
		{
			ref:      Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1"}},
			hasError: true,
		},
		{
			ref:      Ref{},
			hasError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.ref.String(), func(t *testing.T) {
			domain, itemID, err := tc.ref.ToMMS()
			if tc.hasError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if domain != tc.domain {
				t.Errorf("domain = %q, want %q", domain, tc.domain)
			}
			if itemID != tc.itemID {
				t.Errorf("itemID = %q, want %q", itemID, tc.itemID)
			}
		})
	}
}

func TestRefFromMMS(t *testing.T) {
	tests := []struct {
		domain mms.DomainID
		itemID mms.ItemID
		want   Ref
	}{
		{
			domain: "LD1",
			itemID: "GGIO1$ST$Ind1$stVal",
			want:   Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
		},
		{
			domain: "LD1",
			itemID: "LLN0$DC$NamPlt$vendor",
			want:   Ref{LD: "LD1", LN: "LLN0", Path: []string{"NamPlt", "vendor"}, FC: FCDC},
		},
		{
			domain: "LD1",
			itemID: "LLN0",
			want:   Ref{LD: "LD1", LN: "LLN0"},
		},
		{
			domain: "LD1",
			itemID: "",
			want:   Ref{LD: "LD1"},
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.domain)+"/"+string(tc.itemID), func(t *testing.T) {
			got, err := RefFromMMS(tc.domain, tc.itemID)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got.LD != tc.want.LD || got.LN != tc.want.LN || got.FC != tc.want.FC {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			if len(got.Path) != len(tc.want.Path) {
				t.Errorf("path len = %d, want %d", len(got.Path), len(tc.want.Path))
			}
		})
	}
}

func TestRefFromMMS_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		domain mms.DomainID
		itemID mms.ItemID
	}{
		{"empty_domain", "", "LLN0$ST$x"},
		{"empty_LN", "LD1", "$ST$x"},
		{"empty_FC", "LD1", "LLN0$$foo"},
		{"empty_path_segment", "LD1", "LLN0$ST$$stVal"},
		{"trailing_dollar", "LD1", "LLN0$ST$"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RefFromMMS(tc.domain, tc.itemID)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRef_ToMMS_FromMMS_Roundtrip(t *testing.T) {
	refs := []Ref{
		{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST},
		{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"NamPlt", "vendor"}, FC: FCDC},
		{LD: "LD1", LN: "MMXU1", Path: []string{"TotW", "mag", "f"}, FC: FCMX},
	}

	for _, ref := range refs {
		t.Run(ref.String(), func(t *testing.T) {
			domain, itemID, err := ref.ToMMS()
			if err != nil {
				t.Fatalf("ToMMS() error: %v", err)
			}
			got, err := RefFromMMS(domain, itemID)
			if err != nil {
				t.Fatalf("RefFromMMS() error: %v", err)
			}
			if got.LD != ref.LD || got.LN != ref.LN || got.FC != ref.FC {
				t.Errorf("roundtrip mismatch: got %+v, want %+v", got, ref)
			}
			if len(got.Path) != len(ref.Path) {
				t.Fatalf("path len mismatch: got %d, want %d", len(got.Path), len(ref.Path))
			}
			for i := range got.Path {
				if got.Path[i] != ref.Path[i] {
					t.Errorf("path[%d] = %q, want %q", i, got.Path[i], ref.Path[i])
				}
			}
		})
	}
}
