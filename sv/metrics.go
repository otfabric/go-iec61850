// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"log/slog"
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
	// reaching a subscription. reason is "decode", "simulation" or
	// "filtered".
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

func orNop(m Metrics) Metrics {
	if m == nil {
		return NopMetrics{}
	}
	return m
}
