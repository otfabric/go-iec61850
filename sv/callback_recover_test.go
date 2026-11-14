// SPDX-License-Identifier: MIT

package sv

import (
	"testing"
)

// An OnASDU call that does not return normally is recovered and
// counted: OnASDU receives the following ASDUs, and another
// subscription keeps receiving.
func TestOnASDU_CallbackRecovered(t *testing.T) {
	s, err := NewSubscriber(nil, SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var got []uint16
	sub, err := s.Subscribe(SubscriptionConfig{OnASDU: func(smp *Sample) {
		if smp.ASDU.SmpCnt == 1 {
			panic("OnASDU does not return normally")
		}
		got = append(got, smp.ASDU.SmpCnt)
	}})
	if err != nil {
		t.Fatal(err)
	}
	var otherGot int
	if _, err := s.Subscribe(SubscriptionConfig{OnASDU: func(*Sample) { otherGot++ }}); err != nil {
		t.Fatal(err)
	}
	// One frame of two ASDUs (counters 0 and 1), then counter 2.
	s.HandleFrame(frameWith(t, nil, 0, 1))
	s.HandleFrame(frameWith(t, nil, 2))
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("OnASDU received counters %v, want [0 2]", got)
	}
	if otherGot != 3 {
		t.Errorf("other subscription received %d ASDUs, want 3", otherGot)
	}
	if c := sub.Counters(); c.CallbacksRecovered != 1 {
		t.Errorf("CallbacksRecovered = %d, want 1", c.CallbacksRecovered)
	}
	if st := sub.Stats(); len(st) != 1 || st[0].LastSmpCnt != 2 {
		t.Errorf("stats %+v, want one stream at counter 2", st)
	}
}

type failingMetrics struct{ NopMetrics }

func (failingMetrics) ASDUReceived(string) { panic("ASDUReceived does not return normally") }
func (failingMetrics) Event(string, EventKind, bool) {
	panic("Event does not return normally")
}
func (failingMetrics) FrameSent(string, int) { panic("FrameSent does not return normally") }

// Metrics methods that do not return normally are recovered: the ASDU
// is processed and delivered, the events are queued, and the calls are
// counted. The publisher keeps sending.
func TestMetrics_CallbackRecovered(t *testing.T) {
	s, err := NewSubscriber(nil, SubscriberConfig{Metrics: failingMetrics{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var delivered int
	sub, err := s.Subscribe(SubscriptionConfig{OnASDU: func(*Sample) { delivered++ }})
	if err != nil {
		t.Fatal(err)
	}
	s.HandleFrame(frameWith(t, nil, 0))
	s.HandleFrame(frameWith(t, nil, 1))
	if delivered != 2 {
		t.Errorf("%d ASDUs delivered, want 2", delivered)
	}
	if n := len(sub.Events()); n != 1 { // EventStart
		t.Errorf("%d events queued, want 1", n)
	}
	// ASDUReceived twice, Event once.
	if got := s.CallbacksRecovered(); got != 3 {
		t.Errorf("CallbacksRecovered = %d, want 3", got)
	}

	c := pubConfig()
	c.Metrics = failingMetrics{}
	p, err := NewPublisher(discard{}, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if err := p.sendFrame(newFakeClock().Now(), 0); err != nil {
		t.Errorf("sendFrame = %v, want nil", err)
	}
}

// An OnDecodeError call that does not return normally is recovered;
// the next frame is processed.
func TestOnDecodeError_CallbackRecovered(t *testing.T) {
	s, err := NewSubscriber(nil, SubscriberConfig{
		OnDecodeError: func([]byte, error) { panic("OnDecodeError does not return normally") },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var delivered int
	if _, err := s.Subscribe(SubscriptionConfig{OnASDU: func(*Sample) { delivered++ }}); err != nil {
		t.Fatal(err)
	}
	bad := frameWith(t, nil, 0)
	bad[22] = 0xff // the APDU tag, after the 14 octets of addresses and EtherType and the 8 of the header
	s.HandleFrame(bad)
	s.HandleFrame(frameWith(t, nil, 0))
	if got := s.CallbacksRecovered(); got != 1 {
		t.Errorf("CallbacksRecovered = %d, want 1", got)
	}
	if delivered != 1 {
		t.Errorf("%d ASDUs delivered after the recovered call, want 1", delivered)
	}
}
