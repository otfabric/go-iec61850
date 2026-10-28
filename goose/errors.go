// SPDX-License-Identifier: MIT

package goose

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the goose package.
//
// Use [errors.Is] to test against these values. Decoder failures are
// returned as [*DecodeError] values that unwrap to one of the decode
// sentinels below.
var (
	// ErrInvalidFrame indicates an Ethernet frame that cannot carry a
	// GOOSE message: too short, wrong EtherType, or a malformed
	// 802.1Q tag.
	ErrInvalidFrame = errors.New("goose: invalid frame")

	// ErrNotGOOSE indicates a well-formed Ethernet frame whose
	// EtherType is not 0x88B8.
	ErrNotGOOSE = errors.New("goose: not a GOOSE frame")

	// ErrInvalidLength indicates that the GOOSE header length field is
	// inconsistent with the frame or the encoded PDU.
	ErrInvalidLength = errors.New("goose: invalid length")

	// ErrInvalidPDU indicates a BER structure error inside the
	// goosePdu: bad tag, bad length, missing mandatory field,
	// duplicate or out-of-order field, or an out-of-range value.
	ErrInvalidPDU = errors.New("goose: invalid PDU")

	// ErrUnsupportedPDU indicates a GOOSE APDU choice other than
	// goosePdu (for example a GSE management PDU).
	ErrUnsupportedPDU = errors.New("goose: unsupported PDU")

	// ErrInvalidData indicates a malformed or unsupported MMS Data
	// element inside allData.
	ErrInvalidData = errors.New("goose: invalid data")

	// ErrLimitExceeded indicates that a decoder or encoder limit was
	// exceeded (nesting depth, entry count, frame size).
	ErrLimitExceeded = errors.New("goose: limit exceeded")

	// ErrInvalidConfig indicates an invalid publisher, subscriber or
	// transport configuration.
	ErrInvalidConfig = errors.New("goose: invalid configuration")

	// ErrInvalidValue indicates a value that cannot be encoded in a
	// GOOSE message (nil element, unsupported type, out of range).
	ErrInvalidValue = errors.New("goose: invalid value")

	// ErrClosed indicates an operation on a closed publisher,
	// subscriber or transport.
	ErrClosed = errors.New("goose: closed")

	// ErrUnsupportedPlatform indicates that no raw Ethernet transport
	// is available on this operating system.
	ErrUnsupportedPlatform = errors.New("goose: raw Ethernet not supported on this platform")

	// ErrPermission indicates that opening a raw Ethernet socket was
	// denied (on Linux CAP_NET_RAW is required; on macOS/BSD read and
	// write access to /dev/bpf*).
	ErrPermission = errors.New("goose: permission denied")

	// ErrInterfaceNotFound indicates that the named network interface
	// does not exist.
	ErrInterfaceNotFound = errors.New("goose: interface not found")

	// ErrSCL indicates that a GOOSE control block could not be built
	// from an SCL document.
	ErrSCL = errors.New("goose: SCL binding error")

	// ErrNotFound indicates that a requested control block, dataset
	// or stream does not exist.
	ErrNotFound = errors.New("goose: not found")

	// ErrValidation is the base sentinel for subscriber validation
	// failures. [*ValidationError] unwraps to it and to the more
	// specific sentinels below.
	ErrValidation = errors.New("goose: validation failed")

	// ErrConfRevMismatch indicates a received confRev that differs from
	// the expected configuration revision.
	ErrConfRevMismatch = errors.New("goose: confRev mismatch")

	// ErrDatSetMismatch indicates a received datSet reference that
	// differs from the expected one.
	ErrDatSetMismatch = errors.New("goose: datSet mismatch")

	// ErrGoIDMismatch indicates a received goID that differs from the
	// expected one.
	ErrGoIDMismatch = errors.New("goose: goID mismatch")

	// ErrMemberMismatch indicates that numDatSetEntries, the number of
	// allData elements, or the type of an element does not match the
	// expected dataset layout.
	ErrMemberMismatch = errors.New("goose: dataset member mismatch")

	// ErrNeedsCommissioning indicates a received message with ndsCom
	// set.
	ErrNeedsCommissioning = errors.New("goose: publisher needs commissioning")
)

// DecodeError describes a strict-decoding failure. Offset is the byte
// offset in the frame (or in the buffer passed to [DecodePDU]) where
// the problem was detected; Field names the element being decoded.
type DecodeError struct {
	Field   string
	Offset  int
	Message string
	Err     error // one of the decode sentinels
}

func (e *DecodeError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%v: %s at offset %d: %s", e.Err, e.Field, e.Offset, e.Message)
	}
	return fmt.Sprintf("%v at offset %d: %s", e.Err, e.Offset, e.Message)
}

func (e *DecodeError) Unwrap() error { return e.Err }

func decodeErr(sentinel error, field string, off int, format string, args ...any) error {
	return &DecodeError{Field: field, Offset: off, Message: fmt.Sprintf(format, args...), Err: sentinel}
}

// ValidationError describes a received message that failed subscriber
// validation. It unwraps to [ErrValidation] and to the specific
// sentinel in Err ([ErrConfRevMismatch], [ErrDatSetMismatch],
// [ErrGoIDMismatch], [ErrMemberMismatch] or [ErrNeedsCommissioning]).
type ValidationError struct {
	GocbRef string
	Field   string
	Want    string
	Got     string
	Err     error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("goose: %s: %s: want %s, got %s", e.GocbRef, e.Field, e.Want, e.Got)
}

func (e *ValidationError) Unwrap() []error { return []error{ErrValidation, e.Err} }

// ConfigError describes an invalid configuration field. It unwraps to
// [ErrInvalidConfig].
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("goose: invalid configuration: %s: %s", e.Field, e.Message)
}

func (e *ConfigError) Unwrap() error { return ErrInvalidConfig }

func configErr(field, format string, args ...any) error {
	return &ConfigError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// TransportError wraps an operating-system error from a raw Ethernet
// backend. It unwraps to the underlying error and, where applicable,
// to [ErrPermission], [ErrInterfaceNotFound] or [ErrClosed].
type TransportError struct {
	Op        string
	Interface string
	Err       error
	Kind      error // optional classification sentinel
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("goose: %s %s: %v", e.Op, e.Interface, e.Err)
}

func (e *TransportError) Unwrap() []error {
	if e.Kind != nil {
		return []error{e.Kind, e.Err}
	}
	return []error{e.Err}
}

func errValue(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidValue}, args...)...)
}

func errLimit(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrLimitExceeded}, args...)...)
}
