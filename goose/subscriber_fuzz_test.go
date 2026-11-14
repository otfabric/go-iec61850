// SPDX-License-Identifier: MIT

package goose

import (
	"slices"
	"testing"
)

// FuzzSubscriberHandleFrame feeds copies of one frame, each from its own
// source address, to a subscription limited to two streams. A frame
// that does not decode leaves no stream; one that does is supervised
// for at most two sources, the others are refused and counted, and
// every stream and event belongs to the decoded frame's GoCB.
func FuzzSubscriberHandleFrame(f *testing.F) {
	for _, fr := range []*Frame{streamFrame(0, 1, 0), streamFrame(1, 7, 3)} {
		raw, err := Encode(fr)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw, uint8(0))
		f.Add(raw, uint8(4))
		f.Add(raw[:len(raw)-4], uint8(2))
	}
	sim := streamFrame(2, 1, 0)
	sim.Simulation = true
	if raw, err := Encode(sim); err == nil {
		f.Add(raw, uint8(3))
	}
	const limit = 2
	f.Fuzz(func(t *testing.T, raw []byte, copies uint8) {
		n := int(copies%6) + 1
		s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: limit, QueueSize: 64, AcceptSimulation: true})
		if err != nil {
			t.Fatal(err)
		}
		decoded, decErr := DecodeWithLimits(raw, DefaultLimits)
		for i := range n {
			fr := slices.Clone(raw)
			if len(fr) >= 12 {
				fr[10], fr[11] = 0xa5, byte(i) // a source address per copy
			}
			s.HandleFrame(fr)
		}
		sups := sub.Supervision()
		refused := sub.Counters().StreamsRefused
		if decErr != nil || len(raw) < 12 {
			if len(sups) != 0 || refused != 0 {
				t.Fatalf("frame does not decode (%v), yet %d streams and %d refused", decErr, len(sups), refused)
			}
			return
		}
		if len(sups) != min(n, limit) || refused != uint64(max(n-limit, 0)) {
			t.Fatalf("%d copies: %d streams and %d refused, want %d and %d", n, len(sups), refused, min(n, limit), max(n-limit, 0))
		}
		var messages uint64
		for _, sup := range sups {
			if sup.Stream.GocbRef != decoded.PDU.GocbRef || sup.Stream.AppID != decoded.AppID {
				t.Fatalf("stream %+v does not belong to the frame of %q, APPID %#x", sup.Stream, decoded.PDU.GocbRef, decoded.AppID)
			}
			messages += sup.Messages
		}
		if messages > uint64(n) {
			t.Fatalf("%d messages accepted from %d frames", messages, n)
		}
		_ = sub.Close()
		for ev := range sub.Events() {
			if ev.Frame != nil && ev.Frame.PDU.GocbRef != decoded.PDU.GocbRef {
				t.Fatalf("event %v of %q, frame of %q", ev.Kind, ev.Frame.PDU.GocbRef, decoded.PDU.GocbRef)
			}
		}
	})
}
