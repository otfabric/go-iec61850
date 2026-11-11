// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"testing"
	"time"
)

// TestSpec_9_2_RefrTmQuality: refrTm (IEC 61850-9-2 Table 14, an
// IEC 61850-8-1 UtcTime) carries DefaultTimeQuality (not synchronised)
// by default, and with PublisherConfig.Clock the clock's time for the
// frame's last sample, earlier samples one period apart, and the
// clock's TimeQuality octet.
func TestSpec_9_2_RefrTmQuality(t *testing.T) {
	stamp := time.Date(2026, 11, 11, 12, 0, 0, 500_000_000, time.UTC)
	recv := func(t *testing.T, pc PublisherConfig) *Frame {
		t.Helper()
		hub := NewHub()
		rx := hub.NewPort(MAC{})
		p, err := NewPublisher(hub.NewPort(MAC{}), pc)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); _ = p.Run(ctx) }()
		defer func() { cancel(); <-done; _ = p.Close() }()
		raw, err := rx.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}

	t.Run("default", func(t *testing.T) {
		pc := pubConfig()
		pc.RefrTm = true
		f := recv(t, pc)
		if a := f.PDU.ASDUs[0]; !a.HasRefrTm || a.RefrTmQuality != DefaultTimeQuality {
			t.Errorf("refrTm quality %#x, want %#x", a.RefrTmQuality, DefaultTimeQuality)
		}
		if DefaultTimeQuality != 0x2a {
			t.Errorf("DefaultTimeQuality %#x, want 0x2a", DefaultTimeQuality)
		}
	})

	t.Run("clock", func(t *testing.T) {
		pc := pubConfig()
		pc.RefrTm = true
		pc.NoASDU = 2
		pc.Clock = func() (time.Time, uint8) { return stamp, 0x94 }
		f := recv(t, pc)
		a0, a1 := f.PDU.ASDUs[0], f.PDU.ASDUs[1]
		if !a1.RefrTm.Equal(stamp) || a1.RefrTmQuality != 0x94 || a0.RefrTmQuality != 0x94 {
			t.Errorf("last refrTm %v q %#x, want %v q 0x94", a1.RefrTm, a1.RefrTmQuality, stamp)
		}
		// 4000 samples/s: 250 µs apart (UtcTime fraction resolution
		// is 2^-24 s).
		if d := a1.RefrTm.Sub(a0.RefrTm); d < 249*time.Microsecond || d > 251*time.Microsecond {
			t.Errorf("refrTm spacing %v, want 250µs", d)
		}
	})
}
