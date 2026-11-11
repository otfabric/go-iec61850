// SPDX-License-Identifier: MIT

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package rsession

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"runtime"

	"golang.org/x/sys/unix"
)

func control(conn *net.UDPConn, f func(fd int) error) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) { serr = f(int(fd)) }); err != nil {
		return err
	}
	return serr
}

// ifaceIPv4 returns the first IPv4 address of ifi, which selects the
// interface in the IPv4 multicast socket options.
func ifaceIPv4(ifi *net.Interface) ([4]byte, error) {
	addrs, err := ifi.Addrs()
	if err != nil {
		return [4]byte{}, err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(n.IP); ok && ip.Unmap().Is4() {
				return ip.Unmap().As4(), nil
			}
		}
	}
	return [4]byte{}, fmt.Errorf("interface %s has no IPv4 address", ifi.Name)
}

func joinGroup(conn *net.UDPConn, group netip.Addr, ifi *net.Interface) error {
	return control(conn, func(fd int) error {
		if group.Is4() {
			mreq := &unix.IPMreq{Multiaddr: group.As4()}
			if ifi != nil {
				a, err := ifaceIPv4(ifi)
				if err != nil {
					return err
				}
				mreq.Interface = a
			}
			return unix.SetsockoptIPMreq(fd, unix.IPPROTO_IP, unix.IP_ADD_MEMBERSHIP, mreq)
		}
		mreq := &unix.IPv6Mreq{Multiaddr: group.As16()}
		if ifi != nil {
			if ifi.Index < 0 || int64(ifi.Index) > math.MaxUint32 {
				return fmt.Errorf("interface %s: index %d out of range", ifi.Name, ifi.Index)
			}
			mreq.Interface = uint32(ifi.Index)
		}
		return unix.SetsockoptIPv6Mreq(fd, unix.IPPROTO_IPV6, unix.IPV6_JOIN_GROUP, mreq)
	})
}

func setMulticastInterface(conn *net.UDPConn, ifi *net.Interface, v6 bool) error {
	if v6 {
		return control(conn, func(fd int) error {
			return unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_MULTICAST_IF, ifi.Index)
		})
	}
	a, err := ifaceIPv4(ifi)
	if err != nil {
		return err
	}
	return control(conn, func(fd int) error {
		return unix.SetsockoptInet4Addr(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_IF, a)
	})
}

func setMulticastTTL(conn *net.UDPConn, ttl int, v6 bool) error {
	return control(conn, func(fd int) error {
		if v6 {
			return unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_MULTICAST_HOPS, ttl)
		}
		if runtime.GOOS == "linux" {
			return unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_TTL, ttl)
		}
		// The BSDs and macOS take an unsigned char.
		if ttl < 0 || ttl > math.MaxUint8 {
			return fmt.Errorf("multicast TTL %d out of range 0..255", ttl)
		}
		return unix.SetsockoptByte(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_TTL, byte(ttl))
	})
}
