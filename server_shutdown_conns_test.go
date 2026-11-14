// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// listenedServer serves testServerSCL on an in-process listener and
// returns the server, the listener and the channel ListenAndServe reports
// its result on.
func listenedServer(t *testing.T, opts ServerOptions) (*Server, *closingListener, <-chan error) {
	t.Helper()
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, opts)
	if err != nil {
		t.Fatal(err)
	}
	ln := newClosingListener()
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe(context.Background(), ln) }()
	return srv, ln, served
}

// connectTo opens an association through ln and returns its MMS client.
func connectTo(t *testing.T, ln *closingListener) *mms.Client {
	t.Helper()
	clientT, serverT := loopbackPair(t)
	ln.conns <- serverT
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return mc
}

// Shutdown waits for the connection goroutines of ListenAndServe (L15):
// while an OnDisconnect hook has not returned, Shutdown does not return
// nil. A context that ends first cuts the wait short and its error is
// returned; a later Shutdown waits again and returns nil once the
// goroutine has returned. The times are the fake time of the synctest
// bubble, and the bubble ends with no goroutine left.
func TestServerShutdown_WaitsForConnectionGoroutines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		releaseOnce := sync.OnceFunc(func() { close(release) })
		defer releaseOnce() // also when the test fails
		var finished atomic.Bool
		srv, ln, served := listenedServer(t, ServerOptions{
			OnDisconnect: func(ConnectionEvent) {
				close(entered)
				<-release
				finished.Store(true)
			},
		})
		mc := connectTo(t, ln)
		defer func() { _ = mc.Abort(context.Background()) }()

		const limit = 3 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		defer cancel()
		start := time.Now()
		first := make(chan error, 1)
		go func() { first <- srv.Shutdown(ctx) }()

		<-entered // the association is concluded; its goroutine is in OnDisconnect
		synctest.Wait()
		select {
		case err := <-first:
			t.Fatalf("Shutdown returned %v while OnDisconnect was still running", err)
		default:
		}
		if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Shutdown cut short by its context = %v, want context.DeadlineExceeded", err)
		}
		if got := time.Since(start); got != limit {
			t.Errorf("Shutdown returned after %v, want %v", got, limit)
		}
		// ListenAndServe waits for its connection goroutines too.
		select {
		case err := <-served:
			t.Fatalf("ListenAndServe returned %v while OnDisconnect was still running", err)
		default:
		}

		// A later Shutdown waits for the goroutine that is still running.
		second := make(chan error, 1)
		go func() { second <- srv.Shutdown(context.Background()) }()
		synctest.Wait()
		select {
		case err := <-second:
			t.Fatalf("second Shutdown returned %v while OnDisconnect was still running", err)
		default:
		}
		releaseOnce()
		if err := <-second; err != nil {
			t.Errorf("second Shutdown = %v, want nil", err)
		}
		if err := <-served; !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
		}
		if !finished.Load() {
			t.Error("Shutdown returned nil before OnDisconnect had returned")
		}
		srv.lnMu.Lock()
		left := len(srv.conns)
		srv.lnMu.Unlock()
		if left != 0 {
			t.Errorf("%d connection goroutines still registered after Shutdown, want 0", left)
		}
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Errorf("third Shutdown = %v, want nil", err)
		}
	})
}

// Shutdown may be called from OnDisconnect (L10): it does not wait for
// the connection goroutine it is called on, and waits for the others.
func TestServerShutdown_FromOnDisconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var srv *Server
		ready := make(chan struct{})
		result := make(chan error, 1)
		var calls atomic.Int32
		srv, ln, served := listenedServer(t, ServerOptions{
			OnDisconnect: func(ConnectionEvent) {
				<-ready
				if calls.Add(1) == 1 {
					result <- srv.Shutdown(context.Background())
				}
			},
		})
		close(ready)
		a, b := connectTo(t, ln), connectTo(t, ln)
		defer func() { _ = b.Abort(context.Background()) }()

		// The first association ends: its OnDisconnect shuts the server
		// down, which concludes the second association too.
		_ = a.Abort(context.Background())
		synctest.Wait()
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("Shutdown from OnDisconnect = %v, want nil", err)
			}
		default:
			t.Fatal("Shutdown called from OnDisconnect has not returned")
		}
		if got := calls.Load(); got != 2 {
			t.Errorf("OnDisconnect ran %d times before Shutdown returned, want 2 (both connections)", got)
		}
		if err := <-served; !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
		}
	})
}

// A transport accepted after Shutdown has begun is closed and not served.
func TestServerShutdown_AcceptAfterShutdownIsNotServed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var connects atomic.Int32
		srv, ln, served := listenedServer(t, ServerOptions{OnConnect: func(ConnectionEvent) { connects.Add(1) }})
		synctest.Wait() // the accept loop waits in Accept

		srv.lnMu.Lock()
		srv.shutdown = true // as Shutdown sets it, before the listener is closed
		srv.lnMu.Unlock()
		_, serverT := loopbackPair(t)
		ln.conns <- serverT
		if err := <-served; !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
		}
		if _, err := serverT.Receive(context.Background()); err == nil {
			t.Error("the transport accepted after Shutdown is not closed")
		}
		if n := connects.Load(); n != 0 {
			t.Errorf("OnConnect ran %d times for a connection that is not served", n)
		}
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
}
