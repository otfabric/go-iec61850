// SPDX-License-Identifier: MIT

package l2

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Hub is an in-memory Ethernet segment. Every frame sent on one port is
// delivered to every other port, like a shared bridge. It is intended
// for tests, simulations and examples; frames never touch the network.
//
// A Hub can inject faults (loss, duplication, reordering, delay and
// arbitrary mutation) to exercise subscriber supervision. All methods
// are safe for concurrent use.
type Hub struct {
	mu     sync.Mutex
	ports  []*HubPort
	faults Faults
	rng    *rand.Rand
	held   map[*HubPort][]byte // reorder: frame held back per port
	stats  HubStats
	next   byte
}

// Faults configures fault injection on a [Hub]. Rates are
// probabilities in [0, 1], evaluated independently per delivery.
type Faults struct {
	// Loss drops a delivery.
	Loss float64

	// Duplicate delivers a frame twice.
	Duplicate float64

	// Reorder holds a frame back and delivers it after the next frame
	// for the same port.
	Reorder float64

	// Delay postpones every delivery by this duration.
	Delay time.Duration

	// Seed seeds the random source; equal seeds give equal fault
	// sequences for the same traffic.
	Seed int64

	// Mutate, when non-nil, is applied to a private copy of each frame
	// before delivery. Returning nil drops the frame.
	Mutate func(frame []byte) []byte
}

// HubStats counts hub activity.
type HubStats struct {
	Sent, Delivered, Lost, Duplicated, Reordered, Overflow uint64
}

// NewHub returns an empty hub without faults.
func NewHub() *Hub {
	return &Hub{rng: rand.New(rand.NewSource(1)), held: map[*HubPort][]byte{}}
}

// SetFaults replaces the fault configuration and reseeds the random
// source. Frames currently held for reordering are kept.
func (h *Hub) SetFaults(f Faults) {
	h.mu.Lock()
	h.faults = f
	h.rng = rand.New(rand.NewSource(f.Seed))
	h.mu.Unlock()
}

// Stats returns a snapshot of the hub counters.
func (h *Hub) Stats() HubStats {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stats
}

// Flush delivers frames held back for reordering.
func (h *Hub) Flush() {
	h.mu.Lock()
	held := h.held
	h.held = map[*HubPort][]byte{}
	h.mu.Unlock()
	for p, f := range held {
		p.deliver(f, h)
	}
}

// NewPort attaches a new port. A zero mac is replaced by a locally
// administered unicast address unique within the hub.
func (h *Hub) NewPort(mac MAC) *HubPort {
	h.mu.Lock()
	defer h.mu.Unlock()
	if mac.IsZero() {
		h.next++
		mac = MAC{0x02, 0x00, 0x00, 0x00, 0x00, h.next}
	}
	p := &HubPort{hub: h, mac: mac, ch: make(chan []byte, 1024), closed: make(chan struct{})}
	h.ports = append(h.ports, p)
	return p
}

func (h *Hub) send(from *HubPort, frame []byte) {
	h.mu.Lock()
	h.stats.Sent++
	f := h.faults
	type delivery struct {
		p     *HubPort
		frame []byte
	}
	var out []delivery
	for _, p := range h.ports {
		if p == from {
			continue
		}
		cp := append([]byte(nil), frame...)
		if f.Mutate != nil {
			if cp = f.Mutate(cp); cp == nil {
				h.stats.Lost++
				continue
			}
		}
		if f.Loss > 0 && h.rng.Float64() < f.Loss {
			h.stats.Lost++
			continue
		}
		if f.Reorder > 0 && h.held[p] == nil && h.rng.Float64() < f.Reorder {
			h.held[p] = cp
			h.stats.Reordered++
			continue
		}
		out = append(out, delivery{p, cp})
		if f.Duplicate > 0 && h.rng.Float64() < f.Duplicate {
			h.stats.Duplicated++
			out = append(out, delivery{p, append([]byte(nil), cp...)})
		}
		if held := h.held[p]; held != nil {
			delete(h.held, p)
			out = append(out, delivery{p, held})
		}
	}
	h.mu.Unlock()
	deliver := func() {
		for _, d := range out {
			d.p.deliver(d.frame, h)
		}
	}
	if f.Delay > 0 {
		time.AfterFunc(f.Delay, deliver)
		return
	}
	deliver()
}

func (h *Hub) remove(p *HubPort) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, q := range h.ports {
		if q == p {
			h.ports = append(h.ports[:i], h.ports[i+1:]...)
			break
		}
	}
	delete(h.held, p)
}

// HubPort is one attachment to a [Hub]. It implements [Transport].
type HubPort struct {
	hub       *Hub
	mac       MAC
	ch        chan []byte
	closeOnce sync.Once
	closed    chan struct{}
	mu        sync.RWMutex // guards ch sends against close
}

var _ Transport = (*HubPort)(nil)

func (p *HubPort) deliver(frame []byte, h *Hub) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	select {
	case <-p.closed:
		return
	default:
	}
	select {
	case p.ch <- frame:
		h.mu.Lock()
		h.stats.Delivered++
		h.mu.Unlock()
	default:
		h.mu.Lock()
		h.stats.Overflow++
		h.mu.Unlock()
	}
}

// Send delivers frame to every other port of the hub.
func (p *HubPort) Send(frame []byte) error {
	select {
	case <-p.closed:
		return ErrClosed
	default:
	}
	p.hub.send(p, frame)
	return nil
}

// Recv returns the next frame delivered to this port.
func (p *HubPort) Recv(ctx context.Context) ([]byte, error) {
	select {
	case f := <-p.ch:
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closed:
		return nil, ErrClosed
	}
}

// HardwareAddr returns the port address.
func (p *HubPort) HardwareAddr() MAC { return p.mac }

// Close detaches the port from the hub.
func (p *HubPort) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		close(p.closed)
		p.mu.Unlock()
		p.hub.remove(p)
	})
	return nil
}
