// SPDX-License-Identifier: MIT

package goose

import (
	"fmt"
	"net"
	"strings"
)

// MAC is an IEEE 802 48-bit hardware address. The zero value is the
// all-zero address, which subscriber filters treat as "any".
type MAC [6]byte

// Broadcast is the Ethernet broadcast address ff:ff:ff:ff:ff:ff.
var Broadcast = MAC{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// ParseMAC parses a hardware address written as six hexadecimal octets
// separated by '-' or ':' (the SCL MAC-Address form is
// "01-0C-CD-01-00-01") or as twelve contiguous hex digits.
func ParseMAC(s string) (MAC, error) {
	var m MAC
	str := strings.TrimSpace(s)
	if len(str) == 12 && !strings.ContainsAny(str, "-:.") {
		str = str[0:2] + ":" + str[2:4] + ":" + str[4:6] + ":" + str[6:8] + ":" + str[8:10] + ":" + str[10:12]
	}
	str = strings.ReplaceAll(str, "-", ":")
	hw, err := net.ParseMAC(str)
	if err != nil || len(hw) != 6 {
		return m, &ConfigError{Field: "MAC", Message: fmt.Sprintf("invalid MAC address %q", s)}
	}
	copy(m[:], hw)
	return m, nil
}

// String formats the address as lower-case colon-separated octets.
func (m MAC) String() string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// IsZero reports whether m is the all-zero address.
func (m MAC) IsZero() bool { return m == MAC{} }

// IsMulticast reports whether the group bit (I/G) is set.
func (m MAC) IsMulticast() bool { return m[0]&0x01 != 0 }

// IsGOOSEMulticast reports whether m lies in the multicast range
// recommended for GOOSE by IEC 61850-8-1 Annex B
// (01-0C-CD-01-00-00 to 01-0C-CD-01-01-FF).
func (m MAC) IsGOOSEMulticast() bool {
	return m[0] == 0x01 && m[1] == 0x0c && m[2] == 0xcd && m[3] == 0x01 && m[4] <= 0x01
}

// MarshalText implements [encoding.TextMarshaler].
func (m MAC) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

// UnmarshalText implements [encoding.TextUnmarshaler].
func (m *MAC) UnmarshalText(b []byte) error {
	v, err := ParseMAC(string(b))
	if err != nil {
		return err
	}
	*m = v
	return nil
}
