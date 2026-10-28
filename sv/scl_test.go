// SPDX-License-Identifier: MIT

package sv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/scl"
)

func loadMU(t *testing.T) *scl.SCL {
	t.Helper()
	doc, err := scl.ParseFile("testdata/mu-9-2le.icd")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestControlBlocksFromSCL(t *testing.T) {
	doc := loadMU(t)
	cbs, err := ControlBlocksFromSCL(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(cbs) != 3 {
		t.Fatalf("%d blocks", len(cbs))
	}
	cb := cbs[0]
	if cb.SvCBRef != "MUIEDMU01/LLN0$MS$MSVCB01" || cb.SvID != "MU01" || cb.DatSet != "MUIEDMU01/LLN0$PhsMeas1" ||
		cb.ConfRev != 1 || cb.SmpRate != 80 || cb.SmpMod != SmpPerPeriod || cb.NoASDU != 1 ||
		!cb.HasAddress || cb.Dst != (MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}) || cb.AppID != 0x4000 ||
		cb.VLAN == nil || cb.VLAN.Priority != 4 || !cb.Opts.SampleSynchronized || cb.Opts.RefreshTime {
		t.Fatalf("MSVCB01 %+v", cb)
	}
	if len(cb.Members) != 16 || cb.Members[0].Ref != "MU01/ITCTR1.AmpSv.instMag.i" || cb.Members[0].FC != "MX" {
		t.Fatalf("members %+v", cb.Members)
	}
	if cb.Layout.String() != LELayout.String() {
		t.Fatalf("layout %s, want 9-2LE %s", cb.Layout, LELayout)
	}
	b := cbs[1]
	if b.NoASDU != 8 || b.SmpRate != 256 || !b.Opts.RefreshTime || !b.Opts.SampleRate || !b.Opts.DataSet || b.VLAN != nil {
		t.Fatalf("MSVCB02 %+v", b)
	}
	if c := cbs[2]; c.HasAddress || c.SmpMod != SmpPerSec {
		t.Fatalf("MSVCB03 %+v", c)
	}
	if _, err := cbs[2].PublisherConfig(); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no address: %v", err)
	}
	if sc := cbs[2].SubscriptionConfig(); sc.AppIDs != nil || sc.SvID != "MU01-x" {
		t.Errorf("subscription without address %+v", sc)
	}

	got, err := ControlBlockFromSCL(doc, "MUIED", "MU01", "MSVCB02")
	if err != nil || got.SvID != "MU01-256" {
		t.Fatalf("lookup %v %+v", err, got)
	}
	if _, err := ControlBlockFromSCL(doc, "MUIED", "MU01", "nope"); !errors.Is(err, ErrNotFound) || !errors.Is(err, ErrSCL) {
		t.Errorf("missing: %v", err)
	}
	if _, err := ControlBlocksFromSCL(nil); !errors.Is(err, ErrSCL) {
		t.Errorf("nil: %v", err)
	}
}

// TestSCLPubSub publishes MSVCB02 (8 ASDUs, optional fields) and
// subscribes with the configuration derived from the same block.
func TestSCLPubSub(t *testing.T) {
	t.Parallel()
	cb, err := ControlBlockFromSCL(loadMU(t), "MUIED", "MU01", "MSVCB02")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := cb.PublisherConfig()
	if err != nil {
		t.Fatal(err)
	}
	if pc.DatSet != cb.DatSet || !pc.RefrTm || !pc.SendSmpRate || pc.SendSmpMod || pc.NoASDU != 8 {
		t.Fatalf("publisher config %+v", pc)
	}
	var le LESample
	le.Voltage[0].Value = VoltsCount(230)
	pc.Sample = le.Append(nil)
	// Samples are only valid during the callback: hand over a copy.
	got := make(chan ASDU, 1)
	sc := cb.SubscriptionConfig()
	sc.OnASDU = func(s *Sample) {
		a := *s.ASDU
		a.Sample = append([]byte(nil), a.Sample...)
		select {
		case got <- a:
		default:
		}
	}
	hub := NewHub()
	p, err := NewPublisher(hub.NewPort(MAC{}), pc)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := NewSubscriber(hub.NewPort(MAC{}), SubscriberConfig{})
	sub, err := s.Subscribe(sc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	go func() { _ = p.Run(ctx) }()
	select {
	case a := <-got:
		var r LESample
		if err := DecodeLE(a.Sample, &r); err != nil || Volts(r.Voltage[0].Value) != 230 || a.DatSet != cb.DatSet {
			t.Fatalf("sample %v %+v", err, a)
		}
	case <-ctx.Done():
		t.Fatal("no sample")
	}
	cancel()
	for _, st := range sub.Stats() {
		if st.Invalid != 0 {
			t.Errorf("invalid ASDUs: %+v", st)
		}
	}
}

func TestSCLErrors(t *testing.T) {
	src, err := scl.ParseFile("testdata/mu-9-2le.icd")
	if err != nil {
		t.Fatal(err)
	}
	ln0 := src.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0
	ln0.SMVControls = append(ln0.SMVControls,
		scl.SMVControl{Name: "bad1", SmvID: "x", DatSet: "missing", SmpRate: 80, NofASDU: 1},
		scl.SMVControl{Name: "bad2", SmvID: "x", SmpRate: 80, NofASDU: 1},
		scl.SMVControl{Name: "bad3", SmvID: "x", DatSet: "strs", SmpRate: 80, NofASDU: 1},
		scl.SMVControl{Name: "bad4", SmvID: "x", DatSet: "nolink", SmpRate: 80, NofASDU: 1},
	)
	// A string member cannot be sampled.
	for i := range src.DataTypeTemplates.DOTypes {
		if dt := &src.DataTypeTemplates.DOTypes[i]; dt.ID == "INC_MU" {
			dt.DAs = append(dt.DAs, scl.DA{Name: "d", FC: "DC", BType: "VisString255"})
		}
	}
	ln0.DataSets = append(ln0.DataSets,
		scl.DataSet{Name: "strs", FCDAs: []scl.FCDA{{LDInst: "MU01", LNClass: "LLN0", DOName: "Mod", DAName: "d", FC: "DC"}}},
		scl.DataSet{Name: "nolink", FCDAs: []scl.FCDA{{LDInst: "MU01", LNClass: "XXXX", LNInst: "9", DOName: "Mod", FC: "ST"}}},
	)
	cbs, err := ControlBlocksFromSCL(src)
	if len(cbs) != 3 || !errors.Is(err, ErrSCL) || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "no dataset") ||
		!strings.Contains(err.Error(), "cannot be sampled") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("%d blocks, %v", len(cbs), err)
	}
	if _, err := ControlBlockFromSCL(src, "MUIED", "MU01", "zzz"); !errors.Is(err, ErrSCL) {
		t.Errorf("lookup with errors: %v", err)
	}
}
