// SPDX-License-Identifier: MIT

//go:build linux

package goose

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// pollInterval bounds how long Recv blocks in the kernel before
// re-checking ctx and Close.
const pollInterval = 100 * time.Millisecond

// packetTransport is the Linux AF_PACKET backend.
type packetTransport struct {
	iface string
	mac   MAC
	idx   int

	mu     sync.RWMutex // held for reading during syscalls, for writing by Close
	fd     int
	closed bool
	rbuf   []byte
	oob    []byte
	rmu    sync.Mutex // serialises Recv buffers
}

// Open opens a raw Ethernet transport on the named interface using an
// AF_PACKET socket. It requires CAP_NET_RAW (or root). A kernel socket
// filter limits delivery to GOOSE frames (untagged or 802.1Q tagged).
// VLAN tags stripped by NIC offload are restored from PACKET_AUXDATA so
// that decoded frames report their VLAN.
func Open(iface string, opts TransportOptions) (Transport, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, &TransportError{Op: "open", Interface: iface, Err: err, Kind: ErrInterfaceNotFound}
	}
	var mac MAC
	copy(mac[:], ifi.HardwareAddr)
	proto := htons(unix.ETH_P_ALL)
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(proto))
	if err != nil {
		return nil, osErr("socket", iface, err)
	}
	t := &packetTransport{iface: iface, mac: mac, idx: ifi.Index, fd: fd, rbuf: make([]byte, 1<<16), oob: make([]byte, unix.CmsgSpace(sizeofAuxdata))}
	if err := t.setup(proto, opts); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	orDiscard(opts.Logger).Info("goose: AF_PACKET transport opened", "iface", iface, "mac", mac.String())
	return t, nil
}

func (t *packetTransport) setup(proto uint16, opts TransportOptions) error {
	fd := t.fd
	prog := make([]unix.SockFilter, len(gooseFilter))
	for i, in := range gooseFilter {
		prog[i] = unix.SockFilter{Code: in.code, Jt: in.jt, Jf: in.jf, K: in.k}
	}
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	if err := unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &fprog); err != nil {
		return osErr("attach filter", t.iface, err)
	}
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: proto, Ifindex: t.idx}); err != nil {
		return osErr("bind", t.iface, err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_AUXDATA, 1); err != nil {
		return osErr("auxdata", t.iface, err)
	}
	tv := unix.NsecToTimeval(pollInterval.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return osErr("rcvtimeo", t.iface, err)
	}
	add := func(typ uint16, addr []byte) error {
		mr := unix.PacketMreq{Ifindex: int32(t.idx), Type: typ, Alen: uint16(len(addr))}
		copy(mr.Address[:], addr)
		return unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &mr)
	}
	if opts.Promiscuous {
		if err := add(unix.PACKET_MR_PROMISC, nil); err != nil {
			return osErr("promisc", t.iface, err)
		}
	}
	if len(opts.Multicast) == 0 {
		if err := add(unix.PACKET_MR_ALLMULTI, nil); err != nil {
			return osErr("allmulti", t.iface, err)
		}
	}
	for _, m := range opts.Multicast {
		if err := add(unix.PACKET_MR_MULTICAST, m[:]); err != nil {
			return osErr("multicast "+m.String(), t.iface, err)
		}
	}
	return nil
}

func (t *packetTransport) Send(frame []byte) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return ErrClosed
	}
	if _, err := unix.Write(t.fd, frame); err != nil {
		return osErr("send", t.iface, err)
	}
	return nil
}

func (t *packetTransport) Recv(ctx context.Context) ([]byte, error) {
	t.rmu.Lock()
	defer t.rmu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t.mu.RLock()
		if t.closed {
			t.mu.RUnlock()
			return nil, ErrClosed
		}
		n, oobn, _, from, err := unix.Recvmsg(t.fd, t.rbuf, t.oob, 0)
		t.mu.RUnlock()
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, osErr("recv", t.iface, err)
		}
		if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll.Pkttype == unix.PACKET_OUTGOING {
			continue
		}
		return restoreVLAN(t.rbuf[:n], t.oob[:oobn]), nil
	}
}

// restoreVLAN copies frame, re-inserting an 802.1Q tag reported in
// PACKET_AUXDATA.
func restoreVLAN(frame, oob []byte) []byte {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err == nil {
		for _, m := range msgs {
			if m.Header.Level != unix.SOL_PACKET || m.Header.Type != unix.PACKET_AUXDATA || len(m.Data) < sizeofAuxdata {
				continue
			}
			aux := (*unix.TpacketAuxdata)(unsafe.Pointer(&m.Data[0]))
			if aux.Status&unix.TP_STATUS_VLAN_VALID == 0 || len(frame) < 12 {
				break
			}
			tpid := EtherTypeVLAN
			if aux.Status&unix.TP_STATUS_VLAN_TPID_VALID != 0 && aux.Vlan_tpid != 0 {
				tpid = aux.Vlan_tpid
			}
			out := make([]byte, 0, len(frame)+4)
			out = append(out, frame[:12]...)
			out = binary.BigEndian.AppendUint16(out, tpid)
			out = binary.BigEndian.AppendUint16(out, aux.Vlan_tci)
			return append(out, frame[12:]...)
		}
	}
	return append([]byte(nil), frame...)
}

func (t *packetTransport) HardwareAddr() MAC { return t.mac }

func (t *packetTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return unix.Close(t.fd)
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

func osErr(op, iface string, err error) error {
	te := &TransportError{Op: op, Interface: iface, Err: err}
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
		te.Kind = ErrPermission
	}
	if errors.Is(err, unix.EBADF) {
		te.Kind = ErrClosed
	}
	return te
}

const sizeofAuxdata = int(unsafe.Sizeof(unix.TpacketAuxdata{}))
