// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"

	"github.com/otfabric/go-mms"
)

// dataAccessCode returns the MMS DataAccessError code carried by err, or
// mms.DataAccessErrorNone.
func dataAccessCode(err error) mms.DataAccessErrorCode {
	var dae *mms.DataAccessError
	if errors.As(err, &dae) {
		return dae.Code
	}
	return mms.DataAccessErrorNone
}
