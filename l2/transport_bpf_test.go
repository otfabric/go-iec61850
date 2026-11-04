// SPDX-License-Identifier: MIT

//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package l2

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// bpfRecord builds one bpf_hdr record with the given capture.
func bpfRecord(capture []byte, datalen int) []byte {
	hdrlen := bpfHdrMin
	buf := make([]byte, (hdrlen+len(capture)+unix.BPF_ALIGNMENT-1)&^(unix.BPF_ALIGNMENT-1))
	h := (*unix.BpfHdr)(unsafe.Pointer(&buf[0]))
	h.Hdrlen = uint16(hdrlen)
	h.Caplen = uint32(len(capture))
	h.Datalen = uint32(datalen)
	copy(buf[hdrlen:], capture)
	return buf
}

func TestNextRecord(t *testing.T) {
	a, b := []byte{1, 2, 3, 4, 5}, []byte{9, 8, 7}
	buf := append(bpfRecord(a, len(a)), bpfRecord(b, 100)...) // b is truncated
	buf = append(buf, bpfRecord([]byte{6}, 1)...)

	f, rest, ok := nextRecord(buf)
	if !ok || string(f) != string(a) {
		t.Fatalf("first record: %v %v", f, ok)
	}
	f, rest, ok = nextRecord(rest)
	if !ok || f != nil {
		t.Fatalf("truncated record should be skipped: %v %v", f, ok)
	}
	f, rest, ok = nextRecord(rest)
	if !ok || len(f) != 1 || f[0] != 6 || len(rest) != 0 {
		t.Fatalf("last record: %v %v %d", f, ok, len(rest))
	}
	if _, _, ok := nextRecord(nil); ok {
		t.Error("empty buffer")
	}
	// Header claiming more capture than the buffer holds.
	bad := bpfRecord(a, len(a))
	(*unix.BpfHdr)(unsafe.Pointer(&bad[0])).Caplen = 1000
	if _, _, ok := nextRecord(bad); ok {
		t.Error("overlong capture accepted")
	}
	// Header length below the fixed fields.
	bad = bpfRecord(a, len(a))
	(*unix.BpfHdr)(unsafe.Pointer(&bad[0])).Hdrlen = 2
	if _, _, ok := nextRecord(bad); ok {
		t.Error("short header accepted")
	}
	// Last record without trailing alignment padding.
	exact := bpfRecord([]byte{1}, 1)[:bpfHdrMin+1]
	if f, rest, ok := nextRecord(exact); !ok || len(f) != 1 || len(rest) != 0 {
		t.Errorf("unpadded record: %v %v %d", f, ok, len(rest))
	}
}

func TestBPFOsErr(t *testing.T) {
	cases := []struct {
		op   string
		err  error
		kind error
	}{
		{"send", unix.EPERM, ErrPermission},
		{"send", unix.EACCES, ErrPermission},
		{"BIOCSETIF", unix.ENXIO, ErrInterfaceNotFound},
		{"BIOCSETIF", unix.ENODEV, ErrInterfaceNotFound},
		{"recv", unix.EBADF, ErrClosed},
		{"open /dev/bpf", unix.ENOENT, ErrUnsupportedPlatform},
		{"recv", unix.EIO, nil},
	}
	for _, c := range cases {
		err := osErr(c.op, "en0", c.err)
		if !errors.Is(err, c.err) {
			t.Errorf("%s %v: does not wrap the OS error", c.op, c.err)
		}
		if c.kind != nil && !errors.Is(err, c.kind) {
			t.Errorf("%s %v: kind = %v, want %v", c.op, c.err, err, c.kind)
		}
	}
}

// TestNextRecordBounds checks the header fields at their wrap points:
// the decoder must reject, never slice out of range or overflow.
func TestNextRecordBounds(t *testing.T) {
	a := []byte{1, 2, 3, 4}
	set := func(mut func(h *unix.BpfHdr)) []byte {
		b := bpfRecord(a, len(a))
		mut((*unix.BpfHdr)(unsafe.Pointer(&b[0])))
		return b
	}
	for name, buf := range map[string][]byte{
		"hdrlen past buffer":  set(func(h *unix.BpfHdr) { h.Hdrlen = 0xffff; h.Caplen = 0 }),
		"caplen max uint32":   set(func(h *unix.BpfHdr) { h.Caplen = 0xffffffff }),
		"caplen one too long": set(func(h *unix.BpfHdr) { h.Caplen = uint32(len(a)) + uint32(unix.BPF_ALIGNMENT) }),
		"short header":        make([]byte, bpfHdrMin-1),
	} {
		if _, _, ok := nextRecord(buf); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	// Caplen exactly filling the buffer is accepted.
	b := bpfRecord(a, len(a))
	h := (*unix.BpfHdr)(unsafe.Pointer(&b[0]))
	h.Caplen = uint32(len(b) - bpfHdrMin)
	h.Datalen = h.Caplen
	if f, rest, ok := nextRecord(b); !ok || len(f) != len(b)-bpfHdrMin || len(rest) != 0 {
		t.Errorf("full-buffer capture: %d %v %d", len(f), ok, len(rest))
	}
	// The decoder agrees with the struct layout for every field.
	if bpfHdrMin != int(unsafe.Offsetof(unix.BpfHdr{}.Hdrlen))+2 || bpfCaplenOff >= bpfHdrlenOff {
		t.Error("bpf_hdr offsets")
	}
}

// TestOpenBPF opens a real BPF device on the loopback interface when the
// process may (macOS ChmodBPF / access_bpf group or root), covering
// BIOCSETIF via IoctlSetString, BIOCSETF and BIOCSRTIMEOUT.
func TestOpenBPF(t *testing.T) {
	iface := rawTestIface()
	if iface == "" {
		iface = "lo0" // receive-only: nothing is sent
	}
	tr := openOrSkip(t, iface, TransportOptions{EtherTypes: []uint16{EtherTypeGOOSE, EtherTypeSV}, Multicast: []MAC{{0x01, 0x0c, 0xcd, 0x01}}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*pollInterval)
	defer cancel()
	if _, err := tr.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Recv on idle loopback: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Error(err)
	}
	if _, err := Open("this-interface-name-is-too-long0", TransportOptions{}); err == nil {
		t.Error("overlong interface name accepted")
	}
}
