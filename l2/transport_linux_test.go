// SPDX-License-Identifier: MIT

//go:build linux

package l2

import (
	"bytes"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// auxdataCmsg builds a PACKET_AUXDATA control message from aux using the
// kernel struct layout, so the field-wise decoder in restoreVLAN is
// checked against the real struct tpacket_auxdata.
func auxdataCmsg(aux unix.TpacketAuxdata) []byte {
	b := make([]byte, unix.CmsgSpace(sizeofAuxdata))
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level = unix.SOL_PACKET
	h.Type = unix.PACKET_AUXDATA
	h.SetLen(unix.CmsgLen(sizeofAuxdata))
	*(*unix.TpacketAuxdata)(unsafe.Pointer(&b[unix.CmsgLen(0)])) = aux
	return b
}

func TestRestoreVLAN(t *testing.T) {
	frame := []byte{1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 0x88, 0xb8, 0xaa}
	tagged := func(tpid, tci uint16) []byte {
		out := append([]byte(nil), frame[:12]...)
		out = append(out, byte(tpid>>8), byte(tpid), byte(tci>>8), byte(tci))
		return append(out, frame[12:]...)
	}
	for _, tc := range []struct {
		name string
		aux  unix.TpacketAuxdata
		want []byte
	}{
		{"no vlan", unix.TpacketAuxdata{Status: 0, Vlan_tci: 0x0123}, frame},
		{"vlan default tpid", unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID, Vlan_tci: 0xa00b}, tagged(0x8100, 0xa00b)},
		{"vlan tpid valid", unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID | unix.TP_STATUS_VLAN_TPID_VALID, Vlan_tci: 0x0fff, Vlan_tpid: 0x88a8}, tagged(0x88a8, 0x0fff)},
		{"tpid valid but zero", unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID | unix.TP_STATUS_VLAN_TPID_VALID, Vlan_tci: 1}, tagged(0x8100, 1)},
		{"tpid without valid flag", unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID, Vlan_tci: 2, Vlan_tpid: 0x88a8}, tagged(0x8100, 2)},
		{"all status bits", unix.TpacketAuxdata{Status: 0xffffffff, Len: 99, Snaplen: 99, Mac: 7, Net: 9, Vlan_tci: 0xffff, Vlan_tpid: 0x9100}, tagged(0x9100, 0xffff)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := restoreVLAN(frame, auxdataCmsg(tc.aux)); !bytes.Equal(got, tc.want) {
				t.Fatalf("got % x, want % x", got, tc.want)
			}
		})
	}

	// Short frame, truncated and foreign control messages leave the frame untouched.
	short := frame[:10]
	if got := restoreVLAN(short, auxdataCmsg(unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID})); !bytes.Equal(got, short) {
		t.Errorf("short frame: % x", got)
	}
	trunc := auxdataCmsg(unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID})
	(*unix.Cmsghdr)(unsafe.Pointer(&trunc[0])).SetLen(unix.CmsgLen(sizeofAuxdata - 1))
	if got := restoreVLAN(frame, trunc); !bytes.Equal(got, frame) {
		t.Errorf("truncated auxdata: % x", got)
	}
	foreign := auxdataCmsg(unix.TpacketAuxdata{Status: unix.TP_STATUS_VLAN_VALID})
	(*unix.Cmsghdr)(unsafe.Pointer(&foreign[0])).Level = unix.SOL_SOCKET
	if got := restoreVLAN(frame, foreign); !bytes.Equal(got, frame) {
		t.Errorf("foreign cmsg: % x", got)
	}
	if got := restoreVLAN(frame, nil); !bytes.Equal(got, frame) || &got[0] == &frame[0] {
		t.Errorf("no oob: must return a copy")
	}
}
