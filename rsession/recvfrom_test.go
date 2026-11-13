// SPDX-License-Identifier: MIT

package rsession_test

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/l2"
	"github.com/otfabric/go-iec61850/rsession"
	"github.com/otfabric/go-iec61850/sv"
)

var _ l2.SourceReceiver = (*rsession.Transport)(nil)

// loopbackPair opens a receiver on the loopback address of addr's
// family and a sender on an ephemeral port aimed at it. It skips when
// the family is not available.
func loopbackPair(t *testing.T, si rsession.SessionID, addr netip.Addr) (recv, send *rsession.Transport) {
	t.Helper()
	recv, err := rsession.Open(rsession.Config{SI: si, Insecure: true, LocalAddr: netip.AddrPortFrom(addr, 0)})
	if err != nil {
		if addr.Is6() {
			t.Skipf("no IPv6 loopback: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recv.Close() })
	send = open(t, rsession.Config{SI: si, Insecure: true, RemoteAddr: recv.LocalAddr()})
	return recv, send
}

// TestRecvFromReportsSender is the reproduction of the missing sender
// address: Recv returns only a frame whose source address is
// SourceMAC(ip) (02:00:7f:00:00:01 for 127.0.0.1), without the port and,
// for IPv6, without the address. RecvFrom reports both.
func TestRecvFromReportsSender(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "::1"} {
		t.Run(a, func(t *testing.T) {
			addr := netip.MustParseAddr(a)
			recv, send := loopbackPair(t, rsession.SIGOOSE, addr)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := send.Send(gooseFrame(t)); err != nil {
				t.Fatal(err)
			}
			f, from, err := recv.RecvFrom(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if from.Addr() != addr || from.Port() != send.LocalAddr().Port() || from.Port() == 0 {
				t.Fatalf("from %v, want %v port %d", from, addr, send.LocalAddr().Port())
			}
			if src := l2.MAC(f[6:12]); src != rsession.SourceMAC(addr) {
				t.Fatalf("src %v, want %v", src, rsession.SourceMAC(addr))
			}
			if addr.Is4() && rsession.SourceMAC(addr).String() != "02:00:7f:00:00:01" {
				t.Fatalf("SourceMAC %v", rsession.SourceMAC(addr))
			}

			// Recv still returns the frame alone.
			if err := send.Send(gooseFrame(t)); err != nil {
				t.Fatal(err)
			}
			f2, err := recv.Recv(ctx)
			if err != nil || len(f2) != len(f) {
				t.Fatalf("Recv: %d octets, %v", len(f2), err)
			}
		})
	}
}

func TestRecvFromClosedAndCancelled(t *testing.T) {
	recv := open(t, rsession.Config{SI: rsession.SIGOOSE, Insecure: true, LocalAddr: loopback})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, from, err := recv.RecvFrom(ctx); err != context.Canceled || from.IsValid() {
		t.Fatalf("cancelled: %v %v", from, err)
	}
	_ = recv.Close()
	if _, _, err := recv.RecvFrom(context.Background()); err != rsession.ErrClosed {
		t.Fatalf("closed: %v", err)
	}
}

// TestSubscribersReportSender runs the goose and sv subscribers over the
// transport: Frame.From is the sender's address and port.
func TestSubscribersReportSender(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "::1"} {
		addr := netip.MustParseAddr(a)
		t.Run("goose/"+a, func(t *testing.T) {
			recv, send := loopbackPair(t, rsession.SIGOOSE, addr)
			sub, err := goose.NewSubscriber(recv, goose.SubscriberConfig{})
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var got []netip.AddrPort
			if _, err := sub.Subscribe(goose.SubscriptionConfig{Handler: func(e goose.Event) {
				if e.Kind == goose.EventMessage {
					mu.Lock()
					got = append(got, e.Frame.From)
					mu.Unlock()
				}
			}}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); _ = sub.Run(ctx) }()
			defer func() { cancel(); <-done }()
			pub, err := goose.NewPublisher(send, goose.PublisherConfig{
				Dst: goose.MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01}, AppID: 3,
				GocbRef: "IED1LD0/LLN0$GO$gcb1", DatSet: "IED1LD0/LLN0$ds1", ConfRev: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := pub.Publish([]*mms.Value{mms.NewBoolean(true)}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 })
			mu.Lock()
			from := got[0]
			mu.Unlock()
			if want := netip.AddrPortFrom(addr, send.LocalAddr().Port()); from != want {
				t.Fatalf("From %v, want %v", from, want)
			}
		})
		t.Run("sv/"+a, func(t *testing.T) {
			recv, send := loopbackPair(t, rsession.SISV, addr)
			sub, err := sv.NewSubscriber(recv, sv.SubscriberConfig{})
			if err != nil {
				t.Fatal(err)
			}
			froms := make(chan netip.AddrPort, 1)
			if _, err := sub.Subscribe(sv.SubscriptionConfig{SvID: "MU01", OnASDU: func(s *sv.Sample) {
				select {
				case froms <- s.Frame.From:
				default:
				}
			}}); err != nil {
				t.Fatal(err)
			}
			pub, err := sv.NewPublisher(send, sv.PublisherConfig{
				Dst: sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}, AppID: 0x4000, SvID: "MU01", ConfRev: 1,
				SmpRate: 1000, SmpMod: sv.SmpPerSec, Layout: sv.LELayout,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			wg.Go(func() { _ = sub.Run(ctx) })
			wg.Go(func() { _ = pub.Run(ctx) })
			defer func() { cancel(); wg.Wait() }()
			select {
			case from := <-froms:
				if want := netip.AddrPortFrom(addr, send.LocalAddr().Port()); from != want {
					t.Fatalf("From %v, want %v", from, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no sample")
			}
		})
	}
}
