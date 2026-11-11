// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"crypto/tls"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func entryOf(id uint64, vals ...int64) ReportEntry {
	e := ReportEntry{ID: id, TimeOfEntry: time.UnixMilli(int64(id))}
	for _, v := range vals {
		e.Values = append(e.Values, mms.NewInteger(v))
		e.Reasons = append(e.Reasons, ReasonDataChanged)
		e.Inclusion = append(e.Inclusion, true)
	}
	return e
}

func ids(es []ReportEntry) []uint64 {
	var out []uint64
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func TestMemoryReportBuffer_Basics(t *testing.T) {
	b := NewMemoryReportBuffer(0, 0)
	if n, by := b.Limits(); n != defaultRCBBufMax || by != 0 {
		t.Fatalf("default limits = %d, %d", n, by)
	}
	if _, ok := b.Oldest(); ok {
		t.Error("Oldest of an empty buffer")
	}
	if _, ok := b.Newest(); ok {
		t.Error("Newest of an empty buffer")
	}
	if _, ok := b.After(0); ok {
		t.Error("After of an empty buffer")
	}
	for id := uint64(1); id <= 5; id++ {
		if d := b.Append(entryOf(id*2, 1)); d != nil {
			t.Fatalf("discarded %v", ids(d))
		}
	}
	if b.Len() != 5 {
		t.Fatalf("Len = %d", b.Len())
	}
	for _, c := range []struct {
		after, want uint64
		ok          bool
	}{{0, 2, true}, {2, 4, true}, {3, 4, true}, {9, 10, true}, {10, 0, false}, {^uint64(0), 0, false}} {
		e, ok := b.After(c.after)
		if ok != c.ok || e.ID != c.want {
			t.Errorf("After(%d) = %d, %v; want %d, %v", c.after, e.ID, ok, c.want, c.ok)
		}
	}
	if _, ok := b.Get(4); !ok {
		t.Error("Get(4)")
	}
	if _, ok := b.Get(5); ok {
		t.Error("Get(5) found an absent entry")
	}
	if _, ok := b.Get(99); ok {
		t.Error("Get(99) found an absent entry")
	}
	before := b.Bytes()
	b.Remove(6) // middle
	b.Remove(2) // oldest
	b.Remove(7) // absent
	b.Remove(99)
	if got := ids(b.Entries()); !slices.Equal(got, []uint64{4, 8, 10}) {
		t.Fatalf("after Remove: %v", got)
	}
	if b.Bytes() >= before {
		t.Errorf("Bytes %d not reduced from %d", b.Bytes(), before)
	}
	if o, _ := b.Oldest(); o.ID != 4 {
		t.Errorf("Oldest = %d", o.ID)
	}
	if n, _ := b.Newest(); n.ID != 10 {
		t.Errorf("Newest = %d", n.ID)
	}
	b.Purge()
	if b.Len() != 0 || b.Bytes() != 0 {
		t.Errorf("after Purge: %d entries, %d bytes", b.Len(), b.Bytes())
	}
}

// TestMemoryReportBuffer_EntryLimit: a full buffer discards its oldest
// entries for a new one (7-2 §17.2.3.2.2.8); a lower limit takes effect
// with the next Append.
func TestMemoryReportBuffer_EntryLimit(t *testing.T) {
	b := NewMemoryReportBuffer(3, 0)
	var discarded []uint64
	for id := uint64(1); id <= 5; id++ {
		discarded = append(discarded, ids(b.Append(entryOf(id, 1)))...)
	}
	if !slices.Equal(discarded, []uint64{1, 2}) || !slices.Equal(ids(b.Entries()), []uint64{3, 4, 5}) {
		t.Fatalf("discarded %v, kept %v", discarded, ids(b.Entries()))
	}
	b.SetLimits(1, 0)
	if d := ids(b.Append(entryOf(6, 1))); !slices.Equal(d, []uint64{3, 4, 5}) {
		t.Errorf("after SetLimits(1): discarded %v", d)
	}
}

// TestMemoryReportBuffer_ByteLimit: entries are discarded until the new
// one fits the byte limit; one entry larger than the limit is still kept.
func TestMemoryReportBuffer_ByteLimit(t *testing.T) {
	one := entryOf(1, 7).EncodedSize()
	b := NewMemoryReportBuffer(100, 3*one)
	for id := uint64(1); id <= 3; id++ {
		if d := b.Append(entryOf(id, 7)); d != nil {
			t.Fatalf("entry %d discarded %v", id, ids(d))
		}
	}
	if b.Bytes() != 3*one {
		t.Fatalf("Bytes = %d, want %d", b.Bytes(), 3*one)
	}
	if d := ids(b.Append(entryOf(4, 7))); !slices.Equal(d, []uint64{1}) {
		t.Errorf("fourth entry discarded %v, want [1]", d)
	}
	big := entryOf(5, 1, 2, 3, 4, 5, 6, 7, 8)
	if d := ids(b.Append(big)); !slices.Equal(d, []uint64{2, 3, 4}) {
		t.Errorf("big entry discarded %v, want [2 3 4]", d)
	}
	if got := ids(b.Entries()); !slices.Equal(got, []uint64{5}) {
		t.Errorf("kept %v, want the big entry alone", got)
	}
	if b.SetLimits(10, -1); func() bool { _, by := b.Limits(); return by != 0 }() {
		t.Error("a negative byte limit is not 'no limit'")
	}
}

func TestReportEntry_EncodedSize(t *testing.T) {
	e := entryOf(1, 1, 2)
	e.Inclusion[1] = false
	small := e.EncodedSize()
	e.Inclusion[1] = true
	if e.EncodedSize() <= small {
		t.Error("an included member adds no size")
	}
	// Inclusion longer than Values (a malformed custom entry) is safe.
	e.Inclusion = append(e.Inclusion, true)
	_ = e.EncodedSize()
}

// TestSpec_7_2_17_2_3_2_2_8_ByteLimitSetsBufOvfl: a BRCB whose buffer
// reaches its byte limit while the client is away discards the oldest
// entries; the first report after the enable carries BufOvfl=TRUE and
// the remaining entries are replayed in order.
func TestSpec_7_2_17_2_3_2_2_8_ByteLimitSetsBufOvfl(t *testing.T) {
	limit := entryOf(1, 1).EncodedSize() * 3
	srv, err := NewServer(specModel(), ServerOptions{
		NewReportBuffer: func(ld, rcb string) ReportBuffer {
			if ld != "LD1" || rcb != specBRCB {
				t.Errorf("NewReportBuffer(%q, %q)", ld, rcb)
			}
			return NewMemoryReportBuffer(1000, limit)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableReports()
	t.Cleanup(srv.Close)
	ctx := context.Background()
	for v := range int64(6) {
		srv.SetValue(ctx, specModKey, mms.NewInteger(v+1))
	}
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	for k, want := range []int64{4, 5, 6} {
		r := recvReport(t, sub)
		if v, _ := r.Values[0].Int64(); v != want {
			t.Errorf("report %d value = %d, want %d", k, v, want)
		}
		if r.BufOvfl != (k == 0) {
			t.Errorf("report %d BufOvfl = %v", k, r.BufOvfl)
		}
		if id := decodeEntryID(r.EntryID); id != uint64(want) {
			t.Errorf("report %d EntryID = %d, want %d", k, id, want)
		}
	}
	expectNoReport(t, sub, 200*time.Millisecond)
}

// lossyBuffer cannot store an entry while full is set (as a persistent
// buffer on a full disk): it returns the new entry as discarded.
type lossyBuffer struct {
	*MemoryReportBuffer
	full bool
}

func (b *lossyBuffer) Append(e ReportEntry) []ReportEntry {
	if b.full {
		return []ReportEntry{e}
	}
	return b.MemoryReportBuffer.Append(e)
}

// TestReportBuffer_UnstorableEntrySetsBufOvfl: an entry the buffer
// cannot keep is a loss the client learns of through BufOvfl, and a GI
// that cannot be stored is not left pending.
func TestReportBuffer_UnstorableEntrySetsBufOvfl(t *testing.T) {
	lb := &lossyBuffer{MemoryReportBuffer: NewMemoryReportBuffer(0, 0)}
	srv, err := NewServer(specModel(), ServerOptions{NewReportBuffer: func(string, string) ReportBuffer { return lb }})
	if err != nil {
		t.Fatal(err)
	}
	engine := srv.EnableReports()
	t.Cleanup(srv.Close)
	ctx := context.Background()
	rt := engine.rcbs["LD1/"+specBRCB]
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	rt.mu.Lock()
	lb.full = true
	rt.mu.Unlock()
	srv.SetValue(ctx, specModKey, mms.NewInteger(2))
	if err := engine.HandleRCBWrite(ctx, "LD1", specBRCB, "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	if err := engine.HandleRCBWrite(ctx, "LD1", specBRCB, "GI", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.discarded || rt.giPending != 0 || lb.Len() != 1 {
		t.Errorf("discarded=%v giPending=%d len=%d, want true, 0, 1", rt.discarded, rt.giPending, lb.Len())
	}
}

// TestReportBuffer_AdoptsExistingEntries: a buffer that already holds
// entries (a persistent one after a restart) is continued: EntryID and
// TimeOfEntry show its newest entry, new IDs follow it, a resync to one
// of its entries works and its GI entries are not replayed.
func TestReportBuffer_AdoptsExistingEntries(t *testing.T) {
	prev := NewMemoryReportBuffer(0, 0)
	prev.Append(entryOf(41, 1))
	gi := entryOf(42, 2)
	gi.GI = true
	gi.Reasons[0] = ReasonGI
	prev.Append(gi)
	prev.Append(entryOf(43, 3))

	srv, err := NewServer(specModel(), ServerOptions{NewReportBuffer: func(_, rcb string) ReportBuffer {
		if rcb == specBRCB {
			return prev
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableReports()
	t.Cleanup(srv.Close)
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	if id := decodeEntryID(octets(t, rawRead(t, c, specBRCB+"$EntryID"))); id != 43 {
		t.Fatalf("EntryID after start = %d, want 43", id)
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(4))
	if err := rawWrite(c, specBRCB+"$EntryID", mms.NewOctetString(encodeEntryID(41))); err != nil {
		t.Fatalf("resync to an adopted entry: %v", err)
	}
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	for _, want := range []uint64{43, 44} {
		r := recvReport(t, sub)
		if id := decodeEntryID(r.EntryID); id != want {
			t.Errorf("EntryID = %d, want %d", id, want)
		}
	}
	expectNoReport(t, sub, 200*time.Millisecond)
}

func octets(t *testing.T, v *mms.Value) []byte {
	t.Helper()
	b, ok := v.OctetString()
	if !ok {
		t.Fatalf("not an octet string: %v", v)
	}
	return b
}

// TestRespGate: the gate is busy from a received request until the
// server reads again (its response is then sent) or closes; a waiting
// report goes out then, or after gateTimeout.
func TestRespGate(t *testing.T) {
	var nilGate *respGate
	select {
	case <-nilGate.idleCh():
	default:
		t.Fatal("a nil gate is not idle")
	}
	clientT, serverT := loopbackPair()
	g := newRespGate()
	gt := &gatedTransport{Transport: serverT, gate: g}
	if gt.RemoteAddr() != nil || gt.TLSConnectionState() != nil || gt.Unwrap() != serverT {
		t.Error("optional methods of a plain transport")
	}
	ctx := context.Background()
	if err := clientT.Send(ctx, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := gt.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	idle := g.idleCh()
	select {
	case <-idle:
		t.Fatal("idle while a request is processed")
	default:
	}

	re := &ReportEngine{stopCh: make(chan struct{})}
	done := make(chan bool, 1)
	go func() { done <- re.awaitGate(g, nil) }()
	select {
	case <-done:
		t.Fatal("report released before the response")
	case <-time.After(30 * time.Millisecond):
	}
	recvDone := make(chan error, 1)
	go func() { _, err := gt.Receive(ctx); recvDone <- err }() // the server reads the next request
	if !<-done {
		t.Fatal("awaitGate = false")
	}
	if err := clientT.Send(ctx, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if err := <-recvDone; err != nil {
		t.Fatal(err)
	}
	// Busy again; Close releases it.
	if err := gt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.idleCh():
	default:
		t.Error("busy after Close")
	}
	g.setIdle() // idempotent

	// A request that never completes holds a report for gateTimeout only.
	old := gateTimeout
	gateTimeout = 20 * time.Millisecond
	t.Cleanup(func() { gateTimeout = old })
	stuck := newRespGate()
	stuck.setBusy()
	stuck.setBusy() // idempotent
	if !re.awaitGate(stuck, nil) {
		t.Error("awaitGate after the timeout = false")
	}
	stop := make(chan struct{})
	close(stop)
	gateTimeout = time.Hour
	if re.awaitGate(stuck, stop) {
		t.Error("awaitGate after stop = true")
	}
	if respGateFrom(context.Background()) != nil {
		t.Error("gate without one in the context")
	}
}

// remoteAddrTransport adds RemoteAddr and TLS state to a transport.
type remoteAddrTransport struct{ mms.Transport }

func (remoteAddrTransport) RemoteAddr() net.Addr { return netAddr("peer") }

type netAddr string

func (a netAddr) Network() string { return "test" }
func (a netAddr) String() string  { return string(a) }

// tlsStateTransport adds a TLS connection state.
type tlsStateTransport struct{ remoteAddrTransport }

func (tlsStateTransport) TLSConnectionState() *tls.ConnectionState {
	return &tls.ConnectionState{}
}

// TestRespGate_ForwardsRemoteAddr: the wrapped transport keeps the
// optional interfaces go-mms uses for logging and authentication.
func TestRespGate_ForwardsRemoteAddr(t *testing.T) {
	_, serverT := loopbackPair()
	gt := &gatedTransport{Transport: tlsStateTransport{remoteAddrTransport{serverT}}, gate: newRespGate()}
	if a := gt.RemoteAddr(); a == nil || a.String() != "peer" {
		t.Errorf("RemoteAddr = %v", a)
	}
	if gt.TLSConnectionState() == nil {
		t.Error("TLSConnectionState not forwarded")
	}
}

// TestChaos_BufTmConcurrentChanges: changes of several members from many
// goroutines, with GI requests and enable/disable of both RCBs, against a
// short BufTm on the wall clock and a small bounded buffer. Entry IDs stay
// strictly increasing, no entry includes a member twice by construction,
// and nothing leaks after Close. Run with -race.
func TestChaos_BufTmConcurrentChanges(t *testing.T) {
	checkGoroutines(t, func() {
		srv, err := NewServer(specModel(), ServerOptions{
			NewReportBuffer: func(string, string) ReportBuffer { return NewMemoryReportBuffer(16, 512) },
		})
		if err != nil {
			t.Fatal(err)
		}
		engine := srv.EnableReports()
		ctx := context.Background()
		for _, rcb := range []string{specBRCB, specURCB} {
			for _, w := range []struct {
				attr string
				v    *mms.Value
			}{
				{"DatSet", mms.NewVisibleString("LD1/LLN0$dsTwo")},
				{"BufTm", mms.NewUnsigned(3)},
			} {
				if err := engine.HandleRCBWrite(ctx, "LD1", rcb, w.attr, w.v); err != nil {
					t.Fatal(err)
				}
			}
		}
		c := dialLoopback(t, ctx, srv)
		sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)

		var lastID atomic.Uint64
		var bad atomic.Int32
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			for r := range sub.Reports() {
				id := decodeEntryID(r.EntryID)
				if id <= lastID.Load() {
					bad.Add(1)
				}
				lastID.Store(id)
			}
		}()

		var wg sync.WaitGroup
		stop := make(chan struct{})
		for g := range 6 {
			wg.Go(func() {
				key := specModKey
				if g%2 == 1 {
					key = specBehKey
				}
				for i := int64(0); ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					srv.SetValue(ctx, key, mms.NewInteger(i*10+int64(g)))
				}
			})
		}
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = c.TriggerGI(ctx, "LD1", specBRCB)
				_ = engine.HandleRCBWrite(ctx, "LD1", specURCB, "RptEna", mms.NewBoolean(true))
				_ = engine.HandleRCBWrite(ctx, "LD1", specURCB, "GI", mms.NewBoolean(true))
				_ = engine.HandleRCBWrite(ctx, "LD1", specURCB, "RptEna", mms.NewBoolean(false))
				time.Sleep(2 * time.Millisecond)
			}
		})
		time.Sleep(300 * time.Millisecond)
		close(stop)
		wg.Wait()

		rt := engine.rcbs["LD1/"+specBRCB]
		rt.mu.Lock()
		got := rt.buf.(*MemoryReportBuffer).Entries()
		rt.mu.Unlock()
		if len(got) > 16 {
			t.Errorf("buffer holds %d entries, limit 16", len(got))
		}
		if !slices.IsSortedFunc(got, func(a, b ReportEntry) int { return int(a.ID) - int(b.ID) }) {
			t.Errorf("buffer IDs not increasing: %v", ids(got))
		}
		dropAssociation(c)
		srv.Close()
		_ = sub.Close()
		<-readerDone
		if bad.Load() != 0 {
			t.Errorf("%d reports with a non-increasing EntryID", bad.Load())
		}
	})
}

// TestChaos_GateConnectionDropsWhileWaiting: a report waiting for a
// response that never comes is released when the association drops, and
// the engine and connections stop without leaks.
func TestChaos_GateConnectionDropsWhileWaiting(t *testing.T) {
	checkGoroutines(t, func() {
		srv, err := NewServer(specModel(), ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		engine := srv.EnableReports()
		ctx := context.Background()
		for range 5 {
			c := dialLoopback(t, ctx, srv)
			_ = subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
			// Simulate a request in progress on the owner's association.
			rt := engine.rcbs["LD1/"+specBRCB]
			rt.mu.Lock()
			g := rt.sendGate
			rt.mu.Unlock()
			if g == nil {
				t.Fatal("no gate for an association served by Server.Serve")
			}
			g.setBusy()
			srv.SetValue(ctx, specModKey, mms.NewInteger(time.Now().UnixNano()))
			dropAssociation(c)
			waitUntil(t, "release", func() bool { return rcbOwner(engine, specBRCB) == nil })
		}
		srv.Close()
	})
}
