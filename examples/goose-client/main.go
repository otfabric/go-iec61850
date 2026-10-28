// SPDX-License-Identifier: MIT

// Command goose-client reads and writes a GOOSE control block over MMS.
//
// It reads the GoCB (GoEna, GoID, DatSet, ConfRev, NdsCom, DstAddress,
// MinTime, MaxTime), shows that GoID cannot change while the GoCB is
// enabled, then disables it, changes GoID and enables it again with one
// SetGoCB call (GoEna is written last).
//
// Usage:
//
//	goose-client [-addr 127.0.0.1:102] [-ld LD1] [-gocb LLN0$GO$gcb01] [-goid NewGoID]
//
// Run it against examples/goose-server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ld := flag.String("ld", "LD1", "logical device (MMS domain)")
	gocb := flag.String("gocb", "LLN0$GO$gcb01", "GoCB item ID")
	goID := flag.String("goid", "DemoGooseRenamed", "new GoID")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := run(ctx, *addr, *ld, *gocb, *goID, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, ld, gocb, goID string, out io.Writer) error {
	c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.Background()) }()

	cb, err := c.GetGoCB(ctx, ld, gocb)
	if err != nil {
		if errors.Is(err, iec61850.ErrNotFound) {
			return fmt.Errorf("no GoCB %s in %s: %w", gocb, ld, err)
		}
		return err
	}
	printGoCB(out, cb)

	// GoID and DatSet are writable only while GoEna is false: the server
	// answers temporarily-unavailable otherwise.
	if cb.GoEna {
		err := c.SetGoCB(ctx, ld, gocb, iec61850.GoCBUpdate{GoID: &goID})
		var dae *mms.DataAccessError
		if errors.As(err, &dae) && dae.Code == mms.DataAccessErrorTemporarilyUnavailable {
			fmt.Fprintln(out, "GoID write while enabled: rejected (temporarily-unavailable), as expected")
		} else {
			fmt.Fprintf(out, "GoID write while enabled: %v\n", err)
		}
	}

	off, on := false, true
	if err := c.SetGoCB(ctx, ld, gocb, iec61850.GoCBUpdate{GoEna: &off}); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	// One call: GoID first, then GoEna.
	if err := c.SetGoCB(ctx, ld, gocb, iec61850.GoCBUpdate{GoID: &goID, GoEna: &on}); err != nil {
		return fmt.Errorf("rename and enable: %w", err)
	}
	cb, err = c.GetGoCB(ctx, ld, gocb)
	if err != nil {
		return err
	}
	printGoCB(out, cb)
	return nil
}

func printGoCB(out io.Writer, cb *iec61850.GoCB) {
	fmt.Fprintf(out, "%s: GoEna=%v GoID=%q DatSet=%s ConfRev=%d NdsCom=%v\n",
		cb.Reference, cb.GoEna, cb.GoID, cb.DatSet, cb.ConfRev, cb.NdsCom)
	fmt.Fprintf(out, "  DstAddress=%s APPID=%#04x VID=%d priority=%d",
		cb.DstAddress.Addr, cb.DstAddress.AppID, cb.DstAddress.VID, cb.DstAddress.Priority)
	if cb.HasTimes {
		fmt.Fprintf(out, " MinTime=%dms MaxTime=%dms", cb.MinTime, cb.MaxTime)
	}
	fmt.Fprintln(out)
}
