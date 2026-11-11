// SPDX-License-Identifier: MIT

package goose

import (
	"testing"
	"time"
)

// TestSpec_8_1_ClockStampsT: with PublisherConfig.Clock, every new state
// carries the clock's time and TimeQuality octet as T (IEC 61850-8-1
// Table 55 t, 8.1.3.7 UtcTime), while the retransmission curve is paced
// by Now: a Clock far in the past does not make a retransmission due.
func TestSpec_8_1_ClockStampsT(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2026, 11, 11, 12, 0, 0, 500_000_000, time.UTC)
	mono := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	hub := NewHub()
	tx, rx := hub.NewPort(MAC{}), hub.NewPort(MAC{})
	pc := pubConfig()
	pc.TimeQuality = 0x0a // overridden by Clock
	pc.Now = func() time.Time { return mono }
	pc.Clock = func() (time.Time, uint8) { return stamp, 0x94 }
	p, err := NewPublisher(tx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if err := p.Publish(pc.Values); err != nil {
		t.Fatal(err)
	}
	raw, err := rx.Recv(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !f.PDU.T.Equal(stamp) || f.PDU.TimeQuality != 0x94 {
		t.Errorf("T = %v q %#x, want %v q 0x94", f.PDU.T, f.PDU.TimeQuality, stamp)
	}
	if st := p.State(); !st.T.Equal(stamp) {
		t.Errorf("State().T = %v", st.T)
	}
	// The next retransmission is MinTime after Now, not after T.
	if next := p.retransmit(mono); !next.Equal(mono.Add(pc.MinTime)) {
		t.Errorf("next retransmission at %v, want %v", next, mono.Add(pc.MinTime))
	}
	if st := p.State(); st.SqNum != 0 {
		t.Errorf("retransmission sent early: sqNum %d", st.SqNum)
	}
}
