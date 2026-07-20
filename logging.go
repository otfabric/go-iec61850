// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"log/slog"
)

// discardHandler is a [slog.Handler] that discards all log records.
// Used as the default when no logger is configured.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

// discardLogger returns a logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(discardHandler{})
}
