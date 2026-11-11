// SPDX-License-Identifier: MIT

package goose

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// TestSpec_8_1_A3_FixedLengthDecode: 8-1 Annex A.3 fixed-length encoding
// is backward compatible "as long as the subscriber implementation does
// not check if the shortest-length strategy has been respected"; Table
// A.2 sizes must decode (GSE-02).
func TestSpec_8_1_A3_FixedLengthDecode(t *testing.T) {
	t.Parallel()
	padded := append([]byte{0x8a, 35, 'a', 'b', 'c'}, make([]byte, 32)...)
	ok := []struct {
		name string
		in   []byte
		want *mms.Value
	}{
		{"INT64 9 octets", []byte{0x85, 9, 0, 0, 0, 0, 0, 0, 0, 0, 1}, mms.NewInteger(1)},
		{"INT64 9 octets -1", []byte{0x85, 9, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, mms.NewInteger(-1)},
		{"INT8 2 octets -1", []byte{0x85, 2, 0xff, 0xff}, mms.NewInteger(-1)},
		{"INT8U 2 octets 128", []byte{0x86, 2, 0x00, 0x80}, mms.NewUnsigned(128)},
		{"INT32U 5 octets", []byte{0x86, 5, 0, 0, 0, 0, 7}, mms.NewUnsigned(7)},
		{"VISIBLE STRING 35 NUL terminated", padded, mms.NewVisibleString("abc")},
		{"FLOAT32 Table A.2 length 4", []byte{0x87, 4, 0x3f, 0x80, 0, 0}, mms.NewFloat(1)},
	}
	for _, c := range ok {
		v, err := DecodeData(c.in)
		if err != nil || !v.Equal(c.want) {
			t.Errorf("%s: %v %v", c.name, v, err)
		}
	}
	bad := map[string][]byte{
		"INT 9 octets not sign extension": {0x85, 9, 0x01, 0, 0, 0, 0, 0, 0, 0, 0},
		"INT 9 octets 0xff before clear":  {0x85, 9, 0xff, 0x7f, 0, 0, 0, 0, 0, 0, 0},
		"embedded NUL":                    {0x8a, 4, 'a', 0, 'b', 0},
	}
	for name, in := range bad {
		if _, err := DecodeData(in); !errors.Is(err, ErrInvalidData) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// stNum and the other header INT32U fields with length 5 (Table A.1).
	if v, err := decodeUint32([]byte{0, 0, 0, 0, 1}, "stNum", 0); err != nil || v != 1 {
		t.Errorf("stNum 5 octets: %d %v", v, err)
	}
}

// TestSpec_8_1_A3_FixedLengthEncode: with FixedOffs every field keeps
// its offset for a configuration (Table A.1/A.2); the frame length and
// the allData offsets do not depend on the values (GSE-11).
func TestSpec_8_1_A3_FixedLengthEncode(t *testing.T) {
	t.Parallel()
	types := []mms.TypeSpec{
		{Type: mms.ValueTypeBoolean},
		{Type: mms.ValueTypeInteger, Size: 8},
		{Type: mms.ValueTypeInteger, Size: 16},
		{Type: mms.ValueTypeInteger, Size: 32},
		{Type: mms.ValueTypeInteger, Size: 64},
		{Type: mms.ValueTypeUnsigned, Size: 8},
		{Type: mms.ValueTypeUnsigned, Size: 16},
		{Type: mms.ValueTypeUnsigned, Size: 32},
		{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		{Type: mms.ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11},
		{Type: mms.ValueTypeBitString, Size: 13},
		{Type: mms.ValueTypeOctetString, Size: 64},
		{Type: mms.ValueTypeVisibleString, Size: 255},
		{Type: mms.ValueTypeUTCTime},
		{Type: mms.ValueTypeStructure, Elements: []mms.TypeSpecElement{{Name: "stVal", Type: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32}}}},
		{Type: mms.ValueTypeArray, Count: 2, Element: &mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
	}
	vals := func(small bool) []*mms.Value {
		i, u, s := int64(1), uint64(1), "a"
		if !small {
			i, u, s = -0x70000000, 0xfffffff0, "a longer value of 20"
		}
		return []*mms.Value{
			mms.NewBoolean(small), mms.NewInteger(max(i, -128) % 128), mms.NewInteger(i % 30000), mms.NewInteger(i),
			mms.NewInteger(i * 1000), mms.NewUnsigned(u % 256), mms.NewUnsigned(u % 65536), mms.NewUnsigned(u),
			mms.NewFloat(1), mms.NewFloat(1), mms.NewBitStringWithLength([]byte{0, 0}, 13), mms.NewOctetString([]byte(s)),
			mms.NewVisibleString(s), mms.NewUTCTimeWithQuality(time.Unix(0, 0), 0),
			mms.NewStructure([]*mms.Value{mms.NewInteger(i)}), mms.NewArray([]*mms.Value{mms.NewUnsigned(u % 65536), mms.NewUnsigned(0)}),
		}
	}
	frame := func(st uint32, small bool) *Frame {
		return &Frame{Dst: testDst, AppID: 1, PDU: PDU{GocbRef: "LD/LLN0$GO$g", DatSet: "LD/LLN0$ds", GoID: "g",
			TimeAllowedToLive: 2000, T: time.Unix(1e9, 0), StNum: st, SqNum: st, ConfRev: 1, AllData: vals(small)}}
	}
	a, err := EncodeFixedLength(frame(1, true), types)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeFixedLength(frame(0x01000000, false), types)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("lengths %d / %d differ", len(a), len(b))
	}
	// The BER encodings of the same frames differ in length.
	if x, _ := Encode(frame(1, true)); len(x) == len(a) {
		t.Errorf("BER and fixed-length encodings unexpectedly have the same length")
	}
	// The allData element offsets are identical.
	offs := func(raw []byte) []int {
		f, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if f.PDU.StNum == 0 {
			t.Fatal("stNum")
		}
		i := bytes.Index(raw, []byte{0xab}) // allData tag after numDatSetEntries
		return []int{i}
	}
	if oa, ob := offs(a), offs(b); oa[0] != ob[0] {
		t.Errorf("allData offset %d / %d", oa[0], ob[0])
	}
	// Byte vectors from Table A.1 / A.3: header INT32U are 5 octets.
	pdu, err := EncodePDUFixedLength(&frame(1, true).PDU, types)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		{0x81, 5, 0, 0, 0, 0x07, 0xd0},                   // timeAllowedToLive 2000
		{0x85, 5, 0, 0, 0, 0, 1, 0x86, 5, 0, 0, 0, 0, 1}, // stNum 1, sqNum 1
		{0x88, 5, 0, 0, 0, 0, 1},                         // confRev 1
		{0x8a, 5, 0, 0, 0, 0, 16},                        // numDatSetEntries
		{0x85, 2, 0, 1, 0x85, 3, 0, 0, 1, 0x85, 5, 0, 0, 0, 0, 1, 0x85, 9, 0, 0, 0, 0, 0, 0, 0, 0x03, 0xe8}, // INT8/16/32/64
		{0x86, 2, 0, 1, 0x86, 3, 0, 0, 1, 0x86, 5, 0, 0, 0, 0, 1},                                           // INT8U/16U/32U
		{0x87, 5, 8, 0x3f, 0x80, 0, 0, 0x87, 9, 11, 0x3f, 0xf0},                                             // FLOAT32 / FLOAT64
		append([]byte{0x89, 20, 'a'}, make([]byte, 19)...),                                                  // OCTET STRING 20
		append([]byte{0x8a, 35, 'a'}, make([]byte, 34)...),                                                  // VISIBLE STRING 35
	} {
		if !bytes.Contains(pdu, want) {
			t.Errorf("missing % x in % x", want, pdu)
		}
	}
	// Round trip: padded strings decode to the original value.
	p, err := DecodePDU(pdu)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vals(true) {
		if v.Type() == mms.ValueTypeOctetString {
			continue // zero padding is part of an OCTET STRING value
		}
		if !p.AllData[i].Equal(v) {
			t.Errorf("member %d: %v != %v", i, p.AllData[i], v)
		}
	}
	// Negative INT8 sign extension and unknown widths.
	got, _ := EncodePDUFixedLength(&PDU{GocbRef: "g", DatSet: "d", AllData: []*mms.Value{mms.NewInteger(-1), mms.NewInteger(1 << 40), mms.NewUnsigned(1 << 40), mms.NewUnsigned(3)}},
		[]mms.TypeSpec{{Type: mms.ValueTypeInteger, Size: 8}, {Type: mms.ValueTypeInteger}, {Type: mms.ValueTypeUnsigned}, {Type: mms.ValueTypeUnsigned, Size: 24}})
	for _, want := range [][]byte{{0x85, 2, 0xff, 0xff}, {0x85, 9, 0, 0, 0, 1, 0, 0, 0, 0, 0}, {0x86, 9, 0, 0, 0, 1, 0, 0, 0, 0, 0}, {0x86, 5, 0, 0, 0, 0, 3}} {
		if !bytes.Contains(got, want) {
			t.Errorf("missing % x in % x", want, got)
		}
	}
	// Values that do not fit their fixed size.
	for name, c := range map[string]struct {
		v  *mms.Value
		ts mms.TypeSpec
	}{
		"INT8 overflow":   {mms.NewInteger(128), mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}},
		"INT16U overflow": {mms.NewUnsigned(1 << 16), mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}},
		"octets > 20":     {mms.NewOctetString(make([]byte, 21)), mms.TypeSpec{Type: mms.ValueTypeOctetString}},
		"string > 35":     {mms.NewVisibleString(strings.Repeat("x", 36)), mms.TypeSpec{Type: mms.ValueTypeVisibleString}},
		"bit size":        {mms.NewBitStringWithLength([]byte{0}, 2), mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		"nil":             {nil, mms.TypeSpec{}},
	} {
		if _, err := EncodePDUFixedLength(&PDU{GocbRef: "g", DatSet: "d", AllData: []*mms.Value{c.v}}, []mms.TypeSpec{c.ts}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A type that does not match the value is ignored; no types at all.
	if _, err := EncodePDUFixedLength(&PDU{GocbRef: "g", DatSet: "d", AllData: []*mms.Value{mms.NewBoolean(true), mms.NewMmsString("x")}},
		[]mms.TypeSpec{{Type: mms.ValueTypeInteger}}); err != nil {
		t.Error(err)
	}
	deep := mms.NewBoolean(true)
	for range DefaultLimits.MaxDepth + 1 {
		deep = mms.NewStructure([]*mms.Value{deep})
	}
	if _, err := EncodePDUFixedLength(&PDU{GocbRef: "g", DatSet: "d", AllData: []*mms.Value{deep}}, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("depth: %v", err)
	}
}

// TestSpec_8_1_A3_FixedOffsPublisher: a publisher with FixedOffs sends
// frames of constant length whatever the stNum magnitude (GSE-11).
func TestSpec_8_1_A3_FixedOffsPublisher(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	tx, rx := hub.NewPort(MAC{}), hub.NewPort(MAC{})
	c := pubConfig()
	c.FixedOffs = true
	c.MemberTypes = []mms.TypeSpec{{Type: mms.ValueTypeBoolean}, {Type: mms.ValueTypeInteger, Size: 32}}
	c.InitialStNum = 0x00ffffff
	p, err := NewPublisher(tx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	var lens []int
	for _, v := range []int64{1, 0x7fffffff} {
		if err := p.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewInteger(v)}); err != nil {
			t.Fatal(err)
		}
		raw, err := rx.Recv(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		lens = append(lens, len(raw))
	}
	if lens[0] != lens[1] {
		t.Errorf("frame lengths %v", lens)
	}
	if err := p.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewInteger(1 << 40)}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("INT32 overflow: %v", err)
	}
	c.Values = []*mms.Value{mms.NewBoolean(true), mms.NewInteger(1 << 40)}
	if _, err := NewPublisher(tx, c); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("initial values: %v", err)
	}
}

// TestSpec_8_1_18_1_2_5_PublisherRanges: 8-1 18.1.2.5.2 field ranges and
// Figure 10 timer rules are enforced by NewPublisher (GSE-10, GSE-14).
func TestSpec_8_1_18_1_2_5_PublisherRanges(t *testing.T) {
	t.Parallel()
	port := NewHub().NewPort(MAC{})
	for name, mod := range map[string]func(c *PublisherConfig){
		"TAL below MaxTime": func(c *PublisherConfig) {
			c.MaxTime, c.MinTime, c.TimeAllowedToLive = 2*time.Second, time.Second, time.Second
		},
		"TAL equal MaxTime": func(c *PublisherConfig) { c.TimeAllowedToLive = c.MaxTime },
		"MaxTime 60s":       func(c *PublisherConfig) { c.MaxTime = 60 * time.Second },
		"TAL below 1ms": func(c *PublisherConfig) {
			c.MinTime, c.MaxTime, c.TimeAllowedToLive = 100*time.Microsecond, 200*time.Microsecond, 500*time.Microsecond
		},
		"GoID 66":     func(c *PublisherConfig) { c.GoID = strings.Repeat("g", 66) },
		"GocbRef 130": func(c *PublisherConfig) { c.GocbRef = strings.Repeat("g", 130) },
		"DatSet 130":  func(c *PublisherConfig) { c.DatSet = strings.Repeat("d", 130) },
		"unicast Dst": func(c *PublisherConfig) { c.Dst = MAC{0x02, 0, 0, 0, 0, 1} },
	} {
		c := pubConfig()
		mod(&c)
		if _, err := NewPublisher(port, c); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := pubConfig()
	c.GoID, c.GocbRef = strings.Repeat("g", 65), strings.Repeat("r", 129)
	if _, err := NewPublisher(port, c); err != nil {
		t.Errorf("maximum lengths: %v", err)
	}
}

// TestSpec_8_1_TimeQuality: T carries the configured TimeQuality, by
// default ClockNotSynchronized (GSE-14).
func TestSpec_8_1_TimeQuality(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ cfg, want uint8 }{{0, DefaultTimeQuality}, {0x0a, 0x0a}} {
		hub := NewHub()
		tx, rx := hub.NewPort(MAC{}), hub.NewPort(MAC{})
		pc := pubConfig()
		pc.TimeQuality = c.cfg
		p, err := NewPublisher(tx, pc)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Publish(pc.Values); err != nil {
			t.Fatal(err)
		}
		raw, err := rx.Recv(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		f, err := Decode(raw)
		if err != nil || f.PDU.TimeQuality != c.want {
			t.Errorf("TimeQuality %#x, want %#x (%v)", f.PDU.TimeQuality, c.want, err)
		}
		_ = p.Close()
	}
	if DefaultTimeQuality&0x20 == 0 {
		t.Error("default must flag ClockNotSynchronized")
	}
}

// TestSpec_8_1_AnnexC_MaxLength: Length = 8 + m with m < 1492 (GSE-12).
func TestSpec_8_1_AnnexC_MaxLength(t *testing.T) {
	t.Parallel()
	frame := func(apdu int) *Frame {
		f := &Frame{Dst: testDst, PDU: PDU{GocbRef: "g", DatSet: "d", T: time.Unix(0, 0), AllData: []*mms.Value{mms.NewOctetString(nil)}}}
		base, err := EncodePDU(&f.PDU)
		if err != nil {
			t.Fatal(err)
		}
		// Grow the octet string until the APDU has the wanted size
		// (the length-of-length steps make some sizes unreachable).
		for n := 0; n < 2000; n++ {
			f.PDU.AllData[0] = mms.NewOctetString(make([]byte, n))
			b, _ := EncodePDU(&f.PDU)
			if len(b) == apdu {
				return f
			}
		}
		t.Fatalf("APDU of %d octets not reachable (base %d)", apdu, len(base))
		return nil
	}
	if _, err := Encode(frame(1491)); err != nil {
		t.Errorf("m=1491: %v", err)
	}
	if _, err := Encode(frame(1492)); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("m=1492: %v", err)
	}
	if MaxPayload != 1499 {
		t.Errorf("MaxPayload %d", MaxPayload)
	}
}
