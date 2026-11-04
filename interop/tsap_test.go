//go:build interop

// SPDX-License-Identifier: MIT

package interop

// COTP transport-selector (TSAP) interoperability, otfabric/go-iec61850#28.
//
// An ABB REB500 refused a go-iec61850 client whose COTP CR carried no
// T-selectors with a Disconnect Request, reason 3 (address unknown). None of
// the peer servers checks the selectors, so nothing caught it. These tests
// close both directions:
//
//   - go client → strict gate → peer IED server. The gate is a test-side
//     proxy that models such an IED (X.224 §13.3.4 CR parameters 0xC1, 0xC2,
//     0xC0): it refuses a CR whose called T-selector is absent or different,
//     or that carries no TPDU size, with DR reason 3, and otherwise forwards
//     the connection to a real peer server. It is our code, so it lives
//     here and not in iec61850-interop, whose stacks are all independent.
//   - peer client → strict go server: the independent clients, with their
//     default selectors and with --local-tsel/--remote-tsel (iec61850-interop
//     v1.2.0), against a go-iec61850 server that requires its selector.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-cotp"
	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-mms/transport/iso"
)

// gateCR records what the gate saw in one CR and what it did.
type gateCR struct {
	calling, called []byte
	tpduSize        *uint8
	accepted        bool
}

// tsapGate is a strict COTP gate in front of a peer server.
type tsapGate struct {
	addr     string
	required []byte

	mu  sync.Mutex
	crs []gateCR
}

func (g *tsapGate) seen() []gateCR {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gateCR(nil), g.crs...)
}

// startTSAPGate listens on loopback and forwards each connection whose CR
// carries the required called T-selector and a TPDU size to upstream; any
// other CR is answered with DR reason 3 (address unknown).
func startTSAPGate(t *testing.T, upstream string, required []byte) *tsapGate {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &tsapGate{addr: ln.Addr().String(), required: required}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() { g.serve(c, upstream) })
		}
	}()
	return g
}

func (g *tsapGate) serve(c net.Conn, upstream string) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	frame, payload, err := readTPKT(c)
	if err != nil {
		return
	}
	d, err := cotp.Decode(payload)
	if err != nil || d.CR == nil {
		return
	}
	cr := d.CR
	ok := cr.CalledSelector != nil && bytes.Equal(cr.CalledSelector, g.required) && cr.TPDUSize != nil
	g.mu.Lock()
	g.crs = append(g.crs, gateCR{calling: cr.CallingSelector, called: cr.CalledSelector, tpduSize: cr.TPDUSize, accepted: ok})
	g.mu.Unlock()
	if !ok {
		dr, err := (&cotp.DR{DestinationRef: cr.SourceRef, Reason: uint8(cotp.ReasonAddressUnknown)}).MarshalBinary()
		if err == nil {
			_ = writeTPKT(c, dr)
		}
		return
	}
	up, err := net.DialTimeout("tcp", upstream, 5*time.Second)
	if err != nil {
		return
	}
	defer func() { _ = up.Close() }()
	if _, err := up.Write(frame); err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
	<-done
}

// readTPKT reads one RFC 1006 TPKT and returns the whole frame and its
// payload.
func readTPKT(r io.Reader) (frame, payload []byte, err error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[2:]))
	if hdr[0] != 3 || n < 7 {
		return nil, nil, fmt.Errorf("bad TPKT header % x", hdr)
	}
	frame = make([]byte, n)
	copy(frame, hdr)
	if _, err := io.ReadFull(r, frame[4:]); err != nil {
		return nil, nil, err
	}
	return frame, frame[4:], nil
}

func writeTPKT(w io.Writer, payload []byte) error {
	frame := make([]byte, 4+len(payload))
	frame[0] = 3
	binary.BigEndian.PutUint16(frame[2:], uint16(len(frame))) //nolint:gosec // a CR/DR is far below 64 KiB
	copy(frame[4:], payload)
	_, err := w.Write(frame)
	return err
}

// TestTSAPGate_DefaultClientAssociates: the default go-iec61850 client sends
// T-selector 0001/0001 and a TPDU size, so a strict IED accepts it and the
// association and a read through to the peer server work.
func TestTSAPGate_DefaultClientAssociates(t *testing.T) {
	h := startIEDServer(t)
	g := startTSAPGate(t, h.addr, []byte{0x00, 0x01})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := (&iedServerHandle{addr: g.addr, iedName: h.iedName}).dial(t, ctx)
	defer func() { _ = c.Close(context.Background()) }()
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Fatalf("ListLogicalDevices through the gate: %v", err)
	}
	crs := g.seen()
	if len(crs) == 0 {
		t.Fatal("gate saw no CR")
	}
	last := crs[len(crs)-1]
	if !last.accepted || !bytes.Equal(last.calling, []byte{0, 1}) || *last.tpduSize != 0x0A {
		t.Fatalf("CR calling=%x called=%x size=%v accepted=%v, want 0001/0001/0x0A", last.calling, last.called, last.tpduSize, last.accepted)
	}
}

// TestTSAPGate_OmittedSelectorRefused reproduces go-iec61850#28: a client
// that sends no called T-selector (what go-iec61850 v1.5.0 sent by
// default) is refused with reason 3.
func TestTSAPGate_OmittedSelectorRefused(t *testing.T) {
	h := startIEDServer(t)
	g := startTSAPGate(t, h.addr, []byte{0x00, 0x01})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := iec61850.DialOptions{IEDName: h.iedName}
	opts.MMS.Transport.LocalTSelector = []byte{}
	opts.MMS.Transport.RemoteTSelector = []byte{}
	_, err := iec61850.Dial(ctx, g.addr, opts)
	var rej *cotp.RejectionError
	if !errors.As(err, &rej) || rej.Reason != cotp.ReasonAddressUnknown {
		t.Fatalf("err = %v, want a COTP rejection with reason 3 (address unknown)", err)
	}
}

// TestTSAPGate_ConfiguredSelector: an IED configured with OSI-TSEL 00000001
// (the form of the IEC 61850-8-1 SCL example) accepts a client configured
// with it and refuses the default 0001.
func TestTSAPGate_ConfiguredSelector(t *testing.T) {
	h := startIEDServer(t)
	four := []byte{0x00, 0x00, 0x00, 0x01}
	g := startTSAPGate(t, h.addr, four)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := iec61850.Dial(ctx, g.addr, iec61850.DialOptions{IEDName: h.iedName})
	var rej *cotp.RejectionError
	if !errors.As(err, &rej) || rej.Reason != cotp.ReasonAddressUnknown {
		t.Fatalf("default 0001 against 00000001: err = %v, want reason 3", err)
	}

	opts := iec61850.DialOptions{IEDName: h.iedName}
	opts.MMS.Transport.RemoteTSelector = four
	c, err := iec61850.Dial(ctx, g.addr, opts)
	if err != nil {
		t.Fatalf("configured 00000001: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
}

// TestPeerClientTSelectors runs the independent clients against a go server
// that requires its called T-selector: with their own defaults, and with
// 1-, 2- and 4-octet selectors given on the command line. A mismatch must
// fail the association.
func TestPeerClientTSelectors(t *testing.T) {
	type client struct {
		name  string
		start func(t *testing.T, port int, extra ...string) <-chan iedClientResult
	}
	clients := []client{
		{peer().name, startIEDClientAdapter},
		{"iec61850bean", startIEC61850BeanClientAdapter},
	}
	cases := []struct {
		name     string
		required []byte
		args     []string
		ok       bool
	}{
		// libiec61850, iec61850-rust, dscsystems and iec61850bean all call 0001.
		{name: "stack default", required: []byte{0x00, 0x01}, ok: true},
		{name: "one octet", required: []byte{0x01}, args: []string{"--local-tsel", "01", "--remote-tsel", "01"}, ok: true},
		{name: "four octets", required: []byte{0, 0, 0, 1}, args: []string{"--local-tsel", "00000001", "--remote-tsel", "00000001"}, ok: true},
		{name: "mismatch", required: []byte{0x00, 0x02}, args: []string{"--remote-tsel", "0001"}},
	}
	for _, cl := range clients {
		for _, tc := range cases {
			t.Run(cl.name+"/"+tc.name, func(t *testing.T) {
				srv := startGoIEDServerOpts(t, iso.WithCalledTSelector(tc.required))
				results := collectIEDResults(t, cl.start(t, srv.port, tc.args...))
				for _, r := range results {
					skipUnsupported(t, "--local-tsel/--remote-tsel", r.Event, r.Code, r.Error, r.Reason)
				}
				r, found := findIEDOp(t, results, "associate")
				if !found {
					t.Fatalf("%s-ied-client printed no associate", cl.name)
				}
				if r.OK != tc.ok {
					t.Fatalf("%s-ied-client %v against required %x: associate ok=%v (%s), want %v",
						cl.name, tc.args, tc.required, r.OK, r.Error, tc.ok)
				}
				if tc.ok {
					if c, found := findIEDOp(t, results, "conclude"); !found || !c.OK {
						t.Fatalf("%s-ied-client: conclude %+v", cl.name, c)
					}
				}
			})
		}
	}
}
