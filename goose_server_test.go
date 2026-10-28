// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
)

const (
	gcbAllRef  = "InteropLD/LLN0$GO$gcbAll"
	gcbBoolRef = "InteropLD/LLN0$GO$gcbBool"
	sps1Key    = "InteropLD/GGIO1$ST$SPS1$stVal"
)

type gooseFixture struct {
	srv    *Server
	hub    *goose.Hub
	engine *GOOSEEngine
	client *Client
	events <-chan goose.Event
}

func newGOOSEFixture(t *testing.T, opts GOOSEOptions) *gooseFixture {
	t.Helper()
	ctx := context.Background()
	doc, err := scl.ParseFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := &gooseFixture{srv: srv, hub: goose.NewHub()}
	sub, err := goose.NewSubscriber(f.hub.NewPort(goose.MAC{}), goose.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sub.Subscribe(goose.SubscriptionConfig{GocbRef: gcbAllRef, QueueSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	f.events = s.Events()
	sctx, cancel := context.WithCancel(ctx)
	go func() { _ = sub.Run(sctx) }()

	opts.Transport = f.hub.NewPort(goose.MAC{})
	f.engine, err = srv.EnableGOOSE(opts)
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

// waitMessage returns the next EventMessage for which pred holds.
func (f *gooseFixture) waitMessage(t *testing.T, pred func(p *goose.PDU) bool) *goose.PDU {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-f.events:
			if ev.Kind == goose.EventMessage && pred(&ev.Frame.PDU) {
				return &ev.Frame.PDU
			}
		case <-deadline:
			t.Fatal("timeout waiting for GOOSE message")
			return nil
		}
	}
}

func TestGOOSEServerPublishesOnDataChange(t *testing.T) {
	f := newGOOSEFixture(t, GOOSEOptions{})
	p := f.waitMessage(t, func(*goose.PDU) bool { return true })
	if p.DatSet != "InteropLD/LLN0$dsGooseAll" || p.GoID != "InteropGooseAll" || p.ConfRev != 1 || len(p.AllData) != 8 {
		t.Fatalf("initial PDU %+v", p)
	}
	first := p.StNum

	// SetValue with the same value does not publish; a change does.
	f.srv.SetValue(context.Background(), sps1Key, mms.NewBoolean(false))
	f.srv.SetValue(context.Background(), sps1Key, mms.NewBoolean(true))
	p = f.waitMessage(t, func(p *goose.PDU) bool { return p.StNum > first })
	if p.StNum != first+1 || p.SqNum != 0 {
		t.Fatalf("stNum %d sqNum %d", p.StNum, p.SqNum)
	}
	if b, _ := p.AllData[0].Bool(); !b {
		t.Fatal("SPS1.stVal not published")
	}
	// A structured member (MMXU1.TotW [MX]) changes through a leaf.
	f.srv.SetValue(context.Background(), "InteropLD/MMXU1$MX$TotW$mag$f", mms.NewFloat(-0.25))
	p = f.waitMessage(t, func(p *goose.PDU) bool { return p.StNum == first+2 })
	totw, _ := p.AllData[7].Structure()
	mag, _ := totw[0].Structure()
	if v, _ := mag[0].Float64(); v != -0.25 {
		t.Fatalf("TotW.mag.f = %v", mag[0])
	}
	if !f.srv.Capabilities().GOOSE {
		t.Error("capabilities")
	}
	if got := f.engine.ControlBlocks(); len(got) != 2 || got[0] != gcbAllRef {
		t.Errorf("ControlBlocks = %v", got)
	}
	if pub, err := f.engine.Publisher(gcbAllRef); err != nil || pub == nil {
		t.Errorf("Publisher: %v", err)
	}
	if _, err := f.engine.Publisher("nope"); !errors.Is(err, ErrNotFound) {
		t.Error(err)
	}
}

func TestGOOSEServerGoCBOverMMS(t *testing.T) {
	ctx := context.Background()
	f := newGOOSEFixture(t, GOOSEOptions{})
	f.waitMessage(t, func(*goose.PDU) bool { return true })

	cb, err := f.client.GetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll")
	if err != nil {
		t.Fatal(err)
	}
	if !cb.GoEna || cb.GoID != "InteropGooseAll" || cb.DatSet != "InteropLD/LLN0$dsGooseAll" || cb.ConfRev != 1 || cb.NdsCom ||
		cb.DstAddress.Addr != (goose.MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01}) || cb.DstAddress.AppID != 0x1001 ||
		cb.DstAddress.Priority != 4 || !cb.HasTimes || cb.MinTime != 10 || cb.MaxTime != 1000 {
		t.Fatalf("GoCB = %+v", cb)
	}

	// GoID/DatSet are not writable while enabled.
	id := "renamed"
	if err := f.client.SetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll", GoCBUpdate{GoID: &id}); err == nil {
		t.Fatal("GoID write accepted while GoEna")
	}
	// Read-only attributes.
	if err := f.client.Write(ctx, mustParseRef(t, "InteropLD/LLN0.GO.gcbAll.ConfRev"), mms.NewUnsigned(9)); err == nil {
		t.Fatal("ConfRev write accepted")
	}
	// Disable, retarget to dsGooseBool (ConfRev increments), rename, enable.
	off, on := false, true
	ds := "InteropLD/LLN0$dsGooseBool"
	if err := f.client.SetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll", GoCBUpdate{GoEna: &off}); err != nil {
		t.Fatal(err)
	}
	if err := f.client.SetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll", GoCBUpdate{GoID: &id, DatSet: &ds, GoEna: &on}); err != nil {
		t.Fatal(err)
	}
	p := f.waitMessage(t, func(p *goose.PDU) bool { return p.GoID == "renamed" })
	if p.DatSet != ds || p.ConfRev != 2 || len(p.AllData) != 2 {
		t.Fatalf("after retarget: %+v", p)
	}
	cb, _ = f.client.GetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll")
	if cb.ConfRev != 2 || cb.DatSet != ds || !cb.GoEna {
		t.Errorf("GoCB after update = %+v", cb)
	}
	// Unknown dataset is rejected.
	bad := "InteropLD/LLN0$nope"
	_ = f.client.SetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll", GoCBUpdate{GoEna: &off})
	if err := f.client.SetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll", GoCBUpdate{DatSet: &bad}); err == nil {
		t.Error("unknown DatSet accepted")
	}
	// Writing the same DatSet is a no-op (ConfRev unchanged).
	if err := f.client.SetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll", GoCBUpdate{DatSet: &ds}); err != nil {
		t.Error(err)
	}
	if cb, _ = f.client.GetGoCB(ctx, "InteropLD", "LLN0$GO$gcbAll"); cb.ConfRev != 2 || cb.GoEna {
		t.Errorf("after no-op: %+v", cb)
	}
	// A change to a dataset member publishes on the retargeted block
	// (dsGooseBool: SPS1.stVal, q). ST is not client-writable.
	if err := f.engine.SetEnabled(gcbAllRef, true); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Write(ctx, mustParseRef(t, "InteropLD/GGIO1.ST.SPS1.stVal"), mms.NewBoolean(true)); err == nil {
		t.Fatal("ST write accepted")
	}
	f.srv.SetValue(ctx, sps1Key, mms.NewBoolean(true))
	f.waitMessage(t, func(p *goose.PDU) bool {
		b, _ := p.AllData[0].Bool()
		return p.GoID == "renamed" && b
	})
}

func TestGOOSEServerStartDisabledAndErrors(t *testing.T) {
	ctx := context.Background()
	f := newGOOSEFixture(t, GOOSEOptions{StartDisabled: true})
	select {
	case ev := <-f.events:
		t.Fatalf("event while disabled: %v", ev.Kind)
	case <-time.After(50 * time.Millisecond):
	}
	cb, err := f.client.GetGoCB(ctx, "InteropLD", "LLN0$GO$gcbBool")
	if err != nil || cb.GoEna {
		t.Fatalf("%+v %v", cb, err)
	}
	if err := f.engine.SetEnabled(gcbAllRef, true); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.SetEnabled(gcbAllRef, true); err != nil { // idempotent
		t.Fatal(err)
	}
	f.waitMessage(t, func(*goose.PDU) bool { return true })
	if err := f.engine.SetEnabled("nope", true); !errors.Is(err, ErrNotFound) {
		t.Error(err)
	}
	if _, err := f.srv.EnableGOOSE(GOOSEOptions{Transport: f.hub.NewPort(goose.MAC{})}); !errors.Is(err, ErrGOOSE) {
		t.Error(err)
	}
	if _, err := f.srv.EnableGOOSE(GOOSEOptions{}); !errors.Is(err, ErrGOOSE) {
		t.Error(err)
	}
	if f.srv.GOOSEEngine() != f.engine {
		t.Error("GOOSEEngine accessor")
	}
	// Whole-structure and DstAddress writes are denied.
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "InteropLD", ItemID: "LLN0$GO$gcbAll$DstAddress$APPID", Value: mms.NewUnsigned(1)}); err == nil {
		t.Error("APPID write accepted")
	}
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "InteropLD", ItemID: "LLN0$GO$gcbAll$GoEna", Value: mms.NewInteger(1)}); err == nil {
		t.Error("type-inconsistent GoEna write accepted")
	}
	// Client argument validation and decode errors.
	if _, err := f.client.GetGoCB(ctx, "", "x"); !errors.Is(err, ErrInvalidArgument) {
		t.Error(err)
	}
	if err := f.client.SetGoCB(ctx, "", "x", GoCBUpdate{}); !errors.Is(err, ErrInvalidArgument) {
		t.Error(err)
	}
	if _, err := f.client.GetGoCB(ctx, "InteropLD", "LLN0$GO$nope"); err == nil {
		t.Error("missing GoCB read succeeded")
	}
	if _, err := f.client.GetGoCB(ctx, "InteropLD", "LLN0$ST$Mod$stVal"); err == nil {
		t.Error("non-GoCB decoded")
	}
	f.srv.Close()
	f.srv.Close()
	if err := f.engine.SetEnabled(gcbBoolRef, true); err == nil {
		t.Error("enable after stop succeeded")
	}
}

func TestDecodeGoCBErrors(t *testing.T) {
	addr := mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewUnsigned(4), mms.NewUnsigned(0), mms.NewUnsigned(1)})
	base := func() []*mms.Value {
		return []*mms.Value{mms.NewBoolean(true), mms.NewVisibleString("id"), mms.NewVisibleString("ds"), mms.NewUnsigned(1), mms.NewBoolean(false), addr}
	}
	if cb, err := decodeGoCB("r", mms.NewStructure(base())); err != nil || cb.HasTimes {
		t.Fatalf("%+v %v", cb, err)
	}
	mut := []func(e []*mms.Value) *mms.Value{
		func(e []*mms.Value) *mms.Value { return mms.NewBoolean(true) },
		func(e []*mms.Value) *mms.Value { e[1] = mms.NewInteger(1); return mms.NewStructure(e) },
		func(e []*mms.Value) *mms.Value { e[5] = mms.NewBoolean(true); return mms.NewStructure(e) },
		func(e []*mms.Value) *mms.Value {
			e[5] = mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{1}), mms.NewUnsigned(0), mms.NewUnsigned(0), mms.NewUnsigned(0)})
			return mms.NewStructure(e)
		},
		func(e []*mms.Value) *mms.Value {
			e[5] = mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewBoolean(true), mms.NewUnsigned(0), mms.NewUnsigned(0)})
			return mms.NewStructure(e)
		},
	}
	for i, m := range mut {
		if _, err := decodeGoCB("r", m(base())); !errors.Is(err, ErrProtocol) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func mustParseRef(t *testing.T, s string) Ref {
	t.Helper()
	r, err := ParseRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
