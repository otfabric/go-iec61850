// SPDX-License-Identifier: MIT

package rsession_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/internal/leakcheck"
	"github.com/otfabric/go-iec61850/rsession"
)

// TestChaos_ForgedReplayedSPDUsKeyRotation sends a receiver a seeded mix
// of authentic SPDUs and hostile ones (replays of earlier datagrams, bit
// flips, truncations, unsecured SPDUs, SPDUs under an unknown key and
// random octets) while the key ring rotates: key 2 is added and made
// current a third of the way in, key 1 is removed at two thirds.
//
// Invariants (IEC TR 61850-90-5 11.3, 11.4): every frame Recv returns is
// an authentic SPDU, each at most once; no hostile datagram reaches
// Recv; after key 1 is removed nothing signed with it is accepted; every
// datagram is accounted for in Stats (received, dropped, ignored or one
// refusal counter), and OnReject is called once per refusal.
func TestChaos_ForgedReplayedSPDUsKeyRotation(t *testing.T) {
	for _, seed := range []int64{1, 2, 3} {
		t.Run("", func(t *testing.T) { chaosRotation(t, seed, 1500) })
	}
}

func chaosRotation(t *testing.T, seed int64, n int) {
	k1 := rsession.Key{ID: 1, Material: bytes.Repeat([]byte{0x11}, 32), MAC: rsession.MACHMACSHA256_128}
	k2 := rsession.Key{ID: 2, Material: bytes.Repeat([]byte{0x22}, 32), MAC: rsession.MACHMACSHA256_256}
	stranger := rsession.Key{ID: 77, Material: bytes.Repeat([]byte{0x77}, 16), MAC: rsession.MACHMACSHA256_80}
	keys, err := rsession.NewKeyRing(k1)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var rejects int
	recv := open(t, rsession.Config{
		SI: rsession.SIGOOSE, LocalAddr: loopback, Keys: keys, Queue: 4 * n,
		OnReject: func(netip.AddrPort, error) { mu.Lock(); rejects++; mu.Unlock() },
	})
	raw, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(recv.LocalAddr()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()

	apdu := gooseFrame(t)[26:]
	rng := rand.New(rand.NewSource(seed))
	var (
		number    uint32
		authentic = map[uint16]bool{} // APPIDs of the authentic SPDUs
		sent      [][]byte
		current   = &k1
		removed   bool
		datagrams int
	)
	encode := func(appID uint16, key *rsession.Key) []byte {
		number++
		b, err := rsession.Encode(&rsession.SPDU{SI: rsession.SIGOOSE, Number: number, Payloads: []rsession.Payload{{Type: rsession.PayloadGOOSE, AppID: appID, APDU: apdu}}}, key)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	send := func(b []byte) {
		if _, err := raw.Write(b); err != nil {
			t.Fatal(err)
		}
		datagrams++
		if datagrams%64 == 0 {
			time.Sleep(time.Millisecond) // keep within the socket buffer
		}
	}
	for i := range n {
		switch i {
		case n / 3:
			if err := keys.Add(k2); err != nil {
				t.Fatal(err)
			}
			if err := keys.SetCurrent(2); err != nil {
				t.Fatal(err)
			}
			current = &k2
		case 2 * n / 3:
			keys.Remove(1)
			removed = true
		}
		hostileID := uint16(0x8000 + i) // never an authentic APPID
		switch op := rng.Intn(9); {
		case op < 3:
			appID := uint16(i + 1)
			b := encode(appID, current)
			authentic[appID] = true
			sent = append(sent, b)
			send(b)
		case op == 3 && len(sent) > 0: // replay
			send(sent[rng.Intn(len(sent))])
		case op == 4 && len(sent) > 0: // bit flip
			b := bytes.Clone(sent[rng.Intn(len(sent))])
			b[rng.Intn(len(b))] ^= 1 << rng.Intn(8)
			send(b)
		case op == 5 && len(sent) > 0: // truncation
			b := sent[rng.Intn(len(sent))]
			send(b[:rng.Intn(len(b))])
		case op == 6:
			key := &stranger
			if rng.Intn(2) == 0 {
				key = nil // unsecured
			}
			send(encode(hostileID, key))
		case op == 7 && removed: // the retired key
			send(encode(hostileID, &k1))
		default:
			b := make([]byte, rng.Intn(80))
			rng.Read(b)
			send(b)
		}
	}

	accounted := func(s rsession.Stats) int {
		return int(s.Received + s.Dropped + s.Ignored + s.Malformed + s.Unsecured + s.UnknownKey + s.AuthFailed + s.Replayed)
	}
	deadline := time.Now().Add(10 * time.Second)
	for accounted(recv.Stats()) < datagrams {
		if time.Now().After(deadline) {
			t.Fatalf("seed %d: %d of %d datagrams accounted for: %+v", seed, accounted(recv.Stats()), datagrams, recv.Stats())
		}
		time.Sleep(5 * time.Millisecond)
	}
	st := recv.Stats()
	if accounted(st) != datagrams {
		t.Fatalf("seed %d: %d datagrams, %d accounted for: %+v", seed, datagrams, accounted(st), st)
	}
	seen := map[uint16]bool{}
	for range st.Received {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		f, err := recv.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("seed %d: Recv: %v", seed, err)
		}
		appID := binary.BigEndian.Uint16(f[14:16])
		if !authentic[appID] {
			t.Fatalf("seed %d: Recv returned a frame of APPID %#04x that was not sent authentically", seed, appID)
		}
		if seen[appID] {
			t.Fatalf("seed %d: APPID %#04x delivered twice", seed, appID)
		}
		seen[appID] = true
	}
	if int(st.Received)+int(st.Dropped) < len(authentic) {
		t.Errorf("seed %d: %d authentic SPDUs, %d received, %d dropped", seed, len(authentic), st.Received, st.Dropped)
	}
	mu.Lock()
	got := rejects
	mu.Unlock()
	if want := int(st.Malformed + st.Unsecured + st.UnknownKey + st.AuthFailed + st.Replayed); got != want {
		t.Errorf("seed %d: OnReject called %d times, %d refusals counted", seed, got, want)
	}
	if st.Replayed == 0 || st.AuthFailed == 0 || st.UnknownKey == 0 || st.Unsecured == 0 || st.Malformed == 0 {
		t.Errorf("seed %d: a hostile class was never exercised: %+v", seed, st)
	}
}

// TestTransport_CloseContract: Close is idempotent and safe concurrently
// with Send and Recv; after Close both return ErrClosed; the receive
// goroutine is gone.
func TestTransport_CloseContract(t *testing.T) {
	leakcheck.Check(t)
	for range 20 {
		recv, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Insecure: true})
		if err != nil {
			t.Fatal(err)
		}
		send, err := rsession.Open(rsession.Config{SI: rsession.SIGOOSE, RemoteAddr: recv.LocalAddr(), Insecure: true})
		if err != nil {
			t.Fatal(err)
		}
		frame := gooseFrame(t)
		var wg sync.WaitGroup
		var firstRecv sync.Once
		received := make(chan struct{}) // closed by the first Recv that returns a frame
		for range 4 {
			wg.Go(func() {
				for {
					if err := send.Send(frame); err != nil {
						if !errors.Is(err, rsession.ErrClosed) {
							t.Errorf("Send: %v", err)
						}
						return
					}
				}
			})
			wg.Go(func() {
				for {
					if _, err := recv.Recv(context.Background()); err != nil {
						if !errors.Is(err, rsession.ErrClosed) {
							t.Errorf("Recv: %v", err)
						}
						return
					}
					firstRecv.Do(func() { close(received) })
				}
			})
		}
		// Close once frames flow: Send and Recv are running.
		<-received
		wg.Go(func() { _ = recv.Close() })
		wg.Go(func() { _ = send.Close() })
		_ = recv.Close()
		_ = send.Close()
		wg.Wait()
		if err := recv.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
		if _, err := recv.Recv(context.Background()); !errors.Is(err, rsession.ErrClosed) {
			t.Fatalf("Recv after Close: %v", err)
		}
		if err := send.Send(frame); !errors.Is(err, rsession.ErrClosed) {
			t.Fatalf("Send after Close: %v", err)
		}
	}
}
