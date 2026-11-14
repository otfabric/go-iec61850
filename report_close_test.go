// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// Lifecycle contract tests of ReportSubscription.Close and of the
// subscription cleanup of Client.Close and Client.Abort (TESTING.md,
// rows L1, L3, L9, L10, L11).

// quietTransport is the client end of an in-process transport whose peer
// can stop answering: after quiet is closed, nothing the peer sends is
// delivered, and the connection stays up.
type quietTransport struct {
	mms.Transport
	quiet chan struct{}
}

func (q *quietTransport) Receive(ctx context.Context) ([]byte, error) {
	for {
		b, err := q.Transport.Receive(ctx)
		if err != nil {
			return nil, err
		}
		select {
		case <-q.quiet:
			// Not delivered: the peer is silent from here on.
		default:
			return b, nil
		}
	}
}

// sharedAssociation serves one association of srv and returns its MMS
// client, the client end of the transport and a function that ends the
// association and the server side.
func sharedAssociation(t *testing.T, srv *Server) (*mms.Client, *quietTransport, func()) {
	t.Helper()
	sctx, cancel := context.WithCancel(context.Background())
	clientT, serverT := loopbackPair()
	qt := &quietTransport{Transport: clientT, quiet: make(chan struct{})}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(sctx, serverT)
	}()
	mc, err := mms.NewClient(context.Background(), qt, mms.DialOptions{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	end := func() {
		once.Do(func() {
			_ = mc.Abort(context.Background())
			cancel()
			<-served
		})
	}
	return mc, qt, end
}

func urcbState(t *testing.T, c *Client) (rptEna, resv bool) {
	t.Helper()
	rcb, err := c.GetReportControlBlock(waitCtx(t), "LD1", specURCB)
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}
	return rcb.RptEna, rcb.Resv
}

func reportsClosed(sub *ReportSubscription) bool {
	select {
	case _, ok := <-sub.Reports():
		return !ok
	default:
		return false
	}
}

var urcbLifecycle = SubscribeReportOptions{LD: "LD1", RCBItemID: specURCB, AutoEnable: true, ReserveURCB: true}

// Client.Close runs the cleanup of its subscriptions: the Reports channel
// is closed, the RCB the subscription enabled is disabled and the URCB it
// reserved is released, also when the association outlives the client
// (NewClient). A second Close returns nil and does nothing (L1).
func TestClientClose_DisablesAndReleasesRCB(t *testing.T) {
	srv, _ := newSpecServer(t)
	mc, _, end := sharedAssociation(t, srv)
	defer end()
	ctx := waitCtx(t)

	c1, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := c1.SubscribeReport(context.Background(), "rpt_urcb01", urcbLifecycle)
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	if ena, resv := urcbState(t, observer); !ena || !resv {
		t.Fatalf("after SubscribeReport: RptEna=%v Resv=%v, want both TRUE", ena, resv)
	}

	if err := c1.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !reportsClosed(sub) {
		t.Error("Reports is not closed after Client.Close")
	}
	if ena, resv := urcbState(t, observer); ena || resv {
		t.Errorf("after Client.Close: RptEna=%v Resv=%v, want both FALSE", ena, resv)
	}
	if err := c1.Close(ctx); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	if _, err := c1.GetReportControlBlock(ctx, "LD1", specURCB); err != ErrClosed {
		t.Errorf("request after Close = %v, want ErrClosed", err)
	}
}

// Client.Abort closes the subscriptions locally and sends nothing: the
// Reports channel is closed, the RCB keeps its state until the
// association ends.
func TestClientAbort_ClosesSubscriptionsLocally(t *testing.T) {
	srv, _ := newSpecServer(t)
	mc, _, end := sharedAssociation(t, srv)
	defer end()
	ctx := waitCtx(t)

	c1, _ := NewClient(mc, ClientOptions{})
	observer, _ := NewClient(mc, ClientOptions{})
	sub, err := c1.SubscribeReport(context.Background(), "rpt_urcb01", urcbLifecycle)
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	if err := c1.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if !reportsClosed(sub) {
		t.Error("Reports is not closed after Client.Abort")
	}
	if ena, resv := urcbState(t, observer); !ena || !resv {
		t.Errorf("after Client.Abort: RptEna=%v Resv=%v, want both unchanged (TRUE)", ena, resv)
	}
	if err := c1.Abort(ctx); err != nil {
		t.Errorf("second Abort = %v, want nil", err)
	}
}

// ReportSubscription.Close with a peer that has stopped answering: the
// local shutdown does not wait (Reports is closed while the remote
// cleanup is still pending) and the remote cleanup is given up after
// subscriptionCleanupTimeout, when Close returns. The time is the fake
// time of the synctest bubble.
func TestReportSubscriptionClose_QuietPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, err := NewServer(specModel(), ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		srv.EnableReports()
		defer srv.Close()
		mc, qt, end := sharedAssociation(t, srv)
		defer end()
		c, _ := NewClient(mc, ClientOptions{})
		sub, err := c.SubscribeReport(context.Background(), "rpt_urcb01", urcbLifecycle)
		if err != nil {
			t.Fatalf("SubscribeReport: %v", err)
		}

		close(qt.quiet) // the peer stops answering; the connection stays up
		start := time.Now()
		returned := make(chan time.Duration, 1)
		go func() {
			_ = sub.Close()
			returned <- time.Since(start)
		}()

		synctest.Wait() // Close waits for the answer to its first cleanup request
		if !reportsClosed(sub) {
			t.Error("Reports is not closed while the remote cleanup waits for the peer")
		}
		select {
		case d := <-returned:
			t.Fatalf("Close returned after %v without an answer from the peer, want %v", d, subscriptionCleanupTimeout)
		default:
		}

		time.Sleep(subscriptionCleanupTimeout) // fake time
		synctest.Wait()
		select {
		case d := <-returned:
			if d != subscriptionCleanupTimeout {
				t.Errorf("Close returned after %v, want %v", d, subscriptionCleanupTimeout)
			}
		default:
			t.Fatalf("Close has not returned %v after the peer stopped answering", subscriptionCleanupTimeout)
		}
		if err := sub.Close(); err != nil {
			t.Errorf("second Close = %v, want nil", err)
		}
	})
}

// The subscription cleanup of Client.Close with a peer that has stopped
// answering ends with the context Close got, and after
// subscriptionCleanupTimeout at the latest.
func TestClientClose_QuietPeerBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration // of the context given to Close; 0: none
		want    time.Duration
	}{
		{"context ends first", time.Second, time.Second},
		{"context ends at the limit", subscriptionCleanupTimeout, subscriptionCleanupTimeout},
		{"context ends later", time.Minute, subscriptionCleanupTimeout},
		{"no deadline", 0, subscriptionCleanupTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv, err := NewServer(specModel(), ServerOptions{})
				if err != nil {
					t.Fatal(err)
				}
				srv.EnableReports()
				defer srv.Close()
				mc, qt, end := sharedAssociation(t, srv)
				defer end()
				c, _ := NewClient(mc, ClientOptions{})
				subs := make([]*ReportSubscription, 2)
				for i, o := range []SubscribeReportOptions{urcbLifecycle, {LD: "LD1", RCBItemID: specBRCB, AutoEnable: true}} {
					rptID := []string{"rpt_urcb01", "rpt_brcb01"}[i]
					if subs[i], err = c.SubscribeReport(context.Background(), rptID, o); err != nil {
						t.Fatalf("SubscribeReport %s: %v", rptID, err)
					}
				}

				close(qt.quiet)
				ctx := context.Background()
				if tc.timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.timeout)
					defer cancel()
				}
				start := time.Now()
				if err := c.Close(ctx); err != nil {
					t.Errorf("Close: %v", err)
				}
				if got := time.Since(start); got != tc.want {
					t.Errorf("Close took %v, want %v", got, tc.want)
				}
				for _, sub := range subs {
					if !reportsClosed(sub) {
						t.Errorf("Reports of %s is not closed after Client.Close", sub.rptID)
					}
				}
			})
		})
	}
}

// localSubscription is a registered subscription of a client without an
// association: reports are handed to it with deliver, as the client's
// dispatch does, and nothing is sent.
func localSubscription(t *testing.T, rptID string, queue int, policy OverflowPolicy) *ReportSubscription {
	t.Helper()
	c := &Client{logger: discardLogger()}
	c.reportOnce.Do(func() { c.reportSubs = make(map[string][]*ReportSubscription) })
	sub := &ReportSubscription{
		ch:       make(chan *ReportIndication, queue),
		done:     make(chan struct{}),
		client:   c,
		rptID:    rptID,
		overflow: policy,
	}
	if !c.registerSubscription(rptID, sub) {
		t.Fatal("registerSubscription refused")
	}
	return sub
}

// OnOverflow runs without the subscription's lock: it may call Close
// (L10) and other methods of its subscription (L9). After the Close the
// Reports channel is closed and a further report is not delivered.
func TestReportOnOverflow_MayCloseItsSubscription(t *testing.T) {
	sub := localSubscription(t, "rptOverflow", 1, OverflowCallback)
	calls := 0
	sub.onOverflow = func(ri *ReportIndication) {
		calls++
		if ri.SeqNum != 2 {
			t.Errorf("OnOverflow got SeqNum %d, want 2", ri.SeqNum)
		}
		_ = sub.Reports() // another method of the subscription
		if err := sub.Close(); err != nil {
			t.Errorf("Close from OnOverflow = %v", err)
		}
	}

	sub.deliver(&ReportIndication{RptID: "rptOverflow", SeqNum: 1}, discardLogger()) // fills the queue
	done := make(chan struct{})
	go func() { // stands for the goroutine that dispatches reports
		defer close(done)
		sub.deliver(&ReportIndication{RptID: "rptOverflow", SeqNum: 2}, discardLogger()) // overflow
	}()
	select {
	case <-done:
	case <-waitCtx(t).Done():
		t.Fatal("deliver did not return: OnOverflow is blocked in Close")
	}
	if calls != 1 {
		t.Fatalf("OnOverflow called %d times, want 1", calls)
	}
	if ri, ok := <-sub.Reports(); !ok || ri.SeqNum != 1 {
		t.Errorf("first receive = %+v, %v; want the queued report 1", ri, ok)
	}
	if _, ok := <-sub.Reports(); ok {
		t.Error("Reports is not closed after Close from OnOverflow")
	}
	sub.deliver(&ReportIndication{RptID: "rptOverflow", SeqNum: 3}, discardLogger()) // no-op
	if calls != 1 {
		t.Errorf("OnOverflow called for a report after Close (%d calls)", calls)
	}
	if err := sub.Close(); err != nil {
		t.Errorf("Close after Close from OnOverflow = %v, want nil", err)
	}
	sub.client.reportMu.RLock()
	registered := len(sub.client.reportSubs)
	sub.client.reportMu.RUnlock()
	if registered != 0 {
		t.Errorf("%d subscriptions registered after Close, want 0", registered)
	}
}

// An OnOverflow that ends abnormally is recovered and logged; the
// subscription keeps working and its lock is free.
func TestReportOnOverflow_CallbackIsRecovered(t *testing.T) {
	sub := localSubscription(t, "rptOverflow", 1, OverflowCallback)
	sub.onOverflow = func(*ReportIndication) { panic("boom") }
	sub.deliver(&ReportIndication{RptID: "rptOverflow", SeqNum: 1}, discardLogger())
	sub.deliver(&ReportIndication{RptID: "rptOverflow", SeqNum: 2}, discardLogger()) // overflow: the callback ends abnormally
	if ri := <-sub.Reports(); ri.SeqNum != 1 {
		t.Errorf("received SeqNum %d, want 1", ri.SeqNum)
	}
	sub.deliver(&ReportIndication{RptID: "rptOverflow", SeqNum: 3}, discardLogger())
	if ri := <-sub.Reports(); ri.SeqNum != 3 {
		t.Errorf("received SeqNum %d after the recovered callback, want 3", ri.SeqNum)
	}
	if err := sub.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}

// OverflowBlock with a full queue blocks the goroutine that dispatches
// the report, which is the one that reads the association (L11); Close
// releases it (L3). The blocked state is observed with synctest, not
// with a wait.
func TestOverflowBlock_FullQueueBlocksDispatchUntilClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sub := localSubscription(t, "rptBlock", 1, OverflowBlock)
		sub.deliver(&ReportIndication{RptID: "rptBlock", SeqNum: 1}, discardLogger()) // fills the queue

		dispatched := make(chan struct{})
		go func() { // stands for the goroutine that reads the association
			defer close(dispatched)
			sub.deliver(&ReportIndication{RptID: "rptBlock", SeqNum: 2}, discardLogger())
		}()
		synctest.Wait()
		select {
		case <-dispatched:
			t.Fatal("deliver returned with a full queue")
		default: // blocked: nothing else is dispatched or answered meanwhile
		}

		if err := sub.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		synctest.Wait()
		select {
		case <-dispatched:
		default:
			t.Fatal("deliver is still blocked after Close")
		}
		if ri, ok := <-sub.Reports(); !ok || ri.SeqNum != 1 {
			t.Errorf("first receive = %+v, %v; want the queued report 1", ri, ok)
		}
		if _, ok := <-sub.Reports(); ok {
			t.Error("Reports is not closed after Close")
		}
	})
}
