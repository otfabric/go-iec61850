// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

func testJournalModel() *servermodel.Model {
	return &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []servermodel.DataObject{{
					Name: "Mod",
					Attributes: []servermodel.DataAttribute{
						{Name: "stVal", FC: "ST", BType: "INT32"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					},
				}},
				Logs: []servermodel.LogDef{
					{Name: "log1"},
				},
			}},
		}},
	}
}

func newTestJournalServer(t *testing.T) *Server {
	t.Helper()
	model := testJournalModel()

	mmsSrv := mms.NewServer(mms.ServerOptions{})
	vs, err := servermodel.RegisterModel(mmsSrv, model, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	return &Server{
		logger:   noopLogger(),
		model:    model,
		store:    vs,
		mms:      mmsSrv,
		controls: make(map[string]*controlRegistration),
	}
}

// --- MemoryJournalProvider tests ---

func TestMemoryJournalProvider_RegisterAndList(t *testing.T) {
	p := NewMemoryJournalProvider()
	p.RegisterJournal("LD1", "LLN0$log1")
	p.RegisterJournal("LD1", "LLN0$log2")
	p.RegisterJournal("LD2", "LLN0$log1")

	names, err := p.ListJournals(context.Background(), "LD1")
	if err != nil {
		t.Fatalf("ListJournals: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d journals, want 2", len(names))
	}

	names2, _ := p.ListJournals(context.Background(), "LD2")
	if len(names2) != 1 {
		t.Fatalf("got %d LD2 journals, want 1", len(names2))
	}

	names3, _ := p.ListJournals(context.Background(), "LD_unknown")
	if len(names3) != 0 {
		t.Fatalf("got %d unknown domain journals, want 0", len(names3))
	}
}

func TestMemoryJournalProvider_RegisterIdempotent(t *testing.T) {
	p := NewMemoryJournalProvider()
	p.RegisterJournal("LD1", "LLN0$log1")
	p.AddEntry("LD1", "LLN0$log1", time.Now(), nil)
	p.RegisterJournal("LD1", "LLN0$log1") // should not clear entries
	if p.EntryCount("LD1", "LLN0$log1") != 1 {
		t.Fatal("re-register cleared existing entries")
	}
}

func TestMemoryJournalProvider_AddEntry(t *testing.T) {
	p := NewMemoryJournalProvider()
	p.RegisterJournal("LD1", "LLN0$log1")

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	id1 := p.AddEntry("LD1", "LLN0$log1", t0, []mms.JournalVariable{
		{Tag: "LLN0$ST$Mod$stVal", Value: mms.NewInteger(1)},
	})
	id2 := p.AddEntry("LD1", "LLN0$log1", t0, []mms.JournalVariable{
		{Tag: "LLN0$ST$Mod$stVal", Value: mms.NewInteger(2)},
	})

	if len(id1) != 8 || len(id2) != 8 {
		t.Fatalf("entry IDs should be 8 bytes, got %d and %d", len(id1), len(id2))
	}

	v1 := binary.BigEndian.Uint64(id1)
	v2 := binary.BigEndian.Uint64(id2)
	if v2 <= v1 {
		t.Fatalf("entry IDs should be monotonically increasing: %d <= %d", v2, v1)
	}

	if p.EntryCount("LD1", "LLN0$log1") != 2 {
		t.Fatalf("expected 2 entries, got %d", p.EntryCount("LD1", "LLN0$log1"))
	}
}

func TestMemoryJournalProvider_AddEntry_ImplicitCreate(t *testing.T) {
	p := NewMemoryJournalProvider()
	p.AddEntry("LD1", "LLN0$log1", time.Now(), nil)
	if p.EntryCount("LD1", "LLN0$log1") != 1 {
		t.Fatal("implicit journal creation failed")
	}
}

func TestMemoryJournalProvider_MaxEntries(t *testing.T) {
	p := NewMemoryJournalProvider(WithMaxEntries(3))

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), nil)
	}

	if p.EntryCount("LD1", "j1") != 3 {
		t.Fatalf("expected 3 entries (max), got %d", p.EntryCount("LD1", "j1"))
	}

	result, err := p.ReadTimeRange(context.Background(), "LD1", "j1",
		t0, t0.Add(10*time.Second), 100)
	if err != nil {
		t.Fatalf("ReadTimeRange: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}
	if !result.Entries[0].OccurrenceTime.Equal(t0.Add(2 * time.Second)) {
		t.Fatalf("oldest entry should be at t+2s, got %v", result.Entries[0].OccurrenceTime)
	}
}

func TestMemoryJournalProvider_WithMaxEntries_InvalidIgnored(t *testing.T) {
	p := NewMemoryJournalProvider(WithMaxEntries(0))
	if p.maxSize != 10000 {
		t.Fatalf("invalid max (0) should be ignored, got %d", p.maxSize)
	}
	p2 := NewMemoryJournalProvider(WithMaxEntries(-5))
	if p2.maxSize != 10000 {
		t.Fatalf("negative max should be ignored, got %d", p2.maxSize)
	}
}

func TestMemoryJournalProvider_ReadTimeRange(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), []mms.JournalVariable{
			{Tag: "val", Value: mms.NewInteger(int64(i))},
		})
	}

	result, err := p.ReadTimeRange(context.Background(), "LD1", "j1",
		t0.Add(1*time.Second), t0.Add(3*time.Second), 100)
	if err != nil {
		t.Fatalf("ReadTimeRange: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries (t+1..t+3), got %d", len(result.Entries))
	}
	if result.MoreFollows {
		t.Fatal("unexpected MoreFollows")
	}
}

func TestMemoryJournalProvider_ReadTimeRange_MoreFollows(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), nil)
	}

	result, err := p.ReadTimeRange(context.Background(), "LD1", "j1",
		t0, t0.Add(20*time.Second), 3)
	if err != nil {
		t.Fatalf("ReadTimeRange: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries (maxEntries), got %d", len(result.Entries))
	}
	if !result.MoreFollows {
		t.Fatal("expected MoreFollows=true")
	}
}

func TestMemoryJournalProvider_ReadTimeRange_UnknownJournal(t *testing.T) {
	p := NewMemoryJournalProvider()
	result, err := p.ReadTimeRange(context.Background(), "LD1", "nope",
		time.Now(), time.Now(), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries for unknown journal, got %d", len(result.Entries))
	}
}

func TestMemoryJournalProvider_ReadStartAfter(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids [][]byte
	for i := 0; i < 5; i++ {
		id := p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), nil)
		ids = append(ids, id)
	}

	result, err := p.ReadStartAfter(context.Background(), "LD1", "j1",
		ids[1], t0.Add(1*time.Second), 100)
	if err != nil {
		t.Fatalf("ReadStartAfter: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries after entry 1, got %d", len(result.Entries))
	}
	if !result.Entries[0].OccurrenceTime.Equal(t0.Add(2 * time.Second)) {
		t.Fatalf("first entry should be at t+2s, got %v", result.Entries[0].OccurrenceTime)
	}
	if result.MoreFollows {
		t.Fatal("unexpected MoreFollows")
	}
}

func TestMemoryJournalProvider_ReadStartAfter_MoreFollows(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids [][]byte
	for i := 0; i < 10; i++ {
		id := p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), nil)
		ids = append(ids, id)
	}

	result, err := p.ReadStartAfter(context.Background(), "LD1", "j1",
		ids[2], t0.Add(2*time.Second), 3)
	if err != nil {
		t.Fatalf("ReadStartAfter: %v", err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.Entries))
	}
	if !result.MoreFollows {
		t.Fatal("expected MoreFollows=true")
	}
}

func TestMemoryJournalProvider_ReadStartAfter_UnknownJournal(t *testing.T) {
	p := NewMemoryJournalProvider()
	result, err := p.ReadStartAfter(context.Background(), "LD1", "nope",
		nil, time.Now(), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries for unknown journal, got %d", len(result.Entries))
	}
}

// --- Cursor correctness: no-duplicate, no-skip paging ---

func TestMemoryJournalProvider_PaginationNoDuplicatesNoSkips(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	totalEntries := 25
	for i := 0; i < totalEntries; i++ {
		p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), []mms.JournalVariable{
			{Tag: "seq", Value: mms.NewInteger(int64(i))},
		})
	}

	pageSize := 7
	ctx := context.Background()

	result, err := p.ReadTimeRange(ctx, "LD1", "j1", t0, t0.Add(1*time.Hour), pageSize)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	var all []mms.JournalEntry
	all = append(all, result.Entries...)

	for result.MoreFollows {
		last := all[len(all)-1]
		result, err = p.ReadStartAfter(ctx, "LD1", "j1",
			last.EntryID, last.OccurrenceTime, pageSize)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		all = append(all, result.Entries...)
	}

	if len(all) != totalEntries {
		t.Fatalf("expected %d total entries, got %d", totalEntries, len(all))
	}

	seen := make(map[uint64]bool)
	for i, e := range all {
		id := binary.BigEndian.Uint64(e.EntryID)
		if seen[id] {
			t.Fatalf("duplicate entry ID %d at index %d", id, i)
		}
		seen[id] = true
	}

	for i := 1; i < len(all); i++ {
		prev := binary.BigEndian.Uint64(all[i-1].EntryID)
		cur := binary.BigEndian.Uint64(all[i].EntryID)
		if cur != prev+1 {
			t.Fatalf("entry IDs not consecutive: %d -> %d at index %d", prev, cur, i)
		}
	}
}

func TestMemoryJournalProvider_SameTimestampPaging(t *testing.T) {
	p := NewMemoryJournalProvider()

	sameTime := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	totalEntries := 15
	for i := 0; i < totalEntries; i++ {
		p.AddEntry("LD1", "j1", sameTime, []mms.JournalVariable{
			{Tag: "seq", Value: mms.NewInteger(int64(i))},
		})
	}

	pageSize := 4
	ctx := context.Background()

	result, err := p.ReadTimeRange(ctx, "LD1", "j1", sameTime, sameTime, pageSize)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	var all []mms.JournalEntry
	all = append(all, result.Entries...)

	for result.MoreFollows {
		last := all[len(all)-1]
		result, err = p.ReadStartAfter(ctx, "LD1", "j1",
			last.EntryID, last.OccurrenceTime, pageSize)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		all = append(all, result.Entries...)
	}

	if len(all) != totalEntries {
		t.Fatalf("expected %d entries with same timestamp, got %d", totalEntries, len(all))
	}

	seen := make(map[uint64]bool)
	for _, e := range all {
		id := binary.BigEndian.Uint64(e.EntryID)
		if seen[id] {
			t.Fatal("duplicate entry ID in same-timestamp pagination")
		}
		seen[id] = true
	}
}

// --- JournalEngine tests ---

func TestJournalEngine_RegisterFromModel(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	if len(engine.journals) != 1 {
		t.Fatalf("expected 1 journal registered, got %d", len(engine.journals))
	}

	jd, ok := engine.journals["LD1/LLN0$log1"]
	if !ok {
		t.Fatal("expected journal LD1/LLN0$log1")
	}
	if jd.ldName != "LD1" || jd.journalName != "LLN0$log1" || jd.logName != "log1" {
		t.Fatalf("unexpected journalDef: %+v", jd)
	}

	if engine.provider.EntryCount("LD1", "LLN0$log1") != 0 {
		t.Fatal("newly registered journal should have 0 entries")
	}
}

func TestJournalEngine_LogEvent(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	now := time.Now()
	entryID := engine.LogEvent("LD1", "LLN0$log1", now, []mms.JournalVariable{
		{Tag: "LLN0$ST$Mod$stVal", Value: mms.NewInteger(42)},
	})

	if len(entryID) != 8 {
		t.Fatalf("expected 8-byte entry ID, got %d", len(entryID))
	}

	if engine.provider.EntryCount("LD1", "LLN0$log1") != 1 {
		t.Fatal("expected 1 entry after LogEvent")
	}

	result, err := engine.provider.ReadTimeRange(context.Background(), "LD1", "LLN0$log1",
		now.Add(-time.Second), now.Add(time.Second), 100)
	if err != nil {
		t.Fatalf("ReadTimeRange: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	if result.Entries[0].Variables[0].Tag != "LLN0$ST$Mod$stVal" {
		t.Fatalf("unexpected tag: %s", result.Entries[0].Variables[0].Tag)
	}
}

func TestJournalEngine_LogValueWrite(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	storeKey := "LD1/LLN0$ST$Mod$stVal"
	srv.store.Set(storeKey, mms.NewInteger(99))

	now := time.Now()
	engine.LogValueWrite(context.Background(), storeKey, now)

	if engine.provider.EntryCount("LD1", "LLN0$log1") != 1 {
		t.Fatal("expected 1 entry after LogValueChange")
	}

	result, _ := engine.provider.ReadTimeRange(context.Background(), "LD1", "LLN0$log1",
		now.Add(-time.Second), now.Add(time.Second), 100)
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	if result.Entries[0].Variables[0].Tag != "LLN0$ST$Mod$stVal" {
		t.Fatalf("unexpected tag: %s", result.Entries[0].Variables[0].Tag)
	}
	v, ok := result.Entries[0].Variables[0].Value.Int64()
	if !ok || v != 99 {
		t.Fatalf("expected value 99, got %d (ok=%v)", v, ok)
	}
}

func TestJournalEngine_LogValueWrite_InvalidKey(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	engine.LogValueWrite(context.Background(), "invalidkey", time.Now())
	if engine.provider.EntryCount("LD1", "LLN0$log1") != 0 {
		t.Fatal("should not log for invalid store key")
	}
}

func TestJournalEngine_LogValueWrite_NilValue(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	engine.LogValueWrite(context.Background(), "LD1/nonexistent", time.Now())
	if engine.provider.EntryCount("LD1", "LLN0$log1") != 0 {
		t.Fatal("should not log when value is nil")
	}
}

func TestJournalEngine_LogValueWrite_WrongLD(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	srv.store.Set("LD2/some$item", mms.NewInteger(1))
	engine.LogValueWrite(context.Background(), "LD2/some$item", time.Now())

	if engine.provider.EntryCount("LD1", "LLN0$log1") != 0 {
		t.Fatal("should not log to LD1 journal for LD2 key")
	}
}

func TestJournalEngine_Provider(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()
	if engine.Provider() == nil {
		t.Fatal("provider should not be nil")
	}
}

func TestJournalEngine_WithJournalProvider(t *testing.T) {
	srv := newTestJournalServer(t)
	customProvider := NewMemoryJournalProvider(WithMaxEntries(50))
	engine := srv.EnableJournals(WithJournalProvider(customProvider))

	if engine.Provider() != customProvider {
		t.Fatal("engine should use custom provider")
	}

	engine.LogEvent("LD1", "LLN0$log1", time.Now(), nil)
	if customProvider.EntryCount("LD1", "LLN0$log1") != 1 {
		t.Fatal("entry should be in custom provider")
	}
}

func TestJournalEngine_WithJournalMaxEntries(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals(WithJournalMaxEntries(5))

	for i := 0; i < 10; i++ {
		engine.LogEvent("LD1", "LLN0$log1", time.Now(), nil)
	}

	if engine.provider.EntryCount("LD1", "LLN0$log1") != 5 {
		t.Fatalf("expected 5 entries (capped), got %d", engine.provider.EntryCount("LD1", "LLN0$log1"))
	}
}

func TestJournalEngine_WithJournalMaxEntries_Invalid(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals(WithJournalMaxEntries(0))
	if engine.provider.maxSize != 10000 {
		t.Fatalf("invalid max should be ignored, got %d", engine.provider.maxSize)
	}
}

func TestJournalEngine_WithJournalProvider_Nil(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals(WithJournalProvider(nil))
	if engine.Provider() == nil {
		t.Fatal("nil provider option should be ignored")
	}
}

// --- Server integration tests ---

func TestServer_SetValue_LogsToJournal(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()

	ctx := context.Background()
	storeKey := "LD1/LLN0$ST$Mod$stVal"

	srv.store.Set(storeKey, mms.NewInteger(0))

	srv.SetValue(ctx, storeKey, mms.NewInteger(1))
	srv.SetValue(ctx, storeKey, mms.NewInteger(2))
	srv.SetValue(ctx, storeKey, mms.NewInteger(3))

	if engine.provider.EntryCount("LD1", "LLN0$log1") != 3 {
		t.Fatalf("expected 3 entries from SetValue, got %d",
			engine.provider.EntryCount("LD1", "LLN0$log1"))
	}
}

func TestServer_SetValue_NoJournalEngine(t *testing.T) {
	srv := newTestJournalServer(t)
	ctx := context.Background()
	srv.SetValue(ctx, "LD1/LLN0$ST$Mod$stVal", mms.NewInteger(1))
}

func TestServer_JournalEngine_NilBeforeEnable(t *testing.T) {
	srv := newTestJournalServer(t)
	if srv.JournalEngine() != nil {
		t.Fatal("journal engine should be nil before EnableJournals")
	}
}

func TestServer_JournalEngine_AfterEnable(t *testing.T) {
	srv := newTestJournalServer(t)
	engine := srv.EnableJournals()
	if srv.JournalEngine() != engine {
		t.Fatal("JournalEngine() should return the enabled engine")
	}
}

// --- Model helpers ---

func TestModelHasLogs_True(t *testing.T) {
	m := testJournalModel()
	if !modelHasLogs(m) {
		t.Fatal("model with logs should return true")
	}
}

func TestModelHasLogs_False(t *testing.T) {
	m := &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
			}},
		}},
	}
	if modelHasLogs(m) {
		t.Fatal("model without logs should return false")
	}
}

// --- Multi-log model test ---

func TestJournalEngine_MultipleLogsMultipleLDs(t *testing.T) {
	model := &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{
			{
				Name: "LD1",
				LogicalNodes: []servermodel.LogicalNode{{
					Name:    "LLN0",
					LNClass: "LLN0",
					DataObjects: []servermodel.DataObject{{
						Name: "Mod",
						Attributes: []servermodel.DataAttribute{
							{Name: "stVal", FC: "ST", BType: "INT32"},
						},
					}},
					Logs: []servermodel.LogDef{
						{Name: "logA"},
						{Name: "logB"},
					},
				}},
			},
			{
				Name: "LD2",
				LogicalNodes: []servermodel.LogicalNode{{
					Name:    "LLN0",
					LNClass: "LLN0",
					DataObjects: []servermodel.DataObject{{
						Name: "Health",
						Attributes: []servermodel.DataAttribute{
							{Name: "stVal", FC: "ST", BType: "INT32"},
						},
					}},
					Logs: []servermodel.LogDef{
						{Name: "logC"},
					},
				}},
			},
		},
	}

	mmsSrv := mms.NewServer(mms.ServerOptions{})
	vs, err := servermodel.RegisterModel(mmsSrv, model, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	srv := &Server{
		logger:   noopLogger(),
		model:    model,
		store:    vs,
		mms:      mmsSrv,
		controls: make(map[string]*controlRegistration),
	}

	engine := srv.EnableJournals()
	if len(engine.journals) != 3 {
		t.Fatalf("expected 3 journals, got %d", len(engine.journals))
	}

	srv.store.Set("LD1/LLN0$ST$Mod$stVal", mms.NewInteger(1))
	srv.SetValue(context.Background(), "LD1/LLN0$ST$Mod$stVal", mms.NewInteger(42))

	if engine.provider.EntryCount("LD1", "LLN0$logA") != 1 {
		t.Fatalf("expected 1 entry in logA, got %d", engine.provider.EntryCount("LD1", "LLN0$logA"))
	}
	if engine.provider.EntryCount("LD1", "LLN0$logB") != 1 {
		t.Fatalf("expected 1 entry in logB, got %d", engine.provider.EntryCount("LD1", "LLN0$logB"))
	}
	if engine.provider.EntryCount("LD2", "LLN0$logC") != 0 {
		t.Fatalf("expected 0 entries in LD2/logC, got %d", engine.provider.EntryCount("LD2", "LLN0$logC"))
	}
}

func TestEncodeEntryIDUint64(t *testing.T) {
	id := encodeEntryIDUint64(256)
	if len(id) != 8 {
		t.Fatalf("expected 8-byte ID, got %d", len(id))
	}
	v := binary.BigEndian.Uint64(id)
	if v != 256 {
		t.Fatalf("expected 256, got %d", v)
	}
}

// --- MoreFollows false-positive fix tests ---

func TestMemoryJournalProvider_ReadTimeRange_ExactPageNoMoreFollows(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), nil)
	}

	result, err := p.ReadTimeRange(context.Background(), "LD1", "j1",
		t0, t0.Add(10*time.Second), 5)
	if err != nil {
		t.Fatalf("ReadTimeRange: %v", err)
	}
	if len(result.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(result.Entries))
	}
	if result.MoreFollows {
		t.Fatal("MoreFollows should be false when page size equals total entries")
	}
}

func TestMemoryJournalProvider_ReadStartAfter_ExactPageNoMoreFollows(t *testing.T) {
	p := NewMemoryJournalProvider()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids [][]byte
	for i := 0; i < 6; i++ {
		id := p.AddEntry("LD1", "j1", t0.Add(time.Duration(i)*time.Second), nil)
		ids = append(ids, id)
	}

	result, err := p.ReadStartAfter(context.Background(), "LD1", "j1",
		ids[0], t0, 5)
	if err != nil {
		t.Fatalf("ReadStartAfter: %v", err)
	}
	if len(result.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(result.Entries))
	}
	if result.MoreFollows {
		t.Fatal("MoreFollows should be false when remaining entries equal page size")
	}
}

// --- ListJournals determinism test ---

func TestMemoryJournalProvider_ListJournals_Sorted(t *testing.T) {
	p := NewMemoryJournalProvider()
	p.RegisterJournal("LD1", "LLN0$logZ")
	p.RegisterJournal("LD1", "LLN0$logA")
	p.RegisterJournal("LD1", "LLN0$logM")

	names, err := p.ListJournals(context.Background(), "LD1")
	if err != nil {
		t.Fatalf("ListJournals: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 journals, got %d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Fatalf("journals not sorted: %v", names)
		}
	}
}

// --- Auto-adopt MMS journal provider test ---

func TestServer_EnableJournals_AutoAdoptsMmsProvider(t *testing.T) {
	model := testJournalModel()

	jp := NewMemoryJournalProvider()
	mmsSrv := mms.NewServer(mms.ServerOptions{JournalProvider: jp})
	vs, err := servermodel.RegisterModel(mmsSrv, model, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	srv := &Server{
		logger:             noopLogger(),
		model:              model,
		store:              vs,
		mms:                mmsSrv,
		controls:           make(map[string]*controlRegistration),
		mmsJournalProvider: jp,
	}

	engine := srv.EnableJournals()
	if engine.Provider() != jp {
		t.Fatal("engine should auto-adopt the MMS journal provider")
	}

	engine.LogEvent("LD1", "LLN0$log1", time.Now(), nil)
	if jp.EntryCount("LD1", "LLN0$log1") != 1 {
		t.Fatal("entry should be in the MMS provider")
	}
}
