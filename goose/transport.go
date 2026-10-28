// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"log/slog"
)

// Transport sends and receives raw Ethernet frames (without FCS).
//
// Implementations must be safe for concurrent use: one goroutine may
// block in Recv while others call Send. Send must not retain frame after
// it returns. Recv returns a slice owned by the caller.
//
// Backends: [Open] (Linux AF_PACKET, macOS/BSD BPF) and [Hub] (in
// memory, for tests and simulations).
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
