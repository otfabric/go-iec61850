// SPDX-License-Identifier: MIT

package sv

import (
	"errors"
	"fmt"
	"testing"
)

// streamRaw is an encoded SV frame of stream i: its own source address,
// the same svID.
func streamRaw(t testing.TB, i int, cnt uint16) []byte {
	t.Helper()
	f := &Frame{Dst: testDst, Src: MAC{2, 0, byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}, AppID: 0x4000}
	f.PDU.ASDUs = []ASDU{{SvID: "MU01", DatSet: "IED1MU/LLN0$PhsMeas1", SmpCnt: cnt, ConfRev: 1, SmpSynch: 1, Sample: leSample(int32(cnt))}}
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// ASDUs of n distinct streams reach a subscription with a stream limit:
// it supervises min(n, limit) streams and drops and counts the ASDUs of
// the others.
func TestMaxStreams_Boundaries(t *testing.T) {
	cases := []struct {
		maxStreams, limit int
	}{
		{1, 1}, {2, 2}, {5, 5}, {0, DefaultMaxStreams},
	}
	for _, c := range cases {
		for _, n := range []int{c.limit - 1, c.limit, c.limit + 1} {
			t.Run(fmt.Sprintf("MaxStreams=%d/streams=%d", c.maxStreams, n), func(t *testing.T) {
				m := &countMetrics{}
				s, err := NewSubscriber(nil, SubscriberConfig{Metrics: m})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = s.Close() }()
				sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: c.maxStreams, EventQueue: 1})
				if err != nil {
					t.Fatal(err)
				}
				for i := range n {
					s.HandleFrame(streamRaw(t, i, 0))
				}
				want := min(n, c.limit)
				if got := len(sub.Stats()); got != want {
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
	s, err := NewSubscriber(nil, SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var delivered [2]int
	full, err := s.Subscribe(SubscriptionConfig{MaxStreams: 2, OnASDU: func(smp *Sample) {
		delivered[smp.Stream.Src[5]]++
	}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Subscribe(SubscriptionConfig{Src: MAC{2, 0, 0, 0, 0, 7}})
	if err != nil {
		t.Fatal(err)
	}
	s.HandleFrame(streamRaw(t, 0, 0))
	s.HandleFrame(streamRaw(t, 1, 0))
	for i := 2; i < 12; i++ {
		s.HandleFrame(streamRaw(t, i, 0))
	}
	s.HandleFrame(streamRaw(t, 0, 1))
	s.HandleFrame(streamRaw(t, 1, 1))

	if delivered != [2]int{2, 2} {
		t.Errorf("ASDUs delivered per stream %v, want [2 2]", delivered)
	}
	stats := full.Stats()
	if len(stats) != 2 {
		t.Fatalf("%d streams supervised, want 2", len(stats))
	}
	for _, st := range stats {
		if st.ASDUs != 2 {
			t.Errorf("stream %v: %d ASDUs, want 2", st.Stream, st.ASDUs)
		}
	}
	if got := full.Counters().StreamsRefused; got != 10 {
		t.Errorf("StreamsRefused = %d, want 10", got)
	}
	if st := other.Stats(); len(st) != 1 || st[0].ASDUs != 1 {
		t.Errorf("other subscription: %+v, want one stream with one ASDU", st)
	}
}

// A frame of a new stream at the limit allocates nothing on the receive
// path.
func TestMaxStreams_NewStreamAtLimitAllocatesNothing(t *testing.T) {
	s, err := NewSubscriber(nil, SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	sub, err := s.Subscribe(SubscriptionConfig{MaxStreams: 3, OnASDU: func(*Sample) {}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		s.HandleFrame(streamRaw(t, i, 0))
	}
	frames := make([][]byte, 1001) // AllocsPerRun calls once more than its count: every call is a new stream
	for i := range frames {
		frames[i] = streamRaw(t, 100+i, 0)
	}
	k := 0
	allocs := testing.AllocsPerRun(1000, func() {
		s.HandleFrame(frames[k])
		k++
	})
	if allocs != 0 {
		t.Errorf("a frame of a new stream at the limit allocates %.1f times", allocs)
	}
	if got := len(sub.Stats()); got != 3 {
		t.Errorf("%d streams supervised, want 3", got)
	}
}

func TestMaxStreams_Config(t *testing.T) {
	s, err := NewSubscriber(nil, SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Subscribe(SubscriptionConfig{MaxStreams: -1}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("MaxStreams -1: %v, want ErrInvalidConfig", err)
	}
}
