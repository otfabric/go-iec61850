// SPDX-License-Identifier: MIT

package l2

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// testFrame builds a minimal Ethernet frame (optionally 802.1Q tagged)
// with the given EtherType and payload, padded to 60 octets.
func testFrame(dst, src MAC, et uint16, vlan bool, payload []byte) []byte {
	b := append([]byte(nil), dst[:]...)
	b = append(b, src[:]...)
	if vlan {
		b = binary.BigEndian.AppendUint16(b, EtherTypeVLAN)
		b = binary.BigEndian.AppendUint16(b, 4<<13|10)
	}
	b = binary.BigEndian.AppendUint16(b, et)
	b = append(b, payload...)
	for len(b) < 60 {
		b = append(b, 0)
	}
	return b
}

// runBPF interprets the subset of classic BPF emitted by etherTypeFilter.
func runBPF(t *testing.T, prog []bpfInsn, pkt []byte) uint32 {
	t.Helper()
	var a uint32
	for pc := 0; pc < len(prog); pc++ {
		in := prog[pc]
		switch in.code {
		case bpfLdhAbs:
			if int(in.k)+2 > len(pkt) {
				return 0
			}
			a = uint32(binary.BigEndian.Uint16(pkt[in.k:]))
		case bpfJeqK:
			if a == in.k {
				pc += int(in.jt)
			} else {
				pc += int(in.jf)
			}
		case bpfRetK:
			return in.k
		default:
			t.Fatalf("unexpected opcode %#x", in.code)
		}
	}
	t.Fatal("program fell off the end")
	return 0
}

func TestEtherTypeFilter(t *testing.T) {
	dst, src := MAC{0x01, 0x0c, 0xcd, 0x04}, MAC{0x02}
	for _, types := range [][]uint16{{EtherTypeGOOSE}, {EtherTypeSV}, {EtherTypeGOOSE, EtherTypeSV}, {EtherTypeGOOSE, EtherTypeGSE, EtherTypeSV}} {
		prog := etherTypeFilter(types)
		for _, et := range []uint16{EtherTypeGOOSE, EtherTypeGSE, EtherTypeSV, 0x0800, 0x88b5} {
			want := uint32(0)
			for _, x := range types {
				if x == et {
					want = 0xffff
				}
			}
			for _, vlan := range []bool{false, true} {
				if got := runBPF(t, prog, testFrame(dst, src, et, vlan, nil)); got != want {
					t.Errorf("types %04x et %04x vlan %v: %d, want %d", types, et, vlan, got, want)
				}
			}
		}
		// VLAN-tagged frame carrying another VLAN tag is rejected.
		if runBPF(t, prog, testFrame(dst, src, EtherTypeVLAN, true, nil)) != 0 {
			t.Error("double tag accepted")
		}
	}
	// The largest list still yields byte-sized jumps.
	many := make([]uint16, maxEtherTypes)
	for i := range many {
		many[i] = 0x9000 + uint16(i)
	}
	prog := etherTypeFilter(many)
	if runBPF(t, prog, testFrame(dst, src, many[maxEtherTypes-1], true, nil)) != 0xffff {
		t.Error("last EtherType of a full list not accepted")
	}
}

func TestEtherTypesOption(t *testing.T) {
	if got, err := etherTypes(TransportOptions{}); err != nil || len(got) != 1 || got[0] != EtherTypeGOOSE {
		t.Errorf("default = %v, %v", got, err)
	}
	for _, bad := range [][]uint16{{0x0100}, {EtherTypeVLAN}, make([]uint16, maxEtherTypes+1)} {
		if _, err := etherTypes(TransportOptions{EtherTypes: bad}); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%v: err = %v", bad, err)
		}
	}
	if _, err := Open("eth0", TransportOptions{EtherTypes: []uint16{1}}); !errors.Is(err, ErrInvalidConfig) && !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("Open with bad EtherType: %v", err)
	}
}

func TestEtherTypeOf(t *testing.T) {
	if et, ok := EtherTypeOf(testFrame(MAC{}, MAC{}, EtherTypeSV, true, nil)); !ok || et != EtherTypeSV {
		t.Errorf("tagged: %04x %v", et, ok)
	}
	if et, ok := EtherTypeOf(testFrame(MAC{}, MAC{}, EtherTypeGOOSE, false, nil)); !ok || et != EtherTypeGOOSE {
		t.Errorf("untagged: %04x %v", et, ok)
	}
	if _, ok := EtherTypeOf(make([]byte, 13)); ok {
		t.Error("short frame")
	}
	short := testFrame(MAC{}, MAC{}, EtherTypeSV, true, nil)[:16]
	if _, ok := EtherTypeOf(short); ok {
		t.Error("short tagged frame")
	}
}

func TestMAC(t *testing.T) {
	for _, s := range []string{"01-0C-CD-04-00-01", "01:0c:cd:04:00:01", "010CCD040001", " 01-0c-cd-04-00-01 "} {
		m, err := ParseMAC(s)
		if err != nil || m != (MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}) {
			t.Errorf("%q: %v %v", s, m, err)
		}
		if !m.IsSVMulticast() || m.IsGOOSEMulticast() || !m.IsMulticast() || m.IsZero() {
			t.Errorf("%q: classification", s)
		}
	}
	for _, s := range []string{"", "01-0C", "zz-0c-cd-04-00-01", "01-02-03-04-05-06-07-08"} {
		if _, err := ParseMAC(s); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%q: err = %v", s, err)
		}
	}
	if (MAC{0x01, 0x0c, 0xcd, 0x04, 0x02}).IsSVMulticast() {
		t.Error("outside SV range")
	}
	var m MAC
	if err := m.UnmarshalText([]byte("01-0C-CD-01-00-02")); err != nil || !m.IsGOOSEMulticast() {
		t.Errorf("UnmarshalText: %v %v", m, err)
	}
	if err := m.UnmarshalText([]byte("x")); err == nil {
		t.Error("UnmarshalText accepted garbage")
	}
	if b, _ := m.MarshalText(); string(b) != "01:0c:cd:01:00:02" || m.String() != "01:0c:cd:01:00:02" {
		t.Errorf("text %s", b)
	}
	if !(MAC{}).IsZero() || Broadcast.String() != "ff:ff:ff:ff:ff:ff" {
		t.Error("zero/broadcast")
	}
}

func TestErrorTypes(t *testing.T) {
	ce := &ConfigError{Field: "f", Message: "m"}
	te := &TransportError{Op: "open", Interface: "eth0", Err: errors.New("boom"), Kind: ErrPermission}
	plain := &TransportError{Op: "send", Interface: "eth0", Err: errors.New("boom")}
	for _, e := range []error{ce, te, plain} {
		if e.Error() == "" {
			t.Errorf("%T: empty", e)
		}
	}
	if !errors.Is(ce, ErrInvalidConfig) || !errors.Is(te, ErrPermission) || errors.Is(plain, ErrPermission) {
		t.Error("unwrap")
	}
	if orDiscard(nil) == nil {
		t.Error("orDiscard(nil)")
	}
	h := orDiscard(nil).Handler()
	if h.Enabled(context.Background(), 0) || h.Handle(context.Background(), recordForTest()) != nil || h.WithAttrs(nil) == nil || h.WithGroup("g") == nil {
		t.Error("discard handler")
	}
}

// TestHub covers delivery, faults and close on the in-memory segment.
func TestHub(t *testing.T) {
	h := NewHub()
	a, b, c := h.NewPort(MAC{}), h.NewPort(MAC{}), h.NewPort(MAC{0x02, 0, 0, 0, 0, 0x99})
	if a.HardwareAddr() == b.HardwareAddr() || c.HardwareAddr() != (MAC{0x02, 0, 0, 0, 0, 0x99}) {
		t.Fatal("port addresses")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := testFrame(Broadcast, a.HardwareAddr(), EtherTypeSV, false, []byte{1})
	if err := a.Send(f); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*HubPort{b, c} {
		if got, err := p.Recv(ctx); err != nil || got[14] != 1 {
			t.Fatalf("recv: %v", err)
		}
	}

	// Faults: mutation dropping everything, then duplication.
	h.SetFaults(Faults{Mutate: func([]byte) []byte { return nil }})
	_ = a.Send(f)
	h.SetFaults(Faults{Duplicate: 1, Seed: 1})
	_ = a.Send(f)
	for i := 0; i < 2; i++ {
		if _, err := b.Recv(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Reorder holds one frame back until Flush.
	h.SetFaults(Faults{Reorder: 1})
	_ = a.Send(f)
	h.SetFaults(Faults{})
	h.Flush()
	if _, err := b.Recv(ctx); err != nil {
		t.Fatal(err)
	}
	// Loss and delay.
	h.SetFaults(Faults{Loss: 1})
	_ = a.Send(f)
	h.SetFaults(Faults{Delay: time.Millisecond})
	_ = a.Send(f)
	if _, err := b.Recv(ctx); err != nil {
		t.Fatal(err)
	}
	if st := h.Stats(); st.Lost < 2 || st.Duplicated != 2 || st.Reordered != 2 || st.Sent == 0 {
		t.Errorf("stats %+v", st)
	}

	cctx, ccancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer ccancel()
	if _, err := b.Recv(cctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("recv timeout: %v", err)
	}
	_ = b.Close()
	_ = b.Close()
	if _, err := b.Recv(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("recv after close: %v", err)
	}
	if err := b.Send(f); !errors.Is(err, ErrClosed) {
		t.Errorf("send after close: %v", err)
	}
}

func TestHubOverflow(t *testing.T) {
	h := NewHub()
	a, b := h.NewPort(MAC{}), h.NewPort(MAC{})
	f := testFrame(Broadcast, a.HardwareAddr(), EtherTypeGOOSE, false, nil)
	for i := 0; i < 1100; i++ {
		_ = a.Send(f)
	}
	if h.Stats().Overflow == 0 {
		t.Error("no overflow counted")
	}
	_ = b.Close()
}

// errTransport fails Recv with a fixed error after delivering frames.
type errTransport struct {
	frames chan []byte
	err    error
	sent   [][]byte
	mu     sync.Mutex
	once   sync.Once
	closed chan struct{}
}

func newErrTransport(err error) *errTransport {
	return &errTransport{frames: make(chan []byte, 16), err: err, closed: make(chan struct{})}
}

func (e *errTransport) Send(f []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sent = append(e.sent, f)
	return nil
}

func (e *errTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case f, ok := <-e.frames:
		if !ok {
			return nil, e.err
		}
		return f, nil
	case <-e.closed:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *errTransport) HardwareAddr() MAC { return MAC{0x02, 0, 0, 0, 0, 7} }

func (e *errTransport) Close() error {
	e.once.Do(func() { close(e.closed) })
	return nil
}

func TestDemux(t *testing.T) {
	h := NewHub()
	tx := h.NewPort(MAC{})
	d := NewDemux(h.NewPort(MAC{}), 2)
	defer func() { _ = d.Close() }()

	gp, err := d.Port(EtherTypeGOOSE)
	if err != nil {
		t.Fatal(err)
	}
	sp1, _ := d.Port(EtherTypeSV)
	sp2, _ := d.Port(EtherTypeSV, EtherTypeGSE)
	if _, err := d.Port(); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("empty port: %v", err)
	}
	if et := sp2.EtherTypes(); len(et) != 2 || sp2.HardwareAddr() == (MAC{}) {
		t.Errorf("port info %v", et)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Send(testFrame(Broadcast, tx.HardwareAddr(), EtherTypeGOOSE, false, []byte{1}))
	_ = tx.Send(testFrame(Broadcast, tx.HardwareAddr(), EtherTypeSV, true, []byte{2}))
	_ = tx.Send(testFrame(Broadcast, tx.HardwareAddr(), 0x0800, false, nil))
	_ = tx.Send([]byte{1, 2, 3})

	if f, err := gp.Recv(ctx); err != nil || f[14] != 1 {
		t.Fatalf("goose port: %v", err)
	}
	for _, p := range []*DemuxPort{sp1, sp2} {
		f, err := p.Recv(ctx)
		if et, _ := EtherTypeOf(f); err != nil || et != EtherTypeSV {
			t.Fatalf("sv port: %v", err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for d.Stats().Unmatched < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if st := d.Stats(); st.Received != 4 || st.Unmatched != 2 {
		t.Errorf("stats %+v", st)
	}

	// Overflow on a port whose receiver does not read (queue 2).
	for i := 0; i < 5; i++ {
		_ = tx.Send(testFrame(Broadcast, tx.HardwareAddr(), EtherTypeGOOSE, false, nil))
	}
	for d.Stats().Overflow < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if d.Stats().Overflow < 3 {
		t.Errorf("overflow %+v", d.Stats())
	}

	// Sending through a port reaches the segment.
	if err := gp.Send(testFrame(Broadcast, MAC{}, EtherTypeGOOSE, false, []byte{9})); err != nil {
		t.Fatal(err)
	}
	if f, err := tx.Recv(ctx); err != nil || f[14] != 9 {
		t.Fatalf("send via port: %v", err)
	}

	// A closed port is detached; the others keep working.
	_ = sp1.Close()
	if _, err := sp1.Recv(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("closed port recv: %v", err)
	}
	if err := sp1.Send(nil); !errors.Is(err, ErrClosed) {
		t.Errorf("closed port send: %v", err)
	}
	cctx, ccancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer ccancel()
	if _, err := sp2.Recv(cctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("idle port: %v", err)
	}

	if d.Err() != nil {
		t.Errorf("Err while running: %v", d.Err())
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	if !errors.Is(d.Err(), ErrClosed) {
		t.Errorf("Err after close: %v", d.Err())
	}
	// Queued frames are drained before ErrClosed.
	n := 0
	for {
		if _, err := gp.Recv(ctx); err != nil {
			if !errors.Is(err, ErrClosed) {
				t.Errorf("recv after demux close: %v", err)
			}
			break
		}
		n++
	}
	if n == 0 {
		t.Error("queued frames lost on close")
	}
	if _, err := d.Port(EtherTypeSV); !errors.Is(err, ErrClosed) {
		t.Errorf("Port after close: %v", err)
	}
}

func TestDemuxReceiveError(t *testing.T) {
	boom := errors.New("link down")
	et := newErrTransport(boom)
	d := NewDemux(et, 0)
	p, _ := d.Port(EtherTypeSV)
	et.frames <- testFrame(Broadcast, MAC{}, EtherTypeSV, false, nil)
	close(et.frames)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Recv(ctx); err != nil {
		t.Fatalf("queued frame: %v", err)
	}
	if _, err := p.Recv(ctx); !errors.Is(err, boom) {
		t.Errorf("recv: %v, want link down", err)
	}
	if !errors.Is(d.Err(), boom) {
		t.Errorf("Err = %v", d.Err())
	}
	if _, err := d.Port(EtherTypeSV); !errors.Is(err, boom) {
		t.Errorf("Port after failure: %v", err)
	}
	if err := p.Send([]byte{1}); !errors.Is(err, boom) {
		t.Errorf("send after failure: %v", err)
	}
	_ = d.Close()
}

// TestDemuxPortAfterStop covers Port between the receive goroutine
// stopping on ErrClosed and Close being called.
func TestDemuxPortAfterStop(t *testing.T) {
	et := newErrTransport(ErrClosed)
	d := NewDemux(et, 0)
	close(et.frames)
	<-d.done
	if !errors.Is(d.Err(), ErrClosed) {
		t.Errorf("Err = %v", d.Err())
	}
	if _, err := d.Port(EtherTypeSV); !errors.Is(err, ErrClosed) {
		t.Errorf("Port: %v", err)
	}
	_ = d.Close()
}

func recordForTest() slog.Record { return slog.Record{} }

func TestHubBehaviour(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	a, b := hub.NewPort(MAC{}), hub.NewPort(MAC{9})
	if a.HardwareAddr() == b.HardwareAddr() || b.HardwareAddr() != (MAC{9}) {
		t.Fatal("port addresses")
	}
	hub.SetFaults(Faults{Delay: 5 * time.Millisecond})
	start := time.Now()
	_ = a.Send([]byte{1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if f, err := b.Recv(ctx); err != nil || f[0] != 1 || time.Since(start) < 4*time.Millisecond {
		t.Fatalf("delayed recv: %v %v", f, err)
	}
	hub.SetFaults(Faults{Reorder: 1})
	_ = a.Send([]byte{2})
	_ = a.Send([]byte{3}) // held frame 2 is released after 3; 3 is then held? no: one held per port
	f1, _ := b.Recv(ctx)
	f2, _ := b.Recv(ctx)
	if f1[0] != 3 || f2[0] != 2 {
		t.Fatalf("reorder: %v %v", f1, f2)
	}
	_ = a.Send([]byte{4}) // held
	hub.Flush()
	if f, _ := b.Recv(ctx); f[0] != 4 {
		t.Fatalf("flush: %v", f)
	}
	// Overflow: fill b's queue.
	hub.SetFaults(Faults{})
	for i := 0; i < 1100; i++ {
		_ = a.Send([]byte{5})
	}
	if hub.Stats().Overflow == 0 {
		t.Error("no overflow counted")
	}
	// Recv honours ctx and Close.
	short, cancel2 := context.WithCancel(context.Background())
	cancel2()
	c := hub.NewPort(MAC{})
	if _, err := c.Recv(short); err != context.Canceled {
		t.Errorf("recv ctx: %v", err)
	}
	_ = c.Close()
	_ = c.Close()
	if _, err := c.Recv(context.Background()); err != ErrClosed {
		t.Errorf("recv closed: %v", err)
	}
	if err := c.Send([]byte{1}); err != ErrClosed {
		t.Errorf("send closed: %v", err)
	}
	c.deliver([]byte{1}, hub) // no panic after close
}
