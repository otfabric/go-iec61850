// SPDX-License-Identifier: MIT

package scl

import "testing"

func TestKindFromPath(t *testing.T) {
	tests := []struct {
		path string
		want DocumentKind
	}{
		{"plant.scd", KindSCD},
		{"/tmp/Device.CID", KindCID},
		{"template.icd", KindICD},
		{"inst.iid", KindIID},
		{"topo.ssd", KindSSD},
		{"notes.txt", KindUnknown},
		{"noext", KindUnknown},
		{"mixed.ScD", KindSCD},
	}
	for _, tt := range tests {
		if got := KindFromPath(tt.path); got != tt.want {
			t.Errorf("KindFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
