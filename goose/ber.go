// SPDX-License-Identifier: MIT

package goose

import "math"

// Minimal BER (ITU-T X.690) helpers for the GOOSE APDU. Only the
// subset used by IEC 61850-8-1 is supported: single-octet tags and
// definite lengths of at most four octets. Indefinite lengths are
// rejected.

// appendLength appends a definite BER length (X.690 8.1.3.4 short form
// below 128, 8.1.3.5 long form otherwise). Lengths are content sizes
// and therefore non-negative; the encoder never produces content beyond
// four length octets because an encoded frame is bounded by
// [MaxPayload], so n is clamped to the four-octet maximum rather than
// letting the shifts below silently drop high bits.
func appendLength(b []byte, n int) []byte {
	u := uint32(min(max(int64(n), 0), math.MaxUint32))
	switch {
	case u < 0x80:
		return append(b, byte(u))
	case u <= 0xff:
		return append(b, 0x81, byte(u))
	case u <= 0xffff:
		return append(b, 0x82, byte(u>>8&0xff), byte(u&0xff))
	case u <= 0xffffff:
		return append(b, 0x83, byte(u>>16&0xff), byte(u>>8&0xff), byte(u&0xff))
	default:
		return append(b, 0x84, byte(u>>24), byte(u>>16&0xff), byte(u>>8&0xff), byte(u&0xff))
	}
}

// lengthSize returns the number of octets appendLength emits for n.
func lengthSize(n int) int {
	switch {
	case n < 0x80:
		return 1
	case n <= 0xff:
		return 2
	case n <= 0xffff:
		return 3
	case n <= 0xffffff:
		return 4
	default:
		return 5
	}
}

// appendTLV appends tag, length and content.
func appendTLV(b []byte, tag byte, content []byte) []byte {
	b = append(b, tag)
	b = appendLength(b, len(content))
	return append(b, content...)
}

// signedBytes returns the minimal two's-complement encoding of v.
func signedBytes(v int64) []byte {
	var b []byte
	n := 8
	for n > 1 {
		hi := byte(v >> (8 * (n - 1)) & 0xff)
		next := byte(v >> (8 * (n - 2)) & 0xff)
		if (hi == 0x00 && next&0x80 == 0) || (hi == 0xff && next&0x80 != 0) {
			n--
			continue
		}
		break
	}
	for i := n - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*i)&0xff))
	}
	return b
}

// encodeUnsigned returns the minimal non-negative INTEGER encoding of v
// (a leading zero octet is added when the high bit would be set).
func encodeUnsigned(v uint64) []byte {
	var b []byte
	n := 1
	for n < 8 && v>>(8*n) != 0 {
		n++
	}
	if v>>(8*(n-1))&0x80 != 0 {
		b = append(b, 0)
	}
	for i := n - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*i)&0xff))
	}
	return b
}

// tlv is a decoded BER element. Content aliases the input buffer.
type tlv struct {
	tag     byte
	content []byte
	off     int // offset of the tag octet relative to the decode base
	next    int // offset just past the element
}

// readTLV decodes one element at buf[pos:]. base is added to offsets
// reported in errors so they refer to the whole frame.
func readTLV(buf []byte, pos, base int, field string) (tlv, error) {
	if pos >= len(buf) {
		return tlv{}, decodeErr(ErrInvalidPDU, field, base+pos, "truncated: missing tag")
	}
	tag := buf[pos]
	if tag&0x1f == 0x1f {
		return tlv{}, decodeErr(ErrInvalidPDU, field, base+pos, "multi-octet tag 0x%02x not supported", tag)
	}
	p := pos + 1
	if p >= len(buf) {
		return tlv{}, decodeErr(ErrInvalidPDU, field, base+p, "truncated: missing length")
	}
	l := int(buf[p])
	p++
	if l&0x80 != 0 {
		nb := l & 0x7f
		if nb == 0 {
			return tlv{}, decodeErr(ErrInvalidPDU, field, base+p-1, "indefinite length not allowed")
		}
		if nb > 4 {
			return tlv{}, decodeErr(ErrInvalidPDU, field, base+p-1, "length of %d octets not supported", nb)
		}
		if p+nb > len(buf) {
			return tlv{}, decodeErr(ErrInvalidPDU, field, base+p, "truncated length")
		}
		l = 0
		for i := range nb {
			l = l<<8 | int(buf[p+i])
		}
		p += nb
	}
	if l < 0 || l > len(buf)-p {
		return tlv{}, decodeErr(ErrInvalidPDU, field, base+pos, "length %d exceeds remaining %d octets", l, len(buf)-p)
	}
	return tlv{tag: tag, content: buf[p : p+l], off: pos, next: p + l}, nil
}

// decodeUint32 decodes a non-negative INTEGER that must fit in 32 bits.
func decodeUint32(c []byte, field string, off int) (uint32, error) {
	if len(c) == 0 || len(c) > 5 {
		return 0, decodeErr(ErrInvalidPDU, field, off, "integer of %d octets", len(c))
	}
	if c[0]&0x80 != 0 {
		return 0, decodeErr(ErrInvalidPDU, field, off, "negative value")
	}
	if len(c) == 5 && c[0] != 0 {
		return 0, decodeErr(ErrInvalidPDU, field, off, "value exceeds 32 bits")
	}
	var v uint64
	for _, x := range c {
		v = v<<8 | uint64(x)
	}
	if v > math.MaxUint32 {
		return 0, decodeErr(ErrInvalidPDU, field, off, "value exceeds 32 bits")
	}
	return uint32(v), nil
}

// decodeBool decodes a BER BOOLEAN (any non-zero octet is TRUE).
func decodeBool(c []byte, field string, off int) (bool, error) {
	if len(c) != 1 {
		return false, decodeErr(ErrInvalidPDU, field, off, "boolean of %d octets", len(c))
	}
	return c[0] != 0, nil
}
