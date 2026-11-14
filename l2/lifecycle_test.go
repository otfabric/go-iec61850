// SPDX-License-Identifier: MIT

package l2

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
)

// Lifecycle contract tests (TESTING.md, "Lifecycle contract matrix").

// L1: a second Close of a Demux, a DemuxPort and a HubPort returns nil;
// Recv and Send then fail with ErrClosed.
func TestLifecycle_CloseTwice(t *testing.T) {
	h := NewHub()
	hp := h.NewPort(MAC{})
	d := NewDemux(h.NewPort(MAC{}), 1)
	dp, err := d.Port(EtherTypeGOOSE)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		close func() error
	}{
		{"DemuxPort", dp.Close}, {"Demux", d.Close}, {"HubPort", hp.Close},
	} {
		for i := range 2 {
			if err := c.close(); err != nil {
				t.Fatalf("%s.Close #%d: %v", c.name, i+1, err)
			}
		}
	}
	for _, p := range []Transport{dp, hp} {
		if _, err := p.Recv(context.Background()); !errors.Is(err, ErrClosed) {
			t.Fatalf("%T.Recv after Close: %v, want ErrClosed", p, err)
		}
		if err := p.Send([]byte{1}); !errors.Is(err, ErrClosed) {
			t.Fatalf("%T.Send after Close: %v, want ErrClosed", p, err)
		}
	}
	if !errors.Is(d.Err(), ErrClosed) {
		t.Fatalf("Demux.Err after Close: %v", d.Err())
	}
}

// L2 and L3: Close from two goroutines at once while a Recv of every
// port waits: each Close returns nil, each waiting Recv returns
// ErrClosed, and the Demux's receive goroutine has ended (synctest: no
// goroutine of the bubble is left).
func TestLifecycle_ConcurrentCloseDuringRecv(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := NewHub()
		hp := h.NewPort(MAC{})
		d := NewDemux(h.NewPort(MAC{}), 1)
		dp1, err := d.Port(EtherTypeGOOSE)
		if err != nil {
			t.Fatal(err)
		}
		dp2, err := d.Port(EtherTypeSV)
		if err != nil {
			t.Fatal(err)
		}
		recvs := make(chan error, 3)
		for _, p := range []Transport{hp, dp1, dp2} {
			go func() {
				_, err := p.Recv(context.Background())
				recvs <- err
			}()
		}
		synctest.Wait() // every Recv waits

		closers := []func() error{hp.Close, dp1.Close, d.Close}
		errs := make(chan error, 2*len(closers))
		var wg sync.WaitGroup
		for range 2 {
			for _, c := range closers {
				wg.Go(func() { errs <- c() })
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent Close: %v", err)
			}
		}
		for range 3 {
			if err := <-recvs; !errors.Is(err, ErrClosed) {
				t.Fatalf("Recv waiting at Close: %v, want ErrClosed", err)
			}
		}
		if !errors.Is(d.Err(), ErrClosed) {
			t.Fatalf("Demux.Err: %v", d.Err())
		}
	})
}
