// SPDX-License-Identifier: MIT

package mapping

import (
	"reflect"
	"testing"
)

var sampleItems = []string{
	"LLN0$ST$Mod$stVal",
	"LLN0$ST$Mod$q",
	"LLN0$ST$Beh$stVal",
	"LLN0$ST$Beh$q",
	"LLN0$DC$NamPlt$vendor",
	"LLN0$DC$NamPlt$swRev",
	"GGIO1$ST$Ind1$stVal",
	"GGIO1$ST$Ind1$q",
	"GGIO1$ST$Ind1$t",
	"GGIO1$ST$Ind2$stVal",
	"GGIO1$CO$SPCSO1$Oper$ctlVal",
	"GGIO1$CO$SPCSO1$Oper$origin$orCat",
	"MMXU1$MX$TotW$mag$f",
	"MMXU1$MX$TotW$mag$i",
	"MMXU1$MX$TotW$q",
	"MMXU1$MX$TotW$t",
	"MMXU1$MX$TotVAr$mag$f",
}

func TestExtractLogicalNodes(t *testing.T) {
	got := ExtractLogicalNodes(sampleItems)
	want := []string{"GGIO1", "LLN0", "MMXU1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractLogicalNodes = %v, want %v", got, want)
	}
}

func TestExtractLogicalNodes_Empty(t *testing.T) {
	got := ExtractLogicalNodes(nil)
	if len(got) != 0 {
		t.Errorf("ExtractLogicalNodes(nil) = %v, want empty", got)
	}
}

func TestExtractLogicalNodes_NoDollars(t *testing.T) {
	got := ExtractLogicalNodes([]string{"LLN0", "GGIO1"})
	if len(got) != 0 {
		t.Errorf("ExtractLogicalNodes = %v, want empty (items without $ are not valid IEC names)", got)
	}
}

func TestExtractLogicalNodes_MixedItems(t *testing.T) {
	items := []string{"LLN0$ST$Mod$stVal", "plainVar", "GGIO1$CO$SPCSO1$Oper$ctlVal"}
	got := ExtractLogicalNodes(items)
	want := []string{"GGIO1", "LLN0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractLogicalNodes = %v, want %v", got, want)
	}
}

func TestParseItemID(t *testing.T) {
	tests := []struct {
		input string
		want  ParsedVariable
		ok    bool
	}{
		{
			input: "LLN0$ST$Mod$stVal",
			want:  ParsedVariable{LN: "LLN0", FC: "ST", Path: []string{"Mod", "stVal"}},
			ok:    true,
		},
		{
			input: "GGIO1$CO$SPCSO1$Oper$ctlVal",
			want:  ParsedVariable{LN: "GGIO1", FC: "CO", Path: []string{"SPCSO1", "Oper", "ctlVal"}},
			ok:    true,
		},
		{
			input: "LLN0$ST",
			want:  ParsedVariable{LN: "LLN0", FC: "ST"},
			ok:    true,
		},
		{input: "LLN0", ok: false},
		{input: "", ok: false},
		{input: "$ST$x", ok: false},
		{input: "LLN0$$x", ok: false},
		{input: "LLN0$ST$Do$$da", ok: false},
		{input: "LLN0$ST$", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := ParseItemID(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got.LN != tc.want.LN || got.FC != tc.want.FC {
				t.Errorf("LN=%q FC=%q, want LN=%q FC=%q", got.LN, got.FC, tc.want.LN, tc.want.FC)
			}
			if !reflect.DeepEqual(got.Path, tc.want.Path) {
				t.Errorf("Path = %v, want %v", got.Path, tc.want.Path)
			}
		})
	}
}

func TestExtractDataObjects(t *testing.T) {
	tests := []struct {
		ln   string
		want []string
	}{
		{"LLN0", []string{"Beh", "Mod", "NamPlt"}},
		{"GGIO1", []string{"Ind1", "Ind2", "SPCSO1"}},
		{"MMXU1", []string{"TotVAr", "TotW"}},
		{"NONEXIST", nil},
	}

	for _, tc := range tests {
		t.Run(tc.ln, func(t *testing.T) {
			got := ExtractDataObjects(sampleItems, tc.ln)
			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("got %v, want empty", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractChildren(t *testing.T) {
	tests := []struct {
		name     string
		ln       string
		basePath []string
		want     []string
	}{
		{
			name:     "LLN0_Mod_children",
			ln:       "LLN0",
			basePath: []string{"Mod"},
			want:     []string{"q", "stVal"},
		},
		{
			name:     "GGIO1_Ind1_children",
			ln:       "GGIO1",
			basePath: []string{"Ind1"},
			want:     []string{"q", "stVal", "t"},
		},
		{
			name:     "MMXU1_TotW_children",
			ln:       "MMXU1",
			basePath: []string{"TotW"},
			want:     []string{"mag", "q", "t"},
		},
		{
			name:     "MMXU1_TotW_mag_children",
			ln:       "MMXU1",
			basePath: []string{"TotW", "mag"},
			want:     []string{"f", "i"},
		},
		{
			name:     "GGIO1_SPCSO1_children",
			ln:       "GGIO1",
			basePath: []string{"SPCSO1"},
			want:     []string{"Oper"},
		},
		{
			name:     "GGIO1_SPCSO1_Oper_children",
			ln:       "GGIO1",
			basePath: []string{"SPCSO1", "Oper"},
			want:     []string{"ctlVal", "origin"},
		},
		{
			name:     "leaf_no_children",
			ln:       "LLN0",
			basePath: []string{"Mod", "stVal"},
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractChildren(sampleItems, tc.ln, tc.basePath)
			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("got %v, want empty", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGroupByFC(t *testing.T) {
	groups := GroupByFC(sampleItems, "LLN0")
	if len(groups) != 2 {
		t.Fatalf("expected 2 FC groups, got %d", len(groups))
	}
	if len(groups["ST"]) != 4 {
		t.Errorf("ST group has %d items, want 4", len(groups["ST"]))
	}
	if len(groups["DC"]) != 2 {
		t.Errorf("DC group has %d items, want 2", len(groups["DC"]))
	}
}

func TestExtractFCsForLN(t *testing.T) {
	tests := []struct {
		ln   string
		want []string
	}{
		{"LLN0", []string{"DC", "ST"}},
		{"GGIO1", []string{"CO", "ST"}},
		{"MMXU1", []string{"MX"}},
		{"NONEXIST", nil},
	}

	for _, tc := range tests {
		t.Run(tc.ln, func(t *testing.T) {
			got := ExtractFCsForLN(sampleItems, tc.ln)
			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("got %v, want empty", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractFCsForPath(t *testing.T) {
	tests := []struct {
		name     string
		ln       string
		basePath []string
		want     []string
	}{
		{
			name:     "LLN0_Mod_single_FC",
			ln:       "LLN0",
			basePath: []string{"Mod"},
			want:     []string{"ST"},
		},
		{
			name:     "LLN0_all_multi_FC",
			ln:       "LLN0",
			basePath: nil,
			want:     []string{"DC", "ST"},
		},
		{
			name:     "GGIO1_SPCSO1_multi_FC",
			ln:       "GGIO1",
			basePath: []string{"SPCSO1"},
			want:     []string{"CO"},
		},
		{
			name:     "MMXU1_TotW_mag_single_FC",
			ln:       "MMXU1",
			basePath: []string{"TotW", "mag"},
			want:     []string{"MX"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractFCsForPath(sampleItems, tc.ln, tc.basePath)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
