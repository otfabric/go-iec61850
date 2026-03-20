package iec61850

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-mms"
)

// Quality represents an IEC 61850 quality bit string (13 bits).
//
// Quality is stored as a uint16 where bit positions match the
// IEC 61850 standard and the C libiec61850 convention:
//
//   - bits 0–1: Validity
//   - bit 2: Overflow
//   - bit 3: OutOfRange
//   - bit 4: BadReference
//   - bit 5: Oscillatory
//   - bit 6: Failure
//   - bit 7: OldData
//   - bit 8: Inconsistent
//   - bit 9: Inaccurate
//   - bit 10: Source (substituted)
//   - bit 11: Test
//   - bit 12: OperatorBlocked
type Quality uint16

// qualityBitLength is the number of bits in an IEC 61850 quality.
const qualityBitLength = 13

// Validity represents the validity portion of an IEC 61850 quality.
type Validity int

const (
	// ValidityGood indicates the value is valid.
	ValidityGood Validity = 0
	// ValidityReserved is reserved for future use.
	ValidityReserved Validity = 1
	// ValidityInvalid indicates the value is not valid.
	ValidityInvalid Validity = 2
	// ValidityQuestionable indicates the value may not be valid.
	ValidityQuestionable Validity = 3
)

// Detail quality flags (bits 2–9).
const (
	QualityOverflow     Quality = 1 << 2
	QualityOutOfRange   Quality = 1 << 3
	QualityBadReference Quality = 1 << 4
	QualityOscillatory  Quality = 1 << 5
	QualityFailure      Quality = 1 << 6
	QualityOldData      Quality = 1 << 7
	QualityInconsistent Quality = 1 << 8
	QualityInaccurate   Quality = 1 << 9
)

// Source, test, and operator flags (bits 10–12).
const (
	QualitySourceSubstituted Quality = 1 << 10
	QualityTest              Quality = 1 << 11
	QualityOperatorBlocked   Quality = 1 << 12
)

// validityMask covers the two validity bits.
const validityMask Quality = 0x3

// Validity returns the validity component of the quality.
func (q Quality) Validity() Validity {
	return Validity(q & validityMask)
}

// IsGood returns true when validity is [ValidityGood] and no
// detail quality flags are set.
func (q Quality) IsGood() bool {
	return q.Validity() == ValidityGood && q&^validityMask == 0
}

// Has returns true if the specified quality flag is set.
func (q Quality) Has(flag Quality) bool {
	return q&flag != 0
}

// WithValidity returns a copy of the quality with the given validity.
func (q Quality) WithValidity(v Validity) Quality {
	return (q &^ validityMask) | Quality(v)
}

// String returns a human-readable representation of the quality.
func (q Quality) String() string {
	var parts []string

	switch q.Validity() {
	case ValidityGood:
		parts = append(parts, "good")
	case ValidityInvalid:
		parts = append(parts, "invalid")
	case ValidityReserved:
		parts = append(parts, "reserved-validity")
	case ValidityQuestionable:
		parts = append(parts, "questionable")
	default:
		parts = append(parts, fmt.Sprintf("validity(%d)", q.Validity()))
	}

	flags := []struct {
		flag Quality
		name string
	}{
		{QualityOverflow, "overflow"},
		{QualityOutOfRange, "out-of-range"},
		{QualityBadReference, "bad-reference"},
		{QualityOscillatory, "oscillatory"},
		{QualityFailure, "failure"},
		{QualityOldData, "old-data"},
		{QualityInconsistent, "inconsistent"},
		{QualityInaccurate, "inaccurate"},
		{QualitySourceSubstituted, "substituted"},
		{QualityTest, "test"},
		{QualityOperatorBlocked, "operator-blocked"},
	}
	for _, f := range flags {
		if q.Has(f.flag) {
			parts = append(parts, f.name)
		}
	}

	return strings.Join(parts, ",")
}

// DecodeQuality decodes an IEC 61850 quality from an MMS bit string
// value. Returns a [DecodeError] if the value is not a bit string
// or has fewer than 13 bits.
func DecodeQuality(v *mms.Value) (Quality, error) {
	if v == nil {
		return 0, &DecodeError{
			Type:    "Quality",
			Message: "nil value",
		}
	}
	data, ok := v.BitString()
	if !ok {
		return 0, &DecodeError{
			Type:    "Quality",
			Message: fmt.Sprintf("expected bit string, got %v", v.Type()),
		}
	}
	bitLen, _ := v.BitStringLength()
	if bitLen < qualityBitLength {
		return 0, &DecodeError{
			Type:    "Quality",
			Message: fmt.Sprintf("quality requires %d bits, got %d", qualityBitLength, bitLen),
		}
	}

	return decodeQualityBits(data), nil
}

// EncodeQuality encodes an IEC 61850 quality as an MMS bit string value.
func EncodeQuality(q Quality) *mms.Value {
	return mms.NewBitStringWithLength(encodeQualityBits(q), qualityBitLength)
}

// decodeQualityBits converts MMS bit string bytes (MSB-first) to a
// Quality value where bit 0 = first quality bit (matching IEC 61850).
func decodeQualityBits(data []byte) Quality {
	var q uint16
	for bit := 0; bit < qualityBitLength; bit++ {
		byteIdx := bit / 8
		if byteIdx >= len(data) {
			break
		}
		bitInByte := uint(7 - (bit % 8))
		if data[byteIdx]&(1<<bitInByte) != 0 {
			q |= 1 << uint(bit)
		}
	}
	return Quality(q)
}

// encodeQualityBits converts a Quality value to MMS bit string bytes
// (MSB-first).
func encodeQualityBits(q Quality) []byte {
	data := make([]byte, 2)
	for bit := 0; bit < qualityBitLength; bit++ {
		if q&Quality(1<<uint(bit)) != 0 {
			byteIdx := bit / 8
			bitInByte := uint(7 - (bit % 8))
			data[byteIdx] |= 1 << bitInByte
		}
	}
	return data
}
