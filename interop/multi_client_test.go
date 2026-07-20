//go:build interop

// SPDX-License-Identifier: MIT

// Phase H2 — multi-client isolation and concurrency tests.
//
// These tests verify that the go-iec61850 server correctly handles multiple
// concurrent clients: reads and writes from independent clients must not
// interfere, disconnecting one client must not affect others, and each
// client's invoke IDs must remain isolated.
package interop

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// TestGoServer_MultiClient_ConcurrentReads launches five concurrent go-iec61850
// clients against the go server, each reading two attributes. All reads must
// succeed without interfering with each other.
func TestGoServer_MultiClient_ConcurrentReads(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	mxRef, _ := iec61850.ParseRef("InteropLD/MMXU1.TotW.mag.f[MX]")

	const numClients = 5
	errCh := make(chan error, numClients*2)
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
			defer c.Close(ctx)

			if _, err := c.ReadRaw(ctx, stRef); err != nil {
				errCh <- fmt.Errorf("client %d: read SPS1.stVal: %w", id, err)
			}
			if _, err := c.ReadRaw(ctx, mxRef); err != nil {
				errCh <- fmt.Errorf("client %d: read TotW.mag.f: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent read error: %v", err)
	}
}

// TestGoServer_MultiClient_ConcurrentWrites_IndependentVars launches three
// concurrent clients each writing to a distinct boolean attribute. No client
// should see the other's error, and all writes must succeed.
func TestGoServer_MultiClient_ConcurrentWrites_IndependentVars(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Three writable boolean ST attributes — one per client.
	targets := []struct {
		ref string
		val bool
	}{
		{"InteropLD/GGIO1.SPCSO1.stVal[ST]", true},
		{"InteropLD/GGIO1.SPCSO2.stVal[ST]", false},
		{"InteropLD/GGIO1.SPCSO3.stVal[ST]", true},
	}

	errCh := make(chan error, len(targets))
	var wg sync.WaitGroup

	for i, tgt := range targets {
		wg.Add(1)
		go func(id int, refStr string, val bool) {
			defer wg.Done()

			c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
			defer c.Close(ctx)

			ref, parseErr := iec61850.ParseRef(refStr)
			if parseErr != nil {
				errCh <- fmt.Errorf("client %d: ParseRef %q: %w", id, refStr, parseErr)
				return
			}
			if err := c.Write(ctx, ref, mms.NewBoolean(val)); err != nil {
				errCh <- fmt.Errorf("client %d: write %s: %w", id, refStr, err)
			}
		}(i, tgt.ref, tgt.val)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent write error: %v", err)
	}
}

// TestGoServer_MultiClient_InvokeIDIsolation connects three clients and has
// each issue ten sequential reads. No client should receive another client's
// response; all reads must return the correct value.
func TestGoServer_MultiClient_InvokeIDIsolation(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")

	const (
		numClients = 3
		numReads   = 10
	)

	errCh := make(chan error, numClients*numReads)
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
			defer c.Close(ctx)

			for j := 0; j < numReads; j++ {
				raw, err := c.ReadRaw(ctx, stRef)
				if err != nil {
					errCh <- fmt.Errorf("client %d read %d: %w", id, j, err)
					continue
				}
				b, ok := raw.Bool()
				if !ok {
					errCh <- fmt.Errorf("client %d read %d: expected boolean, got %s", id, j, raw.Type())
					continue
				}
				if b != fixVal.SPS1StVal {
					errCh <- fmt.Errorf("client %d read %d: want SPS1.stVal=%v, got %v", id, j, fixVal.SPS1StVal, b)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("invoke-ID isolation error: %v", err)
	}
}

// TestGoServer_MultiClient_DisconnectOneKeepsOthers verifies that when one
// client disconnects abruptly the remaining clients can still read successfully.
func TestGoServer_MultiClient_DisconnectOneKeepsOthers(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")

	c1 := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	c2 := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	c3 := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c2.Close(ctx)
	defer c3.Close(ctx)

	// Pre-flight: all three can read.
	for id, c := range []*iec61850.Client{c1, c2, c3} {
		if _, err := c.ReadRaw(ctx, stRef); err != nil {
			t.Fatalf("pre-flight client %d read: %v", id+1, err)
		}
	}

	// Client 1 closes abruptly.
	if err := c1.Close(ctx); err != nil {
		t.Logf("client 1 close: %v (non-fatal)", err)
	}

	// Allow the server a moment to process the disconnection.
	time.Sleep(50 * time.Millisecond)

	// Clients 2 and 3 must still be able to read.
	for id, c := range []*iec61850.Client{c2, c3} {
		raw, err := c.ReadRaw(ctx, stRef)
		if err != nil {
			t.Errorf("client %d read after client 1 disconnect: %v", id+2, err)
			continue
		}
		if b, ok := raw.Bool(); !ok || b != fixVal.SPS1StVal {
			t.Errorf("client %d: unexpected SPS1.stVal after disconnect: want %v, got %v (ok=%v)", id+2, fixVal.SPS1StVal, b, ok)
		}
	}
}
