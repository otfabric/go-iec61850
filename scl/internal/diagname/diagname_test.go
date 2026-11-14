// SPDX-License-Identifier: MIT

package diagname

import (
	"strings"
	"testing"
)

func TestCap(t *testing.T) {
	for _, tc := range []struct {
		in   string
		max  int
		want string
	}{
		{"abc", 3, "abc"},
		{"abcd", 3, "abc…"},
		{"aé", 2, "a…"}, // "é" is two bytes: cut before it
		{"é", 1, "…"},
		{"", 0, ""},
	} {
		if got := Cap(tc.in, tc.max); got != tc.want {
			t.Errorf("Cap(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
	long := strings.Repeat("x", 10*Max)
	if got := Q(long); len(got) != Max+len("…") {
		t.Errorf("Q: %d bytes", len(got))
	}
	if got := Q("short"); got != "short" {
		t.Errorf("Q(short) = %q", got)
	}
}
