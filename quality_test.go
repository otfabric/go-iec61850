package iec61850

import (
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestQuality_Validity(t *testing.T) {
	tests := []struct {
		name string
		q    Quality
		want Validity
	}{
		{"good", 0, ValidityGood},
		{"invalid", Quality(ValidityInvalid), ValidityInvalid},
		{"reserved", Quality(ValidityReserved), ValidityReserved},
		{"questionable", Quality(ValidityQuestionable), ValidityQuestionable},
		{"good with flags", QualityOverflow | QualityTest, ValidityGood},
		{"invalid with flags", Quality(ValidityInvalid) | QualityFailure, ValidityInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.q.Validity()
			if got != tc.want {
				t.Errorf("Validity() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestQuality_IsGood(t *testing.T) {
	tests := []struct {
		name string
		q    Quality
		want bool
	}{
		{"zero", 0, true},
		{"good no flags", Quality(ValidityGood), true},
		{"good with overflow", QualityOverflow, false},
		{"invalid", Quality(ValidityInvalid), false},
		{"questionable", Quality(ValidityQuestionable), false},
		{"good with test", QualityTest, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.q.IsGood()
			if got != tc.want {
				t.Errorf("IsGood() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQuality_Has(t *testing.T) {
	q := QualityOverflow | QualityTest | QualityFailure
	if !q.Has(QualityOverflow) {
		t.Error("expected Has(Overflow) = true")
	}
	if !q.Has(QualityTest) {
		t.Error("expected Has(Test) = true")
	}
	if q.Has(QualityOldData) {
		t.Error("expected Has(OldData) = false")
	}
}

func TestQuality_WithValidity(t *testing.T) {
	q := QualityOverflow | QualityTest
	q2 := q.WithValidity(ValidityInvalid)
	if q2.Validity() != ValidityInvalid {
		t.Errorf("Validity() = %d, want Invalid", q2.Validity())
	}
	if !q2.Has(QualityOverflow) || !q2.Has(QualityTest) {
		t.Error("flags should be preserved")
	}
}

func TestQuality_String(t *testing.T) {
	tests := []struct {
		q    Quality
		want string
	}{
		{0, "good"},
		{Quality(ValidityInvalid), "invalid"},
		{QualityOverflow | QualityTest, "good,overflow,test"},
		{Quality(ValidityQuestionable) | QualityOldData, "questionable,old-data"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := tc.q.String()
			if got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeQuality(t *testing.T) {
	tests := []struct {
		name string
		q    Quality
	}{
		{"good", 0},
		{"invalid", Quality(ValidityInvalid)},
		{"questionable+overflow", Quality(ValidityQuestionable) | QualityOverflow},
		{"all_flags", Quality(ValidityInvalid) | QualityOverflow | QualityOutOfRange |
			QualityBadReference | QualityOscillatory | QualityFailure |
			QualityOldData | QualityInconsistent | QualityInaccurate |
			QualitySourceSubstituted | QualityTest | QualityOperatorBlocked},
		{"test_only", QualityTest},
		{"operator_blocked", QualityOperatorBlocked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded := EncodeQuality(tc.q)
			decoded, err := DecodeQuality(encoded)
			if err != nil {
				t.Fatalf("DecodeQuality: %v", err)
			}
			if decoded != tc.q {
				t.Errorf("roundtrip: got 0x%04x, want 0x%04x", uint16(decoded), uint16(tc.q))
			}
		})
	}
}

func TestDecodeQuality_Nil(t *testing.T) {
	_, err := DecodeQuality(nil)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Errorf("expected DecodeError, got %T: %v", err, de)
	}
}

func TestDecodeQuality_NotBitString(t *testing.T) {
	v := mms.NewInteger(42)
	_, err := DecodeQuality(v)
	if err == nil {
		t.Fatal("expected error for non-bit-string value")
	}
}

func TestDecodeQuality_TooShort(t *testing.T) {
	v := mms.NewBitStringWithLength([]byte{0xff}, 8)
	_, err := DecodeQuality(v)
	if err == nil {
		t.Fatal("expected error for 8-bit bit string")
	}
}

func TestQualityBitLayout(t *testing.T) {
	// Verify specific bit positions match IEC 61850 / C libiec61850 convention.
	data := encodeQualityBits(Quality(ValidityInvalid))
	// Validity Invalid = 2 = bit 1 set
	// In MMS bit string: bit 1 → byte[0] bit 6
	if data[0]&0x40 == 0 {
		t.Errorf("Validity Invalid should set byte[0] bit 6, got 0x%02x", data[0])
	}

	data = encodeQualityBits(QualityTest)
	// Test = bit 11 → byte[1] bit 4
	if data[1]&0x10 == 0 {
		t.Errorf("Test should set byte[1] bit 4, got 0x%02x", data[1])
	}

	data = encodeQualityBits(QualityOperatorBlocked)
	// OperatorBlocked = bit 12 → byte[1] bit 3
	if data[1]&0x08 == 0 {
		t.Errorf("OperatorBlocked should set byte[1] bit 3, got 0x%02x", data[1])
	}
}
