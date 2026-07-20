// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

type memJournalProvider struct {
	journals map[string][]mms.JournalEntry
}

func (p *memJournalProvider) ListJournals(_ context.Context, domain string) ([]string, error) {
	var names []string
	for name := range p.journals {
		names = append(names, name)
	}
	return names, nil
}

func (p *memJournalProvider) ReadTimeRange(_ context.Context, _, journal string, start, stop time.Time, maxEntries int) (*mms.JournalResult, error) {
	all := p.journals[journal]
	var filtered []mms.JournalEntry
	for _, e := range all {
		if (e.OccurrenceTime.Equal(start) || e.OccurrenceTime.After(start)) &&
			(e.OccurrenceTime.Equal(stop) || e.OccurrenceTime.Before(stop)) {
			filtered = append(filtered, e)
			if len(filtered) >= maxEntries {
				return &mms.JournalResult{Entries: filtered, MoreFollows: true}, nil
			}
		}
	}
	return &mms.JournalResult{Entries: filtered, MoreFollows: false}, nil
}

func (p *memJournalProvider) ReadStartAfter(_ context.Context, _, journal string, afterID []byte, afterTime time.Time, maxEntries int) (*mms.JournalResult, error) {
	all := p.journals[journal]
	pastCursor := false
	var result []mms.JournalEntry
	for _, e := range all {
		if !pastCursor {
			if e.OccurrenceTime.Equal(afterTime) && bytes.Equal(e.EntryID, afterID) {
				pastCursor = true
				continue
			}
			if e.OccurrenceTime.After(afterTime) {
				pastCursor = true
			} else {
				continue
			}
		}
		result = append(result, e)
		if len(result) >= maxEntries {
			return &mms.JournalResult{Entries: result, MoreFollows: true}, nil
		}
	}
	return &mms.JournalResult{Entries: result, MoreFollows: false}, nil
}

func setupJournalLoopback(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 2, 0, 0, 0, time.UTC)

	jp := &memJournalProvider{
		journals: map[string][]mms.JournalEntry{
			"LLN0$journal1": {
				{
					EntryID:        []byte{0x01},
					OccurrenceTime: t0,
					Variables: []mms.JournalVariable{
						{Tag: "var1", Value: mms.NewInteger(100)},
					},
				},
				{
					EntryID:        []byte{0x02},
					OccurrenceTime: t1,
					Variables: []mms.JournalVariable{
						{Tag: "var1", Value: mms.NewInteger(200)},
						{Tag: "var2", Value: mms.NewBoolean(true)},
					},
				},
				{
					EntryID:        []byte{0x03},
					OccurrenceTime: t2,
					Variables: []mms.JournalVariable{
						{Tag: "var1", Value: mms.NewInteger(300)},
					},
				},
			},
		},
	}

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		JournalProvider: jp,
	})

	domain := "testLD"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client
}

func TestListJournals(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	names, err := client.ListJournals(ctx, "testLD")
	if err != nil {
		t.Fatalf("ListJournals: %v", err)
	}

	if len(names) != 1 {
		t.Fatalf("got %d journals, want 1", len(names))
	}
	if names[0] != "LLN0$journal1" {
		t.Errorf("journal name = %q, want %q", names[0], "LLN0$journal1")
	}
}

func TestListJournals_EmptyLD(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	_, err := client.ListJournals(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty LD")
	}
}

func TestReadJournal(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	stop := time.Date(2025, 1, 1, 2, 0, 0, 0, time.UTC)

	result, err := client.ReadJournal(ctx, "testLD", "LLN0$journal1", start, stop)
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(result.Entries))
	}
	if result.MoreFollows {
		t.Error("MoreFollows should be false")
	}

	e := result.Entries[1]
	if len(e.Variables) != 2 {
		t.Fatalf("entry 1 has %d variables, want 2", len(e.Variables))
	}
	if e.Variables[0].Tag != "var1" {
		t.Errorf("entry 1 var 0 tag = %q, want %q", e.Variables[0].Tag, "var1")
	}

	v, err := e.Variables[0].Value.Int64()
	if err != nil {
		t.Fatalf("Int64: %v", err)
	}
	if v != 200 {
		t.Errorf("var1 value = %d, want 200", v)
	}
}

func TestReadJournal_EmptyInputs(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()
	start := time.Now()
	stop := start.Add(time.Hour)

	_, err := client.ReadJournal(ctx, "", "j", start, stop)
	if err == nil {
		t.Fatal("expected error for empty LD")
	}

	_, err = client.ReadJournal(ctx, "LD", "", start, stop)
	if err == nil {
		t.Fatal("expected error for empty journal")
	}
}

func TestReadJournalAfter(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	afterTime := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	result, err := client.ReadJournalAfter(ctx, "testLD", "LLN0$journal1", afterTime, []byte{0x02})
	if err != nil {
		t.Fatalf("ReadJournalAfter: %v", err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}

	v, err := result.Entries[0].Variables[0].Value.Int64()
	if err != nil {
		t.Fatalf("Int64: %v", err)
	}
	if v != 300 {
		t.Errorf("value = %d, want 300", v)
	}
}

func TestReadJournalAfter_EmptyInputs(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()
	at := time.Now()

	_, err := client.ReadJournalAfter(ctx, "", "j", at, nil)
	if err == nil {
		t.Fatal("expected error for empty LD")
	}

	_, err = client.ReadJournalAfter(ctx, "LD", "", at, nil)
	if err == nil {
		t.Fatal("expected error for empty journal")
	}
}

func TestReadJournal_ClosedClient(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ReadJournal(ctx, "testLD", "LLN0$journal1", time.Now(), time.Now())
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestListJournals_ClosedClient(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ListJournals(ctx, "testLD")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestReadJournal_EmptyResult(t *testing.T) {
	client := setupJournalLoopback(t)
	ctx := context.Background()

	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	stop := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)

	result, err := client.ReadJournal(ctx, "testLD", "LLN0$journal1", start, stop)
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("got %d entries, want 0", len(result.Entries))
	}
}

func TestReadJournalAfter_SameTimestamp(t *testing.T) {
	ctx := context.Background()

	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	jp := &memJournalProvider{
		journals: map[string][]mms.JournalEntry{
			"LLN0$journal1": {
				{
					EntryID:        []byte{0x0A},
					OccurrenceTime: ts,
					Variables: []mms.JournalVariable{
						{Tag: "v1", Value: mms.NewInteger(1)},
					},
				},
				{
					EntryID:        []byte{0x0B},
					OccurrenceTime: ts,
					Variables: []mms.JournalVariable{
						{Tag: "v1", Value: mms.NewInteger(2)},
					},
				},
				{
					EntryID:        []byte{0x0C},
					OccurrenceTime: ts,
					Variables: []mms.JournalVariable{
						{Tag: "v1", Value: mms.NewInteger(3)},
					},
				},
			},
		},
	}

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		JournalProvider: jp,
	})
	if err := srv.RegisterDomain("testLD"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	result, err := client.ReadJournalAfter(ctx, "testLD", "LLN0$journal1", ts, []byte{0x0A})
	if err != nil {
		t.Fatalf("ReadJournalAfter: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("got %d entries, want 2 (entries after cursor with same timestamp)", len(result.Entries))
	}

	v0, _ := result.Entries[0].Variables[0].Value.Int64()
	v1, _ := result.Entries[1].Variables[0].Value.Int64()
	if v0 != 2 || v1 != 3 {
		t.Errorf("values = [%d, %d], want [2, 3]", v0, v1)
	}
}

func TestConvertJournalResult_Nil(t *testing.T) {
	_, err := convertJournalResult(nil)
	if err == nil {
		t.Fatal("expected error for nil journal result")
	}
}

// --- Auto-pagination tests ---

// paginatingJournalProvider limits page size to force pagination.
type paginatingJournalProvider struct {
	journals map[string][]mms.JournalEntry
	pageSize int
}

func (p *paginatingJournalProvider) ListJournals(_ context.Context, _ string) ([]string, error) {
	var names []string
	for name := range p.journals {
		names = append(names, name)
	}
	return names, nil
}

func (p *paginatingJournalProvider) ReadTimeRange(_ context.Context, _, journal string, start, stop time.Time, _ int) (*mms.JournalResult, error) {
	all := p.journals[journal]
	var filtered []mms.JournalEntry
	for _, e := range all {
		if (e.OccurrenceTime.Equal(start) || e.OccurrenceTime.After(start)) &&
			(e.OccurrenceTime.Equal(stop) || e.OccurrenceTime.Before(stop)) {
			filtered = append(filtered, e)
			if len(filtered) >= p.pageSize {
				return &mms.JournalResult{Entries: filtered, MoreFollows: true}, nil
			}
		}
	}
	return &mms.JournalResult{Entries: filtered, MoreFollows: false}, nil
}

func (p *paginatingJournalProvider) ReadStartAfter(_ context.Context, _, journal string, afterID []byte, afterTime time.Time, _ int) (*mms.JournalResult, error) {
	all := p.journals[journal]
	pastCursor := false
	var result []mms.JournalEntry
	for _, e := range all {
		if !pastCursor {
			if e.OccurrenceTime.Equal(afterTime) && bytes.Equal(e.EntryID, afterID) {
				pastCursor = true
				continue
			}
			if e.OccurrenceTime.After(afterTime) {
				pastCursor = true
			} else {
				continue
			}
		}
		result = append(result, e)
		if len(result) >= p.pageSize {
			return &mms.JournalResult{Entries: result, MoreFollows: true}, nil
		}
	}
	return &mms.JournalResult{Entries: result, MoreFollows: false}, nil
}

func setupPaginatingJournal(t *testing.T, pageSize int) *Client {
	t.Helper()
	ctx := context.Background()

	entries := make([]mms.JournalEntry, 10)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range entries {
		entries[i] = mms.JournalEntry{
			EntryID:        []byte{byte(i + 1)},
			OccurrenceTime: baseTime.Add(time.Duration(i) * time.Hour),
			Variables: []mms.JournalVariable{
				{Tag: "v1", Value: mms.NewInteger(int64(i * 100))},
			},
		}
	}

	jp := &paginatingJournalProvider{
		journals: map[string][]mms.JournalEntry{
			"LLN0$journal1": entries,
		},
		pageSize: pageSize,
	}

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		JournalProvider: jp,
	})

	if err := srv.RegisterDomain("testLD"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })
	return client
}

func TestReadJournalAll(t *testing.T) {
	client := setupPaginatingJournal(t, 3)
	ctx := context.Background()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	stop := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)

	entries, err := client.ReadJournalAll(ctx, "testLD", "LLN0$journal1", start, stop)
	if err != nil {
		t.Fatalf("ReadJournalAll: %v", err)
	}

	if len(entries) != 10 {
		t.Fatalf("got %d entries, want 10", len(entries))
	}

	// Verify order
	for i, e := range entries {
		v, err := e.Variables[0].Value.Int64()
		if err != nil {
			t.Fatalf("entry[%d] Int64: %v", i, err)
		}
		if v != int64(i*100) {
			t.Errorf("entry[%d] = %d, want %d", i, v, i*100)
		}
	}
}

func TestReadJournalAll_NoPages(t *testing.T) {
	client := setupPaginatingJournal(t, 100)
	ctx := context.Background()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	stop := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)

	entries, err := client.ReadJournalAll(ctx, "testLD", "LLN0$journal1", start, stop)
	if err != nil {
		t.Fatalf("ReadJournalAll: %v", err)
	}

	if len(entries) != 10 {
		t.Fatalf("got %d entries, want 10", len(entries))
	}
}

func TestReadJournalAll_EmptyResult(t *testing.T) {
	client := setupPaginatingJournal(t, 3)
	ctx := context.Background()

	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	stop := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)

	entries, err := client.ReadJournalAll(ctx, "testLD", "LLN0$journal1", start, stop)
	if err != nil {
		t.Fatalf("ReadJournalAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestReadJournalAfterAll(t *testing.T) {
	client := setupPaginatingJournal(t, 2)
	ctx := context.Background()

	afterTime := time.Date(2025, 1, 1, 2, 0, 0, 0, time.UTC)
	afterID := []byte{3}

	entries, err := client.ReadJournalAfterAll(ctx, "testLD", "LLN0$journal1", afterTime, afterID)
	if err != nil {
		t.Fatalf("ReadJournalAfterAll: %v", err)
	}

	if len(entries) != 7 {
		t.Fatalf("got %d entries, want 7 (entries after index 2)", len(entries))
	}

	v0, _ := entries[0].Variables[0].Value.Int64()
	if v0 != 300 {
		t.Errorf("first entry value = %d, want 300", v0)
	}
}

func TestReadJournalAll_ClosedClient(t *testing.T) {
	client := setupPaginatingJournal(t, 3)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ReadJournalAll(ctx, "testLD", "LLN0$journal1", time.Now(), time.Now())
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestReadJournalAfterAll_ClosedClient(t *testing.T) {
	client := setupPaginatingJournal(t, 3)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ReadJournalAfterAll(ctx, "testLD", "LLN0$journal1", time.Now(), nil)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}
