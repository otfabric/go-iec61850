package servermodel

import (
	"testing"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-mms"
)

func TestNewValueStoreWithOptions(t *testing.T) {
	vs := NewValueStoreWithOptions(ValueStoreOptions{DefensiveCopy: true})
	if vs == nil {
		t.Fatal("expected non-nil ValueStore")
	}

	v := mms.NewInteger(42)
	vs.Set("k", v)

	got := vs.Get("k")
	if got == nil {
		t.Fatal("expected value")
	}
	if got == v {
		t.Error("defensive copy should return a different pointer")
	}
	i, ok := got.Int64()
	if !ok || i != 42 {
		t.Errorf("got %d, want 42", i)
	}
}

func TestValidateWriteSize(t *testing.T) {
	tests := []struct {
		name    string
		val     *mms.Value
		ts      mms.TypeSpec
		wantErr bool
	}{
		{
			name: "visible string within limit",
			val:  mms.NewVisibleString("abc"),
			ts:   mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 10},
		},
		{
			name:    "visible string exceeds limit",
			val:     mms.NewVisibleString("this is too long"),
			ts:      mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 5},
			wantErr: true,
		},
		{
			name: "octet string within limit",
			val:  mms.NewOctetString([]byte{1, 2, 3}),
			ts:   mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 6},
		},
		{
			name:    "octet string exceeds limit",
			val:     mms.NewOctetString([]byte{1, 2, 3, 4, 5, 6, 7}),
			ts:      mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 4},
			wantErr: true,
		},
		{
			name: "bitstring matching size",
			val:  mms.NewBitStringWithLength([]byte{0xC0}, 2),
			ts:   mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 2},
		},
		{
			name:    "bitstring mismatched size",
			val:     mms.NewBitStringWithLength([]byte{0xC0, 0x00}, 13),
			ts:      mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 2},
			wantErr: true,
		},
		{
			name: "no size constraint",
			val:  mms.NewVisibleString("anything"),
			ts:   mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 0},
		},
		{
			name: "integer no size check",
			val:  mms.NewInteger(999),
			ts:   mms.TypeSpec{Type: mms.ValueTypeInteger},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWriteSize(tc.val, tc.ts)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateWriteSize() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseInitialValue(t *testing.T) {
	tests := []struct {
		name    string
		btype   string
		val     string
		wantErr bool
		check   func(t *testing.T, v *mms.Value)
	}{
		{name: "BOOLEAN true", btype: "BOOLEAN", val: "true", check: func(t *testing.T, v *mms.Value) {
			b, ok := v.Bool()
			if !ok || !b {
				t.Error("expected true")
			}
		}},
		{name: "BOOLEAN false", btype: "BOOLEAN", val: "false", check: func(t *testing.T, v *mms.Value) {
			b, ok := v.Bool()
			if !ok || b {
				t.Error("expected false")
			}
		}},
		{name: "BOOLEAN 1", btype: "BOOLEAN", val: "1", check: func(t *testing.T, v *mms.Value) {
			b, ok := v.Bool()
			if !ok || !b {
				t.Error("expected true")
			}
		}},
		{name: "BOOLEAN 0", btype: "BOOLEAN", val: "0", check: func(t *testing.T, v *mms.Value) {
			b, ok := v.Bool()
			if !ok || b {
				t.Error("expected false")
			}
		}},
		{name: "BOOLEAN invalid", btype: "BOOLEAN", val: "maybe", wantErr: true},
		{name: "INT32", btype: "INT32", val: "42", check: func(t *testing.T, v *mms.Value) {
			i, ok := v.Int64()
			if !ok || i != 42 {
				t.Errorf("expected 42, got %d", i)
			}
		}},
		{name: "INT8", btype: "INT8", val: "-5", check: func(t *testing.T, v *mms.Value) {
			i, ok := v.Int64()
			if !ok || i != -5 {
				t.Errorf("expected -5, got %d", i)
			}
		}},
		{name: "INT32 invalid", btype: "INT32", val: "abc", wantErr: true},
		{name: "Enum", btype: "Enum", val: "3", check: func(t *testing.T, v *mms.Value) {
			i, ok := v.Int64()
			if !ok || i != 3 {
				t.Errorf("expected 3, got %d", i)
			}
		}},
		{name: "Enum invalid", btype: "Enum", val: "xyz", wantErr: true},
		{name: "INT16U", btype: "INT16U", val: "100"},
		{name: "INT32U", btype: "INT32U", val: "65535"},
		{name: "INT8U invalid", btype: "INT8U", val: "abc", wantErr: true},
		{name: "FLOAT32", btype: "FLOAT32", val: "3.14", check: func(t *testing.T, v *mms.Value) {
			f, ok := v.Float64()
			if !ok || (f < 3.13 || f > 3.15) {
				t.Errorf("expected ~3.14, got %f", f)
			}
		}},
		{name: "FLOAT64", btype: "FLOAT64", val: "2.718"},
		{name: "FLOAT32 invalid", btype: "FLOAT32", val: "notfloat", wantErr: true},
		{name: "VisString32", btype: "VisString32", val: "hello", check: func(t *testing.T, v *mms.Value) {
			s, ok := v.VisibleString()
			if !ok || s != "hello" {
				t.Errorf("expected hello, got %s", s)
			}
		}},
		{name: "VisString64", btype: "VisString64", val: "world"},
		{name: "VisString65", btype: "VisString65", val: "x"},
		{name: "VisString129", btype: "VisString129", val: "long"},
		{name: "VisString255", btype: "VisString255", val: "longer"},
		{name: "Unicode255", btype: "Unicode255", val: "unicode text"},
		{name: "Timestamp empty", btype: "Timestamp", val: ""},
		{name: "Timestamp non-empty", btype: "Timestamp", val: "2024-01-01", wantErr: true},
		{name: "EntryTime", btype: "EntryTime", val: ""},
		{name: "Octet6 decimal", btype: "Octet6", val: "255"},
		{name: "Octet16 hex", btype: "Octet16", val: "0xDEAD"},
		{name: "Octet64 decimal", btype: "Octet64", val: "0"},
		{name: "Tcmd", btype: "Tcmd", val: "1"},
		{name: "unsupported", btype: "UnknownType", val: "x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseInitialValue(tc.btype, tc.val)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseInitialValue(%q, %q) error = %v, wantErr %v", tc.btype, tc.val, err, tc.wantErr)
			}
			if err == nil && tc.check != nil {
				tc.check(t, v)
			}
		})
	}
}

func TestParseInitialOctetString(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		maxSize int
		wantErr bool
		wantLen int
	}{
		{name: "hex valid", val: "0xCAFE", maxSize: 6, wantLen: 2},
		{name: "hex odd length", val: "0xF", maxSize: 6, wantLen: 1},
		{name: "hex exceeds max", val: "0xCAFEBABE", maxSize: 2, wantErr: true},
		{name: "hex invalid chars", val: "0xGG", maxSize: 6, wantErr: true},
		{name: "decimal", val: "256", maxSize: 6, wantLen: 6},
		{name: "decimal zero", val: "0", maxSize: 6, wantLen: 6},
		{name: "decimal invalid", val: "notanumber", maxSize: 6, wantErr: true},
		{name: "upper hex prefix", val: "0XABCD", maxSize: 6, wantLen: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseInitialOctetString(tc.val, tc.maxSize)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseInitialOctetString(%q, %d) error = %v, wantErr %v", tc.val, tc.maxSize, err, tc.wantErr)
			}
			if err == nil {
				bs, ok := v.OctetString()
				if !ok {
					t.Fatal("expected OctetString")
				}
				if len(bs) != tc.wantLen {
					t.Errorf("len = %d, want %d", len(bs), tc.wantLen)
				}
			}
		})
	}
}

func TestHexDecode(t *testing.T) {
	data, err := hexDecode("CAFE")
	if err != nil {
		t.Fatalf("hexDecode: %v", err)
	}
	if len(data) != 2 || data[0] != 0xCA || data[1] != 0xFE {
		t.Errorf("got %x, want CAFE", data)
	}

	data, err = hexDecode("ab")
	if err != nil {
		t.Fatal(err)
	}
	if data[0] != 0xAB {
		t.Errorf("got %x, want AB", data[0])
	}

	_, err = hexDecode("ZZ")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestHexNibble(t *testing.T) {
	tests := []struct {
		c    byte
		want byte
		err  bool
	}{
		{'0', 0, false},
		{'9', 9, false},
		{'a', 10, false},
		{'f', 15, false},
		{'A', 10, false},
		{'F', 15, false},
		{'G', 0, true},
		{'z', 0, true},
	}

	for _, tc := range tests {
		got, err := hexNibble(tc.c)
		if (err != nil) != tc.err {
			t.Errorf("hexNibble(%c) error = %v, wantErr %v", tc.c, err, tc.err)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("hexNibble(%c) = %d, want %d", tc.c, got, tc.want)
		}
	}
}

func TestApplySDIOnAttr(t *testing.T) {
	attr := DataAttribute{
		Name:  "parent",
		BType: "Struct",
		Children: []DataAttribute{
			{Name: "child1", BType: "FLOAT32"},
			{Name: "child2", BType: "INT32"},
		},
	}

	sdi := &scl.SDI{
		Name: "child1",
		DAIs: []scl.DAI{{Name: "subfield", Val: "99"}},
	}

	var warnings []string
	applySDIOnAttr(&attr, sdi, "test", &warnings)

	if len(warnings) == 0 {
		t.Error("expected warning for unmatched DAI on leaf attribute")
	}
}

func TestApplySDIOnAttr_Match(t *testing.T) {
	attr := DataAttribute{
		Name:  "parent",
		BType: "Struct",
		Children: []DataAttribute{
			{
				Name:  "child1",
				BType: "Struct",
				Children: []DataAttribute{
					{Name: "f", BType: "FLOAT32"},
				},
			},
		},
	}

	sdi := &scl.SDI{
		Name: "child1",
		DAIs: []scl.DAI{{Name: "f", Val: "3.14"}},
	}

	var warnings []string
	applySDIOnAttr(&attr, sdi, "test", &warnings)

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if attr.Children[0].Children[0].InitialValue != "3.14" {
		t.Errorf("expected InitialValue 3.14, got %q", attr.Children[0].Children[0].InitialValue)
	}
}

func TestApplySDIOnAttr_NoMatch(t *testing.T) {
	attr := DataAttribute{
		Name:  "parent",
		BType: "Struct",
		Children: []DataAttribute{
			{Name: "a", BType: "INT32"},
		},
	}

	sdi := &scl.SDI{
		Name: "nonexistent",
		DAIs: []scl.DAI{{Name: "x", Val: "1"}},
	}

	var warnings []string
	applySDIOnAttr(&attr, sdi, "test", &warnings)

	if len(warnings) == 0 {
		t.Error("expected warning for unmatched SDI")
	}
}

func TestApplySDIOverrides_AttributeMatch(t *testing.T) {
	obj := &DataObject{
		Name: "DO1",
		Attributes: []DataAttribute{
			{
				Name:  "mag",
				BType: "Struct",
				Children: []DataAttribute{
					{Name: "f", BType: "FLOAT32"},
				},
			},
		},
	}

	sdi := &scl.SDI{
		Name: "mag",
		DAIs: []scl.DAI{{Name: "f", Val: "1.23"}},
	}

	var warnings []string
	applySDIOverrides(obj, sdi, "test", &warnings)

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if obj.Attributes[0].Children[0].InitialValue != "1.23" {
		t.Errorf("expected 1.23, got %q", obj.Attributes[0].Children[0].InitialValue)
	}
}

func TestApplySDIOverrides_UnmatchedWarning(t *testing.T) {
	obj := &DataObject{Name: "DO1"}
	sdi := &scl.SDI{Name: "unknown"}
	var warnings []string
	applySDIOverrides(obj, sdi, "test", &warnings)
	if len(warnings) == 0 {
		t.Error("expected warning for unmatched SDI")
	}
}
