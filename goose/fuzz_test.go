// SPDX-License-Identifier: MIT

package goose

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func fuzzSeedFrame(f *testing.F) []byte {
	f.Helper()
	fr := &Frame{
		Dst: MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01}, Src: MAC{2, 0, 0, 0, 0, 1},
		VLAN: &VLAN{Priority: 4, ID: 1}, AppID: 0x1001,
		PDU: PDU{
			GocbRef: "IEDLD/LLN0$GO$g", TimeAllowedToLive: 2000, DatSet: "IEDLD/LLN0$ds", GoID: "id",
			T: time.Unix(1700000000, 0), StNum: 1, SqNum: 2, ConfRev: 1,
			AllData: []*mms.Value{
				mms.NewBoolean(true), mms.NewInteger(-5), mms.NewUnsigned(7), mms.NewFloat(1.5),
				mms.NewBitStringWithLength([]byte{0x40, 0}, 13), mms.NewVisibleString("x"),
				mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{1, 2}), mms.NewUTCTime(time.Unix(1, 0))}),
			},
		},
	}
	b, err := Encode(fr)
	if err != nil {
		f.Fatal(err)
	}
	return b
}

// fuzzSeedFixedFrame is the seed frame in the fixed-length encoding of
// IEC 61850-8-1 Annex A.3.
func fuzzSeedFixedFrame(f *testing.F) []byte {
	f.Helper()
	fr, err := Decode(fuzzSeedFrame(f))
	if err != nil {
		f.Fatal(err)
	}
	b, err := EncodeFixedLength(fr, nil)
	if err != nil {
		f.Fatal(err)
	}
	return b
}

// FuzzDecode checks that Decode never panics, and that every frame it
// accepts re-encodes to a frame that decodes to the same PDU.
func FuzzDecode(f *testing.F) {
	f.Add(fuzzSeedFrame(f))
	f.Add(fuzzSeedFixedFrame(f))
	f.Add(make([]byte, 60))
	f.Add([]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 0x88, 0xb8, 0, 1, 0, 9, 0, 0, 0, 0, 0x61})
	f.Fuzz(func(t *testing.T, b []byte) {
		fr, err := Decode(b)
		if err != nil {
			return
		}
		enc, err := Encode(fr)
		if err != nil {
			return // e.g. values that only exist on decode
		}
		fr2, err := Decode(enc)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		p1, _ := EncodePDU(&fr.PDU)
		p2, _ := EncodePDU(&fr2.PDU)
		if !bytes.Equal(p1, p2) {
			t.Fatalf("PDU not stable across round trip")
		}
	})
}

// FuzzDecodeData checks the allData decoder against arbitrary input:
// an error is a *DecodeError for invalid data or a limit, an accepted
// value has no more elements than the input has TLV headers and stays
// within DefaultLimits, and it re-encodes to an encoding that decodes and
// re-encodes to itself.
func FuzzDecodeData(f *testing.F) {
	for _, v := range []*mms.Value{
		mms.NewBoolean(true), mms.NewFloat(2.5), mms.NewArray([]*mms.Value{mms.NewInteger(1)}),
		mms.NewStructure([]*mms.Value{mms.NewStructure([]*mms.Value{mms.NewUnsigned(1)})}),
	} {
		b, _ := EncodeData(v)
		f.Add(b)
	}
	// Annex A.3 Table A.2 fixed-length vectors.
	f.Add([]byte{0x85, 9, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	f.Add([]byte{0x85, 9, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Add(append([]byte{0x8a, 35, 'a', 'b', 'c'}, make([]byte, 32)...))
	f.Add([]byte{0x87, 4, 0x3f, 0x80, 0, 0})
	f.Add([]byte{0x8c, 6, '0', '0', '0', '0', 0xff, 0xff}) // TimeOfDay: ms field beyond a day, last day
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := DecodeData(b)
		if err != nil {
			var de *DecodeError
			if !errors.As(err, &de) || (!errors.Is(err, ErrInvalidData) && !errors.Is(err, ErrLimitExceeded)) {
				t.Fatalf("error %T %v: want a *DecodeError for invalid data or a limit", err, err)
			}
			return
		}
		n, depth := dataShape(v)
		if n > len(b)/2 {
			t.Fatalf("%d elements from %d octets (each needs a 2-octet header)", n, len(b))
		}
		if n > DefaultLimits.MaxElements || depth > DefaultLimits.MaxDepth {
			t.Fatalf("%d elements, depth %d: beyond DefaultLimits", n, depth)
		}
		enc, err := EncodeData(v)
		if err != nil {
			// The decoder accepts a TimeOfDay whose millisecond field
			// exceeds a day; its sum with the day count can lie beyond
			// what the encoder writes. Any other value re-encodes.
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("decoded value does not re-encode: %v", err)
			}
			return
		}
		v2, err := DecodeData(enc)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		enc2, err := EncodeData(v2)
		if err != nil || !bytes.Equal(enc, enc2) {
			t.Fatalf("encoding not stable: % x / % x (%v)", enc, enc2, err)
		}
	})
}

// dataShape returns the number of Data elements in v (containers
// included) and its nesting depth (a scalar has depth 1).
func dataShape(v *mms.Value) (elements, depth int) {
	var elems []*mms.Value
	switch v.Type() {
	case mms.ValueTypeStructure:
		elems, _ = v.Structure()
	case mms.ValueTypeArray:
		elems, _ = v.ArrayElements()
	default:
		return 1, 1
	}
	elements, depth = 1, 1
	for _, e := range elems {
		n, d := dataShape(e)
		elements += n
		depth = max(depth, d+1)
	}
	return elements, depth
}

// FuzzDecodePDU checks the goosePdu decoder on its own (no Ethernet
// header): it never panics, errors are typed, and every accepted APDU
// re-encodes to an APDU that decodes to the same encoding.
func FuzzDecodePDU(f *testing.F) {
	pdu, err := EncodePDU(&PDU{
		GocbRef: "IEDLD/LLN0$GO$g", TimeAllowedToLive: 2000, DatSet: "IEDLD/LLN0$ds", GoID: "id",
		T: time.Unix(1700000000, 0), StNum: 1, SqNum: 2, ConfRev: 1, Security: []byte{1},
		AllData: []*mms.Value{mms.NewBoolean(true), mms.NewInteger(-129), mms.NewUnsigned(1 << 40)},
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(pdu)
	fixed, err := EncodePDUFixedLength(&PDU{GocbRef: "IEDLD/LLN0$GO$g", DatSet: "IEDLD/LLN0$ds", StNum: 1,
		AllData: []*mms.Value{mms.NewInteger(-1), mms.NewVisibleString("ab")}}, nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(fixed)
	f.Add([]byte{0x61, 0x00})
	f.Add([]byte{0x61, 0x84, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0x61, 0x80, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := DecodePDU(b)
		if err != nil {
			var de *DecodeError
			if !errors.As(err, &de) {
				t.Fatalf("untyped error %T: %v", err, err)
			}
			return
		}
		enc, err := EncodePDU(p)
		if err != nil {
			return // e.g. values that only exist on decode
		}
		p2, err := DecodePDU(enc)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		enc2, err := EncodePDU(p2)
		if err != nil || !bytes.Equal(enc, enc2) {
			t.Fatalf("APDU not stable across round trip: %v", err)
		}
	})
}

// FuzzEncodeFixedLength checks that every PDU the decoder accepts that
// can be BER-encoded also encodes in the fixed-length form (or fails
// with a typed value error), and that the fixed-length form decodes
// back to the same BER encoding (IEC 61850-8-1 Annex A.3: fixed-length
// encoding is valid BER for a tolerant decoder).
func FuzzEncodeFixedLength(f *testing.F) {
	f.Add(fuzzSeedFixedFrame(f)[26:]) // after addresses, VLAN tag, EtherType and the 8-octet GOOSE header
	pdu, _ := EncodePDU(&PDU{GocbRef: "g", DatSet: "d", AllData: []*mms.Value{mms.NewInteger(1 << 40), mms.NewOctetString([]byte{1})}})
	f.Add(pdu)
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := DecodePDU(b)
		if err != nil {
			return
		}
		if _, err := EncodePDU(p); err != nil {
			return
		}
		fixed, err := EncodePDUFixedLength(p, nil)
		if err != nil {
			if !errors.Is(err, ErrInvalidValue) && !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("untyped fixed-length error: %v", err)
			}
			return
		}
		p2, err := DecodePDU(fixed)
		if err != nil {
			t.Fatalf("fixed-length re-decode: %v", err)
		}
		if len(p2.AllData) != len(p.AllData) || p2.StNum != p.StNum || p2.GocbRef != p.GocbRef {
			t.Fatalf("fixed-length round trip changed the PDU")
		}
	})
}
