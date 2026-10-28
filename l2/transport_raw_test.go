// SPDX-License-Identifier: MIT

package l2

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
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

func TestOpenUnknownInterface(t *testing.T) {
	_, err := Open("goose-no-such-if0", TransportOptions{})
	if !errors.Is(err, ErrInterfaceNotFound) && !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("err = %v", err)
	}
}

// TestRawTransportRoundTrip sends GOOSE and SV frames over a real
// interface on one socket that receives both EtherTypes, plus a frame of
// another EtherType that the kernel filter must drop. On Linux it uses lo
// and needs CAP_NET_RAW (CI runs it as root); elsewhere set
// GOOSE_TEST_IFACE (see the goose package test).
func TestRawTransportRoundTrip(t *testing.T) {
	iface := rawTestIface()
	if iface == "" {
		t.Skip("set GOOSE_TEST_IFACE to run raw transport tests")
	}
	dst := MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x7f}
	tx := openOrSkip(t, iface, TransportOptions{Multicast: []MAC{dst}, EtherTypes: []uint16{EtherTypeSV}})
	defer func() { _ = tx.Close() }()
	rx := openOrSkip(t, iface, TransportOptions{Promiscuous: true, ReceiveLocal: true, EtherTypes: []uint16{EtherTypeGOOSE, EtherTypeSV}})
	d := NewDemux(rx, 0)
	defer func() { _ = d.Close() }()
	gp, _ := d.Port(EtherTypeGOOSE)
	sp, _ := d.Port(EtherTypeSV)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	marker := []byte("l2-raw-test")
	for _, et := range []uint16{0x88b5, EtherTypeGOOSE, EtherTypeSV} {
		if err := tx.Send(testFrame(dst, tx.HardwareAddr(), et, false, marker)); err != nil {
			t.Fatal(err)
		}
	}
	// VLAN-tagged SV frame.
	if err := tx.Send(testFrame(dst, tx.HardwareAddr(), EtherTypeSV, true, marker)); err != nil {
		t.Fatal(err)
	}
	wait := func(p *DemuxPort, want uint16, n int) {
		got := 0
		for got < n {
			f, err := p.Recv(ctx)
			if err != nil {
				t.Fatalf("recv 0x%04x: %v", want, err)
			}
			et, _ := EtherTypeOf(f)
			if !bytes.Contains(f, marker) {
				continue // unrelated traffic on the interface
			}
			if et != want {
				t.Fatalf("port 0x%04x got EtherType 0x%04x", want, et)
			}
			got++
		}
	}
	wait(gp, EtherTypeGOOSE, 1)
	wait(sp, EtherTypeSV, 2)
}

func TestRawTransportCloseAndCancel(t *testing.T) {
	iface := rawTestIface()
	if iface == "" {
		if runtime.GOOS == "darwin" {
			iface = "lo0" // BPF attaches to lo0 without sending anything
		} else {
			t.Skip("no interface")
		}
	}
	tr := openOrSkip(t, iface, TransportOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := tr.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("recv: %v", err)
	}
	if tr.HardwareAddr() == (MAC{0xff}) {
		t.Error("unexpected address")
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Recv(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("recv after close: %v", err)
	}
	if err := tr.Send(make([]byte, 60)); !errors.Is(err, ErrClosed) {
		t.Errorf("send after close: %v", err)
	}
}
