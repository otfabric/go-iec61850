// SPDX-License-Identifier: MIT

package sv

import (
	"fmt"

	"github.com/otfabric/go-iec61850/l2"
)

// The raw Ethernet layer is package l2, shared with package goose. The
// names below are aliases, so an sv.Transport is an l2.Transport (and a
// goose.Transport).

// MAC is an IEEE 802 48-bit hardware address. The zero value is the
// all-zero address, which subscriber filters treat as "any".
type MAC = l2.MAC

// VLAN is an IEEE 802.1Q tag.
type VLAN = l2.VLAN

// Transport sends and receives raw Ethernet frames; see [l2.Transport].
type Transport = l2.Transport

// TransportOptions configures [Open]; see [l2.TransportOptions].
type TransportOptions = l2.TransportOptions

// TransportError wraps an operating-system error from a raw Ethernet
// backend; see [l2.TransportError].
type TransportError = l2.TransportError

// Hub is an in-memory Ethernet segment for tests and simulations; see
// [l2.Hub].
type Hub = l2.Hub

// HubPort is one attachment to a [Hub].
type HubPort = l2.HubPort

// Faults configures fault injection on a [Hub].
type Faults = l2.Faults

// HubStats counts [Hub] activity.
type HubStats = l2.HubStats

// NewHub returns an empty hub without faults.
func NewHub() *Hub { return l2.NewHub() }

// ParseMAC parses a hardware address such as the SCL form
// "01-0C-CD-04-00-01". It fails with a [*ConfigError].
func ParseMAC(s string) (MAC, error) {
	m, err := l2.ParseMAC(s)
	if err != nil {
		return m, &ConfigError{Field: "MAC", Message: fmt.Sprintf("invalid MAC address %q", s)}
	}
	return m, nil
}

// Open opens a raw Ethernet transport on the named interface (Linux
// AF_PACKET, macOS/BSD BPF; see [l2.Open]). When opts.EtherTypes is
// empty only SV frames are received.
func Open(iface string, opts TransportOptions) (Transport, error) {
	if len(opts.EtherTypes) == 0 {
		opts.EtherTypes = []uint16{EtherType}
	}
	return l2.Open(iface, opts)
}
