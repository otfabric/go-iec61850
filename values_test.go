// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func TestNewValue_Nil(t *testing.T) {
	v := NewValue(nil)
	if v != nil {
		t.Error("NewValue(nil) should return nil")
	}
}

func TestValue_NilAccessors(t *testing.T) {
	var v *Value

	accessors := []struct {
		name string
		fn   func() error
	}{
		{"Bool", func() error { _, err := v.Bool(); return err }},
		{"Int32", func() error { _, err := v.Int32(); return err }},
		{"Int64", func() error { _, err := v.Int64(); return err }},
		{"Uint32", func() error { _, err := v.Uint32(); return err }},
		{"Uint64", func() error { _, err := v.Uint64(); return err }},
		{"Float32", func() error { _, err := v.Float32(); return err }},
		{"Float64", func() error { _, err := v.Float64(); return err }},
		{"VisibleString", func() error { _, err := v.VisibleString(); return err }},
		{"MmsString", func() error { _, err := v.MmsString(); return err }},
		{"OctetString", func() error { _, err := v.OctetString(); return err }},
		{"BitString", func() error { _, err := v.BitString(); return err }},
		{"Quality", func() error { _, err := v.Quality(); return err }},
		{"Timestamp", func() error { _, err := v.Timestamp(); return err }},
		{"Elements", func() error { _, err := v.Elements(); return err }},
	}
	for _, tc := range accessors {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected error for nil *Value")
			}
			var de *DecodeError
			if !errors.As(err, &de) {
				t.Errorf("expected DecodeError, got %T: %v", err, err)
			}
		})
	}
}

func TestValue_MMS(t *testing.T) {
	raw := mms.NewInteger(42)
	v := NewValue(raw)
	if v.MMS() != raw {
		t.Error("MMS() should return the underlying value")
	}
}

func TestValue_MMS_Nil(t *testing.T) {
	var v *Value
	if v.MMS() != nil {
		t.Error("nil Value.MMS() should return nil")
	}
}

func TestValue_Type(t *testing.T) {
	tests := []struct {
		name string
		val  *Value
		want mms.ValueType
	}{
		{"bool", BoolValue(true), mms.ValueTypeBoolean},
		{"int", IntValue(42), mms.ValueTypeInteger},
		{"uint", UintValue(42), mms.ValueTypeUnsigned},
		{"float", FloatValue(3.14), mms.ValueTypeFloat},
		{"string", StringValue("hello"), mms.ValueTypeVisibleString},
		{"nil", nil, mms.ValueTypeDataAccessError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.val.Type()
			if got != tc.want {
				t.Errorf("Type() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValue_Bool(t *testing.T) {
	v := BoolValue(true)
	b, err := v.Bool()
	if err != nil {
		t.Fatalf("Bool: %v", err)
	}
	if !b {
		t.Error("Bool() = false, want true")
	}

	_, err = IntValue(1).Bool()
	if err == nil {
		t.Error("expected error for non-bool")
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Errorf("expected DecodeError, got %T", err)
	}
}

func TestValue_Int(t *testing.T) {
	v := IntValue(42)
	i32, err := v.Int32()
	if err != nil {
		t.Fatalf("Int32: %v", err)
	}
	if i32 != 42 {
		t.Errorf("Int32() = %d, want 42", i32)
	}

	i64, err := v.Int64()
	if err != nil {
		t.Fatalf("Int64: %v", err)
	}
	if i64 != 42 {
		t.Errorf("Int64() = %d, want 42", i64)
	}

	_, err = BoolValue(true).Int32()
	if err == nil {
		t.Error("expected error for non-int")
	}
}

func TestValue_Uint(t *testing.T) {
	v := UintValue(100)
	u32, err := v.Uint32()
	if err != nil {
		t.Fatalf("Uint32: %v", err)
	}
	if u32 != 100 {
		t.Errorf("Uint32() = %d, want 100", u32)
	}

	u64, err := v.Uint64()
	if err != nil {
		t.Fatalf("Uint64: %v", err)
	}
	if u64 != 100 {
		t.Errorf("Uint64() = %d, want 100", u64)
	}
}

func TestValue_Float(t *testing.T) {
	v := FloatValue(3.14)
	f32, err := v.Float32()
	if err != nil {
		t.Fatalf("Float32: %v", err)
	}
	if f32 < 3.13 || f32 > 3.15 {
		t.Errorf("Float32() = %f, want ~3.14", f32)
	}

	f64, err := v.Float64()
	if err != nil {
		t.Fatalf("Float64: %v", err)
	}
	if f64 != 3.14 {
		t.Errorf("Float64() = %f, want 3.14", f64)
	}
}

func TestValue_VisibleString(t *testing.T) {
	v := StringValue("hello")
	s, err := v.VisibleString()
	if err != nil {
		t.Fatalf("VisibleString: %v", err)
	}
	if s != "hello" {
		t.Errorf("VisibleString() = %q, want %q", s, "hello")
	}

	_, err = IntValue(1).VisibleString()
	if err == nil {
		t.Error("expected error for non-string")
	}
}

func TestValue_OctetString(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	v := OctetStringValue(data)
	got, err := v.OctetString()
	if err != nil {
		t.Fatalf("OctetString: %v", err)
	}
	if len(got) != 3 || got[0] != 0x01 {
		t.Errorf("OctetString() = %v, want %v", got, data)
	}
}

func TestValue_Quality(t *testing.T) {
	q := Quality(ValidityQuestionable) | QualityOldData
	v := QualityValue(q)
	decoded, err := v.Quality()
	if err != nil {
		t.Fatalf("Quality: %v", err)
	}
	if decoded != q {
		t.Errorf("Quality() = 0x%04x, want 0x%04x", uint16(decoded), uint16(q))
	}

	_, err = IntValue(1).Quality()
	if err == nil {
		t.Error("expected error for non-bit-string")
	}
}

func TestValue_Timestamp(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	ts := Timestamp{Time: now}
	v := TimestampValue(ts)
	decoded, err := v.Timestamp()
	if err != nil {
		t.Fatalf("Timestamp: %v", err)
	}
	if decoded.Time.Unix() != now.Unix() {
		t.Errorf("Timestamp() = %v, want %v", decoded.Time, now)
	}

	_, err = IntValue(1).Timestamp()
	if err == nil {
		t.Error("expected error for non-UTCTime")
	}
}

func TestValue_Elements(t *testing.T) {
	elems := []*Value{IntValue(1), IntValue(2), IntValue(3)}
	v, err := StructureValue(elems)
	if err != nil {
		t.Fatalf("StructureValue: %v", err)
	}
	got, err := v.Elements()
	if err != nil {
		t.Fatalf("Elements: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Elements() returned %d elements, want 3", len(got))
	}

	i, err := got[0].Int64()
	if err != nil {
		t.Fatalf("Elements[0].Int64: %v", err)
	}
	if i != 1 {
		t.Errorf("Elements[0] = %d, want 1", i)
	}

	_, err = IntValue(1).Elements()
	if err == nil {
		t.Error("expected error for non-structure")
	}
}

func TestValue_Elements_Array(t *testing.T) {
	elems := []*Value{BoolValue(true), BoolValue(false)}
	v, err := ArrayValue(elems)
	if err != nil {
		t.Fatalf("ArrayValue: %v", err)
	}
	got, err := v.Elements()
	if err != nil {
		t.Fatalf("Elements: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Elements() returned %d, want 2", len(got))
	}
}

func TestValue_String_Debug(t *testing.T) {
	v := IntValue(42)
	s := v.String()
	if s == "" {
		t.Error("String() should not be empty")
	}

	var nilVal *Value
	if nilVal.String() != "<nil>" {
		t.Errorf("nil String() = %q, want %q", nilVal.String(), "<nil>")
	}
}

func TestValue_MmsString(t *testing.T) {
	v := NewValue(mms.NewMmsString("hello utf8"))
	s, err := v.MmsString()
	if err != nil {
		t.Fatalf("MmsString: %v", err)
	}
	if s != "hello utf8" {
		t.Errorf("MmsString() = %q, want %q", s, "hello utf8")
	}
}

func TestValue_MmsString_TypeMismatch(t *testing.T) {
	v := NewValue(mms.NewInteger(42))
	_, err := v.MmsString()
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
}

func TestValue_BitString(t *testing.T) {
	raw := []byte{0xAB, 0xCD}
	v := NewValue(mms.NewBitString(raw))
	b, err := v.BitString()
	if err != nil {
		t.Fatalf("BitString: %v", err)
	}
	if len(b) != 2 || b[0] != 0xAB || b[1] != 0xCD {
		t.Errorf("BitString() = %x, want abcd", b)
	}
}

func TestValue_BitString_TypeMismatch(t *testing.T) {
	v := NewValue(mms.NewBoolean(true))
	_, err := v.BitString()
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
}

func TestConstructors_MMS(t *testing.T) {
	// Verify constructors produce values with correct MMS types.
	tests := []struct {
		name string
		val  *Value
		typ  mms.ValueType
	}{
		{"BoolValue", BoolValue(false), mms.ValueTypeBoolean},
		{"IntValue", IntValue(-5), mms.ValueTypeInteger},
		{"UintValue", UintValue(0), mms.ValueTypeUnsigned},
		{"FloatValue", FloatValue(0.0), mms.ValueTypeFloat},
		{"StringValue", StringValue(""), mms.ValueTypeVisibleString},
		{"OctetStringValue", OctetStringValue(nil), mms.ValueTypeOctetString},
		{"QualityValue", QualityValue(0), mms.ValueTypeBitString},
		{"TimestampValue", TimestampValue(Timestamp{Time: time.Now()}), mms.ValueTypeUTCTime},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.val.Type() != tc.typ {
				t.Errorf("Type() = %v, want %v", tc.val.Type(), tc.typ)
			}
			if tc.val.MMS() == nil {
				t.Error("MMS() should not be nil")
			}
		})
	}
}

func TestStructureValue_NilElement(t *testing.T) {
	_, err := StructureValue([]*Value{BoolValue(true), nil, IntValue(1)})
	if err == nil {
		t.Fatal("expected error for nil element in StructureValue")
	}
	v := UnsafeStructureValue([]*Value{BoolValue(true), nil, IntValue(1)})
	elems, err := v.Elements()
	if err != nil {
		t.Fatalf("Elements: %v", err)
	}
	if len(elems) != 3 {
		t.Fatalf("got %d elements, want 3", len(elems))
	}
}

func TestArrayValue_NilElement(t *testing.T) {
	_, err := ArrayValue([]*Value{IntValue(1), nil, IntValue(3)})
	if err == nil {
		t.Fatal("expected error for nil element in ArrayValue")
	}
	v := UnsafeArrayValue([]*Value{IntValue(1), nil, IntValue(3)})
	elems, err := v.Elements()
	if err != nil {
		t.Fatalf("Elements: %v", err)
	}
	if len(elems) != 3 {
		t.Fatalf("got %d elements, want 3", len(elems))
	}
}

func TestValue_IsStructure_IsArray(t *testing.T) {
	sv, _ := StructureValue([]*Value{IntValue(1)})
	if !sv.IsStructure() {
		t.Error("expected IsStructure() = true")
	}
	if sv.IsArray() {
		t.Error("expected IsArray() = false for structure")
	}

	av, _ := ArrayValue([]*Value{IntValue(1)})
	if !av.IsArray() {
		t.Error("expected IsArray() = true")
	}
	if av.IsStructure() {
		t.Error("expected IsStructure() = false for array")
	}

	if IntValue(1).IsStructure() || IntValue(1).IsArray() {
		t.Error("scalar should not be structure or array")
	}
}
