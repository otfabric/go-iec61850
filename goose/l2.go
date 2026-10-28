// SPDX-License-Identifier: MIT

package goose

import (
	"fmt"

	"github.com/otfabric/go-iec61850/l2"
)

// The raw Ethernet layer lives in package l2 (shared with package sv).
// The names below keep the goose v1.1.0 API source-compatible: they are
// aliases of the l2 types, so a goose.Transport, goose.Hub or goose.MAC
// is interchangeable with its l2 counterpart.

// MAC is an IEEE 802 48-bit hardware address. The zero value is the
// all-zero address, which subscriber filters treat as "any".
type MAC = l2.MAC

// Broadcast is the Ethernet broadcast address ff:ff:ff:ff:ff:ff.
var Broadcast = l2.Broadcast

// ParseMAC parses a hardware address written as six hexadecimal octets
// separated by '-' or ':' (the SCL MAC-Address form is
// "01-0C-CD-01-00-01") or as twelve contiguous hex digits. It fails with
// a [*ConfigError].
func ParseMAC(s string) (MAC, error) {
	m, err := l2.ParseMAC(s)
	if err != nil {
		return m, &ConfigError{Field: "MAC", Message: fmt.Sprintf("invalid MAC address %q", s)}
	}
	return m, nil
}

// Transport sends and receives raw Ethernet frames; see [l2.Transport].
type Transport = l2.Transport

// TransportOptions configures [Open]; see [l2.TransportOptions].
type TransportOptions = l2.TransportOptions

// TransportError wraps an operating-system error from a raw Ethernet
// backend; see [l2.TransportError].
type TransportError = l2.TransportError

// Hub is an in-memory Ethernet segment; see [l2.Hub].
type Hub = l2.Hub

// HubPort is one attachment to a [Hub]; see [l2.HubPort].
type HubPort = l2.HubPort

// Faults configures fault injection on a [Hub]; see [l2.Faults].
type Faults = l2.Faults

// HubStats counts hub activity; see [l2.HubStats].
type HubStats = l2.HubStats

// NewHub returns an empty hub without faults.
func NewHub() *Hub { return l2.NewHub() }

// Open opens a raw Ethernet transport on the named interface (Linux
// AF_PACKET, macOS/BSD BPF; see [l2.Open]). When opts.EtherTypes is empty
// only GOOSE frames are received; add [l2.EtherTypeSV] and use an
// [l2.Demux] to share the socket with an SV subscriber.
func Open(iface string, opts TransportOptions) (Transport, error) {
	if len(opts.EtherTypes) == 0 {
		opts.EtherTypes = []uint16{EtherType}
	}
	return l2.Open(iface, opts)
}
