// SPDX-License-Identifier: MIT

package rsession

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

// TestReplayOldNumberFarBehind: an SPDU number more than 2^31 behind the
// window top is old, not ahead. Accepting it would also move the window
// back and make the live stream's next numbers look like replays.
func TestReplayOldNumberFarBehind(t *testing.T) {
	g := newReplayGuard(0, 0)
	k := streamKey{si: SIGOOSE, keyID: 1}
	now := time.Unix(0, 0)
	for _, n := range []uint32{0x10, 0x11, 0x800003f8} {
		if !g.accept(k, n, now) {
			t.Fatalf("accept(%#x)", n)
		}
	}
	for _, n := range []uint32{0x10, 0x11, 0, 0x800003f8 - ReplayWindow} {
		if g.accept(k, n, now) {
			t.Fatalf("accept(%#x) more than the window behind %#x", n, uint32(0x800003f8))
		}
	}
	if !g.accept(k, 0x800003f9, now) {
		t.Fatal("next number of the stream refused")
	}
	// A forward step of more than 2^31 is ahead.
	if !g.accept(k, math.MaxUint32, now) || g.accept(k, math.MaxUint32, now) {
		t.Fatal("forward step beyond 2^31 not accepted once")
	}
	// Inside the window below the top, each number once.
	if !g.accept(k, math.MaxUint32-ReplayWindow+1, now) || g.accept(k, math.MaxUint32-ReplayWindow+1, now) {
		t.Fatal("number inside the window not accepted once")
	}
}

// TestReplayRestartAfterSilenceBelowTop: ReplayResetAfter still lets a
// sender restart far below the top (also from a top near 2^32).
func TestReplayRestartAfterSilenceBelowTop(t *testing.T) {
	g := newReplayGuard(0, time.Second)
	k := streamKey{si: SISV, keyID: 7}
	t0 := time.Unix(0, 0)
	g.accept(k, math.MaxUint32, t0)
	if g.accept(k, 0, t0.Add(time.Second/2)) {
		t.Fatal("0 accepted before the silence")
	}
	if !g.accept(k, 0, t0.Add(2*time.Second)) || !g.accept(k, 1, t0.Add(2*time.Second)) {
		t.Fatal("restart refused after the silence")
	}
}

func testTransport(next uint32) *Transport {
	t := &Transport{}
	t.seq.Store(uint64(next))
	return t
}

// TestTakeNumberNoRepeatUnderKey: the sender does not wrap its SPDU
// number under one key; a key with another ID goes on at 0.
func TestTakeNumberNoRepeatUnderKey(t *testing.T) {
	tr := testTransport(math.MaxUint32 - 1)
	for _, want := range []uint32{math.MaxUint32 - 1, math.MaxUint32} {
		if n, err := tr.takeNumber(1); err != nil || n != want {
			t.Fatalf("takeNumber = %#x, %v; want %#x", n, err, want)
		}
	}
	if got := tr.NextSPDUNumber(); got != 0 {
		t.Fatalf("NextSPDUNumber = %#x after the last number, want 0", got)
	}
	for range 2 {
		if _, err := tr.takeNumber(1); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("takeNumber under the used-up key: %v, want ErrLimitExceeded", err)
		}
	}
	for _, want := range []uint32{0, 1} {
		if n, err := tr.takeNumber(2); err != nil || n != want {
			t.Fatalf("takeNumber(2) = %#x, %v; want %#x", n, err, want)
		}
	}
	// Key 1 again after a change: its state was reset by key 2.
	if n, err := tr.takeNumber(1); err != nil || n != 2 {
		t.Fatalf("takeNumber(1) after a key change = %#x, %v", n, err)
	}
}

// TestTakeNumberUnsecuredWraps: unsecured SPDUs have no replay window
// and wrap at 2^32, and do not mark a key used up.
func TestTakeNumberUnsecuredWraps(t *testing.T) {
	tr := testTransport(math.MaxUint32)
	for _, want := range []uint32{math.MaxUint32, 0, 1} {
		if n, err := tr.takeNumber(0); err != nil || n != want {
			t.Fatalf("takeNumber(0) = %#x, %v; want %#x", n, err, want)
		}
	}
	if n, err := tr.takeNumber(3); err != nil || n != 2 {
		t.Fatalf("takeNumber(3) = %#x, %v", n, err)
	}
}

// TestTakeNumberConcurrent: concurrent senders under one key get each
// number once and none after 0xffffffff.
func TestTakeNumberConcurrent(t *testing.T) {
	const start, workers = math.MaxUint32 - 199, 8
	tr := testTransport(start)
	var mu sync.Mutex
	seen := make(map[uint32]bool)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				n, err := tr.takeNumber(9)
				if err != nil {
					if !errors.Is(err, ErrLimitExceeded) {
						t.Error(err)
					}
					continue
				}
				mu.Lock()
				if seen[n] {
					t.Errorf("number %#x taken twice", n)
				}
				seen[n] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 200 {
		t.Fatalf("%d numbers taken, want 200", len(seen))
	}
	for n := range seen {
		if n < start {
			t.Fatalf("number %#x taken after the wrap", n)
		}
	}
}

// TestVerifyProviderKeyChecked: a key a provider returns is used only if
// it is a valid key for the SPDU's key ID.
func TestVerifyProviderKeyChecked(t *testing.T) {
	k := testKey(5, MACHMACSHA256_128)
	b, err := Encode(&SPDU{SI: SIGOOSE, Payloads: []Payload{{Type: PayloadGOOSE, APDU: tinyGOOSE}}}, &k)
	if err != nil {
		t.Fatal(err)
	}
	other := testKey(6, MACHMACSHA256_128)
	other.Material = k.Material
	short := k
	short.Material = k.Material[:MinKeySize-1]
	for _, tc := range []struct {
		name string
		key  *Key
	}{
		{"other ID", &other},
		{"short material", &short},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, got, err := Verify(b, VerifyOptions{Keys: anyIDProvider{tc.key}})
			if !errors.Is(err, ErrUnknownKey) || !errors.Is(err, ErrInvalidKey) || got != nil {
				t.Fatalf("Verify = %v, %v; want ErrUnknownKey and ErrInvalidKey", got, err)
			}
		})
	}
	if _, got, err := Verify(b, VerifyOptions{Keys: anyIDProvider{&k}}); err != nil || got != &k {
		t.Fatalf("valid key: %v, %v", got, err)
	}
}

// anyIDProvider returns its key for every ID.
type anyIDProvider struct{ k *Key }

func (p anyIDProvider) CurrentKey() (*Key, error)     { return p.k, nil }
func (p anyIDProvider) LookupKey(uint32) (*Key, bool) { return p.k, true }
