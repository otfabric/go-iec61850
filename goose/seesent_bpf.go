// SPDX-License-Identifier: MIT

//go:build darwin || freebsd || netbsd || dragonfly

package goose

import "golang.org/x/sys/unix"

// setSeeSent disables loopback of frames sent on this descriptor.
func setSeeSent(fd int) error { return unix.IoctlSetPointerInt(fd, unix.BIOCSSEESENT, 0) }
