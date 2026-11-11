// SPDX-License-Identifier: MIT

package rsession

import (
	"bytes"
	"errors"
	"testing"
)

func testKey(id uint32, alg MACAlgorithm) Key {
	return Key{ID: id, Material: bytes.Repeat([]byte{byte(id)}, 32), MAC: alg, TimeOfCurrentKey: 100, TimeToNextKey: -1}
}

func TestRoundTrip(t *testing.T) {
	payloads := []Payload{
		{Type: PayloadGOOSE, AppID: 0x0001, APDU: tinyGOOSE},
		{Type: PayloadGOOSE, Simulation: true, AppID: 0x3fff, APDU: bytes.Repeat([]byte{0xaa}, 300)},
	}
	for _, version := range []uint16{1, 2} {
		for _, alg := range []MACAlgorithm{MACNone, MACHMACSHA256_80, MACHMACSHA256_128, MACHMACSHA256_256} {
			t.Run(alg.String(), func(t *testing.T) {
				var key *Key
				ring, _ := NewKeyRing()
				if alg != MACNone {
					k := testKey(42, alg)
					key = &k
					if err := ring.Add(k); err != nil {
						t.Fatal(err)
					}
				}
				in := &SPDU{SI: SIGOOSE, Number: 0xfffffffe, Version: version, Payloads: payloads}
				b, err := Encode(in, key)
				if err != nil {
					t.Fatal(err)
				}
				out, k, err := Verify(b, VerifyOptions{Keys: ring, AllowUnsecured: alg == MACNone})
				if err != nil {
					t.Fatal(err)
				}
				if (k == nil) != (alg == MACNone) || out.Number != in.Number || out.Version != version || len(out.Payloads) != 2 {
					t.Fatalf("decoded %+v key %v", out, k)
				}
				for i, p := range out.Payloads {
					w := payloads[i]
					if p.Type != w.Type || p.Simulation != w.Simulation || p.AppID != w.AppID || !bytes.Equal(p.APDU, w.APDU) {
						t.Fatalf("payload %d: %+v", i, p)
					}
				}
				if key != nil && (out.KeyID != 42 || out.TimeOfCurrentKey != 100 || out.TimeToNextKey != -1 || out.MAC != alg) {
					t.Fatalf("security information %+v", out)
				}
				// Decoding does not alias the input.
				b[len(b)-1] ^= 0xff
				if alg == MACNone && bytes.Equal(out.Payloads[1].APDU, b[len(b)-300:]) {
					t.Fatal("APDU aliases the datagram")
				}
			})
		}
	}
}

func TestRoundTripIV(t *testing.T) {
	b, err := Encode(&SPDU{SI: SISV, IV: []byte{1, 2, 3}, Payloads: []Payload{{Type: PayloadSV, APDU: []byte{0x60, 0}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b[3] != 23+3 {
		t.Fatalf("session header length %d", b[3])
	}
	d, err := Decode(b)
	if err != nil || !bytes.Equal(d.IV, []byte{1, 2, 3}) {
		t.Fatalf("%+v %v", d, err)
	}
}

// TestTamper flips every bit of a signed SPDU in turn: none verifies.
func TestTamper(t *testing.T) {
	k := testKey(5, MACHMACSHA256_128)
	ring, _ := NewKeyRing(k)
	b, err := Encode(&SPDU{SI: SIGOOSE, Number: 3, Payloads: []Payload{{Type: PayloadGOOSE, AppID: 1, APDU: tinyGOOSE}}}, &k)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Verify(b, VerifyOptions{Keys: ring}); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		for bit := range 8 {
			c := append([]byte{}, b...)
			c[i] ^= 1 << bit
			if _, _, err := Verify(c, VerifyOptions{Keys: ring, AllowUnsecured: true}); err == nil {
				t.Fatalf("octet %d bit %d: tampered SPDU verified", i, bit)
			}
		}
	}
	for n := range len(b) {
		if _, _, err := Verify(b[:n], VerifyOptions{Keys: ring, AllowUnsecured: true}); err == nil {
			t.Fatalf("truncated to %d octets: verified", n)
		}
	}
}

func TestVerifyRefusals(t *testing.T) {
	k := testKey(5, MACHMACSHA256_128)
	other := testKey(5, MACHMACSHA256_80)
	ring, _ := NewKeyRing(k)
	otherRing, _ := NewKeyRing(other)
	s := &SPDU{SI: SIGOOSE, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}
	signed, _ := Encode(s, &k)
	s1 := *s
	s1.Version = 1
	signedV1, _ := Encode(&s1, &k)
	unsigned, _ := Encode(s, nil)

	wrongKey := testKey(5, MACHMACSHA256_128)
	wrongKey.Material = bytes.Repeat([]byte{9}, 32)
	wrongRing, _ := NewKeyRing(wrongKey)

	noSig := append([]byte{}, unsigned...)
	noSig[25] = 5 // key ID 5, no signature

	sigNoKey := append(append([]byte{}, unsigned...), 0x85, 1, 0)

	for _, tc := range []struct {
		name string
		b    []byte
		opts VerifyOptions
		want error
	}{
		{"unknown key", signed, VerifyOptions{}, ErrUnknownKey},
		{"wrong material", signed, VerifyOptions{Keys: wrongRing}, ErrAuthentication},
		{"v2 algorithm differs", signed, VerifyOptions{Keys: otherRing}, ErrAuthentication},
		{"v1 algorithm differs", signedV1, VerifyOptions{Keys: otherRing}, ErrAuthentication},
		{"no signature", noSig, VerifyOptions{Keys: ring}, ErrAuthentication},
		{"signature on unsecured", sigNoKey, VerifyOptions{AllowUnsecured: true}, ErrAuthentication},
		{"unsupported key algorithm", signed, VerifyOptions{Keys: staticProvider{&Key{ID: 5, MAC: MACAESGMAC128}}}, ErrUnsupported},
		{"malformed", []byte{1}, VerifyOptions{}, ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Verify(tc.b, tc.opts); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

type staticProvider struct{ k *Key }

func (p staticProvider) CurrentKey() (*Key, error)        { return p.k, nil }
func (p staticProvider) LookupKey(id uint32) (*Key, bool) { return p.k, p.k != nil && p.k.ID == id }

func TestDecodeMalformed(t *testing.T) {
	good, _ := Encode(&SPDU{SI: SIGOOSE, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}, nil)
	mut := func(f func(b []byte) []byte) []byte { return f(append([]byte{}, good...)) }
	for _, tc := range []struct {
		name string
		b    []byte
		want error
	}{
		{"too short", good[:10], ErrMalformed},
		{"too long", make([]byte, MaxSPDUSize+1), ErrMalformed},
		{"transport", mut(func(b []byte) []byte { b[1] = 0x41; return b }), ErrMalformed},
		{"session identifier", mut(func(b []byte) []byte { b[2] = 0xA4; return b }), ErrMalformed},
		{"session header length small", mut(func(b []byte) []byte { b[3] = 11; return b }), ErrMalformed},
		{"session header length large", mut(func(b []byte) []byte { b[3] = 200; return b }), ErrMalformed},
		{"common tag", mut(func(b []byte) []byte { b[4] = 0x81; return b }), ErrMalformed},
		{"common length small", mut(func(b []byte) []byte { b[5] = 9; return b }), ErrMalformed},
		{"common length large", mut(func(b []byte) []byte { b[5] = 200; return b }), ErrMalformed},
		{"version", mut(func(b []byte) []byte { b[15] = 3; return b }), ErrUnsupported},
		{"v1 truncated", mut(func(b []byte) []byte { b[15] = 1; return b[:24] }), ErrMalformed},
		{"v2 truncated", good[:24], ErrMalformed},
		{"IV runs past", mut(func(b []byte) []byte { b[26] = 200; return b }), ErrMalformed},
		{"no payload length", good[:29], ErrMalformed},
		{"payload length runs past", mut(func(b []byte) []byte { b[30] = 200; return b }), ErrMalformed},
		{"truncated element header", mut(func(b []byte) []byte { b[30] = 3; return b }), ErrMalformed},
		{"simulation octet", mut(func(b []byte) []byte { b[32] = 2; return b }), ErrMalformed},
		{"APDU length", mut(func(b []byte) []byte { b[36] = 4; b[30] = 10; return b }), ErrMalformed},
		{"trailing octets", append(mut(func(b []byte) []byte { return b }), 0x00), ErrMalformed},
		{"padding truncated", append(mut(func(b []byte) []byte { return b }), 0xAF), ErrMalformed},
		{"padding runs past", append(mut(func(b []byte) []byte { return b }), 0xAF, 5, 0), ErrMalformed},
		{"signature truncated", append(mut(func(b []byte) []byte { return b }), 0x85), ErrMalformed},
		{"signature empty", append(mut(func(b []byte) []byte { return b }), 0x85, 0), ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.b)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			var de *DecodeError
			if !errors.As(err, &de) || de.Error() == "" {
				t.Fatalf("not a DecodeError: %v", err)
			}
		})
	}
}

func TestEncodeErrors(t *testing.T) {
	p := []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}
	bad := testKey(1, MACHMACSHA256_80)
	bad.Material = bad.Material[:8]
	for _, tc := range []struct {
		name string
		s    *SPDU
		key  *Key
		want error
	}{
		{"nil", nil, nil, ErrInvalidConfig},
		{"version", &SPDU{Version: 3, Payloads: p}, nil, ErrUnsupported},
		{"v1 IV", &SPDU{Version: 1, IV: []byte{1}, Payloads: p}, nil, ErrUnsupported},
		{"IV too long", &SPDU{IV: make([]byte, 256), Payloads: p}, nil, ErrLimitExceeded},
		{"APDU too long", &SPDU{Payloads: []Payload{{APDU: make([]byte, 0x10000)}}}, nil, ErrLimitExceeded},
		{"SPDU too long", &SPDU{Payloads: []Payload{{APDU: make([]byte, 0xffff)}, {APDU: make([]byte, 10)}}}, nil, ErrLimitExceeded},
		{"short key", &SPDU{Payloads: p}, &bad, ErrInvalidKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Encode(tc.s, tc.key); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestKeyRing(t *testing.T) {
	r, err := NewKeyRing(testKey(1, MACHMACSHA256_128), testKey(2, MACHMACSHA256_256))
	if err != nil {
		t.Fatal(err)
	}
	if k, err := r.CurrentKey(); err != nil || k.ID != 1 {
		t.Fatalf("current %v %v", k, err)
	}
	if err := r.SetCurrent(2); err != nil {
		t.Fatal(err)
	}
	if err := r.SetCurrent(3); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("SetCurrent(3): %v", err)
	}
	if k, _ := r.CurrentKey(); k.ID != 2 {
		t.Fatalf("current %d", k.ID)
	}
	if _, ok := r.LookupKey(1); !ok {
		t.Fatal("key 1 missing")
	}
	r.Remove(2)
	if _, err := r.CurrentKey(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("after removing the current key: %v", err)
	}
	r.Remove(1)
	if _, ok := r.LookupKey(1); ok {
		t.Fatal("key 1 still present")
	}
	if err := r.Add(testKey(1, MACHMACSHA256_80)); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(testKey(1, MACHMACSHA256_80)); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("duplicate: %v", err)
	}
	// Add copies the material.
	k := testKey(3, MACHMACSHA256_80)
	_ = r.Add(k)
	k.Material[0] ^= 0xff
	if got, _ := r.LookupKey(3); got.Material[0] == k.Material[0] {
		t.Fatal("material aliases the caller's slice")
	}
	var zero KeyRing
	if err := zero.Add(testKey(4, MACHMACSHA256_80)); err != nil {
		t.Fatal(err)
	}
	for _, k := range []Key{{ID: 0, Material: make([]byte, 16), MAC: MACHMACSHA256_80}, {ID: 1, Material: make([]byte, 16), MAC: MACAESGMAC64}} {
		if _, err := NewKeyRing(k); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("%+v: %v", k, err)
		}
	}
	var nilKey *Key
	if err := nilKey.validate(); !errors.Is(err, ErrInvalidKey) {
		t.Fatal(err)
	}
}

func TestStrings(t *testing.T) {
	for _, s := range []SessionID{SITunnelled, SIGOOSE, SISV, SIManagement, 0x10} {
		if s.String() == "" {
			t.Fatal(s)
		}
	}
	for _, a := range []MACAlgorithm{MACNone, MACHMACSHA256_80, MACHMACSHA256_128, MACHMACSHA256_256, MACAESGMAC64, MACAESGMAC128, 99} {
		if a.String() == "" {
			t.Fatal(a)
		}
	}
	if MACAESGMAC64.Size() != 0 {
		t.Fatal("GMAC size")
	}
}
