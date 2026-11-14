// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// Lifecycle contract tests of Client, ReportSubscription and Server
// (TESTING.md, "Lifecycle contract matrix").

// L1: a second Server.Close returns and changes nothing: the journal
// and setting group engines stay reported as gone, and a Shutdown after
// Close returns nil.
func TestLifecycle_ServerCloseTwice(t *testing.T) {
	srv, err := NewServer(specModel(), ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableReports()
	srv.Close()
	srv.Close()
	if srv.JournalEngine() != nil || srv.SettingGroupEngine() != nil {
		t.Fatal("an engine is reported after Close")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown after Close: %v", err)
	}
}

// L2: Close from two goroutines at once on a Client, a ReportSubscription
// whose RCB it enabled and reserved, and Close and Shutdown together on a
// Server: every call returns nil, Reports is closed, the RCB is released
// once, and the client refuses requests with ErrClosed afterwards.
func TestLifecycle_ConcurrentClose(t *testing.T) {
	srv, _ := newSpecServer(t)
	mc, _, end := sharedAssociation(t, srv)
	defer end()
	ctx := waitCtx(t)

	c, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := c.SubscribeReport(context.Background(), "rpt_urcb01", urcbLifecycle)
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}

	closeTwice := func(name string, fn func() error) {
		t.Helper()
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { errs <- fn() })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("%s from two goroutines: %v", name, err)
			}
		}
	}
	closeTwice("ReportSubscription.Close", sub.Close)
	if !reportsClosed(sub) {
		t.Fatal("Reports is not closed")
	}
	if ena, resv := urcbState(t, observer); ena || resv {
		t.Fatalf("after the Close calls: RptEna=%v Resv=%v, want both FALSE", ena, resv)
	}
	closeTwice("Client.Close", func() error { return c.Close(ctx) })
	if _, err := c.GetReportControlBlock(ctx, "LD1", specURCB); !errors.Is(err, ErrClosed) {
		t.Fatalf("request after Close = %v, want ErrClosed", err)
	}

	other, err := NewServer(specModel(), ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other.EnableReports()
	closeTwice("Server.Close and Shutdown", func() error {
		other.Close()
		return other.Shutdown(ctx)
	})
}

// L5: a Dial whose peer stops answering during the COTP handshake (no
// CC) or the MMS association (no initiate response) returns the
// context's error once its context is cancelled, and closes the
// connection: the peer reads end of file.
func TestLifecycle_DialCancelDuringHandshake(t *testing.T) {
	t.Run("COTP", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ln.Close() }()
		accepted := make(chan net.Conn, 1)
		go func() {
			if conn, err := ln.Accept(); err == nil {
				accepted <- conn
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		dialed := dialAsync(ctx, ln.Addr().String())
		conn := <-accepted
		defer func() { _ = conn.Close() }()
		if _, err := conn.Read(make([]byte, 512)); err != nil { // the connection request
			t.Fatal(err)
		}
		cancel()
		if err := <-dialed; !errors.Is(err, context.Canceled) {
			t.Fatalf("Dial = %v, want context.Canceled", err)
		}
		expectEOF(t, conn)
	})
	t.Run("MMS initiate", func(t *testing.T) {
		ln, err := iso.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ln.Close() }()
		lctx, lcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer lcancel()
		accepted := make(chan mms.Transport, 1)
		go func() {
			if tr, err := ln.Accept(lctx); err == nil {
				accepted <- tr
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		dialed := dialAsync(ctx, ln.Addr().String())
		var tr mms.Transport
		select {
		case tr = <-accepted:
		case <-lctx.Done():
			t.Fatal("no connection completed the COTP handshake")
		}
		defer func() { _ = tr.Close() }()
		if _, err := tr.Receive(lctx); err != nil { // the association request
			t.Fatal(err)
		}
		cancel()
		if err := <-dialed; !errors.Is(err, context.Canceled) {
			t.Fatalf("Dial = %v, want context.Canceled", err)
		}
		if _, err := tr.Receive(lctx); err == nil || lctx.Err() != nil {
			t.Fatalf("the peer's next Receive = %v (%v), want the end of the connection", err, lctx.Err())
		}
	})
}

func dialAsync(ctx context.Context, addr string) <-chan error {
	out := make(chan error, 1)
	go func() {
		c, err := Dial(ctx, addr, DialOptions{})
		if c != nil {
			_ = c.Abort(context.Background())
		}
		out <- err
	}()
	return out
}

// expectEOF reads conn until the dialer has closed it; the deadline only
// bounds a failing test.
func expectEOF(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("the dialer did not close the connection: %v", err)
	}
}

// slowServer serves domain LDX with two INT32 variables: GGIO1$ST$Ind1$stVal
// answers 111 once release is closed, GGIO1$ST$Ind2$stVal answers 222 at
// once. reading receives a value each time the first is read.
func slowServer(t *testing.T) (c *Client, reading <-chan struct{}, release chan struct{}) {
	t.Helper()
	srv, reading, release := slowMMSServer(t)
	sctx, cancel := context.WithCancel(context.Background())
	clientT, serverT := loopbackPair(t)
	served := make(chan struct{})
	go func() { defer close(served); _ = srv.Serve(sctx, serverT) }()
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err = NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
		_ = mc.Close(context.Background())
		cancel()
		<-served
	})
	return c, reading, release
}

// slowMMSServer is the MMS server of slowServer.
func slowMMSServer(t *testing.T) (srv *mms.Server, reading <-chan struct{}, release chan struct{}) {
	t.Helper()
	srv = mms.NewServer(mms.ServerOptions{MMS: mms.ServerMMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	}})
	if err := srv.RegisterDomain("LDX"); err != nil {
		t.Fatal(err)
	}
	ts := mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32}
	rd := make(chan struct{}, 4)
	release = make(chan struct{})
	for item, read := range map[string]func(context.Context) (*mms.Value, error){
		"GGIO1$ST$Ind1$stVal": func(context.Context) (*mms.Value, error) {
			rd <- struct{}{}
			<-release
			return mms.NewInteger(111), nil
		},
		"GGIO1$ST$Ind2$stVal": func(context.Context) (*mms.Value, error) { return mms.NewInteger(222), nil },
	} {
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LDX", ItemID: mms.ItemID(item)},
			TypeSpec: ts, Read: read,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return srv, rd, release
}

// dialSlowServer connects to slowMMSServer over an in-memory transport
// and returns a Client that owns its association, as Dial builds it, so
// that the test may run in a synctest bubble. The pending read of Ind1
// is answered when the test ends.
func dialSlowServer(t *testing.T) (c *Client, reading <-chan struct{}) {
	t.Helper()
	srv, reading, ch := slowMMSServer(t)
	release := sync.OnceFunc(func() { close(ch) })
	ctx, cancel := context.WithCancel(context.Background())
	clientT, serverT := loopbackPair(t)
	served := make(chan struct{})
	go func() { defer close(served); _ = srv.Serve(ctx, serverT) }()
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err = NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c.ownsClient = true // as Dial: Close and Abort end the association
	t.Cleanup(func() {
		release()
		_ = c.Abort(context.Background())
		_ = mc.Abort(context.Background())
		cancel()
		<-served
	})
	return c, reading
}

// L3: Abort and Close of a dialled Client while a read waits for its
// response: the read returns mms.ErrClosed at once, and a request after
// them is refused with ErrClosed. Abort returns at once. Close returns
// when its context ends, and 5 seconds after it sent the Conclude at the
// latest: the server reads the Conclude only once its handler of the
// pending read has returned. The times are the fake time of the synctest
// bubble.
func TestLifecycle_ClientCloseDuringPendingRead(t *testing.T) {
	slow := mustParseRef(t, "LDX/GGIO1.Ind1.stVal[ST]")
	pendingRead := func(t *testing.T, c *Client) <-chan error {
		t.Helper()
		pending := make(chan error, 1)
		go func() {
			_, err := c.Read(context.Background(), slow)
			pending <- err
		}()
		return pending
	}
	checkStopped := func(t *testing.T, c *Client, pending <-chan error, what string) {
		t.Helper()
		synctest.Wait()
		select {
		case err := <-pending:
			if !errors.Is(err, mms.ErrClosed) {
				t.Fatalf("the pending Read after %s = %v, want mms.ErrClosed", what, err)
			}
		default:
			t.Fatalf("the pending Read has not returned after %s", what)
		}
		if _, err := c.Read(context.Background(), slow); !errors.Is(err, ErrClosed) {
			t.Fatalf("Read after %s = %v, want ErrClosed", what, err)
		}
	}

	t.Run("Abort", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, reading := dialSlowServer(t)
			pending := pendingRead(t, c)
			<-reading
			if err := c.Abort(context.Background()); err != nil {
				t.Fatalf("Abort with a read pending: %v", err)
			}
			checkStopped(t, c, pending, "Abort")
		})
	})

	t.Run("Close with a deadline", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, reading := dialSlowServer(t)
			pending := pendingRead(t, c)
			<-reading
			const limit = 2 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), limit)
			defer cancel()
			start := time.Now()
			closed := make(chan error, 1)
			go func() { closed <- c.Close(ctx) }()
			checkStopped(t, c, pending, "Close")
			if err := <-closed; !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Close = %v, want context.DeadlineExceeded (no Conclude response)", err)
			}
			if got := time.Since(start); got != limit {
				t.Errorf("Close returned after %v, want %v", got, limit)
			}
		})
	})

	// With a context without deadline, Close gives the Conclude 5
	// seconds and then closes the connection.
	t.Run("Close without a deadline", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, reading := dialSlowServer(t)
			pending := pendingRead(t, c)
			<-reading
			start := time.Now()
			closed := make(chan error, 1)
			go func() { closed <- c.Close(context.Background()) }()
			checkStopped(t, c, pending, "Close")
			if err := <-closed; !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Close = %v, want context.DeadlineExceeded (no Conclude response)", err)
			}
			if got := time.Since(start); got != closeConcludeTimeout {
				t.Errorf("Close returned after %v, want %v", got, closeConcludeTimeout)
			}
			select {
			case <-c.Done():
			default:
				t.Error("the association is up after Close returned")
			}
		})
	})
}

// L6 and L7: a read whose context is cancelled while the server is
// still answering returns context.Canceled; its response, which arrives
// later, completes no other request: the next reads on the same client
// get their own values, also a new read of the same variable.
func TestLifecycle_CancelledRequestReleasesItsSlot(t *testing.T) {
	c, reading, release := slowServer(t)
	ctx := waitCtx(t)
	slow, fast := mustParseRef(t, "LDX/GGIO1.Ind1.stVal[ST]"), mustParseRef(t, "LDX/GGIO1.Ind2.stVal[ST]")

	reqCtx, cancel := context.WithCancel(ctx)
	first := make(chan error, 1)
	go func() {
		_, err := c.Read(reqCtx, slow)
		first <- err
	}()
	<-reading
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Read = %v, want context.Canceled", err)
	}
	close(release) // the server now sends the late response (111)

	for i := range 3 {
		v, err := c.Read(ctx, fast)
		if err != nil {
			t.Fatalf("Read %d after the cancelled one: %v", i, err)
		}
		if n, _ := v.Int64(); n != 222 {
			t.Fatalf("Read %d of Ind2 = %d, want 222 (its own response)", i, n)
		}
	}
	v, err := c.Read(ctx, slow)
	if err != nil {
		t.Fatalf("Read of Ind1 again: %v", err)
	}
	if n, _ := v.Int64(); n != 111 {
		t.Fatalf("Read of Ind1 again = %d, want 111", n)
	}
}

// L9: ControlHandler callbacks may call other methods of their Server:
// the value store, the model and SetValue of another object answer, and
// the control service completes.
func TestLifecycle_ControlHandlerCallsServerMethods(t *testing.T) {
	var srv *Server
	called := make(chan string, 4)
	srv = controlServer(t, ControlHandler{
		OnSelect: func(context.Context, ControlRequest) error {
			if srv.Model() == nil {
				t.Error("Model from OnSelect is nil")
			}
			_ = srv.ReportEngine()
			called <- "select"
			return nil
		},
		OnOperate: func(ctx context.Context, req ControlRequest) error {
			_ = srv.ValueStore().Get(servermodel.StoreKey(ctlLD, "GGIO1$ST$SPCSO2$stVal"))
			srv.SetValue(ctx, servermodel.StoreKey(ctlLD, "GGIO1$ST$SPCSO2$stVal"), mms.NewBoolean(true))
			_ = srv.Capabilities()
			called <- "operate"
			return nil
		},
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 3}
	if err := c.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}
	if _, err := c.OperateWait(ctx, ref, params); err != nil {
		t.Fatalf("OperateWait: %v", err)
	}
	if a, b := <-called, <-called; a != "select" || b != "operate" {
		t.Fatalf("callbacks %q, %q", a, b)
	}
	if v := srv.ValueStore().Get(servermodel.StoreKey(ctlLD, "GGIO1$ST$SPCSO2$stVal")); v == nil || !v.Equal(mms.NewBoolean(true)) {
		t.Fatalf("SPCSO2.stVal set from OnOperate = %v, want TRUE", v)
	}
}

// L11: an OnOperate that has not returned holds only its own
// association: a second association reads the control's status and
// selects and operates another control meanwhile, and the slow operate
// completes once its handler returns.
func TestLifecycle_SlowControlHandlerHoldsOnlyItsAssociation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	srv := controlServer(t, ControlHandler{
		OnOperate: func(context.Context, ControlRequest) error {
			close(entered)
			<-release
			return nil
		},
	})
	slow, other := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlSBOw)
	params := OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1}
	if err := slow.SelectWithValue(ctx, ref, params); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}
	operated := make(chan error, 1)
	go func() {
		_, err := slow.OperateWait(ctx, ref, params)
		operated <- err
	}()
	select {
	case <-entered:
	case err := <-operated:
		t.Fatalf("OperateWait returned %v without calling OnOperate", err)
	}

	if _, err := other.Read(ctx, mustParseRef(t, ctlSBOw+".stVal[ST]")); err != nil {
		t.Fatalf("Read on another association during the slow operate: %v", err)
	}
	ref2 := mustParseRef(t, ctlLD+"/GGIO1.SPCSO2")
	if _, err := other.Select(ctx, ref2); err != nil {
		t.Fatalf("Select on another association during the slow operate: %v", err)
	}
	if err := other.Operate(ctx, ref2, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 2}); err != nil {
		t.Fatalf("Operate on another association during the slow operate: %v", err)
	}
	select {
	case err := <-operated:
		t.Fatalf("the slow OperateWait returned %v before its handler", err)
	default:
	}
	releaseOnce()
	if err := <-operated; err != nil {
		t.Fatalf("the slow OperateWait: %v", err)
	}
}

// L10: Shutdown called from OnConnect returns without waiting for the
// connection goroutine it runs on; ListenAndServe then returns
// mms.ErrServerClosed and the association being set up is refused.
func TestLifecycle_ShutdownFromOnConnect(t *testing.T) {
	ctx := waitCtx(t)
	shutdown := make(chan error, 1)
	var srv *Server
	srv, ln, served := listenedServer(t, ServerOptions{OnConnect: func(ConnectionEvent) {
		shutdown <- srv.Shutdown(ctx)
	}})
	clientT, serverT := loopbackPair(t)
	ln.conns <- serverT
	associated := make(chan error, 1)
	go func() {
		mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
		if mc != nil {
			_ = mc.Abort(context.Background())
		}
		associated <- err
	}()
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatalf("Shutdown from OnConnect = %v, want nil", err)
		}
	case <-ctx.Done():
		t.Fatal("Shutdown called from OnConnect has not returned")
	}
	select {
	case err := <-served:
		if !errors.Is(err, mms.ErrServerClosed) {
			t.Fatalf("ListenAndServe = %v, want mms.ErrServerClosed", err)
		}
	case <-ctx.Done():
		t.Fatal("ListenAndServe has not returned after Shutdown")
	}
	if err := <-associated; err == nil || ctx.Err() != nil {
		t.Fatalf("association after Shutdown = %v (%v), want it refused", err, ctx.Err())
	}
}

// L14: a SubscribeReport whose AutoEnable is refused leaves nothing
// behind: no subscription is registered for its RptID, and a later
// SubscribeReport with the same RptID receives reports.
func TestLifecycle_SubscribeReportFailureRollsBack(t *testing.T) {
	srv, _ := newSpecServer(t)
	mc, _, end := sharedAssociation(t, srv)
	defer end()
	c, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	opts := urcbLifecycle
	opts.RCBItemID = "LLN0$RP$doesNotExist"
	opts.ReserveURCB = false
	if sub, err := c.SubscribeReport(context.Background(), "rpt_urcb01", opts); err == nil {
		_ = sub.Close()
		t.Fatal("SubscribeReport on an RCB that does not exist succeeded")
	}
	c.reportMu.Lock()
	left := len(c.reportSubs["rpt_urcb01"])
	c.reportMu.Unlock()
	if left != 0 {
		t.Fatalf("%d subscriptions registered after the failed SubscribeReport", left)
	}
	sub, err := c.SubscribeReport(context.Background(), "rpt_urcb01", SubscribeReportOptions{
		LD: "LD1", RCBItemID: specURCB, AutoEnable: true, GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport after the failed one: %v", err)
	}
	defer func() { _ = sub.Close() }()
	select {
	case ri, ok := <-sub.Reports():
		if !ok || ri.RptID != "rpt_urcb01" {
			t.Fatalf("report %v (open %v)", ri, ok)
		}
	case <-waitCtx(t).Done():
		t.Fatal("no report after GI")
	}
}
