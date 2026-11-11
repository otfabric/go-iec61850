// SPDX-License-Identifier: MIT

package rsession

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The vectors below are built by hand from the field table in
// docs/adr/0001-routable-goose-sv.md (IEC TR 61850-90-5 clause 11
// structure); the MACs are computed with crypto/hmac independently of the
// package.

// unhex decodes the leading two-digit hex octets of every line; the rest
// of a line is a comment.
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	var out []byte
	for _, line := range strings.Split(s, "\n") {
		for _, f := range strings.Fields(line) {
			b, err := hex.DecodeString(f)
			if err != nil || len(b) != 1 {
				break
			}
			out = append(out, b[0])
		}
	}
	return out
}

// tinyGOOSE is a five-octet [APPLICATION 1] APDU: enough for the session
// layer, which does not look inside it.
var tinyGOOSE = []byte{0x61, 0x03, 0x80, 0x01, 0x41}

// TestSpec_UnsecuredV2 is an unsecured version 2 GOOSE SPDU.
func TestSpec_UnsecuredV2(t *testing.T) {
	want := unhex(t, `
		01 40          transport: LI, UD TPDU
		A1 17          SI GOOSE, session header length 23 = 2 + 10 + 11
		80 0A          common session header, length 10
		00 00 00 20    SPDU length: 32 octets follow
		00 00 00 01    SPDU number 1
		00 02          version 2
		00 00 00 00    TimeOfCurrentKey
		00 00          TimeToNextKey
		00 00 00 00    key ID 0: unsecured
		00             IV length 0
		00 00 00 0B    payload length 11
		81 00 00 01 00 05  GOOSE, not simulated, APPID 1, APDU length 5
		61 03 80 01 41`)
	s := &SPDU{SI: SIGOOSE, Number: 1, Payloads: []Payload{{Type: PayloadGOOSE, AppID: 1, APDU: tinyGOOSE}}}
	got, err := Encode(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoding\n got %x\nwant %x", got, want)
	}
	d, err := Decode(want)
	if err != nil {
		t.Fatal(err)
	}
	if d.SI != SIGOOSE || d.Number != 1 || d.Version != 2 || d.KeyID != 0 || d.Length != 32 || d.Signature != nil ||
		len(d.Payloads) != 1 || d.Payloads[0].AppID != 1 || d.Payloads[0].Simulation || !bytes.Equal(d.Payloads[0].APDU, tinyGOOSE) {
		t.Fatalf("decoded %+v", d)
	}
	if _, _, err := Verify(want, VerifyOptions{}); !errors.Is(err, ErrUnsecured) {
		t.Fatalf("Verify without AllowUnsecured: %v", err)
	}
	if _, k, err := Verify(want, VerifyOptions{AllowUnsecured: true}); err != nil || k != nil {
		t.Fatalf("Verify with AllowUnsecured: %v %v", k, err)
	}
}

// TestSpec_SignedV1 is a version 1 SV SPDU with HMAC-SHA256-128: the
// algorithm octets are on the wire, the MAC covers every octet before the
// signature tag.
func TestSpec_SignedV1(t *testing.T) {
	material := bytes.Repeat([]byte{0x5a}, 32)
	key := &Key{ID: 7, Material: material, MAC: MACHMACSHA256_128, TimeOfCurrentKey: 0x01020304, TimeToNextKey: 0x0010}
	covered := unhex(t, `
		01 40
		A2 18          SI SV, session header length 24 = 2 + 10 + 12
		80 0A
		00 00 00 32    SPDU length: 50 octets follow
		00 00 01 00    SPDU number 256
		00 01          version 1
		01 02 03 04    TimeOfCurrentKey
		00 10          TimeToNextKey
		00 02          encryption none, MAC HMAC-SHA256-128
		00 00 00 07    key ID 7
		00 00 00 0A    payload length 10
		82 01 40 00 00 04  SV, simulated, APPID 0x4000, APDU length 4
		60 02 80 00`)
	m := hmac.New(sha256.New, material)
	m.Write(covered)
	want := append(append(append([]byte{}, covered...), 0x85, 0x10), m.Sum(nil)[:16]...)

	s := &SPDU{SI: SISV, Number: 256, Version: 1, Payloads: []Payload{{Type: PayloadSV, Simulation: true, AppID: 0x4000, APDU: []byte{0x60, 0x02, 0x80, 0x00}}}}
	got, err := Encode(s, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoding\n got %x\nwant %x", got, want)
	}
	ring, _ := NewKeyRing(*key)
	d, k, err := Verify(want, VerifyOptions{Keys: ring})
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != 7 || d.MAC != MACHMACSHA256_128 || d.TimeOfCurrentKey != 0x01020304 || d.TimeToNextKey != 16 || !d.Payloads[0].Simulation {
		t.Fatalf("decoded %+v", d)
	}
}

// TestSpec_SignedV2Sizes checks the trailer of each implemented MAC
// algorithm in version 2, where the algorithm is the key's.
func TestSpec_SignedV2Sizes(t *testing.T) {
	for _, tc := range []struct {
		alg  MACAlgorithm
		size int
	}{{MACHMACSHA256_80, 10}, {MACHMACSHA256_128, 16}, {MACHMACSHA256_256, 32}} {
		t.Run(tc.alg.String(), func(t *testing.T) {
			key := Key{ID: 0x01020304, Material: bytes.Repeat([]byte{1}, 16), MAC: tc.alg}
			b, err := Encode(&SPDU{SI: SIGOOSE, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}, &key)
			if err != nil {
				t.Fatal(err)
			}
			// 4 + 23 + 4 + 11 octets before the trailer.
			if len(b) != 42+2+tc.size || b[42] != 0x85 || int(b[43]) != tc.size {
				t.Fatalf("trailer %x", b[40:])
			}
			if !bytes.Equal(b[22:26], []byte{1, 2, 3, 4}) {
				t.Fatalf("key ID %x", b[22:26])
			}
			if spduLen := int(b[6])<<24 | int(b[7])<<16 | int(b[8])<<8 | int(b[9]); spduLen != len(b)-10 {
				t.Fatalf("SPDU length %d, %d octets follow", spduLen, len(b)-10)
			}
			m := hmac.New(sha256.New, key.Material)
			m.Write(b[:42])
			if !bytes.Equal(b[44:], m.Sum(nil)[:tc.size]) {
				t.Fatal("MAC does not cover the SPDU up to the signature tag")
			}
		})
	}
}

// TestSpec_LibIEC61850Layout decodes what libiec61850 writes: fixed
// header lengths 24 and 18, SPDU length 20 plus the payload, and an APDU
// length two more than the APDU.
func TestSpec_LibIEC61850Layout(t *testing.T) {
	b := unhex(t, `
		01 40 A1 18 80 12
		00 00 00 1F    SPDU length (20 + 11)
		00 00 00 05
		00 02
		00 00 00 00 00 00
		00 00 00 00 00
		00 00 00 0B
		81 00 00 01 00 07
		61 03 80 01 41`)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.Payloads[0].APDU, tinyGOOSE) || d.Number != 5 {
		t.Fatalf("decoded %+v", d)
	}
	// Two more than that is not accepted, nor the +2 on an element
	// that is not the last.
	bad := append([]byte{}, b...)
	bad[36] = 0x09
	if _, err := Decode(bad); !errors.Is(err, ErrMalformed) {
		t.Fatalf("APDU length +4: %v", err)
	}
}

// TestSpec_LibIEC61850HMAC256Length refuses a 32-octet MAC behind a
// length octet of 16, as libiec61850 writes HMAC-SHA256-256.
func TestSpec_LibIEC61850HMAC256Length(t *testing.T) {
	key := Key{ID: 1, Material: bytes.Repeat([]byte{2}, 32), MAC: MACHMACSHA256_256}
	b, err := Encode(&SPDU{SI: SIGOOSE, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}, &key)
	if err != nil {
		t.Fatal(err)
	}
	b[43] = 16
	if _, err := Decode(b); !errors.Is(err, ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

// TestSpec_Padding accepts the padding element between the user data and
// the signature, inside the MAC.
func TestSpec_Padding(t *testing.T) {
	key := Key{ID: 9, Material: bytes.Repeat([]byte{3}, 16), MAC: MACHMACSHA256_80}
	b, err := Encode(&SPDU{SI: SIGOOSE, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Re-key by hand: key ID at 22..25, then pad and sign.
	b[22], b[23], b[24], b[25] = 0, 0, 0, 9
	b = append(b, 0xAF, 0x03, 0, 0, 0)
	m := hmac.New(sha256.New, key.Material)
	m.Write(b)
	b = append(append(b, 0x85, 10), m.Sum(nil)[:10]...)
	ring, _ := NewKeyRing(key)
	if _, _, err := Verify(b, VerifyOptions{Keys: ring}); err != nil {
		t.Fatal(err)
	}
}

// TestSpec_EncryptedV1Refused refuses a version 1 SPDU whose encryption
// octet is set.
func TestSpec_EncryptedV1Refused(t *testing.T) {
	b, err := Encode(&SPDU{SI: SIGOOSE, Version: 1, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b[22] = byte(EncryptionAES128)
	if _, _, err := Verify(b, VerifyOptions{AllowUnsecured: true}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
	b[22], b[23] = 0, byte(MACAESGMAC64)
	if _, _, err := Verify(b, VerifyOptions{AllowUnsecured: true}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("GMAC: got %v", err)
	}
}
