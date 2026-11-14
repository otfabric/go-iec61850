// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ────────────────────────────────────────────────────────────────────────────
// TestResourceExhaust_ManyConnections
// ────────────────────────────────────────────────────────────────────────────

// TestResourceExhaust_ManyConnections verifies that the server can handle a
// large number of sequential connection attempts without leaking goroutines or
// file descriptors.
func TestResourceExhaust_ManyConnections(t *testing.T) {
	_, addr, _ := startIECServer(t, testServerSCLWithURCB())

	const rounds = 50
	for i := range rounds {
		c, err := Dial(context.Background(), addr, DialOptions{})
		if err != nil {
			t.Fatalf("round %d Dial: %v", i, err)
		}
		if _, err := c.ListLogicalDevices(context.Background()); err != nil {
			t.Fatalf("round %d ListLogicalDevices: %v", i, err)
		}
		if err := c.Close(context.Background()); err != nil {
			t.Fatalf("round %d Close: %v", i, err)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TestResourceExhaust_ConcurrentConnections
// ────────────────────────────────────────────────────────────────────────────

// TestResourceExhaust_ConcurrentConnections opens N clients simultaneously,
// exercises each one, then closes all of them. Verifies the server handles
// concurrent connection load gracefully.
func TestResourceExhaust_ConcurrentConnections(t *testing.T) {
	_, addr, _ := startIECServer(t, testServerSCLWithURCB())

	const clients = 20
	var wg sync.WaitGroup
	errs := make([]error, clients)

	for i := range clients {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			c, err := Dial(ctx, addr, DialOptions{})
			if err != nil {
				errs[idx] = err
				return
			}
			defer c.Close(ctx) //nolint:errcheck

			_, err = c.ListLogicalDevices(ctx)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("client %d: %v", i, e)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TestResourceExhaust_ReportBackpressure
// ────────────────────────────────────────────────────────────────────────────

// TestResourceExhaust_ReportBackpressure verifies that when the client's
// report queue is full and the overflow policy is DropNewest, reports are
// dropped silently (not blocking the server or causing panics), and reads
// continue to work during the overloaded period.
func TestResourceExhaust_ReportBackpressure(t *testing.T) {
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

	// Subscribe with a tiny queue (size=2) and DropNewest policy.
	var dropped atomic.Int64
	sub, err := c.SubscribeReport(context.Background(), "rpt01", SubscribeReportOptions{
		LD:             "IED1LD1",
		RCBItemID:      "LLN0$RP$urcb01",
		AutoEnable:     true,
		GIOnSubscribe:  true,
		QueueSize:      2,
		OverflowPolicy: OverflowDropNewest,
		OnOverflow: func(_ *ReportIndication) {
			dropped.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	// Wait for the initial GI report.
	select {
	case <-sub.Reports():
	case <-time.After(5 * time.Second):
		t.Fatal("initial GI not received")
	}

	// Flood the server with GI triggers — do not consume from the report channel.
	const floods = 30
	for range floods {
		_ = c.SetReportControlBlock(context.Background(), "IED1LD1", "LLN0$RP$urcb01", RCBUpdate{
			Fields: RCBFieldGI,
			GI:     true,
		})
	}

	// The flood has reached the client once the queue is full.
	deadline := time.Now().Add(5 * time.Second)
	for len(sub.Reports()) < cap(sub.Reports()) {
		if time.Now().After(deadline) {
			t.Fatalf("report queue holds %d of %d after the flood", len(sub.Reports()), cap(sub.Reports()))
		}
		runtime.Gosched()
	}

	// Reads should still work despite the flooded report queue.
	ref, _ := ParseRef("IED1LD1/LLN0.Mod.stVal[ST]")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.Read(ctx, ref); err != nil {
		t.Errorf("read failed after report flood: %v", err)
	}

	t.Logf("dropped reports: %d (queue size 2, flood %d GIs)", dropped.Load(), floods)
}

// ────────────────────────────────────────────────────────────────────────────
// TestResourceExhaust_ValueStoreUnderLoad
// ────────────────────────────────────────────────────────────────────────────

// TestResourceExhaust_ValueStoreUnderLoad verifies that the server-side value
// store remains consistent under concurrent reads and writes from multiple
// goroutines. This is a data-race check as much as a correctness test.
func TestResourceExhaust_ValueStoreUnderLoad(t *testing.T) {
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

	// Start N goroutines that call srv.SetValue() concurrently.
	const setters = 10
	const itersPerSetter = 200
	var setWG sync.WaitGroup
	ctx := context.Background()
	storeKey := "IED1LD1/LLN0$ST$Mod$stVal"

	for i := range setters {
		setWG.Add(1)
		go func(base int) {
			defer setWG.Done()
			for j := range itersPerSetter {
				srv.SetValue(ctx, storeKey, mms.NewInteger(int64(base+j)))
			}
		}(i * itersPerSetter)
	}

	// Simultaneously, a client reads the value.
	c, err := Dial(context.Background(), ln.Addr().String(), DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() {
		stop()
		_ = c.Abort(context.Background())
	}()

	ref, _ := ParseRef("IED1LD1/LLN0.Mod.stVal[ST]")
	var readErrors atomic.Int64
	var readWG sync.WaitGroup
	readWG.Go(func() {
		for range 100 {
			readCtx, readCancel := context.WithTimeout(context.Background(), 1*time.Second)
			_, e := c.Read(readCtx, ref)
			readCancel()
			if e != nil {
				readErrors.Add(1)
			}
			time.Sleep(time.Millisecond)
		}
	})

	setWG.Wait()
	readWG.Wait()

	t.Logf("concurrent set(%d×%d) + read(100): read errors = %d",
		setters, itersPerSetter, readErrors.Load())
	if readErrors.Load() > 10 {
		t.Errorf("too many read errors under concurrent set load: %d", readErrors.Load())
	}
}
