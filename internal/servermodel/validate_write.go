// SPDX-License-Identifier: MIT

package servermodel

import (
	"fmt"
	"math"
	"slices"

	"github.com/otfabric/go-mms"
)

// writeRefused returns an error that go-mms answers with the
// DataAccessError code (go-mms maps a write callback error without one to
// temporarily-unavailable).
func writeRefused(code mms.DataAccessErrorCode, format string, args ...any) error {
	return fmt.Errorf("%w: %s", &mms.DataAccessError{Code: code}, fmt.Sprintf(format, args...))
}

// CheckWriteValue checks a value a client writes against the type of the
// object. IEC 61850-8-1 8.1.3.4.4.2 (Table 23, type-conflict): data of a
// different MMS type or structure layout, data that does not match a
// fixed size (a bit-string of another length, an integer outside INTn),
// and variable-length data longer than the maximum (a string) are
// type-inconsistent, as libiec61850 answers (8.1.2.1 NOTE suggests
// object-value-invalid for an over-long string; the normative write
// clause and the peers win). The returned error wraps the
// [*mms.DataAccessError].
func CheckWriteValue(val *mms.Value, ts mms.TypeSpec) error {
	return checkWriteValue(val, ts, "")
}

func checkWriteValue(val *mms.Value, ts mms.TypeSpec, path string) error {
	if val == nil {
		return writeRefused(mms.DataAccessErrorTypeInconsistent, "%snil value", path)
	}
	if ts.TypeName != nil {
		// A named type the model does not expand; nothing to check.
		return nil
	}
	if val.Type() != ts.Type {
		return writeRefused(mms.DataAccessErrorTypeInconsistent, "%sgot %v, want %v", path, val.Type(), ts.Type)
	}
	switch ts.Type {
	case mms.ValueTypeStructure:
		members, _ := val.Structure()
		if len(members) != len(ts.Elements) {
			return writeRefused(mms.DataAccessErrorTypeInconsistent, "%sstructure has %d members, want %d", path, len(members), len(ts.Elements))
		}
		for i := range ts.Elements {
			if err := checkWriteValue(members[i], ts.Elements[i].Type, path+ts.Elements[i].Name+": "); err != nil {
				return err
			}
		}
	case mms.ValueTypeArray:
		elems, _ := val.ArrayElements()
		if ts.Count > 0 && len(elems) != ts.Count {
			return writeRefused(mms.DataAccessErrorTypeInconsistent, "%sarray has %d elements, want %d", path, len(elems), ts.Count)
		}
		if ts.Element != nil {
			for i, e := range elems {
				if err := checkWriteValue(e, *ts.Element, fmt.Sprintf("%s[%d]: ", path, i)); err != nil {
					return err
				}
			}
		}
	case mms.ValueTypeInteger:
		if i, ok := val.Int64(); ok && ts.Size > 0 && ts.Size < 64 {
			lim := int64(1) << (ts.Size - 1)
			if i < -lim || i >= lim {
				return writeRefused(mms.DataAccessErrorTypeInconsistent, "%sinteger %d outside INT%d", path, i, ts.Size)
			}
		}
	case mms.ValueTypeUnsigned:
		if u, ok := val.Uint64(); ok && ts.Size > 0 && ts.Size < 64 && u > uint64(1)<<ts.Size-1 {
			return writeRefused(mms.DataAccessErrorTypeInconsistent, "%sunsigned %d outside INT%dU", path, u, ts.Size)
		}
	default:
		if err := validateWriteSize(val, ts); err != nil {
			return writeRefused(mms.DataAccessErrorTypeInconsistent, "%s%v", path, err)
		}
	}
	return nil
}

// checkEnumValue refuses an ENUMERATED value outside the EnumType of the
// attribute with object-value-invalid (IEC 61850-8-1 8.1.3.4.4.2).
func checkEnumValue(val *mms.Value, attr *DataAttribute) error {
	if attr.BType != "Enum" || len(attr.EnumValues) == 0 {
		return nil
	}
	i, ok := val.Int64()
	if !ok || i < math.MinInt32 || i > math.MaxInt32 {
		return nil
	}
	if !slices.Contains(attr.EnumValues, int(i)) {
		return writeRefused(mms.DataAccessErrorObjectValueInvalid, "enum value %d not in EnumType", i)
	}
	return nil
}
