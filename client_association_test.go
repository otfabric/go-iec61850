// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// associationClient connects a Client to srv over an in-memory transport
// and returns it with the server end of the transport.
func associationClient(t *testing.T, srv *Server) (*Client, mms.Transport) {
	t.Helper()
	sctx, cancel := context.WithCancel(context.Background())
	clientT, serverT := loopbackPair(t)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(sctx, serverT)
	}()
	mc, err := mms.NewClient(context.Background(), clientT, mms.DialOptions{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	c, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c.ownsClient = true // as Dial: Close and Abort end the association
	t.Cleanup(func() {
		_ = c.Abort(context.Background())
		cancel()
		<-served
	})
	return c, serverT
}

// closedWithin reports whether ch is closed within a second of fake
// time.
func closedWithin(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(time.Second):
		return false
	}
}

// reportsEnd reports whether sub's Reports channel is closed within d of
// fake time, reading the reports still queued.
func reportsEnd(sub *ReportSubscription, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case _, ok := <-sub.Reports():
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// When the association ends, by the server's Shutdown or by the loss of
// the connection, the report subscriptions are closed: a consumer ranging
// over Reports stops. Done is closed, Err matches ErrClosed and
// mms.ErrClosed, a new subscription is refused with ErrClosed, and Close
// returns without waiting for the server. The times are the fake time of
// the synctest bubble.
func TestReportSubscription_ClosedWhenAssociationEnds(t *testing.T) {
	ends := map[string]func(srv *Server, serverEnd mms.Transport) error{
		"server shutdown": func(srv *Server, _ mms.Transport) error { return srv.Shutdown(context.Background()) },
		"connection lost": func(_ *Server, serverEnd mms.Transport) error { return serverEnd.Close() },
	}
	for name, end := range ends {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv, _ := newSpecServer(t)
				c, serverEnd := associationClient(t, srv)
				ctx := context.Background()
				sub, err := c.SubscribeReport(ctx, "rpt_urcb01", urcbLifecycle)
				if err != nil {
					t.Fatalf("SubscribeReport: %v", err)
				}
				passive, err := c.SubscribeReport(ctx, "other", SubscribeReportOptions{})
				if err != nil {
					t.Fatalf("SubscribeReport: %v", err)
				}
				if closedWithin(c.Done()) || c.Err() != nil {
					t.Fatalf("Done closed or Err %v while the association is up", c.Err())
				}

				if err := end(srv, serverEnd); err != nil {
					t.Fatalf("ending the association: %v", err)
				}
				if !reportsEnd(sub, time.Minute) {
					t.Fatal("Reports is still open a minute after the association ended")
				}
				if !reportsEnd(passive, time.Second) {
					t.Error("Reports of the passive subscription is still open after the association ended")
				}
				if !closedWithin(c.Done()) {
					t.Fatal("Done is not closed after the association ended")
				}
				if err := c.Err(); !errors.Is(err, ErrClosed) || !errors.Is(err, mms.ErrClosed) {
					t.Errorf("Err() = %v, want ErrClosed and mms.ErrClosed", err)
				}
				if _, err := c.SubscribeReport(ctx, "again", SubscribeReportOptions{}); !errors.Is(err, ErrClosed) {
					t.Errorf("SubscribeReport after the association ended = %v, want ErrClosed", err)
				}
				closed := make(chan struct{})
				go func() {
					defer close(closed)
					_ = c.Close(context.Background())
				}()
				if !closedWithin(closed) {
					t.Error("Close after the association ended waits")
				}
			})
		})
	}
}

// Done and Err follow the association of the client; a Client that has
// not subscribed starts no goroutine of its own, and Close ends the one a
// subscription started.
func TestClient_DoneAndErr(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, _ := newSpecServer(t)
		c, _ := associationClient(t, srv)
		if closedWithin(c.Done()) || c.Err() != nil {
			t.Fatalf("Done closed or Err %v while the association is up", c.Err())
		}
		if c.watchStop != nil {
			t.Error("a client without a subscription watches its association")
		}
		sub, err := c.SubscribeReport(context.Background(), "rpt", SubscribeReportOptions{})
		if err != nil {
			t.Fatal(err)
		}
		done := c.watchDone
		if err := c.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if !closedWithin(done) {
			t.Error("the association watch has not ended after Close")
		}
		if !reportsEnd(sub, time.Second) {
			t.Error("Reports is open after Close")
		}
		if !closedWithin(c.Done()) {
			t.Fatal("Done is not closed after Close of a client that owns its association")
		}
		if err := c.Err(); !errors.Is(err, ErrClosed) || !errors.Is(err, mms.ErrClosed) {
			t.Errorf("Err() after Close = %v, want ErrClosed and mms.ErrClosed", err)
		}
		if c.Err().Error() == "" {
			t.Error("Err() has no text")
		}
	})
}
