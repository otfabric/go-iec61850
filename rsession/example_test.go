// SPDX-License-Identifier: MIT

package rsession_test

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/otfabric/go-iec61850/rsession"
)

// Example signs an SPDU and verifies it; a receiver without the key, or
// one that is given a tampered copy, refuses it.
func Example() {
	keys, err := rsession.NewKeyRing(rsession.Key{
		ID: 1, Material: bytes.Repeat([]byte{0x42}, 32), MAC: rsession.MACHMACSHA256_128,
	})
	if err != nil {
		panic(err)
	}
	key, _ := keys.CurrentKey()
	b, err := rsession.Encode(&rsession.SPDU{
		SI: rsession.SIGOOSE, Number: 1,
		Payloads: []rsession.Payload{{Type: rsession.PayloadGOOSE, AppID: 1, APDU: []byte{0x61, 0x00}}},
	}, key)
	if err != nil {
		panic(err)
	}

	s, k, err := rsession.Verify(b, rsession.VerifyOptions{Keys: keys})
	fmt.Println(s.SI, s.Number, k.MAC, err)

	_, _, err = rsession.Verify(b, rsession.VerifyOptions{})
	fmt.Println(errors.Is(err, rsession.ErrUnknownKey))

	b[len(b)-20] ^= 1
	_, _, err = rsession.Verify(b, rsession.VerifyOptions{Keys: keys})
	fmt.Println(errors.Is(err, rsession.ErrAuthentication))
	// Output:
	// GOOSE 1 HMAC-SHA256-128 <nil>
	// true
	// true
}
