// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"

	"github.com/otfabric/go-mms"
)

// Errors a server application callback ([SettingGroupHandler],
// [ControlHandler]) can return, or wrap, to choose the MMS
// DataAccessError of the refused write (IEC 61850-8-1 8.1.3.4.4.2,
// Table 23). A callback may also return an [*mms.DataAccessError]
// directly. An error that carries neither gets the default of the
// service (see each handler's documentation).
var (
	// ErrWriteAccessDenied maps to object-access-denied: the client is not
	// allowed to change the object (access-violation, a read-only
	// component, an object that is not selected).
	ErrWriteAccessDenied = errors.New("iec61850: object access denied")

	// ErrWriteTemporarilyUnavailable maps to temporarily-unavailable: the
	// object is reserved by another client, or its writability depends on
	// the state of another object (instance-locked-by-other-client).
	ErrWriteTemporarilyUnavailable = errors.New("iec61850: temporarily unavailable")

	// ErrWriteTypeInconsistent maps to type-inconsistent: the value does
	// not match the type of the object (type-conflict).
	ErrWriteTypeInconsistent = errors.New("iec61850: type inconsistent")

	// ErrWriteValueInvalid maps to object-value-invalid: the value is of
	// the right type but out of range (parameter-value-inconsistent).
	ErrWriteValueInvalid = errors.New("iec61850: object value invalid")

	// ErrWriteHardwareFault maps to hardware-fault.
	ErrWriteHardwareFault = errors.New("iec61850: hardware fault")
)

// writeErrorCodes maps the sentinels above to their DataAccessError
// codes (IEC 61850-8-1 Table 23).
var writeErrorCodes = []struct {
	err  error
	code mms.DataAccessErrorCode
}{
	{ErrWriteAccessDenied, mms.DataAccessErrorObjectAccessDenied},
	{ErrWriteTemporarilyUnavailable, mms.DataAccessErrorTemporarilyUnavailable},
	{ErrWriteTypeInconsistent, mms.DataAccessErrorTypeInconsistent},
	{ErrWriteValueInvalid, mms.DataAccessErrorObjectValueInvalid},
	{ErrWriteHardwareFault, mms.DataAccessErrorHardwareFault},
}

// writeError is a refused server write: the cause, and the
// [*mms.DataAccessError] go-mms puts on the wire (go-mms maps any write
// callback error without one to temporarily-unavailable).
type writeError struct {
	err error
	dae *mms.DataAccessError
}

func (e *writeError) Error() string   { return e.err.Error() }
func (e *writeError) Unwrap() []error { return []error{e.err, e.dae} }

// dataAccessCodeOf returns the DataAccessError code err carries: that of
// a wrapped [*mms.DataAccessError], or of a wrapped sentinel above.
func dataAccessCodeOf(err error) (mms.DataAccessErrorCode, bool) {
	var dae *mms.DataAccessError
	if errors.As(err, &dae) && dae.Code != mms.DataAccessErrorNone {
		return dae.Code, true
	}
	for _, m := range writeErrorCodes {
		if errors.Is(err, m.err) {
			return m.code, true
		}
	}
	return mms.DataAccessErrorNone, false
}

// asWriteError returns err as the error of a refused MMS write: with the
// DataAccessError code err carries (see [dataAccessCodeOf]), or def when
// it carries none. Nil stays nil.
func asWriteError(err error, def mms.DataAccessErrorCode) error {
	if err == nil {
		return nil
	}
	code, ok := dataAccessCodeOf(err)
	if !ok {
		code = def
	}
	var dae *mms.DataAccessError
	if errors.As(err, &dae) && dae.Code == code {
		return err
	}
	return &writeError{err: err, dae: &mms.DataAccessError{Code: code}}
}
