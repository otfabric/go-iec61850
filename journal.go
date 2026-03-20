package iec61850

import (
	"bytes"
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
		DomainID:    mms.DomainID(ld),
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

	result, err := c.mmsClient.ReadJournalTimeRange(ctx, ld, journal, start, stop)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read journal %s/%s: %w", ld, journal, err)
	}

	jr, jErr := convertJournalResult(result)
	if jErr != nil {
		return nil, fmt.Errorf("iec61850: read journal %s/%s: %w", ld, journal, jErr)
	}
	return jr, nil
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

	result, err := c.mmsClient.ReadJournalStartAfter(ctx, ld, journal, afterTime, afterID)
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
// are handled correctly via EntryID-based continuation.
//
// For very large journals, consider using the paginated
// [Client.ReadJournal] / [Client.ReadJournalAfter] pair to control
// memory usage.
func (c *Client) ReadJournalAll(ctx context.Context, ld, journal string, start, stop time.Time) ([]JournalEntry, error) {
	first, err := c.ReadJournal(ctx, ld, journal, start, stop)
	if err != nil {
		return nil, err
	}

	all := first.Entries
	if !first.MoreFollows || len(first.Entries) == 0 {
		return all, nil
	}

	for {
		last := all[len(all)-1]
		prevTime := last.OccurrenceTime
		prevID := last.EntryID
		page, err := c.ReadJournalAfter(ctx, ld, journal, prevTime, prevID)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Entries...)
		if !page.MoreFollows || len(page.Entries) == 0 {
			break
		}
		cur := all[len(all)-1]
		if cur.OccurrenceTime.Equal(prevTime) && bytes.Equal(cur.EntryID, prevID) {
			return nil, fmt.Errorf("iec61850: read journal all %s/%s: pagination not advancing (stuck at same cursor)", ld, journal)
		}
	}

	return all, nil
}

// ReadJournalAfterAll reads all journal entries starting after the
// given cursor, automatically following pagination until all entries
// are retrieved.
//
// This is the paginating equivalent of [Client.ReadJournalAfter].
func (c *Client) ReadJournalAfterAll(ctx context.Context, ld, journal string, afterTime time.Time, afterID []byte) ([]JournalEntry, error) {
	first, err := c.ReadJournalAfter(ctx, ld, journal, afterTime, afterID)
	if err != nil {
		return nil, err
	}

	all := first.Entries
	if !first.MoreFollows || len(first.Entries) == 0 {
		return all, nil
	}

	for {
		last := all[len(all)-1]
		prevTime := last.OccurrenceTime
		prevID := last.EntryID
		page, err := c.ReadJournalAfter(ctx, ld, journal, prevTime, prevID)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Entries...)
		if !page.MoreFollows || len(page.Entries) == 0 {
			break
		}
		cur := all[len(all)-1]
		if cur.OccurrenceTime.Equal(prevTime) && bytes.Equal(cur.EntryID, prevID) {
			return nil, fmt.Errorf("iec61850: read journal after all %s/%s: pagination not advancing (stuck at same cursor)", ld, journal)
		}
	}

	return all, nil
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
