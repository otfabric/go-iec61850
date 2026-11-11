// SPDX-License-Identifier: MIT

package iec61850

import (
	"time"

	"github.com/otfabric/go-mms"
)

// Clock is the source of the protocol timestamps the client and server
// send: the time and the TimeQuality of IEC 61850-7-2 TimeStamp
// (LeapSecondsKnown, ClockFailure, ClockNotSynchronized, TimeAccuracy),
// which IEC 61850-8-1 encodes as the last octet of a UtcTime.
//
// A Clock stamps values only. Timers (sboTimeout, the operTm wait,
// IntgPd, BufTm, ResvTms, the reservation release) run on the Go
// runtime's monotonic clock and never call a Clock, so stepping the
// wall clock (an NTP or PTP correction, a fake clock in a test) does not
// fire, shorten or stretch them.
//
// Implementations must be safe for concurrent use. Now is called once
// per stamped value and Quality right after it; both should return
// quickly (cache a sync status that is expensive to read).
//
// See [SystemClock] for the default and [ClockWithQuality] to attach a
// synchronisation status to the host clock.
type Clock interface {
	// Now returns the current time. Only the wall-clock reading is
	// used.
	Now() time.Time

	// Quality returns the TimeQuality of the times Now returns.
	Quality() TimeQuality
}

// SystemClock is the default [Clock]: [time.Now] with the quality
// "clock not synchronised" and 10 bits of sub-second accuracy (TimeQuality
// octet 0x2a, the same as goose.DefaultTimeQuality). The library cannot
// know whether the host clock is synchronised to an external source, so
// it does not claim it (IEC 61850-7-2 TimeQuality ClockNotSynchronized;
// IEC 61850-8-1 Table 32). Applications that know their synchronisation
// state supply their own Clock, for example with [ClockWithQuality].
type SystemClock struct{}

// Now returns [time.Now].
func (SystemClock) Now() time.Time { return time.Now() }

// Quality returns ClockNotSynchronized with a TimeAccuracy of 10 bits.
func (SystemClock) Quality() TimeQuality { return systemClockQuality }

// systemClockQuality is the TimeQuality of [SystemClock]: octet 0x2a.
var systemClockQuality = TimeQuality{ClockNotSynchronized: true, TimeAccuracy: 10}

// ClockWithQuality returns a [Clock] that reads the time from [time.Now]
// and the TimeQuality from quality, called once per stamped value. Nil
// quality is [SystemClock].
//
// It is the hook for a synchronisation-status source; the library has
// no OS integration of its own. An application maps its source to a
// TimeQuality, typically from a status it refreshes in the background
// rather than querying per call:
//
//   - chrony: "chronyc -c tracking" gives the leap status (field 14,
//     "Not synchronised" sets ClockNotSynchronized) and the system time
//     offset and root dispersion, from which TimeAccuracy follows as the
//     number of fraction bits n with 2^-n s at or above the error bound.
//   - ptp4l: pmc "GET TIME_STATUS_NP" (gmPresent, master offset) and
//     "GET PARENT_DATA_SET" (grandmaster clockClass: 6 is locked to a
//     primary reference, holdover and free-running classes are not
//     synchronised); a valid currentUtcOffset from "GET
//     TIME_PROPERTIES_DATA_SET" sets LeapSecondsKnown. phc2sys keeps
//     the system clock on the PTP hardware clock.
//   - NTP (ntpd, systemd-timesyncd): the kernel's adjtimex(2) status
//     (TIME_ERROR, STA_UNSYNC) and maxerror give ClockNotSynchronized and
//     TimeAccuracy.
//
// A clock hardware fault the source reports sets ClockFailure.
func ClockWithQuality(quality func() TimeQuality) Clock {
	if quality == nil {
		return SystemClock{}
	}
	return qualityClock(quality)
}

type qualityClock func() TimeQuality

func (qualityClock) Now() time.Time         { return time.Now() }
func (q qualityClock) Quality() TimeQuality { return q() }

// clockOrSystem returns c, or [SystemClock] when c is nil.
func clockOrSystem(c Clock) Clock {
	if c == nil {
		return SystemClock{}
	}
	return c
}

// stampUTC is a UtcTime of c's current time and quality (IEC 61850-8-1
// 8.1.3.7). A nil c is [SystemClock].
func stampUTC(c Clock) *mms.Value {
	c = clockOrSystem(c)
	t := c.Now()
	return mms.NewUTCTimeWithQuality(t, encodeTimeQuality(c.Quality()))
}

// stampFunc adapts c to the func() (time.Time, uint8) of the goose and
// sv publishers.
func stampFunc(c Clock) func() (time.Time, uint8) {
	c = clockOrSystem(c)
	return func() (time.Time, uint8) {
		t := c.Now()
		return t, encodeTimeQuality(c.Quality())
	}
}
