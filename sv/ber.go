// SPDX-License-Identifier: MIT

package sv

import (
	"encoding/binary"
	"time"
)

// Minimal BER (ITU-T X.690) helpers for the savPdu: single-octet tags
// and definite lengths of at most four octets. Indefinite lengths are
// rejected. Encoding helpers append to a caller buffer so that the
// publisher can encode without allocating.

// lengthSize returns the number of octets of the definite length n.
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

// tlvLen returns the size of a TLV with a content of n octets.
func tlvLen(n int) int { return 1 + lengthSize(n) + n }

// appendHdr appends a tag and the definite length n.
func appendHdr(b []byte, tag byte, n int) []byte {
	b = append(b, tag)
	switch {
	case n < 0x80:
		return append(b, byte(n))
	case n <= 0xff:
		return append(b, 0x81, byte(n))
	case n <= 0xffff:
		return append(b, 0x82, byte(n>>8), byte(n))
	case n <= 0xffffff:
		return append(b, 0x83, byte(n>>16), byte(n>>8), byte(n))
	default:
		return append(b, 0x84, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
}

// unsignedBytes appends the minimal non-negative INTEGER encoding of v.
func unsignedBytes(b []byte, v uint32) []byte {
	n := 1
	for n < 4 && v>>(8*n) != 0 {
		n++
	}
	if byte(v>>(8*(n-1)))&0x80 != 0 {
		b = append(b, 0)
	}
	for i := n - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// unsignedLen returns len(unsignedBytes(nil, v)) without allocating.
func unsignedLen(v uint32) int {
	n := 1
	for n < 4 && v>>(8*n) != 0 {
		n++
	}
	if byte(v>>(8*(n-1)))&0x80 != 0 {
		n++
	}
	return n
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
		for i := 0; i < nb; i++ {
			l = l<<8 | int(buf[p+i])
		}
		p += nb
	}
	if l < 0 || l > len(buf)-p {
		return tlv{}, decodeErr(ErrInvalidPDU, field, base+pos, "length %d exceeds remaining %d octets", l, len(buf)-p)
	}
	return tlv{tag: tag, content: buf[p : p+l], off: pos, next: p + l}, nil
}

// decodeUint decodes a non-negative INTEGER of at most 32 bits.
func decodeUint(c []byte, field string, off int) (uint32, error) {
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
	return uint32(v), nil
}

// appendUTCTime appends an 8-octet UtcTime (IEC 61850-8-1 / -9-2):
// seconds since 1970, a 24-bit fraction of a second and TimeQuality.
func appendUTCTime(b []byte, t time.Time, quality uint8) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(t.Unix()))
	frac := (uint64(t.Nanosecond())<<24 + 5e8) / 1e9 // round to nearest
	if frac > 0xffffff {
		frac = 0xffffff
	}
	return append(b, byte(frac>>16), byte(frac>>8), byte(frac), quality)
}

func decodeUTCTime(c []byte) (time.Time, uint8) {
	secs := binary.BigEndian.Uint32(c[0:4])
	frac := uint64(c[4])<<16 | uint64(c[5])<<8 | uint64(c[6])
	ns := (frac*1e9 + (1 << 23)) >> 24
	return time.Unix(int64(secs), int64(ns)).UTC(), c[7]
}
