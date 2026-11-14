// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// recordingHandler keeps the records logged at error level.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelError }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// recovered returns the callback of each record that reports a callback
// that did not return normally, in order, and checks that each record
// carries the stack of the callback.
func (h *recordingHandler) recovered(t *testing.T) []string {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		var callback, stack string
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "callback":
				callback = a.Value.String()
			case "stack":
				stack = a.Value.String()
			}
			return true
		})
		if callback == "" {
			continue
		}
		if r.Level != slog.LevelError || !strings.Contains(stack, "server_callback_recover_test.go") {
			t.Errorf("record of %s: level %v, stack without the callback:\n%s", callback, r.Level, stack)
		}
		out = append(out, callback)
	}
	return out
}

const recoverLD = "IED1LD1"

// recoverServer is a server of testServerSCL with opts, logging to the
// returned handler.
func recoverServer(t *testing.T, opts ServerOptions) (*Server, *recordingHandler) {
	t.Helper()
	h := &recordingHandler{}
	opts.Logger = slog.New(h)
	model, err := NewServerModelFromSCL(testServerSCL(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, h
}

// serveAssociation serves a new in-memory connection on srv and opens an
// association on it with opts; the cleanup ends both.
func serveAssociation(t *testing.T, srv *Server, opts mms.DialOptions) (*mms.Client, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clientT, serverT := loopbackPair(t)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ctx, serverT)
	}()
	mc, err := mms.NewClient(context.Background(), clientT, opts)
	t.Cleanup(func() {
		if mc != nil {
			_ = mc.Abort(context.Background())
		}
		cancel()
		<-served
	})
	return mc, err
}

func readMod(ctx context.Context, mc *mms.Client) error {
	_, err := mc.Read(ctx, mms.ReadRequest{DomainID: recoverLD, ItemID: "LLN0$ST$Mod$stVal"})
	return err
}

// wantRecovered checks the counter and the log of the calls that did not
// return normally.
func wantRecovered(t *testing.T, srv *Server, logs *recordingHandler, want ...string) {
	t.Helper()
	if got := srv.Stats().CallbacksRecovered; got != uint64(len(want)) {
		t.Errorf("Stats().CallbacksRecovered = %d, want %d", got, len(want))
	}
	if got := logs.recovered(t); !slices.Equal(got, want) {
		t.Errorf("logged %q, want %q", got, want)
	}
}

// An OnConnect or OnDisconnect hook that does not return normally is
// logged with its stack and counted; the connection is served as usual,
// and so is the next one.
func TestServer_ConnectionHooksThatDoNotReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var connects, disconnects atomic.Int32
		srv, logs := recoverServer(t, ServerOptions{
			OnConnect: func(ConnectionEvent) {
				if connects.Add(1) == 1 {
					panic("OnConnect test value")
				}
			},
			OnDisconnect: func(ConnectionEvent) {
				if disconnects.Add(1) == 1 {
					panic("OnDisconnect test value")
				}
			},
		})
		ctx := context.Background()
		for i := range 2 {
			mc, err := serveAssociation(t, srv, mms.DialOptions{})
			if err != nil {
				t.Fatalf("association %d: %v", i+1, err)
			}
			if err := readMod(ctx, mc); err != nil {
				t.Fatalf("association %d: read: %v", i+1, err)
			}
			_ = mc.Abort(ctx)
			synctest.Wait()
			if got := disconnects.Load(); got != int32(i+1) {
				t.Fatalf("after association %d: OnDisconnect ran %d times", i+1, got)
			}
		}
		wantRecovered(t, srv, logs, "OnConnect", "OnDisconnect")
	})
}

// failingFiles is a memFileProvider whose method fail does not return
// normally the first time it is called.
type failingFiles struct {
	*memFileProvider
	fail    string
	tripped atomic.Bool
}

func (p *failingFiles) trip(method string) {
	if method == p.fail && p.tripped.CompareAndSwap(false, true) {
		panic("FileProvider test value")
	}
}

func (p *failingFiles) List(ctx context.Context, req mms.FileListRequest) (*mms.FileListResult, error) {
	p.trip("List")
	return p.memFileProvider.List(ctx, req)
}

func (p *failingFiles) Open(ctx context.Context, path string) (mms.FileHandle, mms.FileAttributes, error) {
	p.trip("Open")
	return p.memFileProvider.Open(ctx, path)
}

func (p *failingFiles) Read(ctx context.Context, h mms.FileHandle, maxBytes int) ([]byte, bool, error) {
	p.trip("Read")
	return p.memFileProvider.Read(ctx, h, maxBytes)
}

func (p *failingFiles) Close(ctx context.Context, h mms.FileHandle) error {
	p.trip("Close")
	return p.memFileProvider.Close(ctx, h)
}

func (p *failingFiles) Delete(ctx context.Context, path string) error {
	p.trip("Delete")
	return p.memFileProvider.Delete(ctx, path)
}

func (p *failingFiles) Rename(ctx context.Context, current, next string) error {
	p.trip("Rename")
	return p.memFileProvider.Rename(ctx, current, next)
}

func (p *failingFiles) ObtainFile(ctx context.Context, source, destination string) error {
	p.trip("ObtainFile")
	return p.memFileProvider.ObtainFile(ctx, source, destination)
}

// A FileProvider method that does not return normally answers that file
// service with an error; the same service is served again on that
// association, and other associations are served.
func TestServer_FileProviderThatDoesNotReturn(t *testing.T) {
	ops := map[string]func(context.Context, *Client) error{
		"List":       func(ctx context.Context, c *Client) error { _, err := c.ListFiles(ctx, ""); return err },
		"Open":       func(ctx context.Context, c *Client) error { _, _, err := c.ReadFile(ctx, "/a"); return err },
		"Read":       func(ctx context.Context, c *Client) error { _, _, err := c.ReadFile(ctx, "/a"); return err },
		"Close":      func(ctx context.Context, c *Client) error { _, _, err := c.ReadFile(ctx, "/a"); return err },
		"Delete":     func(ctx context.Context, c *Client) error { return c.DeleteFile(ctx, "/a") },
		"Rename":     func(ctx context.Context, c *Client) error { return c.RenameFile(ctx, "/a", "/b") },
		"ObtainFile": func(ctx context.Context, c *Client) error { return c.ObtainFile(ctx, "/a", "/c") },
	}
	for method, op := range ops {
		t.Run(method, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fp := &failingFiles{memFileProvider: newMemFileProvider(), fail: method}
				fp.addFile("/a", []byte("file data"), time.Time{})
				srv, logs := recoverServer(t, ServerOptions{FileProvider: fp})
				ctx := context.Background()
				clients := make([]*Client, 2)
				for i := range clients {
					mc, err := serveAssociation(t, srv, mms.DialOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if clients[i], err = NewClient(mc, ClientOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				if err := op(ctx, clients[0]); err == nil {
					t.Fatalf("%s did not return normally: the service succeeded", method)
				}
				if err := op(ctx, clients[0]); err != nil {
					t.Errorf("the same service again: %v", err)
				}
				if _, err := clients[1].ListFiles(ctx, ""); err != nil {
					t.Errorf("another association: ListFiles: %v", err)
				}
				wantRecovered(t, srv, logs, "FileProvider."+method)
			})
		})
	}
}

// An Authenticate that does not return normally rejects the association
// it was called for; the next association is authenticated and served.
func TestServer_AuthenticateThatDoesNotReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		srv, logs := recoverServer(t, ServerOptions{Authenticate: func(context.Context, *mms.AuthContext) (mms.AuthResult, error) {
			if calls.Add(1) == 1 {
				panic("Authenticate test value")
			}
			return mms.AuthResult{Accept: true}, nil
		}})
		if _, err := serveAssociation(t, srv, mms.DialOptions{}); err == nil {
			t.Fatal("the association whose Authenticate did not return normally was accepted")
		}
		mc, err := serveAssociation(t, srv, mms.DialOptions{})
		if err != nil {
			t.Fatalf("next association: %v", err)
		}
		if err := readMod(context.Background(), mc); err != nil {
			t.Errorf("next association: read: %v", err)
		}
		wantRecovered(t, srv, logs, "Authenticate")
	})
}

// failingMetrics counts its calls, none of which returns normally.
type failingMetrics struct{ calls atomic.Int32 }

func (m *failingMetrics) ObserveRequest(mms.RequestEvent) {
	m.calls.Add(1)
	panic("Metrics test value")
}

func (m *failingMetrics) ObserveConnection(mms.ConnectionEvent) {
	m.calls.Add(1)
	panic("Metrics test value")
}

// Metrics of the server and of the client whose calls do not return
// normally lose those events; every request is answered and every
// association served.
func TestMetricsThatDoNotReturn(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			m := &failingMetrics{}
			srv, logs := recoverServer(t, ServerOptions{Metrics: m})
			ctx := context.Background()
			for i := range 2 {
				mc, err := serveAssociation(t, srv, mms.DialOptions{})
				if err != nil {
					t.Fatalf("association %d: %v", i+1, err)
				}
				for range 2 {
					if err := readMod(ctx, mc); err != nil {
						t.Fatalf("association %d: read: %v", i+1, err)
					}
				}
				_ = mc.Abort(ctx)
				synctest.Wait()
			}
			n := m.calls.Load()
			if n < 6 {
				t.Fatalf("Metrics called %d times, want the requests and connections of two associations", n)
			}
			if got := srv.Stats().CallbacksRecovered; got != uint64(n) {
				t.Errorf("Stats().CallbacksRecovered = %d, want %d", got, n)
			}
			got := logs.recovered(t)
			if !slices.Contains(got, "Metrics.ObserveRequest") || !slices.Contains(got, "Metrics.ObserveConnection") {
				t.Errorf("logged %q, want both Metrics methods", got)
			}
		})
	})
	t.Run("client", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			srv, _ := recoverServer(t, ServerOptions{})
			m := &failingMetrics{}
			h := &recordingHandler{}
			mc, err := serveAssociation(t, srv, dialMMSOptions(DialOptions{Metrics: m}, slog.New(h)))
			if err != nil {
				t.Fatalf("association: %v", err)
			}
			for range 2 {
				if err := readMod(context.Background(), mc); err != nil {
					t.Fatalf("read: %v", err)
				}
			}
			if m.calls.Load() < 2 {
				t.Fatalf("client Metrics called %d times", m.calls.Load())
			}
			if got := h.recovered(t); !slices.Contains(got, "Metrics.ObserveRequest") {
				t.Errorf("logged %q, want Metrics.ObserveRequest", got)
			}
		})
	})
}

// failingJournal is a JournalProvider of one journal whose method fail
// does not return normally the first time it is called.
type failingJournal struct {
	fail    string
	tripped atomic.Bool
}

func (j *failingJournal) trip(method string) {
	if method == j.fail && j.tripped.CompareAndSwap(false, true) {
		panic("JournalProvider test value")
	}
}

func (j *failingJournal) ListJournals(context.Context, string) ([]string, error) {
	j.trip("ListJournals")
	return []string{"LLN0$Log"}, nil
}

func (j *failingJournal) ReadTimeRange(context.Context, string, string, time.Time, time.Time, int) (*mms.JournalResult, error) {
	j.trip("ReadTimeRange")
	return &mms.JournalResult{}, nil
}

func (j *failingJournal) ReadStartAfter(context.Context, string, string, []byte, time.Time, int) (*mms.JournalResult, error) {
	j.trip("ReadStartAfter")
	return &mms.JournalResult{}, nil
}

// failingQueryJournal adds the optional journal interfaces.
type failingQueryJournal struct{ failingJournal }

func (j *failingQueryJournal) ReadJournal(context.Context, mms.JournalQuery) (*mms.JournalResult, error) {
	j.trip("ReadJournal")
	return &mms.JournalResult{}, nil
}

func (j *failingQueryJournal) InitializeJournal(context.Context, string, string, *mms.JournalLimit) (uint32, error) {
	j.trip("InitializeJournal")
	return 0, nil
}

// A JournalProvider method that does not return normally answers that
// journal service with an error; the service is served again on that
// association, and other associations are served.
func TestServer_JournalProviderThatDoesNotReturn(t *testing.T) {
	cases := []struct {
		method string
		query  bool
		op     func(context.Context, *Client) error
	}{
		{"ListJournals", false, func(ctx context.Context, c *Client) error { _, err := c.ListJournals(ctx, "LD1"); return err }},
		{"ReadTimeRange", false, func(ctx context.Context, c *Client) error {
			_, err := c.ReadJournal(ctx, "LD1", "LLN0$Log", time.Time{}, time.Time{})
			return err
		}},
		{"ReadStartAfter", false, func(ctx context.Context, c *Client) error {
			_, err := c.ReadJournalAfter(ctx, "LD1", "LLN0$Log", time.Unix(1e9, 0), []byte{1})
			return err
		}},
		{"ReadJournal", true, func(ctx context.Context, c *Client) error {
			_, err := c.ReadJournal(ctx, "LD1", "LLN0$Log", time.Time{}, time.Time{})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var jp mms.JournalProvider = &failingJournal{fail: tc.method}
				if tc.query {
					jp = &failingQueryJournal{failingJournal{fail: tc.method}}
				}
				srv, logs := recoverServer(t, ServerOptions{MMS: mms.ServerOptions{JournalProvider: jp}})
				ctx := context.Background()
				clients := make([]*Client, 2)
				for i := range clients {
					mc, err := serveAssociation(t, srv, mms.DialOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if clients[i], err = NewClient(mc, ClientOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				if err := tc.op(ctx, clients[0]); err == nil {
					t.Fatalf("%s did not return normally: the service succeeded", tc.method)
				}
				if err := tc.op(ctx, clients[0]); err != nil {
					t.Errorf("the same service again: %v", err)
				}
				if _, err := clients[1].ListJournals(ctx, "LD1"); err != nil {
					t.Errorf("another association: ListJournals: %v", err)
				}
				wantRecovered(t, srv, logs, "JournalProvider."+tc.method)
			})
		})
	}
}

// The guarded JournalProvider has the optional interfaces of the
// provider it guards, and only those; each of its methods recovers.
func TestGuardJournal_KeepsOptionalInterfaces(t *testing.T) {
	h := &recordingHandler{}
	g := &callbackGuard{logger: slog.New(h)}
	plain := guardJournal(&failingJournal{}, g)
	if _, ok := plain.(mms.JournalQueryProvider); ok {
		t.Error("guarded plain provider is a JournalQueryProvider")
	}
	if _, ok := plain.(mms.JournalInitializer); ok {
		t.Error("guarded plain provider is a JournalInitializer")
	}
	both := guardJournal(&failingQueryJournal{}, g)
	_, okQ := both.(mms.JournalQueryProvider)
	_, okI := both.(mms.JournalInitializer)
	if !okQ || !okI {
		t.Fatalf("guarded provider with both interfaces: query %v, initializer %v", okQ, okI)
	}
	type onlyQuery struct{ mms.JournalQueryProvider }
	type onlyInit struct {
		mms.JournalProvider
		mms.JournalInitializer
	}
	if _, ok := guardJournal(onlyQuery{&failingQueryJournal{}}, g).(mms.JournalInitializer); ok {
		t.Error("guarded query-only provider is a JournalInitializer")
	}
	fj := &failingQueryJournal{}
	gi := guardJournal(onlyInit{fj, fj}, g)
	if _, ok := gi.(mms.JournalQueryProvider); ok {
		t.Error("guarded initializer-only provider is a JournalQueryProvider")
	}
	if _, ok := gi.(mms.JournalInitializer); !ok {
		t.Error("guarded initializer-only provider is not a JournalInitializer")
	}

	ctx := context.Background()
	for _, method := range []string{"ReadJournal", "InitializeJournal"} {
		qj := &failingQueryJournal{failingJournal{fail: method}}
		gq := guardJournal(qj, g)
		var err error
		if method == "ReadJournal" {
			_, err = gq.(mms.JournalQueryProvider).ReadJournal(ctx, mms.JournalQuery{})
		} else {
			_, err = gq.(mms.JournalInitializer).InitializeJournal(ctx, "LD", "j", nil)
		}
		if !errors.Is(err, errCallbackRecovered) {
			t.Errorf("%s = %v, want the recovered error", method, err)
		}
	}
	if got := g.n.Load(); got != 2 {
		t.Errorf("%d calls recovered, want 2", got)
	}
}

// armedClock is a Clock whose Now does not return normally while armed.
type armedClock struct{ armed atomic.Bool }

func (c *armedClock) Now() time.Time {
	if c.armed.Load() {
		panic("Clock test value")
	}
	return time.Now()
}

func (c *armedClock) Quality() TimeQuality { return TimeQuality{} }

// A variable write that reaches application code that does not return
// normally (here the Clock, which stamps the journal entry of the write)
// is answered with object-access-denied; the next write and the other
// associations are served.
func TestServer_WriteThatDoesNotReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := &armedClock{}
		srv, logs := recoverServer(t, ServerOptions{AllowReadOnlyFCWrites: true, Clock: clk})
		srv.EnableJournals()
		ctx := context.Background()
		a, err := serveAssociation(t, srv, mms.DialOptions{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := serveAssociation(t, srv, mms.DialOptions{})
		if err != nil {
			t.Fatal(err)
		}
		write := func(mc *mms.Client, v int64) error {
			_, err := mc.Write(ctx, mms.WriteRequest{DomainID: recoverLD, ItemID: "LLN0$ST$Mod$stVal", Value: mms.NewInteger(v)})
			return err
		}
		clk.armed.Store(true)
		err = write(a, 2)
		clk.armed.Store(false)
		var dae *mms.DataAccessError
		if !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectAccessDenied {
			t.Fatalf("write whose Clock did not return normally = %v, want object-access-denied", err)
		}
		if err := write(a, 3); err != nil {
			t.Errorf("next write: %v", err)
		}
		if err := write(b, 4); err != nil {
			t.Errorf("another association: write: %v", err)
		}
		if err := readMod(ctx, b); err != nil {
			t.Errorf("another association: read: %v", err)
		}
		wantRecovered(t, srv, logs, "Write")
	})
}

// The Read and Write of every variable of the model are registered under
// the guard: a call that does not return normally is answered with
// object-access-denied, and the other variables are read as before.
func TestGuardedRegistrar_ReadAndWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &recordingHandler{}
		g := &callbackGuard{logger: slog.New(h)}
		mmsSrv := mms.NewServer(mms.ServerOptions{MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
		}})
		r := guardedRegistrar{srv: mmsSrv, g: g}
		if err := r.RegisterDomain("LD"); err != nil {
			t.Fatal(err)
		}
		intType := mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32}
		for _, v := range []mms.Variable{
			{
				Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "bad"}, TypeSpec: intType,
				Read:  func(context.Context) (*mms.Value, error) { panic("Read test value") },
				Write: func(context.Context, *mms.Value) error { panic("Write test value") },
			},
			{
				Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "good"}, TypeSpec: intType,
				Read: func(context.Context) (*mms.Value, error) { return mms.NewInteger(7), nil },
			},
		} {
			if err := r.RegisterVariable(v); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.RegisterNamedVariableList(mms.NamedVariableList{
			Name:      mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "ds"},
			Variables: []mms.VariableSpec{{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD", ItemID: "good"}}},
		}); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		clientT, serverT := loopbackPair(t)
		served := make(chan struct{})
		go func() {
			defer close(served)
			_ = mmsSrv.Serve(ctx, serverT)
		}()
		defer func() { cancel(); <-served }()
		mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = mc.Abort(context.Background()) }()

		var dae *mms.DataAccessError
		if _, err := mc.Read(ctx, mms.ReadRequest{DomainID: "LD", ItemID: "bad"}); !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectAccessDenied {
			t.Errorf("read of a Read that did not return normally = %v, want object-access-denied", err)
		}
		if _, err := mc.Write(ctx, mms.WriteRequest{DomainID: "LD", ItemID: "bad", Value: mms.NewInteger(1)}); !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectAccessDenied {
			t.Errorf("write of a Write that did not return normally = %v, want object-access-denied", err)
		}
		if res, err := mc.Read(ctx, mms.ReadRequest{DomainID: "LD", ItemID: "good"}); err != nil || res.Value == nil {
			t.Errorf("read of another variable: %v", err)
		}
		if got := g.n.Load(); got != 2 {
			t.Errorf("%d calls recovered, want 2", got)
		}
		if got := h.recovered(t); !slices.Equal(got, []string{"Read", "Write"}) {
			t.Errorf("logged %q, want Read and Write", got)
		}
	})
}

// A ControlHandler callback that does not return normally refuses that
// control service; it is logged with its stack and counted, and the next
// operate of that association and of another one is served.
func TestServer_ControlHandlerThatDoesNotReturn(t *testing.T) {
	h := &recordingHandler{}
	model, err := NewServerModelFromSCL(specControlSCL(t, specCF{}), "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{Logger: slog.New(h)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	var calls atomic.Int32
	mustRegister(t, srv, "GGIO1.SPCTm", CtlModelDirectNormal, ControlHandler{OnOperate: func(context.Context, ControlRequest) error {
		if calls.Add(1) == 1 {
			panic("ControlHandler test value")
		}
		return nil
	}})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlTm)
	if err := a.Operate(ctx, ref, boolParams(1)); err == nil {
		t.Fatal("the operate whose OnOperate did not return normally succeeded")
	}
	if err := a.Operate(ctx, ref, boolParams(2)); err != nil {
		t.Errorf("next operate: %v", err)
	}
	if err := b.Operate(ctx, ref, boolParams(3)); err != nil {
		t.Errorf("another association: operate: %v", err)
	}
	wantRecovered(t, srv, h, "ControlHandler")
}
