// SPDX-License-Identifier: MIT

package rsession

import (
	"math"
	"testing"
)

// The checked conversions are bit-identical to the plain ones they
// replace, for every 16-bit value and the 32-bit serial number edges.
func TestConvBitIdentical(t *testing.T) {
	for u := range math.MaxUint16 + 1 {
		w := uint16(u)
		if got, want := int16FromWire(w), int16(w); got != want {
			t.Fatalf("int16FromWire(%#x) = %d, want %d", w, got, want)
		}
		v := int16(w)
		if got, want := int16ToWire(v), uint16(v); got != want {
			t.Fatalf("int16ToWire(%d) = %#x, want %#x", v, got, want)
		}
	}
	for _, c := range [][2]uint32{{0, 0}, {1, 0}, {0, 1}, {math.MaxUint32, 0}, {0, math.MaxUint32}, {1 << 31, 0}, {0, 1 << 31}, {1<<31 - 1, 0}, {1<<31 + 1, 0}, {5, math.MaxUint32 - 5}} {
		if got, want := serialDiff(c[0], c[1]), int64(int32(c[0]-c[1])); got != want {
			t.Errorf("serialDiff(%d, %d) = %d, want %d", c[0], c[1], got, want)
		}
	}
	for _, n := range []int{0, 1, 255} {
		if u8(n) != uint8(n) {
			t.Errorf("u8(%d)", n)
		}
	}
	for _, n := range []int{0, 256, 65535} {
		if u16(n) != uint16(n) {
			t.Errorf("u16(%d)", n)
		}
	}
	for _, f := range []func(){func() { u8(256) }, func() { u8(-1) }, func() { u16(65536) }, func() { u16(-1) }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("out-of-range conversion did not panic")
				}
			}()
			f()
		}()
	}
}
