// SPDX-License-Identifier: MIT

package l2

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// sliceTransport delivers frames from a slice once start is closed, and
// then reports [ErrClosed], as a transport that was closed does.
type sliceTransport struct {
	start  chan struct{}
	frames [][]byte
}

func (s *sliceTransport) Send([]byte) error { return nil }
func (s *sliceTransport) Recv(context.Context) ([]byte, error) {
	<-s.start
	if len(s.frames) == 0 {
		return nil, ErrClosed
	}
	f := s.frames[0]
	s.frames = s.frames[1:]
	return f, nil
}
func (s *sliceTransport) HardwareAddr() MAC { return MAC{} }
func (s *sliceTransport) Close() error      { return nil }

// splitFrames reads frames from b, each a length octet followed by that
// many octets (fewer at the end of b).
func splitFrames(b []byte) [][]byte {
	var out [][]byte
	for len(b) > 0 {
		n := min(int(b[0]), len(b)-1)
		out = append(out, b[1:1+n])
		b = b[1+n:]
	}
	return out
}

func lengthPrefixed(frames ...[]byte) []byte {
	var b []byte
	for _, f := range frames {
		b = append(b, byte(len(f)))
		b = append(b, f...)
	}
	return b
}

// FuzzDemux feeds arbitrary frames through a [Demux] with a GOOSE port,
// an SV port and a port for both. Every frame is counted as received;
// each port gets, in order and unchanged, exactly the frames whose
// EtherType (one 802.1Q tag looked through) it registered; the frames no
// port registered are counted as unmatched; with queues as long as the
// input nothing overflows; after the transport ends every port returns
// its queued frames and then ErrClosed.
func FuzzDemux(f *testing.F) {
	goose := []byte{1, 0x0c, 0xcd, 1, 0, 1, 2, 0, 0, 0, 0, 1, 0x88, 0xb8, 0, 1}
	sv := []byte{1, 0x0c, 0xcd, 4, 0, 1, 2, 0, 0, 0, 0, 1, 0x88, 0xba, 0x40, 0}
	tagged := []byte{1, 0x0c, 0xcd, 1, 0, 1, 2, 0, 0, 0, 0, 1, 0x81, 0x00, 0x80, 0x01, 0x88, 0xb8, 0, 1}
	f.Add(lengthPrefixed(goose, sv, tagged))
	f.Add(lengthPrefixed(goose[:13], tagged[:17], []byte{}, sv))
	f.Add(lengthPrefixed(append(append([]byte(nil), goose[:12]...), 0x08, 0x00)))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		frames := splitFrames(b)
		tr := &sliceTransport{start: make(chan struct{}), frames: append([][]byte(nil), frames...)}
		d := NewDemux(tr, len(frames)+1)
		pg, errG := d.Port(EtherTypeGOOSE)
		ps, errS := d.Port(EtherTypeSV)
		pb, errB := d.Port(EtherTypeGOOSE, EtherTypeSV)
		close(tr.start)
		if errG != nil || errS != nil || errB != nil {
			t.Fatalf("Port: %v, %v, %v", errG, errS, errB)
		}
		var wantG, wantS, wantB [][]byte
		unmatched := 0
		for _, fr := range frames {
			et, ok := EtherTypeOf(fr)
			switch {
			case ok && et == EtherTypeGOOSE:
				wantG = append(wantG, fr)
				wantB = append(wantB, fr)
			case ok && et == EtherTypeSV:
				wantS = append(wantS, fr)
				wantB = append(wantB, fr)
			default:
				unmatched++
			}
		}
		gotG, gotS, gotB := drainPort(t, pg), drainPort(t, ps), drainPort(t, pb)
		if err := d.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if !errors.Is(d.Err(), ErrClosed) {
			t.Fatalf("Err after the transport ended: %v", d.Err())
		}
		if st := d.Stats(); st.Received != uint64(len(frames)) || st.Unmatched != uint64(unmatched) || st.Overflow != 0 {
			t.Fatalf("stats %+v for %d frames, %d matching no port", st, len(frames), unmatched)
		}
		sameFrames(t, "GOOSE port", gotG, wantG)
		sameFrames(t, "SV port", gotS, wantS)
		sameFrames(t, "GOOSE+SV port", gotB, wantB)
	})
}

// drainPort returns the frames p receives until it reports ErrClosed,
// which it does once the transport has ended.
func drainPort(t *testing.T, p *DemuxPort) [][]byte {
	t.Helper()
	var out [][]byte
	for {
		fr, err := p.Recv(context.Background())
		if err != nil {
			if !errors.Is(err, ErrClosed) {
				t.Fatalf("Recv after the transport ended: %v", err)
			}
			return out
		}
		out = append(out, fr)
	}
}

func sameFrames(t *testing.T, name string, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d frames, want %d", name, len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("%s: frame %d = % x, want % x", name, i, got[i], want[i])
		}
	}
}
