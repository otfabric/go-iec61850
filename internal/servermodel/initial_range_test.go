// SPDX-License-Identifier: MIT

package servermodel

import (
	"math"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-mms"
)

// TestParseMillisBoundaries: MinTime and MaxTime are numbers from 0 to
// 4294967295 ms; a value that is not such a number is an error.
func TestParseMillisBoundaries(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint32
		ok   bool
	}{
		{"0", 0, true},
		{"4294967295", math.MaxUint32, true},
		{"4294967296", 0, false},
		{"-1", 0, false},
		{"NaN", 0, false},
		{"+Inf", 0, false},
		{"-Inf", 0, false},
	} {
		got, err := parseMillis(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("parseMillis(%q) = %d, %v; want %d, ok %v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

// TestInitialValueOutOfRangeBoundaries: per basic type the minimum and
// the maximum are in range, one beyond is not; values of other types and
// values that are not numbers are left to parseInitialValue.
func TestInitialValueOutOfRangeBoundaries(t *testing.T) {
	for btype, tc := range map[string]struct{ in, out []string }{
		"INT8":    {[]string{"-128", "127"}, []string{"-129", "128", "300"}},
		"INT16":   {[]string{"-32768", "32767"}, []string{"-32769", "32768", "-99999"}},
		"INT24":   {[]string{"-8388608", "8388607"}, []string{"-8388609", "8388608"}},
		"INT32":   {[]string{"-2147483648", "2147483647"}, []string{"-2147483649", "2147483648"}},
		"INT64":   {[]string{"-9223372036854775808", "9223372036854775807"}, []string{"-9223372036854775809", "9223372036854775808"}},
		"INT8U":   {[]string{"0", "255"}, []string{"256"}},
		"INT16U":  {[]string{"0", "65535"}, []string{"65536", "70000"}},
		"INT24U":  {[]string{"0", "16777215"}, []string{"16777216"}},
		"INT32U":  {[]string{"0", "4294967295"}, []string{"4294967296"}},
		"FLOAT32": {[]string{"3.4028234e38", "-3.4028234e38", "0", "NaN", "Inf"}, []string{"3.5e38", "-3.5e38", "1e300"}},
		"FLOAT64": {[]string{"1e300"}, nil},
		"INT128":  {[]string{"9223372036854775807"}, nil},
		"Enum":    {[]string{"300"}, nil},
	} {
		for _, v := range append(tc.in, "", "x") {
			if initialValueOutOfRange(btype, v) {
				t.Errorf("%s %q: out of range, want in range or left to the parser", btype, v)
			}
		}
		for _, v := range tc.out {
			if !initialValueOutOfRange(btype, v) {
				t.Errorf("%s %q: in range, want out of range", btype, v)
			}
		}
	}
}

// TestFromSCL_InitialValueOutOfRangeKeepsDefault: an SCL value outside
// the range of its basic type is a model warning and the attribute keeps
// its type's default value (no InitialValue); per setting group as well.
// Values in range are kept.
func TestFromSCL_InitialValueOutOfRangeKeepsDefault(t *testing.T) {
	doc := &scl.SCL{
		IEDs: []scl.IED{{Name: "IED1", AccessPoints: []scl.AccessPoint{{Name: "AP1", Server: &scl.Server{
			LDevices: []scl.LDevice{{Inst: "LD1",
				LN0: &scl.LN{LNClass: "LLN0", LNType: "LNT_LLN0"},
				LNs: []scl.LN{{LNClass: "GGIO", Inst: "1", LNType: "LNT_GGIO",
					DOIs: []scl.DOI{{Name: "Set", DAIs: []scl.DAI{
						{Name: "small", Val: "300"},
						{Name: "byte", Val: "255"},
						{Name: "f", Val: "1e300"},
						{Name: "grp", Val: "70000", Vals: []scl.Value{{Value: "1", SGroup: 1}, {Value: "70000", SGroup: 2}}},
					}}},
				}},
			}},
		}}}}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT_LLN0", LNClass: "LLN0"},
				{ID: "LNT_GGIO", LNClass: "GGIO", DOs: []scl.DO{{Name: "Set", Type: "DOT_SET"}}},
			},
			DOTypes: []scl.DOType{{ID: "DOT_SET", CDC: "ING", DAs: []scl.DA{
				{Name: "small", FC: "SP", BType: "INT8"},
				{Name: "byte", FC: "SP", BType: "INT8U"},
				{Name: "f", FC: "SP", BType: "FLOAT32"},
				{Name: "grp", FC: "SG", BType: "INT16U"},
			}}},
		},
	}
	m, err := FromSCL(doc, "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	var set *DataObject
	for i := range m.LogicalDevices[0].LogicalNodes {
		if o := findDOByName(m.LogicalDevices[0].LogicalNodes[i].DataObjects, "Set"); o != nil {
			set = o
		}
	}
	if set == nil {
		t.Fatal("no GGIO1.Set")
	}
	values := map[string]*DataAttribute{}
	for i := range set.Attributes {
		values[set.Attributes[i].Name] = &set.Attributes[i]
	}
	for name, want := range map[string]string{"small": "", "byte": "255", "f": "", "grp": ""} {
		if got := values[name].InitialValue; got != want {
			t.Errorf("%s: InitialValue %q, want %q", name, got, want)
		}
	}
	if g := values["grp"].GroupValues; g[1] != "1" || g[2] != "" {
		t.Errorf("grp: GroupValues %v, want group 1 kept and group 2 dropped", g)
	}
	warned := strings.Join(m.Warnings, "\n")
	for _, w := range []string{`Set.small: Val "300" is outside INT8`, `Set.f: Val "1e300" is outside FLOAT32`, `Set.grp: Val sGroup=2 "70000" is outside INT16U`} {
		if !strings.Contains(warned, w) {
			t.Errorf("warnings %q lack %q", m.Warnings, w)
		}
	}
	if strings.Contains(warned, "Set.byte") || strings.Count(warned, "Set.grp") != 1 {
		t.Errorf("warnings %q: want none for byte and one for grp", m.Warnings)
	}
	srv := mms.NewServer(mms.ServerOptions{})
	if _, err := RegisterModel(srv, m, nil); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
}
