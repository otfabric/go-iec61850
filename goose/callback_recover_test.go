// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A Handler call that does not return normally is recovered and
// counted: the Handler receives the following events, and another
// subscription keeps receiving.
func TestHandler_CallbackRecovered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	var calls atomic.Int32
	got := make(chan Event, 8)
	sub, err := s.Subscribe(SubscriptionConfig{Handler: func(ev Event) {
		if calls.Add(1) == 1 {
			panic("Handler does not return normally")
		}
		got <- ev
	}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Subscribe(SubscriptionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// One frame: EventSupervision (the call that does not return), then
	// EventMessage.
	s.HandleFrame(mustEncode(t, streamFrame(0, 1, 0)))
	select {
	case ev := <-got:
		if ev.Kind != EventMessage {
			t.Errorf("Handler received %v after the recovered call, want message", ev.Kind)
		}
	case <-ctx.Done():
		t.Fatal("the Handler received nothing after the recovered call")
	}
	if c := sub.Counters(); c.CallbacksRecovered != 1 {
		t.Errorf("CallbacksRecovered = %d, want 1", c.CallbacksRecovered)
	}
	// A second frame reaches both subscriptions.
	s.HandleFrame(mustEncode(t, streamFrame(0, 2, 0)))
	select {
	case ev := <-got:
		if ev.Kind != EventSupervision && ev.Kind != EventMessage {
			t.Errorf("Handler received %v, want a message", ev.Kind)
		}
	case <-ctx.Done():
		t.Fatal("the Handler received nothing for the second frame")
	}
	if sups := other.Supervision(); len(sups) != 1 || sups[0].Messages != 2 {
		t.Errorf("other subscription: %+v, want two messages", sups)
	}
	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}
}

type failingMetrics struct {
	NopMetrics
	calls atomic.Int32
}

func (m *failingMetrics) FrameReceived(string) {
	m.calls.Add(1)
	panic("FrameReceived does not return normally")
}

func (m *failingMetrics) Event(string, EventKind, bool) {
	panic("Event does not return normally")
}

func (m *failingMetrics) FrameSent(string, bool) {
	panic("FrameSent does not return normally")
}

// Metrics methods that do not return normally are recovered: the
// frame is processed, the events are queued, and the calls are counted.
func TestMetrics_CallbackRecovered(t *testing.T) {
	m := &failingMetrics{}
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{Metrics: m})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	sub, err := s.Subscribe(SubscriptionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s.HandleFrame(mustEncode(t, streamFrame(0, 1, 0)))
	s.HandleFrame(mustEncode(t, streamFrame(0, 1, 1)))
	if sups := sub.Supervision(); len(sups) != 1 || sups[0].Messages != 2 {
		t.Fatalf("supervision %+v, want one stream with two messages", sups)
	}
	if n := len(sub.Events()); n != 3 { // supervision, message, message
		t.Errorf("%d events queued, want 3", n)
	}
	// Per frame: FrameReceived, and Event for each event queued.
	if got := s.CallbacksRecovered(); got != 5 {
		t.Errorf("CallbacksRecovered = %d, want 5", got)
	}

	// Publisher: Publish succeeds and the frame is sent.
	hub := NewHub()
	rx := hub.NewPort(MAC{})
	c := pubConfig()
	c.Metrics = m
	p, err := NewPublisher(hub.NewPort(MAC{2, 0, 0, 0, 0, 1}), c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if err := p.Publish(c.Values); err != nil {
		t.Fatalf("Publish = %v, want nil", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := rx.Recv(ctx); err != nil {
		t.Fatalf("no frame sent: %v", err)
	}
}

// An OnDecodeError call that does not return normally is recovered;
// the next frame is processed.
func TestOnDecodeError_CallbackRecovered(t *testing.T) {
	s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{
		OnDecodeError: func([]byte, error) { panic("OnDecodeError does not return normally") },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	sub, err := s.Subscribe(SubscriptionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	bad := mustEncode(t, streamFrame(0, 1, 0))
	bad[22] = 0xff // the APDU tag, after the 14 octets of addresses and EtherType and the 8 of the header
	s.HandleFrame(bad)
	s.HandleFrame(mustEncode(t, streamFrame(0, 1, 0)))
	if got := s.CallbacksRecovered(); got != 1 {
		t.Errorf("CallbacksRecovered = %d, want 1", got)
	}
	if sups := sub.Supervision(); len(sups) != 1 || sups[0].Messages != 1 {
		t.Errorf("supervision %+v, want one message", sups)
	}
	if _, err := DecodeWithLimits(bad, DefaultLimits); err == nil || errors.Is(err, ErrNotGOOSE) {
		t.Fatalf("the test frame decodes (%v): it must be a GOOSE frame that does not", err)
	}
}
