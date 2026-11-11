//go:build interop

// SPDX-License-Identifier: MIT

package interop

// R-GOOSE and R-SV interoperability tests: package rsession (IEC TR
// 61850-90-5 session protocol over UDP, HMAC-SHA256) against the adapter
// commands that take --transport udp (iec61850-interop contract 1.4,
// docs/routable.md), with the goose-v1 and sv-v1 fixtures.
//
// UDP needs no layer-2 segment and no CAP_NET_RAW: the adapter container
// publishes its UDP port 102 on 127.0.0.1 for a Go publisher, and reaches
// a Go subscriber at host.docker.internal (the host gateway). The tests
// run against every stack whose --capabilities lists the udp transport
// and skip when none does.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/rsession"
	"github.com/otfabric/go-iec61850/sv"
)

// The adapters' and the tests' key: ID 1, 32 octets.
const (
	rKeyID  = 1
	rKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

// rProfiles are the security profiles every test runs: session protocol
// version and MAC, as the adapter's --session-version and --mac name them.
var rProfiles = []struct {
	name    string
	version uint16
	mac     rsession.MACAlgorithm
	flag    string
}{
	{"v1-hmac-sha256-80", 1, rsession.MACHMACSHA256_80, "hmac-sha256-80"},
	{"v2-hmac-sha256-128", 2, rsession.MACHMACSHA256_128, "hmac-sha256-128"},
	{"v2-hmac-sha256-256", 2, rsession.MACHMACSHA256_256, "hmac-sha256-256"},
}

func rKey(t *testing.T, mac rsession.MACAlgorithm, material string) rsession.Key {
	t.Helper()
	b, err := hex.DecodeString(material)
	if err != nil {
		t.Fatal(err)
	}
	return rsession.Key{ID: rKeyID, Material: b, MAC: mac}
}

func rRing(t *testing.T, mac rsession.MACAlgorithm) *rsession.KeyRing {
	t.Helper()
	r, err := rsession.NewKeyRing(rKey(t, mac, rKeyHex))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// routableStacks are the stacks of l2Stacks whose GOOSE and SV commands
// take --transport udp.
func routableStacks(t *testing.T) []peerStack {
	t.Helper()
	var out []peerStack
	for _, st := range l2Stacks() {
		img := st.dockerImage()
		if img == "" {
			continue
		}
		b, err := exec.Command("docker", "run", "--rm", "--entrypoint", st.cmd("server"), img, "--capabilities").Output()
		if err != nil {
			t.Logf("%s: no --capabilities from %s: %v", st.name, img, err)
			continue
		}
		var caps struct {
			Transports []string `json:"transports"`
		}
		line, _, _ := bytes.Cut(b, []byte("\n"))
		if json.Unmarshal(line, &caps) != nil {
			continue
		}
		for _, tr := range caps.Transports {
			if tr == "udp" {
				out = append(out, st)
			}
		}
	}
	if len(out) == 0 {
		t.Skip("no adapter image takes --transport udp (iec61850-interop contract 1.4)")
	}
	return out
}

// rEvent is any adapter event: the common fields, and the line for the
// GOOSE or SV fields.
type rEvent struct {
	Event   string `json:"event"`
	Fixture string `json:"fixture"`
	Adapter string `json:"adapter"`
	Version string `json:"version"`
	Role    string `json:"role"`
	From    string `json:"from"`
	Reason  string `json:"reason"`
	Code    string `json:"code"`
	Error   string `json:"error"`
	line    []byte
}

func (e rEvent) goose(t *testing.T) gooseEvent {
	t.Helper()
	var g gooseEvent
	if err := json.Unmarshal(e.line, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func (e rEvent) sv(t *testing.T) svEvent {
	t.Helper()
	var s svEvent
	if err := json.Unmarshal(e.line, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

type rAdapter struct {
	name   string
	events chan rEvent
	stdin  io.WriteCloser
	addr   netip.AddrPort // a subscriber's UDP port 102 on the host
}

// startRAdapter runs proto-role (goose or sv, publisher or subscriber) of
// st with --transport udp and the test key, and waits for its ready
// event. A subscriber's port 102/udp is published on 127.0.0.1.
func startRAdapter(t *testing.T, st peerStack, proto, role, mac string, version uint16, args ...string) *rAdapter {
	t.Helper()
	name := dockerContainerName(t) + "-r" + proto + "-" + role
	dargs := []string{"run", "--rm", "-i", "--name", name, "--add-host", "host.docker.internal:host-gateway"}
	if role == "subscriber" {
		dargs = append(dargs, "-p", "127.0.0.1::102/udp")
	}
	dargs = append(dargs, "--entrypoint", st.name+"-"+proto+"-"+role, st.dockerImage(),
		"--transport", "udp", "--key-id", strconv.Itoa(rKeyID), "--key", rKeyHex, "--mac", mac,
		"--session-version", strconv.Itoa(int(version)))
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "docker", append(dargs, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start %s: %v", name, err)
	}
	a := &rAdapter{name: name, events: make(chan rEvent, 4096), stdin: stdin}
	go func() {
		defer close(a.events)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		for sc.Scan() {
			ev := rEvent{line: append([]byte(nil), sc.Bytes()...)}
			if json.Unmarshal(ev.line, &ev) != nil {
				continue
			}
			select {
			case a.events <- ev:
			default: // a slow test must not block the adapter
			}
		}
	}()
	t.Cleanup(func() {
		dockerStop(name)
		cancel()
		_ = cmd.Wait()
	})
	ev, ok := a.next(t, iedReadyTimeout, func(ev rEvent) bool { return ev.Event == "ready" })
	if !ok {
		t.Fatalf("%s: no ready event", name)
	}
	want := gooseFixtureRev
	if proto == "sv" {
		want = svFixtureRev
	}
	if ev.Fixture != want || ev.Adapter != st.name || ev.Role != role || ev.Version == "" {
		t.Errorf("%s ready event: %s", name, ev.line)
	}
	if os.Getenv("CI") != "" && ev.Version == "dev" {
		t.Errorf("adapter version is %q in CI; pin a released image digest", ev.Version)
	}
	if role == "subscriber" {
		out, err := exec.Command("docker", "port", name, "102/udp").Output()
		if err != nil {
			t.Fatalf("docker port %s: %v", name, err)
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		if a.addr, err = netip.ParseAddrPort(line); err != nil {
			t.Fatalf("docker port %s: %q: %v", name, out, err)
		}
	}
	return a
}

// next returns the next event matching pred, skipping others.
func (a *rAdapter) next(t *testing.T, timeout time.Duration, pred func(rEvent) bool) (rEvent, bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-a.events:
			if !ok {
				return rEvent{}, false
			}
			if ev.Event == "error" {
				if ev.Code == unsupportedError {
					t.Skipf("%s: unsupported: %s", a.name, ev.Reason)
				}
				t.Logf("%s: error event: %s", a.name, ev.Error)
			}
			if pred(ev) {
				return ev, true
			}
		case <-deadline:
			return rEvent{}, false
		}
	}
}

func (a *rAdapter) send(t *testing.T, cmd any) {
	t.Helper()
	b, _ := json.Marshal(cmd)
	if _, err := a.stdin.Write(append(b, '\n')); err != nil {
		t.Fatalf("%s stdin: %v", a.name, err)
	}
}

// expectReject waits for a reject event with reason.
func (a *rAdapter) expectReject(t *testing.T, reason string) {
	t.Helper()
	ev, ok := a.next(t, 10*time.Second, func(e rEvent) bool { return e.Event == "reject" })
	if !ok {
		t.Fatalf("%s: no reject event (want %s)", a.name, reason)
	}
	if ev.Reason != reason || ev.From == "" {
		t.Errorf("%s: reject %s, want reason %s", a.name, ev.line, reason)
	}
}

// openRSender opens a Go sender to addr.
func openRSender(t *testing.T, si rsession.SessionID, addr netip.AddrPort, keys rsession.KeyProvider, version uint16) *rsession.Transport {
	t.Helper()
	tr, err := rsession.Open(rsession.Config{SI: si, RemoteAddr: addr, Keys: keys, Version: version})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// openRReceiver opens a Go receiver on all addresses, reachable from the
// containers at host.docker.internal:port.
func openRReceiver(t *testing.T, si rsession.SessionID, keys rsession.KeyProvider, dst l2MAC, rejects *atomic.Int64) (*rsession.Transport, int) {
	t.Helper()
	tr, err := rsession.Open(rsession.Config{
		SI: si, LocalAddr: netip.AddrPortFrom(netip.IPv4Unspecified(), 0), Keys: keys, FrameDst: dst,
		OnReject: func(netip.AddrPort, error) { rejects.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr, int(tr.LocalAddr().Port())
}

// dialRaw returns a function that sends one datagram to addr, always from
// the same socket (a receiver's replay window is per sender).
func dialRaw(t *testing.T, addr netip.AddrPort) func([]byte) {
	t.Helper()
	c, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(addr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return func(b []byte) {
		t.Helper()
		if _, err := c.Write(b); err != nil {
			t.Fatal(err)
		}
	}
}

// ---------------------------------------------------------------------------
// R-GOOSE
// ---------------------------------------------------------------------------

// TestRGOOSE_GoPublisher publishes gcbAll from a goose.Publisher over
// rsession to each adapter subscriber: both states with the fixture
// values, then SPDUs the subscriber must refuse: one under another key
// with the same key ID, an unsecured one and a replayed one.
func TestRGOOSE_GoPublisher(t *testing.T) {
	stacks := routableStacks(t)
	fix := loadGooseFixture(t)
	for _, st := range stacks {
		for _, p := range rProfiles {
			t.Run(st.name+"/"+p.name, func(t *testing.T) {
				cb, fx := fix.controlBlock(t, "gcbAll")
				sub := startRAdapter(t, st, "goose", "subscriber", p.flag, p.version, "--app-id", fx.AppID, "--gocb-ref", fx.GocbRef)
				ring := rRing(t, p.mac)
				tr := openRSender(t, rsession.SIGOOSE, sub.addr, ring, p.version)

				pc, err := cb.PublisherConfig()
				if err != nil {
					t.Fatal(err)
				}
				if pc.Values, err = valuesFromJSON(fx.Values); err != nil {
					t.Fatal(err)
				}
				pub, err := goose.NewPublisher(tr, pc)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() { done <- pub.Run(ctx) }()
				stop := sync.OnceFunc(func() {
					cancel()
					if err := <-done; err != nil && ctx.Err() == nil {
						t.Errorf("publisher: %v", err)
					}
				})
				defer stop()

				isFrame := func(ev rEvent) bool { return ev.Event == "goose" }
				ev, ok := sub.next(t, gooseEventTimeout, func(e rEvent) bool { return isFrame(e) && e.goose(t).StNum == 1 })
				if !ok {
					t.Fatal("subscriber saw no frame of state 1")
				}
				g := ev.goose(t)
				if g.GocbRef != fx.GocbRef || g.DatSet != fx.DatSet || g.GoID != fx.GoID || g.AppID != fx.AppID ||
					g.ConfRev != fx.ConfRev || g.Test || g.Simulation || g.NdsCom || ev.From == "" {
					t.Errorf("frame header: %s", ev.line)
				}
				if got, want := canonJSON(t, g.Values), canonJSON(t, fx.Values); got != want {
					t.Errorf("state 1 values:\n got %s\nwant %s", got, want)
				}
				upd, err := valuesFromJSON(fx.Update)
				if err != nil {
					t.Fatal(err)
				}
				if err := pub.Publish(upd); err != nil {
					t.Fatal(err)
				}
				ev, ok = sub.next(t, gooseEventTimeout, func(e rEvent) bool { return isFrame(e) && e.goose(t).StNum == 2 })
				if !ok {
					t.Fatal("subscriber saw no frame of state 2")
				}
				if g := ev.goose(t); g.SqNum != 0 || canonJSON(t, g.Values) != canonJSON(t, fx.Update) {
					t.Errorf("state 2: %s", ev.line)
				}
				stop()

				// The refusals, one at a time, from a socket of their own:
				// the Ethernet header (14 octets, no VLAN tag) and the GOOSE
				// header (8) are not part of the payload.
				frame, err := goose.Encode(&goose.Frame{AppID: cb.AppID, PDU: goose.PDU{
					GocbRef: fx.GocbRef, DatSet: fx.DatSet, GoID: fx.GoID, ConfRev: fx.ConfRev,
					StNum: 9, TimeAllowedToLive: 2000, AllData: upd,
				}})
				if err != nil {
					t.Fatal(err)
				}
				spdu := func(n uint32, key *rsession.Key) []byte {
					t.Helper()
					b, err := rsession.Encode(&rsession.SPDU{SI: rsession.SIGOOSE, Version: p.version, Number: n,
						Payloads: []rsession.Payload{{Type: rsession.PayloadGOOSE, AppID: cb.AppID, APDU: frame[14+8:]}}}, key)
					if err != nil {
						t.Fatal(err)
					}
					return b
				}
				send := dialRaw(t, sub.addr)
				rogue := rKey(t, p.mac, "ff"+rKeyHex[2:])
				send(spdu(1000, &rogue))
				sub.expectReject(t, "authentication")
				send(spdu(1001, nil))
				sub.expectReject(t, "unsecured")
				key, _ := ring.LookupKey(rKeyID)
				good := spdu(1002, key)
				send(good)
				if _, ok := sub.next(t, 10*time.Second, func(e rEvent) bool { return isFrame(e) && e.goose(t).StNum == 9 }); !ok {
					t.Fatal("subscriber refused a valid SPDU from a second socket")
				}
				send(good)
				sub.expectReject(t, "replay")
			})
		}
	}
}

// l2MAC is the MAC type of the frames rsession rebuilds.
type l2MAC = goose.MAC

// TestRGOOSE_GoSubscriber subscribes over rsession, with the SCL binding
// of gcbAll, to each adapter publisher: both states, then the supervision
// timeout after stop; a rogue publisher (same key ID, other key) first,
// whose SPDUs the transport must all refuse.
func TestRGOOSE_GoSubscriber(t *testing.T) {
	stacks := routableStacks(t)
	fix := loadGooseFixture(t)
	for _, st := range stacks {
		for _, p := range rProfiles {
			t.Run(st.name+"/"+p.name, func(t *testing.T) {
				cb, fx := fix.controlBlock(t, "gcbAll")
				var rejects atomic.Int64
				tr, port := openRReceiver(t, rsession.SIGOOSE, rRing(t, p.mac), cb.Dst, &rejects)
				s, err := goose.NewSubscriber(tr, goose.SubscriberConfig{})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = s.Close() }()
				sub, err := s.Subscribe(cb.SubscriptionConfig())
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { _ = s.Run(ctx) }()

				values, _ := json.Marshal(fx.Values)
				args := []string{"--remote", fmt.Sprintf("host.docker.internal:%d", port),
					"--gocb-ref", fx.GocbRef, "--go-id", fx.GoID, "--dataset", fx.DatSet,
					"--app-id", fx.AppID, "--dst", fx.Dst,
					"--conf-rev", strconv.FormatUint(uint64(fx.ConfRev), 10),
					"--min-time", strconv.Itoa(fx.MinTime), "--max-time", strconv.Itoa(fx.MaxTime),
					"--values", string(values)}

				// A rogue publisher: its key has the same ID, other material.
				rogue := startRAdapter(t, st, "goose", "publisher", p.flag, p.version,
					append(args, "--key", "ff"+rKeyHex[2:])...)
				waitUntil(t, 10*time.Second, func() bool { return tr.Stats().AuthFailed > 0 })
				rogue.send(t, map[string]any{"cmd": "stop"})
				select {
				case ev := <-sub.Events():
					t.Fatalf("event from the rogue publisher: %+v", ev)
				case <-time.After(500 * time.Millisecond):
				}

				pub := startRAdapter(t, st, "goose", "publisher", p.flag, p.version, args...)
				wait := func(pred func(goose.Event) bool) goose.Event {
					t.Helper()
					deadline := time.After(gooseEventTimeout)
					for {
						select {
						case ev := <-sub.Events():
							if ev.Kind == goose.EventInvalid {
								t.Fatalf("frame failed validation: %v", ev.Err)
							}
							if pred(ev) {
								return ev
							}
						case <-deadline:
							t.Fatal("timed out waiting for R-GOOSE event")
							return goose.Event{}
						}
					}
				}
				msg := func(ev goose.Event) bool { return ev.Kind == goose.EventMessage }
				ev := wait(func(ev goose.Event) bool { return msg(ev) && ev.Frame.PDU.StNum == 1 })
				if fr := ev.Frame; fr.AppID != cb.AppID || fr.Simulation || fr.PDU.Test || fr.PDU.NdsCom ||
					fr.PDU.NumDatSetEntries != uint32(len(fx.Values)) {
					t.Errorf("frame: %+v", fr)
				}
				if got, want := canonAll(ev.Frame.PDU.AllData), canonJSON(t, fx.Values); got != want {
					t.Errorf("state 1 values:\n got %s\nwant %s", got, want)
				}
				pub.send(t, map[string]any{"cmd": "set", "values": fx.Update})
				ev = wait(func(ev goose.Event) bool { return msg(ev) && ev.Frame.PDU.StNum == 2 })
				if !ev.NewState || ev.Frame.PDU.SqNum != 0 || canonAll(ev.Frame.PDU.AllData) != canonJSON(t, fx.Update) {
					t.Errorf("state 2: newState=%v sqNum=%d values %s", ev.NewState, ev.Frame.PDU.SqNum, canonAll(ev.Frame.PDU.AllData))
				}
				pub.send(t, map[string]any{"cmd": "stop"})
				wait(func(ev goose.Event) bool { return ev.Kind == goose.EventTimeout })

				ts := tr.Stats()
				t.Logf("transport stats %+v", ts)
				if ts.Received == 0 || ts.AuthFailed == 0 || ts.UnknownKey+ts.Unsecured+ts.Malformed+ts.Replayed != 0 ||
					int64(ts.AuthFailed) != rejects.Load() {
					t.Errorf("transport stats %+v, %d rejects", ts, rejects.Load())
				}
			})
		}
	}
}

// waitUntil polls cond until it holds or timeout passes.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// R-SV
// ---------------------------------------------------------------------------

// TestRSV_GoPublisher publishes MSVCB01 (9-2LE) from an sv.Publisher over
// rsession to each adapter subscriber: the fixture values, the update,
// and no loss.
func TestRSV_GoPublisher(t *testing.T) {
	stacks := routableStacks(t)
	fix := loadSVFixture(t)
	for _, st := range stacks {
		for _, p := range rProfiles {
			t.Run(st.name+"/"+p.name, func(t *testing.T) {
				cb, fx := fix.stream(t, "MSVCB01")
				every := fx.SamplesPerSecond / 10
				sub := startRAdapter(t, st, "sv", "subscriber", p.flag, p.version,
					"--app-id", fx.AppID, "--sample-every", strconv.Itoa(every))
				tr := openRSender(t, rsession.SISV, sub.addr, rRing(t, p.mac), p.version)
				pc, err := cb.PublisherConfig()
				if err != nil {
					t.Fatal(err)
				}
				pc.Sample = encodeFixture(t, cb, fx.Values)
				pub, err := sv.NewPublisher(tr, pc)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() { done <- pub.Run(ctx) }()
				defer func() {
					cancel()
					if err := <-done; err != nil && ctx.Err() == nil {
						t.Errorf("publisher: %v", err)
					}
				}()

				isSV := func(e rEvent) bool { return e.Event == "sv" && e.sv(t).SvID == fx.SvID }
				ev, ok := sub.next(t, svEventTimeout, isSV)
				if !ok {
					t.Fatal("subscriber saw no ASDU")
				}
				s := ev.sv(t)
				if s.AppID != fx.AppID || s.ConfRev != fx.ConfRev || s.SmpSynch != fx.SmpSynch || s.Simulation ||
					s.NoASDU != fx.NoASDU || s.SmpCnt%every != 0 || s.SmpCnt >= fx.SmpCntWrap {
					t.Errorf("ASDU header: %s", ev.line)
				}
				if got, want := canonJSON(t, s.Values), canonJSON(t, fx.Values); got != want {
					t.Errorf("values:\n got %s\nwant %s", got, want)
				}
				if err := pub.UpdateSample(encodeFixture(t, cb, fx.Update)); err != nil {
					t.Fatal(err)
				}
				want := canonJSON(t, fx.Update)
				if _, ok := sub.next(t, svEventTimeout, func(e rEvent) bool { return isSV(e) && canonJSON(t, e.sv(t).Values) == want }); !ok {
					t.Fatal("subscriber saw no ASDU with the updated values")
				}
				var stats svEvent
				for i := 0; i < 3; i++ {
					e, ok := sub.next(t, 5*time.Second, func(e rEvent) bool { return e.Event == "stats" })
					if !ok {
						t.Fatal("no stats event")
					}
					stats = e.sv(t)
				}
				ps := pub.State()
				t.Logf("publisher %d samples (%d skipped, %d late); subscriber received %d, lost %d, duplicates %d, out of order %d",
					ps.Samples, ps.Skipped, ps.Late, stats.Received, stats.Lost, stats.Duplicates, stats.OutOfOrder)
				lost := stats.Lost - min(stats.Lost, int64(ps.Skipped))
				if stats.Received == 0 || stats.Duplicates != 0 || lost*100 > stats.Received || stats.OutOfOrder*100 > stats.Received {
					t.Errorf("stats: %+v", stats)
				}
			})
		}
	}
}

// TestRSV_GoSubscriber subscribes over rsession, with the SCL binding of
// MSVCB01, to each adapter publisher: the fixture values, then the
// update, every sample verified.
func TestRSV_GoSubscriber(t *testing.T) {
	stacks := routableStacks(t)
	fix := loadSVFixture(t)
	for _, st := range stacks {
		for _, p := range rProfiles {
			t.Run(st.name+"/"+p.name, func(t *testing.T) {
				cb, fx := fix.stream(t, "MSVCB01")
				var rejects atomic.Int64
				tr, port := openRReceiver(t, rsession.SISV, rRing(t, p.mac), cb.Dst, &rejects)
				s, err := sv.NewSubscriber(tr, sv.SubscriberConfig{})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = s.Close() }()
				sc := cb.SubscriptionConfig()
				sc.SmpCntWrap = fx.SmpCntWrap
				sub, err := s.Subscribe(sc)
				if err != nil {
					t.Fatal(err)
				}
				var n atomic.Int64
				want := []string{canonJSON(t, fx.Values), canonJSON(t, fx.Update)}
				var phase atomic.Int32
				var bad atomic.Value
				if _, err := s.Subscribe(sv.SubscriptionConfig{SvID: fx.SvID, OnASDU: func(smp *sv.Sample) {
					n.Add(1)
					if smp.ASDU.SmpCnt%100 != 0 {
						return
					}
					got := decodeCanon(t, cb, smp.ASDU.Sample)
					switch {
					case got == want[phase.Load()]:
					case phase.Load() == 0 && got == want[1]:
						phase.Store(1)
					default:
						bad.Store(got)
					}
				}}); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { _ = s.Run(ctx) }()

				values, _ := json.Marshal(fx.Values)
				pub := startRAdapter(t, st, "sv", "publisher", p.flag, p.version,
					"--remote", fmt.Sprintf("host.docker.internal:%d", port),
					"--app-id", fx.AppID, "--sv-id", fx.SvID, "--dataset", fx.DatSet,
					"--conf-rev", strconv.FormatUint(uint64(fx.ConfRev), 10),
					"--smp-rate", strconv.Itoa(int(fx.SmpRate)), "--smp-mod", fx.SmpMod,
					"--no-asdu", strconv.Itoa(fx.NoASDU), "--profile", "9-2LE",
					"--values", string(values))
				go func() {
					for ev := range sub.Events() {
						if ev.Kind == sv.EventInvalid {
							bad.Store(fmt.Sprintf("invalid ASDU: %v", ev.Err))
						}
					}
				}()
				waitUntil(t, svEventTimeout, func() bool { return n.Load() >= int64(fx.SamplesPerSecond) })
				pub.send(t, map[string]any{"cmd": "set", "values": fx.Update})
				waitUntil(t, svEventTimeout, func() bool { return phase.Load() == 1 })
				pub.send(t, map[string]any{"cmd": "stop"})
				if b := bad.Load(); b != nil {
					t.Errorf("sample: %v", b)
				}
				ts := tr.Stats()
				t.Logf("%d ASDUs, transport stats %+v", n.Load(), ts)
				if rejects.Load() != 0 || ts.Received == 0 {
					t.Errorf("transport stats %+v, %d rejects", ts, rejects.Load())
				}
			})
		}
	}
}
