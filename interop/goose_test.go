//go:build interop

// SPDX-License-Identifier: MIT

package interop

// GOOSE interoperability tests (fixture goose-v1, iec61850-interop docs/goose.md).
//
// GOOSE is raw Ethernet, so the test process and the adapter containers
// must share a layer-2 segment: a user-defined Docker bridge network
// (GOOSE_NETWORK, default iec61850-interop-goose, bridge device goose0) with the
// test opening GOOSE_IFACE (goose0 on a Linux host, eth0 when the test
// runs in a container on that network). Opening the interface needs
// CAP_NET_RAW, e.g.:
//
//	GOOSE_IFACE=goose0 go test -exec sudo -tags=interop -run GOOSE ./interop/...
//
// The tests are skipped when GOOSE_IFACE is unset.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
)

const (
	defaultGooseNetwork = "iec61850-interop-goose"
	gooseFixtureRev     = "goose-v1"
	gooseEventTimeout   = 30 * time.Second
)

// gooseStacks are the adapter stacks with GOOSE commands.
var gooseStacks = []peerStack{
	{"libiec61850", "LIBIEC61850_IMAGE", defaultLibIECImage},
	{"dscsystems", "DSCSYSTEMS_IMAGE", defaultDSCImage},
	{"iec61850-rust", "IEC61850RUST_IMAGE", defaultRustImage},
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

type typedValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type gooseFixtureCB struct {
	GocbRef  string       `json:"gocbRef"`
	GoID     string       `json:"goID"`
	DatSet   string       `json:"datSet"`
	AppID    string       `json:"appID"`
	Dst      string       `json:"dst"`
	VLANID   int          `json:"vlanId"`
	VLANPrio int          `json:"vlanPrio"`
	ConfRev  uint32       `json:"confRev"`
	MinTime  int          `json:"minTime"`
	MaxTime  int          `json:"maxTime"`
	Values   []typedValue `json:"values"`
	Update   []typedValue `json:"update"`
}

type gooseFixture struct {
	Revision      string                    `json:"revision"`
	ControlBlocks map[string]gooseFixtureCB `json:"controlBlocks"`
	doc           *scl.SCL
}

func gooseFixtureDir() string {
	return filepath.Join(getEnvOr("IEC61850_FIXTURE_DIR", defaultFixtureDir), "goose")
}

func loadGooseFixture(t *testing.T) *gooseFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(gooseFixtureDir(), "goose.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f gooseFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Revision != gooseFixtureRev {
		t.Fatalf("goose fixture revision %q, want %q", f.Revision, gooseFixtureRev)
	}
	if f.doc, err = scl.ParseFile(filepath.Join(gooseFixtureDir(), "interop-goose.cid")); err != nil {
		t.Fatal(err)
	}
	return &f
}

// controlBlock returns the SCL binding of a fixture control block and
// checks it against goose.json.
func (f *gooseFixture) controlBlock(t *testing.T, name string) (*goose.ControlBlock, gooseFixtureCB) {
	t.Helper()
	fx, ok := f.ControlBlocks[name]
	if !ok {
		t.Fatalf("fixture has no control block %q", name)
	}
	cb, err := goose.ControlBlockFromSCL(f.doc, "InteropIED", "InteropLD", name)
	if err != nil {
		t.Fatal(err)
	}
	if cb.GocbRef != fx.GocbRef || cb.DatSet != fx.DatSet || cb.GoID != fx.GoID || cb.ConfRev != fx.ConfRev ||
		fmt.Sprintf("0x%04x", cb.AppID) != fx.AppID || cb.Dst.String() != fx.Dst ||
		cb.VLAN == nil || int(cb.VLAN.Priority) != fx.VLANPrio || int(cb.VLAN.ID) != fx.VLANID ||
		cb.MinTime != time.Duration(fx.MinTime)*time.Millisecond || cb.MaxTime != time.Duration(fx.MaxTime)*time.Millisecond {
		t.Fatalf("SCL binding %+v does not match goose.json %+v", cb, fx)
	}
	return cb, fx
}

// ---------------------------------------------------------------------------
// Typed value JSON <-> mms.Value
// ---------------------------------------------------------------------------

func valueFromJSON(tv typedValue) (*mms.Value, error) {
	switch tv.Type {
	case "boolean":
		var b bool
		err := json.Unmarshal(tv.Value, &b)
		return mms.NewBoolean(b), err
	case "integer":
		var i int64
		err := json.Unmarshal(tv.Value, &i)
		return mms.NewInteger(i), err
	case "unsigned":
		var u uint64
		err := json.Unmarshal(tv.Value, &u)
		return mms.NewUnsigned(u), err
	case "float32", "float64":
		var f float64
		err := json.Unmarshal(tv.Value, &f)
		if tv.Type == "float32" {
			f = float64(float32(f))
		}
		return mms.NewFloat(f), err
	case "bit-string":
		var s string
		if err := json.Unmarshal(tv.Value, &s); err != nil {
			return nil, err
		}
		b := make([]byte, (len(s)+7)/8)
		for i, c := range s {
			if c == '1' {
				b[i/8] |= 0x80 >> (i % 8)
			}
		}
		return mms.NewBitStringWithLength(b, len(s)), nil
	case "utc-time":
		var s string
		if err := json.Unmarshal(tv.Value, &s); err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, s)
		return mms.NewUTCTimeWithQuality(ts, 0x0a), err
	case "visible-string":
		var s string
		err := json.Unmarshal(tv.Value, &s)
		return mms.NewVisibleString(s), err
	case "structure", "array":
		var elems []typedValue
		if err := json.Unmarshal(tv.Value, &elems); err != nil {
			return nil, err
		}
		vals, err := valuesFromJSON(elems)
		if tv.Type == "array" {
			return mms.NewArray(vals), err
		}
		return mms.NewStructure(vals), err
	}
	return nil, fmt.Errorf("unsupported value type %q", tv.Type)
}

func valuesFromJSON(tvs []typedValue) ([]*mms.Value, error) {
	out := make([]*mms.Value, len(tvs))
	for i, tv := range tvs {
		v, err := valueFromJSON(tv)
		if err != nil {
			return nil, fmt.Errorf("value %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// canon renders a value in a canonical text form for comparison:
// float32 in shortest form, times at millisecond precision.
func canon(v *mms.Value) string {
	switch v.Type() {
	case mms.ValueTypeBoolean:
		b, _ := v.Bool()
		return "b:" + strconv.FormatBool(b)
	case mms.ValueTypeInteger:
		i, _ := v.Int64()
		return "i:" + strconv.FormatInt(i, 10)
	case mms.ValueTypeUnsigned:
		u, _ := v.Uint64()
		return "u:" + strconv.FormatUint(u, 10)
	case mms.ValueTypeFloat:
		f, _ := v.Float64()
		return "f:" + strconv.FormatFloat(float64(float32(f)), 'g', -1, 32)
	case mms.ValueTypeBitString:
		b, _ := v.BitString()
		n, _ := v.BitStringLength()
		var sb strings.Builder
		for i := 0; i < n; i++ {
			if b[i/8]&(0x80>>(i%8)) != 0 {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
		return "bs:" + sb.String()
	case mms.ValueTypeUTCTime:
		ts, _ := v.UTCTime()
		ms := int64(math.Round(float64(ts.UnixNano()) / 1e6))
		return "t:" + time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
	case mms.ValueTypeVisibleString:
		s, _ := v.VisibleString()
		return "vs:" + s
	case mms.ValueTypeStructure, mms.ValueTypeArray:
		var elems []*mms.Value
		if v.Type() == mms.ValueTypeStructure {
			elems, _ = v.Structure()
		} else {
			elems, _ = v.ArrayElements()
		}
		parts := make([]string, len(elems))
		for i, e := range elems {
			parts[i] = canon(e)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return v.String()
}

func canonAll(vs []*mms.Value) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = canon(v)
	}
	return strings.Join(parts, " ")
}

func canonJSON(t *testing.T, tvs []typedValue) string {
	t.Helper()
	vs, err := valuesFromJSON(tvs)
	if err != nil {
		t.Fatal(err)
	}
	return canonAll(vs)
}

// ---------------------------------------------------------------------------
// Adapter processes
// ---------------------------------------------------------------------------

type gooseEvent struct {
	Event             string       `json:"event"`
	Adapter           string       `json:"adapter"`
	Fixture           string       `json:"fixture"`
	Version           string       `json:"version"`
	Role              string       `json:"role"`
	Src               string       `json:"src"`
	GocbRef           string       `json:"gocbRef"`
	DatSet            string       `json:"datSet"`
	GoID              string       `json:"goID"`
	AppID             string       `json:"appID"`
	Dst               string       `json:"dst"`
	StNum             uint32       `json:"stNum"`
	SqNum             uint32       `json:"sqNum"`
	TimeAllowedToLive uint32       `json:"timeAllowedToLive"`
	ConfRev           uint32       `json:"confRev"`
	Test              bool         `json:"test"`
	Simulation        bool         `json:"simulation"`
	NdsCom            bool         `json:"ndsCom"`
	NumDatSetEntries  int          `json:"numDatSetEntries"`
	T                 string       `json:"t"`
	Values            []typedValue `json:"values"`
	State             string       `json:"state"`
	Error             string       `json:"error"`
	Code              string       `json:"code"`
	Reason            string       `json:"reason"`
}

type gooseAdapter struct {
	name   string
	events chan gooseEvent
	stdin  io.WriteCloser
	ready  gooseEvent
}

func gooseIface(t *testing.T) string {
	t.Helper()
	iface := os.Getenv("GOOSE_IFACE")
	if iface == "" {
		t.Skip("GOOSE_IFACE not set; GOOSE interop needs a raw interface on the adapter network (see goose_test.go)")
	}
	return iface
}

func ensureGooseNetwork(t *testing.T) string {
	t.Helper()
	network := getEnvOr("GOOSE_NETWORK", defaultGooseNetwork)
	if exec.Command("docker", "network", "inspect", network).Run() == nil {
		return network
	}
	out, err := exec.Command("docker", "network", "create",
		"-o", "com.docker.network.bridge.name=goose0", network).CombinedOutput()
	if err != nil {
		t.Fatalf("docker network create %s: %v: %s", network, err, out)
	}
	return network
}

// startGooseAdapter runs a GOOSE adapter container on the GOOSE network
// and waits for its ready event.
func startGooseAdapter(t *testing.T, st peerStack, role string, args ...string) *gooseAdapter {
	t.Helper()
	network := ensureGooseNetwork(t)
	name := dockerContainerName(t) + "-" + role
	ctx, cancel := context.WithCancel(context.Background())
	dargs := append([]string{"run", "--rm", "-i", "--name", name, "--network", network,
		"--cap-add", "NET_RAW", "--cap-add", "NET_ADMIN",
		"--entrypoint", st.name + "-goose-" + role, st.dockerImage(), "--iface", "eth0"}, args...)
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
	a := &gooseAdapter{name: name, events: make(chan gooseEvent, 4096), stdin: stdin}
	go func() {
		defer close(a.events)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		for sc.Scan() {
			var ev gooseEvent
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
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
	ev, ok := a.next(t, iedReadyTimeout, func(ev gooseEvent) bool { return ev.Event == "ready" })
	if !ok {
		t.Fatalf("%s: no ready event", name)
	}
	if ev.Fixture != gooseFixtureRev || ev.Adapter != st.name || ev.Role != role || ev.Version == "" {
		t.Errorf("%s ready event: %+v", name, ev)
	}
	if os.Getenv("CI") != "" && ev.Version == "dev" {
		t.Errorf("adapter version is %q in CI; pin a released image digest", ev.Version)
	}
	a.ready = ev
	return a
}

// next returns the next event matching pred, skipping others.
func (a *gooseAdapter) next(t *testing.T, timeout time.Duration, pred func(gooseEvent) bool) (gooseEvent, bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-a.events:
			if !ok {
				return gooseEvent{}, false
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
			return gooseEvent{}, false
		}
	}
}

func (a *gooseAdapter) send(t *testing.T, cmd any) {
	t.Helper()
	b, _ := json.Marshal(cmd)
	if _, err := a.stdin.Write(append(b, '\n')); err != nil {
		t.Fatalf("%s stdin: %v", a.name, err)
	}
}

// ---------------------------------------------------------------------------
// Go publisher -> adapter subscriber
// ---------------------------------------------------------------------------

func TestGOOSE_GoPublisher(t *testing.T) {
	iface := gooseIface(t)
	fix := loadGooseFixture(t)
	for _, st := range gooseStacks {
		t.Run(st.name, func(t *testing.T) {
			cb, fx := fix.controlBlock(t, "gcbAll")
			sub := startGooseAdapter(t, st, "subscriber", "--app-id", fx.AppID)
			// The subscriber container is the bridge's carrier; give the
			// segment a moment before timing-sensitive traffic.
			time.Sleep(time.Second)

			tr, err := goose.Open(iface, goose.TransportOptions{})
			if err != nil {
				t.Fatalf("open %s: %v", iface, err)
			}
			defer tr.Close()
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

			isFrame := func(ev gooseEvent) bool { return ev.Event == "goose" && ev.GocbRef == fx.GocbRef }
			ev, ok := sub.next(t, gooseEventTimeout, func(ev gooseEvent) bool { return isFrame(ev) && ev.StNum == 1 })
			if !ok {
				cancel()
				t.Fatal("subscriber saw no frame of state 1")
			}
			if ev.DatSet != fx.DatSet || ev.GoID != fx.GoID || ev.AppID != fx.AppID || ev.Dst != fx.Dst ||
				ev.ConfRev != fx.ConfRev || ev.Test || ev.Simulation || ev.NdsCom || ev.Src != tr.HardwareAddr().String() {
				t.Errorf("frame header: %+v", ev)
			}
			if got, want := canonJSON(t, ev.Values), canonJSON(t, fx.Values); got != want {
				t.Errorf("state 1 values:\n got %s\nwant %s", got, want)
			}
			// Retransmissions of state 1 count sqNum up. The Go publisher
			// announces a constant timeAllowedToLive (2 x MaxTime).
			ev2, ok := sub.next(t, 5*time.Second, func(e gooseEvent) bool { return isFrame(e) && e.StNum == 1 && e.SqNum >= 3 })
			if !ok || ev2.TimeAllowedToLive != uint32(2*fx.MaxTime) {
				t.Errorf("retransmission: ok=%v TAL %d, want %d", ok, ev2.TimeAllowedToLive, 2*fx.MaxTime)
			}

			upd, err := valuesFromJSON(fx.Update)
			if err != nil {
				t.Fatal(err)
			}
			if err := pub.Publish(upd); err != nil {
				t.Fatal(err)
			}
			ev, ok = sub.next(t, gooseEventTimeout, func(e gooseEvent) bool { return isFrame(e) && e.StNum == 2 })
			if !ok {
				t.Fatal("subscriber saw no frame of state 2")
			}
			if ev.SqNum != 0 {
				t.Errorf("first frame of state 2 has sqNum %d", ev.SqNum)
			}
			if got, want := canonJSON(t, ev.Values), canonJSON(t, fx.Update); got != want {
				t.Errorf("state 2 values:\n got %s\nwant %s", got, want)
			}

			// Stop publishing: the subscriber reports a timeout.
			cancel()
			if err := <-done; err != nil && ctx.Err() == nil {
				t.Errorf("publisher: %v", err)
			}
			if _, ok := sub.next(t, 5*time.Second, func(e gooseEvent) bool { return e.Event == "supervision" && e.State == "timeout" }); !ok {
				t.Error("no supervision timeout after the publisher stopped")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Adapter publisher -> Go subscriber
// ---------------------------------------------------------------------------

func TestGOOSE_GoSubscriber(t *testing.T) {
	iface := gooseIface(t)
	fix := loadGooseFixture(t)
	for _, st := range gooseStacks {
		t.Run(st.name, func(t *testing.T) {
			cb, fx := fix.controlBlock(t, "gcbAll")
			tr, err := goose.Open(iface, goose.TransportOptions{Multicast: []goose.MAC{cb.Dst}})
			if err != nil {
				t.Fatalf("open %s: %v", iface, err)
			}
			s, err := goose.NewSubscriber(tr, goose.SubscriberConfig{})
			if err != nil {
				t.Fatal(err)
			}
			sub, err := s.Subscribe(cb.SubscriptionConfig()) // validates datSet, goID, confRev, members
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = s.Run(ctx) }()
			defer s.Close()

			values, _ := json.Marshal(fx.Values)
			pub := startGooseAdapter(t, st, "publisher",
				"--gocb-ref", fx.GocbRef, "--go-id", fx.GoID, "--dataset", fx.DatSet,
				"--app-id", fx.AppID, "--dst", fx.Dst,
				"--vlan-id", strconv.Itoa(fx.VLANID), "--vlan-prio", strconv.Itoa(fx.VLANPrio),
				"--conf-rev", strconv.FormatUint(uint64(fx.ConfRev), 10),
				"--min-time", strconv.Itoa(fx.MinTime), "--max-time", strconv.Itoa(fx.MaxTime),
				"--values", string(values))

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
						t.Fatal("timed out waiting for GOOSE event")
						return goose.Event{}
					}
				}
			}
			msg := func(ev goose.Event) bool { return ev.Kind == goose.EventMessage }
			ev := wait(func(ev goose.Event) bool { return msg(ev) && ev.Frame.PDU.StNum == 1 })
			fr := ev.Frame
			if fr.Src.String() != pub.ready.Src || fr.Dst != cb.Dst || fr.AppID != cb.AppID || fr.Simulation {
				t.Errorf("frame header: src %s (adapter %s) dst %s appID %#x sim %v", fr.Src, pub.ready.Src, fr.Dst, fr.AppID, fr.Simulation)
			}
			if fr.VLAN == nil || int(fr.VLAN.Priority) != fx.VLANPrio || int(fr.VLAN.ID) != fx.VLANID {
				t.Errorf("VLAN tag = %+v, want prio %d id %d", fr.VLAN, fx.VLANPrio, fx.VLANID)
			}
			if p := fr.PDU; p.Test || p.NdsCom || p.NumDatSetEntries != uint32(len(fx.Values)) {
				t.Errorf("PDU flags: %+v", p)
			}
			if got, want := canonAll(fr.PDU.AllData), canonJSON(t, fx.Values); got != want {
				t.Errorf("state 1 values:\n got %s\nwant %s", got, want)
			}

			wait(func(ev goose.Event) bool { return msg(ev) && ev.Frame.PDU.StNum == 1 && ev.Frame.PDU.SqNum >= 2 })

			pub.send(t, map[string]any{"cmd": "set", "values": fx.Update})
			ev = wait(func(ev goose.Event) bool { return msg(ev) && ev.Frame.PDU.StNum == 2 })
			if !ev.NewState || ev.Frame.PDU.SqNum != 0 {
				t.Errorf("state change: newState=%v sqNum=%d", ev.NewState, ev.Frame.PDU.SqNum)
			}
			if got, want := canonAll(ev.Frame.PDU.AllData), canonJSON(t, fx.Update); got != want {
				t.Errorf("state 2 values:\n got %s\nwant %s", got, want)
			}
			if sv := sub.Supervision(); len(sv) != 1 || sv[0].Status != goose.StatusOK || sv[0].StNumGaps != 0 || sv[0].Invalid != 0 {
				t.Errorf("supervision: %+v", sv)
			}

			pub.send(t, map[string]any{"cmd": "stop"})
			wait(func(ev goose.Event) bool { return ev.Kind == goose.EventTimeout })
		})
	}
}

// ---------------------------------------------------------------------------
// Go IED server GoCB -> adapter subscriber
// ---------------------------------------------------------------------------

// TestGOOSE_GoServer publishes gcbBool from a running go-iec61850 server
// (Server.EnableGOOSE) and checks that a data change reaches each
// adapter subscriber as a new state.
func TestGOOSE_GoServer(t *testing.T) {
	iface := gooseIface(t)
	fix := loadGooseFixture(t)
	for _, st := range gooseStacks {
		t.Run(st.name, func(t *testing.T) {
			_, fx := fix.controlBlock(t, "gcbBool")
			sub := startGooseAdapter(t, st, "subscriber", "--app-id", fx.AppID)
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
			tr, err := goose.Open(iface, goose.TransportOptions{})
			if err != nil {
				t.Fatalf("open %s: %v", iface, err)
			}
			if _, err := srv.EnableGOOSE(iec61850.GOOSEOptions{Transport: tr}); err != nil {
				t.Fatal(err)
			}
			// The server's MMS domain is the bare LD inst, so is the gocbRef.
			ref := "InteropLD/LLN0$GO$gcbBool"
			isFrame := func(ev gooseEvent) bool { return ev.Event == "goose" && ev.GocbRef == ref }
			first, ok := sub.next(t, gooseEventTimeout, isFrame)
			if !ok {
				t.Fatal("no frame from the Go server")
			}
			if first.AppID != fx.AppID || first.Dst != fx.Dst || first.GoID != fx.GoID || first.NumDatSetEntries != 2 {
				t.Errorf("frame: %+v", first)
			}
			srv.SetValue(context.Background(), "InteropLD/GGIO1$ST$SPS1$stVal", mms.NewBoolean(true))
			ev, ok := sub.next(t, gooseEventTimeout, func(e gooseEvent) bool { return isFrame(e) && e.StNum == first.StNum+1 })
			if !ok {
				t.Fatal("data change did not produce a new state")
			}
			if got := canonJSON(t, ev.Values[:1]); got != "b:true" {
				t.Errorf("SPS1.stVal published as %s", got)
			}
		})
	}
}
