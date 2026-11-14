// SPDX-License-Identifier: MIT

package rsession

import (
	"math"
	"testing"
)

// The checked conversions are bit-identical to the plain ones they
// replace, for every 16-bit value.
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
