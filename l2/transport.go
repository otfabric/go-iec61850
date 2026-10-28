// SPDX-License-Identifier: MIT

package l2

import (
	"context"
	"log/slog"
)

// EtherTypes of the IEC 61850 layer-2 protocols and of the 802.1Q tag.
const (
	// EtherTypeGOOSE is the GOOSE EtherType (IEC 61850-8-1).
	EtherTypeGOOSE uint16 = 0x88B8

	// EtherTypeGSE is the GSE management EtherType (IEC 61850-8-1).
	EtherTypeGSE uint16 = 0x88B9

	// EtherTypeSV is the Sampled Values EtherType (IEC 61850-9-2).
	EtherTypeSV uint16 = 0x88BA

	// EtherTypeVLAN is the IEEE 802.1Q tag protocol identifier.
	EtherTypeVLAN uint16 = 0x8100
)

// Transport sends and receives raw Ethernet frames (without FCS).
//
// Implementations must be safe for concurrent use: one goroutine may
// block in Recv while others call Send. Send must not retain frame after
// it returns. Recv returns a slice owned by the caller.
//
// Backends: [Open] (Linux AF_PACKET, macOS/BSD BPF) and [Hub] (in
// memory, for tests and simulations). A [Demux] shares one Transport
// between several receivers.
type Transport interface {
	// Send transmits one complete Ethernet frame.
	Send(frame []byte) error

	// Recv blocks until a frame is received, ctx is done (returning
	// ctx.Err()) or the transport is closed (returning [ErrClosed]).
	Recv(ctx context.Context) ([]byte, error)

	// HardwareAddr returns the interface address, used as the default
	// source address by publishers.
	HardwareAddr() MAC

	// Close releases the transport. Blocked Recv calls return
	// [ErrClosed]. Close is idempotent.
	Close() error
}

// TransportOptions configures a raw Ethernet transport opened with
// [Open].
type TransportOptions struct {
	// EtherTypes lists the EtherTypes delivered by Recv; a kernel filter
	// drops everything else (untagged or with one 802.1Q tag). Empty
	// means GOOSE only ([EtherTypeGOOSE]), the behaviour of
	// goose.Open; sv.Open passes [EtherTypeSV]. Pass both to receive
	// GOOSE and SV on one socket and split them with a [Demux].
	EtherTypes []uint16

	// Promiscuous puts the interface in promiscuous mode. On macOS/BSD
	// it is required to receive multicast groups that the host has not
	// joined, so [Open] enables it there unless Multicast is non-empty
	// and the platform supports group membership.
	Promiscuous bool

	// Multicast lists destination groups to join. When empty on Linux
	// the socket receives all multicast traffic (PACKET_MR_ALLMULTI).
	Multicast []MAC

	// ReceiveLocal also delivers frames sent from this host by other
	// sockets, so a publisher and a subscriber can run on the same
	// machine. On Linux the transport then also receives its own
	// frames.
	ReceiveLocal bool

	// Logger receives transport diagnostics. Nil disables logging.
	Logger *slog.Logger
}
