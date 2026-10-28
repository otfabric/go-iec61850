// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// faultTransport wraps an MMS transport and injects faults into the
// frames it receives once the association is up (after skip frames):
// random corruption, truncation, drops and a hard disconnect after
// closeAfter frames.
type faultTransport struct {
	mms.Transport

	mu         sync.Mutex
	rng        *rand.Rand
	skip       int
	corrupt    float64
	truncate   float64
	drop       float64
	closeAfter int // 0 = never
	seen       int
	injected   atomic.Int64
}

func (t *faultTransport) Receive(ctx context.Context) ([]byte, error) {
	for {
		b, err := t.Transport.Receive(ctx)
		if err != nil {
			return nil, err
		}
		t.mu.Lock()
		t.seen++
		n := t.seen
		if t.closeAfter > 0 && n > t.closeAfter {
			t.mu.Unlock()
			t.injected.Add(1)
			_ = t.Transport.Close()
			return nil, errors.New("fault: connection reset")
		}
		if n <= t.skip || len(b) == 0 {
			t.mu.Unlock()
			return b, nil
		}
		r := t.rng.Float64()
		switch {
		case r < t.drop:
			t.mu.Unlock()
			t.injected.Add(1)
			continue
		case r < t.drop+t.truncate:
			b = b[:t.rng.Intn(len(b))]
			t.injected.Add(1)
		case r < t.drop+t.truncate+t.corrupt:
			b = append([]byte(nil), b...)
			for i := 0; i < 1+t.rng.Intn(3); i++ {
				b[t.rng.Intn(len(b))] ^= byte(1 + t.rng.Intn(255))
			}
			t.injected.Add(1)
		}
		t.mu.Unlock()
		return b, nil
	}
}

// pipeEnd is a context-aware in-process transport: unlike chanTransport
// it never blocks past the caller's context or a close of either end,
// which matters when a peer stops reading after a fault.
type pipeEnd struct {
	in, out chan []byte
	done    chan struct{}
	once    *sync.Once
}

func pipePair() (mms.Transport, mms.Transport) {
	a, b := make(chan []byte, 16), make(chan []byte, 16)
	done, once := make(chan struct{}), &sync.Once{}
	return &pipeEnd{in: a, out: b, done: done, once: once}, &pipeEnd{in: b, out: a, done: done, once: once}
}

func (p *pipeEnd) Send(ctx context.Context, b []byte) error {
	select {
	case p.out <- append([]byte(nil), b...):
		return nil
	case <-p.done:
		return errors.New("pipe closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pipeEnd) Receive(ctx context.Context) ([]byte, error) {
	select {
	case b := <-p.in:
		return b, nil
	case <-p.done:
		return nil, errors.New("pipe closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pipeEnd) Close() error {
	p.once.Do(func() { close(p.done) })
	return nil
}

// closeMMS closes an MMS client with a bounded conclude: after a fault
// the peer may no longer read, and Close blocks for as long as its
// context allows.
func closeMMS(mc *mms.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = mc.Close(ctx)
}

func newChaosServer(t *testing.T) *Server {
	t.Helper()
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// chaosOps runs a mix of client operations, each bounded by a short
// timeout. It fails the test if an operation hangs past the deadline.
func chaosOps(t *testing.T, c *Client, n int) (ok, failed int) {
	t.Helper()
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		start := time.Now()
		var err error
		switch i % 4 {
		case 0:
			_, err = c.MMS().Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$ST$Mod$stVal"})
		case 1:
			_, err = c.ListLogicalDevices(ctx)
		case 2:
			_, err = c.GetReportControlBlock(ctx, "LD1", "LLN0$RP$urcb01")
		case 3:
			_, err = c.MMS().Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$ST$Mod"})
		}
		cancel()
		if time.Since(start) > 2*time.Second {
			t.Fatalf("operation %d did not honour its deadline", i)
		}
		if err != nil {
			failed++
		} else {
			ok++
		}
	}
	return ok, failed
}

// TestChaosMMSClientFaultyResponses corrupts, truncates and drops server
// responses. The client must never panic or hang; every operation ends
// with a result or an error within its deadline.
func TestChaosMMSClientFaultyResponses(t *testing.T) {
	for seed := int64(1); seed <= 4; seed++ {
		srv := newChaosServer(t)
		ctx, cancel := context.WithCancel(context.Background())
		clientT, serverT := pipePair()
		go func() { _ = srv.Serve(ctx, serverT) }()
		ft := &faultTransport{Transport: clientT, rng: rand.New(rand.NewSource(seed)), skip: 1, corrupt: 0.15, truncate: 0.1, drop: 0.1}
		mc, err := mms.NewClient(ctx, ft, mms.DialOptions{})
		if err != nil {
			cancel()
			t.Fatalf("seed %d: associate: %v", seed, err)
		}
		c, _ := NewClient(mc, ClientOptions{})
		ok, failed := chaosOps(t, c, 20)
		t.Logf("seed %d: ok=%d failed=%d injected=%d", seed, ok, failed, ft.injected.Load())
		if ft.injected.Load() == 0 {
			t.Errorf("seed %d: no faults injected", seed)
		}
		closeMMS(mc)
		cancel()
	}
}

// TestChaosMMSServerFaultyRequests corrupts client requests on the
// server side. The server must keep serving: a clean second connection
// still works.
func TestChaosMMSServerFaultyRequests(t *testing.T) {
	srv := newChaosServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientT, serverT := pipePair()
	ft := &faultTransport{Transport: serverT, rng: rand.New(rand.NewSource(7)), skip: 1, corrupt: 0.3, truncate: 0.2}
	go func() { _ = srv.Serve(ctx, ft) }()
	if mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{}); err == nil {
		c, _ := NewClient(mc, ClientOptions{})
		chaosOps(t, c, 20)
		closeMMS(mc)
	}

	clientT2, serverT2 := pipePair()
	go func() { _ = srv.Serve(ctx, serverT2) }()
	mc, err := mms.NewClient(ctx, clientT2, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeMMS(mc)
	c, _ := NewClient(mc, ClientOptions{})
	if ok, failed := chaosOps(t, c, 8); failed != 0 {
		t.Fatalf("clean connection after chaos: ok=%d failed=%d", ok, failed)
	}
}

// TestChaosMMSConnectionDrop resets the connection mid-session: pending
// and later operations fail promptly instead of hanging.
func TestChaosMMSConnectionDrop(t *testing.T) {
	srv := newChaosServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientT, serverT := pipePair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	ft := &faultTransport{Transport: clientT, rng: rand.New(rand.NewSource(1)), closeAfter: 5}
	mc, err := mms.NewClient(ctx, ft, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewClient(mc, ClientOptions{})
	ok, failed := chaosOps(t, c, 12)
	if ok == 0 || failed == 0 {
		t.Fatalf("ok=%d failed=%d, want some of each", ok, failed)
	}
}

// TestDialRetry covers RetryPolicy: a server that comes up after the
// first attempts is reached, and a dead address fails after the
// configured attempts.
func TestDialRetry(t *testing.T) {
	// Reserve a port, release it, and start the server on it later.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := newChaosServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(150 * time.Millisecond)
		ln, err := iso.Listen(addr)
		if err != nil {
			t.Errorf("listen: %v", err)
			return
		}
		_ = srv.ListenAndServe(ctx, ln)
	}()
	dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
	defer dcancel()
	c, err := Dial(dctx, addr, DialOptions{Retry: RetryPolicy{Attempts: 20, Backoff: 20 * time.Millisecond, MaxBackoff: 100 * time.Millisecond}})
	if err != nil {
		t.Fatalf("dial with retry: %v", err)
	}
	_ = c.Close(ctx)

	// Dead address: fails after the attempts are exhausted.
	l2, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := l2.Addr().String()
	_ = l2.Close()
	start := time.Now()
	if _, err := Dial(ctx, dead, DialOptions{Retry: RetryPolicy{Attempts: 3, Backoff: 10 * time.Millisecond}}); err == nil {
		t.Fatal("dial to dead address succeeded")
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Error("retries did not back off")
	}
	// Context cancellation stops the retry loop.
	cctx, ccancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer ccancel()
	if _, err := Dial(cctx, dead, DialOptions{Retry: RetryPolicy{Attempts: 100, Backoff: time.Second}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancelled dial: %v", err)
	}

	p := RetryPolicy{}
	if p.delay(1) != 100*time.Millisecond || p.delay(10) != 5*time.Second {
		t.Errorf("default delays %v %v", p.delay(1), p.delay(10))
	}
}
