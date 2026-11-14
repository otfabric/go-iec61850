// SPDX-License-Identifier: MIT

package rsession_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/internal/leakcheck"
	"github.com/otfabric/go-iec61850/l2"
	"github.com/otfabric/go-iec61850/rsession"
	"github.com/otfabric/go-iec61850/sv"
)

var loopback = netip.MustParseAddrPort("127.0.0.1:0")

func ring(t *testing.T) *rsession.KeyRing {
	t.Helper()
	r, err := rsession.NewKeyRing(rsession.Key{ID: 1, Material: bytes.Repeat([]byte{0x11}, 32), MAC: rsession.MACHMACSHA256_128})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func open(t *testing.T, cfg rsession.Config) *rsession.Transport {
	t.Helper()
	tr, err := rsession.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// pair opens a receiver on loopback and a sender aimed at it.
func pair(t *testing.T, si rsession.SessionID, rx, tx rsession.Config) (recv, send *rsession.Transport) {
	t.Helper()
	rx.SI, tx.SI = si, si
	rx.LocalAddr = loopback
	recv = open(t, rx)
	tx.RemoteAddr = recv.LocalAddr()
	send = open(t, tx)
	return recv, send
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestGOOSEOverUDP(t *testing.T) {
	keys := ring(t)
	recv, send := pair(t, rsession.SIGOOSE, rsession.Config{Keys: keys}, rsession.Config{Keys: keys})

	sub, err := goose.NewSubscriber(recv, goose.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	var mu sync.Mutex
	var got []*goose.Frame
	_, err = sub.Subscribe(goose.SubscriptionConfig{
		GocbRef: "IED1LD0/LLN0$GO$gcb1",
		Src:     rsession.SourceMAC(netip.MustParseAddr("127.0.0.1")),
		// The publisher sets the simulation bit, which the session
		// header carries.
		AcceptSimulation: true,
		Handler: func(e goose.Event) {
			if e.Kind == goose.EventMessage {
				mu.Lock()
				got = append(got, e.Frame)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := goose.NewPublisher(send, goose.PublisherConfig{
		Dst: goose.MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01}, AppID: 0x0003,
		GocbRef: "IED1LD0/LLN0$GO$gcb1", DatSet: "IED1LD0/LLN0$ds1", ConfRev: 1,
		Simulation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sub.Run(ctx) }()
	if err := pub.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewInteger(7)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 })
	mu.Lock()
	f := got[0]
	mu.Unlock()
	if f.AppID != 3 || !f.Simulation || f.PDU.StNum != 1 || len(f.PDU.AllData) != 2 || f.Dst != l2.Broadcast {
		t.Fatalf("frame %+v", f)
	}
	if st := send.Stats(); st.Sent == 0 {
		t.Fatalf("sender stats %+v", st)
	}
	if st := recv.Stats(); st.Received == 0 {
		t.Fatalf("receiver stats %+v", st)
	}
	if send.NextSPDUNumber() == 0 {
		t.Fatal("SPDU number not advanced")
	}
}

func TestSVOverUDP(t *testing.T) {
	keys := ring(t)
	recv, send := pair(t, rsession.SISV, rsession.Config{Keys: keys, Version: 1}, rsession.Config{Keys: keys, Version: 1})
	sub, _ := sv.NewSubscriber(recv, sv.SubscriberConfig{})
	var n atomic.Int64
	if _, err := sub.Subscribe(sv.SubscriptionConfig{SvID: "MU01", OnASDU: func(s *sv.Sample) { n.Add(1) }}); err != nil {
		t.Fatal(err)
	}
	pub, err := sv.NewPublisher(send, sv.PublisherConfig{
		Dst: sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}, AppID: 0x4000, SvID: "MU01", ConfRev: 1,
		SmpRate: 1000, SmpMod: sv.SmpPerSec, Layout: sv.LELayout,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sub.Run(ctx) }()
	go func() { _ = pub.Run(ctx) }()
	waitFor(t, func() bool { return n.Load() >= 20 })
}

func TestUnsecuredRequiresOptIn(t *testing.T) {
	if _, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE}); !errors.Is(err, rsession.ErrInvalidConfig) {
		t.Fatalf("no keys: %v", err)
	}
	// An insecure sender is refused by a secured receiver.
	var rejected atomic.Value
	recv, send := pair(t, rsession.SIGOOSE,
		rsession.Config{Keys: ring(t), OnReject: func(_ netip.AddrPort, err error) { rejected.Store(err) }},
		rsession.Config{Insecure: true})
	if err := send.Send(gooseFrame(t)); err != nil {
		t.Fatal(err)
	}
	// The counter is raised before OnReject runs: wait for both.
	waitFor(t, func() bool { return recv.Stats().Unsecured == 1 && rejected.Load() != nil })
	if err, _ := rejected.Load().(error); !errors.Is(err, rsession.ErrUnsecured) {
		t.Fatalf("OnReject got %v", err)
	}
	// An insecure receiver accepts it.
	recv2, send2 := pair(t, rsession.SIGOOSE, rsession.Config{Insecure: true}, rsession.Config{Insecure: true})
	if err := send2.Send(gooseFrame(t)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f, err := recv2.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goose.Decode(f); err != nil {
		t.Fatalf("rebuilt frame: %v", err)
	}
}

// gooseFrame returns a layer-2 GOOSE frame with a VLAN tag.
func gooseFrame(t *testing.T) []byte {
	t.Helper()
	b, err := goose.Encode(&goose.Frame{
		Dst: goose.MAC{1, 0x0c, 0xcd, 1, 0, 1}, VLAN: &goose.VLAN{Priority: 4, ID: 10}, AppID: 9,
		PDU: goose.PDU{GocbRef: "LD/LLN0$GO$g", DatSet: "LD/LLN0$ds", StNum: 1, TimeAllowedToLive: 2000, AllData: []*mms.Value{mms.NewBoolean(false)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestRejectOnTheWire sends forged, tampered, replayed and foreign
// datagrams to a receiver: none reaches Recv, each is counted.
func TestRejectOnTheWire(t *testing.T) {
	keys := ring(t)
	recv := open(t, rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Keys: keys})
	raw, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(recv.LocalAddr()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	k, _ := keys.CurrentKey()
	apdu := gooseFrame(t)[26:]
	spdu := func(n uint32, si rsession.SessionID, typ rsession.PayloadType, key *rsession.Key) []byte {
		b, err := rsession.Encode(&rsession.SPDU{SI: si, Number: n, Payloads: []rsession.Payload{{Type: typ, AppID: 9, APDU: apdu}}}, key)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	good := spdu(10, rsession.SIGOOSE, rsession.PayloadGOOSE, k)
	tampered := append([]byte{}, good...)
	tampered[40] ^= 1
	stranger := rsession.Key{ID: 77, Material: bytes.Repeat([]byte{7}, 16), MAC: rsession.MACHMACSHA256_80}
	for _, b := range [][]byte{
		good,     // accepted
		good,     // replay
		tampered, // authentication
		spdu(11, rsession.SIGOOSE, rsession.PayloadGOOSE, &stranger), // unknown key
		{0x01, 0x40}, // malformed
		spdu(12, rsession.SISV, rsession.PayloadSV, k),    // other SI: ignored
		spdu(13, rsession.SIGOOSE, rsession.PayloadSV, k), // other payload type: ignored
	} {
		if _, err := raw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		s := recv.Stats()
		return s.Received == 1 && s.Replayed == 1 && s.AuthFailed == 1 && s.UnknownKey == 1 && s.Malformed == 1 && s.Ignored == 2
	})
}

func TestQueueFull(t *testing.T) {
	recv, send := pair(t, rsession.SIGOOSE, rsession.Config{Insecure: true, Queue: 1}, rsession.Config{Insecure: true})
	for range 5 {
		if err := send.Send(gooseFrame(t)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { s := recv.Stats(); return s.Received == 1 && s.Dropped >= 1 })
}

func TestSendErrors(t *testing.T) {
	keys := ring(t)
	recvOnly := open(t, rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Keys: keys})
	if err := recvOnly.Send(gooseFrame(t)); !errors.Is(err, rsession.ErrInvalidConfig) {
		t.Fatalf("receive-only: %v", err)
	}
	_, send := pair(t, rsession.SIGOOSE, rsession.Config{Keys: keys}, rsession.Config{Keys: keys})
	frame := gooseFrame(t)
	svFrame, _ := sv.Encode(&sv.Frame{Dst: sv.MAC{1}, PDU: sv.PDU{ASDUs: []sv.ASDU{{SvID: "x", Sample: []byte{0}}}}})
	for name, f := range map[string][]byte{
		"short":        frame[:13],
		"vlan":         frame[:17],
		"ethertype":    svFrame,
		"header":       frame[:20],
		"length":       append(append([]byte{}, frame[:20]...), 0xff, 0xff, 0, 0, 0, 0),
		"length small": func() []byte { b := append([]byte{}, frame...); b[20], b[21] = 0, 4; return b }(),
	} {
		if err := send.Send(f); !errors.Is(err, rsession.ErrInvalidFrame) {
			t.Errorf("%s: %v", name, err)
		}
	}
	keys.Remove(1)
	if err := send.Send(frame); !errors.Is(err, rsession.ErrNoKey) {
		t.Fatalf("no key: %v", err)
	}
	nilKey := open(t, rsession.Config{SI: rsession.SIGOOSE, RemoteAddr: netip.MustParseAddrPort("127.0.0.1:9"), Keys: nilProvider{}})
	if err := nilKey.Send(frame); !errors.Is(err, rsession.ErrNoKey) {
		t.Fatalf("nil key: %v", err)
	}
	badKey := open(t, rsession.Config{SI: rsession.SIGOOSE, RemoteAddr: netip.MustParseAddrPort("127.0.0.1:9"), Keys: badProvider{}})
	if err := badKey.Send(frame); !errors.Is(err, rsession.ErrInvalidKey) {
		t.Fatalf("bad key: %v", err)
	}
	if s := badKey.Stats(); s.SendErrors != 1 {
		t.Fatalf("stats %+v", s)
	}
	_ = send.Close()
	if err := send.Send(frame); !errors.Is(err, rsession.ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
	if _, err := send.Recv(context.Background()); !errors.Is(err, rsession.ErrClosed) {
		t.Fatalf("Recv after Close: %v", err)
	}
	if err := send.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

type nilProvider struct{}

func (nilProvider) CurrentKey() (*rsession.Key, error)     { return nil, nil }
func (nilProvider) LookupKey(uint32) (*rsession.Key, bool) { return nil, false }

type badProvider struct{}

func (badProvider) CurrentKey() (*rsession.Key, error) {
	return &rsession.Key{ID: 1, Material: []byte{1}, MAC: rsession.MACHMACSHA256_80}, nil
}
func (badProvider) LookupKey(uint32) (*rsession.Key, bool) { return nil, false }

func TestRecvContextAndClose(t *testing.T) {
	tr := open(t, rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Insecure: true})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := tr.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Recv: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := tr.Recv(context.Background()); done <- err }()
	if !leakcheck.WaitBlocked("rsession.(*Transport).Recv", 5*time.Second) {
		t.Fatal("Recv did not block")
	}
	_ = tr.Close()
	if err := <-done; !errors.Is(err, rsession.ErrClosed) {
		t.Fatalf("blocked Recv: %v", err)
	}
	if tr.HardwareAddr() != (l2.MAC{}) {
		t.Fatal("HardwareAddr")
	}
}

func TestOpenConfigErrors(t *testing.T) {
	g := netip.MustParseAddr("239.1.2.3")
	for name, cfg := range map[string]rsession.Config{
		"SI":         {SI: rsession.SITunnelled, Insecure: true},
		"version":    {SI: rsession.SIGOOSE, Insecure: true, Version: 3},
		"TTL":        {SI: rsession.SIGOOSE, Insecure: true, MulticastTTL: 256},
		"queue":      {SI: rsession.SIGOOSE, Insecure: true, Queue: -1},
		"group":      {SI: rsession.SIGOOSE, Insecure: true, Groups: []netip.Addr{netip.MustParseAddr("10.0.0.1")}},
		"family":     {SI: rsession.SIGOOSE, Insecure: true, Groups: []netip.Addr{g, netip.MustParseAddr("ff02::1")}},
		"remote fam": {SI: rsession.SIGOOSE, Insecure: true, LocalAddr: loopback, RemoteAddr: netip.MustParseAddrPort("[::1]:102")},
		"interface":  {SI: rsession.SIGOOSE, Insecure: true, Interface: "no-such-interface0"},
	} {
		if _, err := rsession.Open(cfg); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// A port in use.
	a := open(t, rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Insecure: true})
	if _, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE, LocalAddr: a.LocalAddr(), Insecure: true}); err == nil {
		t.Error("port in use: no error")
	}
}

func TestDefaults(t *testing.T) {
	// The default port fills in a remote without one; IPv6 binds udp6.
	tr := open(t, rsession.Config{SI: rsession.SIGOOSE, Insecure: true, RemoteAddr: netip.MustParseAddrPort("127.0.0.1:0")})
	if !tr.LocalAddr().Addr().Is4() {
		t.Fatalf("local %v", tr.LocalAddr())
	}
	tr6, err := rsession.Open(rsession.Config{SI: rsession.SISV, Insecure: true, RemoteAddr: netip.MustParseAddrPort("[::1]:0"), InitialSPDUNumber: 41})
	if err != nil {
		t.Skipf("no IPv6: %v", err)
	}
	defer func() { _ = tr6.Close() }()
	if tr6.LocalAddr().Addr().Is4() || tr6.NextSPDUNumber() != 41 {
		t.Fatalf("local %v next %d", tr6.LocalAddr(), tr6.NextSPDUNumber())
	}
	tr6b, err := rsession.Open(rsession.Config{SI: rsession.SISV, Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = tr6b.Close()
}

func TestSourceMAC(t *testing.T) {
	if m := rsession.SourceMAC(netip.MustParseAddr("192.0.2.7")); m != (l2.MAC{2, 0, 192, 0, 2, 7}) {
		t.Fatalf("IPv4: %v", m)
	}
	if rsession.SourceMAC(netip.MustParseAddr("::ffff:192.0.2.7")) != rsession.SourceMAC(netip.MustParseAddr("192.0.2.7")) {
		t.Fatal("mapped address differs")
	}
	a, b := rsession.SourceMAC(netip.MustParseAddr("2001:db8::1")), rsession.SourceMAC(netip.MustParseAddr("2001:db8::2"))
	if a == b || a[0] != 2 {
		t.Fatalf("IPv6: %v %v", a, b)
	}
}

// TestMulticast sends to a group joined by a receiver on the same host
// (multicast loopback is on by default). It is skipped where the host
// has no multicast route.
func TestMulticast(t *testing.T) {
	group := netip.MustParseAddr("239.255.61.85")
	keys := ring(t)
	recv, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE, Groups: []netip.Addr{group}, LocalAddr: netip.AddrPortFrom(netip.IPv4Unspecified(), 0), Keys: keys})
	if err != nil {
		t.Skipf("multicast join: %v", err)
	}
	defer func() { _ = recv.Close() }()
	send, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE, RemoteAddr: netip.AddrPortFrom(group, recv.LocalAddr().Port()), MulticastTTL: 4, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = send.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			_ = send.Send(gooseFrame(t))
			time.Sleep(20 * time.Millisecond)
		}
	}()
	if _, err := recv.Recv(ctx); err != nil {
		t.Skipf("no multicast delivery on this host: %v", err)
	}
}

// TestMulticastInterface joins and sends on a named interface.
func TestMulticastInterface(t *testing.T) {
	ifi := multicastInterface(t)
	group := netip.MustParseAddr("239.255.61.86")
	tr, err := rsession.Open(rsession.Config{
		SI: rsession.SIGOOSE, Insecure: true, Interface: ifi.Name,
		Groups:     []netip.Addr{group},
		LocalAddr:  netip.AddrPortFrom(netip.IPv4Unspecified(), 0),
		RemoteAddr: netip.AddrPortFrom(group, 10102), MulticastTTL: 2,
	})
	if err != nil {
		t.Skipf("multicast on %s: %v", ifi.Name, err)
	}
	_ = tr.Close()
	g6 := netip.MustParseAddr("ff15::6185")
	tr6, err := rsession.Open(rsession.Config{
		SI: rsession.SISV, Insecure: true, Interface: ifi.Name,
		Groups: []netip.Addr{g6}, LocalAddr: netip.AddrPortFrom(netip.IPv6Unspecified(), 0),
		RemoteAddr: netip.AddrPortFrom(g6, 10102), MulticastTTL: 2,
	})
	if err != nil {
		t.Skipf("IPv6 multicast on %s: %v", ifi.Name, err)
	}
	_ = tr6.Close()
}

func multicastInterface(t *testing.T) *net.Interface {
	t.Helper()
	ifs, _ := net.Interfaces()
	for i := range ifs {
		f := ifs[i].Flags
		if f&net.FlagUp == 0 || f&net.FlagMulticast == 0 || f&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifs[i].Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				return &ifs[i]
			}
		}
	}
	t.Skip("no multicast interface with an IPv4 address")
	return nil
}
