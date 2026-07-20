// SPDX-License-Identifier: MIT

package iec61850

import (
	"fmt"

	"github.com/otfabric/go-mms"
)

// Value wraps an [mms.Value] with IEC 61850 semantic accessors.
//
// Value provides typed accessor methods that return errors instead
// of ok booleans, and IEC 61850-specific decoders for Quality and
// Timestamp. Use [Value.MMS] to access the underlying MMS value
// when needed.
//
// Values are created by [NewValue], the constructor functions
// ([BoolValue], [IntValue], etc.), or returned from read operations.
type Value struct {
	mmsVal *mms.Value
}

// NewValue wraps an [mms.Value]. Returns nil if v is nil.
func NewValue(v *mms.Value) *Value {
	if v == nil {
		return nil
	}
	return &Value{mmsVal: v}
}

// MMS returns the underlying [mms.Value]. This is the escape hatch
// for operations not covered by the IEC 61850 typed accessors.
func (v *Value) MMS() *mms.Value {
	if v == nil {
		return nil
	}
	return v.mmsVal
}

// Type returns the MMS value type. Returns
// [mms.ValueTypeDataAccessError] when the Value or its underlying
// MMS value is nil.
func (v *Value) Type() mms.ValueType {
	if v == nil || v.mmsVal == nil {
		return mms.ValueTypeDataAccessError
	}
	return v.mmsVal.Type()
}

// Bool returns the value as a bool.
// Returns a [DecodeError] if the value is nil or not a boolean.
func (v *Value) Bool() (bool, error) {
	if err := v.requireValue("bool"); err != nil {
		return false, err
	}
	b, ok := v.mmsVal.Bool()
	if !ok {
		return false, v.typeError("bool")
	}
	return b, nil
}

// Int32 returns the value as an int32.
// Returns a [DecodeError] if the value is nil or not an integer.
func (v *Value) Int32() (int32, error) {
	if err := v.requireValue("int32"); err != nil {
		return 0, err
	}
	i, ok := v.mmsVal.Int32()
	if !ok {
		return 0, v.typeError("int32")
	}
	return i, nil
}

// Int64 returns the value as an int64.
// Returns a [DecodeError] if the value is nil or not an integer.
func (v *Value) Int64() (int64, error) {
	if err := v.requireValue("int64"); err != nil {
		return 0, err
	}
	i, ok := v.mmsVal.Int64()
	if !ok {
		return 0, v.typeError("int64")
	}
	return i, nil
}

// Uint32 returns the value as a uint32.
// Returns a [DecodeError] if the value is nil or not an unsigned integer.
func (v *Value) Uint32() (uint32, error) {
	if err := v.requireValue("uint32"); err != nil {
		return 0, err
	}
	u, ok := v.mmsVal.Uint32()
	if !ok {
		return 0, v.typeError("uint32")
	}
	return u, nil
}

// Uint64 returns the value as a uint64.
// Returns a [DecodeError] if the value is nil or not an unsigned integer.
func (v *Value) Uint64() (uint64, error) {
	if err := v.requireValue("uint64"); err != nil {
		return 0, err
	}
	u, ok := v.mmsVal.Uint64()
	if !ok {
		return 0, v.typeError("uint64")
	}
	return u, nil
}

// Float32 returns the value as a float32.
// Returns a [DecodeError] if the value is nil or not a float.
func (v *Value) Float32() (float32, error) {
	if err := v.requireValue("float32"); err != nil {
		return 0, err
	}
	f, ok := v.mmsVal.Float32()
	if !ok {
		return 0, v.typeError("float32")
	}
	return f, nil
}

// Float64 returns the value as a float64.
// Returns a [DecodeError] if the value is nil or not a float.
func (v *Value) Float64() (float64, error) {
	if err := v.requireValue("float64"); err != nil {
		return 0, err
	}
	f, ok := v.mmsVal.Float64()
	if !ok {
		return 0, v.typeError("float64")
	}
	return f, nil
}

// VisibleString returns the value as a string (MMS VisibleString).
// Returns a [DecodeError] if the value is nil or not a visible string.
func (v *Value) VisibleString() (string, error) {
	if err := v.requireValue("visible-string"); err != nil {
		return "", err
	}
	s, ok := v.mmsVal.VisibleString()
	if !ok {
		return "", v.typeError("visible-string")
	}
	return s, nil
}

// MmsString returns the value as a string (MMS MmsString / UTF-8).
// Returns a [DecodeError] if the value is nil or not an MMS string.
func (v *Value) MmsString() (string, error) {
	if err := v.requireValue("mms-string"); err != nil {
		return "", err
	}
	s, ok := v.mmsVal.MmsString()
	if !ok {
		return "", v.typeError("mms-string")
	}
	return s, nil
}

// OctetString returns the value as a byte slice.
// The returned slice may alias the underlying MMS value memory.
// Copy it if ownership or later mutation is required.
// Returns a [DecodeError] if the value is nil or not an octet string.
func (v *Value) OctetString() ([]byte, error) {
	if err := v.requireValue("octet-string"); err != nil {
		return nil, err
	}
	b, ok := v.mmsVal.OctetString()
	if !ok {
		return nil, v.typeError("octet-string")
	}
	return b, nil
}

// BitString returns the raw bit string bytes.
// The returned slice may alias the underlying MMS value memory.
// Copy it if ownership or later mutation is required.
// Returns a [DecodeError] if the value is nil or not a bit string.
func (v *Value) BitString() ([]byte, error) {
	if err := v.requireValue("bit-string"); err != nil {
		return nil, err
	}
	b, ok := v.mmsVal.BitString()
	if !ok {
		return nil, v.typeError("bit-string")
	}
	return b, nil
}

// Quality decodes the value as an IEC 61850 quality bit string.
// Returns a [DecodeError] if the value is nil or not a 13-bit bit string.
func (v *Value) Quality() (Quality, error) {
	if err := v.requireValue("Quality"); err != nil {
		return 0, err
	}
	return DecodeQuality(v.mmsVal)
}

// Timestamp decodes the value as an IEC 61850 timestamp.
// Returns a [DecodeError] if the value is nil or not a UTCTime.
func (v *Value) Timestamp() (Timestamp, error) {
	if err := v.requireValue("Timestamp"); err != nil {
		return Timestamp{}, err
	}
	return DecodeTimestamp(v.mmsVal)
}

// IsStructure reports whether the value is an MMS structure.
func (v *Value) IsStructure() bool {
	if v == nil || v.mmsVal == nil {
		return false
	}
	return v.mmsVal.Type() == mms.ValueTypeStructure
}

// IsArray reports whether the value is an MMS array.
func (v *Value) IsArray() bool {
	if v == nil || v.mmsVal == nil {
		return false
	}
	return v.mmsVal.Type() == mms.ValueTypeArray
}

// Elements returns the structure or array elements as Values.
// Use [Value.IsStructure] and [Value.IsArray] to determine which
// container kind the value holds before calling this method.
// Returns a [DecodeError] if the value is nil or not a structure or array.
func (v *Value) Elements() ([]*Value, error) {
	if err := v.requireValue("structure or array"); err != nil {
		return nil, err
	}
	elems, ok := v.mmsVal.Structure()
	if !ok {
		elems, ok = v.mmsVal.ArrayElements()
		if !ok {
			return nil, v.typeError("structure or array")
		}
	}
	result := make([]*Value, len(elems))
	for i, e := range elems {
		result[i] = NewValue(e)
	}
	return result, nil
}

// String returns a debug-friendly string representation of the value.
func (v *Value) String() string {
	if v == nil || v.mmsVal == nil {
		return "<nil>"
	}
	return v.mmsVal.String()
}

// requireValue returns a DecodeError if the Value or its underlying
// mms.Value is nil.
func (v *Value) requireValue(expected string) error {
	if v == nil || v.mmsVal == nil {
		return &DecodeError{
			Type:    expected,
			Message: "nil value",
		}
	}
	return nil
}

func (v *Value) typeError(expected string) error {
	if v == nil || v.mmsVal == nil {
		return &DecodeError{
			Type:    expected,
			Message: "nil value",
		}
	}
	return &DecodeError{
		Type:    expected,
		Message: fmt.Sprintf("cannot decode %v as %s", v.mmsVal.Type(), expected),
	}
}

// --- Value constructors ---

// BoolValue creates a Value from a bool.
func BoolValue(b bool) *Value {
	return NewValue(mms.NewBoolean(b))
}

// IntValue creates a Value from an int64.
func IntValue(i int64) *Value {
	return NewValue(mms.NewInteger(i))
}

// UintValue creates a Value from a uint64.
func UintValue(u uint64) *Value {
	return NewValue(mms.NewUnsigned(u))
}

// FloatValue creates a Value from a float64.
func FloatValue(f float64) *Value {
	return NewValue(mms.NewFloat(f))
}

// StringValue creates a Value from a string (MMS VisibleString).
func StringValue(s string) *Value {
	return NewValue(mms.NewVisibleString(s))
}

// OctetStringValue creates a Value from a byte slice.
func OctetStringValue(data []byte) *Value {
	return NewValue(mms.NewOctetString(data))
}

// QualityValue creates a Value from a [Quality].
func QualityValue(q Quality) *Value {
	return NewValue(EncodeQuality(q))
}

// TimestampValue creates a Value from a [Timestamp].
func TimestampValue(ts Timestamp) *Value {
	return NewValue(EncodeTimestamp(ts))
}

// StructureValue creates a Value from a slice of element Values with
// validation. Returns an error if any element is nil.
//
// Use [UnsafeStructureValue] for the unchecked fast path when all
// elements are guaranteed non-nil.
func StructureValue(elements []*Value) (*Value, error) {
	mmsElements := make([]*mms.Value, len(elements))
	for i, e := range elements {
		if e == nil || e.MMS() == nil {
			return nil, fmt.Errorf("iec61850: structure element[%d] is nil", i)
		}
		mmsElements[i] = e.MMS()
	}
	return NewValue(mms.NewStructure(mmsElements)), nil
}

// UnsafeStructureValue creates a Value from a slice of element Values
// without nil-checking. Nil elements produce a malformed MMS
// structure that will fail encoding. Prefer [StructureValue] unless
// performance requires skipping validation.
func UnsafeStructureValue(elements []*Value) *Value {
	mmsElements := make([]*mms.Value, len(elements))
	for i, e := range elements {
		mmsElements[i] = e.MMS()
	}
	return NewValue(mms.NewStructure(mmsElements))
}

// ArrayValue creates a Value from a slice of element Values with
// validation. Returns an error if any element is nil.
//
// Use [UnsafeArrayValue] for the unchecked fast path when all
// elements are guaranteed non-nil.
func ArrayValue(elements []*Value) (*Value, error) {
	mmsElements := make([]*mms.Value, len(elements))
	for i, e := range elements {
		if e == nil || e.MMS() == nil {
			return nil, fmt.Errorf("iec61850: array element[%d] is nil", i)
		}
		mmsElements[i] = e.MMS()
	}
	return NewValue(mms.NewArray(mmsElements)), nil
}

// UnsafeArrayValue creates a Value from a slice of element Values
// without nil-checking. Nil elements produce a malformed MMS
// array that will fail encoding. Prefer [ArrayValue] unless
// performance requires skipping validation.
func UnsafeArrayValue(elements []*Value) *Value {
	mmsElements := make([]*mms.Value, len(elements))
	for i, e := range elements {
		mmsElements[i] = e.MMS()
	}
	return NewValue(mms.NewArray(mmsElements))
}
