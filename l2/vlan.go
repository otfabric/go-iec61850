// SPDX-License-Identifier: MIT

package l2

// VLAN is an IEEE 802.1Q tag.
type VLAN struct {
	// Priority is the 3-bit priority code point (0-7). IEC 61850
	// recommends 4 for GOOSE and SV.
	Priority uint8

	// DEI is the drop eligible indicator.
	DEI bool

	// ID is the 12-bit VLAN identifier (0-4095). 0 means priority
	// tagged only.
	ID uint16
}

// TCI returns the tag control information field. ok is false when
// Priority or ID is out of range.
func (v VLAN) TCI() (tci uint16, ok bool) {
	if v.Priority > 7 || v.ID > 0x0fff {
		return 0, false
	}
	tci = uint16(v.Priority)<<13 | v.ID
	if v.DEI {
		tci |= 0x1000
	}
	return tci, true
}

// VLANFromTCI decodes a tag control information field.
func VLANFromTCI(tci uint16) VLAN {
	return VLAN{Priority: uint8(tci >> 13), DEI: tci&0x1000 != 0, ID: tci & 0x0fff}
}
