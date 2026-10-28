// SPDX-License-Identifier: MIT

//go:build openbsd

package goose

import "golang.org/x/sys/unix"

// setSeeSent disables loopback of frames sent on this descriptor
// (OpenBSD uses a direction filter instead of BIOCSSEESENT).
func setSeeSent(fd int) error { return unix.IoctlSetPointerInt(fd, unix.BIOCSDIRFILT, unix.BPF_DIRECTION_OUT) }
