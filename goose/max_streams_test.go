// SPDX-License-Identifier: MIT

package goose

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// streamFrame is a valid GOOSE frame of stream i: its own source
// address, the same GoCB. TimeAllowedToLive is zero, so no timer runs.
func streamFrame(i int, stNum, sqNum uint32) *Frame {
	return &Frame{
		Dst: testDst, Src: MAC{2, 0, byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}, AppID: 0x1000,
		PDU: PDU{GocbRef: "LD/LLN0$GO$gcb1", DatSet: "LD/LLN0$ds1", GoID: "gcb1",
			T: time.Unix(0, 0), StNum: stNum, SqNum: sqNum, ConfRev: 1, AllData: []*mms.Value{mms.NewBoolean(true)}},
	}
}

// Frames of n distinct streams reach a subscription with a stream
// limit: it supervises min(n, limit) streams and drops and counts the
// frames of the others.
func TestMaxStreams_Boundaries(t *testing.T) {
	cases := []struct {
		maxStreams, limit int
	}{
		{1, 1}, {2, 2}, {5, 5}, {0, DefaultMaxStreams},
	}
	for _, c := range cases {
		for _, n := range []int{c.limit - 1, c.limit, c.limit + 1} {
			t.Run(fmt.Sprintf("MaxStreams=%d/streams=%d", c.maxStreams, n), func(t *testing.T) {
				m := newCountingMetrics()
				s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{Metrics: m})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = s.Close() }()
				sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: c.maxStreams, QueueSize: 1})
				if err != nil {
					t.Fatal(err)
				}
				for i := range n {
					s.HandleFrame(mustEncode(t, streamFrame(i, 1, 0)))
				}
				want := min(n, c.limit)
				if got := len(sub.Supervision()); got != want {
					t.Errorf("%d streams supervised, want %d", got, want)
				}
				refused := uint64(max(n-c.limit, 0))
				if got := sub.Counters().StreamsRefused; got != refused {
					t.Errorf("StreamsRefused = %d, want %d", got, refused)
				}
				m.mu.Lock()
				defer m.mu.Unlock()
				if got := m.dropped["streams"]; got != int(refused) {
					t.Errorf(`FrameDropped("streams") %d times, want %d`, got, refused)
				}
			})
		}
	}
}

// At the limit the streams already supervised keep receiving, no
// stream is replaced, and another subscription is not affected.
func TestMaxStreams_KnownStreamsKeepReceiving(t *testing.T) {
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	full, err := s.Subscribe(SubscriptionConfig{MaxStreams: 2})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Subscribe(SubscriptionConfig{Src: streamFrame(7, 0, 0).Src})
	if err != nil {
		t.Fatal(err)
	}
	s.HandleFrame(mustEncode(t, streamFrame(0, 1, 0)))
	s.HandleFrame(mustEncode(t, streamFrame(1, 1, 0)))
	for i := 2; i < 12; i++ {
		s.HandleFrame(mustEncode(t, streamFrame(i, 1, 0)))
	}
	s.HandleFrame(mustEncode(t, streamFrame(0, 1, 1)))
	s.HandleFrame(mustEncode(t, streamFrame(1, 2, 0)))

	sups := full.Supervision()
	if len(sups) != 2 {
		t.Fatalf("%d streams supervised, want 2", len(sups))
	}
	for i, sup := range sups {
		if sup.Stream.Src != streamFrame(i, 0, 0).Src || sup.Messages != 2 || sup.Status != StatusOK {
			t.Errorf("stream %d: Src %v, %d messages, status %v; want %v, 2, ok", i, sup.Stream.Src, sup.Messages, sup.Status, streamFrame(i, 0, 0).Src)
		}
	}
	if got := full.Counters().StreamsRefused; got != 10 {
		t.Errorf("StreamsRefused = %d, want 10", got)
	}
	if sups := other.Supervision(); len(sups) != 1 || sups[0].Messages != 1 {
		t.Errorf("other subscription: %+v, want one stream with one message", sups)
	}
	if got := other.Counters().StreamsRefused; got != 0 {
		t.Errorf("other subscription: StreamsRefused = %d, want 0", got)
	}
}

// A frame of a new stream at the limit allocates nothing in the
// subscription.
func TestMaxStreams_NewStreamAtLimitAllocatesNothing(t *testing.T) {
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: 3})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	for i := range 3 {
		sub.process(streamFrame(i, 1, 0), now)
	}
	frames := make([]*Frame, 1001) // AllocsPerRun calls once more than its count: every call is a new stream
	for i := range frames {
		frames[i] = streamFrame(100+i, 1, 0)
	}
	k := 0
	allocs := testing.AllocsPerRun(1000, func() {
		sub.process(frames[k], now)
		k++
	})
	if allocs != 0 {
		t.Errorf("a frame of a new stream at the limit allocates %.1f times", allocs)
	}
	if got := len(sub.Supervision()); got != 3 {
		t.Errorf("%d streams supervised, want 3", got)
	}
}

func TestMaxStreams_Config(t *testing.T) {
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Subscribe(SubscriptionConfig{MaxStreams: -1}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("MaxStreams -1: %v, want ErrInvalidConfig", err)
	}
}
