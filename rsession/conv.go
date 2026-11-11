// SPDX-License-Identifier: MIT

package rsession

import (
	"fmt"
	"math"
)

// Field conversions the encoder and decoder share. Each one is checked
// where it converts, so a value outside the field is a bug here that
// panics rather than going out truncated.

// u8 returns n for a one-octet length field. Callers bound n first.
func u8(n int) uint8 {
	if n < 0 || n > math.MaxUint8 {
		panic(fmt.Sprintf("rsession: %d does not fit a one-octet field", n))
	}
	return uint8(n)
}

// u16 returns n for a two-octet length field. Callers bound n first.
func u16(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		panic(fmt.Sprintf("rsession: %d does not fit a two-octet field", n))
	}
	return uint16(n)
}

// int16FromWire reads a two-octet two's-complement field.
func int16FromWire(u uint16) int16 {
	if u <= math.MaxInt16 {
		return int16(u)
	}
	return -int16(math.MaxUint16-u) - 1
}

// int16ToWire writes v as a two-octet two's-complement field.
func int16ToWire(v int16) uint16 {
	if v >= 0 {
		return uint16(v)
	}
	x := int32(v) + 1<<16 // 0x8000..0xffff
	if x < 0 || x > math.MaxUint16 {
		panic("rsession: two's complement out of range")
	}
	return uint16(x)
}

// serialDiff is a - b in 32-bit serial number arithmetic (RFC 1982):
// positive when a is ahead of b by less than 2^31.
func serialDiff(a, b uint32) int64 {
	d := a - b
	if d <= math.MaxInt32 {
		return int64(d)
	}
	return int64(d) - (1 << 32)
}
