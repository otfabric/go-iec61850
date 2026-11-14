// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ────────────────────────────────────────────────────────────────────────────
// TestConcurrent_ContextCancelReleasesInflightReads
// ────────────────────────────────────────────────────────────────────────────

// TestConcurrent_ContextCancelReleasesInflightReads starts N concurrent reads
// against a server whose read handler blocks until the request context is
// cancelled. It verifies that cancelling the request context unblocks all
// callers within a short deadline.
func TestConcurrent_ContextCancelReleasesInflightReads(t *testing.T) {
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Intercept reads so they block until ctx is done.
	reading := make(chan struct{}, 1)
	if err := srv.MMS().SetVariableRead("IED1LD1", "LLN0$ST$Mod$stVal", func(ctx context.Context) (*mms.Value, error) {
		select {
		case reading <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}); err != nil {
		t.Fatalf("SetVariableRead: %v", err)
	}

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	stop := serveISO(t, srv, ln)

	c, err := Dial(context.Background(), ln.Addr().String(), DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	const workers = 4
	reqCtx, reqCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ref, _ := ParseRef("IED1LD1/LLN0.Mod.stVal[ST]")
			_, errs[idx] = c.Read(reqCtx, ref)
		}(i)
	}

	// Wait for at least one request to reach the blocking handler.
	select {
	case <-reading:
	case <-time.After(2 * time.Second):
		reqCancel()
		t.Fatal("no request reached the blocking handler")
	}
	reqCancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled requests did not return within 3 s")
	}

	ctxErrCount := 0
	for _, e := range errs {
		if e != nil && (errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded)) {
			ctxErrCount++
		}
	}

	// Stop the server (unblocks any server-side handlers; returns once its
	// connection goroutines have ended), then abort the client.
	stop()
	_ = c.Abort(context.Background())

	if ctxErrCount == 0 {
		t.Errorf("expected at least one context-cancelled error, got none (errors: %v)", errs)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TestConcurrent_ReportsDuringOutstandingRequests
// ────────────────────────────────────────────────────────────────────────────

// TestConcurrent_ReportsDuringOutstandingRequests verifies that asynchronous
// report delivery does not interfere with synchronous read requests running
// concurrently.
func TestConcurrent_ReportsDuringOutstandingRequests(t *testing.T) {
	sclData := testServerSCLWithURCB()
	model, err := NewServerModelFromSCL(sclData, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	re := srv.EnableReports()
	defer re.Stop()

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	stop := serveISO(t, srv, ln)

	c, err := Dial(context.Background(), ln.Addr().String(), DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() {
		stop()
		_ = c.Abort(context.Background())
	}()

	// Subscribe to URCB with GI.
	sub, err := c.SubscribeReport(context.Background(), "rpt01", SubscribeReportOptions{
		LD:            "IED1LD1",
		RCBItemID:     "LLN0$RP$urcb01",
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	// Wait for the initial GI report.
	select {
	case <-sub.Reports():
	case <-time.After(5 * time.Second):
		t.Fatal("GI report not received")
	}

	// Run concurrent reads while triggering periodic GI reports.
	var readsOK atomic.Int64
	var reportsRx atomic.Int64

	stopGI := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopGI:
				return
			case <-time.After(20 * time.Millisecond):
				_ = c.SetReportControlBlock(context.Background(), "IED1LD1", "LLN0$RP$urcb01", RCBUpdate{
					Fields: RCBFieldGI,
					GI:     true,
				})
			}
		}
	}()

	var readWG sync.WaitGroup
	const readers = 6
	for range readers {
		readWG.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for range 10 {
				ref, _ := ParseRef("IED1LD1/LLN0.Mod.stVal[ST]")
				_, e := c.Read(ctx, ref)
				if e == nil {
					readsOK.Add(1)
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}

	// Drain reports concurrently.
	stopDrain := make(chan struct{})
	go func() {
		for {
			select {
			case <-sub.Reports():
				reportsRx.Add(1)
			case <-stopDrain:
				return
			}
		}
	}()

	readWG.Wait()
	close(stopGI)
	close(stopDrain)

	t.Logf("reads OK: %d, reports received: %d", readsOK.Load(), reportsRx.Load())

	if readsOK.Load() == 0 {
		t.Error("all reads failed during concurrent report delivery")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TestConcurrent_ManyWritesThenClose
// ────────────────────────────────────────────────────────────────────────────

// TestConcurrent_ManyWritesThenClose verifies that closing the client while
// many concurrent writes are in-flight does not panic or hang.
func TestConcurrent_ManyWritesThenClose(t *testing.T) {
	_, addr, _ := startIECServer(t, testServerSCLWithURCB())

	c, err := Dial(context.Background(), addr, DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// Every worker writes until the client is closed, so writes are in
	// flight whenever Abort comes. The server refuses each one (ST is read
	// only): a refusal is an answer like any other.
	var wg sync.WaitGroup
	var answered sync.Once
	firstAnswer := make(chan struct{})
	const workers = 20
	for range workers {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ref, _ := ParseRef("IED1LD1/LLN0.Mod.stVal[ST]")
			for {
				err := c.Write(ctx, ref, mms.NewInteger(1))
				if errors.Is(err, ErrClosed) || ctx.Err() != nil {
					return
				}
				answered.Do(func() { close(firstAnswer) })
			}
		})
	}

	// Abort while writes are flying: after the first answer.
	select {
	case <-firstAnswer:
	case <-time.After(5 * time.Second):
		t.Fatal("no write was answered")
	}
	_ = c.Abort(context.Background())

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("goroutines did not exit after client.Abort()")
	}
}
