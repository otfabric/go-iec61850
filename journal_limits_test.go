// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// scriptedJournalProvider answers every journal read with page(n), n
// the number of the read, and records the domains asked for.
type scriptedJournalProvider struct {
	page func(n int) *mms.JournalResult

	mu      sync.Mutex
	reads   int
	domains []string
}

func (p *scriptedJournalProvider) next(domain string) *mms.JournalResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.domains = append(p.domains, domain)
	n := p.reads
	p.reads++
	return p.page(n)
}

func (p *scriptedJournalProvider) ListJournals(context.Context, string) ([]string, error) {
	return []string{"LLN0$j"}, nil
}

func (p *scriptedJournalProvider) ReadTimeRange(_ context.Context, domain, _ string, _, _ time.Time, _ int) (*mms.JournalResult, error) {
	return p.next(domain), nil
}

func (p *scriptedJournalProvider) ReadStartAfter(_ context.Context, domain, _ string, _ []byte, _ time.Time, _ int) (*mms.JournalResult, error) {
	return p.next(domain), nil
}

func scriptedJournalClient(t *testing.T, domain string, opts ClientOptions, page func(n int) *mms.JournalResult) (*Client, *scriptedJournalProvider) {
	t.Helper()
	ctx := context.Background()
	jp := &scriptedJournalProvider{page: page}
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
		},
		JournalProvider: jp,
	})
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatal(err)
	}
	clientT, serverT := loopbackPair(t)
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(mmsClient, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })
	return c, jp
}

var journalBase = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// journalPage returns one page of size entries numbered from first, one
// second apart, each with a value of payload octets.
func journalPage(first, size, payload int, more bool) *mms.JournalResult {
	r := &mms.JournalResult{MoreFollows: more}
	for i := first; i < first+size; i++ {
		r.Entries = append(r.Entries, mms.JournalEntry{
			EntryID:        []byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)},
			OccurrenceTime: journalBase.Add(time.Duration(i) * time.Second),
			Variables:      []mms.JournalVariable{{Tag: "v", Value: mms.NewOctetString(make([]byte, payload))}},
		})
	}
	return r
}

// endlessJournal answers every read with a new page of 3 entries and
// MoreFollows.
func endlessJournal(n int) *mms.JournalResult { return journalPage(3*n, 3, 10, true) }

// tenEntries answers with 10 entries in pages of 3.
func tenEntries(n int) *mms.JournalResult {
	if 3*n+3 >= 10 {
		return journalPage(3*n, 10-3*n, 10, false)
	}
	return journalPage(3*n, 3, 10, true)
}

type journalAllFunc func(c *Client) ([]JournalEntry, error)

var journalAllHelpers = map[string]journalAllFunc{
	"ReadJournalAll": func(c *Client) ([]JournalEntry, error) {
		return c.ReadJournalAll(context.Background(), "LD", "LLN0$j", time.Time{}, time.Time{})
	},
	"ReadJournalAfterAll": func(c *Client) ([]JournalEntry, error) {
		return c.ReadJournalAfterAll(context.Background(), "LD", "LLN0$j", journalBase, []byte{0})
	},
}

// The entries collected over the pages are bounded by MaxEntries: a
// journal of exactly MaxEntries entries is read, one more is given up
// with ErrLimitExceeded, and a negative MaxEntries reads a journal of
// any size.
func TestReadJournalAll_MaxEntries(t *testing.T) {
	for name, read := range journalAllHelpers {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				max     int
				wantErr bool
			}{{10, false}, {9, true}, {2, true}, {-1, false}, {0, false}} {
				c, _ := scriptedJournalClient(t, "LD", ClientOptions{JournalLimits: JournalLimits{MaxEntries: tc.max}}, tenEntries)
				entries, err := read(c)
				if tc.wantErr {
					if !errors.Is(err, ErrLimitExceeded) || entries != nil {
						t.Errorf("MaxEntries %d: %d entries, err %v; want ErrLimitExceeded", tc.max, len(entries), err)
					}
				} else if err != nil || len(entries) != 10 {
					t.Errorf("MaxEntries %d: %d entries, err %v; want 10", tc.max, len(entries), err)
				}
			}

			// A peer that keeps announcing more entries.
			c, jp := scriptedJournalClient(t, "LD", ClientOptions{JournalLimits: JournalLimits{MaxEntries: 100}}, endlessJournal)
			if _, err := read(c); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("endless journal: err %v, want ErrLimitExceeded", err)
			}
			if jp.reads > 34 {
				t.Errorf("endless journal: %d reads for 100 entries in pages of 3", jp.reads)
			}
		})
	}
}

// The estimated memory of the entries collected is bounded by MaxBytes,
// with the values counted at their decoded size.
func TestReadJournalAll_MaxBytes(t *testing.T) {
	big := func(n int) *mms.JournalResult { return journalPage(n, 1, 40000, true) }
	for name, read := range journalAllHelpers {
		t.Run(name, func(t *testing.T) {
			c, jp := scriptedJournalClient(t, "LD", ClientOptions{JournalLimits: JournalLimits{MaxBytes: 1 << 20}}, big)
			_, err := read(c)
			if !errors.Is(err, ErrLimitExceeded) || !strings.Contains(err.Error(), "bytes") {
				t.Fatalf("err %v, want ErrLimitExceeded on bytes", err)
			}
			if jp.reads > 27 {
				t.Errorf("%d reads of 40000 octets for a 1 MiB limit", jp.reads)
			}

			c, _ = scriptedJournalClient(t, "LD", ClientOptions{JournalLimits: JournalLimits{MaxBytes: -1}}, tenEntries)
			if entries, err := read(c); err != nil || len(entries) != 10 {
				t.Errorf("no byte limit: %d entries, err %v", len(entries), err)
			}
		})
	}
}

// A cursor already used ends the pagination with an error: the same
// cursor again, and a cycle of cursors (A, B, A).
func TestReadJournalAll_CursorNotAdvancing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cycle []int
	}{
		{"same cursor", []int{0, 0}},
		{"A B A", []int{0, 1, 0}},
		{"A B C B", []int{0, 1, 2, 1}},
	} {
		for name, read := range journalAllHelpers {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				cyc := func(n int) *mms.JournalResult {
					return journalPage(tc.cycle[n%len(tc.cycle)], 1, 1, true)
				}
				c, jp := scriptedJournalClient(t, "LD", ClientOptions{}, cyc)
				entries, err := read(c)
				if err == nil || !strings.Contains(err.Error(), "not advancing") || entries != nil {
					t.Fatalf("%d entries, err %v; want pagination not advancing", len(entries), err)
				}
				if jp.reads != len(tc.cycle) {
					t.Errorf("%d reads, want %d", jp.reads, len(tc.cycle))
				}
			})
		}
	}

	// Entries of the same time with different EntryIDs advance.
	sameTime := func(n int) *mms.JournalResult {
		r := journalPage(n, 1, 1, n < 4)
		r.Entries[0].OccurrenceTime = journalBase
		return r
	}
	c, _ := scriptedJournalClient(t, "LD", ClientOptions{}, sameTime)
	if entries, err := journalAllHelpers["ReadJournalAll"](c); err != nil || len(entries) != 5 {
		t.Errorf("same time, new EntryIDs: %d entries, err %v; want 5", len(entries), err)
	}
}

// With an IED name, the journal reads address the MMS domain of the
// IED name and the LD instance, as the other services do.
func TestReadJournal_IEDNamePrefix(t *testing.T) {
	c, jp := scriptedJournalClient(t, "IED1LD", ClientOptions{IEDName: "IED1"}, tenEntries)
	ctx := context.Background()
	if _, err := c.ReadJournal(ctx, "LD", "LLN0$j", time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadJournalAfter(ctx, "LD", "LLN0$j", journalBase, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if len(jp.domains) != 2 || jp.domains[0] != "IED1LD" || jp.domains[1] != "IED1LD" {
		t.Errorf("domains %v, want [IED1LD IED1LD]", jp.domains)
	}
}

func TestJournalLimits_Resolved(t *testing.T) {
	if got := (JournalLimits{}).resolved(); got.MaxEntries != DefaultMaxJournalEntries || got.MaxBytes != DefaultMaxJournalBytes {
		t.Errorf("zero: %+v, want the defaults", got)
	}
	if got := (JournalLimits{MaxEntries: -1, MaxBytes: -1}).resolved(); got.MaxEntries != math.MaxInt || got.MaxBytes != math.MaxInt64 {
		t.Errorf("negative: %+v, want no limit", got)
	}
	if got := (JournalLimits{MaxEntries: 7, MaxBytes: 9}).resolved(); got.MaxEntries != 7 || got.MaxBytes != 9 {
		t.Errorf("set: %+v", got)
	}
}
