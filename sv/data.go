// SPDX-License-Identifier: MIT

package sv

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/otfabric/go-mms"
)

// Kind is the type of one dataset leaf in the IEC 61850-9-2 sample
// encoding (Ed2 table "Encoding for the basic data types"). Every kind
// has a fixed size; values are big-endian.
type Kind uint8

// Sample encodings.
const (
	KindInvalid   Kind = iota
	KindBoolean        // BOOLEAN, 1 octet (0 = false)
	KindInt8           // INT8
	KindInt16          // INT16
	KindInt32          // INT32
	KindInt64          // INT64
	KindUint8          // INT8U
	KindUint16         // INT16U
	KindUint24         // INT24U
	KindUint32         // INT32U
	KindFloat32        // FLOAT32 (IEEE 754 single)
	KindFloat64        // FLOAT64 (IEEE 754 double)
	KindEnum           // ENUMERATED, 32 bits
	KindDbpos          // CODED ENUM (Dbpos), 32 bits holding 0..3
	KindQuality        // Quality, 32-bit bit string (see [Quality])
	KindTimestamp      // TimeStamp, 8-octet UtcTime
	kindCount
)

var kindInfo = [kindCount]struct {
	name string
	size int
}{
	{"invalid", 0}, {"BOOLEAN", 1}, {"INT8", 1}, {"INT16", 2}, {"INT32", 4}, {"INT64", 8},
	{"INT8U", 1}, {"INT16U", 2}, {"INT24U", 3}, {"INT32U", 4}, {"FLOAT32", 4}, {"FLOAT64", 8},
	{"ENUM", 4}, {"Dbpos", 4}, {"Quality", 4}, {"Timestamp", 8},
}

// Size returns the encoded size of k in octets (0 for an invalid kind).
func (k Kind) Size() int {
	if k >= kindCount {
		return 0
	}
	return kindInfo[k].size
}

// String returns the IEC 61850 basic type name.
func (k Kind) String() string {
	if k >= kindCount {
		return fmt.Sprintf("Kind(%d)", uint8(k))
	}
	return kindInfo[k].name
}

// KindOf maps an SCL bType (and, for ENUM, nothing else) to its sample
// encoding. ok is false for types that cannot be sampled.
func KindOf(bType string) (k Kind, ok bool) {
	switch bType {
	case "BOOLEAN":
		return KindBoolean, true
	case "INT8":
		return KindInt8, true
	case "INT16":
		return KindInt16, true
	case "INT32":
		return KindInt32, true
	case "INT64":
		return KindInt64, true
	case "INT8U":
		return KindUint8, true
	case "INT16U":
		return KindUint16, true
	case "INT24U":
		return KindUint24, true
	case "INT32U":
		return KindUint32, true
	case "FLOAT32":
		return KindFloat32, true
	case "FLOAT64":
		return KindFloat64, true
	case "Enum", "ENUM":
		return KindEnum, true
	case "Dbpos", "Tcmd":
		return KindDbpos, true
	case "Quality":
		return KindQuality, true
	case "Timestamp":
		return KindTimestamp, true
	}
	return KindInvalid, false
}

// Layout is the flattened list of dataset leaves of a sample, in
// transmission order.
type Layout []Kind

// Size returns the sample size of l in octets.
func (l Layout) Size() int {
	n := 0
	for _, k := range l {
		n += k.Size()
	}
	return n
}

// Offsets returns the byte offset of every leaf.
func (l Layout) Offsets() []int {
	out := make([]int, len(l))
	n := 0
	for i, k := range l {
		out[i] = n
		n += k.Size()
	}
	return out
}

// String formats the layout as a comma-separated list of type names.
func (l Layout) String() string {
	parts := make([]string, len(l))
	for i, k := range l {
		parts[i] = k.String()
	}
	return strings.Join(parts, ",")
}

// Validate reports an invalid kind.
func (l Layout) Validate() error {
	for i, k := range l {
		if k == KindInvalid || k >= kindCount {
			return configErr("Layout", "leaf %d: invalid kind %d", i, uint8(k))
		}
	}
	return nil
}

// Encode appends the sample encoding of values (one per leaf) to dst.
// Values use the MMS mapping of IEC 61850-8-1: Boolean, Integer
// (INT*, ENUM), Unsigned (INT*U), Float, a 2-bit BitString (Dbpos), a
// 13-bit BitString (Quality) and UTCTime (Timestamp).
func (l Layout) Encode(dst []byte, values []*mms.Value) ([]byte, error) {
	if len(values) != len(l) {
		return dst, errValue("%d values for %d dataset leaves", len(values), len(l))
	}
	for i, k := range l {
		var err error
		if dst, err = appendValue(dst, k, values[i]); err != nil {
			return dst, fmt.Errorf("leaf %d (%s): %w", i, k, err)
		}
	}
	return dst, nil
}

// Decode decodes a sample into one MMS value per leaf. It fails with
// [ErrInvalidData] when the sample size does not match the layout.
func (l Layout) Decode(sample []byte) ([]*mms.Value, error) {
	if len(sample) != l.Size() {
		return nil, fmt.Errorf("%w: sample of %d octets, layout needs %d", ErrInvalidData, len(sample), l.Size())
	}
	out := make([]*mms.Value, len(l))
	off := 0
	for i, k := range l {
		c := sample[off : off+k.Size()]
		off += k.Size()
		v, err := decodeValue(k, c)
		if err != nil {
			return nil, fmt.Errorf("%w: leaf %d (%s): %w", ErrInvalidData, i, k, err)
		}
		out[i] = v
	}
	return out, nil
}

func appendValue(b []byte, k Kind, v *mms.Value) ([]byte, error) {
	if v == nil {
		return b, errValue("nil value")
	}
	switch k {
	case KindBoolean:
		x, ok := v.Bool()
		if !ok {
			return b, errValue("want Boolean, got %v", v.Type())
		}
		if x {
			return append(b, 1), nil
		}
		return append(b, 0), nil
	case KindInt8, KindInt16, KindInt32, KindInt64, KindEnum:
		x, ok := v.Int64()
		if !ok {
			if u, uok := v.Uint64(); uok && u <= math.MaxInt64 {
				x, ok = int64(u), true
			}
		}
		if !ok {
			return b, errValue("want Integer, got %v", v.Type())
		}
		return appendInt(b, k, x)
	case KindUint8, KindUint16, KindUint24, KindUint32:
		x, ok := v.Uint64()
		if !ok {
			if i, iok := v.Int64(); iok && i >= 0 {
				x, ok = uint64(i), true
			}
		}
		if !ok {
			return b, errValue("want Unsigned, got %v", v.Type())
		}
		return appendUint(b, k, x)
	case KindFloat32:
		x, ok := v.Float64()
		if !ok {
			return b, errValue("want Float, got %v", v.Type())
		}
		return binary.BigEndian.AppendUint32(b, math.Float32bits(float32(x))), nil
	case KindFloat64:
		x, ok := v.Float64()
		if !ok {
			return b, errValue("want Float, got %v", v.Type())
		}
		return binary.BigEndian.AppendUint64(b, math.Float64bits(x)), nil
	case KindDbpos:
		bits, ok := v.BitString()
		if n, _ := v.BitStringLength(); !ok || n != 2 || len(bits) == 0 {
			if i, iok := v.Int64(); iok && i >= 0 && i <= 3 {
				return binary.BigEndian.AppendUint32(b, uint32(i)), nil
			}
			return b, errValue("want a 2-bit BitString, got %v", v)
		}
		return binary.BigEndian.AppendUint32(b, uint32(bits[0]>>6)), nil
	case KindQuality:
		q, err := QualityFromValue(v)
		if err != nil {
			return b, err
		}
		return binary.BigEndian.AppendUint32(b, uint32(q)), nil
	case KindTimestamp:
		t, ok := v.UTCTime()
		if !ok {
			return b, errValue("want UTCTime, got %v", v.Type())
		}
		return appendUTCTime(b, t, v.UTCTimeQuality()), nil
	default:
		return b, errValue("invalid kind %d", uint8(k))
	}
}

func appendInt(b []byte, k Kind, x int64) ([]byte, error) {
	switch k {
	case KindInt8:
		if x < math.MinInt8 || x > math.MaxInt8 {
			return b, errValue("%d out of INT8 range", x)
		}
		return append(b, byte(int8(x))), nil
	case KindInt16:
		if x < math.MinInt16 || x > math.MaxInt16 {
			return b, errValue("%d out of INT16 range", x)
		}
		return binary.BigEndian.AppendUint16(b, uint16(int16(x))), nil
	case KindInt64:
		return binary.BigEndian.AppendUint64(b, uint64(x)), nil
	default: // INT32, ENUM
		if x < math.MinInt32 || x > math.MaxInt32 {
			return b, errValue("%d out of 32-bit range", x)
		}
		return binary.BigEndian.AppendUint32(b, uint32(int32(x))), nil
	}
}

func appendUint(b []byte, k Kind, x uint64) ([]byte, error) {
	switch k {
	case KindUint8:
		if x > math.MaxUint8 {
			return b, errValue("%d out of INT8U range", x)
		}
		return append(b, byte(x)), nil
	case KindUint16:
		if x > math.MaxUint16 {
			return b, errValue("%d out of INT16U range", x)
		}
		return binary.BigEndian.AppendUint16(b, uint16(x)), nil
	case KindUint24:
		if x > 0xffffff {
			return b, errValue("%d out of INT24U range", x)
		}
		return append(b, byte(x>>16), byte(x>>8), byte(x)), nil
	default:
		if x > math.MaxUint32 {
			return b, errValue("%d out of INT32U range", x)
		}
		return binary.BigEndian.AppendUint32(b, uint32(x)), nil
	}
}

func decodeValue(k Kind, c []byte) (*mms.Value, error) {
	switch k {
	case KindBoolean:
		return mms.NewBoolean(c[0] != 0), nil
	case KindInt8:
		return mms.NewInteger(int64(int8(c[0]))), nil
	case KindInt16:
		return mms.NewInteger(int64(int16(binary.BigEndian.Uint16(c)))), nil
	case KindInt32, KindEnum:
		return mms.NewInteger(int64(int32(binary.BigEndian.Uint32(c)))), nil
	case KindInt64:
		return mms.NewInteger(int64(binary.BigEndian.Uint64(c))), nil
	case KindUint8:
		return mms.NewUnsigned(uint64(c[0])), nil
	case KindUint16:
		return mms.NewUnsigned(uint64(binary.BigEndian.Uint16(c))), nil
	case KindUint24:
		return mms.NewUnsigned(uint64(c[0])<<16 | uint64(c[1])<<8 | uint64(c[2])), nil
	case KindUint32:
		return mms.NewUnsigned(uint64(binary.BigEndian.Uint32(c))), nil
	case KindFloat32:
		return mms.NewFloat(float64(math.Float32frombits(binary.BigEndian.Uint32(c)))), nil
	case KindFloat64:
		return mms.NewFloat(math.Float64frombits(binary.BigEndian.Uint64(c))), nil
	case KindDbpos:
		x := binary.BigEndian.Uint32(c)
		if x > 3 {
			return nil, fmt.Errorf("dbpos value %d", x)
		}
		return mms.NewBitStringWithLength([]byte{byte(x << 6)}, 2), nil
	case KindQuality:
		return Quality(binary.BigEndian.Uint32(c)).Value(), nil
	case KindTimestamp:
		t, q := decodeUTCTime(c)
		return mms.NewUTCTimeWithQuality(t, q), nil
	default:
		return nil, fmt.Errorf("invalid kind %d", uint8(k))
	}
}

// Quality is the IEC 61850 quality in the 32-bit SV encoding: bit i of
// the integer is bit i of the IEC 61850-7-3 quality (validity in bits
// 0-1: 00 good, 01 invalid, 11 questionable as written bit0 bit1, i.e.
// the integer values 0, 2 and 3), the detail-quality bits 2-9, source
// (bit 10), test (bit 11), operatorBlocked (bit 12) and derived (bit
// 13); bits 14-31 are reserved and zero.
type Quality uint32

// Quality bits.
const (
	QualityGood            Quality = 0
	QualityInvalid         Quality = 1 << 1
	QualityReserved        Quality = 1 << 0
	QualityQuestionable    Quality = QualityInvalid | QualityReserved
	QualityValidityMask    Quality = 0x3
	QualityOverflow        Quality = 1 << 2
	QualityOutOfRange      Quality = 1 << 3
	QualityBadReference    Quality = 1 << 4
	QualityOscillatory     Quality = 1 << 5
	QualityFailure         Quality = 1 << 6
	QualityOldData         Quality = 1 << 7
	QualityInconsistent    Quality = 1 << 8
	QualityInaccurate      Quality = 1 << 9
	QualitySubstituted     Quality = 1 << 10
	QualityTest            Quality = 1 << 11
	QualityOperatorBlocked Quality = 1 << 12
	QualityDerived         Quality = 1 << 13
)

// Validity returns the validity bits (QualityGood, QualityInvalid,
// QualityReserved or QualityQuestionable).
func (q Quality) Validity() Quality { return q & QualityValidityMask }

// IsGood reports whether the validity is good.
func (q Quality) IsGood() bool { return q.Validity() == QualityGood }

// Value returns q as the MMS quality BitString of IEC 61850-8-1 (bit 0
// first, i.e. in the most significant bit of the first octet): 13 bits,
// or 14 when the derived bit is set.
func (q Quality) Value() *mms.Value {
	n := 13
	if q&QualityDerived != 0 {
		n = 14
	}
	var bits [2]byte
	for i := 0; i < n; i++ {
		if q&(1<<i) != 0 {
			bits[i/8] |= 0x80 >> (i % 8)
		}
	}
	return mms.NewBitStringWithLength(bits[:], n)
}

// QualityFromValue converts an MMS quality BitString (13 or 14 bits) or
// an integer to a [Quality].
func QualityFromValue(v *mms.Value) (Quality, error) {
	if v == nil {
		return 0, errValue("nil quality")
	}
	if bits, ok := v.BitString(); ok {
		n, _ := v.BitStringLength()
		var q Quality
		for i := 0; i < n && i < 32 && i/8 < len(bits); i++ {
			if bits[i/8]&(0x80>>(i%8)) != 0 {
				q |= 1 << i
			}
		}
		return q, nil
	}
	if u, ok := v.Uint64(); ok && u <= math.MaxUint32 {
		return Quality(u), nil
	}
	if i, ok := v.Int64(); ok && i >= 0 && i <= math.MaxUint32 {
		return Quality(i), nil
	}
	return 0, errValue("want a quality BitString, got %v", v.Type())
}

// 9-2LE dataset (UCA implementation guideline): four currents (IA, IB,
// IC, IN) then four voltages (VA, VB, VC, VN), each an INT32 instMag.i
// followed by its Quality — 64 octets.
const (
	// LESampleSize is the size of the 9-2LE sample.
	LESampleSize = 64

	// LECurrentScale is the 9-2LE current resolution: 1 mA per count.
	LECurrentScale = 0.001

	// LEVoltageScale is the 9-2LE voltage resolution: 10 mV per count.
	LEVoltageScale = 0.01

	// LESmpRateProtection and LESmpRateMetering are the 9-2LE sample
	// rates in samples per nominal period (noASDU 1 and 8).
	LESmpRateProtection = 80
	LESmpRateMetering   = 256
)

// LELayout is the [Layout] of the 9-2LE dataset.
var LELayout = Layout{
	KindInt32, KindQuality, KindInt32, KindQuality, KindInt32, KindQuality, KindInt32, KindQuality,
	KindInt32, KindQuality, KindInt32, KindQuality, KindInt32, KindQuality, KindInt32, KindQuality,
}

// Point is one measured value with its quality.
type Point struct {
	Value int32
	Q     Quality
}

// LESample is the 9-2LE dataset: Current is IA, IB, IC, IN in mA and
// Voltage is VA, VB, VC, VN in units of 10 mV.
type LESample struct {
	Current [4]Point
	Voltage [4]Point
}

// Append appends the 64-octet encoding of s to dst.
func (s *LESample) Append(dst []byte) []byte {
	for _, p := range s.Current {
		dst = binary.BigEndian.AppendUint32(dst, uint32(p.Value))
		dst = binary.BigEndian.AppendUint32(dst, uint32(p.Q))
	}
	for _, p := range s.Voltage {
		dst = binary.BigEndian.AppendUint32(dst, uint32(p.Value))
		dst = binary.BigEndian.AppendUint32(dst, uint32(p.Q))
	}
	return dst
}

// DecodeLE decodes a 9-2LE sample without allocating.
func DecodeLE(sample []byte, s *LESample) error {
	if len(sample) != LESampleSize {
		return fmt.Errorf("%w: 9-2LE sample of %d octets, want %d", ErrInvalidData, len(sample), LESampleSize)
	}
	for i := 0; i < 8; i++ {
		p := Point{
			Value: int32(binary.BigEndian.Uint32(sample[i*8:])),
			Q:     Quality(binary.BigEndian.Uint32(sample[i*8+4:])),
		}
		if i < 4 {
			s.Current[i] = p
		} else {
			s.Voltage[i-4] = p
		}
	}
	return nil
}

// Amps converts a 9-2LE current count to amperes.
func Amps(count int32) float64 { return float64(count) * LECurrentScale }

// Volts converts a 9-2LE voltage count to volts.
func Volts(count int32) float64 { return float64(count) * LEVoltageScale }

// AmpsCount converts amperes to a 9-2LE current count, rounding to the
// nearest count and saturating at the INT32 range.
func AmpsCount(a float64) int32 { return saturate(a / LECurrentScale) }

// VoltsCount converts volts to a 9-2LE voltage count, rounding to the
// nearest count and saturating at the INT32 range.
func VoltsCount(v float64) int32 { return saturate(v / LEVoltageScale) }

func saturate(x float64) int32 {
	x = math.Round(x)
	switch {
	case math.IsNaN(x):
		return 0
	case x > math.MaxInt32:
		return math.MaxInt32
	case x < math.MinInt32:
		return math.MinInt32
	}
	return int32(x)
}

// Int32At returns the INT32 at byte offset off of a sample, for
// zero-allocation access with [Layout.Offsets].
func Int32At(sample []byte, off int) int32 {
	return int32(binary.BigEndian.Uint32(sample[off:]))
}

// QualityAt returns the Quality at byte offset off of a sample.
func QualityAt(sample []byte, off int) Quality {
	return Quality(binary.BigEndian.Uint32(sample[off:]))
}

// Float32At returns the FLOAT32 at byte offset off of a sample.
func Float32At(sample []byte, off int) float32 {
	return math.Float32frombits(binary.BigEndian.Uint32(sample[off:]))
}

// TimestampAt returns the TimeStamp at byte offset off of a sample.
func TimestampAt(sample []byte, off int) (time.Time, uint8) {
	return decodeUTCTime(sample[off : off+8])
}
