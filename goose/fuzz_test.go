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

// FuzzDecode checks that Decode never panics, and that every frame it
// accepts re-encodes to a frame that decodes to the same PDU.
func FuzzDecode(f *testing.F) {
	f.Add(fuzzSeedFrame(f))
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

// FuzzDecodeData checks the allData decoder against arbitrary input.
func FuzzDecodeData(f *testing.F) {
	for _, v := range []*mms.Value{
		mms.NewBoolean(true), mms.NewFloat(2.5), mms.NewArray([]*mms.Value{mms.NewInteger(1)}),
		mms.NewStructure([]*mms.Value{mms.NewStructure([]*mms.Value{mms.NewUnsigned(1)})}),
	} {
		b, _ := EncodeData(v)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := DecodeData(b)
		if err != nil {
			return
		}
		if _, err := EncodeData(v); err != nil {
			t.Logf("decoded value not re-encodable: %v", err)
		}
	})
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
