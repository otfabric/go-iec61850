// SPDX-License-Identifier: MIT

package scl

import (
	"fmt"
	"math"
)

// convSettingControl converts the SettingControl attributes shared by
// every schema version. The XSD types numOfSGs and actSG as
// xs:unsignedInt, but the SGCB carries them as INT8U (IEC 61850-7-2
// clause 16), so a value above 255 cannot be represented: it is reported
// as an error diagnostic and left zero instead of being truncated to a
// different group number.
func convSettingControl(numOfSGs, actSG uint32, resvTms uint16, diags *diagSink) *SettingControl {
	sc := &SettingControl{ResvTms: resvTms}
	if numOfSGs > math.MaxUint8 {
		diags.add(settingControlRangeDiag("numOfSGs", numOfSGs))
	} else {
		sc.NumOfSGs = uint8(numOfSGs)
	}
	if actSG > math.MaxUint8 {
		diags.add(settingControlRangeDiag("actSG", actSG))
	} else {
		sc.ActSG = uint8(actSG)
	}
	return sc
}

func settingControlRangeDiag(attr string, v uint32) Diagnostic {
	return Diagnostic{
		Severity: DiagError,
		Code:     "invalid-setting-control",
		Path:     "LN0.SettingControl@" + attr,
		Message:  fmt.Sprintf("%s %d exceeds the INT8U range of the setting group control block", attr, v),
	}
}
