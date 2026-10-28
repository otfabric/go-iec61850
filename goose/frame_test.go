// SPDX-License-Identifier: MIT

package goose

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func testFrame() *Frame {
	return &Frame{
		Dst:   MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01},
		Src:   MAC{0x02, 0, 0, 0, 0, 0x01},
		VLAN:  &VLAN{Priority: 4, ID: 1},
		AppID: 0x1000,
		PDU: PDU{
			GocbRef:           "IED1LD0/LLN0$GO$gcb1",
			TimeAllowedToLive: 2000,
			DatSet:            "IED1LD0/LLN0$ds1",
			GoID:              "gcb1",
			T:                 time.Date(2024, 1, 1, 0, 0, 1, 500_000_000, time.UTC),
			TimeQuality:       0x0a,
			StNum:             1,
			SqNum:             0,
			ConfRev:           1,
			AllData: []*mms.Value{
				mms.NewBoolean(true),
				mms.NewInteger(-5),
				mms.NewUnsigned(42),
				mms.NewFloat(1234.5),
				mms.NewBitStringWithLength([]byte{0x00, 0x00}, 13),
				mms.NewUTCTimeWithQuality(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 0x0a),
				mms.NewStructure([]*mms.Value{mms.NewStructure([]*mms.Value{mms.NewFloat(-0.25)}), mms.NewBitStringWithLength([]byte{0x80, 0x00}, 13)}),
			},
		},
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	f := testFrame()
	b, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := b[12:14]; !bytes.Equal(got, []byte{0x81, 0x00}) {
		t.Fatalf("TPID = %x", got)
	}
	if got := b[14:16]; !bytes.Equal(got, []byte{0x80, 0x01}) {
		t.Fatalf("TCI = %x", got)
	}
	g, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if g.Dst != f.Dst || g.Src != f.Src || g.AppID != f.AppID || *g.VLAN != *f.VLAN {
		t.Fatalf("header mismatch: %+v", g)
	}
	p, q := g.PDU, f.PDU
	if p.GocbRef != q.GocbRef || p.DatSet != q.DatSet || p.GoID != q.GoID || p.TimeAllowedToLive != q.TimeAllowedToLive ||
		p.StNum != q.StNum || p.SqNum != q.SqNum || p.ConfRev != q.ConfRev || p.NumDatSetEntries != uint32(len(q.AllData)) {
		t.Fatalf("pdu mismatch: %+v", p)
	}
	if !p.T.Equal(q.T) || p.TimeQuality != 0x0a {
		t.Fatalf("t = %v q=%x", p.T, p.TimeQuality)
	}
	for i := range q.AllData {
		if !p.AllData[i].Equal(q.AllData[i]) {
			t.Errorf("allData[%d] = %v, want %v", i, p.AllData[i], q.AllData[i])
		}
	}
}

func TestEncodeGolden(t *testing.T) {
	f := &Frame{
		Dst:   MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01},
		Src:   MAC{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		AppID: 0x0001,
		PDU: PDU{
			GocbRef: "A", TimeAllowedToLive: 200, DatSet: "B", GoID: "C",
			T: time.Unix(0, 0).UTC(), StNum: 1, SqNum: 2, ConfRev: 3,
			AllData: []*mms.Value{mms.NewBoolean(true)},
		},
	}
	b, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "010ccd010001" + "001122334455" + "88b8" +
		"0001" + "0038" + "0000" + "0000" +
		"612e" +
		"800141" + // gocbRef
		"810200c8" + // TAL 200 (leading zero: high bit)
		"820142" + // datSet
		"830143" + // goID
		"84080000000000000000" + // t
		"850101" + "860102" + // stNum, sqNum
		"870100" + // simulation/test
		"880103" + // confRev
		"890100" + // ndsCom
		"8a0101" + // numDatSetEntries
		"ab03" + "8301ff" // allData
	got := hex.EncodeToString(b)
	if !strings.HasPrefix(got, want) {
		t.Fatalf("encoding\n got %s\nwant %s", got, want)
	}
	if len(b) != 14+0x38 {
		t.Fatalf("len = %d", len(b))
	}
}

func TestEncodePadsShortFrames(t *testing.T) {
	f := &Frame{Dst: Broadcast, PDU: PDU{GocbRef: "g", DatSet: "d", T: time.Unix(0, 0)}}
	b, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 60 {
		t.Fatalf("frame not padded: %d", len(b))
	}
	if _, err := Decode(b); err != nil {
		t.Fatalf("padded frame does not decode: %v", err)
	}
}

func TestDataRoundTripAllTypes(t *testing.T) {
	vals := []*mms.Value{
		mms.NewBoolean(false),
		mms.NewInteger(math.MinInt64),
		mms.NewInteger(math.MaxInt64),
		mms.NewInteger(0),
		mms.NewInteger(-129),
		mms.NewUnsigned(math.MaxUint64),
		mms.NewUnsigned(0),
		mms.NewFloat(0.1),
		mms.NewFloat(float64(float32(3.5))),
		mms.NewBitStringWithLength([]byte{0xc0}, 2),
		mms.NewBitStringWithLength(nil, 0),
		mms.NewBooleanArray([]byte{0xa0}, 3),
		mms.NewOctetString([]byte{1, 2, 3}),
		mms.NewVisibleString("hello world"),
		mms.NewMmsString("grüße"),
		mms.NewUTCTimeWithQuality(time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC), 0x8a),
		mms.NewBinaryTime(time.Date(2024, 2, 3, 4, 5, 6, 7_000_000, time.UTC).UnixMilli()),
		mms.NewGeneralizedTime(time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)),
		mms.NewBCD(1234),
		mms.NewObjectIdentifier([]int{1, 0, 9506, 2, 3}),
		mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}),
		mms.NewStructure(nil),
	}
	for _, v := range vals {
		b, err := EncodeData(v)
		if err != nil {
			t.Fatalf("encode %v: %v", v, err)
		}
		got, err := DecodeData(b)
		if err != nil {
			t.Fatalf("decode %v (%x): %v", v, b, err)
		}
		if !got.Equal(v) {
			t.Errorf("round trip %v -> %x -> %v", v, b, got)
		}
	}
}

func TestEncodeDataErrors(t *testing.T) {
	cases := []*mms.Value{
		nil,
		mms.NewDataAccessError(1),
		mms.NewReal(1),
		mms.NewBCD(-1),
		mms.NewObjectIdentifier([]int{3, 1}),
		mms.NewObjectIdentifier([]int{1, 2, -1}),
		mms.NewBinaryTime(0),
		mms.NewBitStringWithLength([]byte{0}, 9),
		mms.NewStructure([]*mms.Value{nil}),
	}
	for i, v := range cases {
		if _, err := EncodeData(v); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("case %d: err = %v, want ErrInvalidValue", i, err)
		}
	}
	deep := mms.NewInteger(1)
	for i := 0; i < 20; i++ {
		deep = mms.NewStructure([]*mms.Value{deep})
	}
	if _, err := EncodeData(deep); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("deep: err = %v", err)
	}
	if _, err := EncodePDU(&PDU{AllData: []*mms.Value{nil}}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("pdu nil: %v", err)
	}
	var ee *EncodeError
	if _, err := EncodePDU(&PDU{AllData: []*mms.Value{mms.NewBoolean(true), nil}}); !errors.As(err, &ee) || ee.Index != 1 || !strings.Contains(err.Error(), "allData[1]") {
		t.Errorf("EncodeError: %v", err)
	}
}

func TestEncodeFrameErrors(t *testing.T) {
	if _, err := Encode(nil); !errors.Is(err, ErrInvalidValue) {
		t.Fatal(err)
	}
	f := testFrame()
	f.VLAN = &VLAN{Priority: 8}
	if _, err := Encode(f); !errors.Is(err, ErrInvalidValue) {
		t.Fatal(err)
	}
	f = testFrame()
	f.PDU.AllData = []*mms.Value{mms.NewOctetString(make([]byte, 1600))}
	if _, err := Encode(f); !errors.Is(err, ErrLimitExceeded) {
		t.Fatal(err)
	}
}

func mustEncode(t *testing.T, f *Frame) []byte {
	t.Helper()
	b, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeErrors(t *testing.T) {
	good := mustEncode(t, &Frame{Dst: Broadcast, AppID: 1, PDU: PDU{GocbRef: "g", DatSet: "d", T: time.Unix(0, 0), AllData: []*mms.Value{mms.NewBoolean(true)}}})
	// untagged: header at 14, APDU at 22
	mut := func(fn func(b []byte) []byte) []byte { return fn(append([]byte(nil), good...)) }
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"short", good[:10], ErrInvalidFrame},
		{"not goose", mut(func(b []byte) []byte { b[12], b[13] = 0x08, 0x00; return b }), ErrNotGOOSE},
		{"truncated vlan", []byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 0x81, 0x00, 0}, ErrInvalidFrame},
		{"truncated header", good[:18], ErrInvalidFrame},
		{"length too small", mut(func(b []byte) []byte { b[16], b[17] = 0, 4; return b }), ErrInvalidLength},
		{"length too big", mut(func(b []byte) []byte { b[16], b[17] = 0xff, 0xff; return b }), ErrInvalidLength},
		{"empty apdu", mut(func(b []byte) []byte { b[16], b[17] = 0, 8; return b }), ErrInvalidLength},
		{"mngt pdu", mut(func(b []byte) []byte { b[22] = 0x60; return b }), ErrUnsupportedPDU},
		{"bad apdu tag", mut(func(b []byte) []byte { b[22] = 0x30; return b }), ErrInvalidPDU},
		{"indefinite", mut(func(b []byte) []byte { b[23] = 0x80; return b }), ErrInvalidPDU},
		{"bad field tag", mut(func(b []byte) []byte { b[24] = 0x9f; return b }), ErrInvalidPDU},
		{"unknown field", mut(func(b []byte) []byte { b[24] = 0x8d; return b }), ErrInvalidPDU},
		{"order", mut(func(b []byte) []byte { b[24] = 0x82; return b }), ErrInvalidPDU},
		{"bad string", mut(func(b []byte) []byte { b[26] = 0x01; return b }), ErrInvalidPDU},
	}
	for _, c := range cases {
		_, err := Decode(c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		var de *DecodeError
		if err != nil && c.want != nil && !errors.As(err, &de) {
			t.Errorf("%s: not a DecodeError: %T", c.name, err)
		}
	}
}

// pduBytes builds a goosePdu from raw field TLVs.
func pduBytes(fields ...string) []byte {
	c, _ := hex.DecodeString(strings.Join(fields, ""))
	return appendTLV(nil, tagGoosePDU, c)
}

func TestDecodePDUFieldErrors(t *testing.T) {
	const (
		gocb = "800141"
		tal  = "810164"
		ds   = "820142"
		tm   = "84080000000000000000"
		st   = "850101"
		sq   = "860100"
		cr   = "880101"
		n0   = "8a0100"
		ad0  = "ab00"
	)
	ok := pduBytes(gocb, tal, ds, tm, st, sq, cr, n0, ad0)
	if _, err := DecodePDU(ok); err != nil {
		t.Fatalf("minimal pdu: %v", err)
	}
	bad := map[string][]byte{
		"missing gocbRef":   pduBytes(tal, ds, tm, st, sq, cr, n0, ad0),
		"missing allData":   pduBytes(gocb, tal, ds, tm, st, sq, cr, n0),
		"duplicate":         pduBytes(gocb, gocb, tal, ds, tm, st, sq, cr, n0, ad0),
		"negative stNum":    pduBytes(gocb, tal, ds, tm, "8501ff", sq, cr, n0, ad0),
		"empty int":         pduBytes(gocb, "8100", ds, tm, st, sq, cr, n0, ad0),
		"int too large":     pduBytes(gocb, tal, ds, tm, "85050100000000", sq, cr, n0, ad0),
		"int too long":      pduBytes(gocb, tal, ds, tm, "8506000000000001", sq, cr, n0, ad0),
		"bad time":          pduBytes(gocb, tal, ds, "8401ff", st, sq, cr, n0, ad0),
		"bad bool":          pduBytes(gocb, tal, ds, tm, st, sq, "87020000", cr, n0, ad0),
		"trailing":          append(append([]byte(nil), ok...), 0),
		"long length":       {0x61, 0x85, 0, 0, 0, 0, 0},
		"truncated len":     {0x61, 0x82, 0},
		"truncated":         {0x61},
		"bad data":          pduBytes(gocb, tal, ds, tm, st, sq, cr, "8a0101", "ab02", "8800"),
		"truncated element": pduBytes(gocb, tal, ds, tm, st, sq, cr, "8a0101", "ab01", "83"),
	}
	for name, b := range bad {
		if _, err := DecodePDU(b); err == nil {
			t.Errorf("%s: decoded without error", name)
		} else if !errors.Is(err, ErrInvalidPDU) && !errors.Is(err, ErrInvalidData) && !errors.Is(err, ErrInvalidLength) {
			t.Errorf("%s: unexpected error class %v", name, err)
		} else if !strings.Contains(err.Error(), "offset") {
			t.Errorf("%s: error lacks offset: %v", name, err)
		}
	}
	// security, both primitive and constructed forms
	for _, sec := range []string{"8c0101", "ac00"} {
		p, err := DecodePDU(pduBytes(gocb, tal, ds, tm, st, sq, cr, n0, ad0, sec))
		if err != nil || p.Security == nil {
			t.Errorf("security %s: %v", sec, err)
		}
	}
	// non-minimal long-form length is accepted
	if _, err := DecodePDU(pduBytes("80810141", tal, ds, tm, st, sq, cr, n0, ad0)); err != nil {
		t.Errorf("long form length: %v", err)
	}
}

func TestDecodeDataErrors(t *testing.T) {
	bad := []string{
		"", "8300", "830200ff", "8500", "85090000000000000001", "8600", "860a00000000000000000001", "860180",
		"8702ff00", "870508000000", "8400", "8401ff", "840101", "8a0101", "9002c328",
		"9101ff", "8c03000000", "8b0278", "8f00", "8f0181", "8d01ff", "a003010101", "830101830101",
		"9f0100", "a20283",
	}
	for _, h := range bad {
		b, _ := hex.DecodeString(h)
		if _, err := DecodeData(b); err == nil {
			t.Errorf("%q: decoded without error", h)
		}
	}
	// 4-byte TimeOfDay
	v, err := DecodeData([]byte{0x8c, 4, 0, 0, 0, 10})
	if err != nil {
		t.Fatal(err)
	}
	if ms, _ := v.BinaryTime(); ms != epoch1984.UnixMilli()+10 {
		t.Errorf("binary time = %d", ms)
	}
	// generalized time without fraction
	if _, err := DecodeData(append([]byte{0x8b, 15}, "20240102030405Z"...)); err != nil {
		t.Error(err)
	}
	// float64
	if v, err := DecodeData([]byte{0x87, 9, 11, 0x3f, 0xf0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Error(err)
	} else if f, _ := v.Float64(); f != 1 {
		t.Errorf("float64 = %v", f)
	}
}

func TestDecodeLimits(t *testing.T) {
	v := mms.NewInteger(1)
	for i := 0; i < 5; i++ {
		v = mms.NewStructure([]*mms.Value{v})
	}
	f := &Frame{Dst: Broadcast, PDU: PDU{GocbRef: "g", DatSet: "d", T: time.Unix(0, 0), AllData: []*mms.Value{v, mms.NewBoolean(true)}}}
	b := mustEncode(t, f)
	if _, err := DecodeWithLimits(b, Limits{MaxDepth: 3, MaxElements: 100}); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("depth: %v", err)
	}
	if _, err := DecodeWithLimits(b, Limits{MaxDepth: 10, MaxElements: 4}); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("elements: %v", err)
	}
	if _, err := DecodeWithLimits(b, Limits{MaxDepth: 10, MaxElements: 100}); err != nil {
		t.Errorf("within limits: %v", err)
	}
}

func TestMAC(t *testing.T) {
	for _, s := range []string{"01-0C-CD-01-00-01", "01:0c:cd:01:00:01", "010CCD010001"} {
		m, err := ParseMAC(s)
		if err != nil || m.String() != "01:0c:cd:01:00:01" || !m.IsMulticast() || !m.IsGOOSEMulticast() {
			t.Errorf("%s: %v %v", s, m, err)
		}
	}
	for _, s := range []string{"", "01-02", "zz:zz:zz:zz:zz:zz", "01:02:03:04:05:06:07:08"} {
		if _, err := ParseMAC(s); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%q: %v", s, err)
		}
	}
	var m MAC
	if !m.IsZero() || m.IsMulticast() {
		t.Error("zero MAC")
	}
	if err := m.UnmarshalText([]byte("bad")); err == nil {
		t.Error("UnmarshalText accepted bad input")
	}
	if err := m.UnmarshalText([]byte("02:00:00:00:00:01")); err != nil || m[5] != 1 {
		t.Error(err)
	}
	if b, _ := m.MarshalText(); string(b) != "02:00:00:00:00:01" {
		t.Error(string(b))
	}
	if (MAC{0x01, 0x0c, 0xcd, 0x04, 0, 0}).IsGOOSEMulticast() {
		t.Error("SV range reported as GOOSE")
	}
}

func TestErrorStrings(t *testing.T) {
	errs := []error{
		&DecodeError{Field: "x", Offset: 1, Message: "m", Err: ErrInvalidPDU},
		&DecodeError{Offset: 1, Message: "m", Err: ErrInvalidPDU},
		&ValidationError{GocbRef: "g", Field: "confRev", Want: "1", Got: "2", Err: ErrConfRevMismatch},
		&ConfigError{Field: "f", Message: "m"},
		&TransportError{Op: "open", Interface: "eth0", Err: errors.New("boom"), Kind: ErrPermission},
		&TransportError{Op: "open", Interface: "eth0", Err: errors.New("boom")},
	}
	for _, e := range errs {
		if e.Error() == "" {
			t.Errorf("%T: empty message", e)
		}
	}
	if !errors.Is(errs[2], ErrValidation) || !errors.Is(errs[2], ErrConfRevMismatch) {
		t.Error("ValidationError unwrap")
	}
	if !errors.Is(errs[3], ErrInvalidConfig) || !errors.Is(errs[4], ErrPermission) {
		t.Error("unwrap")
	}
}

func TestBERHelpers(t *testing.T) {
	for _, n := range []int{0, 127, 128, 255, 256, 65535, 65536, 1 << 24} {
		b := appendLength(nil, n)
		if len(b) != lengthSize(n) {
			t.Errorf("lengthSize(%d) = %d, encoded %d", n, lengthSize(n), len(b))
		}
	}
	for _, v := range []int64{0, 1, -1, 127, 128, -128, -129, 1 << 40, math.MinInt64, math.MaxInt64} {
		b := appendSigned(nil, v)
		x := int64(int8(b[0]))
		for _, o := range b[1:] {
			x = x<<8 | int64(o)
		}
		if x != v {
			t.Errorf("appendSigned(%d) = %x", v, b)
		}
	}
}
