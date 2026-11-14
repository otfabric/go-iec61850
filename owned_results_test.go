// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// TestFindPaths_ResultIsCallersCopy: a caller that changes the Path of a
// FindPaths result leaves the cached parsed references unchanged; the
// next query gives the same answer.
func TestFindPaths_ResultIsCallersCopy(t *testing.T) {
	c, _ := oddItemsClient(t, ClientOptions{Cache: CacheLazy})
	ctx := context.Background()
	want := []string{"LDX/LLN0.Mod.stVal[ST]"}
	for round := range 3 {
		refs, err := c.FindPaths(ctx, FindQuery{Pattern: "LDX/LLN0*"})
		if err != nil {
			t.Fatal(err)
		}
		if got := refStrings(refs); !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: FindPaths = %v, want %v", round, got, want)
		}
		refs[0].Path[0] = "Beh" // the caller's copy
	}
}

// TestFindPaths_ConcurrentCallersOwnTheirResults: callers that change
// their results while others query share no storage (run with -race).
func TestFindPaths_ConcurrentCallersOwnTheirResults(t *testing.T) {
	c, _ := oddItemsClient(t, ClientOptions{Cache: CacheLazy})
	ctx := context.Background()
	if _, err := c.FindPaths(ctx, FindQuery{Pattern: "*"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				refs, err := c.FindPaths(ctx, FindQuery{Pattern: "LDX/LLN0*"})
				if err != nil || len(refs) != 1 || refs[0].String() != "LDX/LLN0.Mod.stVal[ST]" {
					t.Errorf("FindPaths = %v, %v", refs, err)
					return
				}
				refs[0].Path[0] = "Beh"
			}
		})
	}
	wg.Wait()
}

// TestListLogicalDevices_CoalescedCallersGetCopies: callers whose fetch
// of the logical devices was coalesced into one each get a slice of their
// own.
func TestListLogicalDevices_CoalescedCallersGetCopies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		c := &Client{cache: newModelCache(CacheExplicit)}
		c.fetchLDsFn = func(context.Context) ([]LogicalDevice, error) {
			close(started)
			<-release
			return []LogicalDevice{{Name: "LD0"}, {Name: "LD1"}}, nil
		}
		ctx := context.Background()
		results := make([][]LogicalDevice, 2)
		var wg sync.WaitGroup
		wg.Go(func() { results[0], _ = c.cachedLDs(ctx) })
		<-started
		wg.Go(func() { results[1], _ = c.cachedLDs(ctx) })
		synctest.Wait() // the second caller waits for the first fetch
		close(release)
		wg.Wait()
		if len(results[0]) != 2 || len(results[1]) != 2 {
			t.Fatalf("results %v", results)
		}
		results[0][0].Name = "changed"
		if results[1][0].Name != "LD0" {
			t.Errorf("second caller sees %q after the first changed its result, want LD0", results[1][0].Name)
		}
	})
}

// TestMemoryJournalProvider_ResultsAreCopies: the EntryID AddEntry
// returns and the entries of a read are the caller's: changing them
// leaves the journal, and the cursor of ReadStartAfter, unchanged.
func TestMemoryJournalProvider_ResultsAreCopies(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryJournalProvider()
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	vars := func(tag string) []mms.JournalVariable {
		return []mms.JournalVariable{{Tag: tag, Value: mms.NewInteger(1)}}
	}
	id1 := p.AddEntry("LD0", "J", at, vars("first"))
	p.AddEntry("LD0", "J", at, vars("second")) // same time: only the ID locates the cursor
	cursor := bytes.Clone(id1)
	id1[len(id1)-1] ^= 0xff // the caller's copy

	after, err := p.ReadStartAfter(ctx, "LD0", "J", cursor, at, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 1 || after.Entries[0].Variables[0].Tag != "second" {
		t.Fatalf("ReadStartAfter(first entry) = %+v, want the second entry", after.Entries)
	}

	for _, read := range []func() (*mms.JournalResult, error){
		func() (*mms.JournalResult, error) { return p.ReadTimeRange(ctx, "LD0", "J", at, at, 0) },
		func() (*mms.JournalResult, error) {
			return p.ReadStartAfter(ctx, "LD0", "J", nil, at.Add(-time.Second), 0)
		},
	} {
		res, err := read()
		if err != nil || len(res.Entries) != 2 {
			t.Fatalf("read = %+v, %v", res, err)
		}
		res.Entries[0].EntryID[len(res.Entries[0].EntryID)-1] ^= 0xff
		res.Entries[0].Variables[0] = mms.JournalVariable{Tag: "changed"}
		again, err := read()
		if err != nil || len(again.Entries) != 2 {
			t.Fatalf("read again = %+v, %v", again, err)
		}
		if !bytes.Equal(again.Entries[0].EntryID, cursor) || again.Entries[0].Variables[0].Tag != "first" {
			t.Errorf("after the caller changed a result: entry %x %+v, want %x first",
				again.Entries[0].EntryID, again.Entries[0].Variables, cursor)
		}
	}
}
