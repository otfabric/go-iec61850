// SPDX-License-Identifier: MIT

//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package l2

import (
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
