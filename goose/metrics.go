// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

// Metrics receives counters from publishers and subscribers. All
// methods must be safe for concurrent use and should return quickly;
// they are called on the send and receive paths. Embed [NopMetrics] to
// implement only the hooks you need.
type Metrics interface {
	// FrameSent is called after a frame has been handed to the
	// transport. retransmission is false for the first frame of a new
	// state (sqNum 0).
	FrameSent(gocbRef string, retransmission bool)

	// SendError is called when the transport rejected a frame.
	SendError(gocbRef string, err error)

	// FrameReceived is called for every decoded frame that matched at
	// least one subscription.
	FrameReceived(gocbRef string)

	// FrameDropped is called for a received frame that was discarded
	// before reaching a subscription. reason is one of "decode",
	// "simulation", "filtered", or "streams" (a subscription that
	// supervises SubscriptionConfig.MaxStreams streams dropped the frame
	// of a new stream).
	FrameDropped(reason string)

	// Event is called for every subscriber event, including events
	// dropped because the event queue was full (dropped == true).
	Event(gocbRef string, kind EventKind, dropped bool)
}

// NopMetrics is a [Metrics] implementation that does nothing.
type NopMetrics struct{}

// FrameSent implements [Metrics].
func (NopMetrics) FrameSent(string, bool) {}

// SendError implements [Metrics].
func (NopMetrics) SendError(string, error) {}

// FrameReceived implements [Metrics].
func (NopMetrics) FrameReceived(string) {}

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
		g.logger.Error("goose: Metrics callback recovered", "hook", hook, "value", r, "stack", string(debug.Stack()))
	}
}

func (g *guardedMetrics) FrameSent(gocbRef string, retransmission bool) {
	defer g.done("FrameSent")
	g.m.FrameSent(gocbRef, retransmission)
}

func (g *guardedMetrics) SendError(gocbRef string, err error) {
	defer g.done("SendError")
	g.m.SendError(gocbRef, err)
}

func (g *guardedMetrics) FrameReceived(gocbRef string) {
	defer g.done("FrameReceived")
	g.m.FrameReceived(gocbRef)
}

func (g *guardedMetrics) FrameDropped(reason string) {
	defer g.done("FrameDropped")
	g.m.FrameDropped(reason)
}

func (g *guardedMetrics) Event(gocbRef string, kind EventKind, dropped bool) {
	defer g.done("Event")
	g.m.Event(gocbRef, kind, dropped)
}
