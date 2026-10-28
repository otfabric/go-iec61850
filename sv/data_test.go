// SPDX-License-Identifier: MIT

package sv

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func TestKinds(t *testing.T) {
	sizes := map[Kind]int{
		KindBoolean: 1, KindInt8: 1, KindInt16: 2, KindInt32: 4, KindInt64: 8, KindUint8: 1, KindUint16: 2,
		KindUint24: 3, KindUint32: 4, KindFloat32: 4, KindFloat64: 8, KindEnum: 4, KindDbpos: 4, KindQuality: 4, KindTimestamp: 8,
	}
	for k, n := range sizes {
		if k.Size() != n || k.String() == "" {
			t.Errorf("%v: size %d", k, k.Size())
		}
	}
	if Kind(200).Size() != 0 || Kind(200).String() != "Kind(200)" {
		t.Error("invalid kind")
	}
	for _, bt := range []string{"BOOLEAN", "INT8", "INT16", "INT32", "INT64", "INT8U", "INT16U", "INT24U", "INT32U", "FLOAT32", "FLOAT64", "Enum", "ENUM", "Dbpos", "Tcmd", "Quality", "Timestamp"} {
		if k, ok := KindOf(bType(bt)); !ok || k == KindInvalid {
			t.Errorf("KindOf(%s)", bt)
		}
	}
	if _, ok := KindOf("VisString255"); ok {
		t.Error("strings cannot be sampled")
	}
	l := Layout{KindInt32, KindQuality, KindFloat64}
	if l.Size() != 16 || l.String() != "INT32,Quality,FLOAT64" {
		t.Errorf("layout %d %s", l.Size(), l)
	}
	if offs := l.Offsets(); offs[0] != 0 || offs[1] != 4 || offs[2] != 8 {
		t.Errorf("offsets %v", offs)
	}
	if err := (Layout{KindInt8, KindInvalid}).Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("Validate: %v", err)
	}
	if err := (Layout{Kind(99)}).Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("Validate: %v", err)
	}
}

func bType(s string) string { return s }

func TestLayoutRoundTrip(t *testing.T) {
	ts := time.Date(2026, 10, 28, 1, 2, 3, 250_000_000, time.UTC)
	l := Layout{KindBoolean, KindInt8, KindInt16, KindInt32, KindInt64, KindUint8, KindUint16, KindUint24, KindUint32,
		KindFloat32, KindFloat64, KindEnum, KindDbpos, KindQuality, KindTimestamp}
	in := []*mms.Value{
		mms.NewBoolean(true), mms.NewInteger(-5), mms.NewInteger(-300), mms.NewInteger(-70000), mms.NewInteger(math.MinInt64),
		mms.NewUnsigned(200), mms.NewUnsigned(60000), mms.NewUnsigned(0xabcdef), mms.NewUnsigned(math.MaxUint32),
		mms.NewFloat(1.5), mms.NewFloat(-2.25), mms.NewInteger(3), mms.NewBitStringWithLength([]byte{0x80}, 2),
		(QualityInvalid | QualityTest).Value(), mms.NewUTCTimeWithQuality(ts, 0x0a),
	}
	b, err := l.Encode(nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != l.Size() {
		t.Fatalf("size %d, want %d", len(b), l.Size())
	}
	out, err := l.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if !in[i].Equal(out[i]) {
			if tin, ok := in[i].UTCTime(); ok {
				if tout, _ := out[i].UTCTime(); tin.Equal(tout) {
					continue
				}
			}
			t.Errorf("leaf %d (%v): %v != %v", i, l[i], out[i], in[i])
		}
	}
	// Cross-type acceptance: Unsigned for INT, non-negative Integer for
	// INT*U, integer Dbpos, integer Quality.
	alt := []*mms.Value{mms.NewUnsigned(7), mms.NewInteger(9), mms.NewInteger(2), mms.NewUnsigned(uint64(QualityTest))}
	b, err = (Layout{KindInt32, KindUint16, KindDbpos, KindQuality}).Encode(nil, alt)
	if err != nil {
		t.Fatal(err)
	}
	if Int32At(b, 0) != 7 || QualityAt(b, 10) != QualityTest {
		t.Errorf("alt %x", b)
	}
	if v, _ := (Layout{KindBoolean}).Decode([]byte{0}); v[0].String() == "" {
		t.Error("false")
	}
	if _, err := (Layout{KindBoolean}).Encode(nil, []*mms.Value{mms.NewBoolean(false)}); err != nil {
		t.Error(err)
	}
	if f := Float32At([]byte{0x3f, 0xc0, 0, 0}, 0); f != 1.5 {
		t.Errorf("Float32At %v", f)
	}
	b, _ = (Layout{KindTimestamp}).Encode(nil, []*mms.Value{mms.NewUTCTimeWithQuality(ts, 3)})
	if tt, q := TimestampAt(b, 0); !tt.Equal(ts) || q != 3 {
		t.Errorf("TimestampAt %v %d", tt, q)
	}
}

func TestLayoutEncodeErrors(t *testing.T) {
	cases := []struct {
		k Kind
		v *mms.Value
	}{
		{KindBoolean, mms.NewInteger(1)},
		{KindBoolean, nil},
		{KindInt8, mms.NewInteger(200)},
		{KindInt16, mms.NewInteger(1 << 20)},
		{KindInt32, mms.NewInteger(1 << 40)},
		{KindInt32, mms.NewVisibleString("x")},
		{KindInt64, mms.NewUnsigned(math.MaxUint64)},
		{KindUint8, mms.NewUnsigned(300)},
		{KindUint16, mms.NewUnsigned(1 << 20)},
		{KindUint24, mms.NewUnsigned(1 << 25)},
		{KindUint32, mms.NewUnsigned(1 << 40)},
		{KindUint32, mms.NewInteger(-1)},
		{KindFloat32, mms.NewBoolean(true)},
		{KindFloat64, mms.NewBoolean(true)},
		{KindDbpos, mms.NewBitStringWithLength([]byte{0}, 3)},
		{KindDbpos, mms.NewInteger(4)},
		{KindQuality, mms.NewFloat(1)},
		{KindQuality, nil},
		{KindTimestamp, mms.NewInteger(1)},
		{Kind(77), mms.NewInteger(1)},
	}
	for _, c := range cases {
		if _, err := (Layout{c.k}).Encode(nil, []*mms.Value{c.v}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("%v %v: %v", c.k, c.v, err)
		}
	}
	if _, err := (Layout{KindInt8}).Encode(nil, nil); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("count: %v", err)
	}
	if _, err := (Layout{KindInt8}).Decode([]byte{1, 2}); !errors.Is(err, ErrInvalidData) {
		t.Errorf("size: %v", err)
	}
	if _, err := (Layout{KindDbpos}).Decode([]byte{0, 0, 0, 9}); !errors.Is(err, ErrInvalidData) {
		t.Errorf("dbpos: %v", err)
	}
	if _, err := (Layout{Kind(77)}).Decode(nil); !errors.Is(err, ErrInvalidData) {
		t.Errorf("kind: %v", err)
	}
}

func TestQuality(t *testing.T) {
	q := QualityQuestionable | QualityOldData | QualitySubstituted | QualityDerived
	if q.Validity() != QualityQuestionable || q.IsGood() || !QualityGood.IsGood() {
		t.Error("validity")
	}
	v := q.Value()
	bits, _ := v.BitString()
	// IEC bit 0 is the MSB of the first octet: questionable = bits 0 and 1.
	if n, _ := v.BitStringLength(); n != 14 || bits[0]&0xc0 != 0xc0 || bits[0]&0x01 == 0 || bits[1]&0x20 == 0 || bits[1]&0x04 == 0 {
		t.Errorf("bits %08b %08b", bits[0], bits[1])
	}
	back, err := QualityFromValue(v)
	if err != nil || back != q {
		t.Errorf("round trip %x %v", back, err)
	}
	// Invalid is bit 1 (integer 2), the 9-2LE/IEC mapping.
	if iv, _ := QualityFromValue(mms.NewBitStringWithLength([]byte{0x40, 0}, 13)); iv != QualityInvalid || uint32(iv) != 2 {
		t.Errorf("invalid = %d", iv)
	}
	if x, err := QualityFromValue(mms.NewInteger(5)); err != nil || x != 5 {
		t.Errorf("int %v %v", x, err)
	}
	if _, err := QualityFromValue(mms.NewInteger(-1)); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("negative: %v", err)
	}
}

func TestLE(t *testing.T) {
	var s LESample
	s.Current[0] = Point{Value: AmpsCount(1.234), Q: QualityGood}
	s.Current[3] = Point{Value: -1, Q: QualityInvalid}
	s.Voltage[1] = Point{Value: VoltsCount(-63509.1), Q: QualityTest}
	b := s.Append(nil)
	if len(b) != LESampleSize || LELayout.Size() != LESampleSize {
		t.Fatalf("size %d", len(b))
	}
	var r LESample
	if err := DecodeLE(b, &r); err != nil || r != s {
		t.Fatalf("decode %v %+v", err, r)
	}
	if Amps(r.Current[0].Value) != 1.234 || math.Abs(Volts(r.Voltage[1].Value)+63509.1) > 0.006 {
		t.Errorf("scaling %v %v", Amps(r.Current[0].Value), Volts(r.Voltage[1].Value))
	}
	if AmpsCount(1e12) != math.MaxInt32 || AmpsCount(-1e12) != math.MinInt32 || AmpsCount(math.NaN()) != 0 {
		t.Error("saturation")
	}
	vals, err := LELayout.Decode(b)
	if err != nil || len(vals) != 16 {
		t.Fatal(err)
	}
	if err := DecodeLE(b[:10], &r); !errors.Is(err, ErrInvalidData) {
		t.Errorf("short: %v", err)
	}
	allocs := testing.AllocsPerRun(100, func() { _ = DecodeLE(b, &r) })
	if allocs != 0 {
		t.Errorf("DecodeLE allocates %.0f", allocs)
	}
}
