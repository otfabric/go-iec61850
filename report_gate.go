// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
)

// respGate orders the reports of one association after the response to
// the confirmed request that is being processed.
//
// A report triggered by a request (RptEna=TRUE that replays the buffer,
// GI=TRUE, a client write of a data set member) is formatted while the
// request is handled, before go-mms sends its response. Peers such as
// libiec61850 drop a report that arrives while they wait for that
// response, so the delivery goroutine holds such a report until the
// response is out. go-mms serves one request at a time and reads the next
// only once it has sent the response (or failed to), so "the server is in
// Receive" is exactly "no response is pending".
//
// IEC 61850-7-2 does not require this order (a report is unconfirmed and
// independent of the SetxRCBValues response, §17.2.3.2.1); it keeps the
// peers working without a fixed delay.
type respGate struct {
	mu   sync.Mutex
	busy bool
	idle chan struct{} // closed while not busy
}

func newRespGate() *respGate {
	idle := make(chan struct{})
	close(idle)
	return &respGate{idle: idle}
}

// setBusy marks a request as being processed.
func (g *respGate) setBusy() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.busy {
		g.busy = true
		g.idle = make(chan struct{})
	}
}

// setIdle marks that no response is pending.
func (g *respGate) setIdle() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy {
		g.busy = false
		close(g.idle)
	}
}

// idleCh returns a channel that is closed once no response is pending. A
// nil gate (an association not served through [Server.Serve]) is always
// idle.
func (g *respGate) idleCh() <-chan struct{} {
	if g == nil {
		return closedChan
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.idle
}

var closedChan = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

// respGateKey carries the gate of an association in the request context.
type respGateKey struct{}

// respGateFrom returns the gate of the association that issued the
// request in ctx, or nil.
func respGateFrom(ctx context.Context) *respGate {
	g, _ := ctx.Value(respGateKey{}).(*respGate)
	return g
}

// gatedTransport is the transport of one association served by
// [Server.Serve]: it drives the association's respGate. It forwards the
// optional [mms.RemoteAddrTransport] and [mms.TLSTransport] methods,
// returning nil where the wrapped transport has none (what go-mms treats
// as "not available").
type gatedTransport struct {
	mms.Transport
	gate *respGate
}

// Receive implements [mms.Transport]: entering it means the previous
// request has been answered; data returned starts the next one.
func (t *gatedTransport) Receive(ctx context.Context) ([]byte, error) {
	t.gate.setIdle()
	data, err := t.Transport.Receive(ctx)
	if err == nil {
		t.gate.setBusy()
	}
	return data, err
}

// Close implements [mms.Transport]. No response follows a close, so a
// waiting report is released (its send then fails).
func (t *gatedTransport) Close() error {
	err := t.Transport.Close()
	t.gate.setIdle()
	return err
}

// RemoteAddr forwards [mms.RemoteAddrTransport].
func (t *gatedTransport) RemoteAddr() net.Addr {
	if ra, ok := t.Transport.(mms.RemoteAddrTransport); ok {
		return ra.RemoteAddr()
	}
	return nil
}

// TLSConnectionState forwards [mms.TLSTransport].
func (t *gatedTransport) TLSConnectionState() *tls.ConnectionState {
	if tt, ok := t.Transport.(mms.TLSTransport); ok {
		return tt.TLSConnectionState()
	}
	return nil
}

// Unwrap returns the transport of the association.
func (t *gatedTransport) Unwrap() mms.Transport { return t.Transport }

// gateTimeout bounds how long a report waits for a pending response: a
// request handler that blocks (a slow application callback) must not hold
// reporting forever. The report is sent after it regardless.
var gateTimeout = 5 * time.Second
