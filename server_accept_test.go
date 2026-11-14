// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// temporaryError is an accept error that says it is temporary.
type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary accept error" }
func (temporaryError) Temporary() bool { return true }

// acceptResult is one answer of a scriptedListener: a transport or an
// error.
type acceptResult struct {
	t   mms.Transport
	err error
}

// scriptedListener answers Accept from results and records when each
// Accept was called.
type scriptedListener struct {
	results chan acceptResult
	closed  chan struct{}
	once    sync.Once

	mu    sync.Mutex
	calls []time.Time
}

func newScriptedListener() *scriptedListener {
	return &scriptedListener{results: make(chan acceptResult, 64), closed: make(chan struct{})}
}

func (l *scriptedListener) Accept(ctx context.Context) (mms.Transport, error) {
	l.mu.Lock()
	l.calls = append(l.calls, time.Now())
	l.mu.Unlock()
	select {
	case r := <-l.results:
		return r.t, r.err
	case <-l.closed:
		return nil, mms.ErrServerClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *scriptedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *scriptedListener) Addr() net.Addr { return &net.UnixAddr{Name: "scripted", Net: "unix"} }

func (l *scriptedListener) callTimes() []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Time(nil), l.calls...)
}

func acceptServer(t *testing.T, opts ServerOptions) *Server {
	t.Helper()
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// dialScripted hands the server end of a new in-memory transport to ln
// and opens an association on the client end.
func dialScripted(t *testing.T, ln *scriptedListener) (*mms.Client, error) {
	t.Helper()
	clientT, serverT := loopbackPair(t)
	ln.results <- acceptResult{t: serverT}
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err == nil {
		t.Cleanup(func() { _ = mc.Abort(context.Background()) })
	}
	return mc, err
}

// ListenAndServe returns only after the connection goroutines it started
// have returned, OnDisconnect included (L15): when its context ends, and
// after an accept error that is not temporary, which first ends the
// associations it serves. The times are the fake time of the synctest
// bubble.
func TestListenAndServe_ReturnsAfterConnections(t *testing.T) {
	for _, how := range []string{"context ends", "listener fails"} {
		t.Run(how, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered := make(chan struct{}, 1)
				release := make(chan struct{})
				releaseOnce := sync.OnceFunc(func() { close(release) })
				defer releaseOnce()
				var disconnected atomic.Bool
				srv := acceptServer(t, ServerOptions{OnDisconnect: func(ConnectionEvent) {
					entered <- struct{}{}
					<-release
					disconnected.Store(true)
				}})
				ln := newScriptedListener()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				served := make(chan error, 1)
				go func() { served <- srv.ListenAndServe(ctx, ln) }()
				mc, err := dialScripted(t, ln)
				if err != nil {
					t.Fatalf("association: %v", err)
				}

				broke := errors.New("listener broke")
				if how == "context ends" {
					cancel()
				} else {
					ln.results <- acceptResult{err: broke}
				}
				<-entered // the association has ended; its goroutine is in OnDisconnect
				synctest.Wait()
				select {
				case err := <-served:
					t.Fatalf("ListenAndServe returned %v while OnDisconnect was still running", err)
				default:
				}
				releaseOnce()
				err = <-served
				if !disconnected.Load() {
					t.Error("ListenAndServe returned before OnDisconnect had returned")
				}
				want := context.Canceled
				if how == "listener fails" {
					want = broke
				}
				if !errors.Is(err, want) {
					t.Errorf("ListenAndServe = %v, want %v", err, want)
				}
				// The server has closed its end; the client's reader
				// goroutine sees that when it runs next.
				synctest.Wait()
				select {
				case <-mc.Done():
				default:
					t.Error("the association is still up after ListenAndServe returned")
				}
				if n := srv.Stats().Connections; n != 0 {
					t.Errorf("Stats().Connections = %d after ListenAndServe returned, want 0", n)
				}
			})
		})
	}
}

// ListenAndServe serves at most MaxConnections at once: a connection
// beyond it is closed before OnConnect and its association, and counted.
// When a connection ends, the next one is served again.
func TestServer_MaxConnections(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		t.Run("limit "+string(rune('0'+limit)), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var connects atomic.Int32
				srv := acceptServer(t, ServerOptions{MaxConnections: limit, OnConnect: func(ConnectionEvent) { connects.Add(1) }})
				ln := newScriptedListener()
				ctx, cancel := context.WithCancel(context.Background())
				served := make(chan error, 1)
				go func() { served <- srv.ListenAndServe(ctx, ln) }()
				defer func() {
					cancel()
					if err := <-served; !errors.Is(err, context.Canceled) {
						t.Errorf("ListenAndServe = %v, want context.Canceled", err)
					}
				}()

				var clients []*mms.Client
				for i := range limit {
					mc, err := dialScripted(t, ln)
					if err != nil {
						t.Fatalf("connection %d of %d: %v", i+1, limit, err)
					}
					clients = append(clients, mc)
				}
				synctest.Wait()
				if st := srv.Stats(); st.Connections != limit || st.ConnectionsRefused != 0 {
					t.Fatalf("at the limit: Stats() = %+v, want %d connections, none refused", st, limit)
				}
				if _, err := dialScripted(t, ln); err == nil {
					t.Fatal("a connection beyond MaxConnections was served")
				}
				synctest.Wait()
				if st := srv.Stats(); st.Connections != limit || st.ConnectionsRefused != 1 {
					t.Errorf("beyond the limit: Stats() = %+v, want %d connections, 1 refused", st, limit)
				}
				if got := connects.Load(); got != int32(limit) {
					t.Errorf("OnConnect ran %d times, want %d (not for the refused connection)", got, limit)
				}

				// One connection ends: the next is served.
				_ = clients[0].Abort(context.Background())
				synctest.Wait()
				if _, err := dialScripted(t, ln); err != nil {
					t.Fatalf("connection after one ended: %v", err)
				}
				synctest.Wait()
				if st := srv.Stats(); st.Connections != limit || st.ConnectionsRefused != 1 {
					t.Errorf("after one ended: Stats() = %+v, want %d connections, 1 refused", st, limit)
				}
			})
		})
	}
	t.Run("default", func(t *testing.T) {
		for _, n := range []int{0, -1} {
			if got := acceptServer(t, ServerOptions{MaxConnections: n}).maxConns; got != DefaultMaxConnections {
				t.Errorf("MaxConnections %d: limit %d, want DefaultMaxConnections", n, got)
			}
		}
		if got := acceptServer(t, ServerOptions{MaxConnections: 5000}).maxConns; got != 5000 {
			t.Errorf("MaxConnections 5000: limit %d", got)
		}
	})
}

// After a temporary accept error ListenAndServe waits before the next
// Accept: 5 ms, doubling up to 1 s; a connection accepted starts the
// pause again at 5 ms. Shutdown ends a pause at once. The times are the
// fake time of the synctest bubble.
func TestServer_AcceptBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := acceptServer(t, ServerOptions{})
		ln := newScriptedListener()
		const errs = 10
		for range errs {
			ln.results <- acceptResult{err: temporaryError{}}
		}
		served := make(chan error, 1)
		go func() { served <- srv.ListenAndServe(context.Background(), ln) }()
		time.Sleep(time.Minute) // fake time: every pause has passed
		synctest.Wait()

		want := []time.Duration{5, 10, 20, 40, 80, 160, 320, 640, 1000, 1000}
		calls := ln.callTimes()
		if len(calls) != errs+1 {
			t.Fatalf("%d Accept calls after %d temporary errors, want %d", len(calls), errs, errs+1)
		}
		for i, w := range want {
			if got := calls[i+1].Sub(calls[i]); got != w*time.Millisecond {
				t.Errorf("pause %d: %v, want %v", i+1, got, w*time.Millisecond)
			}
		}

		// A connection resets the pause.
		mc, err := dialScripted(t, ln)
		if err != nil {
			t.Fatal(err)
		}
		ln.results <- acceptResult{err: temporaryError{}}
		ln.results <- acceptResult{err: temporaryError{}}
		synctest.Wait()
		time.Sleep(time.Minute) // fake time
		synctest.Wait()
		calls = ln.callTimes()
		n := len(calls)
		if got := calls[n-2].Sub(calls[n-3]); got != 5*time.Millisecond {
			t.Errorf("first pause after a connection: %v, want 5ms", got)
		}
		if got := calls[n-1].Sub(calls[n-2]); got != 10*time.Millisecond {
			t.Errorf("second pause after a connection: %v, want 10ms", got)
		}
		_ = mc.Abort(context.Background())

		// Shutdown during a pause: ListenAndServe returns at once.
		ln.results <- acceptResult{err: temporaryError{}}
		synctest.Wait()
		start := time.Now()
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if err := <-served; !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
		}
		if got := time.Since(start); got != 0 {
			t.Errorf("ListenAndServe returned %v after Shutdown, want at once", got)
		}
	})
}
