// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// chanListener is an in-memory mms.TransportListener: each transport
// pushed onto conns is returned from Accept; acceptErr, when set, is
// returned once the conns channel is drained and closed.
type chanListener struct {
	conns     chan mms.Transport
	acceptErr error
	closed    atomic.Bool
}

func newChanListener() *chanListener {
	return &chanListener{conns: make(chan mms.Transport, 4)}
}

func (l *chanListener) Accept(ctx context.Context) (mms.Transport, error) {
	select {
	case t, ok := <-l.conns:
		if !ok {
			return nil, l.acceptErr
		}
		return t, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *chanListener) Close() error {
	l.closed.Store(true)
	return nil
}

func (l *chanListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

func newHookedServer(t *testing.T, connected, disconnected chan<- struct{}) *Server {
	t.Helper()
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}
	srv, err := NewServer(model, ServerOptions{
		OnConnect:    func(ConnectionEvent) { connected <- struct{}{} },
		OnDisconnect: func(ConnectionEvent) { disconnected <- struct{}{} },
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestListenAndServe_WithHooksServesAndFiresCallbacks(t *testing.T) {
	connected := make(chan struct{}, 4)
	disconnected := make(chan struct{}, 4)
	srv := newHookedServer(t, connected, disconnected)

	ln := newChanListener()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, ln) }()

	// Two sequential connections through the hooked accept loop.
	for i := 0; i < 2; i++ {
		clientT, serverT := loopbackPair()
		ln.conns <- serverT
		mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
		if err != nil {
			t.Fatalf("mms.NewClient #%d: %v", i, err)
		}
		c, err := NewClient(mmsClient, ClientOptions{})
		if err != nil {
			t.Fatalf("NewClient #%d: %v", i, err)
		}
		waitSignal(t, connected, "OnConnect")

		lds, err := c.ListLogicalDevices(ctx)
		if err != nil {
			t.Fatalf("ListLogicalDevices #%d: %v", i, err)
		}
		if len(lds) != 1 || lds[0].Name != "LD1" {
			t.Errorf("logical devices = %v, want [LD1]", lds)
		}
		_ = c.Close(ctx)
		_ = mmsClient.Close(ctx)
		_ = clientT.Close()
		waitSignal(t, disconnected, "OnDisconnect")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("ListenAndServe = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after cancel")
	}
	if !ln.closed.Load() {
		t.Error("listener should be closed when the accept loop exits")
	}
}

func TestListenAndServe_WithHooksReturnsAcceptError(t *testing.T) {
	srv := newHookedServer(t, make(chan struct{}, 1), make(chan struct{}, 1))
	ln := newChanListener()
	ln.acceptErr = errors.New("listener broke")
	close(ln.conns)

	err := srv.ListenAndServe(context.Background(), ln)
	if err == nil || err.Error() != "listener broke" {
		t.Fatalf("ListenAndServe = %v, want accept error", err)
	}
	if !ln.closed.Load() {
		t.Error("listener should be closed on accept error")
	}
}

func TestListenAndServe_WithoutHooksDelegatesToMMS(t *testing.T) {
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ln := newChanListener()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, ln) }()

	clientT, serverT := loopbackPair()
	ln.conns <- serverT
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	c, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lds, err := c.ListLogicalDevices(ctx); err != nil || len(lds) != 1 {
		t.Fatalf("ListLogicalDevices = %v, %v", lds, err)
	}
	_ = c.Close(ctx)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after cancel")
	}
}

// stubFileProvider satisfies mms.FileProvider for option-wiring tests; its
// methods are never invoked.
type stubFileProvider struct{ mms.FileProvider }

func TestNewServer_ConvenienceOptionsAreWired(t *testing.T) {
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(model, ServerOptions{
		FileProvider: stubFileProvider{},
		Authenticate: func(context.Context, *mms.AuthContext) (mms.AuthResult, error) {
			return mms.AuthResult{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	if !srv.Capabilities().Files {
		t.Error("FileProvider convenience field should enable file services")
	}

	// An explicitly supplied MemoryJournalProvider is adopted as the
	// server's journal provider.
	jp := NewMemoryJournalProvider()
	srv2, err := NewServer(model, ServerOptions{MMS: mms.ServerOptions{JournalProvider: jp}})
	if err != nil {
		t.Fatal(err)
	}
	defer srv2.Close()
	if srv2.mmsJournalProvider != jp {
		t.Error("explicit MemoryJournalProvider should be used by the server")
	}
}

func TestNewServer_AutoJournalProviderForModelsWithLogs(t *testing.T) {
	model := &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []servermodel.LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			DataObjects: []servermodel.DataObject{{
				Name:       "Mod",
				Attributes: []servermodel.DataAttribute{{Name: "stVal", FC: "ST", BType: "INT32"}},
			}},
			Logs: []servermodel.LogDef{{Name: "EventLog"}},
		}},
	}}}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	if srv.mmsJournalProvider == nil {
		t.Fatal("a model with logs should get an automatic MemoryJournalProvider")
	}

	plain, err := NewServer(testReportSCL(), ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if plain.mmsJournalProvider != nil {
		t.Error("a model without logs should not get a journal provider")
	}
}
