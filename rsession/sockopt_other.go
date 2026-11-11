// SPDX-License-Identifier: MIT

//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package rsession

import (
	"net"
	"net/netip"
)

// Multicast group membership and socket options are implemented on Linux,
// macOS and the BSDs; unicast works everywhere.

func joinGroup(*net.UDPConn, netip.Addr, *net.Interface) error { return ErrUnsupportedPlatform }

func setMulticastInterface(*net.UDPConn, *net.Interface, bool) error { return ErrUnsupportedPlatform }

func setMulticastTTL(*net.UDPConn, int, bool) error { return ErrUnsupportedPlatform }
