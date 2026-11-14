// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

// Metrics receives counters from publishers and subscribers. All
// methods must be safe for concurrent use and return quickly: they are
// called on the send and receive paths (per frame or per ASDU). Embed
// [NopMetrics] to implement only the hooks you need.
type Metrics interface {
	// FrameSent is called after a frame of asdus samples was handed to
	// the transport.
	FrameSent(svID string, asdus int)

	// SendError is called when a frame could not be encoded or the
	// transport rejected it.
	SendError(svID string, err error)

	// ASDUReceived is called for every received ASDU that matched a
	// subscription.
	ASDUReceived(svID string)

	// FrameDropped is called for a received frame discarded before
	// reaching a subscription. reason is "decode", "simulation",
	// "filtered", or "streams" (a subscription that supervises
	// SubscriptionConfig.MaxStreams streams dropped an ASDU of a new
	// stream).
	FrameDropped(reason string)

	// Event is called for every subscriber event, including events
	// dropped because the event queue was full (dropped == true).
	Event(svID string, kind EventKind, dropped bool)
}

// NopMetrics is a [Metrics] implementation that does nothing.
type NopMetrics struct{}

// FrameSent implements [Metrics].
func (NopMetrics) FrameSent(string, int) {}

// SendError implements [Metrics].
func (NopMetrics) SendError(string, error) {}

// ASDUReceived implements [Metrics].
func (NopMetrics) ASDUReceived(string) {}

// FrameDropped implements [Metrics].
func (NopMetrics) FrameDropped(string) {}

// Event implements [Metrics].
func (NopMetrics) Event(string, EventKind, bool) {}

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

func orDiscard(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(discardHandler{})
	}
	return l
}

// guardMetrics returns m (or [NopMetrics] when m is nil) wrapped so that
// a method that does not return normally is recovered: it is logged at
// error level with its stack and counted in recovered (when non-nil),
// and the caller goes on as if the method had returned.
func guardMetrics(m Metrics, logger *slog.Logger, recovered *atomic.Uint64) Metrics {
	if m == nil {
		return NopMetrics{}
	}
	return &guardedMetrics{m: m, logger: logger, recovered: recovered}
}

type guardedMetrics struct {
	m         Metrics
	logger    *slog.Logger
	recovered *atomic.Uint64
}

// done recovers a Metrics method that did not return normally. It is
// deferred by every method, so recover is called by the deferred
// function itself.
func (g *guardedMetrics) done(hook string) {
	if r := recover(); r != nil {
		if g.recovered != nil {
			g.recovered.Add(1)
		}
		g.logger.Error("sv: Metrics callback recovered", "hook", hook, "value", r, "stack", string(debug.Stack()))
	}
}

func (g *guardedMetrics) FrameSent(svID string, asdus int) {
	defer g.done("FrameSent")
	g.m.FrameSent(svID, asdus)
}

func (g *guardedMetrics) SendError(svID string, err error) {
	defer g.done("SendError")
	g.m.SendError(svID, err)
}

func (g *guardedMetrics) ASDUReceived(svID string) {
	defer g.done("ASDUReceived")
	g.m.ASDUReceived(svID)
}

func (g *guardedMetrics) FrameDropped(reason string) {
	defer g.done("FrameDropped")
	g.m.FrameDropped(reason)
}

func (g *guardedMetrics) Event(svID string, kind EventKind, dropped bool) {
	defer g.done("Event")
	g.m.Event(svID, kind, dropped)
}
