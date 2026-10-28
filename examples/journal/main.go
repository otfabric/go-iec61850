// SPDX-License-Identifier: MIT

// Command journal reads IEC 61850 logs (MMS journals) from a server.
//
// It lists the journals of a logical device, reads the entries of the
// last -since interval and, with -follow, keeps polling for entries
// after the last one it has seen (ReadJournalAfterAll with the entry
// ID as cursor), as a historian would.
//
// Usage:
//
//	journal [-addr 127.0.0.1:102] [-ld LD1] [-since 1h] [-follow] [-poll 2s]
//
// Run it against examples/ied-server, whose demo IED logs the changes
// of its measurement in LLN0$EventLog.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ld := flag.String("ld", "LD1", "logical device (MMS domain)")
	since := flag.Duration("since", time.Hour, "read entries of this period")
	follow := flag.Bool("follow", false, "keep polling for new entries")
	poll := flag.Duration("poll", 2*time.Second, "poll interval with -follow")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *ld, *since, *follow, *poll, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, ld string, since time.Duration, follow bool, poll time.Duration, out io.Writer) error {
	c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.Background()) }()

	journals, err := c.ListJournals(ctx, ld)
	if err != nil {
		return err
	}
	if len(journals) == 0 {
		return fmt.Errorf("%s has no journals", ld)
	}

	// The cursor of each journal: the time and ID of its last entry.
	type cursor struct {
		t  time.Time
		id []byte
	}
	cursors := make(map[string]cursor, len(journals))
	now := time.Now()
	for _, j := range journals {
		entries, err := c.ReadJournalAll(ctx, ld, j, now.Add(-since), now)
		if err != nil {
			return fmt.Errorf("read %s: %w", j, err)
		}
		fmt.Fprintf(out, "%s/%s: %d entries\n", ld, j, len(entries))
		printEntries(out, j, entries)
		cur := cursor{t: now.Add(-since)}
		if n := len(entries); n > 0 {
			cur = cursor{t: entries[n-1].OccurrenceTime, id: entries[n-1].EntryID}
		}
		cursors[j] = cur
	}
	if !follow {
		return nil
	}

	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		for _, j := range journals {
			cur := cursors[j]
			var entries []iec61850.JournalEntry
			var err error
			if cur.id == nil {
				entries, err = c.ReadJournalAll(ctx, ld, j, cur.t, time.Now())
			} else {
				entries, err = c.ReadJournalAfterAll(ctx, ld, j, cur.t, cur.id)
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("poll %s: %w", j, err)
			}
			printEntries(out, j, entries)
			if n := len(entries); n > 0 {
				cursors[j] = cursor{t: entries[n-1].OccurrenceTime, id: entries[n-1].EntryID}
			}
		}
	}
}

func printEntries(out io.Writer, journal string, entries []iec61850.JournalEntry) {
	for _, e := range entries {
		for _, v := range e.Variables {
			fmt.Fprintf(out, "  %s #%x %s %s = %v\n", journal, e.EntryID, e.OccurrenceTime.Format(time.RFC3339Nano), v.Tag, v.Value)
		}
	}
}
