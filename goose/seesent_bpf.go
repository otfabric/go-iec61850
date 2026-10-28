// SPDX-License-Identifier: MIT

//go:build darwin || freebsd || netbsd || dragonfly

package goose

import "golang.org/x/sys/unix"

// setSeeSent controls whether locally sent frames are captured. Frames
// written to this descriptor itself are never captured.
func setSeeSent(fd int, local bool) error {
	v := 0
	if local {
		v = 1
	}
	return unix.IoctlSetPointerInt(fd, unix.BIOCSSEESENT, v)
}
