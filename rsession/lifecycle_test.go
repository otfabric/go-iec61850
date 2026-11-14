// SPDX-License-Identifier: MIT

package rsession_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/rsession"
)

// Lifecycle contract tests (TESTING.md, "Lifecycle contract matrix").

// L9: OnReject may call the methods of its transport: Stats already
// counts the refused datagram, LocalAddr and HardwareAddr answer, and a
// later datagram is still received.
func TestLifecycle_OnRejectCallsPublicMethods(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type seen struct {
		stats rsession.Stats
		local netip.AddrPort
	}
	got := make(chan seen, 4)
	trc := make(chan *rsession.Transport, 1)
	recv := open(t, rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Insecure: true,
		OnReject: func(netip.AddrPort, error) {
			tr := <-trc
			trc <- tr
			_ = tr.HardwareAddr()
			got <- seen{stats: tr.Stats(), local: tr.LocalAddr()}
		}})
	trc <- recv
	raw, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(recv.LocalAddr()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Write([]byte{0x01, 0x40}); err != nil { // malformed
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s.stats.Malformed != 1 || s.local != recv.LocalAddr() {
			t.Fatalf("from OnReject: Stats %+v, LocalAddr %v; want 1 malformed, %v", s.stats, s.local, recv.LocalAddr())
		}
	case <-ctx.Done():
		t.Fatal("OnReject did not return its observation")
	}
	send := open(t, rsession.Config{SI: rsession.SIGOOSE, Insecure: true, RemoteAddr: recv.LocalAddr()})
	if err := send.Send(gooseFrame(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := recv.Recv(ctx); err != nil {
		t.Fatalf("Recv after OnReject: %v", err)
	}
}

// openFiles counts the open file descriptors of the process.
func openFiles(t *testing.T) int {
	t.Helper()
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("cannot count descriptors: %v", err)
	}
	return len(ents)
}

// L14: Open that fails after its socket is bound (the second join of the
// same multicast group is refused by the system) returns the error,
// closes the socket and starts no goroutine; the address can be bound
// again at once.
func TestLifecycle_OpenFailureAfterBindReleasesSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("descriptor count and multicast membership errors are POSIX")
	}
	group := netip.MustParseAddr("239.255.61.85")
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	local := netip.AddrPortFrom(netip.IPv4Unspecified(), probe.LocalAddr().(*net.UDPAddr).AddrPort().Port())
	_ = probe.Close()

	before := openFiles(t)
	tr, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE, LocalAddr: local, Insecure: true,
		Groups: []netip.Addr{group, group}})
	if err == nil {
		_ = tr.Close()
		t.Skip("this system accepts a second membership of the same group")
	}
	if errors.Is(err, rsession.ErrInvalidConfig) {
		t.Fatalf("Open: %v, want a join error", err)
	}
	if after := openFiles(t); after != before {
		t.Fatalf("%d descriptors open after the failed Open, %d before", after, before)
	}
	again, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(local))
	if err != nil {
		t.Fatalf("the address of the failed Open cannot be bound again: %v", err)
	}
	_ = again.Close()
}
