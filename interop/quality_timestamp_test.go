//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase C4 — quality and timestamp semantics.
//
// All tests use the go-iec61850 server (no external adapter required), so they
// run in every environment including local development without Docker.
//
// Quality encoding: IEC 61850-7-3 Quality is a 13-bit BitString.
// Bit layout (MSB of byte[0] is bit 0):
//
//	bits  0–1 : validity (00=good, 01=invalid, 10=reserved, 11=questionable)
//	bit   2   : overflow
//	bit   3   : outOfRange
//	bit   4   : badReference
//	bit   5   : oscillatory
//	bit   6   : failure
//	bit   7   : oldData
//	bit   8   : inconsistent
//	bit   9   : inaccurate
//	bit  10   : source  (0=process, 1=substituted)
//	bit  11   : test
//	bit  12   : operatorBlocked
//
// Timestamp encoding: IEC 61850-8-1 UTCTime — 8 bytes:
//   - bytes 0–3: seconds since epoch (1970-01-01T00:00:00Z)
//   - bytes 4–6: fractional seconds (24-bit, resolution ~60 ns)
//   - byte    7: time-quality flags (see mms.UTCTimeQuality*)
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// qualityBits builds a 13-bit quality bit string from a raw uint16.
// Bit 0 of the uint16 maps to bit 7 (MSB) of byte[0] in the encoded form.
func qualityBits(v uint16) *mms.Value {
	b0 := byte(v >> 5)          // upper 8 bits of the 13-bit field
	b1 := byte((v & 0x1F) << 3) // lower 5 bits, shifted into the high end of byte[1]
	return mms.NewBitStringWithLength([]byte{b0, b1}, 13)
}

// qualityVal extracts the numeric value of a 13-bit quality bit string.
func qualityVal(v *mms.Value) (uint16, bool) {
	bits, ok := v.BitString()
	if !ok {
		return 0, false
	}
	bl, _ := v.BitStringLength()
	if bl != 13 || len(bits) < 2 {
		return 0, false
	}
	hi := uint16(bits[0])
	lo := uint16(bits[1]) >> 3
	return (hi << 5) | lo, true
}

// ---------------------------------------------------------------------------
// Quality validity states
// ---------------------------------------------------------------------------

// TestGoServer_Quality_Good verifies that the default quality value reads as
// "good" (all bits zero).
func TestGoServer_Quality_Good(t *testing.T) {
	srv := startGoIEDServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	qRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.q[ST]")
	raw, err := c.ReadRaw(ctx, qRef)
	if err != nil {
		t.Fatalf("ReadRaw SPS1.q: %v", err)
	}
	qv, ok := qualityVal(raw)
	if !ok {
		t.Fatalf("SPS1.q: expected 13-bit BitString, got %s", raw.Type())
	}
	if qv != 0 {
		t.Errorf("default quality: want 0 (good), got 0x%04x", qv)
	}
}

// TestGoServer_Quality_Invalid sets SPS1.q to validity=invalid (bit 1 set) via
// the server store and verifies the client reads the same value back.
func TestGoServer_Quality_Invalid(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// validity=invalid → bits 0-1 = 01 → uint16 value of (0b01 << 11) = 0x0800
	const qInvalid uint16 = 0x0800
	srv.srv.SetValue(ctx,
		"InteropLD/GGIO1$ST$SPS1$q",
		qualityBits(qInvalid),
	)

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	qRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.q[ST]")
	raw, err := c.ReadRaw(ctx, qRef)
	if err != nil {
		t.Fatalf("ReadRaw SPS1.q: %v", err)
	}
	qv, ok := qualityVal(raw)
	if !ok {
		t.Fatalf("SPS1.q: expected 13-bit BitString, got %s", raw.Type())
	}
	if qv != qInvalid {
		t.Errorf("quality validity=invalid: want 0x%04x, got 0x%04x", qInvalid, qv)
	}
}

// TestGoServer_Quality_Questionable sets SPS1.q to validity=questionable
// (bits 0-1 = 11) and verifies round-trip.
func TestGoServer_Quality_Questionable(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// validity=questionable → bits 0-1 = 11 → uint16 = 0x1800
	const qQst uint16 = 0x1800
	srv.srv.SetValue(ctx, "InteropLD/GGIO1$ST$SPS1$q", qualityBits(qQst))

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	qRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.q[ST]")
	raw, err := c.ReadRaw(ctx, qRef)
	if err != nil {
		t.Fatalf("ReadRaw SPS1.q: %v", err)
	}
	qv, ok := qualityVal(raw)
	if !ok {
		t.Fatalf("SPS1.q: expected 13-bit BitString, got %s", raw.Type())
	}
	if qv != qQst {
		t.Errorf("quality validity=questionable: want 0x%04x, got 0x%04x", qQst, qv)
	}
}

// TestGoServer_Quality_DetailBits sets multiple quality detail bits and verifies
// they are all preserved on read-back.
func TestGoServer_Quality_DetailBits(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// validity=good, source=substituted (bit 10), test=true (bit 11),
	// operatorBlocked=true (bit 12).
	// bits 10,11,12 set → uint16 = 0x0007
	const qDetail uint16 = 0x0007
	srv.srv.SetValue(ctx, "InteropLD/GGIO1$ST$SPS1$q", qualityBits(qDetail))

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	qRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.q[ST]")
	raw, err := c.ReadRaw(ctx, qRef)
	if err != nil {
		t.Fatalf("ReadRaw SPS1.q: %v", err)
	}
	qv, ok := qualityVal(raw)
	if !ok {
		t.Fatalf("SPS1.q: expected 13-bit BitString, got %s", raw.Type())
	}
	if qv != qDetail {
		t.Errorf("quality detail bits: want 0x%04x, got 0x%04x", qDetail, qv)
	}
}

// ---------------------------------------------------------------------------
// Timestamp semantics
// ---------------------------------------------------------------------------

// TestGoServer_Timestamp_Round_Trip writes a specific UTC timestamp to SPS1.t
// via the server store and verifies the client reads it back with millisecond
// precision.
func TestGoServer_Timestamp_Round_Trip(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a round-millisecond timestamp for reliable comparison.
	wantTime := time.Date(2024, 6, 15, 10, 30, 0, 500_000_000, time.UTC) // 500 ms fractional

	srv.srv.SetValue(ctx, "InteropLD/GGIO1$ST$SPS1$t", mms.NewUTCTime(wantTime))

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	tRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.t[ST]")
	raw, err := c.ReadRaw(ctx, tRef)
	if err != nil {
		t.Fatalf("ReadRaw SPS1.t: %v", err)
	}

	gotTime, ok := raw.UTCTime()
	if !ok {
		t.Fatalf("SPS1.t: expected UTCTime, got %s", raw.Type())
	}

	// Allow 1 ms tolerance for fractional-second encoding.
	diff := wantTime.Sub(gotTime)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Millisecond {
		t.Errorf("timestamp round-trip: want %v, got %v (diff=%v)", wantTime, gotTime, diff)
	}
}

// TestGoServer_Timestamp_QualityFlags writes a UTC timestamp with specific
// time-quality flags and verifies they survive the round-trip.
func TestGoServer_Timestamp_QualityFlags(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wantTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Set LeapSecondsKnown + ClockNotSynchronized flags.
	wantQuality := mms.UTCTimeQualityLeapSecondsKnown | mms.UTCTimeQualityClockNotSynchronized

	srv.srv.SetValue(ctx, "InteropLD/GGIO1$ST$SPS1$t",
		mms.NewUTCTimeWithQuality(wantTime, wantQuality),
	)

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	tRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.t[ST]")
	raw, err := c.ReadRaw(ctx, tRef)
	if err != nil {
		t.Fatalf("ReadRaw SPS1.t: %v", err)
	}

	gotQuality := raw.UTCTimeQuality()
	if gotQuality != wantQuality {
		t.Errorf("timestamp quality flags: want 0x%02x, got 0x%02x", wantQuality, gotQuality)
	}

	gotTime, ok := raw.UTCTime()
	if !ok {
		t.Fatalf("SPS1.t: expected UTCTime, got %s", raw.Type())
	}
	diff := wantTime.Sub(gotTime)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Millisecond {
		t.Errorf("timestamp: want %v, got %v (diff=%v)", wantTime, gotTime, diff)
	}
}

// TestGoServer_Timestamp_Accuracy writes timestamps with different fractional-
// second resolutions and verifies the precision fields survive round-trip.
func TestGoServer_Timestamp_Accuracy(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	tRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.t[ST]")

	// Write timestamps at increasing fractional-second resolutions.
	cases := []struct {
		name string
		ns   int
	}{
		{"0ms", 0},
		{"100ms", 100_000_000},
		{"250ms", 250_000_000},
		{"999ms", 999_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantTime := time.Date(2025, 3, 1, 12, 0, 0, tc.ns, time.UTC)
			srv.srv.SetValue(ctx, "InteropLD/GGIO1$ST$SPS1$t", mms.NewUTCTime(wantTime))

			raw, err := c.ReadRaw(ctx, tRef)
			if err != nil {
				t.Fatalf("ReadRaw SPS1.t (%s): %v", tc.name, err)
			}
			gotTime, ok := raw.UTCTime()
			if !ok {
				t.Fatalf("SPS1.t: expected UTCTime, got %s", raw.Type())
			}
			diff := wantTime.Sub(gotTime)
			if diff < 0 {
				diff = -diff
			}
			// MMS UTCTime has ~60 ns resolution; allow 1 ms for encoding rounding.
			if diff > time.Millisecond {
				t.Errorf("%s: want %v, got %v (diff=%v)", tc.name, wantTime, gotTime, diff)
			}
		})
	}
}
