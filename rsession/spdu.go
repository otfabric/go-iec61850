// SPDX-License-Identifier: MIT

package rsession

import (
	"crypto/hmac"
	"encoding/binary"
	"fmt"
)

// SessionID is the session identifier (SI) of an SPDU: the kind of
// application data it carries.
type SessionID uint8

// Session identifiers (IEC TR 61850-90-5). The transport serves GOOSE and
// SV; tunnelled and management SPDUs are decoded but not served.
const (
	SITunnelled  SessionID = 0xA0
	SIGOOSE      SessionID = 0xA1
	SISV         SessionID = 0xA2
	SIManagement SessionID = 0xA3
)

func (s SessionID) String() string {
	switch s {
	case SITunnelled:
		return "tunnelled"
	case SIGOOSE:
		return "GOOSE"
	case SISV:
		return "SV"
	case SIManagement:
		return "management"
	default:
		return fmt.Sprintf("SessionID(0x%02X)", uint8(s))
	}
}

// PayloadType tags one APDU in the session user data.
type PayloadType uint8

// Payload types of GOOSE and SV APDUs.
const (
	PayloadGOOSE PayloadType = 0x81
	PayloadSV    PayloadType = 0x82
)

// Wire constants (IEC TR 61850-90-5 clause 11; see docs/adr/0001).
const (
	cltpLI       = 0x01 // connectionless transport: length indicator
	cltpUD       = 0x40 // connectionless transport: UD TPDU code
	tagCommon    = 0x80 // common session header parameter identifier
	commonLen    = 10   // SPDU length, SPDU number, version
	tagSignature = 0x85 // signature after the user data
	tagPadding   = 0xAF // padding before the signature
	elementHdr   = 6    // payload type, simulation, APPID, APDU length
	fixedHdr     = 16   // up to and including the version field
	secInfoV1    = 12   // TimeOfCurrentKey, TimeToNextKey, algorithms, key ID
	secInfoV2    = 11   // TimeOfCurrentKey, TimeToNextKey, key ID, IV length
)

// MaxSPDUSize is the largest SPDU encoded or decoded: the largest UDP
// payload over IPv4.
const MaxSPDUSize = 65507

// Payload is one application PDU in an SPDU: a goosePdu or savPdu without
// its layer-2 header, whose APPID and simulation bit the session header
// carries instead.
type Payload struct {
	Type       PayloadType
	Simulation bool
	AppID      uint16
	APDU       []byte
}

// SPDU is a session protocol data unit.
type SPDU struct {
	// SI is the session identifier.
	SI SessionID

	// Number is the SPDU number, which a sender increments for every
	// SPDU.
	Number uint32

	// Version is the session protocol version, 1 or 2. Encode treats 0
	// as 2.
	Version uint16

	// TimeOfCurrentKey, TimeToNextKey and KeyID are the security
	// information. Encode takes them from the key and ignores these.
	TimeOfCurrentKey uint32
	TimeToNextKey    int16
	KeyID            uint32

	// Encryption and MAC are the algorithm octets of a version 1 SPDU
	// as decoded. Encode takes the MAC algorithm from the key.
	Encryption EncryptionAlgorithm
	MAC        MACAlgorithm

	// IV is the initialisation vector of a version 2 SPDU (empty unless
	// a peer encrypts). Encode sends it as given.
	IV []byte

	// Payloads are the APDUs carried.
	Payloads []Payload

	// Length is the SPDU length field as decoded. It is not checked:
	// implementations disagree on what it counts (docs/adr/0001).
	Length uint32

	// Signature is the signature value as decoded, nil when absent.
	Signature []byte

	// signed is the number of octets covered by the signature (the
	// offset of the signature tag); set by Decode.
	signed int
}

// Encode returns the wire encoding of s, secured with key; key nil sends
// it unsecured (key ID 0, no signature).
func Encode(s *SPDU, key *Key) ([]byte, error) { return AppendSPDU(nil, s, key) }

// AppendSPDU appends the wire encoding of s, secured with key (nil for
// unsecured), to dst. The layout, in network byte order:
//
//	01 40                          connectionless transport header
//	SI LI                          session identifier, session header length
//	80 0A length(4) number(4) version(2)
//	v1: timeOfCurrentKey(4) timeToNextKey(2) encryption(1) mac(1) keyID(4)
//	v2: timeOfCurrentKey(4) timeToNextKey(2) keyID(4) ivLength(1) iv
//	payloadLength(4)
//	{ type(1) simulation(1) APPID(2) APDULength(2) APDU }...
//	85 len MAC                     when secured
//
// The session header length counts the octets after it to the end of the
// security information; the SPDU length counts the octets after it to the
// end of the SPDU. The MAC covers everything before its tag.
func AppendSPDU(dst []byte, s *SPDU, key *Key) ([]byte, error) {
	if s == nil {
		return dst, fmt.Errorf("%w: rsession: nil SPDU", ErrInvalidConfig)
	}
	version := s.Version
	if version == 0 {
		version = 2
	}
	if version != 1 && version != 2 {
		return dst, fmt.Errorf("%w: protocol version %d", ErrUnsupported, version)
	}
	if key != nil {
		if err := key.validate(); err != nil {
			return dst, err
		}
	}
	if version == 1 && len(s.IV) > 0 {
		return dst, fmt.Errorf("%w: protocol version 1 has no initialisation vector", ErrUnsupported)
	}
	if len(s.IV) > 0xff {
		return dst, fmt.Errorf("%w: initialisation vector of %d octets", ErrLimitExceeded, len(s.IV))
	}
	user := 0
	for _, p := range s.Payloads {
		if len(p.APDU) > 0xffff {
			return dst, fmt.Errorf("%w: APDU of %d octets exceeds the 16-bit APDU length", ErrLimitExceeded, len(p.APDU))
		}
		user += elementHdr + len(p.APDU)
	}
	secInfo := secInfoV1
	if version == 2 {
		secInfo = secInfoV2 + len(s.IV)
	}
	trailer := 0
	if key != nil {
		trailer = 2 + key.MAC.Size()
	}
	sessionHdr := 2 + commonLen + secInfo
	total := 4 + sessionHdr + 4 + user + trailer
	if total > MaxSPDUSize {
		return dst, fmt.Errorf("%w: SPDU of %d octets exceeds %d", ErrLimitExceeded, total, MaxSPDUSize)
	}
	start := len(dst)
	dst = append(dst, cltpLI, cltpUD, byte(s.SI), byte(sessionHdr), tagCommon, commonLen)
	dst = binary.BigEndian.AppendUint32(dst, uint32(total-10)) // octets after the SPDU length field
	dst = binary.BigEndian.AppendUint32(dst, s.Number)
	dst = binary.BigEndian.AppendUint16(dst, version)
	var keyID, tock uint32
	var ttnk int16
	mac := MACNone
	if key != nil {
		keyID, tock, ttnk, mac = key.ID, key.TimeOfCurrentKey, key.TimeToNextKey, key.MAC
	}
	dst = binary.BigEndian.AppendUint32(dst, tock)
	dst = binary.BigEndian.AppendUint16(dst, int16ToWire(ttnk))
	if version == 1 {
		dst = append(dst, byte(EncryptionNone), byte(mac))
		dst = binary.BigEndian.AppendUint32(dst, keyID)
	} else {
		dst = binary.BigEndian.AppendUint32(dst, keyID)
		dst = append(dst, u8(len(s.IV)))
		dst = append(dst, s.IV...)
	}
	dst = binary.BigEndian.AppendUint32(dst, uint32(user))
	for _, p := range s.Payloads {
		sim := byte(0)
		if p.Simulation {
			sim = 1
		}
		dst = append(dst, byte(p.Type), sim)
		dst = binary.BigEndian.AppendUint16(dst, p.AppID)
		dst = binary.BigEndian.AppendUint16(dst, u16(len(p.APDU)))
		dst = append(dst, p.APDU...)
	}
	if key != nil {
		sum := key.mac(dst[start:])
		dst = append(dst, tagSignature, u8(len(sum)))
		dst = append(dst, sum...)
	}
	return dst, nil
}

// reader reads big-endian fields with bounds checks.
type reader struct {
	b   []byte
	pos int
}

func (r *reader) need(n int, field string) error {
	if n < 0 || len(r.b)-r.pos < n {
		return malformed(field, r.pos, "%d octets needed, %d left", n, len(r.b)-r.pos)
	}
	return nil
}

func (r *reader) u8() byte {
	v := r.b[r.pos]
	r.pos++
	return v
}

func (r *reader) u16() uint16 {
	v := binary.BigEndian.Uint16(r.b[r.pos:])
	r.pos += 2
	return v
}

func (r *reader) u32() uint32 {
	v := binary.BigEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) bytes(n int) []byte {
	v := append([]byte(nil), r.b[r.pos:r.pos+n]...)
	r.pos += n
	return v
}

// Decode strictly decodes the structure of an SPDU without verifying it
// (see [Verify]). Returned slices do not alias b.
//
// It fails with a [*DecodeError] wrapping [ErrMalformed] for a datagram
// that is not a well-formed SPDU and [ErrUnsupported] for a protocol
// version other than 1 and 2.
func Decode(b []byte) (*SPDU, error) {
	if len(b) > MaxSPDUSize {
		return nil, malformed("datagram", 0, "%d octets exceed %d", len(b), MaxSPDUSize)
	}
	r := &reader{b: b}
	if err := r.need(fixedHdr, "session header"); err != nil {
		return nil, err
	}
	if b[0] != cltpLI || b[1] != cltpUD {
		return nil, malformed("transport header", 0, "0x%02X 0x%02X, want 0x01 0x40", b[0], b[1])
	}
	s := &SPDU{SI: SessionID(b[2])}
	if s.SI < SITunnelled || s.SI > SIManagement {
		return nil, malformed("session identifier", 2, "0x%02X", b[2])
	}
	// The header lengths are checked only against the datagram:
	// implementations write different values (docs/adr/0001).
	sessionHdr, common := int(b[3]), int(b[5])
	if sessionHdr < 2+commonLen || 4+sessionHdr > len(b) {
		return nil, malformed("session header length", 3, "%d", sessionHdr)
	}
	if b[4] != tagCommon {
		return nil, malformed("common session header", 4, "tag 0x%02X, want 0x80", b[4])
	}
	if common < commonLen || 6+common > len(b) {
		return nil, malformed("common session header length", 5, "%d", common)
	}
	r.pos = 6
	s.Length = r.u32()
	s.Number = r.u32()
	s.Version = r.u16()
	switch s.Version {
	case 1:
		if err := r.need(secInfoV1, "security information"); err != nil {
			return nil, err
		}
		s.TimeOfCurrentKey = r.u32()
		s.TimeToNextKey = int16FromWire(r.u16())
		s.Encryption = EncryptionAlgorithm(r.u8())
		s.MAC = MACAlgorithm(r.u8())
		s.KeyID = r.u32()
	case 2:
		if err := r.need(secInfoV2, "security information"); err != nil {
			return nil, err
		}
		s.TimeOfCurrentKey = r.u32()
		s.TimeToNextKey = int16FromWire(r.u16())
		s.KeyID = r.u32()
		n := int(r.u8())
		if err := r.need(n, "initialisation vector"); err != nil {
			return nil, err
		}
		if n > 0 {
			s.IV = r.bytes(n)
		}
	default:
		return nil, unsupported("version", 14, "protocol version %d", s.Version)
	}
	if err := r.need(4, "payload length"); err != nil {
		return nil, err
	}
	payloadOff := r.pos
	pl := r.u32()
	if int64(pl) > int64(len(b)-r.pos) {
		return nil, malformed("payload length", payloadOff, "%d octets, %d left", pl, len(b)-r.pos)
	}
	end := r.pos + int(pl)
	for r.pos < end {
		if end-r.pos < elementHdr {
			return nil, malformed("payload element", r.pos, "truncated header")
		}
		off := r.pos
		p := Payload{Type: PayloadType(r.u8())}
		switch sim := r.u8(); sim {
		case 0:
		case 1:
			p.Simulation = true
		default:
			return nil, malformed("simulation", off+1, "0x%02X", sim)
		}
		p.AppID = r.u16()
		n := int(r.u16())
		if n > end-r.pos {
			// libiec61850 writes the APDU length plus 2; accepted on
			// the last element, where it is unambiguous.
			if n != end-r.pos+2 {
				return nil, malformed("APDU length", off+4, "%d octets, %d left in the payload", n, end-r.pos)
			}
			n = end - r.pos
		}
		p.APDU = r.bytes(n)
		s.Payloads = append(s.Payloads, p)
	}
	if r.pos < len(b) && b[r.pos] == tagPadding {
		if err := r.need(2, "padding"); err != nil {
			return nil, err
		}
		r.pos++
		n := int(r.u8())
		if err := r.need(n, "padding"); err != nil {
			return nil, err
		}
		r.pos += n
	}
	s.signed = r.pos
	if r.pos < len(b) {
		if b[r.pos] != tagSignature {
			return nil, malformed("signature", r.pos, "tag 0x%02X, want 0x85", b[r.pos])
		}
		if err := r.need(2, "signature"); err != nil {
			return nil, err
		}
		r.pos++
		n := int(r.u8())
		if n == 0 || n != len(b)-r.pos {
			return nil, malformed("signature", r.pos-1, "length %d, %d octets left", n, len(b)-r.pos)
		}
		s.Signature = r.bytes(n)
	}
	return s, nil
}

// VerifyOptions configures [Verify].
type VerifyOptions struct {
	// Keys verifies secured SPDUs. Nil refuses every secured SPDU with
	// [ErrUnknownKey].
	Keys KeyProvider

	// AllowUnsecured accepts SPDUs without key ID and signature.
	AllowUnsecured bool
}

// Verify decodes b and checks its signature. It returns the SPDU and the
// key that verified it (nil for an accepted unsecured SPDU).
//
// An SPDU is unsecured when its key ID is 0 (and, in version 1, its MAC
// algorithm is none) and it has no signature; it is refused with
// [ErrUnsecured] unless opts.AllowUnsecured. A secured SPDU is refused
// with [ErrUnknownKey] when the provider does not hold its key, with
// [ErrUnsupported] when it is encrypted or uses a MAC algorithm other
// than HMAC-SHA256, and with [ErrAuthentication] when the signature is
// missing, its length or algorithm does not match the key, or it does
// not verify. A key the provider returns with another ID than the
// SPDU's, with ID 0 or with less than [MinKeySize] octets of material is
// not used: the SPDU is refused with an error that wraps both
// [ErrUnknownKey] and [ErrInvalidKey]. Replay is checked by [Transport],
// not here.
func Verify(b []byte, opts VerifyOptions) (*SPDU, *Key, error) {
	s, err := Decode(b)
	if err != nil {
		return nil, nil, err
	}
	if s.Version == 1 && s.Encryption != EncryptionNone {
		return nil, nil, unsupported("encryption", 22, "algorithm %d", s.Encryption)
	}
	unsecured := s.KeyID == 0
	if s.Version == 1 {
		unsecured = s.MAC == MACNone
	}
	if unsecured {
		if s.Signature != nil {
			return nil, nil, fmt.Errorf("%w: signature on an unsecured SPDU", ErrAuthentication)
		}
		if !opts.AllowUnsecured {
			return nil, nil, ErrUnsecured
		}
		return s, nil, nil
	}
	if s.Version == 1 && s.MAC.Size() == 0 {
		return nil, nil, unsupported("MAC algorithm", 23, "%v", s.MAC)
	}
	var key *Key
	if opts.Keys != nil {
		key, _ = opts.Keys.LookupKey(s.KeyID)
	}
	if key == nil {
		return nil, nil, fmt.Errorf("%w: %d", ErrUnknownKey, s.KeyID)
	}
	if key.MAC.Size() == 0 {
		return nil, nil, fmt.Errorf("%w: key %d uses %v", ErrUnsupported, key.ID, key.MAC)
	}
	if key.ID != s.KeyID {
		return nil, nil, fmt.Errorf("%w: %w: provider returned key %d for key ID %d", ErrUnknownKey, ErrInvalidKey, key.ID, s.KeyID)
	}
	if err := key.validate(); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnknownKey, err)
	}
	if s.Version == 1 && s.MAC != key.MAC {
		return nil, nil, fmt.Errorf("%w: SPDU uses %v, key %d is %v", ErrAuthentication, s.MAC, key.ID, key.MAC)
	}
	if s.Signature == nil {
		return nil, nil, fmt.Errorf("%w: no signature", ErrAuthentication)
	}
	if len(s.Signature) != key.MAC.Size() {
		return nil, nil, fmt.Errorf("%w: %d-octet signature, %v is %d", ErrAuthentication, len(s.Signature), key.MAC, key.MAC.Size())
	}
	if !hmac.Equal(s.Signature, key.mac(b[:s.signed])) {
		return nil, nil, fmt.Errorf("%w: signature does not verify", ErrAuthentication)
	}
	s.MAC = key.MAC
	return s, key, nil
}
