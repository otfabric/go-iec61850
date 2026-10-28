// SPDX-License-Identifier: MIT

package sv

import (
	"errors"
	"fmt"

	"github.com/otfabric/go-iec61850/l2"
)

// Sentinel errors returned by the sv package.
//
// Use [errors.Is] to test against these values. Decoder failures are
// [*DecodeError] values that unwrap to one of the decode sentinels. The
// transport and configuration sentinels (ErrClosed,
// ErrUnsupportedPlatform, ErrPermission, ErrInterfaceNotFound,
// ErrInvalidConfig) are the l2 values, shared with package goose.
var (
	// ErrInvalidFrame indicates an Ethernet frame that cannot carry an
	// SV message: too short or a malformed 802.1Q tag.
	ErrInvalidFrame = errors.New("sv: invalid frame")

	// ErrNotSV indicates a well-formed Ethernet frame whose EtherType is
	// not 0x88BA.
	ErrNotSV = errors.New("sv: not a Sampled Values frame")

	// ErrInvalidLength indicates that the SV header length field is
	// inconsistent with the frame or the encoded APDU.
	ErrInvalidLength = errors.New("sv: invalid length")

	// ErrInvalidPDU indicates a BER structure error inside the savPdu:
	// bad tag, bad length, missing mandatory field, duplicate or
	// out-of-order field, wrong field size, or noASDU not matching the
	// number of ASDUs.
	ErrInvalidPDU = errors.New("sv: invalid PDU")

	// ErrInvalidData indicates a sample that does not match its dataset
	// layout (wrong size or an undecodable member).
	ErrInvalidData = errors.New("sv: invalid sample data")

	// ErrLimitExceeded indicates that a decoder or encoder limit was
	// exceeded (ASDU count, sample size, frame size).
	ErrLimitExceeded = errors.New("sv: limit exceeded")

	// ErrInvalidValue indicates a value that cannot be encoded in a
	// sample (nil, wrong type, out of range).
	ErrInvalidValue = errors.New("sv: invalid value")

	// ErrInvalidConfig indicates an invalid publisher, subscriber or
	// transport configuration ([*ConfigError] unwraps to it).
	ErrInvalidConfig = l2.ErrInvalidConfig

	// ErrClosed indicates an operation on a closed publisher,
	// subscriber or transport.
	ErrClosed = l2.ErrClosed

	// ErrUnsupportedPlatform indicates that no raw Ethernet transport is
	// available on this operating system.
	ErrUnsupportedPlatform = l2.ErrUnsupportedPlatform

	// ErrPermission indicates that opening a raw Ethernet socket was
	// denied.
	ErrPermission = l2.ErrPermission

	// ErrInterfaceNotFound indicates that the named network interface
	// does not exist.
	ErrInterfaceNotFound = l2.ErrInterfaceNotFound

	// ErrSCL indicates that an SV control block could not be built from
	// an SCL document.
	ErrSCL = errors.New("sv: SCL binding error")

	// ErrNotFound indicates that a requested control block or stream
	// does not exist.
	ErrNotFound = errors.New("sv: not found")

	// ErrValidation is the base sentinel for subscriber validation
	// failures. [*ValidationError] unwraps to it and to one of the more
	// specific sentinels below.
	ErrValidation = errors.New("sv: validation failed")

	// ErrConfRevMismatch indicates a received confRev that differs from
	// the expected configuration revision.
	ErrConfRevMismatch = errors.New("sv: confRev mismatch")

	// ErrDatSetMismatch indicates a received datSet reference that
	// differs from the expected one.
	ErrDatSetMismatch = errors.New("sv: datSet mismatch")

	// ErrSmpRateMismatch indicates a received smpRate or smpMod that
	// differs from the expected one.
	ErrSmpRateMismatch = errors.New("sv: smpRate mismatch")

	// ErrSampleSizeMismatch indicates a sample whose length differs from
	// the expected dataset layout.
	ErrSampleSizeMismatch = errors.New("sv: sample size mismatch")
)

// DecodeError describes a strict-decoding failure. Offset is the byte
// offset in the frame (or in the buffer passed to [DecodePDU]) where the
// problem was detected; Field names the element being decoded.
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

// ValidationError describes a received ASDU that failed subscriber
// validation. It unwraps to [ErrValidation] and to the specific sentinel
// in Err.
type ValidationError struct {
	SvID  string
	Field string
	Want  string
	Got   string
	Err   error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("sv: %s: %s: want %s, got %s", e.SvID, e.Field, e.Want, e.Got)
}

func (e *ValidationError) Unwrap() []error { return []error{ErrValidation, e.Err} }

// ConfigError describes an invalid configuration field. It unwraps to
// [ErrInvalidConfig].
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("sv: invalid configuration: %s: %s", e.Field, e.Message)
}

func (e *ConfigError) Unwrap() error { return ErrInvalidConfig }

func configErr(field, format string, args ...any) error {
	return &ConfigError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func errValue(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidValue}, args...)...)
}

func errLimit(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrLimitExceeded}, args...)...)
}
