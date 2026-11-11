// SPDX-License-Identifier: MIT

package rsession

import (
	"errors"
	"fmt"

	"github.com/otfabric/go-iec61850/l2"
)

// Sentinel errors returned by the rsession package. Use [errors.Is] to
// test against them. Decoder failures are [*DecodeError] values that
// unwrap to [ErrMalformed] or [ErrUnsupported]; verification failures
// wrap [ErrUnsecured], [ErrUnknownKey], [ErrAuthentication] or
// [ErrReplay].
var (
	// ErrMalformed indicates a datagram that is not a well-formed SPDU:
	// a wrong transport header, a length that runs past the datagram,
	// a truncated field or trailing octets.
	ErrMalformed = errors.New("rsession: malformed SPDU")

	// ErrUnsupported indicates a well-formed SPDU that uses something
	// this package does not implement: a protocol version other than 1
	// or 2, encryption, or a MAC algorithm other than HMAC-SHA256.
	ErrUnsupported = errors.New("rsession: unsupported")

	// ErrUnsecured indicates an SPDU without a key ID and signature,
	// refused unless unsecured SPDUs are allowed.
	ErrUnsecured = errors.New("rsession: unsecured SPDU")

	// ErrUnknownKey indicates an SPDU secured with a key ID that the
	// key provider does not hold.
	ErrUnknownKey = errors.New("rsession: unknown key")

	// ErrAuthentication indicates a secured SPDU whose signature is
	// missing, has the wrong length or algorithm, or does not verify.
	ErrAuthentication = errors.New("rsession: authentication failed")

	// ErrReplay indicates an SPDU whose number was already accepted
	// from the same sender under the same key, or is older than the
	// replay window.
	ErrReplay = errors.New("rsession: replayed SPDU")

	// ErrNoKey indicates that the key provider has no current key to
	// secure an SPDU with.
	ErrNoKey = errors.New("rsession: no current key")

	// ErrInvalidKey indicates a key that cannot be used: ID 0, no or
	// too short key material, or an unsupported MAC algorithm.
	ErrInvalidKey = errors.New("rsession: invalid key")

	// ErrInvalidFrame indicates a frame passed to [Transport.Send] that
	// is not a layer-2 GOOSE or SV frame of the transport's kind.
	ErrInvalidFrame = errors.New("rsession: invalid frame")

	// ErrLimitExceeded indicates an SPDU that would exceed
	// [MaxSPDUSize] or an APDU longer than the 16-bit APDU length.
	ErrLimitExceeded = errors.New("rsession: limit exceeded")

	// ErrInvalidConfig indicates an invalid transport configuration. It
	// is the l2 value, shared with packages goose and sv.
	ErrInvalidConfig = l2.ErrInvalidConfig

	// ErrClosed indicates an operation on a closed transport. It is the
	// l2 value, shared with packages goose and sv.
	ErrClosed = l2.ErrClosed

	// ErrUnsupportedPlatform indicates that multicast group membership
	// or multicast socket options are not available on this platform.
	// It is the l2 value.
	ErrUnsupportedPlatform = l2.ErrUnsupportedPlatform
)

// DecodeError reports where and why an SPDU could not be decoded.
type DecodeError struct {
	// Field names the SPDU field, for example "session header".
	Field string
	// Offset is the octet offset of the field in the datagram.
	Offset int
	// Detail describes the problem.
	Detail string
	// Err is [ErrMalformed] or [ErrUnsupported].
	Err error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("%v: %s at offset %d: %s", e.Err, e.Field, e.Offset, e.Detail)
}

func (e *DecodeError) Unwrap() error { return e.Err }

func malformed(field string, off int, format string, args ...any) error {
	return &DecodeError{Field: field, Offset: off, Detail: fmt.Sprintf(format, args...), Err: ErrMalformed}
}

func unsupported(field string, off int, format string, args ...any) error {
	return &DecodeError{Field: field, Offset: off, Detail: fmt.Sprintf(format, args...), Err: ErrUnsupported}
}

func configErr(field, format string, args ...any) error {
	return fmt.Errorf("%w: rsession: %s: %s", ErrInvalidConfig, field, fmt.Sprintf(format, args...))
}
