// SPDX-License-Identifier: MIT

package sv

import (
	"slices"
	"testing"
)

// FuzzSubscriberHandleFrame feeds copies of one frame, each from its own
// source address, to a subscription limited to two streams. A frame
// that does not decode leaves no stream and delivers nothing; for one
// that does, at most two streams are supervised, a stream is refused
// only at the limit, and every ASDU delivered is one of the frame's.
func FuzzSubscriberHandleFrame(f *testing.F) {
	f.Add(streamRaw(f, 0, 0), uint8(0))
	f.Add(streamRaw(f, 1, 5), uint8(4))
	two := fullFrame()
	two.Simulation = false
	if raw, err := Encode(two); err == nil {
		f.Add(raw, uint8(2))
		f.Add(raw[:len(raw)-3], uint8(1))
	}
	const limit = 2
	f.Fuzz(func(t *testing.T, raw []byte, copies uint8) {
		n := int(copies%6) + 1
		s, err := NewSubscriber(nil, SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		decoded, decErr := Decode(raw)
		var svIDs []string
		if decErr == nil {
			for _, a := range decoded.PDU.ASDUs {
				svIDs = append(svIDs, a.SvID)
			}
		}
		delivered := 0
		var foreign []string
		sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: limit, OnASDU: func(smp *Sample) {
			delivered++
			if !slices.Contains(svIDs, smp.ASDU.SvID) {
				foreign = append(foreign, smp.ASDU.SvID)
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		for i := range n {
			fr := slices.Clone(raw)
			if len(fr) >= 12 {
				fr[10], fr[11] = 0xa5, byte(i) // a source address per copy
			}
			s.HandleFrame(fr)
		}
		stats := sub.Stats()
		refused := sub.Counters().StreamsRefused
		if decErr != nil || len(raw) < 12 {
			if len(stats) != 0 || refused != 0 || delivered != 0 {
				t.Fatalf("frame does not decode (%v), yet %d streams, %d refused, %d delivered", decErr, len(stats), refused, delivered)
			}
			return
		}
		if len(stats) > limit || (refused > 0 && len(stats) != limit) {
			t.Fatalf("%d streams supervised and %d ASDUs refused, limit %d", len(stats), refused, limit)
		}
		if len(foreign) > 0 || delivered > n*len(svIDs) {
			t.Fatalf("%d ASDUs delivered from %d copies of %d (foreign svIDs %q)", delivered, n, len(svIDs), foreign)
		}
	})
}
