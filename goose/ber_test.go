// SPDX-License-Identifier: MIT

package goose

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func TestAppendLengthForms(t *testing.T) {
	for _, tc := range []struct {
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
		{0x7fffffff, []byte{0x84, 0x7f, 0xff, 0xff, 0xff}},
		{-1, []byte{0x00}}, // never produced by the encoder; clamped, not wrapped
	} {
		got := appendLength(nil, tc.n)
		if !bytes.Equal(got, tc.want) {
			t.Errorf("appendLength(%#x) = % x, want % x", tc.n, got, tc.want)
		}
		if tc.n >= 0 && lengthSize(tc.n) != len(got) {
			t.Errorf("lengthSize(%#x) = %d, appendLength emitted %d", tc.n, lengthSize(tc.n), len(got))
		}
		// Every emitted form decodes back through readTLV.
		if tc.n >= 0 && tc.n <= 0x10000 {
			buf := append(append([]byte{0x04}, got...), make([]byte, tc.n)...)
			e, err := readTLV(buf, 0, 0, "x")
			if err != nil || len(e.content) != tc.n || e.next != len(buf) {
				t.Errorf("readTLV round trip of %#x: %v len=%d", tc.n, err, len(e.content))
			}
		}
	}
}

func TestAppendSignedWidths(t *testing.T) {
	for _, tc := range []struct {
		v    int64
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{-1, []byte{0xff}},
		{127, []byte{0x7f}},
		{128, []byte{0x00, 0x80}},
		{-128, []byte{0x80}},
		{-129, []byte{0xff, 0x7f}},
		{32767, []byte{0x7f, 0xff}},
		{-32768, []byte{0x80, 0x00}},
		{math.MaxInt32, []byte{0x7f, 0xff, 0xff, 0xff}},
		{math.MinInt32, []byte{0x80, 0x00, 0x00, 0x00}},
		{math.MaxInt64, []byte{0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{math.MinInt64, []byte{0x80, 0, 0, 0, 0, 0, 0, 0}},
	} {
		if got := signedBytes(tc.v); !bytes.Equal(got, tc.want) {
			t.Errorf("signedBytes(%d) = % x, want % x", tc.v, got, tc.want)
		}
		// Integer round trip through the Data decoder (sign extension).
		b, err := EncodeData(mms.NewInteger(tc.v))
		if err != nil {
			t.Fatal(err)
		}
		v, err := DecodeData(b)
		if err != nil {
			t.Fatal(err)
		}
		if x, _ := v.Int64(); x != tc.v {
			t.Errorf("integer %d decoded as %d", tc.v, x)
		}
	}
}

func TestEncodeUnsignedWidths(t *testing.T) {
	for _, tc := range []struct {
		v    uint64
		want []byte
	}{
		{0, []byte{0x00}},
		{0x7f, []byte{0x7f}},
		{0x80, []byte{0x00, 0x80}},
		{0xff, []byte{0x00, 0xff}},
		{0x100, []byte{0x01, 0x00}},
		{math.MaxUint32, []byte{0x00, 0xff, 0xff, 0xff, 0xff}},
		{math.MaxInt64, []byte{0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{math.MaxUint64, []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	} {
		if got := encodeUnsigned(tc.v); !bytes.Equal(got, tc.want) {
			t.Errorf("encodeUnsigned(%#x) = % x, want % x", tc.v, got, tc.want)
		}
	}
}

func TestReadTLVMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    []byte
	}{
		{"empty", nil},
		{"missing length", []byte{0x80}},
		{"multi-octet tag", []byte{0x1f, 0x01, 0x00}},
		{"indefinite length", []byte{0x80, 0x80, 0x00, 0x00}},
		{"five length octets", []byte{0x80, 0x85, 0, 0, 0, 0, 1, 0}},
		{"truncated long length", []byte{0x80, 0x82, 0x01}},
		{"content beyond buffer", []byte{0x80, 0x03, 0x01, 0x02}},
		{"long length beyond buffer", []byte{0x80, 0x84, 0xff, 0xff, 0xff, 0xff, 0x00}},
	} {
		if _, err := readTLV(tc.b, 0, 10, "f"); !errors.Is(err, ErrInvalidPDU) {
			t.Errorf("%s: err = %v, want ErrInvalidPDU", tc.name, err)
		}
	}
	// Non-minimal long form is accepted (X.690 BER, not DER).
	e, err := readTLV([]byte{0x80, 0x81, 0x01, 0xaa}, 0, 0, "f")
	if err != nil || !bytes.Equal(e.content, []byte{0xaa}) {
		t.Errorf("non-minimal length: %v % x", err, e.content)
	}
}

func TestDecodeUint32Bounds(t *testing.T) {
	for _, tc := range []struct {
		c    []byte
		want uint32
		ok   bool
	}{
		{nil, 0, false},
		{[]byte{0x00}, 0, true},
		{[]byte{0x7f}, 0x7f, true},
		{[]byte{0x80}, 0, false}, // negative
		{[]byte{0x00, 0x80}, 0x80, true},
		{[]byte{0x7f, 0xff, 0xff, 0xff}, math.MaxInt32, true},
		{[]byte{0x00, 0xff, 0xff, 0xff, 0xff}, math.MaxUint32, true},
		{[]byte{0x01, 0x00, 0x00, 0x00, 0x00}, 0, false}, // 2^32
		{[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01}, 0, false},
	} {
		got, err := decodeUint32(tc.c, "f", 0)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("decodeUint32(% x) = %d, %v; want %d ok=%v", tc.c, got, err, tc.want, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrInvalidPDU) {
			t.Errorf("decodeUint32(% x): %v does not wrap ErrInvalidPDU", tc.c, err)
		}
	}
}

// Regression: UtcTime SecondSinceEpoch is 32-bit unsigned. Before the
// fix, instants before 1970 or after 2106-02-07T06:28:15Z were wrapped
// silently (time.Unix(-1, 0) went out as 0xffffffff, i.e. 2106).
func TestEncodeUTCTimeRange(t *testing.T) {
	maxT := time.Unix(math.MaxUint32, 0)
	c, err := encodeUTCTime(maxT, 0x0a)
	if err != nil || !bytes.Equal(c, []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0x0a}) {
		t.Fatalf("max UtcTime: % x %v", c, err)
	}
	if c, err := encodeUTCTime(time.Unix(0, 0), 0); err != nil || !bytes.Equal(c, make([]byte, 8)) {
		t.Fatalf("epoch UtcTime: % x %v", c, err)
	}
	// A zero time.Time (unset timestamp) is the epoch, also via Encode.
	if c, err := encodeUTCTime(time.Time{}, 0x0a); err != nil || !bytes.Equal(c, []byte{0, 0, 0, 0, 0, 0, 0, 0x0a}) {
		t.Fatalf("zero UtcTime: % x %v", c, err)
	}
	b, err := Encode(&Frame{Dst: Broadcast, PDU: PDU{GocbRef: "g", DatSet: "d"}})
	if err != nil {
		t.Fatalf("Encode with zero T: %v", err)
	}
	if fr, err := Decode(b); err != nil || fr.PDU.T.Unix() != 0 {
		t.Fatalf("zero T decoded as %v, %v; want epoch", fr.PDU.T, err)
	}
	for _, tm := range []time.Time{time.Unix(-1, 0), time.Unix(math.MaxUint32+1, 0), time.Date(1969, 12, 31, 23, 59, 59, 999_999_999, time.UTC)} {
		if _, err := encodeUTCTime(tm, 0); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("encodeUTCTime(%v): %v, want ErrInvalidValue", tm, err)
		}
		if _, err := EncodeData(mms.NewUTCTime(tm)); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("EncodeData(UTCTime %v): %v, want ErrInvalidValue", tm, err)
		}
		f := &Frame{Dst: Broadcast, PDU: PDU{GocbRef: "g", DatSet: "d", T: tm}}
		if _, err := Encode(f); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("Encode(T=%v): %v, want ErrInvalidValue", tm, err)
		}
	}
	// Fraction of second: 0.999999999 s rounds to the 24-bit maximum.
	c, err = encodeUTCTime(time.Unix(1, 999_999_999), 0)
	if err != nil || !bytes.Equal(c[4:7], []byte{0xff, 0xff, 0xff}) {
		t.Errorf("fraction: % x %v", c, err)
	}
}

func TestEncodeBinaryTimeRange(t *testing.T) {
	minMs := epoch1984.UnixMilli()
	maxMs := epoch1984.AddDate(0, 0, 0x10000).UnixMilli() - 1
	for _, tc := range []struct {
		ms   int64
		want []byte
	}{
		{minMs, []byte{0, 0, 0, 0, 0, 0}},
		{minMs + 86400000 + 1, []byte{0, 0, 0, 1, 0, 1}},
		{maxMs, []byte{0x05, 0x26, 0x5b, 0xff, 0xff, 0xff}},
	} {
		c, err := encodeBinaryTime(tc.ms)
		if err != nil || !bytes.Equal(c, tc.want) {
			t.Errorf("encodeBinaryTime(%d) = % x, %v; want % x", tc.ms, c, err, tc.want)
		}
	}
	for _, ms := range []int64{minMs - 1, minMs - 86400000, maxMs + 1, math.MinInt64, math.MaxInt64} {
		if _, err := encodeBinaryTime(ms); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("encodeBinaryTime(%d): %v, want ErrInvalidValue", ms, err)
		}
	}
}

func TestAppendBitsUnusedBits(t *testing.T) {
	for n := range 17 {
		bits := []byte{0xff, 0xff, 0xff}
		got, err := appendBits(nil, tagBitString, bits, n)
		if err != nil {
			t.Fatal(err)
		}
		nb := (n + 7) / 8
		if got[1] != byte(1+nb) || got[2] != byte(nb*8-n) {
			t.Errorf("n=%d: % x", n, got)
		}
	}
	if _, err := appendBits(nil, tagBitString, []byte{0}, 9); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("n beyond octets: %v", err)
	}
	if _, err := appendBits(nil, tagBitString, nil, -1); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("negative n: %v", err)
	}
}

func TestTalMillis(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want uint32
	}{
		{0, 0},
		{-time.Second, 0},
		{1500 * time.Microsecond, 1},
		{2 * time.Second, 2000},
		{math.MaxUint32 * time.Millisecond, math.MaxUint32},
		{(math.MaxUint32 + 1) * time.Millisecond, math.MaxUint32},
	} {
		if got := talMillis(tc.d); got != tc.want {
			t.Errorf("talMillis(%v) = %d, want %d", tc.d, got, tc.want)
		}
	}
}

func TestEncodePDUDefaultNumDatSetEntries(t *testing.T) {
	p := &PDU{GocbRef: "g", DatSet: "d", T: time.Unix(0, 0), AllData: []*mms.Value{mms.NewBoolean(true), mms.NewInteger(1), mms.NewUnsigned(2)}}
	b, err := EncodePDU(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePDU(b)
	if err != nil || got.NumDatSetEntries != 3 {
		t.Fatalf("numDatSetEntries = %d, %v", got.NumDatSetEntries, err)
	}
	p.NumDatSetEntries = 7
	b, _ = EncodePDU(p)
	if got, err = DecodePDU(b); err != nil || got.NumDatSetEntries != 7 {
		t.Fatalf("explicit numDatSetEntries = %d, %v", got.NumDatSetEntries, err)
	}
}

// Every strict prefix of a valid frame (whose length field covers the
// whole APDU) and of a valid APDU must be rejected with a typed error,
// never accepted or panicking.
func TestDecodeTruncatedFrames(t *testing.T) {
	f := &Frame{
		Dst: Broadcast, AppID: 1, VLAN: &VLAN{Priority: 4, ID: 1},
		PDU: PDU{
			GocbRef: "LD/LLN0$GO$g", TimeAllowedToLive: 2000, DatSet: "LD/LLN0$ds", GoID: "id",
			T: time.Unix(1700000000, 0), StNum: 1, ConfRev: 1,
			AllData: []*mms.Value{mms.NewBoolean(true), mms.NewInteger(-300), mms.NewVisibleString("abc")},
		},
	}
	full, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	apdu, err := EncodePDU(&f.PDU)
	if err != nil {
		t.Fatal(err)
	}
	// MACs (12) + VLAN tag (4) + EtherType (2) + GOOSE header (8).
	frameLen := 26 + len(apdu)
	if len(full) != frameLen {
		t.Fatalf("frame is %d octets, want %d", len(full), frameLen)
	}
	var de *DecodeError
	for n := range frameLen {
		if _, err := Decode(full[:n]); !errors.As(err, &de) {
			t.Errorf("Decode of %d/%d octets: %v, want *DecodeError", n, frameLen, err)
		}
	}
	for n := range len(apdu) {
		if _, err := DecodePDU(apdu[:n]); !errors.As(err, &de) {
			t.Errorf("DecodePDU of %d/%d octets: %v, want *DecodeError", n, len(apdu), err)
		}
	}
}
