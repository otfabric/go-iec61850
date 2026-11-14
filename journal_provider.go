// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
)

// MemoryJournalProvider is a thread-safe, in-memory implementation of
// [mms.JournalProvider]. It stores entries per (domain, journal) pair
// with configurable maximum capacity. When the capacity is exceeded,
// the oldest entries are discarded (ring-buffer semantics).
//
// Entry IDs are auto-assigned as monotonically increasing 8-byte
// big-endian integers, guaranteeing strict ordering even when
// multiple entries share the same OccurrenceTime. This makes cursor
// pagination deterministic and skip-free.
//
// This provider is intended for testing, development, and
// lightweight server deployments. For production use with large
// journal volumes, a persistent provider backed by a database
// or file storage should be used instead.
type MemoryJournalProvider struct {
	mu      sync.RWMutex
	logs    map[journalKey]*journalLog
	maxSize int
}

type journalKey struct {
	domain, journal string
}

type journalLog struct {
	entries []mms.JournalEntry
	nextID  uint64
}

// MemoryJournalOption configures a [MemoryJournalProvider].
type MemoryJournalOption func(*MemoryJournalProvider)

// WithMaxEntries sets the maximum number of entries per journal.
// When exceeded, the oldest entries are dropped. Default is 10000.
func WithMaxEntries(n int) MemoryJournalOption {
	return func(p *MemoryJournalProvider) {
		if n > 0 {
			p.maxSize = n
		}
	}
}

// NewMemoryJournalProvider creates a new in-memory journal provider.
func NewMemoryJournalProvider(opts ...MemoryJournalOption) *MemoryJournalProvider {
	p := &MemoryJournalProvider{
		logs:    make(map[journalKey]*journalLog),
		maxSize: 10000,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// RegisterJournal creates an empty journal. This must be called
// before [AddEntry] to establish the journal's existence. Calling
// RegisterJournal for an already-registered journal is a no-op.
func (p *MemoryJournalProvider) RegisterJournal(domain, journal string) {
	k := journalKey{domain, journal}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.logs[k]; !ok {
		p.logs[k] = &journalLog{nextID: 1}
	}
}

// AddEntry appends a journal entry with the given timestamp and
// variable data. The EntryID is assigned automatically. Returns the
// assigned EntryID, a copy the caller owns.
//
// The provided variables are defensively copied so that later
// mutation of the caller's slice or the values within it does not
// corrupt the journal history.
//
// If the journal does not exist, it is created implicitly.
func (p *MemoryJournalProvider) AddEntry(domain, journal string, occTime time.Time, vars []mms.JournalVariable) []byte {
	k := journalKey{domain, journal}
	p.mu.Lock()
	defer p.mu.Unlock()

	jl, ok := p.logs[k]
	if !ok {
		jl = &journalLog{nextID: 1}
		p.logs[k] = jl
	}

	entryID := encodeEntryIDUint64(jl.nextID)
	jl.nextID++

	copiedVars := make([]mms.JournalVariable, len(vars))
	for i, v := range vars {
		copiedVars[i].Tag = v.Tag
		if v.Value != nil {
			cp := *v.Value
			copiedVars[i].Value = &cp
		}
	}

	// The occurrence time travels as an MMS TimeOfDay (millisecond
	// resolution): store it at that resolution so that the cursor a
	// client sends back (ReadStartAfter) matches the entry exactly.
	jl.entries = append(jl.entries, mms.JournalEntry{
		EntryID:        entryID,
		OccurrenceTime: occTime.Truncate(time.Millisecond),
		Variables:      copiedVars,
	})

	if len(jl.entries) > p.maxSize {
		excess := len(jl.entries) - p.maxSize
		jl.entries = jl.entries[excess:]
	}

	return bytes.Clone(entryID)
}

// copyJournalEntry copies a stored entry for a read result, so that a
// caller that changes the result leaves the journal unchanged: the
// EntryID and the Variables slice are copied; the values are shared, as an
// [mms.Value] does not change after it is made.
func copyJournalEntry(e mms.JournalEntry) mms.JournalEntry {
	e.EntryID = bytes.Clone(e.EntryID)
	e.Variables = slices.Clone(e.Variables)
	return e
}

// EntryCount returns the number of entries in the specified journal.
func (p *MemoryJournalProvider) EntryCount(domain, journal string) int {
	k := journalKey{domain, journal}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if jl, ok := p.logs[k]; ok {
		return len(jl.entries)
	}
	return 0
}

// ListJournals implements [mms.JournalProvider]. Results are sorted
// for deterministic output.
func (p *MemoryJournalProvider) ListJournals(_ context.Context, domain string) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var names []string
	for k := range p.logs {
		if k.domain == domain {
			names = append(names, k.journal)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ReadTimeRange implements [mms.JournalProvider]. The entries of the
// result are copies the caller owns.
func (p *MemoryJournalProvider) ReadTimeRange(_ context.Context, domain, journal string, start, stop time.Time, maxEntries int) (*mms.JournalResult, error) {
	k := journalKey{domain, journal}
	p.mu.RLock()
	defer p.mu.RUnlock()

	jl, ok := p.logs[k]
	if !ok {
		return nil, errUnknownJournal(domain, journal)
	}

	var filtered []mms.JournalEntry
	moreFollows := false
	for _, e := range jl.entries {
		if !e.OccurrenceTime.Before(start) && !e.OccurrenceTime.After(stop) {
			if maxEntries > 0 && len(filtered) >= maxEntries {
				moreFollows = true
				break
			}
			filtered = append(filtered, copyJournalEntry(e))
		}
	}
	return &mms.JournalResult{Entries: filtered, MoreFollows: moreFollows}, nil
}

// ReadStartAfter implements [mms.JournalProvider]. The entries of the
// result are copies the caller owns.
func (p *MemoryJournalProvider) ReadStartAfter(_ context.Context, domain, journal string, afterID []byte, afterTime time.Time, maxEntries int) (*mms.JournalResult, error) {
	k := journalKey{domain, journal}
	p.mu.RLock()
	defer p.mu.RUnlock()

	jl, ok := p.logs[k]
	if !ok {
		return nil, errUnknownJournal(domain, journal)
	}

	// The entry ID locates the cursor; the time only when the entry is
	// no longer in the log (evicted), then entries after afterTime follow.
	afterTime = afterTime.Truncate(time.Millisecond)
	start := -1
	for i, e := range jl.entries {
		if bytes.Equal(e.EntryID, afterID) && e.OccurrenceTime.Equal(afterTime) {
			start = i + 1
			break
		}
	}

	pastCursor := false
	var result []mms.JournalEntry
	moreFollows := false
	for i, e := range jl.entries {
		if start >= 0 {
			if i < start {
				continue
			}
			pastCursor = true
		}
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
		if maxEntries > 0 && len(result) >= maxEntries {
			moreFollows = true
			break
		}
		result = append(result, copyJournalEntry(e))
	}
	return &mms.JournalResult{Entries: result, MoreFollows: moreFollows}, nil
}

func encodeEntryIDUint64(id uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, id)
	return buf
}

// errUnknownJournal is the error of a read of a journal that does not
// exist: ReadJournal answers Response– (IEC 61850-7-2 §17.3.5.2.4,
// IEC 61850-8-1 §17.3.4.5), which go-mms maps from fs.ErrNotExist to
// object-non-existent.
func errUnknownJournal(domain, journal string) error {
	return fmt.Errorf("iec61850: journal %s/%s: %w", domain, journal, fs.ErrNotExist)
}

// hasJournal reports whether the journal is registered.
func (p *MemoryJournalProvider) hasJournal(domain, journal string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.logs[journalKey{domain, journal}]
	return ok
}

// bounds returns the entry IDs and times of the oldest and newest entry
// of a journal; nil IDs and zero times when it is empty or unknown.
func (p *MemoryJournalProvider) bounds(domain, journal string) (oldID, newID []byte, oldT, newT time.Time) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	jl, ok := p.logs[journalKey{domain, journal}]
	if !ok || len(jl.entries) == 0 {
		return nil, nil, time.Time{}, time.Time{}
	}
	first, last := jl.entries[0], jl.entries[len(jl.entries)-1]
	return bytes.Clone(first.EntryID), bytes.Clone(last.EntryID), first.OccurrenceTime, last.OccurrenceTime
}
