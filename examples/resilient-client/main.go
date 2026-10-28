// SPDX-License-Identifier: MIT

// Command resilient-client is a long-running IEC 61850 client that
// survives server restarts and network outages.
//
// It shows the client options of a production poller:
//
//   - DialOptions.Retry: connection attempts with exponential backoff
//     and jitter (mms.DefaultRetryPolicy); errors that another attempt
//     cannot fix, such as a rejected association, fail at once.
//   - DialOptions.Logger: structured logging with log/slog (JSON).
//   - DialOptions.Metrics: an mms.Metrics implementation counting
//     requests, errors and association changes, printed at exit.
//
// It polls one attribute every -interval. When the connection is lost
// it closes the client and dials again with the retry policy; other
// errors (a missing attribute, an access error) are logged and polling
// continues.
//
// Usage:
//
//	resilient-client [-addr 127.0.0.1:102] [-ref LD1/MMXU1.PhV.phsA.cVal.mag.f[MX]] [-interval 1s]
//
// Run it against examples/ied-server and restart the server to see the
// reconnects.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ref := flag.String("ref", "LD1/MMXU1.PhV.phsA.cVal.mag.f[MX]", "attribute to poll")
	interval := flag.Duration("interval", time.Second, "poll interval")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var m counters
	cfg := config{Addr: *addr, Ref: *ref, Interval: *interval, Retry: mms.DefaultRetryPolicy(), Logger: logger, Metrics: &m}
	err := run(ctx, cfg, os.Stdout)
	m.print(os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
}

// counters is an mms.Metrics that counts events. A real application
// would export them to Prometheus or OpenTelemetry (see go-mms
// OBSERVABILITY.md). Embedding NopMetrics keeps it compiling when
// go-mms adds methods.
type counters struct {
	mms.NopMetrics
	requests, failed, established, closed atomic.Int64
}

func (c *counters) ObserveRequest(ev mms.RequestEvent) {
	c.requests.Add(1)
	if ev.Err != nil {
		c.failed.Add(1)
	}
}

func (c *counters) ObserveConnection(ev mms.ConnectionEvent) {
	switch ev.State {
	case mms.ConnectionEstablished:
		c.established.Add(1)
	case mms.ConnectionClosed:
		c.closed.Add(1)
	default: // rejected associations show up as failed dials
	}
}

func (c *counters) print(out io.Writer) {
	fmt.Fprintf(out, "metrics: %d requests (%d failed), %d associations established, %d closed\n",
		c.requests.Load(), c.failed.Load(), c.established.Load(), c.closed.Load())
}

type config struct {
	Addr     string
	Ref      string
	Interval time.Duration
	Retry    mms.RetryPolicy
	Logger   *slog.Logger
	Metrics  mms.Metrics
	Polls    int // stop after this many successful reads; 0 means until ctx is done
}

func run(ctx context.Context, cfg config, out io.Writer) error {
	ref, err := iec61850.ParseRef(cfg.Ref)
	if err != nil {
		return err
	}
	retry := cfg.Retry
	retry.MaxAttempts = -1 // a poller keeps trying until it is stopped
	retry.OnRetry = func(attempt int, err error, delay time.Duration) {
		fmt.Fprintf(out, "connect attempt %d failed (%v), next in %v\n", attempt, err, delay.Round(time.Millisecond))
	}
	opts := iec61850.DialOptions{Retry: retry, Logger: cfg.Logger, Metrics: cfg.Metrics}

	var c *iec61850.Client
	defer func() {
		if c != nil {
			_ = c.Close(context.Background())
		}
	}()
	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	polls := 0
	for {
		if c == nil {
			// Dial retries per the policy until it connects, gives up
			// (an error IsRetryable rejects) or ctx is done.
			c, err = iec61850.Dial(ctx, cfg.Addr, opts)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("connect: %w", err)
			}
			fmt.Fprintf(out, "connected to %s\n", cfg.Addr)
		}

		v, err := c.Read(ctx, ref)
		switch {
		case err == nil:
			fmt.Fprintf(out, "%s = %v\n", ref, v)
			polls++
			if cfg.Polls > 0 && polls >= cfg.Polls {
				return nil
			}
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, iec61850.ErrClosed) || mms.IsRetryable(err):
			// The association is gone: reconnect.
			fmt.Fprintf(out, "connection lost: %v\n", err)
			_ = c.Close(context.Background())
			c = nil
			continue
		default:
			// The connection is fine; the request failed.
			fmt.Fprintf(out, "read %s: %v\n", ref, err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
