// SPDX-License-Identifier: MIT

package l2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Sentinel errors returned by the l2 package. They are shared with the
// goose and sv packages (goose.ErrClosed == l2.ErrClosed, and so on), so
// one errors.Is test covers the transport and the protocol layer.
var (
	// ErrClosed indicates an operation on a closed transport (or a
	// closed publisher, subscriber or demultiplexer port).
	ErrClosed = errors.New("closed")

	// ErrUnsupportedPlatform indicates that no raw Ethernet transport
	// is available on this operating system.
	ErrUnsupportedPlatform = errors.New("raw Ethernet not supported on this platform")

	// ErrPermission indicates that opening a raw Ethernet socket was
	// denied (on Linux CAP_NET_RAW is required; on macOS/BSD read and
	// write access to /dev/bpf*).
	ErrPermission = errors.New("raw Ethernet: permission denied")

	// ErrInterfaceNotFound indicates that the named network interface
	// does not exist.
	ErrInterfaceNotFound = errors.New("raw Ethernet: interface not found")

	// ErrInvalidConfig indicates an invalid configuration value, such
	// as a malformed MAC address or EtherType list.
	ErrInvalidConfig = errors.New("invalid configuration")
)

// ConfigError describes an invalid configuration field. It unwraps to
// [ErrInvalidConfig].
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("l2: invalid configuration: %s: %s", e.Field, e.Message)
}

func (e *ConfigError) Unwrap() error { return ErrInvalidConfig }

// TransportError wraps an operating-system error from a raw Ethernet
// backend. It unwraps to the underlying error and, where applicable,
// to [ErrPermission], [ErrInterfaceNotFound], [ErrUnsupportedPlatform]
// or [ErrClosed].
type TransportError struct {
	Op        string
	Interface string
	Err       error
	Kind      error // optional classification sentinel
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("l2: %s %s: %v", e.Op, e.Interface, e.Err)
}

func (e *TransportError) Unwrap() []error {
	if e.Kind != nil {
		return []error{e.Kind, e.Err}
	}
	return []error{e.Err}
}

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
