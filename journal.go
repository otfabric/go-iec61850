// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-mms"
)

// JournalEntry represents a single entry in an IEC 61850 journal (log).
type JournalEntry struct {
	// EntryID is the opaque entry identifier assigned by the server.
	// It is used as a cursor for pagination via [Client.ReadJournalAfter].
	EntryID []byte

	// OccurrenceTime is the timestamp when the event occurred.
	OccurrenceTime time.Time

	// Variables contains the data values recorded in this entry.
	Variables []JournalVariable
}

// JournalVariable represents a single variable recorded in a journal
// entry.
type JournalVariable struct {
	// Tag identifies the variable (typically an MMS variable name).
	Tag string

	// Value is the recorded value. Wraps the underlying [mms.Value].
	Value *Value
}

// JournalReadResult holds the result of a journal read operation,
// including pagination state.
type JournalReadResult struct {
	// Entries contains the journal entries in chronological order.
	Entries []JournalEntry

	// MoreFollows indicates that additional entries are available
	// beyond those returned. Use [Client.ReadJournalAfter] with the
	// last entry's OccurrenceTime and EntryID to retrieve the next page.
	MoreFollows bool
}

// ListJournals returns the names of all journals (logs) defined in the
// specified logical device (MMS domain).
func (c *Client) ListJournals(ctx context.Context, ld string) ([]string, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: list journals: %w: empty logical device name", ErrInvalidArgument)
	}

	names, err := c.mmsClient.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassJournal,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    c.ldDomain(ld),
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: list journals for %q: %w", ld, err)
	}

	c.logger.Debug("iec61850: list journals", "ld", ld, "count", len(names))
	return names, nil
}

// ReadJournal reads journal entries within the given time range
// [start, stop] from the specified journal in the logical device.
//
// RangeStartTime and RangeStopTime are optional in QueryLogByTime
// (IEC 61850-7-2 §17.3.5.2): a zero start reads from the first entry and
// a zero stop up to the last. The MMS ReadJournal request always carries
// both, so they are sent as the first and last instant a BinaryTime can
// hold (1984-01-01 and 2163-06-05 23:59:59.999 UTC).
//
// The returned entries are in chronological order. When
// [JournalReadResult.MoreFollows] is true, use [Client.ReadJournalAfter]
// with the last entry's OccurrenceTime and EntryID to page through
// remaining entries.
func (c *Client) ReadJournal(ctx context.Context, ld, journal string, start, stop time.Time) (*JournalReadResult, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: read journal: %w: empty logical device name", ErrInvalidArgument)
	}
	if journal == "" {
		return nil, fmt.Errorf("iec61850: read journal: %w: empty journal name", ErrInvalidArgument)
	}

	start, stop = openJournalRange(start, stop)
	result, err := c.mmsClient.ReadJournalTimeRange(ctx, string(c.ldDomain(ld)), journal, start, stop)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read journal %s/%s: %w", ld, journal, err)
	}

	jr, jErr := convertJournalResult(result)
	if jErr != nil {
		return nil, fmt.Errorf("iec61850: read journal %s/%s: %w", ld, journal, jErr)
	}
	return jr, nil
}

// journalRangeFirst and journalRangeLast are the first and last instant
// of the dated BinaryTime form (ISO 9506-2 TimeOfDay: days since
// 1984-01-01 in 16 bits; go-mms encodes up to 2163-06-05).
var (
	journalRangeFirst = time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC)
	journalRangeLast  = time.Date(2163, 6, 5, 23, 59, 59, 999e6, time.UTC)
)

// openJournalRange replaces a zero start or stop by the first or last
// BinaryTime instant (LOG-06).
func openJournalRange(start, stop time.Time) (time.Time, time.Time) {
	if start.IsZero() {
		start = journalRangeFirst
	}
	if stop.IsZero() {
		stop = journalRangeLast
	}
	return start, stop
}

// ReadJournalAfter reads journal entries starting after the given
// cursor position. Use the last entry's OccurrenceTime and EntryID
// from a previous [ReadJournal] or [ReadJournalAfter] call to page
// through journal data.
func (c *Client) ReadJournalAfter(ctx context.Context, ld, journal string, afterTime time.Time, afterID []byte) (*JournalReadResult, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: read journal after: %w: empty logical device name", ErrInvalidArgument)
	}
	if journal == "" {
		return nil, fmt.Errorf("iec61850: read journal after: %w: empty journal name", ErrInvalidArgument)
	}

	result, err := c.mmsClient.ReadJournalStartAfter(ctx, string(c.ldDomain(ld)), journal, afterTime, afterID)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read journal after %s/%s: %w", ld, journal, err)
	}

	jr, jErr := convertJournalResult(result)
	if jErr != nil {
		return nil, fmt.Errorf("iec61850: read journal after %s/%s: %w", ld, journal, jErr)
	}
	return jr, nil
}

// ReadJournalAll reads all journal entries within the given time range,
// automatically following pagination (MoreFollows) until all entries
// are retrieved.
//
// Entries are returned in chronological order. The stop condition is
// MoreFollows=false from the server. Same-timestamp cursor semantics
// are handled correctly via EntryID-based continuation. A page whose
// last entry is a cursor already used (the pagination does not
// advance) is refused.
//
// The entries collected are bounded by [ClientOptions.JournalLimits]
// (1,000,000 entries and an estimated 256 MiB by default): a journal
// beyond a limit is given up with an error that wraps
// [ErrLimitExceeded]. For very large journals, use the paginated
// [Client.ReadJournal] / [Client.ReadJournalAfter] pair.
func (c *Client) ReadJournalAll(ctx context.Context, ld, journal string, start, stop time.Time) ([]JournalEntry, error) {
	first, err := c.ReadJournal(ctx, ld, journal, start, stop)
	if err != nil {
		return nil, err
	}
	return c.readJournalPages(ctx, "read journal all", ld, journal, first)
}

// ReadJournalAfterAll reads all journal entries starting after the
// given cursor, automatically following pagination until all entries
// are retrieved.
//
// This is the paginating equivalent of [Client.ReadJournalAfter], with
// the limits and the cursor check of [Client.ReadJournalAll].
func (c *Client) ReadJournalAfterAll(ctx context.Context, ld, journal string, afterTime time.Time, afterID []byte) ([]JournalEntry, error) {
	first, err := c.ReadJournalAfter(ctx, ld, journal, afterTime, afterID)
	if err != nil {
		return nil, err
	}
	return c.readJournalPages(ctx, "read journal after all", ld, journal, first)
}

// journalCursor identifies the position after an entry: its time and
// its EntryID.
type journalCursor struct {
	sec  int64 // the time in Unix seconds and nanoseconds; the location does not matter
	nsec int
	id   string
}

// readJournalPages collects first and the pages that follow it, within
// the journal limits, refusing a cursor already used.
func (c *Client) readJournalPages(ctx context.Context, op, ld, journal string, first *JournalReadResult) ([]JournalEntry, error) {
	limits := c.opts.JournalLimits.resolved()
	var (
		all  []JournalEntry
		size int64
		seen map[journalCursor]struct{}
	)
	page := first
	for {
		if len(page.Entries) > limits.MaxEntries-len(all) {
			return nil, fmt.Errorf("iec61850: %s %s/%s: %w: more than %d entries", op, ld, journal, ErrLimitExceeded, limits.MaxEntries)
		}
		for i := range page.Entries {
			size += journalEntrySize(&page.Entries[i])
			if size > limits.MaxBytes {
				return nil, fmt.Errorf("iec61850: %s %s/%s: %w: entries beyond %d bytes", op, ld, journal, ErrLimitExceeded, limits.MaxBytes)
			}
		}
		all = append(all, page.Entries...)
		if !page.MoreFollows || len(page.Entries) == 0 {
			return all, nil
		}

		last := all[len(all)-1]
		cur := journalCursor{sec: last.OccurrenceTime.Unix(), nsec: last.OccurrenceTime.Nanosecond(), id: string(last.EntryID)}
		if seen == nil {
			seen = make(map[journalCursor]struct{})
		}
		if _, dup := seen[cur]; dup {
			return nil, fmt.Errorf("iec61850: %s %s/%s: pagination not advancing (cursor %x at %s already used)",
				op, ld, journal, last.EntryID, last.OccurrenceTime.Format(time.RFC3339Nano))
		}
		seen[cur] = struct{}{}

		var err error
		page, err = c.ReadJournalAfter(ctx, ld, journal, last.OccurrenceTime, last.EntryID)
		if err != nil {
			return nil, err
		}
	}
}

// journalEntrySize estimates the memory of a decoded journal entry: its
// fixed part, its EntryID and its variables' tags and values.
func journalEntrySize(e *JournalEntry) int64 {
	n := int64(64 + len(e.EntryID))
	for _, v := range e.Variables {
		n += int64(32 + len(v.Tag))
		if v.Value != nil {
			n += int64(valueFootprint(v.Value.MMS(), 0))
		}
	}
	return n
}

// convertJournalResult maps an MMS journal result to the IEC 61850
// model. Returns an error if r is nil, which indicates a go-mms
// invariant violation on the success path.
func convertJournalResult(r *mms.JournalResult) (*JournalReadResult, error) {
	if r == nil {
		return nil, fmt.Errorf("nil journal result from MMS layer (possible go-mms bug)")
	}
	entries := make([]JournalEntry, len(r.Entries))
	for i, e := range r.Entries {
		vars := make([]JournalVariable, len(e.Variables))
		for j, v := range e.Variables {
			vars[j] = JournalVariable{
				Tag:   v.Tag,
				Value: NewValue(v.Value),
			}
		}
		entries[i] = JournalEntry{
			EntryID:        append([]byte(nil), e.EntryID...),
			OccurrenceTime: e.OccurrenceTime,
			Variables:      vars,
		}
	}

	return &JournalReadResult{
		Entries:     entries,
		MoreFollows: r.MoreFollows,
	}, nil
}
