// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// startDialerTestServer serves the chaos test model on a loopback ISO
// listener and returns its address.
func startDialerTestServer(t *testing.T) string {
	t.Helper()
	srv := newChaosServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.ListenAndServe(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		<-done
	})
	return ln.Addr().String()
}

var errDestinationDenied = errors.New("destination not allowed")

// allowlistDialer resolves addr and dials only IPs inside allowed: the
// check is made against the resolved address, not the host name.
func allowlistDialer(allowed netip.Prefix, dialed *atomic.Int32) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if allowed.Contains(ip.Unmap()) {
				dialed.Add(1)
				var d net.Dialer
				return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
		}
		return nil, fmt.Errorf("%w: %s", errDestinationDenied, host)
	}
}

// TestDialUsesDialer: DialOptions.Dialer opens the connection, receives
// network "tcp" and the address given to Dial, and the resulting client
// works end to end.
func TestDialUsesDialer(t *testing.T) {
	addr := startDialerTestServer(t)
	var calls atomic.Int32
	var gotNetwork, gotAddr string
	dialer := func(ctx context.Context, network, a string) (net.Conn, error) {
		calls.Add(1)
		gotNetwork, gotAddr = network, a
		var d net.Dialer
		return d.DialContext(ctx, network, a)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, addr, DialOptions{Dialer: dialer})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()
	if calls.Load() != 1 || gotNetwork != "tcp" || gotAddr != addr {
		t.Fatalf("dialer calls=%d network=%q addr=%q, want 1/tcp/%s", calls.Load(), gotNetwork, gotAddr, addr)
	}
	if lds, err := c.ListLogicalDevices(ctx); err != nil || len(lds) == 0 {
		t.Fatalf("ListLogicalDevices over custom dialer: %v (%d LDs)", err, len(lds))
	}
}

// TestDialDialerErrorWrapped: an error from the dialer is returned
// wrapped, so callers can match it with errors.Is, and nothing else
// is dialed.
func TestDialDialerErrorWrapped(t *testing.T) {
	var dialed atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Dial(ctx, "127.0.0.1:102", DialOptions{
		Dialer: allowlistDialer(netip.MustParsePrefix("10.0.0.0/8"), &dialed),
	})
	if !errors.Is(err, errDestinationDenied) {
		t.Fatalf("err = %v, want errors.Is(errDestinationDenied)", err)
	}
	if dialed.Load() != 0 {
		t.Fatalf("denied destination was dialed %d times", dialed.Load())
	}
}

// TestDialAllowlistByResolvedIP: an allowlist dialer checks the resolved
// IP, so "localhost" is accepted by a loopback allowlist.
func TestDialAllowlistByResolvedIP(t *testing.T) {
	addr := startDialerTestServer(t)
	_, port, _ := net.SplitHostPort(addr)
	var dialed atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, net.JoinHostPort("localhost", port), DialOptions{
		Dialer: allowlistDialer(netip.MustParsePrefix("127.0.0.0/8"), &dialed),
	})
	if err != nil {
		t.Fatalf("Dial via allowlist: %v", err)
	}
	_ = c.Close(ctx)
	if dialed.Load() != 1 {
		t.Fatalf("dialed = %d, want 1", dialed.Load())
	}
}

// TestDialDefaultDialerUnchanged: without a Dialer, Dial still connects
// with the default TCP dialer.
func TestDialDefaultDialerUnchanged(t *testing.T) {
	addr := startDialerTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, addr, DialOptions{})
	if err != nil {
		t.Fatalf("Dial with default dialer: %v", err)
	}
	_ = c.Close(ctx)
}

// faultConn fails the connection on its first Read, simulating a peer
// that resets the connection during the COTP handshake.
type faultConn struct {
	net.Conn
	failed atomic.Bool
}

func (c *faultConn) Read(b []byte) (int, error) {
	if c.failed.CompareAndSwap(false, true) {
		_ = c.Close()
		return 0, errors.New("fault: connection reset during handshake")
	}
	return c.Conn.Read(b)
}

// TestChaosDialerHandshakeFaults: a fault-injecting dialer resets the
// first connections mid-handshake; with Retry, Dial reaches the server
// on a later attempt, and without Retry it fails without hanging.
func TestChaosDialerHandshakeFaults(t *testing.T) {
	addr := startDialerTestServer(t)
	const faulty = 2
	var calls atomic.Int32
	dialer := func(ctx context.Context, network, a string) (net.Conn, error) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, network, a)
		if err != nil {
			return nil, err
		}
		if calls.Add(1) <= faulty {
			return &faultConn{Conn: conn}, nil
		}
		return conn, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, addr, DialOptions{
		Dialer: dialer,
		Retry:  mms.RetryPolicy{MaxAttempts: 5, InitialBackoff: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("Dial with retry over faulty dialer: %v", err)
	}
	if lds, err := c.ListLogicalDevices(ctx); err != nil || len(lds) == 0 {
		t.Fatalf("ListLogicalDevices after faulty attempts: %v", err)
	}
	_ = c.Close(ctx)
	if calls.Load() != faulty+1 {
		t.Fatalf("dialer calls = %d, want %d", calls.Load(), faulty+1)
	}

	calls.Store(0)
	start := time.Now()
	if c, err := Dial(ctx, addr, DialOptions{Dialer: dialer}); err == nil {
		_ = c.Close(ctx)
		t.Fatal("Dial succeeded over a connection reset during the handshake")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("failed dial took %v", d)
	}
}
