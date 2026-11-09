// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"sync"
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

// pipeListener is an in-memory net.Listener: Dial hands it the server end
// of a net.Pipe and returns the client end, so no socket is opened.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		_, _ = client.Close(), server.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_, _ = client.Close(), server.Close()
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// startPipeServer serves the chaos test model on an in-memory listener.
func startPipeServer(t *testing.T) *pipeListener {
	t.Helper()
	srv := newChaosServer(t)
	pl := newPipeListener()
	ln := iso.NewListener(pl)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.ListenAndServe(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		_ = pl.Close()
		<-done
	})
	return pl
}

// TestDialInMemoryDialer: a dialer returning one end of a net.Pipe gives a
// fully working client without any socket; the address is only passed
// through to the dialer.
func TestDialInMemoryDialer(t *testing.T) {
	pl := startPipeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, "ied.invalid:102", DialOptions{Dialer: pl.dial})
	if err != nil {
		t.Fatalf("Dial over in-memory dialer: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()
	lds, err := c.ListLogicalDevices(ctx)
	if err != nil || len(lds) == 0 {
		t.Fatalf("ListLogicalDevices: %v (%d LDs)", err, len(lds))
	}
	if _, err := c.MMS().Read(ctx, mms.ReadRequest{DomainID: "IED1LD1", ItemID: "LLN0$ST$Mod$stVal"}); err != nil {
		t.Fatalf("Read over in-memory dialer: %v", err)
	}
}

// TestDialDialerRetryClassification: a dialer error is retried like any
// other by default, and Retry.Retryable can stop on an allowlist refusal
// (API.md, DialOptions).
func TestDialDialerRetryClassification(t *testing.T) {
	var calls atomic.Int32
	deny := func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errDestinationDenied
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	retry := mms.RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Millisecond}

	if _, err := Dial(ctx, "10.0.0.1:102", DialOptions{Dialer: deny, Retry: retry}); !errors.Is(err, errDestinationDenied) {
		t.Fatalf("err = %v, want errDestinationDenied", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("default classification: dialer calls = %d, want 3", calls.Load())
	}

	calls.Store(0)
	retry.Retryable = func(err error) bool {
		return !errors.Is(err, errDestinationDenied) && mms.IsRetryable(err)
	}
	if _, err := Dial(ctx, "10.0.0.1:102", DialOptions{Dialer: deny, Retry: retry}); !errors.Is(err, errDestinationDenied) {
		t.Fatalf("err = %v, want errDestinationDenied", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("Retryable refusing denial: dialer calls = %d, want 1", calls.Load())
	}
}

// TestDialDialerHonoursContext: the dialer receives Dial's context, so a
// dialer that blocks until it is done ends with the context's error.
func TestDialDialerHonoursContext(t *testing.T) {
	block := func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Dial(ctx, "10.0.0.1:102", DialOptions{Dialer: block}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Dial took %v after its context ended", d)
	}
}

// byteFaultConn injects faults into the bytes a client reads, from a fixed
// seed: after a random number of clean bytes it corrupts one, truncates a
// read, or resets the connection.
type byteFaultConn struct {
	net.Conn
	mu    sync.Mutex
	rng   *rand.Rand
	clean int // bytes left before the next fault
}

func (c *byteFaultConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= c.clean {
		c.clean -= n
		return n, err
	}
	i := c.clean
	c.clean = 16 + c.rng.Intn(512)
	switch c.rng.Intn(3) {
	case 0:
		b[i] ^= byte(1 + c.rng.Intn(255))
		return n, err
	case 1:
		return i, err
	default:
		_ = c.Close()
		return i, errors.New("fault: connection reset")
	}
}

// TestChaosDialerByteFaults: for fixed seeds, a dialer whose connections
// corrupt, truncate or reset the byte stream at random offsets. Dial and
// every operation and Close must end, with a result or an error, within
// their deadlines; nothing may panic or hang.
func TestChaosDialerByteFaults(t *testing.T) {
	pl := startPipeServer(t)
	var dialed, failedDials, okOps, failedOps int
	for seed := int64(1); seed <= 12; seed++ {
		rng := rand.New(rand.NewSource(seed))
		dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := pl.dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &byteFaultConn{Conn: conn, rng: rng, clean: rng.Intn(256)}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		start := time.Now()
		c, err := Dial(ctx, "ied.invalid:102", DialOptions{
			Dialer: dialer,
			Retry:  mms.RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Millisecond},
		})
		if time.Since(start) > 3*time.Second {
			cancel()
			t.Fatalf("seed %d: Dial did not honour its deadline", seed)
		}
		if err != nil {
			failedDials++
		} else {
			dialed++
			ok, failed := chaosOps(t, c, 6)
			okOps += ok
			failedOps += failed
			// Close must be bounded by its context even when a write to
			// a peer that stopped reading holds the send lock (go-mms
			// v1.7.1, otfabric/go-mms#34).
			cctx, ccancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			start := time.Now()
			_ = c.Close(cctx)
			ccancel()
			if d := time.Since(start); d > 2*time.Second {
				cancel()
				t.Fatalf("seed %d: Close took %v", seed, d)
			}
		}
		cancel()
	}
	if failedDials+failedOps == 0 {
		t.Fatal("no fault reached Dial or an operation: the fault injector is not working")
	}
	t.Logf("dials ok=%d failed=%d; operations ok=%d failed=%d", dialed, failedDials, okOps, failedOps)
}
