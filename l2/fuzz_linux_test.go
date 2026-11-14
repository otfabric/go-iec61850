// SPDX-License-Identifier: MIT

//go:build linux

package l2

import (
	"bytes"
	"testing"

	"golang.org/x/sys/unix"
)

// FuzzRestoreVLAN gives restoreVLAN an arbitrary frame and arbitrary
// control-message octets, as the packet socket's Recv does. The result
// is a copy the caller owns; it is the frame unchanged, or the frame
// with one 4-octet tag inserted after the two addresses.
func FuzzRestoreVLAN(f *testing.F) {
	frame := []byte{1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 0x88, 0xb8, 0xaa}
	f.Add(frame, auxdataCmsg(unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID, Vlan_tci: 0xa00b}))
	f.Add(frame, auxdataCmsg(unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID | unix.TP_STATUS_VLAN_TPID_VALID, Vlan_tpid: 0x88a8}))
	f.Add(frame[:10], auxdataCmsg(unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID}))
	f.Add(frame, []byte{})
	f.Fuzz(func(t *testing.T, frame, oob []byte) {
		origFrame, origOOB := append([]byte(nil), frame...), append([]byte(nil), oob...)
		got := restoreVLAN(frame, oob)
		if !bytes.Equal(frame, origFrame) || !bytes.Equal(oob, origOOB) {
			t.Fatal("restoreVLAN changed its input")
		}
		switch len(got) {
		case len(frame):
			if !bytes.Equal(got, frame) {
				t.Fatalf("% x, want the frame % x", got, frame)
			}
		case len(frame) + 4:
			if len(frame) < 12 || !bytes.Equal(got[:12], frame[:12]) || !bytes.Equal(got[16:], frame[12:]) {
				t.Fatalf("% x is not % x with a tag after the addresses", got, frame)
			}
		default:
			t.Fatalf("%d octets from a frame of %d", len(got), len(frame))
		}
		if len(got) > 0 {
			got[0] ^= 0xff
			if !bytes.Equal(frame, origFrame) {
				t.Fatal("the result shares the frame")
			}
		}
	})
}
