// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// startServer serves the demo IED on addr until the returned stop.
func startServer(t *testing.T, addr string) (stop func()) {
	t.Helper()
	srv, err := demoied.NewServer(iec61850.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := iso.Listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.ListenAndServe(ctx, ln) }()
	return func() {
		cancel()
		srv.Close()
		<-done
	}
}

// TestRun starts the client before the server (dial retries), then
// restarts the server (reconnect) and checks the metrics.
func TestRun(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	var out demoied.Output
	var m counters
	retry := mms.RetryPolicy{InitialBackoff: 20 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	cfg := config{
		Addr: addr, Ref: demoied.LD + "/MMXU1.PhV.phsA.cVal.mag.f[MX]", Interval: 10 * time.Millisecond,
		Retry: retry, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: &m,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, &out) }()

	out.WaitFor(t, "connect attempt 2 failed", 5*time.Second)
	stop := startServer(t, addr)
	out.WaitFor(t, "connected to", 5*time.Second)
	out.WaitFor(t, "MMXU1.PhV.phsA.cVal.mag.f[MX] = 0", 5*time.Second)

	stop()
	out.WaitFor(t, "connection lost", 5*time.Second)
	stop = startServer(t, addr)
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for m.established.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no reconnect:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.requests.Load() == 0 || m.closed.Load() == 0 {
		t.Errorf("metrics: %d requests, %d closed", m.requests.Load(), m.closed.Load())
	}
}
