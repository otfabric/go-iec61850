//go:build interop

// SPDX-License-Identifier: MIT

package interop

// Sampled Values interoperability tests (fixture sv-v1, iec61850-interop
// docs/sv.md). SV is raw Ethernet like GOOSE and uses the same Docker
// bridge and GOOSE_IFACE (see goose_test.go):
//
//	GOOSE_IFACE=goose0 go test -exec sudo -tags=interop -run 'SV_' ./interop/...
//
// The tests are skipped when GOOSE_IFACE is unset.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
)

const (
	svFixtureRev   = "sv-v1"
	svEventTimeout = 30 * time.Second
)

// requireSVImages skips unless IEC61850_SV_FIXTURE names the fixture
// revision the adapter images implement: the sv-* entrypoints and the
// MSVCBs of the IED servers first ship in iec61850-interop v0.4.0, and older
// images would fail every SV test rather than skip.
func requireSVImages(t *testing.T) {
	t.Helper()
	if v := os.Getenv("IEC61850_SV_FIXTURE"); v != svFixtureRev {
		t.Skipf("IEC61850_SV_FIXTURE=%q, want %q: the adapter images predate the SV fixture (iec61850-interop v0.4.0)", v, svFixtureRev)
	}
}

// svStacks are the adapter stacks with SV commands (iec61850bean has
// none).
var svStacks = gooseStacks

// svcbOverMMS mirrors fixtures/capabilities.json svcbOverMms: the IED
// servers that expose LLN0$MS$MSVCB02.
var svcbOverMMS = map[string]bool{"libiec61850": true, "dscsystems": true}

// svKnownLimitation returns why a stack subscriber cannot receive a
// stream, or "" (iec61850-interop docs/sv.md, "Differences between the
// stacks"): libiec61850's SVReceiver drops ASDUs whose smpMod has two
// octets, as IEC 61850-9-2 (INT16U) and the Go publisher send it.
func svKnownLimitation(subscriber string, fx svFixtureStream) string {
	if subscriber == "libiec61850" && fx.OptionalFields["smpMod"] {
		return "libiec61850-sv-subscriber drops ASDUs with a two-octet smpMod (iec61850-interop docs/sv.md)"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

type svFixtureStream struct {
	MsvcbRef         string          `json:"msvcbRef"`
	Profile          string          `json:"profile"`
	SvID             string          `json:"svID"`
	DatSet           string          `json:"datSet"`
	AppID            string          `json:"appID"`
	Dst              string          `json:"dst"`
	VLANID           int             `json:"vlanId"`
	VLANPrio         int             `json:"vlanPrio"`
	ConfRev          uint32          `json:"confRev"`
	SmpRate          uint16          `json:"smpRate"`
	SmpMod           string          `json:"smpMod"`
	SamplesPerSecond int             `json:"samplesPerSecond"`
	SmpCntWrap       int             `json:"smpCntWrap"`
	NoASDU           int             `json:"noASDU"`
	SmpSynch         uint8           `json:"smpSynch"`
	OptionalFields   map[string]bool `json:"optionalFields"`
	Values           []typedValue    `json:"values"`
	Update           []typedValue    `json:"update"`
}

type svFixture struct {
	Revision string                     `json:"revision"`
	Streams  map[string]svFixtureStream `json:"streams"`
	doc      *scl.SCL
}

func svFixtureDir() string {
	return filepath.Join(getEnvOr("IEC61850_FIXTURE_DIR", defaultFixtureDir), "sv")
}

func loadSVFixture(t *testing.T) *svFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(svFixtureDir(), "sv.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f svFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Revision != svFixtureRev {
		t.Fatalf("sv fixture revision %q, want %q", f.Revision, svFixtureRev)
	}
	if f.doc, err = scl.ParseFile(filepath.Join(svFixtureDir(), "interop-sv.cid")); err != nil {
		t.Fatal(err)
	}
	return &f
}

var svSmpModNames = map[string]sv.SmpMod{
	"samples-per-period": sv.SmpPerPeriod, "samples-per-second": sv.SmpPerSec, "seconds-per-sample": sv.SecPerSmp,
}

// stream returns the SCL binding of a fixture stream and checks it
// against sv.json.
func (f *svFixture) stream(t *testing.T, name string) (*sv.ControlBlock, svFixtureStream) {
	t.Helper()
	fx, ok := f.Streams[name]
	if !ok {
		t.Fatalf("fixture has no stream %q", name)
	}
	cb, err := sv.ControlBlockFromSCL(f.doc, "InteropIED", "InteropLD", name)
	if err != nil {
		t.Fatal(err)
	}
	if cb.SvCBRef != fx.MsvcbRef || cb.DatSet != fx.DatSet || cb.SvID != fx.SvID || cb.ConfRev != fx.ConfRev ||
		cb.SmpRate != fx.SmpRate || cb.SmpMod != svSmpModNames[fx.SmpMod] || cb.NoASDU != fx.NoASDU ||
		"0x"+strconv.FormatUint(uint64(cb.AppID), 16) != fx.AppID || cb.Dst.String() != fx.Dst ||
		cb.VLAN == nil || int(cb.VLAN.Priority) != fx.VLANPrio || int(cb.VLAN.ID) != fx.VLANID ||
		cb.Opts.DataSet != fx.OptionalFields["datSet"] || cb.Opts.RefreshTime != fx.OptionalFields["refrTm"] ||
		cb.Opts.SampleRate != fx.OptionalFields["smpRate"] || len(cb.Layout) != len(fx.Values) {
		t.Fatalf("SCL binding %+v does not match sv.json %+v", cb, fx)
	}
	return cb, fx
}

// ---------------------------------------------------------------------------
// Adapter process
// ---------------------------------------------------------------------------

type svEvent struct {
	Event      string       `json:"event"`
	Fixture    string       `json:"fixture"`
	Adapter    string       `json:"adapter"`
	Version    string       `json:"version"`
	Role       string       `json:"role"`
	Src        string       `json:"src"`
	AppID      string       `json:"appID"`
	Dst        string       `json:"dst"`
	SvID       string       `json:"svID"`
	DatSet     *string      `json:"datSet"`
	SmpCnt     int          `json:"smpCnt"`
	ConfRev    uint32       `json:"confRev"`
	RefrTm     *string      `json:"refrTm"`
	SmpSynch   uint8        `json:"smpSynch"`
	SmpRate    *int         `json:"smpRate"`
	SmpMod     *string      `json:"smpMod"`
	Simulation bool         `json:"simulation"`
	ASDUIndex  int          `json:"asduIndex"`
	NoASDU     int          `json:"noASDU"`
	Values     []typedValue `json:"values"`
	Received   int64        `json:"received"`
	Lost       int64        `json:"lost"`
	Duplicates int64        `json:"duplicates"`
	OutOfOrder int64        `json:"outOfOrder"`
	Reason     string       `json:"reason"`
	Frames     int64        `json:"frames"`
	Samples    int64        `json:"samples"`
	Error      string       `json:"error"`
	Code       string       `json:"code"`
}

type svAdapter struct {
	name   string
	events chan svEvent
	stdin  interface{ Write([]byte) (int, error) }
	ready  svEvent
}

// startSVAdapter runs an SV adapter container on the L2 network and
// waits for its ready event.
func startSVAdapter(t *testing.T, st peerStack, role string, args ...string) *svAdapter {
	t.Helper()
	network := ensureGooseNetwork(t)
	name := dockerContainerName(t) + "-sv-" + role
	ctx, cancel := context.WithCancel(context.Background())
	dargs := append([]string{"run", "--rm", "-i", "--name", name, "--network", network,
		"--cap-add", "NET_RAW", "--cap-add", "NET_ADMIN",
		"--entrypoint", st.name + "-sv-" + role, st.dockerImage(), "--iface", "eth0"}, args...)
	cmd := exec.CommandContext(ctx, "docker", dargs...)
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
	a := &svAdapter{name: name, events: make(chan svEvent, 4096), stdin: stdin}
	go func() {
		defer close(a.events)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		for sc.Scan() {
			var ev svEvent
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				continue
			}
			select {
			case a.events <- ev:
			default:
			}
		}
	}()
	t.Cleanup(func() {
		dockerStop(name)
		cancel()
		_ = cmd.Wait()
	})
	ev, ok := a.next(t, iedReadyTimeout, func(ev svEvent) bool { return ev.Event == "ready" })
	if !ok {
		t.Fatalf("%s: no ready event", name)
	}
	if ev.Fixture != svFixtureRev || ev.Adapter != st.name || ev.Role != role || ev.Version == "" {
		t.Errorf("%s ready event: %+v", name, ev)
	}
	if os.Getenv("CI") != "" && ev.Version == "dev" {
		t.Errorf("adapter version is %q in CI; pin a released image digest", ev.Version)
	}
	a.ready = ev
	return a
}

func (a *svAdapter) next(t *testing.T, timeout time.Duration, pred func(svEvent) bool) (svEvent, bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-a.events:
			if !ok {
				return svEvent{}, false
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
			return svEvent{}, false
		}
	}
}

func (a *svAdapter) send(t *testing.T, cmd any) {
	t.Helper()
	b, _ := json.Marshal(cmd)
	if _, err := a.stdin.Write(append(b, '\n')); err != nil {
		t.Fatalf("%s stdin: %v", a.name, err)
	}
}

// encodeFixture encodes typed fixture values with the block's layout.
func encodeFixture(t *testing.T, cb *sv.ControlBlock, tvs []typedValue) []byte {
	t.Helper()
	vals, err := valuesFromJSON(tvs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cb.Layout.Encode(nil, vals)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// decodeCanon decodes a sample with the block's layout into canonical
// form. Qualities are compared on their 13 IEC bits, as the adapters
// report them.
func decodeCanon(t *testing.T, cb *sv.ControlBlock, sample []byte) string {
	t.Helper()
	vals, err := cb.Layout.Decode(sample)
	if err != nil {
		t.Fatal(err)
	}
	for i, k := range cb.Layout {
		if k == sv.KindQuality {
			q, _ := sv.QualityFromValue(vals[i])
			vals[i] = (q &^ sv.QualityDerived).Value()
		}
	}
	return canonAll(vals)
}

// ---------------------------------------------------------------------------
// Go publisher -> adapter subscriber
// ---------------------------------------------------------------------------

func TestSV_GoPublisher(t *testing.T) {
	requireSVImages(t)
	iface := gooseIface(t)
	fix := loadSVFixture(t)
	for _, st := range svStacks {
		for _, stream := range []string{"MSVCB01", "MSVCB02"} {
			t.Run(st.name+"/"+stream, func(t *testing.T) {
				cb, fx := fix.stream(t, stream)
				limitation := svKnownLimitation(st.name, fx)
				every := fx.SamplesPerSecond / 10 // ten events per second
				sub := startSVAdapter(t, st, "subscriber", "--app-id", fx.AppID, "--sample-every", strconv.Itoa(every))
				time.Sleep(time.Second)

				tr, err := sv.Open(iface, sv.TransportOptions{})
				if err != nil {
					t.Fatalf("open %s: %v", iface, err)
				}
				defer tr.Close()
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
					<-done
				}()

				isSV := func(ev svEvent) bool { return ev.Event == "sv" && ev.SvID == fx.SvID }
				ev, ok := sub.next(t, svEventTimeout, isSV)
				if limitation != "" {
					if ok {
						t.Errorf("known limitation no longer holds (%s): received %+v", limitation, ev)
					}
					t.Skip(limitation)
				}
				if !ok {
					t.Fatal("subscriber saw no ASDU")
				}
				if ev.AppID != fx.AppID || ev.Dst != fx.Dst || ev.Src != tr.HardwareAddr().String() || ev.ConfRev != fx.ConfRev ||
					ev.SmpSynch != fx.SmpSynch || ev.Simulation || ev.NoASDU != fx.NoASDU || ev.SmpCnt%every != 0 || ev.SmpCnt >= fx.SmpCntWrap {
					t.Errorf("ASDU header: %+v", ev)
				}
				opt := fx.OptionalFields
				if (ev.DatSet != nil) != opt["datSet"] || (ev.RefrTm != nil) != opt["refrTm"] ||
					(ev.SmpRate != nil) != opt["smpRate"] || (ev.SmpMod != nil) != opt["smpMod"] {
					t.Errorf("optional fields: datSet %v refrTm %v smpRate %v smpMod %v, want %v", ev.DatSet, ev.RefrTm, ev.SmpRate, ev.SmpMod, opt)
				}
				if ev.DatSet != nil && *ev.DatSet != fx.DatSet || ev.SmpRate != nil && *ev.SmpRate != int(fx.SmpRate) || ev.SmpMod != nil && *ev.SmpMod != fx.SmpMod {
					t.Errorf("optional values: %+v", ev)
				}
				if got, want := canonJSON(t, ev.Values), canonJSON(t, fx.Values); got != want {
					t.Errorf("values:\n got %s\nwant %s", got, want)
				}

				if err := pub.UpdateSample(encodeFixture(t, cb, fx.Update)); err != nil {
					t.Fatal(err)
				}
				want := canonJSON(t, fx.Update)
				if _, ok := sub.next(t, svEventTimeout, func(e svEvent) bool { return isSV(e) && canonJSON(t, e.Values) == want }); !ok {
					t.Fatal("subscriber saw no ASDU with the updated values")
				}

				// Three stats reports: the stream runs without loss.
				var stats svEvent
				for i := 0; i < 3; i++ {
					if stats, ok = sub.next(t, 5*time.Second, func(e svEvent) bool { return e.Event == "stats" }); !ok {
						t.Fatal("no stats event")
					}
				}
				ps := pub.State()
				t.Logf("publisher %d samples (%d skipped, %d late); subscriber received %d, lost %d, duplicates %d, out of order %d",
					ps.Samples, ps.Skipped, ps.Late, stats.Received, stats.Lost, stats.Duplicates, stats.OutOfOrder)
				// Samples the Go publisher skipped (it fell a whole sample
				// behind schedule) were never sent: not the subscriber's loss.
				lost := stats.Lost - min(stats.Lost, int64(ps.Skipped))
				if stats.Received == 0 || stats.Duplicates != 0 || lost*100 > stats.Received || stats.OutOfOrder*100 > stats.Received {
					t.Errorf("stats: %+v", stats)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Adapter publisher -> Go subscriber
// ---------------------------------------------------------------------------

func TestSV_GoSubscriber(t *testing.T) {
	requireSVImages(t)
	iface := gooseIface(t)
	fix := loadSVFixture(t)
	for _, st := range svStacks {
		for _, stream := range []string{"MSVCB01", "MSVCB02"} {
			t.Run(st.name+"/"+stream, func(t *testing.T) {
				cb, fx := fix.stream(t, stream)
				tr, err := sv.Open(iface, sv.TransportOptions{Multicast: []sv.MAC{cb.Dst}})
				if err != nil {
					t.Fatalf("open %s: %v", iface, err)
				}
				s, err := sv.NewSubscriber(tr, sv.SubscriberConfig{})
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				samples := make(chan sv.Sample, 64)
				sc := cb.SubscriptionConfig() // validates datSet, confRev, smpRate, sample size
				sc.SmpCntWrap = fx.SmpCntWrap
				sub, err := s.Subscribe(sc)
				if err != nil {
					t.Fatal(err)
				}
				// Take every 100th sample through a callback copy.
				sub2, err := s.Subscribe(sv.SubscriptionConfig{SvID: fx.SvID, OnASDU: func(smp *sv.Sample) {
					if smp.ASDU.SmpCnt%100 != 0 {
						return
					}
					c := *smp
					a := *smp.ASDU
					a.Sample = append([]byte(nil), a.Sample...)
					fr := *smp.Frame
					fr.PDU.ASDUs = nil
					if smp.Frame.VLAN != nil {
						v := *smp.Frame.VLAN
						fr.VLAN = &v
					}
					c.ASDU, c.Frame = &a, &fr
					select {
					case samples <- c:
					default:
					}
				}})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { _ = s.Run(ctx) }()

				values, _ := json.Marshal(fx.Values)
				profile := "9-2LE"
				if fx.Profile == "generic" {
					profile = "generic"
				}
				pub := startSVAdapter(t, st, "publisher",
					"--app-id", fx.AppID, "--dst", fx.Dst, "--sv-id", fx.SvID, "--dataset", fx.DatSet,
					"--vlan-id", strconv.Itoa(fx.VLANID), "--vlan-prio", strconv.Itoa(fx.VLANPrio),
					"--conf-rev", strconv.FormatUint(uint64(fx.ConfRev), 10),
					"--smp-rate", strconv.Itoa(int(fx.SmpRate)), "--smp-mod", fx.SmpMod,
					"--no-asdu", strconv.Itoa(fx.NoASDU), "--profile", profile,
					"--values", string(values))

				next := func(pred func(sv.Sample) bool) sv.Sample {
					t.Helper()
					deadline := time.After(svEventTimeout)
					for {
						select {
						case smp := <-samples:
							if pred(smp) {
								return smp
							}
						case ev := <-sub.Events():
							if ev.Kind == sv.EventInvalid {
								t.Fatalf("ASDU failed validation: %v", ev.Err)
							}
						case <-deadline:
							t.Fatal("timed out waiting for samples")
							return sv.Sample{}
						}
					}
				}
				smp := next(func(sv.Sample) bool { return true })
				fr, a := smp.Frame, smp.ASDU
				if fr.Src.String() != pub.ready.Src || fr.Dst != cb.Dst || fr.AppID != cb.AppID || fr.Simulation {
					t.Errorf("frame header: src %s (adapter %s) dst %s appID %#x sim %v", fr.Src, pub.ready.Src, fr.Dst, fr.AppID, fr.Simulation)
				}
				if fr.VLAN != nil && (int(fr.VLAN.Priority) != fx.VLANPrio || int(fr.VLAN.ID) != fx.VLANID) {
					t.Errorf("VLAN tag = %+v, want prio %d id %d", fr.VLAN, fx.VLANPrio, fx.VLANID)
				}
				if a.ConfRev != fx.ConfRev || a.SmpSynch != fx.SmpSynch || int(a.SmpCnt) >= fx.SmpCntWrap || fr.PDU.NoASDU != fx.NoASDU {
					t.Errorf("ASDU header: %+v (noASDU %d)", a, fr.PDU.NoASDU)
				}
				opt := fx.OptionalFields
				if (a.DatSet != "") != opt["datSet"] || a.HasRefrTm != opt["refrTm"] || a.HasSmpRate != opt["smpRate"] || a.HasSmpMod != opt["smpMod"] {
					t.Errorf("optional fields: %+v, want %v", a, opt)
				}
				if a.HasSmpMod && a.SmpMod != cb.SmpMod || a.HasSmpRate && a.SmpRate != cb.SmpRate {
					t.Errorf("smpRate/smpMod: %d %v", a.SmpRate, a.SmpMod)
				}
				if got, want := decodeCanon(t, cb, a.Sample), canonJSON(t, fx.Values); got != want {
					t.Errorf("values:\n got %s\nwant %s", got, want)
				}

				pub.send(t, map[string]any{"cmd": "set", "values": fx.Update})
				want := canonJSON(t, fx.Update)
				next(func(s sv.Sample) bool { return decodeCanon(t, cb, s.ASDU.Sample) == want })

				time.Sleep(2 * time.Second)
				// The rate drops to 0 on the timeout after the stop: sample it now.
				// A publisher stalled by a busy host times out the same way, and the
				// rate comes back with the stream, so one reading is not enough.
				r := float64(fx.SamplesPerSecond)
				var rate float64
				for limit := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
					if st := sub.Stats(); len(st) == 1 {
						rate = st[0].Rate
					}
					if (rate >= 0.9*r && rate <= 1.1*r) || time.Now().After(limit) {
						break
					}
				}
				pub.send(t, map[string]any{"cmd": "stop"})
				stopped, ok := pub.next(t, 10*time.Second, func(e svEvent) bool { return e.Event == "stopped" })
				deadline := time.After(5 * time.Second)
			wait:
				for {
					select {
					case ev := <-sub.Events():
						// A stall above leaves its own timeout event in the queue.
						if st := sub.Stats(); ev.Kind == sv.EventTimeout && len(st) == 1 && st[0].Status == sv.StatusTimeout {
							break wait
						}
					case <-deadline:
						t.Error("no timeout event after the publisher stopped")
						break wait
					}
				}
				stats := sub.Stats()
				if len(stats) != 1 {
					t.Fatalf("streams: %+v", stats)
				}
				st0 := stats[0]
				t.Logf("publisher stopped after %d samples; Go subscriber %d ASDUs, lost %d, duplicates %d, out of order %d, rate %.0f/s",
					stopped.Samples, st0.ASDUs, st0.Lost, st0.Duplicates, st0.OutOfOrder, rate)
				if st0.Invalid != 0 || st0.Duplicates != 0 || st0.Lost*100 > st0.ASDUs || st0.OutOfOrder*100 > st0.ASDUs {
					t.Errorf("stats: %+v", st0)
				}
				if ok && stopped.Samples > 0 && (st0.ASDUs+st0.Lost)*100 < uint64(stopped.Samples)*99 {
					t.Errorf("received %d + lost %d of %d published samples", st0.ASDUs, st0.Lost, stopped.Samples)
				}
				if rate < 0.9*r || rate > 1.1*r {
					t.Errorf("measured rate %.0f/s, want %.0f/s", rate, r)
				}
				_ = sub2.Close()
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Go IED server MSVCB -> adapter subscriber
// ---------------------------------------------------------------------------

// TestSV_GoServer publishes MSVCB01 (9-2LE) from a running go-iec61850
// server (Server.EnableSV) and checks that a data change reaches each
// adapter subscriber.
func TestSV_GoServer(t *testing.T) {
	requireSVImages(t)
	iface := gooseIface(t)
	fix := loadSVFixture(t)
	for _, st := range svStacks {
		t.Run(st.name, func(t *testing.T) {
			_, fx := fix.stream(t, "MSVCB01")
			sub := startSVAdapter(t, st, "subscriber", "--app-id", fx.AppID, "--sample-every", "400")
			time.Sleep(time.Second)

			model, err := iec61850.NewServerModelFromSCL(fix.doc, "InteropIED", "")
			if err != nil {
				t.Fatal(err)
			}
			srv, err := iec61850.NewServer(model, iec61850.ServerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Close()
			tr, err := sv.Open(iface, sv.TransportOptions{})
			if err != nil {
				t.Fatalf("open %s: %v", iface, err)
			}
			if _, err := srv.EnableSV(iec61850.SVOptions{Transport: tr}); err != nil {
				t.Fatal(err)
			}
			isSV := func(ev svEvent) bool { return ev.Event == "sv" && ev.SvID == fx.SvID }
			first, ok := sub.next(t, svEventTimeout, isSV)
			if !ok {
				t.Fatal("no ASDU from the Go server")
			}
			if first.AppID != fx.AppID || first.Dst != fx.Dst || first.ConfRev != fx.ConfRev || first.NoASDU != 1 || len(first.Values) != 16 {
				t.Errorf("ASDU: %+v", first)
			}
			srv.SetValue(context.Background(), "InteropLD/TCTR1$MX$Amp$instMag$i", mms.NewInteger(4321))
			srv.SetValue(context.Background(), "InteropLD/TVTR4$MX$Vol$q", sv.QualityTest.Value())
			ev, ok := sub.next(t, svEventTimeout, func(e svEvent) bool {
				return isSV(e) && len(e.Values) == 16 && canonJSON(t, e.Values[:1]) == "i:4321"
			})
			if !ok {
				t.Fatal("data change did not reach the samples")
			}
			if got := canonJSON(t, ev.Values[15:]); got != "bs:0000000000010" {
				t.Errorf("TVTR4.Vol.q published as %s", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Go client -> stack IED server MSVCB over MMS
// ---------------------------------------------------------------------------

// TestLibIECClient_SVCB_Read reads LLN0$MS$MSVCB02 (fixture
// iec61850-v3) from the peer stack's IED server, where it is modelled
// (capabilities.json svcbOverMms).
func TestLibIECClient_SVCB_Read(t *testing.T) {
	requireSVImages(t)
	if !svcbOverMMS[peer().name] {
		t.Skipf("%s does not expose SV control blocks over MMS (capabilities.json svcbOverMms)", peer().name)
	}
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)
	cb, err := c.GetMSVCB(ctx, "InteropLD", "LLN0$MS$MSVCB02")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MSVCB02 from %s: %+v", peer().name, cb)
	if cb.MsvID != "InteropSvGeneric" || !strings.HasSuffix(cb.DatSet, "LLN0$dsSvGeneric") || cb.ConfRev != 1 || cb.SmpRate != 1000 ||
		(cb.HasSmpMod && cb.SmpMod != sv.SmpPerSec) || cb.DstAddress.AppID != 0x4001 ||
		cb.DstAddress.Addr != (sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}) || cb.DstAddress.Priority != 4 ||
		(cb.NoASDU != 0 && cb.NoASDU != 4) || !cb.OptFlds.RefreshTime || !cb.OptFlds.SampleRate || !cb.OptFlds.DataSet {
		t.Errorf("MSVCB02 = %+v", cb)
	}
}
