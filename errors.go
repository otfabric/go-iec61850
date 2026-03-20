package iec61850

import (
	"errors"
	"fmt"
)

// Sentinel errors for major IEC 61850 failure categories.
//
// Use [errors.Is] to test against these values.
var (
	// ErrInvalidReference indicates a syntactically invalid IEC 61850
	// object reference.
	ErrInvalidReference = errors.New("iec61850: invalid reference")

	// ErrInvalidFunctionalConstraint indicates an unrecognized
	// functional constraint value.
	ErrInvalidFunctionalConstraint = errors.New("iec61850: invalid functional constraint")

	// ErrNotFound indicates that the requested IEC 61850 object does
	// not exist on the server.
	ErrNotFound = errors.New("iec61850: not found")

	// ErrTypeMismatch indicates that a value's type does not match the
	// expected IEC 61850 type.
	ErrTypeMismatch = errors.New("iec61850: type mismatch")

	// ErrUnsupportedService indicates that the requested service is not
	// supported by the server or this library.
	ErrUnsupportedService = errors.New("iec61850: unsupported service")

	// ErrSubscriptionClosed indicates that a report subscription has
	// been closed.
	ErrSubscriptionClosed = errors.New("iec61850: subscription closed")

	// ErrSCLParse indicates a failure to parse an SCL file.
	ErrSCLParse = errors.New("iec61850: SCL parse error")

	// ErrModelMismatch indicates a mismatch between the expected and
	// actual data model.
	ErrModelMismatch = errors.New("iec61850: model mismatch")

	// ErrUnsupportedCDC indicates an unsupported Common Data Class.
	ErrUnsupportedCDC = errors.New("iec61850: unsupported CDC")

	// ErrReportDecode indicates a failure to decode a report payload.
	ErrReportDecode = errors.New("iec61850: report decode error")

	// ErrDatasetDecode indicates a failure to decode dataset contents.
	ErrDatasetDecode = errors.New("iec61850: dataset decode error")

	// ErrClosed indicates that the client connection has been closed.
	ErrClosed = errors.New("iec61850: connection closed")

	// ErrDataAccess indicates a per-variable data access failure
	// returned by the server for an individual read or write.
	ErrDataAccess = errors.New("iec61850: data access error")

	// ErrInvalidArgument indicates that a caller-supplied argument is
	// invalid (empty required field, nil value, etc.). Use [errors.Is]
	// to distinguish argument errors from server-side failures.
	ErrInvalidArgument = errors.New("iec61850: invalid argument")

	// ErrProtocol indicates a protocol-level mismatch between the
	// client and server, such as an unexpected response count or
	// missing mandatory fields in a response.
	ErrProtocol = errors.New("iec61850: protocol error")

	// ErrControlFailed indicates that a control operation (operate,
	// select, cancel) was rejected or failed.
	ErrControlFailed = errors.New("iec61850: control failed")

	// ErrSelectFailed indicates that a select or select-with-value
	// request was denied by the server.
	ErrSelectFailed = errors.New("iec61850: select failed")

	// ErrOperateFailed indicates that an operate request was denied
	// or could not be executed.
	ErrOperateFailed = errors.New("iec61850: operate failed")

	// ErrCancelFailed indicates that a cancel request was denied
	// or could not be executed.
	ErrCancelFailed = errors.New("iec61850: cancel failed")

	// ErrNotControllable indicates that the target data object's
	// ctlModel is status-only and does not support control.
	ErrNotControllable = errors.New("iec61850: not controllable")
)

// ReferenceError is a typed error for malformed or invalid IEC 61850
// object references.
type ReferenceError struct {
	Input   string
	Reason  string
	Wrapped error
}

func (e *ReferenceError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("iec61850: invalid reference %q: %s: %v", e.Input, e.Reason, e.Wrapped)
	}
	return fmt.Sprintf("iec61850: invalid reference %q: %s", e.Input, e.Reason)
}

func (e *ReferenceError) Unwrap() error {
	if e.Wrapped != nil {
		return e.Wrapped
	}
	return ErrInvalidReference
}

// DecodeError indicates a failure during semantic decoding of an
// MMS value into an IEC 61850 type.
type DecodeError struct {
	Ref     string
	Type    string
	Message string
	Wrapped error
}

func (e *DecodeError) Error() string {
	var prefix string
	if e.Ref != "" {
		prefix = fmt.Sprintf("iec61850: decode error for %q (type %s)", e.Ref, e.Type)
	} else {
		prefix = fmt.Sprintf("iec61850: decode error (type %s)", e.Type)
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %v", prefix, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", prefix, e.Message)
}

func (e *DecodeError) Unwrap() error {
	if e.Wrapped != nil {
		return e.Wrapped
	}
	return ErrTypeMismatch
}

// ModelError indicates a mismatch or inconsistency in the IEC 61850
// data model.
type ModelError struct {
	Ref     string
	Message string
	Wrapped error
}

func (e *ModelError) Error() string {
	prefix := "iec61850: model error"
	if e.Ref != "" {
		prefix = fmt.Sprintf("iec61850: model error at %q", e.Ref)
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %v", prefix, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", prefix, e.Message)
}

func (e *ModelError) Unwrap() error {
	if e.Wrapped != nil {
		return e.Wrapped
	}
	return ErrModelMismatch
}

// ReportError indicates a failure related to report control block
// operations or report decoding.
type ReportError struct {
	RCBRef  string
	Message string
	Wrapped error
}

func (e *ReportError) Error() string {
	prefix := "iec61850: report error"
	if e.RCBRef != "" {
		prefix = fmt.Sprintf("iec61850: report error for %q", e.RCBRef)
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %v", prefix, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", prefix, e.Message)
}

func (e *ReportError) Unwrap() error {
	if e.Wrapped != nil {
		return e.Wrapped
	}
	return ErrReportDecode
}

// SCLParseError indicates a failure during SCL file parsing.
type SCLParseError struct {
	File    string
	Line    int
	Message string
	Wrapped error
}

func (e *SCLParseError) Error() string {
	prefix := "iec61850: SCL parse error"
	if e.File != "" && e.Line > 0 {
		prefix = fmt.Sprintf("iec61850: SCL parse error in %s:%d", e.File, e.Line)
	} else if e.File != "" {
		prefix = fmt.Sprintf("iec61850: SCL parse error in %s", e.File)
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %v", prefix, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", prefix, e.Message)
}

func (e *SCLParseError) Unwrap() error {
	if e.Wrapped != nil {
		return e.Wrapped
	}
	return ErrSCLParse
}

// DataAccessError is a typed per-item error for bulk read/write
// operations. It wraps the MMS-level error code into an IEC 61850
// error that supports [errors.Is] with [ErrDataAccess].
type DataAccessError struct {
	Ref       string
	ErrorCode int
	Operation string
}

func (e *DataAccessError) Error() string {
	return fmt.Sprintf("iec61850: %s %s: data access error (code %d)", e.Operation, e.Ref, e.ErrorCode)
}

func (e *DataAccessError) Unwrap() error {
	return ErrDataAccess
}

// ControlError is a typed error for control operation failures.
// It captures the object reference, the operation attempted, and
// the server-reported additional cause when available.
type ControlError struct {
	// Ref is the IEC 61850 object reference of the controlled object.
	Ref string

	// Operation is the control operation that failed (e.g. "operate",
	// "select", "cancel").
	Operation string

	// AddCause is the additional cause reported by the server via
	// LastApplError, if available. Zero means unknown or not read.
	AddCause AddCause

	// Wrapped is the underlying error.
	Wrapped error
}

func (e *ControlError) Error() string {
	prefix := fmt.Sprintf("iec61850: control %s %s", e.Operation, e.Ref)
	if e.AddCause != 0 {
		prefix += fmt.Sprintf(" (cause: %s)", e.AddCause)
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %v", prefix, e.Wrapped)
	}
	return prefix + ": failed"
}

// Unwrap returns a slice of errors so that [errors.Is] matches both
// the operation-specific sentinel (e.g. [ErrOperateFailed]) and the
// wrapped cause, if any.
func (e *ControlError) Unwrap() []error {
	sentinel := ErrControlFailed
	switch e.Operation {
	case "select":
		sentinel = ErrSelectFailed
	case "operate":
		sentinel = ErrOperateFailed
	case "cancel":
		sentinel = ErrCancelFailed
	}
	if e.Wrapped != nil {
		return []error{sentinel, e.Wrapped}
	}
	return []error{sentinel}
}
