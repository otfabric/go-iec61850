// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
)

// Store keys of testdata/setting-groups.cid (domain SGIEDProt).
const (
	sgFixtureLD = "SGIEDProt"
	keyStrSG    = sgFixtureLD + "/PTOC1$SG$StrVal$setMag$f"
	keyStrSE    = sgFixtureLD + "/PTOC1$SE$StrVal$setMag$f"
	keyOpDlSG   = sgFixtureLD + "/PTOC1$SG$OpDlTmms$setVal"
	keyTmMultSG = sgFixtureLD + "/PTOC1$SG$TmMult$setMag$f"
	keyCurveSG  = sgFixtureLD + "/PTOC1$SG$TmACrv$setCharact"
)

var (
	refStrVal = Ref{LD: sgFixtureLD, LN: "PTOC1", Path: []string{"StrVal", "setMag", "f"}}
	refOpDl   = Ref{LD: sgFixtureLD, LN: "PTOC1", Path: []string{"OpDlTmms", "setVal"}}
	refTmMult = Ref{LD: sgFixtureLD, LN: "PTOC1", Path: []string{"TmMult", "setMag", "f"}}
)

// sgFixtureServer serves testdata/setting-groups.cid with setting groups
// enabled. extra runs before EnableSettingGroups (reports, GOOSE).
func sgFixtureServer(t *testing.T, h SettingGroupHandler, extra ...func(*Server)) *Server {
	t.Helper()
	doc, err := scl.ParseFile("testdata/setting-groups.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "SGIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	for _, f := range extra {
		f(srv)
	}
	srv.EnableSettingGroups(h)
	return srv
}

func floatOf(t *testing.T, v interface{ Float64() (float64, bool) }) float64 {
	t.Helper()
	f, ok := v.Float64()
	if !ok {
		t.Fatalf("not a float: %v", v)
	}
	return f
}

func intOf(t *testing.T, v interface{ Int64() (int64, bool) }) int64 {
	t.Helper()
	i, ok := v.Int64()
	if !ok {
		t.Fatalf("not an integer: %v", v)
	}
	return i
}

// groupFloat is the bank value of a FLOAT32 setting in group g.
func groupFloat(t *testing.T, srv *Server, key string, g uint8) float64 {
	t.Helper()
	v, err := srv.SettingGroupEngine().SettingGroupValue(key, g)
	if err != nil {
		t.Fatal(err)
	}
	return floatOf(t, v)
}

func activeFloat(t *testing.T, c *Client, ref Ref) float64 {
	t.Helper()
	v, err := c.GetActiveSGValue(waitCtx(t), ref)
	if err != nil {
		t.Fatalf("GetActiveSGValue %s: %v", ref, err)
	}
	return floatOf(t, v.MMS())
}

// IEC 61850-6 9.5.4.4: a Val with sGroup is the setting's value in that
// group; a Val without sGroup applies to all groups. Groups without a
// value of their own take it, else the data type's value. fc="SE" implies
// fc="SG" (Table 47).
func TestSpec_6_9_5_4_4_SGroupValuesFillBanks(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	e := srv.SettingGroupEngine()

	for _, c := range []struct {
		key  string
		want [3]float64
	}{
		{keyStrSG, [3]float64{600, 800, 500}},
		{keyStrSE, [3]float64{600, 800, 500}}, // the SE twin names the same setting
		{keyTmMultSG, [3]float64{1.5, 1.5, 1.5}},
	} {
		for g := uint8(1); g <= 3; g++ {
			if got := groupFloat(t, srv, c.key, g); got != c.want[g-1] {
				t.Errorf("%s group %d = %v, want %v", c.key, g, got, c.want[g-1])
			}
		}
	}
	for g, want := range map[uint8]int64{1: 300, 2: 500, 3: 100} {
		v, err := e.SettingGroupValue(keyOpDlSG, g)
		if err != nil || intOf(t, v) != want {
			t.Errorf("OpDlTmms group %d = %v, %v; want %d", g, v, err, want)
		}
	}
	for g, want := range map[uint8]int64{1: 2, 2: 9, 3: 2} {
		v, err := e.SettingGroupValue(keyCurveSG, g)
		if err != nil || intOf(t, v) != want {
			t.Errorf("TmACrv group %d = %v, %v; want ordinal %d", g, v, err, want)
		}
	}

	// The SE-only StrVal.setMag got its implied SG attribute, which holds
	// the active group's value.
	if v := srv.ValueStore().Get(keyStrSG); v == nil || floatOf(t, v) != 600 {
		t.Errorf("StrVal [SG] = %v, want 600", v)
	}
	keys := e.SettingKeys(sgFixtureLD)
	if len(keys) != 4 || keys[0] != keyOpDlSG || keys[1] != keyStrSG {
		t.Errorf("SettingKeys = %v", keys)
	}

	// sGroup 7 is beyond numOfSGs 3: a model warning.
	doc, err := scl.ParseFile("testdata/setting-groups.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "SGIED", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range model.Warnings {
		found = found || strings.Contains(w, "sGroup=7 exceeds numOfSGs 3")
	}
	if !found {
		t.Errorf("no sGroup warning in %v", model.Warnings)
	}
}

// IEC 61850-7-2 16.2.2.4, 16.3.2 and Figure 22: SelectActiveSG loads the
// selected group's values into the active buffer, FC=SG, and sets ActSG
// and LActTm (16.2.2.7).
func TestSpec_7_2_16_3_2_SelectActiveSGSwitchesBank(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)

	if got := activeFloat(t, c, refStrVal); got != 600 {
		t.Fatalf("group 1 StrVal = %v", got)
	}
	before, err := c.GetSettingGroupInfo(ctx, sgFixtureLD)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	for _, step := range []struct {
		sg         uint8
		str, tmMul float64
		opDl       int64
	}{
		{2, 800, 1.5, 500},
		{3, 500, 1.5, 100},
		{1, 600, 1.5, 300},
	} {
		if err := c.SelectActiveSG(ctx, sgFixtureLD, step.sg); err != nil {
			t.Fatalf("SelectActiveSG %d: %v", step.sg, err)
		}
		if got := activeFloat(t, c, refStrVal); got != step.str {
			t.Errorf("group %d StrVal = %v, want %v", step.sg, got, step.str)
		}
		if got := activeFloat(t, c, refTmMult); got != step.tmMul {
			t.Errorf("group %d TmMult = %v, want %v", step.sg, got, step.tmMul)
		}
		v, err := c.GetActiveSGValue(ctx, refOpDl)
		if err != nil || intOf(t, v.MMS()) != step.opDl {
			t.Errorf("group %d OpDlTmms = %v, %v; want %d", step.sg, v, err, step.opDl)
		}
		info, err := c.GetSettingGroupInfo(ctx, sgFixtureLD)
		if err != nil || info.ActSG != step.sg {
			t.Fatalf("SGCB = %+v, %v", info, err)
		}
		if !info.LActTm.Time.After(before.LActTm.Time) {
			t.Errorf("LActTm %v not after %v", info.LActTm.Time, before.LActTm.Time)
		}
	}
}

// IEC 61850-7-2 16.2.2.5, 16.3.3.1: SelectEditSG copies the group's
// values into the edit buffer (FC=SE); the active values are unchanged.
// A setting under FC=SG only is not editable (IEC 61850-6 Table 47).
func TestSpec_7_2_16_3_3_SelectEditSGLoadsEditBuffer(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)

	for _, g := range []uint8{2, 3, 1} {
		if err := c.SelectEditSG(ctx, sgFixtureLD, g); err != nil {
			t.Fatal(err)
		}
		v, err := c.GetEditSGValue(ctx, refStrVal)
		if err != nil {
			t.Fatal(err)
		}
		if want := groupFloat(t, srv, keyStrSG, g); floatOf(t, v.MMS()) != want {
			t.Errorf("edit group %d StrVal [SE] = %v, want %v", g, v, want)
		}
		if got := activeFloat(t, c, refStrVal); got != 600 {
			t.Errorf("active StrVal = %v while editing %d", got, g)
		}
	}
	if err := c.SetEditSGValue(ctx, refTmMult, mms.NewFloat(2)); err == nil {
		t.Error("SetEditSGValue of an SG-only setting succeeded")
	}
}

// IEC 61850-7-2 16.3.4: SetEditSGValue writes the edit buffer only; the
// group's values stay until ConfirmEditSGValues (16.2.2.5). The Reference
// may be an FCDA of a constructed attribute (16.3.4.2.1).
func TestSpec_7_2_16_3_4_SetEditSGValueWritesSE(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)

	if err := c.SelectEditSG(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(850)); err != nil {
		t.Fatal(err)
	}
	v, err := c.GetEditSGValue(ctx, refStrVal)
	if err != nil || floatOf(t, v.MMS()) != 850 {
		t.Fatalf("StrVal [SE] = %v, %v", v, err)
	}
	if got := groupFloat(t, srv, keyStrSG, 2); got != 800 {
		t.Errorf("group 2 bank = %v before confirm, want 800", got)
	}
	if got := activeFloat(t, c, refStrVal); got != 600 {
		t.Errorf("active StrVal = %v", got)
	}

	// The constructed setMag as a whole.
	setMag := Ref{LD: sgFixtureLD, LN: "PTOC1", Path: []string{"StrVal", "setMag"}}
	if err := c.SetEditSGValue(ctx, setMag, mms.NewStructure([]*mms.Value{mms.NewFloat(875)})); err != nil {
		t.Fatal(err)
	}
	if v := srv.ValueStore().Get(keyStrSE); floatOf(t, v) != 875 {
		t.Errorf("setMag.f [SE] = %v after FCDA write, want 875", v)
	}
	if err := c.SetEditSGValue(ctx, setMag, mms.NewFloat(1)); dataAccessCode(err) != mms.DataAccessErrorTypeInconsistent {
		t.Errorf("setMag write of a float = %v, want type-inconsistent", err)
	}
	if err := c.ConfirmEditSG(ctx, sgFixtureLD); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSG, 2); got != 875 {
		t.Errorf("group 2 bank = %v after confirm, want 875", got)
	}
}

// IEC 61850-7-2 16.3.5.1: ConfirmEditSGValues overwrites the edited
// group's values with the edit buffer; when the edited group is the
// active one, it is re-activated with the new values. CnfEdit returns to
// FALSE (IEC 61850-8-1 §16.2.4).
func TestSpec_7_2_16_3_5_ConfirmEditSGValues(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)

	// A group that is not active: the bank changes, FC=SG does not.
	if err := c.SelectEditSG(ctx, sgFixtureLD, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(550)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refOpDl, mms.NewInteger(120)); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmEditSG(ctx, sgFixtureLD); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSG, 3); got != 550 {
		t.Errorf("group 3 StrVal = %v, want 550", got)
	}
	if got := activeFloat(t, c, refStrVal); got != 600 {
		t.Errorf("active StrVal = %v after confirming group 3", got)
	}
	info, err := c.GetSettingGroupInfo(ctx, sgFixtureLD)
	if err != nil || info.CnfEdit || info.EditSG != 0 {
		t.Fatalf("SGCB after confirm = %+v, %v", info, err)
	}
	if err := c.SelectActiveSG(ctx, sgFixtureLD, 3); err != nil {
		t.Fatal(err)
	}
	if got := activeFloat(t, c, refStrVal); got != 550 {
		t.Errorf("group 3 active StrVal = %v, want 550", got)
	}
	if v, _ := c.GetActiveSGValue(ctx, refOpDl); intOf(t, v.MMS()) != 120 {
		t.Errorf("group 3 active OpDlTmms = %v, want 120", v)
	}

	// The active group: FC=SG changes at once.
	if err := c.SelectEditSG(ctx, sgFixtureLD, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(560)); err != nil {
		t.Fatal(err)
	}
	if got := activeFloat(t, c, refStrVal); got != 550 {
		t.Errorf("active StrVal = %v before confirm", got)
	}
	if err := c.ConfirmEditSG(ctx, sgFixtureLD); err != nil {
		t.Fatal(err)
	}
	if got := activeFloat(t, c, refStrVal); got != 560 {
		t.Errorf("active StrVal = %v after confirming the active group, want 560", got)
	}
	// Leaving and coming back keeps it.
	if err := c.SelectActiveSG(ctx, sgFixtureLD, 1); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSG, 3); got != 560 {
		t.Errorf("group 3 bank = %v after leaving it, want 560", got)
	}
}

// IEC 61850-7-2 16.3.3.1: SelectEditSG 0 ends the edit (CancelEditSG);
// the edit buffer is discarded and the group keeps its values. So do
// the ResvTms timeout and the loss of the association.
func TestSpec_7_2_16_3_3_1_CancelEditSGDiscards(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	c := controlClient(t, srv)
	ctx := waitCtx(t)

	if err := c.SelectEditSG(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(1)); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelEditSG(ctx, sgFixtureLD); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSG, 2); got != 800 {
		t.Errorf("group 2 = %v after cancel, want 800", got)
	}
	if err := c.ConfirmEditSG(ctx, sgFixtureLD); dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("ConfirmEditSG after cancel = %v, want temporarily-unavailable", err)
	}
	// Selecting again loads the group's values, not the discarded edit.
	if err := c.SelectEditSG(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if v, err := c.GetEditSGValue(ctx, refStrVal); err != nil || floatOf(t, v.MMS()) != 800 {
		t.Errorf("StrVal [SE] = %v, %v after reselect, want 800", v, err)
	}
	// Re-selecting the same group reloads it too (16.3.3.1).
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(2)); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectEditSG(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.GetEditSGValue(ctx, refStrVal); floatOf(t, v.MMS()) != 800 {
		t.Errorf("StrVal [SE] = %v after selecting group 2 again", v)
	}
}

// A data change of an active setting is reported (IEC 61850-7-2 §17.2,
// dchg) when SelectActiveSG or ConfirmEditSGValues of the active group
// changes it, and not when a group that is not active is confirmed.
func TestSpec_7_2_16_ActiveSettingChangeIsReported(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{}, func(s *Server) { s.EnableReports() })
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	sub, err := c.SubscribeReport(ctx, "urcbSG", SubscribeReportOptions{
		LD: sgFixtureLD, RCBItemID: "LLN0$RP$urcbSG01", AutoEnable: true, QueueSize: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })

	// Each changed setting is a data change of its own (BufTm 0): wait
	// for the report that carries StrVal, the first member of dsSettings.
	wantReport := func(what string, str float64) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case r := <-sub.Reports():
				if len(r.Inclusion) == 0 || !r.Inclusion[0] || len(r.Values) == 0 {
					continue
				}
				if got := floatOf(t, r.Values[0].MMS()); got != str {
					t.Errorf("%s: reported StrVal %v, want %v", what, got, str)
				}
				return
			case <-deadline:
				t.Fatalf("%s: no report of StrVal", what)
			}
		}
	}

	if err := c.SelectActiveSG(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	wantReport("SelectActiveSG 2", 800)

	if err := c.SelectEditSG(ctx, sgFixtureLD, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(510)); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmEditSG(ctx, sgFixtureLD); err != nil {
		t.Fatal(err)
	}
	expectNoReport(t, sub, 200*time.Millisecond)

	if err := c.SelectEditSG(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(820)); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmEditSG(ctx, sgFixtureLD); err != nil {
		t.Fatal(err)
	}
	wantReport("ConfirmEditSGValues of the active group", 820)
}

// GOOSE publishes a data set with an active setting when SelectActiveSG
// changes it (a new StNum, IEC 61850-8-1 §18.1.2.5).
func TestSpec_7_2_16_ActiveSettingChangeIsPublished(t *testing.T) {
	hub := goose.NewHub()
	sub, err := goose.NewSubscriber(hub.NewPort(goose.MAC{}), goose.SubscriberConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sub.Subscribe(goose.SubscriptionConfig{GocbRef: sgFixtureLD + "/LLN0$GO$gcbSG", QueueSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = sub.Close() })
	go func() { _ = sub.Run(ctx) }()

	srv := sgFixtureServer(t, SettingGroupHandler{}, func(srv *Server) {
		if _, err := srv.EnableGOOSE(GOOSEOptions{Transport: hub.NewPort(goose.MAC{})}); err != nil {
			t.Fatal(err)
		}
	})
	next := func(pred func(*goose.PDU) bool) *goose.PDU {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-s.Events():
				if ev.Kind == goose.EventMessage && pred(&ev.Frame.PDU) {
					return &ev.Frame.PDU
				}
			case <-deadline:
				t.Fatal("no GOOSE message")
				return nil
			}
		}
	}
	first := next(func(*goose.PDU) bool { return true }).StNum
	if err := srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	// Each changed setting is a state change of its own.
	next(func(p *goose.PDU) bool {
		if p.StNum <= first || len(p.AllData) != 2 {
			return false
		}
		f, _ := p.AllData[0].Float64()
		i, _ := p.AllData[1].Int64()
		return f == 800 && i == 500
	})
}

// SettingGroupValue and SetSettingGroupValue read and seed the banks:
// the active group's value is the FC=SG attribute.
func TestSettingGroupEngine_SettingValueAPI(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{})
	e := srv.SettingGroupEngine()
	ctx := context.Background()

	if err := e.SetSettingGroupValue(ctx, keyStrSG, 2, mms.NewFloat(810)); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSE, 2); got != 810 {
		t.Errorf("group 2 = %v, want 810", got)
	}
	if v := srv.ValueStore().Get(keyStrSG); floatOf(t, v) != 600 {
		t.Errorf("active StrVal = %v after seeding group 2", v)
	}
	// The active group changes FC=SG.
	if err := e.SetSettingGroupValue(ctx, keyStrSE, 1, mms.NewFloat(610)); err != nil {
		t.Fatal(err)
	}
	if v := srv.ValueStore().Get(keyStrSG); floatOf(t, v) != 610 {
		t.Errorf("active StrVal = %v, want 610", v)
	}
	// Server.SetValue of an active setting is the active group's value.
	srv.SetValue(ctx, keyStrSG, mms.NewFloat(620))
	if err := srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSG, 1); got != 620 {
		t.Errorf("group 1 = %v, want 620", got)
	}
	if v := srv.ValueStore().Get(keyStrSG); floatOf(t, v) != 810 {
		t.Errorf("active StrVal = %v, want 810", v)
	}

	for _, c := range []struct {
		name string
		err  error
		want error
	}{
		{"no LD", e.SetSettingGroupValue(ctx, "nokey", 1, mms.NewFloat(1)), ErrInvalidArgument},
		{"unknown LD", e.SetSettingGroupValue(ctx, "X/PTOC1$SG$StrVal$setMag$f", 1, mms.NewFloat(1)), ErrNotFound},
		{"not a setting", e.SetSettingGroupValue(ctx, sgFixtureLD+"/LLN0$ST$Mod$stVal", 1, mms.NewInteger(1)), ErrNotFound},
		{"group 0", e.SetSettingGroupValue(ctx, keyStrSG, 0, mms.NewFloat(1)), ErrInvalidArgument},
		{"group 4", e.SetSettingGroupValue(ctx, keyStrSG, 4, mms.NewFloat(1)), ErrInvalidArgument},
		{"type", e.SetSettingGroupValue(ctx, keyStrSG, 1, mms.NewInteger(1)), ErrTypeMismatch},
	} {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, c.err, c.want)
		}
	}
	if _, err := e.SettingGroupValue(keyStrSG, 9); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("SettingGroupValue group 9: %v", err)
	}
	if _, err := e.SettingGroupValue("X/y", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("SettingGroupValue unknown: %v", err)
	}
	if keys := e.SettingKeys("nope"); keys != nil {
		t.Errorf("SettingKeys(nope) = %v", keys)
	}
}

// Applications that kept the group values themselves before v1.18.0 set
// FC=SG in OnActiveSGChanged and FC=SE in OnEditSGSelected: those values
// win and are kept in the banks.
func TestSettingGroupEngine_HandlerValuesWin(t *testing.T) {
	var srv *Server
	srv = sgFixtureServer(t, SettingGroupHandler{
		OnActiveSGChanged: func(ctx context.Context, _ string, sg uint8) error {
			if sg == 2 {
				srv.SetValue(ctx, keyStrSG, mms.NewFloat(42))
			}
			return nil
		},
		OnEditSGSelected: func(ctx context.Context, _ string, sg uint8) error {
			if sg == 3 {
				srv.ValueStore().Set(keyStrSE, mms.NewFloat(43))
			}
			return nil
		},
	})
	ctx := context.Background()
	e := srv.SettingGroupEngine()
	if err := srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if v := srv.ValueStore().Get(keyStrSG); floatOf(t, v) != 42 {
		t.Errorf("active StrVal = %v, want the handler's 42", v)
	}
	if v := srv.ValueStore().Get(keyOpDlSG); intOf(t, v) != 500 {
		t.Errorf("active OpDlTmms = %v, want group 2's 500", v)
	}
	if got := groupFloat(t, srv, keyStrSG, 1); got != 600 {
		t.Errorf("group 1 = %v, want 600 (the value before the callback)", got)
	}
	// Re-activating group 2 keeps the handler's value.
	if err := srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, 2); err != nil {
		t.Fatal(err)
	}
	if got := groupFloat(t, srv, keyStrSG, 2); got != 42 {
		t.Errorf("group 2 = %v, want 42", got)
	}
	if err := e.HandleSGCBWrite(ctx, sgFixtureLD, "EditSG", mms.NewUnsigned(3)); err != nil {
		t.Fatal(err)
	}
	if v := srv.ValueStore().Get(keyStrSE); floatOf(t, v) != 43 {
		t.Errorf("edit StrVal = %v, want the handler's 43", v)
	}
	if v := srv.ValueStore().Get(sgFixtureLD + "/PTOC1$SE$OpDlTmms$setVal"); intOf(t, v) != 100 {
		t.Errorf("edit OpDlTmms = %v, want group 3's 100", v)
	}
}
