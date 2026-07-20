// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func TestTimestamp_IsZero(t *testing.T) {
	ts := Timestamp{}
	if !ts.IsZero() {
		t.Error("zero Timestamp should be zero")
	}

	ts.Time = time.Now()
	if ts.IsZero() {
		t.Error("non-zero Timestamp should not be zero")
	}
}

func TestTimestamp_String(t *testing.T) {
	ts := Timestamp{}
	if ts.String() != "<zero>" {
		t.Errorf("zero String() = %q, want %q", ts.String(), "<zero>")
	}

	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	ts = Timestamp{Time: now}
	want := "2025-06-15T10:30:00Z"
	if ts.String() != want {
		t.Errorf("String() = %q, want %q", ts.String(), want)
	}
}

func TestDecodeTimestamp(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 45, 123456789, time.UTC)
	v := mms.NewUTCTime(now)

	ts, err := DecodeTimestamp(v)
	if err != nil {
		t.Fatalf("DecodeTimestamp: %v", err)
	}
	if ts.Time.IsZero() {
		t.Fatal("decoded time should not be zero")
	}
	if ts.Time.Unix() != now.Unix() {
		t.Errorf("seconds: got %d, want %d", ts.Time.Unix(), now.Unix())
	}
}

func TestDecodeTimestamp_Nil(t *testing.T) {
	_, err := DecodeTimestamp(nil)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Errorf("expected DecodeError, got %T: %v", err, de)
	}
}

func TestDecodeTimestamp_NotUTCTime(t *testing.T) {
	v := mms.NewInteger(42)
	_, err := DecodeTimestamp(v)
	if err == nil {
		t.Fatal("expected error for non-UTCTime value")
	}
}

func TestEncodeTimestamp(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 45, 0, time.UTC)
	ts := Timestamp{Time: now}
	v := EncodeTimestamp(ts)

	decoded, ok := v.UTCTime()
	if !ok {
		t.Fatal("encoded value should be UTCTime")
	}
	if decoded.Unix() != now.Unix() {
		t.Errorf("roundtrip seconds: got %d, want %d", decoded.Unix(), now.Unix())
	}
}

func TestTimestamp_Roundtrip(t *testing.T) {
	now := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	original := Timestamp{Time: now}

	encoded := EncodeTimestamp(original)
	decoded, err := DecodeTimestamp(encoded)
	if err != nil {
		t.Fatalf("roundtrip decode: %v", err)
	}
	if decoded.Time.Unix() != original.Time.Unix() {
		t.Errorf("roundtrip: got %v, want %v", decoded.Time, original.Time)
	}
}

func TestTimeQuality_ZeroValue(t *testing.T) {
	ts := Timestamp{}
	if ts.Quality.LeapSecondKnown {
		t.Error("zero TimeQuality should have LeapSecondKnown=false")
	}
	if ts.Quality.ClockFailure {
		t.Error("zero TimeQuality should have ClockFailure=false")
	}
	if ts.Quality.ClockNotSynchronized {
		t.Error("zero TimeQuality should have ClockNotSynchronized=false")
	}
	if ts.Quality.TimeAccuracy != 0 {
		t.Errorf("zero TimeQuality TimeAccuracy = %d, want 0", ts.Quality.TimeAccuracy)
	}
}

func TestTimestamp_QualityRoundtrip(t *testing.T) {
	original := Timestamp{
		Time: time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC),
		Quality: TimeQuality{
			LeapSecondKnown:      true,
			ClockFailure:         false,
			ClockNotSynchronized: true,
			TimeAccuracy:         10,
		},
	}

	encoded := EncodeTimestamp(original)
	decoded, err := DecodeTimestamp(encoded)
	if err != nil {
		t.Fatalf("DecodeTimestamp: %v", err)
	}

	if !decoded.Quality.LeapSecondKnown {
		t.Error("LeapSecondKnown lost in roundtrip")
	}
	if decoded.Quality.ClockFailure {
		t.Error("ClockFailure should be false")
	}
	if !decoded.Quality.ClockNotSynchronized {
		t.Error("ClockNotSynchronized lost in roundtrip")
	}
	if decoded.Quality.TimeAccuracy != 10 {
		t.Errorf("TimeAccuracy = %d, want 10", decoded.Quality.TimeAccuracy)
	}
}

func TestTimestamp_QualityAllBits(t *testing.T) {
	original := Timestamp{
		Time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Quality: TimeQuality{
			LeapSecondKnown:      true,
			ClockFailure:         true,
			ClockNotSynchronized: true,
			TimeAccuracy:         31,
		},
	}

	encoded := EncodeTimestamp(original)
	decoded, err := DecodeTimestamp(encoded)
	if err != nil {
		t.Fatalf("DecodeTimestamp: %v", err)
	}

	if !decoded.Quality.LeapSecondKnown || !decoded.Quality.ClockFailure ||
		!decoded.Quality.ClockNotSynchronized || decoded.Quality.TimeAccuracy != 31 {
		t.Errorf("quality mismatch: got %+v", decoded.Quality)
	}
}
