// SPDX-License-Identifier: MIT

package rsession_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/rsession"
)

// OnReject may close its transport (L10): the Close returns without
// waiting for the receive goroutine it runs on, blocked and later Recv
// calls return ErrClosed, and a Close from another goroutine returns once
// the receive goroutine has ended.
func TestClose_FromOnReject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opened := make(chan *rsession.Transport, 1)
	returned := make(chan error, 1)
	tr, err := rsession.Open(rsession.Config{
		SI: rsession.SIGOOSE, LocalAddr: loopback, Insecure: true,
		OnReject: func(netip.AddrPort, error) {
			tr := <-opened // Open has returned
			returned <- tr.Close()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// When the Close from OnReject does not return, a further Close would
	// wait behind it: the test then leaves the transport open.
	stuck := false
	defer func() {
		if !stuck {
			_ = tr.Close()
		}
	}()
	opened <- tr

	blocked := make(chan error, 1)
	go func() {
		_, err := tr.Recv(ctx)
		blocked <- err
	}()

	raw, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(tr.LocalAddr()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Write([]byte{0x01, 0x40}); err != nil { // not an SPDU: refused
		t.Fatal(err)
	}

	select {
	case err := <-returned:
		if err != nil {
			t.Errorf("Close from OnReject = %v, want nil", err)
		}
	case <-ctx.Done():
		stuck = true
		t.Fatal("Close called from OnReject did not return")
	}
	if err := <-blocked; !errors.Is(err, rsession.ErrClosed) {
		t.Errorf("blocked Recv = %v, want ErrClosed", err)
	}
	if _, err := tr.Recv(ctx); !errors.Is(err, rsession.ErrClosed) {
		t.Errorf("Recv after Close = %v, want ErrClosed", err)
	}
	if err := tr.Send(nil); !errors.Is(err, rsession.ErrClosed) {
		t.Errorf("Send after Close = %v, want ErrClosed", err)
	}

	// From the test goroutine: returns when the receive goroutine has ended.
	closed := make(chan error, 1)
	go func() { closed <- tr.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("second Close = %v, want nil", err)
		}
	case <-ctx.Done():
		t.Fatal("Close after the Close from OnReject did not return")
	}
	if got := tr.Stats().Malformed; got != 1 {
		t.Errorf("Malformed = %d, want 1", got)
	}
}
