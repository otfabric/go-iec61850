// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// Spec-verification tests of the log control block (IEC 61850-7-2 §17.3,
// IEC 61850-8-1 §17.3).

const (
	specLCB     = "LLN0$LG$lcb1"
	specJournal = "LLN0$EventLog"
)

var specSetKey = servermodel.StoreKey("LD1", "LLN0$CF$Set$val")

func lcbModel() *servermodel.Model {
	return &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []servermodel.LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			DataObjects: []servermodel.DataObject{
				{Name: "Mod", Attributes: []servermodel.DataAttribute{
					{Name: "stVal", FC: "ST", BType: "INT32", Dchg: true},
					{Name: "q", FC: "ST", BType: "Quality", Qchg: true},
				}},
				{Name: "Beh", Attributes: []servermodel.DataAttribute{
					{Name: "stVal", FC: "ST", BType: "INT32", Dchg: true},
				}},
				{Name: "Set", Attributes: []servermodel.DataAttribute{
					{Name: "val", FC: "CF", BType: "INT32", Dchg: true},
				}},
			},
			DataSets: []servermodel.DataSetDef{
				{Name: "dsLog", Members: []servermodel.DataSetMemberDef{
					{LNName: "LLN0", DOPath: "Mod.stVal", FC: "ST"},
					{LNName: "LLN0", DOPath: "Set.val", FC: "CF"},
				}},
				{Name: "dsBeh", Members: []servermodel.DataSetMemberDef{
					{LNName: "LLN0", DOPath: "Beh.stVal", FC: "ST"},
				}},
			},
			Logs: []servermodel.LogDef{
				{Name: "lcb1", LogName: "EventLog", DatSet: "dsLog", TrgOps: servermodel.TrgOpsDef{Dchg: true}, LogEna: true},
				{Name: "lcb2", LogName: "Other"},
			},
		}},
	}}}
}

func newLCBServer(t *testing.T, m *servermodel.Model) (*Server, *JournalEngine) {
	t.Helper()
	srv, err := NewServer(m, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	je := srv.EnableJournals()
	t.Cleanup(srv.Close)
	return srv, je
}

func journalEntries(t *testing.T, je *JournalEngine, journal string) []mms.JournalEntry {
	t.Helper()
	res, err := je.Provider().ReadTimeRange(context.Background(), "LD1", journal, journalRangeFirst, journalRangeLast, 0)
	if err != nil {
		t.Fatal(err)
	}
	return res.Entries
}

// TestSpec_8_1_17_3_3_2_LCBStructure: the LCB is the MMS structure of
// Table 65 under FC LG, in this order (LOG-01).
func TestSpec_8_1_17_3_3_2_LCBStructure(t *testing.T) {
	srv, _ := newLCBServer(t, lcbModel())
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	attrs, err := c.mmsClient.GetVariableAccessAttributes(ctx, mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: specLCB})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		typ  mms.ValueType
	}{
		{"LogEna", mms.ValueTypeBoolean}, {"LogRef", mms.ValueTypeVisibleString}, {"DatSet", mms.ValueTypeVisibleString},
		{"OldEntrTm", mms.ValueTypeBinaryTime}, {"NewEntrTm", mms.ValueTypeBinaryTime},
		{"OldEnt", mms.ValueTypeOctetString}, {"NewEnt", mms.ValueTypeOctetString},
		{"TrgOps", mms.ValueTypeBitString}, {"IntgPd", mms.ValueTypeUnsigned},
	}
	if len(attrs.TypeSpec.Elements) != len(want) {
		t.Fatalf("LCB has %d components, want %d", len(attrs.TypeSpec.Elements), len(want))
	}
	for i, w := range want {
		if e := attrs.TypeSpec.Elements[i]; e.Name != w.name || e.Type.Type != w.typ {
			t.Errorf("component %d = %s/%v, want %s/%v", i, e.Name, e.Type.Type, w.name, w.typ)
		}
	}
	if ref, _ := rawRead(t, c, specLCB+"$LogRef").VisibleString(); ref != "LD1/"+specJournal {
		t.Errorf("LogRef = %q", ref)
	}
	if ds, _ := rawRead(t, c, specLCB+"$DatSet").VisibleString(); ds != "LD1/LLN0$dsLog" {
		t.Errorf("DatSet = %q", ds)
	}
	// The LN structure exposes the LG FC group.
	ln, err := c.mmsClient.GetVariableAccessAttributes(ctx, mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: "LLN0$LG"})
	if err != nil || len(ln.TypeSpec.Elements) != 2 {
		t.Errorf("LLN0$LG = %+v, %v", ln, err)
	}
	journals, err := c.ListJournals(ctx, "LD1")
	if err != nil || len(journals) != 2 || journals[0] != specJournal || journals[1] != "LLN0$Other" {
		t.Errorf("journals = %v, %v", journals, err)
	}
}

// TestSpec_7_2_17_3_4_LogsOnlyDatasetMembersWithReason: only data set
// members whose trigger condition is met are journaled, as the DatRef
// value followed by "ReasonCode", and only while LogEna is TRUE; a
// client write is logged too (IEC 61850-7-2 §17.3.4, IEC 61850-8-1
// §17.3.3.4.4) (LOG-03).
func TestSpec_7_2_17_3_4_LogsOnlyDatasetMembersWithReason(t *testing.T) {
	srv, je := newLCBServer(t, lcbModel())
	ctx := context.Background()
	srv.SetValue(ctx, specBehKey, mms.NewInteger(3))                                                                 // not a member
	srv.SetValue(ctx, servermodel.StoreKey("LD1", "LLN0$ST$Mod$q"), mms.NewBitStringWithLength([]byte{0x40, 0}, 13)) // no qchg in TrgOps
	srv.SetValue(ctx, specModKey, mms.NewInteger(7))
	srv.SetValue(ctx, specModKey, mms.NewInteger(7)) // no change, no dupd
	entries := journalEntries(t, je, specJournal)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: %+v", len(entries), entries)
	}
	vars := entries[0].Variables
	if len(vars) != 2 || vars[0].Tag != "LD1/LLN0$ST$Mod$stVal" || vars[1].Tag != lcbReasonTag {
		t.Fatalf("variables = %+v", vars)
	}
	if v, _ := vars[0].Value.Int64(); v != 7 {
		t.Errorf("value = %d", v)
	}
	if n, _ := vars[1].Value.BitStringLength(); n != 7 {
		t.Errorf("ReasonCode bits = %d", n)
	}
	if raw, _ := vars[1].Value.BitString(); raw[0] != 0x40 {
		t.Errorf("ReasonCode = %x, want data-change", raw)
	}

	c := dialLoopback(t, ctx, srv)
	if err := rawWrite(c, "LLN0$CF$Set$val", mms.NewInteger(11)); err != nil {
		t.Fatal(err)
	}
	if entries := journalEntries(t, je, specJournal); len(entries) != 2 || entries[1].Variables[0].Tag != "LD1/LLN0$CF$Set$val" {
		t.Fatalf("after client write: %+v", entries)
	}

	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(8))
	if n := len(journalEntries(t, je, specJournal)); n != 2 {
		t.Errorf("entries with LogEna=FALSE = %d, want 2", n)
	}
	if n := len(journalEntries(t, je, "LLN0$Other")); n != 0 {
		t.Errorf("lcb2 without a data set logged %d entries", n)
	}
}

// TestSpec_8_1_17_3_4_3_LCBWriteRules: "A V-Put, while the component
// LogEna has a value of TRUE, shall fail for the components of LogRef,
// DatSet, TrgOps, and IntgPd"; a LogRef or DatSet naming nothing fails;
// OldEntrTm/NewEntrTm/OldEnt/NewEnt are read-only (8-1 §17.3.4.3,
// Table 65) (LOG-01).
func TestSpec_8_1_17_3_4_3_LCBWriteRules(t *testing.T) {
	srv, _ := newLCBServer(t, lcbModel())
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	for attr, v := range map[string]*mms.Value{
		"LogRef": mms.NewVisibleString("LD1/LLN0$Other"),
		"DatSet": mms.NewVisibleString("LD1/LLN0$dsBeh"),
		"TrgOps": encodeTrgOps(TrgOpDataUpdate),
		"IntgPd": mms.NewUnsigned(1000),
	} {
		if code := dataAccessCode(rawWrite(c, specLCB+"$"+attr, v)); code != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("%s while enabled: code %v, want temporarily-unavailable", attr, code)
		}
	}
	for _, attr := range []string{"OldEntrTm", "NewEntrTm", "OldEnt", "NewEnt"} {
		v := rawRead(t, c, specLCB+"$"+attr)
		if code := dataAccessCode(rawWrite(c, specLCB+"$"+attr, v)); code != mms.DataAccessErrorObjectAccessDenied {
			t.Errorf("%s: code %v, want object-access-denied", attr, code)
		}
	}
	if code := dataAccessCode(rawWrite(c, specLCB, rawRead(t, c, specLCB))); code != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("structure write: code %v", code)
	}
	if code := dataAccessCode(rawWrite(c, specLCB+"$LogEna", mms.NewInteger(1))); code != mms.DataAccessErrorTypeInconsistent {
		t.Errorf("LogEna integer: code %v", code)
	}
	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	for attr, v := range map[string]*mms.Value{
		"LogRef": mms.NewVisibleString("LD1/LLN0$nope"),
		"DatSet": mms.NewVisibleString("LD1/LLN0$nope"),
	} {
		if code := dataAccessCode(rawWrite(c, specLCB+"$"+attr, v)); code != mms.DataAccessErrorObjectValueInvalid {
			t.Errorf("%s naming nothing: code %v, want object-value-invalid", attr, code)
		}
	}
	for attr, v := range map[string]*mms.Value{
		"LogRef": mms.NewVisibleString("LD1/LLN0$Other"),
		"DatSet": mms.NewVisibleString("LD1/LLN0$dsBeh"),
		"TrgOps": encodeTrgOps(TrgOpDataChanged),
		"IntgPd": mms.NewUnsigned(0),
	} {
		if err := rawWrite(c, specLCB+"$"+attr, v); err != nil {
			t.Errorf("%s while disabled: %v", attr, err)
		}
	}
	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	srv.SetValue(ctx, specBehKey, mms.NewInteger(5))
	srv.SetValue(ctx, specModKey, mms.NewInteger(5)) // no longer a member
	entries, err := c.ReadJournalAll(ctx, "LD1", "LLN0$Other", time.Time{}, time.Time{})
	if err != nil || len(entries) != 1 || entries[0].Variables[0].Tag != "LD1/LLN0$ST$Beh$stVal" {
		t.Fatalf("Other journal = %+v, %v", entries, err)
	}
	// NULL values select nothing.
	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{"LogRef", "DatSet"} {
		if err := rawWrite(c, specLCB+"$"+attr, mms.NewVisibleString("")); err != nil {
			t.Errorf("NULL %s: %v", attr, err)
		}
	}
}

// TestSpec_8_1_17_3_3_3_4_StatusReflectsJournal: "If there are no
// journal entries ... the value of these components shall be equal and
// the binary-time values shall be 0"; OldEnt/NewEnt are the entry IDs of
// the oldest and newest entries (8-1 §17.3.3.3.4/5) (LOG-01).
func TestSpec_8_1_17_3_3_3_4_StatusReflectsJournal(t *testing.T) {
	srv, je := newLCBServer(t, lcbModel())
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	read := func() (oldT, newT time.Time, oldID, newID []byte) {
		o, _ := rawRead(t, c, specLCB+"$OldEntrTm").BinaryTime()
		n, _ := rawRead(t, c, specLCB+"$NewEntrTm").BinaryTime()
		oldT, newT = time.UnixMilli(o).UTC(), time.UnixMilli(n).UTC()
		oldID, _ = rawRead(t, c, specLCB+"$OldEnt").OctetString()
		newID, _ = rawRead(t, c, specLCB+"$NewEnt").OctetString()
		return
	}
	oldT, newT, oldID, newID := read()
	if !oldT.Equal(newT) || !oldT.Equal(journalRangeFirst) || !bytes.Equal(oldID, make([]byte, 8)) || !bytes.Equal(newID, oldID) {
		t.Errorf("empty journal status = %v %v %x %x", oldT, newT, oldID, newID)
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	time.Sleep(5 * time.Millisecond)
	srv.SetValue(ctx, specModKey, mms.NewInteger(2))
	entries := journalEntries(t, je, specJournal)
	oldT, newT, oldID, newID = read()
	if !bytes.Equal(oldID, entries[0].EntryID) || !bytes.Equal(newID, entries[1].EntryID) {
		t.Errorf("OldEnt/NewEnt = %x/%x, want %x/%x", oldID, newID, entries[0].EntryID, entries[1].EntryID)
	}
	if !oldT.Equal(entries[0].OccurrenceTime) || !newT.Equal(entries[1].OccurrenceTime) {
		t.Errorf("OldEntrTm/NewEntrTm = %v/%v", oldT, newT)
	}
	// LogEvent goes through the same status update.
	id := je.LogEvent("LD1", specJournal, time.Now(), []mms.JournalVariable{{Tag: "x", Value: mms.NewBoolean(true)}})
	if _, _, _, newID = read(); !bytes.Equal(newID, id) {
		t.Errorf("NewEnt after LogEvent = %x, want %x", newID, id)
	}
}

// TestSpec_7_2_17_3_4_IntegrityLogged: with TrgOps integrity, all members
// are journaled every IntgPd with reason integrity (7-2 §17.3.4,
// §17.2.3.2.3.3) (LOG-03).
func TestSpec_7_2_17_3_4_IntegrityLogged(t *testing.T) {
	m := lcbModel()
	lcb := &m.LogicalDevices[0].LogicalNodes[0].Logs[0]
	lcb.TrgOps.Period, lcb.IntgPd = true, 20
	srv, je := newLCBServer(t, m)
	waitUntil(t, "integrity entry", func() bool { return len(journalEntries(t, je, specJournal)) > 0 })
	e := journalEntries(t, je, specJournal)[0]
	if len(e.Variables) != 4 || e.Variables[2].Tag != "LD1/LLN0$CF$Set$val" {
		t.Fatalf("integrity entry = %+v", e.Variables)
	}
	if raw, _ := e.Variables[1].Value.BitString(); raw[0] != 0x08 {
		t.Errorf("reason = %x, want integrity", raw)
	}
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	n := len(journalEntries(t, je, specJournal))
	time.Sleep(80 * time.Millisecond)
	if got := len(journalEntries(t, je, specJournal)); got != n {
		t.Errorf("integrity continued after LogEna=FALSE: %d → %d", n, got)
	}
	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "integrity after re-enable", func() bool { return len(journalEntries(t, je, specJournal)) > n })
}

// TestSpec_7_2_17_3_5_2_UnknownJournalFails: QueryLogByTime of a journal
// that does not exist is answered with Response– (7-2 §17.3.5.2.4,
// 8-1 §17.3.4.5) (LOG-05); omitted range bounds read the whole journal
// (7-2 §17.3.5.2, LOG-06).
func TestSpec_7_2_17_3_5_2_UnknownJournalFails(t *testing.T) {
	srv, _ := newLCBServer(t, lcbModel())
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	_, err := c.ReadJournal(ctx, "LD1", "LLN0$nope", time.Time{}, time.Time{})
	var dae *mms.DataAccessError
	var se *mms.ServiceError
	if err == nil || (!errors.As(err, &dae) && !errors.As(err, &se)) {
		t.Fatalf("unknown journal: %v, want a service error", err)
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	res, err := c.ReadJournal(ctx, "LD1", specJournal, time.Time{}, time.Time{})
	if err != nil || len(res.Entries) != 1 {
		t.Fatalf("open-ended read = %+v, %v", res, err)
	}
	if s, e := openJournalRange(time.Time{}, time.Time{}); !s.Equal(journalRangeFirst) || !e.Equal(journalRangeLast) {
		t.Errorf("openJournalRange = %v, %v", s, e)
	}
}

// TestJournalEngine_StopEndsIntegrity: Server.Close ends the LCB
// integrity goroutines.
func TestJournalEngine_StopEndsIntegrity(t *testing.T) {
	checkGoroutines(t, func() {
		m := lcbModel()
		lcb := &m.LogicalDevices[0].LogicalNodes[0].Logs[0]
		lcb.TrgOps.Period, lcb.IntgPd = true, 10
		srv, err := NewServer(m, ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		srv.EnableJournals()
		time.Sleep(30 * time.Millisecond)
		srv.Close()
	})
}

// TestJournalEngine_HandleLCBWriteUnknown: a write to an LCB the engine
// does not know is object-non-existent.
func TestJournalEngine_HandleLCBWriteUnknown(t *testing.T) {
	_, je := newLCBServer(t, lcbModel())
	if code := dataAccessCode(je.HandleLCBWrite(context.Background(), "LD1", "LLN0$LG$nope", "LogEna", mms.NewBoolean(true))); code != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("code %v", code)
	}
	if code := dataAccessCode(je.HandleLCBWrite(context.Background(), "LD1", specLCB, "Bogus", mms.NewBoolean(true))); code != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("unknown attribute: code %v", code)
	}
	if _, ok := parseLogRef("nodomain"); ok {
		t.Error("parseLogRef accepted a reference without a domain")
	}
}

// TestChaos_LCBConcurrentLogging: value writes, integrity logging and
// LCB reconfiguration from two clients run concurrently (race test).
func TestChaos_LCBConcurrentLogging(t *testing.T) {
	checkGoroutines(t, func() {
		m := lcbModel()
		lcb := &m.LogicalDevices[0].LogicalNodes[0].Logs[0]
		lcb.TrgOps.Period, lcb.IntgPd = true, 5
		m.LogicalDevices[0].LogicalNodes[0].Logs[1].DatSet = "dsBeh"
		srv, err := NewServer(m, ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		je := srv.EnableJournals()
		ctx := context.Background()
		a, b := dialLoopback(t, ctx, srv), dialLoopback(t, ctx, srv)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := range 200 {
				srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
				srv.SetValue(ctx, specBehKey, mms.NewInteger(int64(i)))
			}
		}()
		for i := range 20 {
			_ = rawWrite(a, specLCB+"$LogEna", mms.NewBoolean(i%2 == 0))
			_ = rawWrite(b, "LLN0$LG$lcb2$LogEna", mms.NewBoolean(i%2 == 1))
			_ = rawWrite(b, "LLN0$LG$lcb2$LogRef", mms.NewVisibleString("LD1/"+specJournal))
			_ = rawWrite(a, "LLN0$CF$Set$val", mms.NewInteger(int64(i)))
		}
		<-done
		if len(journalEntries(t, je, specJournal)) == 0 {
			t.Error("nothing journaled")
		}
		dropAssociation(a)
		dropAssociation(b)
		srv.Close()
	})
}
