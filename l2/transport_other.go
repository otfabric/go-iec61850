// SPDX-License-Identifier: MIT

//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package l2

// Open is not available on this platform: it always fails with
// [ErrUnsupportedPlatform]. Use a [Hub] or a custom [Transport].
func Open(iface string, _ TransportOptions) (Transport, error) {
	return nil, &TransportError{Op: "open", Interface: iface, Err: ErrUnsupportedPlatform, Kind: ErrUnsupportedPlatform}
}
