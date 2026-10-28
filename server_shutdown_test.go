// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// countMetrics counts the go-mms metric events it receives.
type countMetrics struct {
	mu       sync.Mutex
	requests int
	conns    int
}

func (m *countMetrics) ObserveRequest(mms.RequestEvent) {
	m.mu.Lock()
	m.requests++
	m.mu.Unlock()
}

func (m *countMetrics) ObserveConnection(mms.ConnectionEvent) {
	m.mu.Lock()
	m.conns++
	m.mu.Unlock()
}

func (m *countMetrics) counts() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests, m.conns
}

// closingListener is a chanListener whose Accept returns once Close is
// called, like a real listener.
type closingListener struct {
	*chanListener
	once sync.Once
	done chan struct{}
}

func newClosingListener() *closingListener {
	return &closingListener{chanListener: newChanListener(), done: make(chan struct{})}
}

func (l *closingListener) Accept(ctx context.Context) (mms.Transport, error) {
	select {
	case t := <-l.conns:
		return t, nil
	case <-l.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *closingListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.chanListener.Close()
}

// TestServerShutdown_TCP covers Server.Shutdown on the go-mms accept loop
// and the Metrics convenience fields of ServerOptions and DialOptions.
func TestServerShutdown_TCP(t *testing.T) {
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	var srvMetrics, cliMetrics countMetrics
	srv, err := NewServer(model, ServerOptions{Metrics: &srvMetrics})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(context.Background(), ln) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, ln.Addr().String(), DialOptions{Metrics: &cliMetrics})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
	select {
	case <-c.MMS().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client association not ended by Shutdown")
	}

	if r, n := srvMetrics.counts(); r == 0 || n == 0 {
		t.Errorf("server metrics: requests=%d conns=%d, want both > 0", r, n)
	}
	if r, n := cliMetrics.counts(); r == 0 || n == 0 {
		t.Errorf("client metrics: requests=%d conns=%d, want both > 0", r, n)
	}
}

// TestServerShutdown_WithHooks covers Server.Shutdown on the hook-routing
// accept loop, including a ListenAndServe started after Shutdown.
func TestServerShutdown_WithHooks(t *testing.T) {
	srv := newHookedServer(t, make(chan struct{}, 4), make(chan struct{}, 4))
	ln := newClosingListener()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(context.Background(), ln) }()

	// Let the loop register its listener before shutting down.
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}

	ln2 := newClosingListener()
	if err := srv.ListenAndServe(context.Background(), ln2); !errors.Is(err, mms.ErrServerClosed) {
		t.Errorf("ListenAndServe after Shutdown = %v, want ErrServerClosed", err)
	}
	if !ln2.closed.Load() {
		t.Error("listener passed after Shutdown should be closed")
	}
}
