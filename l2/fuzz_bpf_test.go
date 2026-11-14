// SPDX-License-Identifier: MIT

//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package l2

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// FuzzNextRecord splits arbitrary read buffers into bpf_hdr records as
// the BPF transport's Recv does. Every record taken shortens the
// buffer, so the loop ends; a record is refused only when the buffer
// cannot hold its header or its capture; an accepted capture is the
// buffer's bytes after bh_hdrlen, of bh_caplen octets, in a copy the
// caller owns; a truncated capture (bh_caplen != bh_datalen) is skipped.
func FuzzNextRecord(f *testing.F) {
	f.Add(append(bpfRecord([]byte{1, 2, 3, 4, 5}, 5), bpfRecord([]byte{9, 8, 7}, 100)...))
	f.Add(bpfRecord([]byte{1}, 1)[:bpfHdrMin+1])
	f.Add(make([]byte, bpfHdrMin))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, buf []byte) {
		orig := append([]byte(nil), buf...)
		rest := buf
		for range len(buf) + 1 {
			fr, next, ok := nextRecord(rest)
			if !ok {
				if len(rest) >= bpfHdrMin {
					caplen := binary.NativeEndian.Uint32(rest[bpfCaplenOff:])
					start := int(binary.NativeEndian.Uint16(rest[bpfHdrlenOff:]))
					if start >= bpfHdrMin && start <= len(rest) && int64(caplen) <= int64(len(rest)-start) {
						t.Fatalf("record with header %d and capture %d refused in %d octets", start, caplen, len(rest))
					}
				}
				break
			}
			if len(next) >= len(rest) {
				t.Fatalf("record taken without progress: %d octets, then %d", len(rest), len(next))
			}
			caplen := binary.NativeEndian.Uint32(rest[bpfCaplenOff:])
			datalen := binary.NativeEndian.Uint32(rest[bpfDatalenOff:])
			start := int(binary.NativeEndian.Uint16(rest[bpfHdrlenOff:]))
			switch {
			case caplen != datalen:
				if fr != nil {
					t.Fatalf("truncated capture (%d of %d) returned", caplen, datalen)
				}
			case !bytes.Equal(fr, rest[start:start+int(caplen)]):
				t.Fatalf("capture % x, want % x", fr, rest[start:start+int(caplen)])
			case len(fr) > 0:
				fr[0] ^= 0xff
				if !bytes.Equal(buf, orig) {
					t.Fatal("the capture shares the read buffer")
				}
			}
			rest = next
		}
		if !bytes.Equal(buf, orig) {
			t.Fatal("nextRecord changed the read buffer")
		}
	})
}
