// SPDX-License-Identifier: MIT

package iec61850

import (
	"testing"

	"github.com/otfabric/go-mms"
)

// IEC 61850-7-2 Table 36, IEC 61850-8-1 8.1.3.4.4.2: writes of single SGCB
// components with the setting group engine enabled keep the engine's
// answers; read-only components and mistyped values are refused before
// it.
func TestSpec_8_1_SGCBComponentItemWrite(t *testing.T) {
	srv := sgSpecServer(t, 0, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	for _, tc := range []struct {
		field string
		val   *mms.Value
		want  mms.DataAccessErrorCode
	}{
		{"NumOfSG", mms.NewUnsigned(2), mms.DataAccessErrorObjectAccessDenied},
		{"LActTm", mms.NewUnsigned(2), mms.DataAccessErrorObjectAccessDenied},
		{"ResvTms", mms.NewUnsigned(2), mms.DataAccessErrorObjectAccessDenied},
		{"ActSG", mms.NewBoolean(true), mms.DataAccessErrorTypeInconsistent},
		{"ActSG", mms.NewUnsigned(256), mms.DataAccessErrorTypeInconsistent},
		{"ActSG", mms.NewUnsigned(4), mms.DataAccessErrorObjectValueInvalid},
		{"CnfEdit", mms.NewUnsigned(1), mms.DataAccessErrorTypeInconsistent},
		{"CnfEdit", mms.NewBoolean(true), mms.DataAccessErrorTemporarilyUnavailable},
		{"ActSG", mms.NewUnsigned(2), mms.DataAccessErrorNone},
	} {
		_, err := c.mmsClient.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: mms.ItemID("LLN0$SP$SGCB$" + tc.field), Value: tc.val})
		if got := dataAccessCode(err); got != tc.want {
			t.Errorf("%s = %v: %v (%v), want %v", tc.field, tc.val, got, err, tc.want)
		}
	}
	info, err := c.GetSettingGroupInfo(ctx, "LD1")
	if err != nil {
		t.Fatal(err)
	}
	if info.NumOfSGs != 3 || info.ActSG != 2 {
		t.Errorf("SGCB = %+v", info)
	}
}
