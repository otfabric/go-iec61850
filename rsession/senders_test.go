// SPDX-License-Identifier: MIT

package rsession

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

var (
	senderA = netip.MustParseAddrPort("192.0.2.1:102")
	senderB = netip.MustParseAddrPort("192.0.2.2:102")
	senderC = netip.MustParseAddrPort("198.51.100.7:102")
)

// openHandleOnly opens a transport on loopback that receives nothing from
// the network in these tests: datagrams are passed to handle directly,
// with the sender address of the test.
func openHandleOnly(t *testing.T, cfg Config) *Transport {
	t.Helper()
	ring, err := NewKeyRing(testKey(1, MACHMACSHA256_128))
	if err != nil {
		t.Fatal(err)
	}
	cfg.SI = SIGOOSE
	cfg.LocalAddr = netip.MustParseAddrPort("127.0.0.1:0")
	cfg.Keys = ring
	tr, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

func signedSPDU(t *testing.T, number uint32) []byte {
	t.Helper()
	k := testKey(1, MACHMACSHA256_128)
	b, err := Encode(&SPDU{SI: SIGOOSE, Number: number, Payloads: []Payload{{Type: PayloadGOOSE, AppID: 1, APDU: tinyGOOSE}}}, &k)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// One verified SPDU, received from A, from A again and from B: which
// copies are delivered and how the others are counted, per option.
func TestHandle_SameSPDUFromSeveralSenders(t *testing.T) {
	cases := []struct {
		name       string
		allowed    []netip.Prefix
		single     bool
		from       []netip.AddrPort
		received   uint64
		replayed   uint64
		notAllowed uint64
		rejects    []error
	}{
		{
			name: "no option: one window per sender address", from: []netip.AddrPort{senderA, senderA, senderB},
			received: 2, replayed: 1, rejects: []error{ErrReplay},
		},
		{
			name: "AllowedSenders: B is not listed", allowed: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")},
			from:     []netip.AddrPort{senderA, senderA, senderB},
			received: 1, replayed: 1, notAllowed: 1, rejects: []error{ErrReplay, ErrSenderNotAllowed},
		},
		{
			name: "AllowedSenders: B is listed", allowed: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
			from:     []netip.AddrPort{senderA, senderA, senderB, senderC},
			received: 2, replayed: 1, notAllowed: 1, rejects: []error{ErrReplay, ErrSenderNotAllowed},
		},
		{
			name: "SingleSenderPerKey", single: true, from: []netip.AddrPort{senderA, senderA, senderB},
			received: 1, replayed: 2, rejects: []error{ErrReplay, ErrReplay},
		},
		{
			name: "SingleSenderPerKey and AllowedSenders", single: true,
			allowed:  []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
			from:     []netip.AddrPort{senderA, senderA, senderB, senderC},
			received: 1, replayed: 2, notAllowed: 1, rejects: []error{ErrReplay, ErrReplay, ErrSenderNotAllowed},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var rejects []error
			tr := openHandleOnly(t, Config{
				AllowedSenders: c.allowed, SingleSenderPerKey: c.single,
				OnReject: func(_ netip.AddrPort, err error) {
					mu.Lock()
					rejects = append(rejects, err)
					mu.Unlock()
				},
			})
			b := signedSPDU(t, 42)
			for _, from := range c.from {
				tr.handle(b, from)
			}
			st := tr.Stats()
			if st.Received != c.received || st.Replayed != c.replayed || st.NotAllowed != c.notAllowed {
				t.Errorf("Received %d, Replayed %d, NotAllowed %d; want %d, %d, %d",
					st.Received, st.Replayed, st.NotAllowed, c.received, c.replayed, c.notAllowed)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(rejects) != len(c.rejects) {
				t.Fatalf("OnReject called with %v, want %v", rejects, c.rejects)
			}
			for i, want := range c.rejects {
				if !errors.Is(rejects[i], want) {
					t.Errorf("OnReject %d: %v, want %v", i, rejects[i], want)
				}
			}
			if got := uint64(len(tr.queue)); got != c.received {
				t.Errorf("%d frames queued, want %d", got, c.received)
			}
		})
	}
}

// A sender outside AllowedSenders is refused before verification: an
// SPDU that would not verify is counted as not allowed, not as an
// authentication failure.
func TestHandle_NotAllowedBeforeVerification(t *testing.T) {
	tr := openHandleOnly(t, Config{AllowedSenders: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")}})
	b := signedSPDU(t, 1)
	b[len(b)-1] ^= 0xff // signature does not verify
	tr.handle(b, senderB)
	tr.handle([]byte{0x01}, senderB) // not an SPDU
	st := tr.Stats()
	if st.NotAllowed != 2 || st.AuthFailed != 0 || st.Malformed != 0 {
		t.Errorf("NotAllowed %d, AuthFailed %d, Malformed %d; want 2, 0, 0", st.NotAllowed, st.AuthFailed, st.Malformed)
	}
	tr.handle(b, senderA)
	if st := tr.Stats(); st.AuthFailed != 1 {
		t.Errorf("AuthFailed = %d for a listed sender, want 1", st.AuthFailed)
	}
}

// IPv4 senders, also given as IPv4-mapped IPv6 addresses, match IPv4
// prefixes and IPv4-mapped IPv6 prefixes alike.
func TestAllowedSenders_IPv4Mapped(t *testing.T) {
	mapped := netip.AddrPortFrom(netip.AddrFrom16(senderA.Addr().As16()), 102)
	for _, prefix := range []string{"192.0.2.0/24", "::ffff:192.0.2.0/120", "::ffff:192.0.2.1/128"} {
		tr := openHandleOnly(t, Config{AllowedSenders: []netip.Prefix{netip.MustParsePrefix(prefix)}})
		tr.handle(signedSPDU(t, 1), mapped)
		tr.handle(signedSPDU(t, 2), senderA)
		tr.handle(signedSPDU(t, 3), senderC)
		if st := tr.Stats(); st.Received != 2 || st.NotAllowed != 1 {
			t.Errorf("%s: Received %d, NotAllowed %d; want 2, 1", prefix, st.Received, st.NotAllowed)
		}
	}
	v6 := netip.MustParseAddrPort("[2001:db8::1]:102")
	tr := openHandleOnly(t, Config{AllowedSenders: []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")}})
	tr.handle(signedSPDU(t, 1), v6)
	tr.handle(signedSPDU(t, 2), senderA)
	if st := tr.Stats(); st.Received != 1 || st.NotAllowed != 1 {
		t.Errorf("IPv6 prefix: Received %d, NotAllowed %d; want 1, 1", st.Received, st.NotAllowed)
	}
}

func TestAllowedSenders_Config(t *testing.T) {
	for _, p := range []netip.Prefix{{}, netip.MustParsePrefix("::ffff:0:0/95")} {
		_, err := Open(Config{SI: SIGOOSE, Insecure: true, LocalAddr: netip.MustParseAddrPort("127.0.0.1:0"), AllowedSenders: []netip.Prefix{p}})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("AllowedSenders %v: Open = %v, want ErrInvalidConfig", p, err)
		}
	}
}

// A callback on the receive goroutine that does not return normally is
// recovered: the datagram is dropped and counted, and the transport
// keeps receiving.
func TestOnReject_CallbackRecovered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ring, err := NewKeyRing(testKey(1, MACHMACSHA256_128))
	if err != nil {
		t.Fatal(err)
	}
	rx, err := Open(Config{
		SI: SIGOOSE, LocalAddr: netip.MustParseAddrPort("127.0.0.1:0"), Keys: ring,
		OnReject: func(netip.AddrPort, error) { panic("OnReject does not return normally") },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rx.Close() }()
	tx, err := Open(Config{SI: SIGOOSE, RemoteAddr: rx.LocalAddr(), Keys: ring})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Close() }()

	bad := signedSPDU(t, 5)
	bad[len(bad)-1] ^= 0xff
	if _, err := tx.conn.WriteToUDPAddrPort(bad, rx.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.conn.WriteToUDPAddrPort(signedSPDU(t, 6), rx.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := rx.Recv(ctx); err != nil {
		t.Fatalf("Recv after a recovered OnReject: %v", err)
	}
	st := rx.Stats()
	if st.CallbacksRecovered != 1 || st.AuthFailed != 1 || st.Received != 1 {
		t.Errorf("CallbacksRecovered %d, AuthFailed %d, Received %d; want 1, 1, 1", st.CallbacksRecovered, st.AuthFailed, st.Received)
	}
}

type failingProvider struct{ *KeyRing }

func (failingProvider) LookupKey(uint32) (*Key, bool) { panic("LookupKey does not return normally") }

// A KeyProvider that does not return normally on the receive goroutine
// is recovered in the same way.
func TestKeyProvider_CallbackRecovered(t *testing.T) {
	ring, err := NewKeyRing(testKey(1, MACHMACSHA256_128))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Open(Config{SI: SIGOOSE, LocalAddr: netip.MustParseAddrPort("127.0.0.1:0"), Keys: failingProvider{ring}, Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	tr.handleRecovered(signedSPDU(t, 1), senderA)
	unsecured, err := Encode(&SPDU{SI: SIGOOSE, Number: 2, Payloads: []Payload{{Type: PayloadGOOSE, AppID: 1, APDU: tinyGOOSE}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr.handleRecovered(unsecured, senderA)
	if st := tr.Stats(); st.CallbacksRecovered != 1 || st.Received != 1 {
		t.Errorf("CallbacksRecovered %d, Received %d; want 1, 1", st.CallbacksRecovered, st.Received)
	}
}
