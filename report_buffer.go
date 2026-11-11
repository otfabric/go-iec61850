// SPDX-License-Identifier: MIT

package iec61850

import (
	"slices"
	"time"

	"github.com/otfabric/go-mms"
)

// ReportEntry is one entry of a report control block's queue: the
// content of one report before it is formatted (IEC 61850-7-2
// §17.2.3.2.2.9).
//
// The report engine creates entries and never changes one after it has
// been appended to a [ReportBuffer]; a buffer may keep the slices as
// they are.
type ReportEntry struct {
	// ID is the EntryID as a number; the wire EntryID is its 8-octet
	// big-endian encoding (IEC 61850-8-1 §17.2.1). IDs start at 1 and
	// increase by one for each entry the RCB creates; 0 is reserved for
	// an empty buffer (IEC 61850-7-2 §17.2.2.15).
	ID uint64

	// TimeOfEntry is the time the report handler received the first
	// internal notification of the entry (IEC 61850-7-2 §17.2.2.16).
	TimeOfEntry time.Time

	// Values, Reasons and Inclusion have one element per data set
	// member. Inclusion marks the members the report carries, Reasons
	// their reason for inclusion and Values their values; the elements
	// of members not included are unused.
	Values    []*mms.Value
	Reasons   []ReasonCode
	Inclusion []bool

	// GI marks a general-interrogation entry. It is sent once, in the
	// enable session that requested it, and is not replayed: a BRCB
	// buffers only dchg, qchg, dupd and integrity entries
	// (IEC 61850-7-2 §17.2.3.2.3.6).
	GI bool
}

// EncodedSize returns an upper bound of the encoded size of the entry's
// member values in octets: the size [MemoryReportBuffer] counts against
// its byte limit. Data references and the report header are not
// included.
func (e ReportEntry) EncodedSize() int {
	n := len(e.Inclusion)/8 + 1 // inclusion bit string
	for i, in := range e.Inclusion {
		if !in {
			continue
		}
		if i < len(e.Values) {
			n += dataLen(e.Values[i])
		}
		n += 3 // reason code bit string
	}
	return n
}

// ReportBuffer stores the entries of one report control block in
// creation order: the queue of a URCB and the buffer of a BRCB
// (IEC 61850-7-2 §17.2.2.15, §17.2.3.2.3.6).
//
// The report engine owns the buffer: it calls the methods with the RCB's
// lock held, one buffer per RCB, so an implementation need not be safe
// for concurrent use. It assigns the entry IDs, keeps track of what was
// sent and sets BufOvfl from the entries [ReportBuffer.Append]
// discards.
//
// The default is [NewMemoryReportBuffer] with 1000 entries and no byte
// limit; [ServerOptions.NewReportBuffer] installs another one.
//
// An implementation that cannot keep an entry (a full disk, for a
// persistent one) returns it from Append as discarded, so the client
// learns of the loss through BufOvfl. A buffer that already holds entries
// when [Server.EnableReports] runs continues them: new entry IDs follow
// the newest one, and GI entries in it are not sent again.
type ReportBuffer interface {
	// Append stores e as the newest entry. e.ID is greater than the ID
	// of every entry in the buffer. To make room, the buffer discards
	// its oldest entries and returns them, oldest first; it never
	// discards e itself unless it cannot store it at all.
	Append(e ReportEntry) (discarded []ReportEntry)

	// After returns the oldest entry whose ID is greater than id.
	After(id uint64) (ReportEntry, bool)

	// Get returns the entry with the given ID.
	Get(id uint64) (ReportEntry, bool)

	// Oldest and Newest return the oldest and the newest entry.
	Oldest() (ReportEntry, bool)
	Newest() (ReportEntry, bool)

	// Remove deletes the entry with the given ID, if present.
	Remove(id uint64)

	// Purge deletes every entry (IEC 61850-7-2 §17.2.2.14).
	Purge()

	// Len returns the number of entries.
	Len() int
}

// defaultRCBBufMax is the default entry capacity of a report buffer.
const defaultRCBBufMax = 1000

// MemoryReportBuffer is the default [ReportBuffer]: entries in memory,
// bounded by a number of entries and, optionally, by their
// [ReportEntry.EncodedSize] in octets. It does not survive a restart.
//
// A MemoryReportBuffer is not safe for concurrent use; the report engine
// serialises its calls.
type MemoryReportBuffer struct {
	entries    []ReportEntry
	bytes      int
	maxEntries int
	maxBytes   int
}

// NewMemoryReportBuffer returns an in-memory report buffer that holds at
// most maxEntries entries (at least 1; 0 or less means 1000) and, when
// maxBytes is positive, entries of at most maxBytes octets in all. When
// an entry is appended to a full buffer the oldest entries are discarded
// (IEC 61850-7-2 §17.2.3.2.2.8). The newest entry is kept even when it
// alone exceeds maxBytes.
func NewMemoryReportBuffer(maxEntries, maxBytes int) *MemoryReportBuffer {
	b := &MemoryReportBuffer{}
	b.setLimits(maxEntries, maxBytes)
	return b
}

func (b *MemoryReportBuffer) setLimits(maxEntries, maxBytes int) {
	if maxEntries <= 0 {
		maxEntries = defaultRCBBufMax
	}
	b.maxEntries = maxEntries
	b.maxBytes = max(maxBytes, 0)
}

// SetLimits changes the limits as [NewMemoryReportBuffer] defines them.
// Entries above a lower limit are discarded with the next Append.
func (b *MemoryReportBuffer) SetLimits(maxEntries, maxBytes int) {
	b.setLimits(maxEntries, maxBytes)
}

// Limits returns the entry and byte limits (0 bytes: no byte limit).
func (b *MemoryReportBuffer) Limits() (maxEntries, maxBytes int) {
	return b.maxEntries, b.maxBytes
}

// Bytes returns the [ReportEntry.EncodedSize] of all entries.
func (b *MemoryReportBuffer) Bytes() int { return b.bytes }

// Append implements [ReportBuffer].
func (b *MemoryReportBuffer) Append(e ReportEntry) []ReportEntry {
	size := e.EncodedSize()
	var discarded []ReportEntry
	for len(b.entries) > 0 &&
		(len(b.entries) >= b.maxEntries || (b.maxBytes > 0 && b.bytes+size > b.maxBytes)) {
		d := b.entries[0]
		b.entries[0] = ReportEntry{}
		b.entries = b.entries[1:]
		b.bytes -= d.EncodedSize()
		discarded = append(discarded, d)
	}
	b.entries = append(b.entries, e)
	b.bytes += size
	return discarded
}

// index returns the position of the oldest entry with ID ≥ id.
func (b *MemoryReportBuffer) index(id uint64) int {
	i, _ := slices.BinarySearchFunc(b.entries, id, func(e ReportEntry, id uint64) int {
		switch {
		case e.ID < id:
			return -1
		case e.ID > id:
			return 1
		}
		return 0
	})
	return i
}

// After implements [ReportBuffer].
func (b *MemoryReportBuffer) After(id uint64) (ReportEntry, bool) {
	if id == ^uint64(0) {
		return ReportEntry{}, false
	}
	i := b.index(id + 1)
	if i >= len(b.entries) {
		return ReportEntry{}, false
	}
	return b.entries[i], true
}

// Get implements [ReportBuffer].
func (b *MemoryReportBuffer) Get(id uint64) (ReportEntry, bool) {
	i := b.index(id)
	if i >= len(b.entries) || b.entries[i].ID != id {
		return ReportEntry{}, false
	}
	return b.entries[i], true
}

// Oldest implements [ReportBuffer].
func (b *MemoryReportBuffer) Oldest() (ReportEntry, bool) {
	if len(b.entries) == 0 {
		return ReportEntry{}, false
	}
	return b.entries[0], true
}

// Newest implements [ReportBuffer].
func (b *MemoryReportBuffer) Newest() (ReportEntry, bool) {
	if len(b.entries) == 0 {
		return ReportEntry{}, false
	}
	return b.entries[len(b.entries)-1], true
}

// Remove implements [ReportBuffer].
func (b *MemoryReportBuffer) Remove(id uint64) {
	i := b.index(id)
	if i >= len(b.entries) || b.entries[i].ID != id {
		return
	}
	b.bytes -= b.entries[i].EncodedSize()
	if i == 0 {
		b.entries[0] = ReportEntry{}
		b.entries = b.entries[1:]
		return
	}
	b.entries = slices.Delete(b.entries, i, i+1)
}

// Purge implements [ReportBuffer].
func (b *MemoryReportBuffer) Purge() {
	b.entries = nil
	b.bytes = 0
}

// Len implements [ReportBuffer].
func (b *MemoryReportBuffer) Len() int { return len(b.entries) }

// Entries returns a copy of the entries, oldest first.
func (b *MemoryReportBuffer) Entries() []ReportEntry {
	return slices.Clone(b.entries)
}
