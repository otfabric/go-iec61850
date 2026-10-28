// SPDX-License-Identifier: MIT

package l2

import "testing"

func TestVLAN(t *testing.T) {
	for _, v := range []VLAN{{}, {Priority: 4, ID: 1}, {Priority: 7, DEI: true, ID: 0x0fff}} {
		tci, ok := v.TCI()
		if !ok || VLANFromTCI(tci) != v {
			t.Errorf("%+v: tci %#04x ok %v", v, tci, ok)
		}
	}
	if tci, _ := (VLAN{Priority: 4, DEI: true, ID: 0x123}).TCI(); tci != 0x9123 {
		t.Errorf("tci %#04x", tci)
	}
	for _, v := range []VLAN{{Priority: 8}, {ID: 0x1000}} {
		if _, ok := v.TCI(); ok {
			t.Errorf("%+v accepted", v)
		}
	}
}
