// SPDX-License-Identifier: MIT

package rsession

import (
	"net/netip"
	"sync"
	"time"
)

// ReplayWindow is the number of SPDU numbers below the highest accepted
// one that may still arrive once each (reordering on the network).
const ReplayWindow = 64

// DefaultMaxSenders bounds the replay state a [Transport] keeps.
const DefaultMaxSenders = 1024

// streamKey identifies one sender's sequence of SPDU numbers.
type streamKey struct {
	addr  netip.Addr
	si    SessionID
	keyID uint32
}

// window is the sliding replay window of one stream (RFC 4303 3.4.3,
// with RFC 1982 serial number arithmetic for the 32-bit wrap).
type window struct {
	top    uint32
	bitmap uint64 // bit i: top-i was accepted
	last   time.Time
}

// replayGuard keeps the replay windows of a transport's senders.
type replayGuard struct {
	mu         sync.Mutex
	windows    map[streamKey]*window
	max        int
	resetAfter time.Duration
}

func newReplayGuard(max int, resetAfter time.Duration) *replayGuard {
	if max <= 0 {
		max = DefaultMaxSenders
	}
	return &replayGuard{windows: make(map[streamKey]*window), max: max, resetAfter: resetAfter}
}

// accept records n for k and reports whether it is new. Call it only for
// SPDUs whose signature verified, so a forged SPDU cannot move a window.
func (g *replayGuard) accept(k streamKey, n uint32, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	w, ok := g.windows[k]
	if !ok {
		if len(g.windows) >= g.max {
			g.evictLocked()
		}
		g.windows[k] = &window{top: n, bitmap: 1, last: now}
		return true
	}
	diff := serialDiff(n, w.top)
	switch {
	case diff > 0:
		if diff >= ReplayWindow {
			w.bitmap = 1
		} else {
			w.bitmap = w.bitmap<<uint(diff) | 1
		}
		w.top = n
	case -diff < ReplayWindow:
		bit := uint64(1) << uint(-diff)
		if w.bitmap&bit != 0 {
			return false
		}
		w.bitmap |= bit
	default:
		// Older than the window: a replay, or a sender that restarted
		// its numbering. Accept the latter only after silence.
		if g.resetAfter <= 0 || now.Sub(w.last) < g.resetAfter {
			return false
		}
		w.top, w.bitmap = n, 1
	}
	w.last = now
	return true
}

// evictLocked drops the stream heard from least recently.
func (g *replayGuard) evictLocked() {
	var oldest streamKey
	var at time.Time
	first := true
	for k, w := range g.windows {
		if first || w.last.Before(at) {
			oldest, at, first = k, w.last, false
		}
	}
	delete(g.windows, oldest)
}
