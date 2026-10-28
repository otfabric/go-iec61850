// SPDX-License-Identifier: MIT

//go:build openbsd

package goose

import "golang.org/x/sys/unix"

// setSeeSent controls whether locally sent frames are captured
// (OpenBSD uses a direction filter instead of BIOCSSEESENT).
func setSeeSent(fd int, local bool) error {
	if local {
		return nil
	}
	return unix.IoctlSetPointerInt(fd, unix.BIOCSDIRFILT, unix.BPF_DIRECTION_OUT)
}
