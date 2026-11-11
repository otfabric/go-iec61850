// SPDX-License-Identifier: MIT

package rsession

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sync"
)

// MACAlgorithm identifies the signature algorithm of an SPDU. The values
// are the code points of the IEC TR 61850-90-5 security algorithm table
// (carried on the wire in protocol version 1).
type MACAlgorithm uint8

// MAC algorithms. Only the HMAC-SHA256 variants are implemented; an SPDU
// that uses another one is refused with [ErrUnsupported].
const (
	MACNone          MACAlgorithm = 0
	MACHMACSHA256_80 MACAlgorithm = 1 // HMAC-SHA256 truncated to 80 bits
	// MACHMACSHA256_128 is HMAC-SHA256 truncated to 128 bits.
	MACHMACSHA256_128 MACAlgorithm = 2
	// MACHMACSHA256_256 is HMAC-SHA256, untruncated.
	MACHMACSHA256_256 MACAlgorithm = 3
	// MACAESGMAC64 (AES-GMAC, 64 bits) is not implemented.
	MACAESGMAC64 MACAlgorithm = 4
	// MACAESGMAC128 (AES-GMAC, 128 bits) is not implemented.
	MACAESGMAC128 MACAlgorithm = 5
)

// Size returns the MAC length in octets, or 0 for [MACNone] and the
// algorithms this package does not implement.
func (a MACAlgorithm) Size() int {
	switch a {
	case MACHMACSHA256_80:
		return 10
	case MACHMACSHA256_128:
		return 16
	case MACHMACSHA256_256:
		return 32
	default:
		return 0
	}
}

func (a MACAlgorithm) String() string {
	switch a {
	case MACNone:
		return "none"
	case MACHMACSHA256_80:
		return "HMAC-SHA256-80"
	case MACHMACSHA256_128:
		return "HMAC-SHA256-128"
	case MACHMACSHA256_256:
		return "HMAC-SHA256-256"
	case MACAESGMAC64:
		return "AES-GMAC-64"
	case MACAESGMAC128:
		return "AES-GMAC-128"
	default:
		return fmt.Sprintf("MACAlgorithm(%d)", uint8(a))
	}
}

// EncryptionAlgorithm identifies the encryption algorithm octet of a
// protocol version 1 SPDU. Encryption is not implemented: an SPDU with a
// value other than [EncryptionNone] is refused with [ErrUnsupported].
type EncryptionAlgorithm uint8

// Encryption algorithms (IEC TR 61850-90-5 code points).
const (
	EncryptionNone   EncryptionAlgorithm = 0
	EncryptionAES128 EncryptionAlgorithm = 1 // AES-128-GCM, not implemented
	EncryptionAES256 EncryptionAlgorithm = 2 // AES-256-GCM, not implemented
)

// MinKeySize is the minimum key material length accepted, in octets.
const MinKeySize = 16

// Key is a symmetric key that secures SPDUs.
type Key struct {
	// ID identifies the key in the SPDU header. 0 is reserved for
	// unsecured SPDUs.
	ID uint32

	// Material is the secret, at least [MinKeySize] octets.
	Material []byte

	// MAC is the signature algorithm used with this key. In protocol
	// version 2 the algorithm is a property of the key ID and not
	// carried on the wire.
	MAC MACAlgorithm

	// TimeOfCurrentKey and TimeToNextKey are sent verbatim in the
	// security information of every SPDU secured with this key. Their
	// meaning (when the key became current, how long until the next
	// one) belongs to the key management in use.
	TimeOfCurrentKey uint32
	TimeToNextKey    int16
}

func (k *Key) validate() error {
	if k == nil {
		return fmt.Errorf("%w: nil key", ErrInvalidKey)
	}
	if k.ID == 0 {
		return fmt.Errorf("%w: key ID 0 is reserved for unsecured SPDUs", ErrInvalidKey)
	}
	if len(k.Material) < MinKeySize {
		return fmt.Errorf("%w: key %d has %d octets of material, at least %d required", ErrInvalidKey, k.ID, len(k.Material), MinKeySize)
	}
	if k.MAC.Size() == 0 {
		return fmt.Errorf("%w: key %d: MAC algorithm %v not supported", ErrInvalidKey, k.ID, k.MAC)
	}
	return nil
}

// mac returns the truncated HMAC-SHA256 of data under k.
func (k *Key) mac(data []byte) []byte {
	m := hmac.New(sha256.New, k.Material)
	m.Write(data)
	return m.Sum(nil)[:k.MAC.Size()]
}

// KeyProvider supplies the keys of a [Transport]. Implementations must
// be safe for concurrent use; they are called for every SPDU sent and
// received, so they should not block.
//
// An application with its own key distribution (for example IEC 62351-9
// GDOI) implements KeyProvider over it; [KeyRing] is a static provider.
type KeyProvider interface {
	// CurrentKey returns the key that secures the next SPDU sent, or
	// an error wrapping [ErrNoKey].
	CurrentKey() (*Key, error)

	// LookupKey returns the key with the given ID, used to verify a
	// received SPDU, and whether it is known.
	LookupKey(id uint32) (*Key, bool)
}

// KeyRing is a static, concurrency-safe [KeyProvider]. It holds any
// number of keys, all accepted on receipt, and one current key used to
// send. A key change without loss is: Add the new key at every receiver,
// SetCurrent at the sender, then Remove the old key.
type KeyRing struct {
	mu      sync.RWMutex
	keys    map[uint32]*Key
	current uint32
}

// NewKeyRing returns a key ring holding keys; the first one is current.
// It fails with [ErrInvalidKey] for an invalid or duplicate key.
func NewKeyRing(keys ...Key) (*KeyRing, error) {
	r := &KeyRing{keys: make(map[uint32]*Key)}
	for _, k := range keys {
		if err := r.Add(k); err != nil {
			return nil, err
		}
	}
	if len(keys) > 0 {
		r.current = keys[0].ID
	}
	return r, nil
}

// Add adds a copy of k. It fails with [ErrInvalidKey] if k is invalid
// or its ID is already present.
func (r *KeyRing) Add(k Key) error {
	if err := k.validate(); err != nil {
		return err
	}
	k.Material = append([]byte(nil), k.Material...)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys == nil {
		r.keys = make(map[uint32]*Key)
	}
	if _, ok := r.keys[k.ID]; ok {
		return fmt.Errorf("%w: duplicate key ID %d", ErrInvalidKey, k.ID)
	}
	r.keys[k.ID] = &k
	return nil
}

// Remove removes the key with the given ID. Removing the current key
// leaves the ring without one until [KeyRing.SetCurrent].
func (r *KeyRing) Remove(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.keys, id)
	if r.current == id {
		r.current = 0
	}
}

// SetCurrent makes the key with the given ID the one used to send. It
// fails with [ErrUnknownKey] if the ring does not hold it.
func (r *KeyRing) SetCurrent(id uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.keys[id]; !ok {
		return fmt.Errorf("%w: %d", ErrUnknownKey, id)
	}
	r.current = id
	return nil
}

// CurrentKey implements [KeyProvider].
func (r *KeyRing) CurrentKey() (*Key, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if k, ok := r.keys[r.current]; ok && r.current != 0 {
		return k, nil
	}
	return nil, ErrNoKey
}

// LookupKey implements [KeyProvider].
func (r *KeyRing) LookupKey(id uint32) (*Key, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.keys[id]
	return k, ok
}
