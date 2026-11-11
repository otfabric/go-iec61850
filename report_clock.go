// SPDX-License-Identifier: MIT

package iec61850

import "time"

// reportClock runs the BufTm timer of the report engine. Tests replace
// it to run BufTm deterministically. TimeOfEntry is a stamp and comes
// from the server's [Clock] instead.
type reportClock interface {
	// AfterFunc calls f in its own goroutine after d.
	AfterFunc(d time.Duration, f func()) reportTimer
}

// reportTimer is a timer started by [reportClock.AfterFunc].
type reportTimer interface {
	// Stop prevents the call if it has not started.
	Stop() bool
}

// systemClock runs timers on the Go runtime's monotonic clock.
type systemClock struct{}

func (systemClock) AfterFunc(d time.Duration, f func()) reportTimer {
	return time.AfterFunc(d, f)
}
