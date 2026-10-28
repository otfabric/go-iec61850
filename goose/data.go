// SPDX-License-Identifier: MIT

package goose

import (
	"encoding/binary"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/otfabric/go-mms"
)

// MMS Data CHOICE tags (ISO 9506-2) used in GOOSE allData
// (IEC 61850-8-1 Annex A). REAL ([8]) and dataAccessError ([0]) are not
// valid in GOOSE and are rejected.
const (
	tagArray        byte = 0xa1
	tagStructure    byte = 0xa2
	tagBoolean      byte = 0x83
	tagBitString    byte = 0x84
	tagInteger      byte = 0x85
	tagUnsigned     byte = 0x86
	tagFloat        byte = 0x87
	tagOctetString  byte = 0x89
	tagVisibleStr   byte = 0x8a
	tagGenTime      byte = 0x8b
	tagBinaryTime   byte = 0x8c
	tagBCD          byte = 0x8d
	tagBooleanArray byte = 0x8e
	tagObjID        byte = 0x8f
	tagMMSString    byte = 0x90
	tagUTCTime      byte = 0x91
)

// Limits bounds the work the decoder performs on untrusted input.
type Limits struct {
	// MaxDepth is the maximum nesting depth of structures and arrays
	// in allData (a flat list of scalars has depth 1).
	MaxDepth int

	// MaxElements is the maximum total number of Data elements
	// (scalars and containers, at every level) in allData.
	MaxElements int
}

// DefaultLimits are the limits used by [Decode] and [DecodePDU].
var DefaultLimits = Limits{MaxDepth: 16, MaxElements: 4096}

var epoch1984 = time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC)

// appendData appends the BER encoding of v.
func appendData(b []byte, v *mms.Value, depth int) ([]byte, error) {
	if v == nil {
		return b, errValue("nil value")
	}
	if depth > DefaultLimits.MaxDepth {
		return b, errLimit("allData nesting deeper than %d", DefaultLimits.MaxDepth)
	}
	switch v.Type() {
	case mms.ValueTypeBoolean:
		x, _ := v.Bool()
		c := byte(0)
		if x {
			c = 0xff
		}
		return append(b, tagBoolean, 1, c), nil
	case mms.ValueTypeInteger:
		x, _ := v.Int64()
		return appendTLV(b, tagInteger, appendSigned(nil, x)), nil
	case mms.ValueTypeUnsigned:
		x, _ := v.Uint64()
		return appendTLV(b, tagUnsigned, appendUnsigned(nil, x)), nil
	case mms.ValueTypeFloat:
		f, _ := v.Float64()
		if f32 := float32(f); float64(f32) == f || math.IsNaN(f) {
			c := make([]byte, 5)
			c[0] = 8
			binary.BigEndian.PutUint32(c[1:], math.Float32bits(f32))
			return appendTLV(b, tagFloat, c), nil
		}
		c := make([]byte, 9)
		c[0] = 11
		binary.BigEndian.PutUint64(c[1:], math.Float64bits(f))
		return appendTLV(b, tagFloat, c), nil
	case mms.ValueTypeBitString:
		bits, _ := v.BitString()
		n, _ := v.BitStringLength()
		return appendBits(b, tagBitString, bits, n)
	case mms.ValueTypeBooleanArray:
		bits, n, _ := v.BooleanArray()
		return appendBits(b, tagBooleanArray, bits, n)
	case mms.ValueTypeOctetString:
		x, _ := v.OctetString()
		return appendTLV(b, tagOctetString, x), nil
	case mms.ValueTypeVisibleString:
		x, _ := v.VisibleString()
		return appendTLV(b, tagVisibleStr, []byte(x)), nil
	case mms.ValueTypeMmsString:
		x, _ := v.MmsString()
		return appendTLV(b, tagMMSString, []byte(x)), nil
	case mms.ValueTypeUTCTime:
		t, _ := v.UTCTime()
		return appendTLV(b, tagUTCTime, encodeUTCTime(t, v.UTCTimeQuality())), nil
	case mms.ValueTypeBinaryTime:
		ms, _ := v.BinaryTime()
		c, err := encodeBinaryTime(ms)
		if err != nil {
			return b, err
		}
		return appendTLV(b, tagBinaryTime, c), nil
	case mms.ValueTypeGeneralizedTime:
		t, _ := v.GeneralizedTime()
		return appendTLV(b, tagGenTime, []byte(t.UTC().Format("20060102150405.000Z"))), nil
	case mms.ValueTypeBCD:
		x, _ := v.BCD()
		if x < 0 {
			return b, errValue("negative BCD %d", x)
		}
		return appendTLV(b, tagBCD, appendSigned(nil, x)), nil
	case mms.ValueTypeObjectIdentifier:
		oid, _ := v.ObjectIdentifier()
		c, err := encodeOID(oid)
		if err != nil {
			return b, err
		}
		return appendTLV(b, tagObjID, c), nil
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
		for _, e := range elems {
			if inner, err = appendData(inner, e, depth+1); err != nil {
				return b, err
			}
		}
		return appendTLV(b, tag, inner), nil
	default:
		return b, errValue("type %v cannot be sent in GOOSE", v.Type())
	}
}

func appendBits(b []byte, tag byte, bits []byte, n int) ([]byte, error) {
	if n < 0 || n > len(bits)*8 {
		return b, errValue("bit length %d inconsistent with %d octets", n, len(bits))
	}
	nb := (n + 7) / 8
	c := make([]byte, 1+nb)
	c[0] = byte(nb*8 - n)
	copy(c[1:], bits[:nb])
	if nb > 0 && c[0] != 0 {
		c[nb] &^= byte(1<<c[0]) - 1 // clear unused trailing bits
	}
	return appendTLV(b, tag, c), nil
}

func encodeUTCTime(t time.Time, quality uint8) []byte {
	c := make([]byte, 8)
	binary.BigEndian.PutUint32(c[0:4], uint32(t.Unix()))
	// Round to nearest so that decode/encode is stable.
	frac := (uint64(t.Nanosecond())<<24 + 5e8) / 1e9
	if frac > 0xffffff {
		frac = 0xffffff
	}
	c[4], c[5], c[6] = byte(frac>>16), byte(frac>>8), byte(frac)
	c[7] = quality
	return c
}

func decodeUTCTime(c []byte) (time.Time, uint8) {
	secs := binary.BigEndian.Uint32(c[0:4])
	frac := uint64(c[4])<<16 | uint64(c[5])<<8 | uint64(c[6])
	ns := (frac*1e9 + (1 << 23)) >> 24 // round to nearest
	return time.Unix(int64(secs), int64(ns)).UTC(), c[7]
}

func encodeBinaryTime(ms int64) ([]byte, error) {
	minMs := epoch1984.UnixMilli()
	maxMs := epoch1984.AddDate(0, 0, 0x10000).UnixMilli() - 1
	if ms < minMs || ms > maxMs {
		return nil, errValue("binary time outside 1984-01-01..2163-06-05")
	}
	d := ms - minMs
	days := d / 86400000
	c := make([]byte, 6)
	binary.BigEndian.PutUint32(c[0:4], uint32(d%86400000))
	binary.BigEndian.PutUint16(c[4:6], uint16(days))
	return c, nil
}

func encodeOID(oid []int) ([]byte, error) {
	if len(oid) < 2 || oid[0] < 0 || oid[0] > 2 || oid[1] < 0 || (oid[0] < 2 && oid[1] > 39) {
		return nil, errValue("invalid object identifier %v", oid)
	}
	arcs := append([]int{oid[0]*40 + oid[1]}, oid[2:]...)
	var c []byte
	for _, a := range arcs {
		if a < 0 || a > math.MaxInt32 {
			return nil, errValue("object identifier arc %d out of range", a)
		}
		var tmp [5]byte
		i := len(tmp) - 1
		tmp[i] = byte(a & 0x7f)
		for a >>= 7; a > 0; a >>= 7 {
			i--
			tmp[i] = byte(a&0x7f) | 0x80
		}
		c = append(c, tmp[i:]...)
	}
	return c, nil
}

// dataDecoder decodes allData with limits.
type dataDecoder struct {
	lim      Limits
	elements int
}

// decodeSeq decodes a SEQUENCE OF Data from content (offset base).
func (d *dataDecoder) decodeSeq(content []byte, base, depth int) ([]*mms.Value, error) {
	if depth > d.lim.MaxDepth {
		return nil, decodeErr(ErrLimitExceeded, "allData", base, "nesting deeper than %d", d.lim.MaxDepth)
	}
	var out []*mms.Value
	pos := 0
	for pos < len(content) {
		e, err := readTLV(content, pos, base, "allData")
		if err != nil {
			return nil, asDataErr(err)
		}
		d.elements++
		if d.elements > d.lim.MaxElements {
			return nil, decodeErr(ErrLimitExceeded, "allData", base+pos, "more than %d data elements", d.lim.MaxElements)
		}
		v, err := d.decodeOne(e, base, depth)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		pos = e.next
	}
	return out, nil
}

func asDataErr(err error) error {
	if de, ok := err.(*DecodeError); ok && de.Err == ErrInvalidPDU {
		de.Err = ErrInvalidData
	}
	return err
}

func (d *dataDecoder) decodeOne(e tlv, base, depth int) (*mms.Value, error) {
	c := e.content
	off := base + e.off
	bad := func(format string, args ...any) error {
		return decodeErr(ErrInvalidData, "allData", off, format, args...)
	}
	cbase := base + e.next - len(c)
	switch e.tag {
	case tagStructure, tagArray:
		elems, err := d.decodeSeq(c, cbase, depth+1)
		if err != nil {
			return nil, err
		}
		if e.tag == tagArray {
			return mms.NewArray(elems), nil
		}
		return mms.NewStructure(elems), nil
	case tagBoolean:
		if len(c) != 1 {
			return nil, bad("boolean of %d octets", len(c))
		}
		return mms.NewBoolean(c[0] != 0), nil
	case tagInteger, tagBCD:
		if len(c) == 0 || len(c) > 8 {
			return nil, bad("integer of %d octets", len(c))
		}
		x := int64(int8(c[0]))
		for _, o := range c[1:] {
			x = x<<8 | int64(o)
		}
		if e.tag == tagBCD {
			if x < 0 {
				return nil, bad("negative BCD")
			}
			return mms.NewBCD(x), nil
		}
		return mms.NewInteger(x), nil
	case tagUnsigned:
		if len(c) == 0 || len(c) > 9 || (len(c) == 9 && c[0] != 0) {
			return nil, bad("unsigned of %d octets", len(c))
		}
		if c[0]&0x80 != 0 {
			return nil, bad("negative unsigned")
		}
		var x uint64
		for _, o := range c {
			x = x<<8 | uint64(o)
		}
		return mms.NewUnsigned(x), nil
	case tagFloat:
		switch {
		case len(c) == 5 && c[0] == 8:
			return mms.NewFloat(float64(math.Float32frombits(binary.BigEndian.Uint32(c[1:])))), nil
		case len(c) == 9 && c[0] == 11:
			return mms.NewFloat(math.Float64frombits(binary.BigEndian.Uint64(c[1:]))), nil
		default:
			return nil, bad("unsupported floating-point format (%d octets)", len(c))
		}
	case tagBitString, tagBooleanArray:
		if len(c) == 0 || c[0] > 7 || (len(c) == 1 && c[0] != 0) {
			return nil, bad("malformed bit string")
		}
		n := (len(c)-1)*8 - int(c[0])
		if e.tag == tagBooleanArray {
			return mms.NewBooleanArray(c[1:], n), nil
		}
		return mms.NewBitStringWithLength(c[1:], n), nil
	case tagOctetString:
		return mms.NewOctetString(c), nil
	case tagVisibleStr:
		for _, ch := range c {
			if ch < 0x20 || ch > 0x7e {
				return nil, bad("non-visible character 0x%02x in VisibleString", ch)
			}
		}
		return mms.NewVisibleString(string(c)), nil
	case tagMMSString:
		if !utf8.Valid(c) {
			return nil, bad("invalid UTF-8 in MMSString")
		}
		return mms.NewMmsString(string(c)), nil
	case tagUTCTime:
		if len(c) != 8 {
			return nil, bad("UtcTime of %d octets", len(c))
		}
		t, q := decodeUTCTime(c)
		return mms.NewUTCTimeWithQuality(t, q), nil
	case tagBinaryTime:
		switch len(c) {
		case 4:
			return mms.NewBinaryTime(epoch1984.UnixMilli() + int64(binary.BigEndian.Uint32(c))), nil
		case 6:
			ms := int64(binary.BigEndian.Uint32(c[0:4]))
			days := int64(binary.BigEndian.Uint16(c[4:6]))
			return mms.NewBinaryTime(epoch1984.UnixMilli() + days*86400000 + ms), nil
		default:
			return nil, bad("TimeOfDay of %d octets", len(c))
		}
	case tagGenTime:
		t, err := parseGeneralizedTime(string(c))
		if err != nil {
			return nil, bad("%v", err)
		}
		return mms.NewGeneralizedTime(t), nil
	case tagObjID:
		oid, err := decodeOID(c)
		if err != nil {
			return nil, bad("%v", err)
		}
		return mms.NewObjectIdentifier(oid), nil
	default:
		return nil, bad("unsupported Data tag 0x%02x", e.tag)
	}
}

func parseGeneralizedTime(s string) (time.Time, error) {
	layouts := []string{"20060102150405Z0700", "20060102150405.999999999Z0700", "20060102150405", "20060102150405.999999999"}
	if strings.HasSuffix(s, "Z") {
		s = strings.TrimSuffix(s, "Z") + "+0000"
	}
	var err error
	for _, l := range layouts {
		var t time.Time
		if t, err = time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, err
}

func decodeOID(c []byte) ([]int, error) {
	if len(c) == 0 {
		return nil, errValue("empty object identifier")
	}
	var arcs []int
	v := 0
	for i, o := range c {
		if v > math.MaxInt32>>7 {
			return nil, errValue("object identifier arc overflow")
		}
		v = v<<7 | int(o&0x7f)
		if o&0x80 == 0 {
			arcs = append(arcs, v)
			v = 0
		} else if i == len(c)-1 {
			return nil, errValue("truncated object identifier")
		}
	}
	first := arcs[0]
	var out []int
	switch {
	case first < 40:
		out = []int{0, first}
	case first < 80:
		out = []int{1, first - 40}
	default:
		out = []int{2, first - 80}
	}
	return append(out, arcs[1:]...), nil
}

// EncodeData returns the BER encoding of a single MMS Data value as used
// in GOOSE allData. Floats are sent as FLOAT32 when exactly
// representable and as FLOAT64 otherwise.
func EncodeData(v *mms.Value) ([]byte, error) { return appendData(nil, v, 1) }

// DecodeData decodes a single BER-encoded MMS Data value using
// [DefaultLimits]. Trailing bytes are rejected.
func DecodeData(b []byte) (*mms.Value, error) {
	d := dataDecoder{lim: DefaultLimits}
	vals, err := d.decodeSeq(b, 0, 1)
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, decodeErr(ErrInvalidData, "Data", 0, "expected exactly one element, got %d", len(vals))
	}
	return vals[0], nil
}
