// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// TestChaosLossReorderDuplicate runs a publisher and a subscriber over
// a lossy hub and checks the supervision invariants: delivered states
// never go backwards, faults are reported, and retransmissions make the
// subscriber converge on the last published state.
func TestChaosLossReorderDuplicate(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	hub.SetFaults(Faults{Loss: 0.15, Duplicate: 0.15, Reorder: 0.15, Seed: 42})

	var mu sync.Mutex
	var delivered []uint32
	var lastVal int64
	kinds := map[EventKind]int{}
	s, err := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Subscribe(SubscriptionConfig{
		GocbRef: "LD/LLN0$GO$gcb1", ConfRev: 1, QueueSize: 4096,
		Members: []mms.TypeSpec{{Type: mms.ValueTypeInteger}},
		Handler: func(e Event) {
			mu.Lock()
			defer mu.Unlock()
			kinds[e.Kind]++
			if e.Kind == EventMessage && e.NewState {
				delivered = append(delivered, e.Frame.PDU.StNum)
				lastVal, _ = e.Frame.PDU.AllData[0].Int64()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	c := pubConfig()
	c.MinTime, c.MaxTime = time.Millisecond, 4*time.Millisecond
	c.Values = []*mms.Value{mms.NewInteger(0)}
	p, err := NewPublisher(hub.NewPort(MAC{}), c)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.Run(ctx) }()

	const states = 200
	for i := 1; i <= states; i++ {
		if err := p.Publish([]*mms.Value{mms.NewInteger(int64(i))}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Microsecond)
	}
	final := p.State().StNum
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return lastVal == states
	})
	hub.Flush()

	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(delivered); i++ {
		if delivered[i] <= delivered[i-1] {
			t.Fatalf("state went backwards: %d after %d", delivered[i], delivered[i-1])
		}
	}
	if delivered[len(delivered)-1] != final {
		t.Errorf("last delivered stNum %d, want %d", delivered[len(delivered)-1], final)
	}
	if kinds[EventStNumGap] == 0 || kinds[EventDuplicate] == 0 || kinds[EventOutOfOrder] == 0 {
		t.Errorf("faults not observed: %v", kinds)
	}
	if kinds[EventInvalid] != 0 {
		t.Errorf("unexpected invalid events: %d", kinds[EventInvalid])
	}
	st := hub.Stats()
	if st.Lost == 0 || st.Duplicated == 0 || st.Reordered == 0 {
		t.Errorf("hub stats %+v", st)
	}
}

// TestChaosCorruption mutates frames on the wire: the subscriber must
// never panic, must count decode failures, and must still converge.
func TestChaosCorruption(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	var n int
	var nmu sync.Mutex
	hub.SetFaults(Faults{Seed: 7, Mutate: func(b []byte) []byte {
		nmu.Lock()
		defer nmu.Unlock()
		n++
		switch n % 5 {
		case 1:
			b[14+(n%(len(b)-14))] ^= 0xff // flip a byte in the GOOSE part
		case 2:
			return b[:len(b)/2] // truncate
		case 3:
			return nil // drop
		}
		return b
	}})
	m := newCountingMetrics()
	s, _ := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{Metrics: m})
	sub, _ := s.Subscribe(SubscriptionConfig{GocbRef: "LD/LLN0$GO$gcb1", QueueSize: 4096})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	c := pubConfig()
	c.MinTime, c.MaxTime = time.Millisecond, 2*time.Millisecond
	p, _ := NewPublisher(hub.NewPort(MAC{}), c)
	go func() { _ = p.Run(ctx) }()
	for i := 0; i < 20; i++ {
		_ = p.Publish([]*mms.Value{mms.NewBoolean(i%2 == 0), mms.NewInteger(int64(i))})
		time.Sleep(time.Millisecond)
	}
	want := p.State().StNum
	waitFor(t, func() bool {
		sv := sub.Supervision()
		for _, x := range sv {
			if x.Stream.Src == p.Config().Src && x.LastStNum == want {
				return true
			}
		}
		return false
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dropped["decode"] == 0 {
		t.Errorf("no decode failures counted: %v", m.dropped)
	}
	go func() {
		for range sub.Events() {
		}
	}()
}
