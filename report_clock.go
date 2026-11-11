// SPDX-License-Identifier: MIT

package iec61850

import "time"

// reportClock is the time source of the report engine: TimeOfEntry
// stamps and the BufTm timer. Tests replace it to run BufTm
// deterministically.
type reportClock interface {
	Now() time.Time
	// AfterFunc calls f in its own goroutine after d.
	AfterFunc(d time.Duration, f func()) reportTimer
}

// reportTimer is a timer started by [reportClock.AfterFunc].
type reportTimer interface {
	// Stop prevents the call if it has not started.
	Stop() bool
}

// systemClock is the wall clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(d time.Duration, f func()) reportTimer {
	return time.AfterFunc(d, f)
}
