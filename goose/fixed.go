// SPDX-License-Identifier: MIT

package goose

import (
	"encoding/binary"
	"math"

	"github.com/otfabric/go-mms"
)

// Fixed-length GOOSE encoding (IEC 61850-8-1:2011 Annex A.3, SCL
// GSEControl fixedOffs="true"): every field keeps the same offset for a
// given configuration; only the content of the values varies. The
// encoding is still valid BER as long as the subscriber does not insist
// on the shortest length form (A.3), so the decoder accepts it.

// Fixed sizes of Table A.2 that do not follow from the member type.
const (
	// FixedOctetStringLen is the content length of an OCTET STRING in a
	// fixed-length message (Table A.2: 20, zero padded).
	FixedOctetStringLen = 20

	// FixedVisibleStringLen is the content length of a VISIBLE STRING in
	// a fixed-length message (Table A.2: 35, "Null terminated").
	FixedVisibleStringLen = 35
)

// fixedLength carries the member types of a fixed-length encoding.
// A nil *fixedLength selects the normal (shortest-form) BER encoding.
type fixedLength struct {
	types []mms.TypeSpec // per top-level allData member; may be short or nil
}

// EncodePDUFixedLength returns the fixed-length encoding of the
// goosePdu (IEC 61850-8-1 Annex A.3). types gives the MMS type of each
// allData member: integer widths come from TypeSpec.Size (8, 16, 32 or
// 64 bits; unknown widths are sent as 32 bits, or 64 for values outside
// the 32-bit range), FLOAT64 from FormatWidth 64. Without a type every
// member is encoded as above with unknown widths. Values that do not fit
// their fixed size (an OCTET STRING longer than 20 octets, a VISIBLE
// STRING longer than 35) fail with [ErrInvalidValue].
//
// Deviations: FLOAT32 is sent with the 5-octet MMS FloatingPoint
// encoding (exponent-width octet 8 plus IEEE 754 single) that MMS
// decoders expect, not the 4 octets of Table A.2; MMSString,
// GeneralizedTime, BCD and object identifiers, not covered by Table
// A.2, keep their BER encoding.
func EncodePDUFixedLength(p *PDU, types []mms.TypeSpec) ([]byte, error) {
	return encodePDU(p, &fixedLength{types: types})
}

// EncodeFixedLength is [Encode] with the fixed-length goosePdu of
// [EncodePDUFixedLength].
func EncodeFixedLength(f *Frame, types []mms.TypeSpec) ([]byte, error) {
	return appendFrame(nil, f, &fixedLength{types: types})
}

func (fx *fixedLength) encodeAllData(values []*mms.Value) ([]byte, error) {
	if fx == nil {
		return encodeAllData(values)
	}
	var out []byte
	var err error
	for i, v := range values {
		var ts *mms.TypeSpec
		if i < len(fx.types) {
			ts = &fx.types[i]
		}
		if out, err = appendDataFixed(out, v, ts, 1); err != nil {
			return nil, &EncodeError{Field: "allData", Index: i, Err: err}
		}
	}
	return out, nil
}

// fixedUint32 is the Table A.1 / A.3 INT32U encoding: length 5, a zero
// octet and the 32-bit big-endian value.
func fixedUint32(v uint64) []byte {
	c := make([]byte, 5)
	binary.BigEndian.PutUint32(c[1:], uint32(v&math.MaxUint32)) // callers pass INT32U fields
	return c
}

// appendDataFixed appends the Table A.2 encoding of v (type ts, may be
// nil).
func appendDataFixed(b []byte, v *mms.Value, ts *mms.TypeSpec, depth int) ([]byte, error) {
	if v == nil {
		return b, errValue("nil value")
	}
	if depth > DefaultLimits.MaxDepth {
		return b, errLimit("allData nesting deeper than %d", DefaultLimits.MaxDepth)
	}
	if ts != nil && ts.Type != v.Type() {
		ts = nil // the member type does not describe this value
	}
	size := 0
	if ts != nil {
		size = ts.Size
	}
	switch v.Type() {
	case mms.ValueTypeInteger:
		x, _ := v.Int64()
		bits := size
		if bits <= 0 || bits > 64 {
			bits = 32
			if x < math.MinInt32 || x > math.MaxInt32 {
				bits = 64
			}
		}
		n := fixedIntOctets(bits)
		if bits < 64 && (x < -(1<<(bits-1)) || x >= 1<<(bits-1)) {
			return b, errValue("integer %d does not fit INT%d", x, bits)
		}
		c := make([]byte, n)
		for i, y := n-1, x; i >= 0; i-- {
			c[i] = byte(y & 0xff)
			y >>= 8 // arithmetic shift: the leading octets sign-extend
		}
		return appendTLV(b, tagInteger, c), nil
	case mms.ValueTypeUnsigned:
		x, _ := v.Uint64()
		bits := size
		if bits <= 0 || bits > 64 {
			bits = 32
			if x > math.MaxUint32 {
				bits = 64
			}
		}
		if bits == 24 {
			bits = 32 // INT24U is "not used" in Table A.2
		}
		if bits < 64 && x>>bits != 0 {
			return b, errValue("unsigned %d does not fit INT%dU", x, bits)
		}
		n := fixedIntOctets(bits)
		c := make([]byte, n)
		for i := n - 1; i >= 0; i-- {
			c[i] = byte(x)
			x >>= 8
		}
		return appendTLV(b, tagUnsigned, c), nil
	case mms.ValueTypeFloat:
		f, _ := v.Float64()
		if ts != nil && ts.FormatWidth == 64 {
			c := make([]byte, 9)
			c[0] = 11
			binary.BigEndian.PutUint64(c[1:], math.Float64bits(f))
			return appendTLV(b, tagFloat, c), nil
		}
		c := make([]byte, 5)
		c[0] = 8
		binary.BigEndian.PutUint32(c[1:], math.Float32bits(float32(f)))
		return appendTLV(b, tagFloat, c), nil
	case mms.ValueTypeBitString, mms.ValueTypeBooleanArray:
		n := 0
		if v.Type() == mms.ValueTypeBitString {
			n, _ = v.BitStringLength()
		} else {
			_, n, _ = v.BooleanArray()
		}
		if size > 0 && n != size {
			return b, errValue("bit string of %d bits, member has %d", n, size)
		}
		return appendData(b, v, depth)
	case mms.ValueTypeOctetString:
		x, _ := v.OctetString()
		if len(x) > FixedOctetStringLen {
			return b, errValue("octet string of %d octets exceeds fixed length %d", len(x), FixedOctetStringLen)
		}
		c := make([]byte, FixedOctetStringLen)
		copy(c, x)
		return appendTLV(b, tagOctetString, c), nil
	case mms.ValueTypeVisibleString:
		x, _ := v.VisibleString()
		if len(x) > FixedVisibleStringLen {
			return b, errValue("visible string of %d octets exceeds fixed length %d", len(x), FixedVisibleStringLen)
		}
		c := make([]byte, FixedVisibleStringLen)
		copy(c, x)
		return appendTLV(b, tagVisibleStr, c), nil
	case mms.ValueTypeStructure, mms.ValueTypeArray:
		var elems []*mms.Value
		tag := tagStructure
		if v.Type() == mms.ValueTypeArray {
			elems, _ = v.ArrayElements()
			tag = tagArray
		} else {
			elems, _ = v.Structure()
		}
		var inner []byte
		var err error
		for i, e := range elems {
			var ets *mms.TypeSpec
			switch {
			case ts == nil:
			case tag == tagArray:
				ets = ts.Element
			case i < len(ts.Elements):
				ets = &ts.Elements[i].Type
			}
			if inner, err = appendDataFixed(inner, e, ets, depth+1); err != nil {
				return b, err
			}
		}
		return appendTLV(b, tag, inner), nil
	default:
		// Boolean, UtcTime and TimeOfDay already have a fixed length;
		// the types not in Table A.2 keep their BER encoding.
		return appendData(b, v, depth)
	}
}

// fixedIntOctets is the Table A.2 content length of an integer of the
// given width: INT8 2, INT16 3, INT32 5, INT64 9 (one sign or zero
// octet in front of the big-endian value).
func fixedIntOctets(bits int) int {
	switch {
	case bits <= 8:
		return 2
	case bits <= 16:
		return 3
	case bits <= 32:
		return 5
	default:
		return 9
	}
}
