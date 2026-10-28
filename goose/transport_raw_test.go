// SPDX-License-Identifier: MIT

package goose

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// rawTestIface returns the interface for raw-socket tests: $GOOSE_TEST_IFACE,
// or the loopback interface on Linux (where AF_PACKET works on lo).
func rawTestIface() string {
	if v := os.Getenv("GOOSE_TEST_IFACE"); v != "" {
		return v
	}
	if runtime.GOOS == "linux" {
		return "lo"
	}
	return ""
}

func openOrSkip(t *testing.T, iface string, opts TransportOptions) Transport {
	t.Helper()
	tr, err := Open(iface, opts)
	if errors.Is(err, ErrPermission) || errors.Is(err, ErrUnsupportedPlatform) {
		t.Skipf("raw sockets unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// TestRawTransportRoundTrip sends GOOSE over a real interface. On Linux
// it uses lo and needs CAP_NET_RAW (CI runs it as root); elsewhere set
// GOOSE_TEST_IFACE to an Ethernet interface you may send test multicast
// on (on macOS e.g. a host-only bridge) with access to /dev/bpf*.
func TestRawTransportRoundTrip(t *testing.T) {
	iface := rawTestIface()
	if iface == "" {
		t.Skip("set GOOSE_TEST_IFACE to run raw transport tests")
	}
	tx := openOrSkip(t, iface, TransportOptions{Multicast: []MAC{testDst}})
	defer func() { _ = tx.Close() }()
	rx := openOrSkip(t, iface, TransportOptions{Promiscuous: true, ReceiveLocal: true})
	defer func() { _ = rx.Close() }()

	s, _ := NewSubscriber(rx, SubscriberConfig{})
	sub, _ := s.Subscribe(SubscriptionConfig{GocbRef: "RAW/LLN0$GO$gcb1"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	c := pubConfig()
	c.GocbRef = "RAW/LLN0$GO$gcb1"
	c.VLAN = &VLAN{Priority: 4, ID: 0}
	p, err := NewPublisher(tx, c)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.Run(ctx) }()
	_ = p.Publish([]*mms.Value{mms.NewBoolean(true), mms.NewInteger(7)})
	for {
		select {
		case ev := <-sub.Events():
			if ev.Kind == EventMessage {
				v, _ := ev.Frame.PDU.AllData[1].Int64()
				if v == 7 {
					return
				}
			}
		case <-ctx.Done():
			t.Fatal("no GOOSE received over raw transport")
		}
	}
}
