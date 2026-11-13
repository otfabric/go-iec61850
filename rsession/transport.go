// SPDX-License-Identifier: MIT

package rsession

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-iec61850/l2"
)

// DefaultPort is the UDP port of R-GOOSE and R-SV.
const DefaultPort = 102

// DefaultQueue is the receive queue length of a [Transport].
const DefaultQueue = 1024

// Config configures a [Transport].
type Config struct {
	// SI selects what the transport carries: [SIGOOSE] (layer-2 frames
	// of EtherType 0x88B8) or [SISV] (0x88BA). Required.
	SI SessionID

	// LocalAddr is the address to bind. The zero value binds the
	// wildcard address, on [DefaultPort] when Groups is set and on an
	// ephemeral port otherwise (enough for a sender). Binding port 102
	// needs privileges on most systems.
	LocalAddr netip.AddrPort

	// RemoteAddr is where [Transport.Send] sends: a unicast address or a
	// multicast group. The zero value makes the transport receive-only.
	// A zero port means [DefaultPort].
	RemoteAddr netip.AddrPort

	// Groups are multicast groups to join for receiving.
	Groups []netip.Addr

	// Interface names the network interface for multicast: the groups
	// are joined on it and multicast is sent from it. Empty lets the
	// system choose.
	Interface string

	// MulticastTTL is the IPv4 TTL or IPv6 hop limit of multicast sent;
	// 0 keeps the system default (1, which does not cross a router).
	MulticastTTL int

	// Keys secures what is sent and verifies what is received. Nil
	// requires Insecure.
	Keys KeyProvider

	// Insecure is the explicit opt-in to unsecured SPDUs: with Keys nil
	// they are sent unsecured, and in any case unsecured SPDUs are
	// accepted on receipt. Secured SPDUs are still verified.
	Insecure bool

	// Version is the session protocol version sent, 1 or 2; 0 means 2.
	// Both are received.
	Version uint16

	// InitialSPDUNumber is the number of the first SPDU sent. A
	// publisher that restarts can continue from a persisted
	// [Transport.NextSPDUNumber] so that receivers do not take its
	// SPDUs for replays.
	InitialSPDUNumber uint32

	// ReplayResetAfter, when positive, lets a sender restart its SPDU
	// numbering: an SPDU older than the replay window is accepted if
	// nothing was accepted from that sender and key for this long.
	// Zero refuses it until the key changes.
	ReplayResetAfter time.Duration

	// MaxSenders bounds the replay state; zero means
	// [DefaultMaxSenders]. The sender heard from least recently is
	// forgotten first.
	MaxSenders int

	// Queue is the receive queue length; zero means [DefaultQueue].
	// Frames that do not fit are dropped and counted.
	Queue int

	// FrameDst is the destination address of the frames [Transport.Recv]
	// returns; zero means [l2.Broadcast]. The source address is
	// [SourceMAC] of the sender.
	FrameDst l2.MAC

	// HardwareAddr is returned by [Transport.HardwareAddr], the default
	// source address of a publisher. It is not sent.
	HardwareAddr l2.MAC

	// OnReject, when non-nil, is called with every refused datagram's
	// sender and the reason, for a security event log. It runs on the
	// receive goroutine and must not block.
	OnReject func(from netip.AddrPort, err error)

	// Logger receives transport diagnostics. Nil disables logging.
	Logger *slog.Logger

	// Now returns the current time; nil uses [time.Now]. For tests.
	Now func() time.Time
}

// Stats counts transport activity.
type Stats struct {
	Sent        uint64 // SPDUs sent
	SendErrors  uint64 // SPDUs that could not be sent
	Received    uint64 // frames delivered to Recv
	Dropped     uint64 // frames dropped because the queue was full
	Ignored     uint64 // valid SPDUs of another session identifier or payload type
	Malformed   uint64 // datagrams refused by the decoder or as unsupported
	Unsecured   uint64 // unsecured SPDUs refused
	UnknownKey  uint64 // SPDUs under a key the provider does not hold
	AuthFailed  uint64 // SPDUs whose signature did not verify
	Replayed    uint64 // SPDUs refused by the replay window
	OtherErrors uint64 // receive errors of the socket
}

var _ l2.SourceReceiver = (*Transport)(nil)

// Transport carries GOOSE or SV over UDP as R-GOOSE or R-SV. It
// implements [l2.Transport] (goose.Transport, sv.Transport), so the
// goose and sv publishers and subscribers run over it unchanged:
// [Transport.Send] takes the layer-2 frame a publisher builds and sends
// its APDU in an SPDU; [Transport.Recv] returns a layer-2 frame for each
// APDU of every SPDU that verifies, and [Transport.RecvFrom] that frame
// with the sender's address ([l2.SourceReceiver]), which the goose and
// sv subscribers report as Frame.From.
//
// A Transport is safe for concurrent use.
type Transport struct {
	cfg       Config
	conn      *net.UDPConn
	remote    netip.AddrPort
	etherType uint16
	payload   PayloadType
	logger    *slog.Logger
	guard     *replayGuard

	number atomic.Uint32
	queue  chan received

	sent, sendErrors, received, dropped, ignored           atomic.Uint64
	malformed, unsecured, unknownKey, authFailed, replayed atomic.Uint64
	otherErrors                                            atomic.Uint64

	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Open validates cfg, binds the socket, joins the multicast groups and
// starts receiving.
func Open(cfg Config) (*Transport, error) {
	t := &Transport{cfg: cfg, closed: make(chan struct{})}
	switch cfg.SI {
	case SIGOOSE:
		t.etherType, t.payload = l2.EtherTypeGOOSE, PayloadGOOSE
	case SISV:
		t.etherType, t.payload = l2.EtherTypeSV, PayloadSV
	default:
		return nil, configErr("SI", "%v is not GOOSE or SV", cfg.SI)
	}
	if cfg.Keys == nil && !cfg.Insecure {
		return nil, configErr("Keys", "no key provider; set Insecure to send and accept unsecured SPDUs")
	}
	if cfg.Version > 2 {
		return nil, configErr("Version", "protocol version %d", cfg.Version)
	}
	if cfg.MulticastTTL < 0 || cfg.MulticastTTL > 255 {
		return nil, configErr("MulticastTTL", "%d out of range 0..255", cfg.MulticastTTL)
	}
	if cfg.Queue < 0 || cfg.MaxSenders < 0 || cfg.ReplayResetAfter < 0 {
		return nil, configErr("Queue/MaxSenders/ReplayResetAfter", "negative")
	}
	if cfg.Queue == 0 {
		cfg.Queue = DefaultQueue
	}
	if cfg.FrameDst.IsZero() {
		cfg.FrameDst = l2.Broadcast
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	t.cfg = cfg
	t.logger = cfg.Logger
	if t.logger == nil {
		t.logger = slog.New(slog.DiscardHandler)
	}
	t.remote = cfg.RemoteAddr
	if t.remote.IsValid() && t.remote.Port() == 0 {
		t.remote = netip.AddrPortFrom(t.remote.Addr(), DefaultPort)
	}
	for _, g := range cfg.Groups {
		if !g.IsMulticast() {
			return nil, configErr("Groups", "%v is not a multicast address", g)
		}
	}
	local, network, err := t.localAddr()
	if err != nil {
		return nil, err
	}
	var ifi *net.Interface
	if cfg.Interface != "" {
		if ifi, err = net.InterfaceByName(cfg.Interface); err != nil {
			return nil, fmt.Errorf("%w: rsession: interface %q: %w", l2.ErrInterfaceNotFound, cfg.Interface, err)
		}
	}
	conn, err := net.ListenUDP(network, net.UDPAddrFromAddrPort(local))
	if err != nil {
		return nil, fmt.Errorf("rsession: listen %v: %w", local, err)
	}
	t.conn = conn
	if err := t.setupMulticast(ifi); err != nil {
		_ = conn.Close()
		return nil, err
	}
	t.guard = newReplayGuard(cfg.MaxSenders, cfg.ReplayResetAfter)
	t.number.Store(cfg.InitialSPDUNumber)
	t.queue = make(chan received, cfg.Queue)
	t.wg.Add(1)
	go t.readLoop()
	return t, nil
}

// localAddr returns the address to bind and the network ("udp4" or
// "udp6") that the configured addresses imply.
func (t *Transport) localAddr() (netip.AddrPort, string, error) {
	c := &t.cfg
	var fam netip.Addr
	switch {
	case c.LocalAddr.Addr().IsValid():
		fam = c.LocalAddr.Addr()
	case len(c.Groups) > 0:
		fam = c.Groups[0]
	case t.remote.IsValid():
		fam = t.remote.Addr()
	default:
		fam = netip.IPv4Unspecified()
	}
	v4 := fam.Unmap().Is4()
	check := func(a netip.Addr, field string) error {
		if a.IsValid() && a.Unmap().Is4() != v4 {
			return configErr(field, "%v is not in the address family of %v", a, fam)
		}
		return nil
	}
	for _, g := range c.Groups {
		if err := check(g, "Groups"); err != nil {
			return netip.AddrPort{}, "", err
		}
	}
	if err := check(t.remote.Addr(), "RemoteAddr"); err != nil {
		return netip.AddrPort{}, "", err
	}
	network := "udp6"
	if v4 {
		network = "udp4"
	}
	if c.LocalAddr.Addr().IsValid() {
		return netip.AddrPortFrom(c.LocalAddr.Addr().Unmap(), c.LocalAddr.Port()), network, nil
	}
	port := uint16(0)
	if len(c.Groups) > 0 {
		port = DefaultPort
	}
	if c.LocalAddr.Port() != 0 {
		port = c.LocalAddr.Port()
	}
	if v4 {
		return netip.AddrPortFrom(netip.IPv4Unspecified(), port), network, nil
	}
	return netip.AddrPortFrom(netip.IPv6Unspecified(), port), network, nil
}

func (t *Transport) setupMulticast(ifi *net.Interface) error {
	for _, g := range t.cfg.Groups {
		if err := joinGroup(t.conn, g.Unmap(), ifi); err != nil {
			return fmt.Errorf("rsession: join %v: %w", g, err)
		}
	}
	if !t.remote.Addr().IsMulticast() {
		return nil
	}
	v6 := !t.remote.Addr().Unmap().Is4()
	if ifi != nil {
		if err := setMulticastInterface(t.conn, ifi, v6); err != nil {
			return fmt.Errorf("rsession: multicast interface %s: %w", ifi.Name, err)
		}
	}
	if t.cfg.MulticastTTL > 0 {
		if err := setMulticastTTL(t.conn, t.cfg.MulticastTTL, v6); err != nil {
			return fmt.Errorf("rsession: multicast TTL: %w", err)
		}
	}
	return nil
}

// LocalAddr returns the address the transport is bound to.
func (t *Transport) LocalAddr() netip.AddrPort {
	return t.conn.LocalAddr().(*net.UDPAddr).AddrPort()
}

// NextSPDUNumber returns the number the next SPDU sent will carry.
func (t *Transport) NextSPDUNumber() uint32 { return t.number.Load() }

// HardwareAddr implements [l2.Transport]: it returns
// Config.HardwareAddr.
func (t *Transport) HardwareAddr() l2.MAC { return t.cfg.HardwareAddr }

// Stats returns the transport counters.
func (t *Transport) Stats() Stats {
	return Stats{
		Sent: t.sent.Load(), SendErrors: t.sendErrors.Load(), Received: t.received.Load(),
		Dropped: t.dropped.Load(), Ignored: t.ignored.Load(), Malformed: t.malformed.Load(),
		Unsecured: t.unsecured.Load(), UnknownKey: t.unknownKey.Load(), AuthFailed: t.authFailed.Load(),
		Replayed: t.replayed.Load(), OtherErrors: t.otherErrors.Load(),
	}
}

// Send implements [l2.Transport]. frame is a layer-2 GOOSE or SV frame
// (optionally 802.1Q tagged) of the transport's kind; its APPID,
// simulation bit and APDU are sent in one SPDU to Config.RemoteAddr,
// secured with the provider's current key. The Ethernet addresses, VLAN
// tag and Reserved 2 are not carried.
func (t *Transport) Send(frame []byte) error {
	select {
	case <-t.closed:
		return ErrClosed
	default:
	}
	if !t.remote.IsValid() {
		return configErr("RemoteAddr", "not set: the transport is receive-only")
	}
	p, err := t.parseFrame(frame)
	if err != nil {
		return err
	}
	var key *Key
	if t.cfg.Keys != nil {
		if key, err = t.cfg.Keys.CurrentKey(); err != nil {
			t.sendErrors.Add(1)
			return fmt.Errorf("rsession: send: %w", err)
		}
		if key == nil {
			t.sendErrors.Add(1)
			return fmt.Errorf("rsession: send: %w", ErrNoKey)
		}
	}
	s := SPDU{SI: t.cfg.SI, Number: t.number.Add(1) - 1, Version: t.cfg.Version, Payloads: []Payload{p}}
	b, err := Encode(&s, key)
	if err != nil {
		t.sendErrors.Add(1)
		return err
	}
	if _, err := t.conn.WriteToUDPAddrPort(b, t.remote); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return ErrClosed // Close ran during the send
		}
		t.sendErrors.Add(1)
		return fmt.Errorf("rsession: send to %v: %w", t.remote, err)
	}
	t.sent.Add(1)
	return nil
}

// parseFrame extracts the APDU of a layer-2 frame (IEC 61850-8-1
// Annex C, 9-2 5.3.3).
func (t *Transport) parseFrame(b []byte) (Payload, error) {
	if len(b) < 14 {
		return Payload{}, fmt.Errorf("%w: frame of %d octets", ErrInvalidFrame, len(b))
	}
	pos := 12
	et := binary.BigEndian.Uint16(b[pos:])
	if et == l2.EtherTypeVLAN {
		pos += 4
		if len(b) < pos+2 {
			return Payload{}, fmt.Errorf("%w: truncated 802.1Q tag", ErrInvalidFrame)
		}
		et = binary.BigEndian.Uint16(b[pos:])
	}
	pos += 2
	if et != t.etherType {
		return Payload{}, fmt.Errorf("%w: EtherType 0x%04X on a %v transport", ErrInvalidFrame, et, t.cfg.SI)
	}
	if len(b) < pos+8 {
		return Payload{}, fmt.Errorf("%w: truncated header", ErrInvalidFrame)
	}
	appID := binary.BigEndian.Uint16(b[pos:])
	length := int(binary.BigEndian.Uint16(b[pos+2:]))
	r1 := binary.BigEndian.Uint16(b[pos+4:])
	if length < 8 || pos+length > len(b) {
		return Payload{}, fmt.Errorf("%w: length %d", ErrInvalidFrame, length)
	}
	return Payload{
		Type:       t.payload,
		Simulation: r1&0x8000 != 0,
		AppID:      appID,
		APDU:       b[pos+8 : pos+length],
	}, nil
}

// received is one queued frame and the address it came from.
type received struct {
	frame []byte
	from  netip.AddrPort
}

// Recv implements [l2.Transport]: it returns the next layer-2 frame
// rebuilt from a verified SPDU. [Transport.RecvFrom] also returns the
// sender's address.
func (t *Transport) Recv(ctx context.Context) ([]byte, error) {
	f, _, err := t.RecvFrom(ctx)
	return f, err
}

// RecvFrom implements [l2.SourceReceiver]: it is [Transport.Recv] that
// also returns the UDP address and port of the sender of the SPDU the
// frame came from (an IPv4 address unmapped from IPv6). The frame's
// source address is [SourceMAC] of that address, which carries neither
// the port nor, for IPv6, the address itself. On error the address is
// zero.
func (t *Transport) RecvFrom(ctx context.Context) ([]byte, netip.AddrPort, error) {
	select {
	case <-t.closed:
		return nil, netip.AddrPort{}, ErrClosed
	default:
	}
	select {
	case r := <-t.queue:
		return r.frame, r.from, nil
	case <-ctx.Done():
		return nil, netip.AddrPort{}, ctx.Err()
	case <-t.closed:
		return nil, netip.AddrPort{}, ErrClosed
	}
}

// Close implements [l2.Transport]. Blocked Recv calls return
// [ErrClosed]; Close is idempotent.
func (t *Transport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		close(t.closed)
		err = t.conn.Close()
		t.wg.Wait()
	})
	return err
}

func (t *Transport) readLoop() {
	defer t.wg.Done()
	buf := make([]byte, 65536)
	for {
		n, from, err := t.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			select {
			case <-t.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			t.otherErrors.Add(1)
			t.logger.Warn("rsession: receive failed", "error", err)
			continue
		}
		t.handle(buf[:n], from)
	}
}

// handle verifies one datagram and queues its frames.
func (t *Transport) handle(b []byte, from netip.AddrPort) {
	s, key, err := Verify(b, VerifyOptions{Keys: t.cfg.Keys, AllowUnsecured: t.cfg.Insecure})
	if err == nil && key != nil {
		k := streamKey{addr: from.Addr().Unmap(), si: s.SI, keyID: key.ID}
		if !t.guard.accept(k, s.Number, t.cfg.Now()) {
			err = fmt.Errorf("%w: SPDU number %d from %v", ErrReplay, s.Number, from)
		}
	}
	if err != nil {
		t.reject(from, err)
		return
	}
	if s.SI != t.cfg.SI {
		t.ignored.Add(1)
		return
	}
	from = netip.AddrPortFrom(from.Addr().Unmap(), from.Port())
	src := SourceMAC(from.Addr())
	for _, p := range s.Payloads {
		if p.Type != t.payload {
			t.ignored.Add(1)
			continue
		}
		f, ok := t.buildFrame(p, src)
		if !ok {
			t.malformed.Add(1)
			continue
		}
		select {
		case t.queue <- received{frame: f, from: from}:
			t.received.Add(1)
		default:
			t.dropped.Add(1)
		}
	}
}

func (t *Transport) reject(from netip.AddrPort, err error) {
	switch {
	case errors.Is(err, ErrUnsecured):
		t.unsecured.Add(1)
	case errors.Is(err, ErrUnknownKey):
		t.unknownKey.Add(1)
	case errors.Is(err, ErrAuthentication):
		t.authFailed.Add(1)
	case errors.Is(err, ErrReplay):
		t.replayed.Add(1)
	default:
		t.malformed.Add(1)
	}
	t.logger.Debug("rsession: SPDU refused", "from", from, "error", err)
	if t.cfg.OnReject != nil {
		t.cfg.OnReject(from, err)
	}
}

// buildFrame returns the layer-2 frame of one APDU: FrameDst, src, the
// transport's EtherType, APPID, Length, Reserved 1 with the simulation
// bit, Reserved 2 zero, the APDU, padded to 60 octets.
func (t *Transport) buildFrame(p Payload, src l2.MAC) ([]byte, bool) {
	length := 8 + len(p.APDU)
	if length > 0xffff {
		return nil, false
	}
	f := make([]byte, 0, max(60, 14+length))
	f = append(f, t.cfg.FrameDst[:]...)
	f = append(f, src[:]...)
	f = binary.BigEndian.AppendUint16(f, t.etherType)
	f = binary.BigEndian.AppendUint16(f, p.AppID)
	f = binary.BigEndian.AppendUint16(f, uint16(length))
	r1 := uint16(0)
	if p.Simulation {
		r1 = 0x8000
	}
	f = binary.BigEndian.AppendUint16(f, r1)
	f = append(f, 0, 0)
	f = append(f, p.APDU...)
	for len(f) < 60 {
		f = append(f, 0)
	}
	return f, true
}

// SourceMAC returns the source address of the frames [Transport.Recv]
// rebuilds from SPDUs sent by addr, so that subscribers keep the streams
// of different senders apart and can filter on them. It is a locally
// administered unicast address: 02-00 followed by the four octets of an
// IPv4 address, or 02 followed by the first five octets of the SHA-256
// of an IPv6 address.
func SourceMAC(addr netip.Addr) l2.MAC {
	addr = addr.Unmap()
	var m l2.MAC
	m[0] = 0x02
	if addr.Is4() {
		a := addr.As4()
		copy(m[2:], a[:])
		return m
	}
	a := addr.As16()
	h := sha256.Sum256(a[:])
	copy(m[1:], h[:5])
	return m
}
