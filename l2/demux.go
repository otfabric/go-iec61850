// SPDX-License-Identifier: MIT

package l2

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
)

// EtherTypeOf returns the EtherType of an Ethernet frame, looking
// through one 802.1Q tag. ok is false when the frame is too short.
func EtherTypeOf(frame []byte) (etherType uint16, ok bool) {
	if len(frame) < 14 {
		return 0, false
	}
	et := binary.BigEndian.Uint16(frame[12:14])
	if et == EtherTypeVLAN {
		if len(frame) < 18 {
			return 0, false
		}
		et = binary.BigEndian.Uint16(frame[16:18])
	}
	return et, true
}

// DefaultDemuxQueue is the per-port receive queue length of a [Demux].
const DefaultDemuxQueue = 4096

// Demux shares one [Transport] between several receivers, for example a
// GOOSE and a Sampled Values subscriber on the same raw socket. A
// background goroutine receives from the transport and hands each frame
// to every port registered for its EtherType (one 802.1Q tag is looked
// through); frames of other EtherTypes are discarded. Sends go straight
// to the shared transport.
//
// Each port has a bounded queue; when a slow receiver lets it fill up,
// further frames for that port are dropped and counted in
// [DemuxStats].Overflow, so one receiver never blocks another.
type Demux struct {
	t     Transport
	queue int

	mu     sync.RWMutex
	ports  []*DemuxPort
	err    error // terminal receive error
	done   chan struct{}
	closed bool

	received, unmatched, overflow atomic.Uint64
}

// DemuxStats counts demultiplexer activity.
type DemuxStats struct {
	// Received counts frames received from the shared transport.
	Received uint64

	// Unmatched counts frames no port was registered for.
	Unmatched uint64

	// Overflow counts frames dropped because a port queue was full.
	Overflow uint64
}

// NewDemux starts demultiplexing t. The Demux owns t: [Demux.Close]
// closes it. queue is the per-port queue length; values below 1 mean
// [DefaultDemuxQueue].
func NewDemux(t Transport, queue int) *Demux {
	if queue < 1 {
		queue = DefaultDemuxQueue
	}
	d := &Demux{t: t, queue: queue, done: make(chan struct{})}
	go d.run()
	return d
}

func (d *Demux) run() {
	defer close(d.done)
	for {
		frame, err := d.t.Recv(context.Background())
		if err != nil {
			d.mu.Lock()
			if !d.closed && !errors.Is(err, ErrClosed) {
				d.err = err
			}
			ports := d.ports
			d.ports = nil
			d.mu.Unlock()
			for _, p := range ports {
				p.closeWith(d.Err())
			}
			return
		}
		d.received.Add(1)
		et, ok := EtherTypeOf(frame)
		if !ok {
			d.unmatched.Add(1)
			continue
		}
		matched := false
		d.mu.RLock()
		for _, p := range d.ports {
			if !slices.Contains(p.types, et) {
				continue
			}
			matched = true
			if !p.deliver(frame) {
				d.overflow.Add(1)
			}
		}
		d.mu.RUnlock()
		if !matched {
			d.unmatched.Add(1)
		}
	}
}

// Port returns a new [Transport] that receives the frames of the given
// EtherTypes. Several ports may share an EtherType; each gets every
// matching frame (frames are shared read-only between ports, so a
// receiver must not modify a frame it got from Recv). Sending on a port
// sends on the shared transport. Closing a port detaches it without
// affecting the others.
func (d *Demux) Port(etherTypes ...uint16) (*DemuxPort, error) {
	if len(etherTypes) == 0 {
		return nil, &ConfigError{Field: "EtherTypes", Message: "no EtherType"}
	}
	p := &DemuxPort{d: d, types: slices.Clone(etherTypes), ch: make(chan []byte, d.queue), closed: make(chan struct{})}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.err != nil {
		return nil, d.errLocked()
	}
	select {
	case <-d.done:
		return nil, d.errLocked()
	default:
	}
	d.ports = append(d.ports, p)
	return p, nil
}

func (d *Demux) errLocked() error {
	if d.err != nil {
		return d.err
	}
	return ErrClosed
}

// Err returns the receive error that stopped the Demux, [ErrClosed]
// after Close, or nil while it is running.
func (d *Demux) Err() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return d.err
	}
	if d.closed {
		return ErrClosed
	}
	select {
	case <-d.done:
		return ErrClosed
	default:
		return nil
	}
}

// Stats returns a snapshot of the counters.
func (d *Demux) Stats() DemuxStats {
	return DemuxStats{Received: d.received.Load(), Unmatched: d.unmatched.Load(), Overflow: d.overflow.Load()}
}

// Close closes the shared transport and every port, and waits for the
// receive goroutine to stop. It is idempotent.
func (d *Demux) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		<-d.done
		return nil
	}
	d.closed = true
	d.mu.Unlock()
	err := d.t.Close()
	<-d.done
	return err
}

func (d *Demux) remove(p *DemuxPort) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i := slices.Index(d.ports, p); i >= 0 {
		d.ports = slices.Delete(d.ports, i, i+1)
	}
}

// DemuxPort is one receiver of a [Demux]. It implements [Transport].
type DemuxPort struct {
	d     *Demux
	types []uint16
	ch    chan []byte

	mu        sync.RWMutex // guards ch sends against close
	closeOnce sync.Once
	closed    chan struct{}
	err       error
}

var _ Transport = (*DemuxPort)(nil)

func (p *DemuxPort) deliver(frame []byte) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	select {
	case <-p.closed:
		return true
	default:
	}
	select {
	case p.ch <- frame:
		return true
	default:
		return false
	}
}

func (p *DemuxPort) closeWith(err error) {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.err = err
		close(p.closed)
		p.mu.Unlock()
	})
}

// EtherTypes returns the EtherTypes this port receives.
func (p *DemuxPort) EtherTypes() []uint16 { return slices.Clone(p.types) }

// Send sends frame on the shared transport.
func (p *DemuxPort) Send(frame []byte) error {
	select {
	case <-p.closed:
		return p.closeErr()
	default:
	}
	return p.d.t.Send(frame)
}

// Recv returns the next frame for this port. Frames already queued are
// still returned after the Demux stopped; then Recv returns
// [ErrClosed] or the Demux's receive error.
func (p *DemuxPort) Recv(ctx context.Context) ([]byte, error) {
	select {
	case f := <-p.ch:
		return f, nil
	default:
	}
	select {
	case f := <-p.ch:
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closed:
		select {
		case f := <-p.ch:
			return f, nil
		default:
		}
		return nil, p.closeErr()
	}
}

func (p *DemuxPort) closeErr() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.err != nil {
		return p.err
	}
	return ErrClosed
}

// HardwareAddr returns the shared transport's address.
func (p *DemuxPort) HardwareAddr() MAC { return p.d.t.HardwareAddr() }

// Close detaches the port. The shared transport stays open.
func (p *DemuxPort) Close() error {
	p.d.remove(p)
	p.closeWith(nil)
	return nil
}
