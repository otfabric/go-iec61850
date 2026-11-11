// SPDX-License-Identifier: MIT

package rsession

import (
	"bytes"
	"testing"
)

func fuzzSeeds(f *testing.F) *KeyRing {
	k := testKey(1, MACHMACSHA256_128)
	ring, _ := NewKeyRing(k)
	p := []Payload{{Type: PayloadGOOSE, AppID: 1, APDU: tinyGOOSE}}
	for _, v := range []uint16{1, 2} {
		for _, key := range []*Key{nil, &k} {
			b, _ := Encode(&SPDU{SI: SIGOOSE, Version: v, Number: 7, Payloads: p}, key)
			f.Add(b)
		}
	}
	b, _ := Encode(&SPDU{SI: SISV, IV: []byte{1, 2}, Payloads: append(p, p...)}, nil)
	f.Add(b)
	f.Add([]byte{0x01, 0x40, 0xA1, 0x18, 0x80, 0x12})
	return ring
}

// FuzzDecode checks that Decode never panics and that what it accepts
// re-encodes to an SPDU that decodes to the same payloads.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Decode(b)
		if err != nil {
			return
		}
		if s.Version == 1 && len(s.IV) > 0 {
			t.Fatal("version 1 with an IV")
		}
		out, err := Encode(s, nil)
		if err != nil {
			return // e.g. an SPDU that only fits with its original header lengths
		}
		s2, err := Decode(out)
		if err != nil {
			t.Fatalf("re-encoded SPDU does not decode: %v", err)
		}
		if len(s2.Payloads) != len(s.Payloads) {
			t.Fatalf("%d payloads, then %d", len(s.Payloads), len(s2.Payloads))
		}
		for i := range s.Payloads {
			if !bytes.Equal(s.Payloads[i].APDU, s2.Payloads[i].APDU) || s.Payloads[i].AppID != s2.Payloads[i].AppID {
				t.Fatalf("payload %d changed", i)
			}
		}
	})
}

// FuzzVerify checks that Verify never panics and accepts a secured SPDU
// only with a valid signature.
func FuzzVerify(f *testing.F) {
	ring := fuzzSeeds(f)
	k, _ := ring.LookupKey(1)
	f.Fuzz(func(t *testing.T, b []byte) {
		s, key, err := Verify(b, VerifyOptions{Keys: ring, AllowUnsecured: true})
		if err != nil || key == nil {
			return
		}
		if !bytes.Equal(s.Signature, k.mac(b[:len(b)-2-len(s.Signature)])) {
			t.Fatal("accepted an SPDU whose signature is not the MAC of its content")
		}
	})
}
