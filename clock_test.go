// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
)

// stepClock is a fake [Clock] that tests set and step.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
	q  TimeQuality
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) Quality() TimeQuality {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.q
}

func (c *stepClock) step(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// clockStamp is the fake clock's time: the half second is exact in the
// UtcTime fraction (IEC 61850-8-1 8.1.3.7) and in BinaryTime
// milliseconds.
var clockStamp = time.Date(2026, 11, 11, 12, 0, 0, 500_000_000, time.UTC)

// clockQuality is LeapSecondsKnown with 20 bits of accuracy: octet 0x94.
var clockQuality = TimeQuality{LeapSecondKnown: true, TimeAccuracy: 20}

const clockQualityOctet = 0x94

func newStepClock() *stepClock { return &stepClock{t: clockStamp, q: clockQuality} }

func checkStamp(t *testing.T, what string, v *mms.Value, want time.Time) {
	t.Helper()
	ts, err := DecodeTimestamp(v)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if !ts.Time.Equal(want) || v.UTCTimeQuality() != clockQualityOctet || ts.Quality != clockQuality {
		t.Errorf("%s = %v q %#x (%+v), want %v q %#x", what, ts.Time, v.UTCTimeQuality(), ts.Quality, want, clockQualityOctet)
	}
}

// TestSpec_7_2_TimeQuality_SystemClock: the default clock is the host
// time, flagged ClockNotSynchronized with 10 bits of accuracy (IEC
// 61850-7-2 TimeQuality; IEC 61850-8-1 Table 32), the same octet as the
// GOOSE and SV publisher defaults.
func TestSpec_7_2_TimeQuality_SystemClock(t *testing.T) {
	before := time.Now()
	c := clockOrSystem(nil)
	now := c.Now()
	if now.Before(before) || now.After(time.Now()) {
		t.Errorf("SystemClock.Now = %v", now)
	}
	if q := encodeTimeQuality(c.Quality()); q != 0x2a || goose.DefaultTimeQuality != 0x2a || sv.DefaultTimeQuality != 0x2a {
		t.Errorf("SystemClock quality %#x, want 0x2a", q)
	}
	if !c.Quality().ClockNotSynchronized || c.Quality().ClockFailure || c.Quality().LeapSecondKnown {
		t.Errorf("SystemClock quality %+v", c.Quality())
	}
	if _, ok := ClockWithQuality(nil).(SystemClock); !ok {
		t.Error("ClockWithQuality(nil) is not SystemClock")
	}
	var calls atomic.Int32
	sync := ClockWithQuality(func() TimeQuality {
		calls.Add(1)
		return TimeQuality{TimeAccuracy: 18}
	})
	for range 2 {
		if v := stampUTC(sync); v.UTCTimeQuality() != 18 {
			t.Errorf("ClockWithQuality octet %#x, want 0x12", v.UTCTimeQuality())
		}
	}
	if calls.Load() != 2 {
		t.Errorf("quality called %d times, want once per stamp", calls.Load())
	}
	if n := sync.Now(); n.Before(before) {
		t.Errorf("ClockWithQuality.Now = %v", n)
	}
}

// TestSpec_8_1_UtcTimeWithQuality: a stamped value is the UtcTime of IEC
// 61850-8-1 8.1.3.7: four octets of seconds, three of binary fraction,
// then the TimeQuality octet of the clock.
func TestSpec_8_1_UtcTimeWithQuality(t *testing.T) {
	v := stampUTC(newStepClock())
	enc, err := goose.EncodeData(v)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x91, 0x08, 0, 0, 0, 0, 0x80, 0x00, 0x00, clockQualityOctet}
	binary.BigEndian.PutUint32(want[2:], uint32(clockStamp.Unix()))
	if !bytes.Equal(enc, want) {
		t.Errorf("UtcTime % x, want % x", enc, want)
	}
	checkStamp(t, "stamp", v, clockStamp)
	// Every TimeQuality bit maps to its octet bit.
	c := &stepClock{t: clockStamp, q: TimeQuality{LeapSecondKnown: true, ClockFailure: true, ClockNotSynchronized: true, TimeAccuracy: TimeAccuracyUnspecified}}
	if q := stampUTC(c).UTCTimeQuality(); q != 0xff {
		t.Errorf("all flags octet %#x, want 0xff", q)
	}
}

// TestSpec_7_2_20_5_2_5_ClientControlT: the T of Oper, SBOw and Cancel is
// the client clock's time and quality; a per-call TQuality overrides only
// the quality.
func TestSpec_7_2_20_5_2_5_ClientControlT(t *testing.T) {
	clk := newStepClock()
	m, _ := encodeOperClock(OperateParams{CtlVal: BoolCtlVal(true)}, false, clk).Structure()
	checkStamp(t, "Oper T", m[3], clockStamp)
	override := &TimeQuality{ClockFailure: true, TimeAccuracy: TimeAccuracyUnspecified}
	m, _ = encodeOperClock(OperateParams{CtlVal: BoolCtlVal(true), TQuality: override}, true, clk).Structure()
	if tm, _ := m[4].UTCTime(); !tm.Equal(clockStamp) || m[4].UTCTimeQuality() != 0x5f {
		t.Errorf("Oper T with TQuality = %v q %#x, want %v q 0x5f", tm, m[4].UTCTimeQuality(), clockStamp)
	}
	m, _ = encodeCancelClock(CancelParams{CtlVal: BoolCtlVal(true)}, false, clk).Structure()
	checkStamp(t, "Cancel T", m[3], clockStamp)

	// The clock reaches the client from ClientOptions and DialOptions.
	mc := &mms.Client{}
	c, err := NewClient(mc, ClientOptions{Clock: clk})
	if err != nil || c.opts.Clock != Clock(clk) {
		t.Errorf("NewClient Clock not kept: %v", err)
	}
	ctx := waitCtx(t)
	d, err := Dial(ctx, startDialerTestServer(t), DialOptions{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(ctx) }()
	if d.opts.Clock != Clock(clk) {
		t.Error("DialOptions.Clock not passed to the client")
	}
}

// TestSpec_7_3_DefaultOperateT: the default operate stamps the `t` of the
// controlled DO (IEC 61850-7-3) with the server clock.
func TestSpec_7_3_DefaultOperateT(t *testing.T) {
	clk := newStepClock()
	srv := specControlServer(t, specCF{}, func(s *Server) {
		mustRegister(t, s, "GGIO1.DPCSO1", CtlModelDirectNormal, ControlHandler{})
	})
	srv.clock = clk
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := mustParseRef(t, ctlDPC)
	if err := c.Operate(ctx, ref, OperateParams{CtlVal: DPCCtlVal(true), CtlNum: 1}); err != nil {
		t.Fatal(err)
	}
	v, err := c.ReadRaw(ctx, Ref{LD: ref.LD, LN: ref.LN, Path: []string{"DPCSO1", "t"}, FC: FCST})
	if err != nil {
		t.Fatal(err)
	}
	checkStamp(t, "DPCSO1.t", v, clockStamp)
}

// TestSpec_7_2_17_2_2_16_TimeOfEntryFromClock: the report TimeStamp and
// the BRCB TimeOfEntry are the server clock's time of the entry (IEC
// 61850-7-2 §17.2.2.16, §17.2.3.2.2.9; EntryTime carries no quality).
func TestSpec_7_2_17_2_2_16_TimeOfEntryFromClock(t *testing.T) {
	clk := newStepClock()
	srv, err := NewServer(specModel(), ServerOptions{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	engine := srv.EnableReports()
	ctx := context.Background()
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	if r := recvReport(t, sub); !r.Timestamp.Equal(clockStamp) {
		t.Errorf("report TimeStamp = %v, want %v", r.Timestamp, clockStamp)
	}
	if ms, _ := rawRead(t, c, specBRCB+"$TimeOfEntry").BinaryTime(); ms != clockStamp.UnixMilli() {
		t.Errorf("TimeOfEntry = %d ms, want %d", ms, clockStamp.UnixMilli())
	}
	// GI and integrity entries (snapshots) and an unset entry time.
	clk.step(time.Minute)
	rt := engine.rcbs["LD1/"+specBRCB]
	rt.mu.Lock()
	snap := engine.snapshotEntryLocked(rt, ReasonGI, true)
	rt.mu.Unlock()
	if !snap.timeOfEntry.Equal(clockStamp.Add(time.Minute)) {
		t.Errorf("GI TimeOfEntry = %v", snap.timeOfEntry)
	}
	vals := encodeReportValuesEx(reportParams{optFlds: OptFldTimeStamp, clock: clk})
	if ms, _ := vals[2].BinaryTime(); ms != clockStamp.Add(time.Minute).UnixMilli() {
		t.Errorf("unset TimeStamp = %d ms", ms)
	}
}

// TestSpec_8_1_17_3_JournalEntryTimeFromClock: journal entries written
// by SetValue and by an MMS client write carry the server clock's time
// (IEC 61850-8-1 §17.3.3.4 EntryTime / occurrence time).
func TestSpec_8_1_17_3_JournalEntryTimeFromClock(t *testing.T) {
	clk := newStepClock()
	srv, err := NewServer(testJournalModel(), ServerOptions{Clock: clk, AllowReadOnlyFCWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	je := srv.EnableJournals()
	ctx := context.Background()
	key := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.store.Set(key, mms.NewInteger(0))
	srv.SetValue(ctx, key, mms.NewInteger(1))
	clk.step(time.Second)
	c := dialLoopback(t, ctx, srv)
	if err := rawWrite(c, "LLN0$ST$Mod$stVal", mms.NewInteger(2)); err != nil {
		t.Fatal(err)
	}
	entries := journalEntries(t, je, "LLN0$log1")
	if len(entries) != 2 {
		t.Fatalf("%d entries, want 2", len(entries))
	}
	for i, want := range []time.Time{clockStamp, clockStamp.Add(time.Second)} {
		if !entries[i].OccurrenceTime.Equal(want) {
			t.Errorf("entry %d time %v, want %v", i, entries[i].OccurrenceTime, want)
		}
	}
}

// TestSpec_7_2_16_LActTmFromClock: SelectActiveSG sets LActTm (IEC
// 61850-7-2 16.2.2.6, a TimeStamp) from the server clock, with its
// quality.
func TestSpec_7_2_16_LActTmFromClock(t *testing.T) {
	clk := newStepClock()
	model := &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []servermodel.LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			SettingGroup: &servermodel.SettingGroupDef{NumOfSGs: 3, ActSG: 1},
		}},
	}}}
	srv, err := NewServer(model, ServerOptions{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	srv.EnableSettingGroups(SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	clk.step(time.Hour)
	if err := c.SelectActiveSG(ctx, "LD1", 2); err != nil {
		t.Fatal(err)
	}
	info, err := c.GetSettingGroupInfo(ctx, "LD1")
	if err != nil {
		t.Fatal(err)
	}
	if !info.LActTm.Time.Equal(clockStamp.Add(time.Hour)) || info.LActTm.Quality != clockQuality {
		t.Errorf("LActTm = %v %+v, want %v %+v", info.LActTm.Time, info.LActTm.Quality, clockStamp.Add(time.Hour), clockQuality)
	}
}

// TestSpec_8_1_GOOSE_TFromClock: the server's GOOSE T is the server
// clock's time with its quality; GOOSEOptions.TimeQuality overrides the
// quality only (IEC 61850-8-1 Table 55 t).
func TestSpec_8_1_GOOSE_TFromClock(t *testing.T) {
	for _, c := range []struct {
		tq, want uint8
	}{{0, clockQualityOctet}, {0x0a, 0x0a}} {
		srv := gooseServerFromCID(t)
		srv.clock = newStepClock()
		hub := goose.NewHub()
		rx := hub.NewPort(goose.MAC{})
		if _, err := srv.EnableGOOSE(GOOSEOptions{Transport: hub.NewPort(goose.MAC{}), TimeQuality: c.tq}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		raw, err := rx.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		f, err := goose.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !f.PDU.T.Equal(clockStamp) || f.PDU.TimeQuality != c.want {
			t.Errorf("TimeQuality option %#x: T %v q %#x, want %v q %#x", c.tq, f.PDU.T, f.PDU.TimeQuality, clockStamp, c.want)
		}
	}
}

// TestSpec_9_2_SV_RefrTmFromClock: the server's SV refrTm is the server
// clock's time of the frame's last sample, with its quality (IEC
// 61850-9-2 Table 14).
func TestSpec_9_2_SV_RefrTmFromClock(t *testing.T) {
	raw, err := os.ReadFile("sv/testdata/mu-9-2le.icd")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := scl.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "MUIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{Clock: newStepClock()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hub := sv.NewHub()
	rx := hub.NewPort(sv.MAC{})
	if _, err := srv.EnableSV(SVOptions{Transport: hub.NewPort(sv.MAC{})}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		b, err := rx.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
		f, err := sv.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		as := f.PDU.ASDUs
		if len(as) == 0 || !as[0].HasRefrTm {
			continue // MSVCB01/03 send no refrTm
		}
		last := as[len(as)-1]
		if !last.RefrTm.Equal(clockStamp) || last.RefrTmQuality != clockQualityOctet {
			t.Errorf("refrTm %v q %#x, want %v q %#x", last.RefrTm, last.RefrTmQuality, clockStamp, clockQualityOctet)
		}
		if !as[0].RefrTm.Before(last.RefrTm) {
			t.Errorf("first refrTm %v not before last %v", as[0].RefrTm, last.RefrTm)
		}
		return
	}
}

// TestSpec_7_2_ClockStepKeepsTimers: timers run on the monotonic clock.
// Stepping the server clock by hours neither expires a selection early
// nor keeps it past sboTimeout (IEC 61850-7-2 20.2.2), and integrity
// reports keep their IntgPd (§17.2.2.12) while their TimeStamp follows
// the stepped clock.
func TestSpec_7_2_ClockStepKeepsTimers(t *testing.T) {
	ctx := waitCtx(t)
	t.Run("sboTimeout", func(t *testing.T) {
		clk := newStepClock()
		srv := specControlServer(t, specCF{sboTimeoutMs: 400}, func(s *Server) {
			mustRegister(t, s, "GGIO1.SPCTm", CtlModelSBOEnhanced, ControlHandler{})
		})
		srv.clock = clk
		c := controlClient(t, srv)
		ref := mustParseRef(t, ctlTm)
		if err := c.SelectWithValue(ctx, ref, boolParams(1)); err != nil {
			t.Fatal(err)
		}
		clk.step(24 * time.Hour) // forward: the selection does not expire
		if _, err := c.OperateWait(ctx, ref, boolParams(1)); err != nil {
			t.Fatalf("operate after a forward clock step: %v", err)
		}
		if err := c.SelectWithValue(ctx, ref, boolParams(2)); err != nil {
			t.Fatal(err)
		}
		clk.step(-48 * time.Hour) // backward: sboTimeout still fires
		time.Sleep(700 * time.Millisecond)
		refusal(t, "operate after sboTimeout", c.Operate(ctx, ref, boolParams(2)), AddCauseObjectNotSelected, mms.DataAccessErrorObjectAccessDenied)
	})

	t.Run("IntgPd", func(t *testing.T) {
		clk := newStepClock()
		m := specModel()
		rpt := &m.LogicalDevices[0].LogicalNodes[0].Reports[1] // urcb01
		rpt.IntgPd = 100
		rpt.TrgOps.Period = true
		srv, err := NewServer(m, ServerOptions{Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(srv.Close)
		srv.EnableReports()
		c := dialLoopback(t, ctx, srv)
		sub := subscribeRCB(t, c, "rpt_urcb01", specURCB, true)
		integrity := func() *ReportIndication {
			t.Helper()
			for {
				r := recvReport(t, sub)
				if len(r.ReasonCodes) > 0 && r.ReasonCodes[0]&ReasonIntegrity != 0 {
					return r
				}
			}
		}
		integrity()
		for _, d := range []time.Duration{24 * time.Hour, -48 * time.Hour} {
			clk.step(d)
			start := time.Now()
			integrity() // may be the one already due
			r := integrity()
			if el := time.Since(start); el > 2*time.Second {
				t.Errorf("clock step %v: two integrity periods took %v", d, el)
			}
			if !r.Timestamp.Equal(clk.Now()) {
				t.Errorf("clock step %v: TimeStamp %v, want %v", d, r.Timestamp, clk.Now())
			}
		}
	})
}
