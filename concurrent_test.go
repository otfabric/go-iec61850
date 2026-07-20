// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"runtime"
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
	srv.MMS().SetVariableRead("LD1", "LLN0$ST$Mod$stVal", func(ctx context.Context) (*mms.Value, error) {
		select {
		case reading <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}) //nolint:errcheck

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(bgCtx, ln) }()

	c, err := Dial(context.Background(), ln.Addr().String(), DialOptions{})
	if err != nil {
		bgCancel()
		t.Fatalf("Dial: %v", err)
	}

	const workers = 4
	reqCtx, reqCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ref, _ := ParseRef("LD1/LLN0.Mod.stVal[ST]")
			_, errs[idx] = c.Read(reqCtx, ref)
		}(i)
	}

	// Wait for at least one request to reach the blocking handler.
	select {
	case <-reading:
	case <-time.After(2 * time.Second):
		reqCancel()
		bgCancel()
		t.Fatal("no request reached the blocking handler")
	}
	reqCancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		bgCancel()
		t.Fatal("cancelled requests did not return within 3 s")
	}

	ctxErrCount := 0
	for _, e := range errs {
		if e != nil && (errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded)) {
			ctxErrCount++
		}
	}

	// Cancel server (unblocks any server-side handlers), then abort client.
	bgCancel()
	srv.Close()
	time.Sleep(50 * time.Millisecond)
	c.Abort(context.Background())

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

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()
	go func() { _ = srv.ListenAndServe(bgCtx, ln) }()

	c, err := Dial(context.Background(), ln.Addr().String(), DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() {
		bgCancel()
		time.Sleep(30 * time.Millisecond)
		c.Abort(context.Background())
	}()

	// Subscribe to URCB with GI.
	sub, err := c.SubscribeReport(context.Background(), "rpt01", SubscribeReportOptions{
		LD:            "LD1",
		RCBItemID:     "LLN0$RP$urcb01",
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close()

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
				_ = c.SetReportControlBlock(context.Background(), "LD1", "LLN0$RP$urcb01", RCBUpdate{
					Fields: RCBFieldGI,
					GI:     true,
				})
			}
		}
	}()

	var readWG sync.WaitGroup
	const readers = 6
	for i := 0; i < readers; i++ {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for j := 0; j < 10; j++ {
				ref, _ := ParseRef("LD1/LLN0.Mod.stVal[ST]")
				_, e := c.Read(ctx, ref)
				if e == nil {
					readsOK.Add(1)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
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

	var wg sync.WaitGroup
	const workers = 20
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			ref, _ := ParseRef("LD1/LLN0.Mod.stVal[ST]")
			_ = c.Write(ctx, ref, mms.NewInteger(1))
		}()
	}

	// Close while writes are flying.
	time.Sleep(10 * time.Millisecond)
	c.Abort(context.Background())

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("goroutines did not exit after client.Abort()")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TestConcurrent_TimeoutDoesNotLeakGoroutines
// ────────────────────────────────────────────────────────────────────────────

// TestConcurrent_TimeoutDoesNotLeakGoroutines verifies that timed-out requests
// do not leave behind goroutines. It issues requests with a very short timeout,
// lets them expire, then checks the goroutine count.
func TestConcurrent_TimeoutDoesNotLeakGoroutines(t *testing.T) {
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Make reads block until their ctx is done.
	srv.MMS().SetVariableRead("LD1", "LLN0$ST$Mod$stVal", func(ctx context.Context) (*mms.Value, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}) //nolint:errcheck

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(bgCtx, ln) }()

	c, err := Dial(context.Background(), ln.Addr().String(), DialOptions{})
	if err != nil {
		bgCancel()
		t.Fatalf("Dial: %v", err)
	}

	ref, _ := ParseRef("LD1/LLN0.Mod.stVal[ST]")

	// Warm-up.
	for i := 0; i < 3; i++ {
		tCtx, tCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, _ = c.Read(tCtx, ref)
		tCancel()
	}
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		tCtx, tCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, _ = c.Read(tCtx, ref)
		tCancel()
	}
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()

	// Cancel server and abort client cleanly.
	bgCancel()
	srv.Close()
	time.Sleep(50 * time.Millisecond)
	c.Abort(context.Background())

	const maxDelta = 3
	if delta := after - before; delta > maxDelta {
		t.Errorf("goroutine leak after timed-out requests: before=%d after=%d delta=%d (max %d)",
			before, after, delta, maxDelta)
	}
}
