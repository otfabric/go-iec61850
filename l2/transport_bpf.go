// SPDX-License-Identifier: MIT

//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package l2

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// pollInterval bounds how long Recv blocks in the kernel before
// re-checking ctx and Close (BIOCSRTIMEOUT).
const pollInterval = 100 * time.Millisecond

// bpfTransport is the macOS/BSD /dev/bpf backend.
type bpfTransport struct {
	iface string
	mac   MAC

	mu      sync.RWMutex
	fd      int
	closed  bool
	rmu     sync.Mutex
	buf     []byte
	pending []byte // unread records from the last read
}

// Open opens a raw Ethernet transport on the named interface using a
// BPF device (/dev/bpf*). It requires read/write access to the device
// (root, or membership of the group that owns /dev/bpf*, e.g. via
// Wireshark's ChmodBPF). A kernel filter limits delivery to
// opts.EtherTypes (default GOOSE). Frames sent by this transport are not looped back.
//
// BPF has no per-group multicast membership, so the interface is put in
// promiscuous mode unless opts.Multicast is non-empty and
// opts.Promiscuous is false; in that case only groups the host has
// already joined are received.
func Open(iface string, opts TransportOptions) (Transport, error) {
	types, err := etherTypes(opts)
	if err != nil {
		return nil, err
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, &TransportError{Op: "open", Interface: iface, Err: err, Kind: ErrInterfaceNotFound}
	}
	if len(iface) >= unix.IFNAMSIZ {
		return nil, &TransportError{Op: "open", Interface: iface, Err: errors.New("interface name too long"), Kind: ErrInterfaceNotFound}
	}
	fd, err := openBPF()
	if err != nil {
		return nil, osErr("open /dev/bpf", iface, err)
	}
	var mac MAC
	copy(mac[:], ifi.HardwareAddr)
	t := &bpfTransport{iface: iface, mac: mac, fd: fd}
	promisc := opts.Promiscuous || len(opts.Multicast) == 0
	if err := t.setup(types, promisc, opts.ReceiveLocal); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	orDiscard(opts.Logger).Info("l2: BPF transport opened", "iface", iface, "mac", mac.String(), "promiscuous", promisc)
	return t, nil
}

func openBPF() (int, error) {
	fd, err := unix.Open("/dev/bpf", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err == nil {
		return fd, nil
	}
	last := err
	for i := range 256 {
		fd, err := unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		last = err
		if errors.Is(err, unix.EBUSY) {
			continue
		}
		if errors.Is(err, unix.ENOENT) && i > 0 {
			break
		}
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			return -1, err
		}
	}
	return -1, last
}

// ioctlPtr issues an ioctl whose argument is a pointer to a struct.
// x/sys/unix exports no typed setter for struct bpf_program
// (BIOCSETF) or struct timeval (BIOCSRTIMEOUT), and the bpf_program
// argument itself holds a pointer, so these two requests have no
// unsafe-free form; every such call goes through this one function.
func ioctlPtr[T unix.BpfProgram | unix.Timeval](fd int, req uint, arg *T) error {
	//lint:ignore SA1019 no libSystem wrapper takes a pointer argument
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, //nolint:staticcheck // SA1019, see above
		uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(arg))); e != 0 {
		return e
	}
	return nil
}

// sizeofIfreq is sizeof(struct ifreq), the argument size encoded in
// BIOCSETIF (_IOW('B', 108, struct ifreq)).
const sizeofIfreq = 32

func (t *bpfTransport) setup(types []uint16, promisc, local bool) error {
	fd := t.fd
	// struct ifreq starts with the NUL-padded name; pad it to the full
	// struct size because the kernel copies in sizeof(struct ifreq).
	// IoctlSetString appends the final NUL; Open bounds len(iface).
	ifr := t.iface + strings.Repeat("\x00", sizeofIfreq-1-len(t.iface))
	if err := unix.IoctlSetString(fd, unix.BIOCSETIF, ifr); err != nil {
		return osErr("BIOCSETIF", t.iface, err)
	}
	for _, s := range []struct {
		name string
		req  uint
	}{{"BIOCIMMEDIATE", unix.BIOCIMMEDIATE}, {"BIOCSHDRCMPLT", unix.BIOCSHDRCMPLT}} {
		if err := unix.IoctlSetPointerInt(fd, s.req, 1); err != nil {
			return osErr(s.name, t.iface, err)
		}
	}
	if err := setSeeSent(fd, local); err != nil {
		return osErr("BIOCSSEESENT", t.iface, err)
	}
	filter := etherTypeFilter(types)
	insns := make([]unix.BpfInsn, len(filter))
	for i, in := range filter {
		insns[i] = unix.BpfInsn{Code: in.code, Jt: in.jt, Jf: in.jf, K: in.k}
	}
	n := len(insns)
	if n == 0 || n > unix.BPF_MAXINSNS {
		return &ConfigError{Field: "EtherTypes", Message: "BPF program length out of range"}
	}
	prog := unix.BpfProgram{Len: uint32(n), Insns: &insns[0]}
	if err := ioctlPtr(fd, unix.BIOCSETF, &prog); err != nil {
		return osErr("BIOCSETF", t.iface, err)
	}
	tv := unix.NsecToTimeval(pollInterval.Nanoseconds())
	if err := ioctlPtr(fd, unix.BIOCSRTIMEOUT, &tv); err != nil {
		return osErr("BIOCSRTIMEOUT", t.iface, err)
	}
	if promisc {
		if err := unix.IoctlSetInt(fd, unix.BIOCPROMISC, 0); err != nil {
			return osErr("BIOCPROMISC", t.iface, err)
		}
	}
	n, err := unix.IoctlGetInt(fd, unix.BIOCGBLEN)
	if err != nil {
		return osErr("BIOCGBLEN", t.iface, err)
	}
	t.buf = make([]byte, n)
	return nil
}

func (t *bpfTransport) Send(frame []byte) error {
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

func (t *bpfTransport) Recv(ctx context.Context) ([]byte, error) {
	t.rmu.Lock()
	defer t.rmu.Unlock()
	for {
		if f, rest, ok := nextRecord(t.pending); ok {
			t.pending = rest
			if f != nil {
				return f, nil
			}
			continue
		}
		t.pending = nil
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t.mu.RLock()
		if t.closed {
			t.mu.RUnlock()
			return nil, ErrClosed
		}
		n, err := unix.Read(t.fd, t.buf)
		t.mu.RUnlock()
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, osErr("recv", t.iface, err)
		}
		t.pending = t.buf[:n]
	}
}

// bpf_hdr fields are decoded in host byte order at the offsets of the
// x/sys layout (the timestamp width differs between the BSDs).
const (
	bpfCaplenOff  = int(unsafe.Offsetof(unix.BpfHdr{}.Caplen))
	bpfDatalenOff = int(unsafe.Offsetof(unix.BpfHdr{}.Datalen))
	bpfHdrlenOff  = int(unsafe.Offsetof(unix.BpfHdr{}.Hdrlen))
)

// bpfHdrMin is the size of the bpf_hdr fields without trailing
// padding; bh_hdrlen may be smaller than sizeof(struct bpf_hdr) (18 vs
// 20 on Darwin).
const bpfHdrMin = bpfHdrlenOff + 2

// nextRecord splits one bpf_hdr record off buf. It returns ok=false when
// buf holds no complete record, and f=nil for a truncated capture.
func nextRecord(buf []byte) (f, rest []byte, ok bool) {
	if len(buf) < bpfHdrMin {
		return nil, nil, false
	}
	caplen := binary.NativeEndian.Uint32(buf[bpfCaplenOff:])
	datalen := binary.NativeEndian.Uint32(buf[bpfDatalenOff:])
	start := int(binary.NativeEndian.Uint16(buf[bpfHdrlenOff:]))
	if start < bpfHdrMin || start > len(buf) || int64(caplen) > int64(len(buf)-start) {
		return nil, nil, false
	}
	end := start + int(caplen)
	next := min((end+unix.BPF_ALIGNMENT-1)&^(unix.BPF_ALIGNMENT-1), len(buf))
	if caplen != datalen {
		return nil, buf[next:], true
	}
	return append([]byte(nil), buf[start:end]...), buf[next:], true
}

func (t *bpfTransport) HardwareAddr() MAC { return t.mac }

func (t *bpfTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return unix.Close(t.fd)
}

func osErr(op, iface string, err error) error {
	te := &TransportError{Op: op, Interface: iface, Err: err}
	switch {
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		te.Kind = ErrPermission
	case errors.Is(err, unix.ENXIO), errors.Is(err, unix.ENODEV):
		te.Kind = ErrInterfaceNotFound
	case errors.Is(err, unix.EBADF):
		te.Kind = ErrClosed
	}
	if strings.HasPrefix(op, "open") && errors.Is(err, unix.ENOENT) {
		te.Kind = ErrUnsupportedPlatform
	}
	return te
}
