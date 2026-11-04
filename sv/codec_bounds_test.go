// SPDX-License-Identifier: MIT

package sv

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// TestBERLengthForms checks every definite-length form (X.690 8.1.3)
// at its boundaries: short form below 0x80, long form with the minimal
// number of octets, and that readTLV and lengthSize agree with it.
func TestBERLengthForms(t *testing.T) {
	cases := []struct {
		n    int
		want []byte
	}{
		{0, []byte{0x00}},
		{0x7f, []byte{0x7f}},
		{0x80, []byte{0x81, 0x80}},
		{0xff, []byte{0x81, 0xff}},
		{0x100, []byte{0x82, 0x01, 0x00}},
		{0xffff, []byte{0x82, 0xff, 0xff}},
		{0x10000, []byte{0x83, 0x01, 0x00, 0x00}},
		{0xffffff, []byte{0x83, 0xff, 0xff, 0xff}},
		{0x1000000, []byte{0x84, 0x01, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		h := appendHdr(nil, 0x80, c.n)
		if !bytes.Equal(h[1:], c.want) {
			t.Errorf("length %#x: % x, want % x", c.n, h[1:], c.want)
		}
		if lengthSize(c.n) != len(c.want) {
			t.Errorf("lengthSize(%#x) = %d, want %d", c.n, lengthSize(c.n), len(c.want))
		}
		if c.n > 0x10000 {
			continue // keep the buffer small
		}
		buf := append(h, make([]byte, c.n)...)
		e, err := readTLV(buf, 0, 0, "x")
		if err != nil || len(e.content) != c.n || e.next != len(buf) {
			t.Errorf("readTLV %#x: %v len %d next %d", c.n, err, len(e.content), e.next)
		}
		if tlvLen(c.n) != len(buf) {
			t.Errorf("tlvLen(%#x) = %d, want %d", c.n, tlvLen(c.n), len(buf))
		}
	}
	// Five length octets and a length past the buffer are rejected.
	for _, b := range [][]byte{
		{0x80, 0x85, 0, 0, 0, 0, 1},
		{0x80, 0x84, 0xff, 0xff, 0xff, 0xff},
		{0x80, 0x82, 0x01},
		{0x80, 0x81},
	} {
		if _, err := readTLV(b, 0, 0, "x"); !errors.Is(err, ErrInvalidPDU) {
			t.Errorf("% x: %v", b, err)
		}
	}
}

// TestUnsignedIntegerWidths checks the minimal non-negative INTEGER
// encoding (X.690 8.3.2) used for noASDU at every octet boundary,
// including the leading zero needed when the top bit is set, and the
// decoder's 32-bit limit used for smpCnt-wide and confRev-wide fields.
func TestUnsignedIntegerWidths(t *testing.T) {
	cases := []struct {
		v    uint32
		want []byte
	}{
		{0, []byte{0}},
		{0x7f, []byte{0x7f}},
		{0x80, []byte{0, 0x80}},
		{0xff, []byte{0, 0xff}},
		{0x100, []byte{1, 0}},
		{0x7fff, []byte{0x7f, 0xff}},
		{0x8000, []byte{0, 0x80, 0}},
		{0xffff, []byte{0, 0xff, 0xff}},
		{0x800000, []byte{0, 0x80, 0, 0}},
		{0x7fffffff, []byte{0x7f, 0xff, 0xff, 0xff}},
		{0x80000000, []byte{0, 0x80, 0, 0, 0}},
		{math.MaxUint32, []byte{0, 0xff, 0xff, 0xff, 0xff}},
	}
	for _, c := range cases {
		got := unsignedBytes(nil, c.v)
		if !bytes.Equal(got, c.want) || unsignedLen(c.v) != len(c.want) {
			t.Errorf("%#x: % x (len %d), want % x", c.v, got, unsignedLen(c.v), c.want)
		}
		if d, err := decodeUint(got, "x", 0); err != nil || d != c.v {
			t.Errorf("decode %#x: %#x %v", c.v, d, err)
		}
	}
	for _, b := range [][]byte{nil, {0x80}, {1, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0}} {
		if _, err := decodeUint(b, "x", 0); !errors.Is(err, ErrInvalidPDU) {
			t.Errorf("% x: %v", b, err)
		}
	}
}

// TestSmpCntConfRevWidths checks the fixed-width smpCnt (2 octets) and
// confRev (4 octets) at their extremes, and that a wrong width on the
// wire is rejected.
func TestSmpCntConfRevWidths(t *testing.T) {
	f := fullFrame()
	f.PDU.ASDUs[0].SmpCnt, f.PDU.ASDUs[0].ConfRev = math.MaxUint16, math.MaxUint32
	f.PDU.ASDUs[1].SmpCnt, f.PDU.ASDUs[1].ConfRev = 0, 0
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a := g.PDU.ASDUs[0]; a.SmpCnt != math.MaxUint16 || a.ConfRev != math.MaxUint32 {
		t.Errorf("extremes %d %d", a.SmpCnt, a.ConfRev)
	}
	// Turn the 2-octet smpCnt of the second ASDU into 1 octet by
	// rewriting its length: the decoder must refuse it.
	i := bytes.Index(raw, []byte{tagSmpCnt, 2, 0, 0, tagConfRev, 4})
	if i < 0 {
		t.Fatal("smpCnt not found")
	}
	bad := bytes.Clone(raw)
	bad[i+1] = 1
	if _, err := Decode(bad); err == nil {
		t.Error("1-octet smpCnt accepted")
	}
	bad = bytes.Clone(raw)
	bad[i+5] = 3
	if _, err := Decode(bad); err == nil {
		t.Error("3-octet confRev accepted")
	}
}

// TestSmpCntWrapFits checks that the smpCnt reduction always fits the
// 16-bit field for every allowed wrap.
func TestSmpCntWrapFits(t *testing.T) {
	cases := []struct {
		x, w uint64
		want uint16
	}{
		{65535, 65536, 65535},
		{65536, 65536, 0},
		{65536 + 7, 65536, 7},
		{3999, 4000, 3999},
		{4000, 4000, 0},
		{math.MaxUint64, 65536, 65535},
		{12345, 1, 0},
	}
	for _, c := range cases {
		if got := nextSmpCnt(c.x, c.w); got != c.want {
			t.Errorf("nextSmpCnt(%d, %d) = %d, want %d", c.x, c.w, got, c.want)
		}
	}
}

// TestUTCTimeRange: UtcTime carries seconds since 1970 as 32 unsigned
// bits (IEC 61850-8-1 UtcTime, used by IEC 61850-9-2 RefrTm and
// TimeStamp). Instants outside 1970-01-01..2106-02-07 used to be
// written with wrapped seconds (1969-12-31T23:59:59 became 2106); they
// are now rejected. The zero time.Time encodes as the epoch, as in
// go-mms.
func TestUTCTimeRange(t *testing.T) {
	last := time.Unix(math.MaxUint32, 999_999_999).UTC()
	ok := []struct {
		in   time.Time
		want []byte
	}{
		{time.Time{}, []byte{0, 0, 0, 0, 0, 0, 0, 0x0a}},
		{time.Unix(0, 0), []byte{0, 0, 0, 0, 0, 0, 0, 0x0a}},
		{time.Unix(1, 500_000_000), []byte{0, 0, 0, 1, 0x80, 0, 0, 0x0a}},
		// The fraction saturates at 2^24-1 instead of carrying.
		{last, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x0a}},
	}
	for _, c := range ok {
		b, err := (Layout{KindTimestamp}).Encode(nil, []*mms.Value{mms.NewUTCTimeWithQuality(c.in, 0x0a)})
		if err != nil || !bytes.Equal(b, c.want) {
			t.Errorf("%v: % x %v, want % x", c.in, b, err, c.want)
		}
	}
	for _, in := range []time.Time{
		time.Unix(-1, 0),
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Unix(math.MaxUint32+1, 0),
		time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		_, err := (Layout{KindTimestamp}).Encode(nil, []*mms.Value{mms.NewUTCTimeWithQuality(in, 0)})
		if !errors.Is(err, ErrInvalidValue) {
			t.Errorf("TimeStamp %v: %v", in, err)
		}
		f := fullFrame()
		f.PDU.ASDUs[0].RefrTm = in
		dst := []byte{0xaa}
		out, err := AppendFrame(dst, f)
		if !errors.Is(err, ErrInvalidValue) || len(out) != 1 {
			t.Errorf("RefrTm %v: %v (%d octets)", in, err, len(out))
		}
		if _, err := EncodePDU(&f.PDU); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("EncodePDU RefrTm %v: %v", in, err)
		}
		// Without HasRefrTm the field is not sent and not checked.
		f.PDU.ASDUs[0].HasRefrTm = false
		if _, err := Encode(f); err != nil {
			t.Errorf("RefrTm %v without HasRefrTm: %v", in, err)
		}
	}
	// Decode covers the whole 32-bit range.
	if tt, q := decodeUTCTime([]byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 7}); tt.Unix() != math.MaxUint32 || q != 7 {
		t.Errorf("max decode %v %d", tt, q)
	}
}

// TestSignedWireEncoding pins the two's-complement big-endian wire
// form of the INTn sample types (IEC 61850-9-2 Ed2 "Encoding for the
// basic data types") at their extremes, both ways.
func TestSignedWireEncoding(t *testing.T) {
	cases := []struct {
		k    Kind
		v    int64
		want []byte
	}{
		{KindInt8, math.MinInt8, []byte{0x80}},
		{KindInt8, -1, []byte{0xff}},
		{KindInt8, math.MaxInt8, []byte{0x7f}},
		{KindInt16, math.MinInt16, []byte{0x80, 0}},
		{KindInt16, -2, []byte{0xff, 0xfe}},
		{KindInt16, math.MaxInt16, []byte{0x7f, 0xff}},
		{KindInt32, math.MinInt32, []byte{0x80, 0, 0, 0}},
		{KindInt32, -1, []byte{0xff, 0xff, 0xff, 0xff}},
		{KindInt32, math.MaxInt32, []byte{0x7f, 0xff, 0xff, 0xff}},
		{KindEnum, -3, []byte{0xff, 0xff, 0xff, 0xfd}},
		{KindInt64, math.MinInt64, []byte{0x80, 0, 0, 0, 0, 0, 0, 0}},
		{KindInt64, -1, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{KindInt64, math.MaxInt64, []byte{0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{KindInt64, -0x100000000, []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}},
	}
	for _, c := range cases {
		l := Layout{c.k}
		b, err := l.Encode(nil, []*mms.Value{mms.NewInteger(c.v)})
		if err != nil || !bytes.Equal(b, c.want) {
			t.Errorf("%v %d: % x %v, want % x", c.k, c.v, b, err, c.want)
			continue
		}
		out, err := l.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := out[0].Int64(); got != c.v {
			t.Errorf("%v % x: decoded %d, want %d", c.k, b, got, c.v)
		}
	}
	// Unsigned extremes, INT24U included.
	u := []struct {
		k    Kind
		v    uint64
		want []byte
	}{
		{KindUint8, math.MaxUint8, []byte{0xff}},
		{KindUint16, math.MaxUint16, []byte{0xff, 0xff}},
		{KindUint24, 0xffffff, []byte{0xff, 0xff, 0xff}},
		{KindUint24, 0x010203, []byte{1, 2, 3}},
		{KindUint32, math.MaxUint32, []byte{0xff, 0xff, 0xff, 0xff}},
	}
	for _, c := range u {
		b, err := (Layout{c.k}).Encode(nil, []*mms.Value{mms.NewUnsigned(c.v)})
		if err != nil || !bytes.Equal(b, c.want) {
			t.Errorf("%v %d: % x %v", c.k, c.v, b, err)
		}
	}
	if v := Int32At([]byte{0x80, 0, 0, 0}, 0); v != math.MinInt32 {
		t.Errorf("Int32At %d", v)
	}
}

// TestLEValueAndQualityWire pins the 9-2LE / IEC 61869-9 sample: each
// measurand an INT32 count followed by a 32-bit quality word, all
// big-endian, at the INT32 extremes and with every defined quality bit.
func TestLEValueAndQualityWire(t *testing.T) {
	var s LESample
	allQ := QualityQuestionable | QualityOverflow | QualityOutOfRange | QualityBadReference | QualityOscillatory |
		QualityFailure | QualityOldData | QualityInconsistent | QualityInaccurate | QualitySubstituted |
		QualityTest | QualityOperatorBlocked | QualityDerived
	s.Current[0] = Point{Value: math.MinInt32, Q: QualityInvalid}
	s.Current[1] = Point{Value: math.MaxInt32, Q: allQ}
	s.Current[2] = Point{Value: -1, Q: QualityGood}
	s.Voltage[3] = Point{Value: -2, Q: QualityTest}
	b := s.Append(nil)
	if len(b) != LESampleSize {
		t.Fatalf("size %d", len(b))
	}
	want := map[int][]byte{
		0:  {0x80, 0, 0, 0, 0, 0, 0, 0x02},
		8:  {0x7f, 0xff, 0xff, 0xff, 0, 0, 0x3f, 0xff},
		16: {0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0},
		56: {0xff, 0xff, 0xff, 0xfe, 0, 0, 0x08, 0},
	}
	for off, w := range want {
		if !bytes.Equal(b[off:off+8], w) {
			t.Errorf("offset %d: % x, want % x", off, b[off:off+8], w)
		}
	}
	var r LESample
	if err := DecodeLE(b, &r); err != nil || r != s {
		t.Errorf("round trip %+v %v", r, err)
	}
	if QualityAt(b, 4) != QualityInvalid || QualityAt(b, 12) != allQ {
		t.Errorf("quality %v %v", QualityAt(b, 4), QualityAt(b, 12))
	}
	// Reserved bits 14-31 are carried, not interpreted, by the
	// zero-allocation accessors.
	b[28], b[29] = 0xff, 0xc0
	if q := QualityAt(b, 28); q != 0xffc00000 {
		t.Errorf("reserved bits %#x", uint32(q))
	}
	for _, n := range []int{0, LESampleSize - 1, LESampleSize + 1} {
		if err := DecodeLE(make([]byte, n), &r); !errors.Is(err, ErrInvalidData) {
			t.Errorf("DecodeLE(%d octets): %v", n, err)
		}
	}
}

// TestDecodeTruncated feeds every prefix of a valid frame: none may
// panic, and every prefix that cuts into the savPdu is rejected.
func TestDecodeTruncated(t *testing.T) {
	f := fullFrame()
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	var reuse Frame
	for n := range len(raw) {
		_, err := Decode(raw[:n])
		err2 := DecodeInto(&reuse, raw[:n], DefaultLimits)
		if err == nil || err2 == nil {
			t.Errorf("prefix of %d/%d octets accepted (%v, %v)", n, len(raw), err, err2)
		}
	}
}
