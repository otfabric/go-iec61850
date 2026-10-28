// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"log/slog"
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
	// "simulation", "filtered".
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

func orNop(m Metrics) Metrics {
	if m == nil {
		return NopMetrics{}
	}
	return m
}
