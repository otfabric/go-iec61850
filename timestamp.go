// SPDX-License-Identifier: MIT

package iec61850

import (
	"fmt"
	"time"

	"github.com/otfabric/go-mms"
)

// Timestamp represents an IEC 61850 timestamp (UTCTime) with
// optional time quality information.
//
// The IEC 61850 timestamp is an 8-byte structure:
//   - bytes 0–3: seconds since Unix epoch
//   - bytes 4–6: fraction of second (24-bit binary fraction)
//   - byte 7: time quality
//
// [EncodeTimestamp] preserves the full [TimeQuality] in the
// UTCTimeQuality byte. [DecodeTimestamp] quality fidelity depends
// on go-mms exposing UTCTimeQuality(); if the MMS layer does not
// populate the quality byte, decoded quality will be zero-valued.
type Timestamp struct {
	// Time is the timestamp value.
	Time time.Time

	// Quality contains time quality flags. Zero-valued when
	// decoded from go-mms (time quality not yet exposed by
	// the MMS layer).
	Quality TimeQuality
}

// TimeQuality represents the time quality byte of an IEC 61850
// timestamp.
type TimeQuality struct {
	// LeapSecondKnown indicates whether leap second information
	// is known (bit 7 of quality byte).
	LeapSecondKnown bool

	// ClockFailure indicates a clock hardware failure
	// (bit 6 of quality byte).
	ClockFailure bool

	// ClockNotSynchronized indicates the clock is not synchronized
	// to an external time source (bit 5 of quality byte).
	ClockNotSynchronized bool

	// TimeAccuracy is the number of significant bits in the
	// fraction-of-second field (bits 0–4, range 0–31). Higher
	// values indicate better precision. Zero means unspecified.
	TimeAccuracy int
}

// IsZero returns true if the timestamp has a zero time value.
func (ts Timestamp) IsZero() bool {
	return ts.Time.IsZero()
}

// String returns the timestamp formatted as RFC 3339 with
// nanosecond precision.
func (ts Timestamp) String() string {
	if ts.Time.IsZero() {
		return "<zero>"
	}
	return ts.Time.Format(time.RFC3339Nano)
}

// DecodeTimestamp decodes an IEC 61850 timestamp from an MMS
// UTCTime value. Returns a [DecodeError] if the value is not
// a UTCTime.
//
// Time quality flags (leap second awareness, clock failure, clock
// synchronization, sub-second accuracy) are decoded from the
// go-mms UTCTimeQuality byte.
func DecodeTimestamp(v *mms.Value) (Timestamp, error) {
	if v == nil {
		return Timestamp{}, &DecodeError{
			Type:    "Timestamp",
			Message: "nil value",
		}
	}
	t, ok := v.UTCTime()
	if !ok {
		return Timestamp{}, &DecodeError{
			Type:    "Timestamp",
			Message: fmt.Sprintf("expected UTC time, got %v", v.Type()),
		}
	}

	qb := v.UTCTimeQuality()
	tq := TimeQuality{
		LeapSecondKnown:      qb&0x80 != 0,
		ClockFailure:         qb&0x40 != 0,
		ClockNotSynchronized: qb&0x20 != 0,
		TimeAccuracy:         int(qb & 0x1F),
	}

	return Timestamp{Time: t, Quality: tq}, nil
}

// EncodeTimestamp encodes an IEC 61850 timestamp as an MMS UTCTime
// value, including the TimeQuality byte.
//
// The quality byte encodes leap-second awareness, clock failure,
// clock synchronization, and sub-second accuracy bits.
func EncodeTimestamp(ts Timestamp) *mms.Value {
	qb := encodeTimeQuality(ts.Quality)
	return mms.NewUTCTimeWithQuality(ts.Time, qb)
}

func encodeTimeQuality(tq TimeQuality) uint8 {
	var qb uint8
	if tq.LeapSecondKnown {
		qb |= 0x80
	}
	if tq.ClockFailure {
		qb |= 0x40
	}
	if tq.ClockNotSynchronized {
		qb |= 0x20
	}
	qb |= uint8(tq.TimeAccuracy) & 0x1F
	return qb
}
