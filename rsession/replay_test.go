// SPDX-License-Identifier: MIT

package rsession

import (
	"net/netip"
	"testing"
	"time"
)

func TestReplayWindow(t *testing.T) {
	g := newReplayGuard(0, 0)
	k := streamKey{addr: netip.MustParseAddr("192.0.2.1"), si: SIGOOSE, keyID: 1}
	now := time.Unix(1000, 0)
	steps := []struct {
		n    uint32
		want bool
	}{
		{110, true},                    // first
		{110, false},                   // duplicate
		{111, true},                    // next
		{113, true},                    // gap
		{112, true},                    // late, inside the window
		{112, false},                   // duplicate of a late one
		{113 - ReplayWindow + 1, true}, // oldest number still inside
		{113 - ReplayWindow, false},    // just outside
		{113 + 200, true},              // jump beyond the window
		{113, false},                   // now far behind
	}
	for i, s := range steps {
		if got := g.accept(k, s.n, now); got != s.want {
			t.Fatalf("step %d: accept(%d) = %v, want %v", i, s.n, got, s.want)
		}
	}
	// Streams are independent per sender, SI and key.
	if !g.accept(streamKey{addr: k.addr, si: SIGOOSE, keyID: 2}, 13, now) {
		t.Fatal("new key must start a new window")
	}
	if !g.accept(streamKey{addr: k.addr, si: SISV, keyID: 1}, 13, now) {
		t.Fatal("other SI must have its own window")
	}
}

func TestReplayWrap(t *testing.T) {
	g := newReplayGuard(0, 0)
	k := streamKey{si: SIGOOSE, keyID: 1}
	now := time.Unix(0, 0)
	for _, n := range []uint32{0xfffffffe, 0xffffffff, 0, 1} {
		if !g.accept(k, n, now) {
			t.Fatalf("accept(%#x) across the wrap", n)
		}
	}
	if g.accept(k, 0xffffffff, now) {
		t.Fatal("duplicate across the wrap accepted")
	}
}

func TestReplayResetAfter(t *testing.T) {
	g := newReplayGuard(0, time.Second)
	k := streamKey{si: SIGOOSE, keyID: 1}
	t0 := time.Unix(0, 0)
	g.accept(k, 1000, t0)
	if g.accept(k, 1, t0.Add(500*time.Millisecond)) {
		t.Fatal("restart accepted before the silence")
	}
	if !g.accept(k, 1, t0.Add(2*time.Second)) {
		t.Fatal("restart refused after the silence")
	}
	if !g.accept(k, 2, t0.Add(2*time.Second)) || g.accept(k, 1, t0.Add(2*time.Second)) {
		t.Fatal("window not restarted at the new number")
	}
}

func TestReplayEviction(t *testing.T) {
	g := newReplayGuard(2, 0)
	t0 := time.Unix(0, 0)
	a := streamKey{keyID: 1}
	b := streamKey{keyID: 2}
	c := streamKey{keyID: 3}
	g.accept(a, 5, t0)
	g.accept(b, 5, t0.Add(time.Second))
	g.accept(c, 5, t0.Add(2*time.Second)) // evicts a, heard from least recently
	if len(g.windows) != 2 {
		t.Fatalf("%d windows", len(g.windows))
	}
	if _, ok := g.windows[a]; ok {
		t.Fatal("least recent stream kept")
	}
}
