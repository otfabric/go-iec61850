// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
)

const (
	msvcb1Ref = "MU01/LLN0$MS$MSVCB01"
	msvcb2Ref = "MU01/LLN0$MS$MSVCB02"
	ia1Key    = "MU01/ITCTR1$MX$AmpSv$instMag$i"
	ua1QKey   = "MU01/UTVTR1$MX$VolSv$q"
)

type svFixture struct {
	srv    *Server
	hub    *sv.Hub
	engine *SVEngine
	client *Client

	mu      sync.Mutex
	samples map[string]*sv.Sample // last sample per svID
}

func newSVFixture(t *testing.T, opts SVOptions) *svFixture {
	t.Helper()
	ctx := context.Background()
	doc, err := scl.ParseFile("sv/testdata/mu-9-2le.icd")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "MUIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := &svFixture{srv: srv, hub: sv.NewHub(), samples: map[string]*sv.Sample{}}
	sub, err := sv.NewSubscriber(f.hub.NewPort(sv.MAC{}), sv.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sub.Subscribe(sv.SubscriptionConfig{OnASDU: func(s *sv.Sample) {
		cp := *s
		a := *s.ASDU
		a.Sample = append([]byte(nil), s.ASDU.Sample...)
		cp.ASDU = &a
		// The subscriber reuses its Frame: copy what the tests read.
		fr := *s.Frame
		fr.PDU.ASDUs = append([]sv.ASDU(nil), s.Frame.PDU.ASDUs...)
		if s.Frame.VLAN != nil {
			v := *s.Frame.VLAN
			fr.VLAN = &v
		}
		cp.Frame = &fr
		f.mu.Lock()
		f.samples[s.ASDU.SvID] = &cp
		f.mu.Unlock()
	}}); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithCancel(ctx)
	go func() { _ = sub.Run(sctx) }()

	opts.Transport = f.hub.NewPort(sv.MAC{})
	f.engine, err = srv.EnableSV(opts)
	if err != nil {
		t.Fatal(err)
	}
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(sctx, serverT) }()
	mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.client, err = NewClient(mc, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.client.Close(ctx)
		srv.Close()
		cancel()
		_ = sub.Close()
	})
	return f
}

// wait returns the latest sample of svID once pred holds for it.
func (f *svFixture) wait(t *testing.T, svID string, pred func(s *sv.Sample) bool) *sv.Sample {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		s := f.samples[svID]
		f.mu.Unlock()
		if s != nil && pred(s) {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", svID)
	return nil
}

func TestSVServerPublishesDataset(t *testing.T) {
	f := newSVFixture(t, SVOptions{SmpSynch: sv.SmpSynchLocal})
	if got := f.engine.ControlBlocks(); len(got) != 3 || got[0] != msvcb1Ref {
		t.Fatalf("control blocks %v", got)
	}
	s := f.wait(t, "MU01", func(*sv.Sample) bool { return true })
	if s.ASDU.ConfRev != 1 || s.ASDU.SmpSynch != sv.SmpSynchLocal || s.ASDU.DatSet != "" || len(s.ASDU.Sample) != sv.LESampleSize ||
		s.Frame.AppID != 0x4000 || s.Frame.VLAN == nil || s.Frame.VLAN.Priority != 4 {
		t.Fatalf("MSVCB01 sample %+v", s.ASDU)
	}
	s2 := f.wait(t, "MU01-256", func(*sv.Sample) bool { return true })
	if len(s2.Frame.PDU.ASDUs) != 8 || !s2.ASDU.HasRefrTm || !s2.ASDU.HasSmpRate || s2.ASDU.SmpRate != 256 || s2.ASDU.DatSet != "MU01/LLN0$PhsMeas1" {
		t.Fatalf("MSVCB02 sample %+v", s2.ASDU)
	}

	// Dataset changes reach the samples.
	ctx := context.Background()
	f.srv.SetValue(ctx, ia1Key, mms.NewInteger(1234))
	f.srv.SetValue(ctx, ua1QKey, sv.QualityTest.Value())
	f.wait(t, "MU01", func(s *sv.Sample) bool {
		var le sv.LESample
		_ = sv.DecodeLE(s.ASDU.Sample, &le)
		return le.Current[0].Value == 1234 && le.Voltage[0].Q == sv.QualityTest
	})
	// A value that cannot be encoded is logged, not published.
	f.srv.SetValue(ctx, ia1Key, mms.NewVisibleString("x"))

	if !f.srv.Capabilities().SV || f.srv.SVEngine() != f.engine {
		t.Error("capabilities")
	}
	if p, err := f.engine.Publisher(msvcb1Ref); err != nil || p == nil || p.State().Rate != 4000 {
		t.Errorf("publisher %v", err)
	}
	// MSVCB03 has no address and stays disabled.
	if p, err := f.engine.Publisher("MU01/LLN0$MS$MSVCB03"); err != nil || p != nil {
		t.Errorf("MSVCB03 publisher %v %v", p, err)
	}
	if err := f.engine.SetEnabled("MU01/LLN0$MS$MSVCB03", true); err == nil {
		t.Error("MSVCB03 enabled")
	}
}

func TestSVServerMSVCBOverMMS(t *testing.T) {
	ctx := context.Background()
	f := newSVFixture(t, SVOptions{})
	f.wait(t, "MU01", func(*sv.Sample) bool { return true })

	cb, err := f.client.GetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01")
	if err != nil {
		t.Fatal(err)
	}
	if !cb.SvEna || cb.MsvID != "MU01" || cb.DatSet != "MU01/LLN0$PhsMeas1" || cb.ConfRev != 1 || cb.SmpRate != 80 ||
		!cb.HasSmpMod || cb.SmpMod != sv.SmpPerPeriod || cb.NoASDU != 1 || !cb.OptFlds.SampleSynchronized ||
		cb.DstAddress.Addr != (sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}) || cb.DstAddress.AppID != 0x4000 || cb.DstAddress.Priority != 4 {
		t.Fatalf("MSVCB = %+v", cb)
	}

	// Configuration is not writable while enabled.
	id := "MU01-new"
	if err := f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", MSVCBUpdate{MsvID: &id}); err == nil {
		t.Fatal("MsvID write accepted while SvEna")
	}
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MU01", ItemID: "LLN0$MS$MSVCB01$ConfRev", Value: mms.NewUnsigned(9)}); err == nil {
		t.Fatal("ConfRev write accepted")
	}
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MU01", ItemID: "LLN0$MS$MSVCB01$noASDU", Value: mms.NewUnsigned(2)}); err == nil {
		t.Fatal("noASDU write accepted")
	}

	// Disable, reconfigure, enable.
	off, on := false, true
	rate := uint16(4000)
	mod := sv.SmpPerSec
	opts := sv.OptFlds{RefreshTime: true, SampleRate: true, DataSet: true}
	ds := "MU01/LLN0$PhsMeas1"
	if err := f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", MSVCBUpdate{SvEna: &off}); err != nil {
		t.Fatal(err)
	}
	if err := f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", MSVCBUpdate{
		MsvID: &id, DatSet: &ds, SmpRate: &rate, SmpMod: &mod, OptFlds: &opts, SvEna: &on,
	}); err != nil {
		t.Fatal(err)
	}
	s := f.wait(t, id, func(*sv.Sample) bool { return true })
	if !s.ASDU.HasSmpRate || s.ASDU.SmpRate != 4000 || !s.ASDU.HasSmpMod || s.ASDU.SmpMod != sv.SmpPerSec ||
		!s.ASDU.HasRefrTm || s.ASDU.DatSet != ds || s.ASDU.ConfRev != 1 {
		t.Fatalf("after reconfigure %+v", s.ASDU)
	}
	cb, _ = f.client.GetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01")
	if cb.MsvID != id || cb.SmpRate != 4000 || cb.SmpMod != sv.SmpPerSec || cb.OptFlds != opts || !cb.SvEna {
		t.Errorf("MSVCB after update = %+v", cb)
	}

	// Invalid values are rejected while disabled.
	_ = f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", MSVCBUpdate{SvEna: &off})
	empty, zero, badMod, bad := "", uint16(0), sv.SmpMod(7), "MU01/LLN0$nope"
	for i, u := range []MSVCBUpdate{{MsvID: &empty}, {SmpRate: &zero}, {SmpMod: &badMod}, {DatSet: &bad}} {
		if err := f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", u); err == nil {
			t.Errorf("update %d accepted", i)
		}
	}
	// The same DatSet is a no-op.
	if err := f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", MSVCBUpdate{DatSet: &ds}); err != nil {
		t.Error(err)
	}
	if cb, _ = f.client.GetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01"); cb.ConfRev != 1 || cb.SvEna {
		t.Errorf("after no-op: %+v", cb)
	}
}

func TestSVServerStartDisabledAndErrors(t *testing.T) {
	ctx := context.Background()
	f := newSVFixture(t, SVOptions{StartDisabled: true})
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	n := len(f.samples)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d streams while disabled", n)
	}
	if p, _ := f.engine.Publisher(msvcb2Ref); p != nil {
		t.Error("publisher while disabled")
	}
	if err := f.engine.SetEnabled(msvcb2Ref, true); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.SetEnabled(msvcb2Ref, true); err != nil { // idempotent
		t.Fatal(err)
	}
	f.wait(t, "MU01-256", func(*sv.Sample) bool { return true })
	if err := f.engine.SetEnabled("nope", true); !errors.Is(err, ErrNotFound) {
		t.Error(err)
	}
	if _, err := f.engine.Publisher("nope"); !errors.Is(err, ErrNotFound) {
		t.Error(err)
	}
	if _, err := f.srv.EnableSV(SVOptions{Transport: f.hub.NewPort(sv.MAC{})}); !errors.Is(err, ErrSV) {
		t.Error(err)
	}
	if _, err := f.srv.EnableSV(SVOptions{}); !errors.Is(err, ErrSV) {
		t.Error(err)
	}
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MU01", ItemID: "LLN0$MS$MSVCB01$SvEna", Value: mms.NewInteger(1)}); err == nil {
		t.Error("type-inconsistent SvEna write accepted")
	}
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MU01", ItemID: "LLN0$MS$MSVCB01$DstAddress$APPID", Value: mms.NewUnsigned(1)}); err == nil {
		t.Error("APPID write accepted")
	}
	if _, err := f.client.GetMSVCB(ctx, "", "x"); !errors.Is(err, ErrInvalidArgument) {
		t.Error(err)
	}
	if err := f.client.SetMSVCB(ctx, "", "x", MSVCBUpdate{}); !errors.Is(err, ErrInvalidArgument) {
		t.Error(err)
	}
	if _, err := f.client.GetMSVCB(ctx, "MU01", "LLN0$MS$nope"); err == nil {
		t.Error("missing MSVCB read succeeded")
	}
	if _, err := f.client.GetMSVCB(ctx, "MU01", "LLN0$ST$Mod$stVal"); err == nil {
		t.Error("non-MSVCB decoded")
	}
	id := "x"
	if err := f.client.SetMSVCB(ctx, "MU01", "LLN0$MS$nope", MSVCBUpdate{MsvID: &id}); err == nil {
		t.Error("write to missing MSVCB succeeded")
	}
	f.srv.Close()
	f.srv.Close()
	if err := f.engine.SetEnabled(msvcb1Ref, true); err == nil {
		t.Error("enable after stop succeeded")
	}
	f.engine.notify(ia1Key) // no-op after stop
}

func TestDecodeMSVCB(t *testing.T) {
	addr := mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewUnsigned(4), mms.NewUnsigned(0), mms.NewUnsigned(0x4000)})
	opt := mms.NewBitStringWithLength([]byte{0x20}, 5)
	base := func() []*mms.Value {
		return []*mms.Value{mms.NewBoolean(true), mms.NewVisibleString("id"), mms.NewVisibleString("ds"), mms.NewUnsigned(1), mms.NewUnsigned(80), opt}
	}
	// Edition 1: no SmpMod, no noASDU.
	cb, err := decodeMSVCB("r", mms.NewStructure(append(base(), addr)))
	if err != nil || cb.HasSmpMod || cb.NoASDU != 0 || !cb.OptFlds.SampleRate || cb.DstAddress.AppID != 0x4000 {
		t.Fatalf("ed1 %+v %v", cb, err)
	}
	// SmpMod as Unsigned.
	cb, err = decodeMSVCB("r", mms.NewStructure(append(base(), mms.NewUnsigned(1), addr, mms.NewUnsigned(2))))
	if err != nil || cb.SmpMod != sv.SmpPerSec || cb.NoASDU != 2 {
		t.Fatalf("unsigned SmpMod %+v %v", cb, err)
	}
	mut := []func(e []*mms.Value) *mms.Value{
		func(e []*mms.Value) *mms.Value { return mms.NewBoolean(true) },
		func(e []*mms.Value) *mms.Value { e[1] = mms.NewInteger(1); return mms.NewStructure(append(e, addr)) },
		func(e []*mms.Value) *mms.Value { return mms.NewStructure(append(e, mms.NewInteger(0))) },
		func(e []*mms.Value) *mms.Value { return mms.NewStructure(append(e, mms.NewBoolean(true))) },
		func(e []*mms.Value) *mms.Value {
			return mms.NewStructure(append(e, mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{1}), mms.NewUnsigned(0), mms.NewUnsigned(0), mms.NewUnsigned(0)})))
		},
		func(e []*mms.Value) *mms.Value {
			return mms.NewStructure(append(e, mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewBoolean(true), mms.NewUnsigned(0), mms.NewUnsigned(0)})))
		},
	}
	for i, m := range mut {
		if _, err := decodeMSVCB("r", m(base())); !errors.Is(err, ErrProtocol) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}
