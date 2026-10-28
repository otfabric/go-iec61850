// SPDX-License-Identifier: MIT

// Command ied-client is an end-to-end IEC 61850 client application.
//
// It shows the lifecycle of a typical client:
//
//  1. Connect with structured logging and identify the server.
//  2. Discover the data model.
//  3. Read a measurement (FC MX).
//  4. Subscribe to an unbuffered report control block and print the
//     reports until interrupted (SIGINT/SIGTERM), then shut down
//     cleanly: the subscription is closed, which disables the RCB, and
//     the association is concluded.
//
// For reconnecting after a lost connection see examples/resilient-client.
//
// Usage:
//
//	ied-client [-addr 127.0.0.1:102]
//
// Run it against examples/ied-server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(ctx, *addr, logger, 0, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run connects to addr and prints reports until ctx is done or, when
// maxReports > 0, that many reports arrived.
func run(ctx context.Context, addr string, logger *slog.Logger, maxReports int, out io.Writer) error {
	// 1. Connect and identify.
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	client, err := iec61850.Dial(dialCtx, addr, iec61850.DialOptions{Logger: logger})
	cancel()
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			logger.Warn("close", "err", err)
		}
	}()
	if id, err := client.MMS().Identify(ctx); err == nil {
		fmt.Fprintf(out, "connected to %s %s %s\n", id.Vendor, id.Model, id.Revision)
	} else {
		logger.Info("identify not supported", "err", err)
	}

	// 2. Discover the model.
	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return errors.New("server has no logical devices")
	}
	ld := devices[0].Name
	nodes, err := client.ListLogicalNodes(ctx, ld)
	if err != nil {
		return err
	}
	names := make([]string, len(nodes))
	for i, ln := range nodes {
		names[i] = ln.Name
	}
	fmt.Fprintf(out, "%s: %s\n", ld, strings.Join(names, " "))

	// 3. Read a measurement.
	root, err := client.TreeWithOptions(ctx, iec61850.TreeOptions{LDFilter: ld, IncludeFCs: true})
	if err != nil {
		return err
	}
	if ref, ok := findLeaf(root, "MX"); ok {
		v, err := client.Read(ctx, ref)
		if err != nil {
			return fmt.Errorf("read %s: %w", ref, err)
		}
		fmt.Fprintf(out, "%s = %v\n", ref, v)
	}

	// 4. Subscribe to the first URCB ($RP$; buffered RCBs are $BR$).
	rcbs, err := client.ListReports(ctx, ld)
	if err != nil {
		return err
	}
	var rcbItem string
	for _, item := range rcbs {
		if strings.Contains(item, "$RP$") {
			rcbItem = item
			break
		}
	}
	if rcbItem == "" {
		fmt.Fprintln(out, "no URCB; done")
		return nil
	}
	rcb, err := client.GetReportControlBlock(ctx, ld, rcbItem)
	if err != nil {
		return err
	}
	sub, err := client.SubscribeReport(ctx, rcb.RptID, iec61850.SubscribeReportOptions{
		LD:            ld,
		RCBItemID:     rcbItem,
		AutoEnable:    true,
		GIOnSubscribe: true,
		QueueSize:     64,
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", rcbItem, err)
	}
	defer func() { _ = sub.Close() }()
	fmt.Fprintf(out, "subscribed to %s (RptID %s)\n", rcbItem, rcb.RptID)

	for n := 0; maxReports == 0 || n < maxReports; n++ {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "shutting down")
			return nil
		case rpt, ok := <-sub.Reports():
			if !ok {
				return errors.New("subscription closed by the server")
			}
			fmt.Fprintf(out, "report seq=%d:", rpt.SeqNum)
			for _, v := range rpt.Values {
				fmt.Fprintf(out, " %v", v)
			}
			fmt.Fprintln(out)
		}
	}
	return nil
}

// findLeaf returns the first leaf attribute with functional constraint fc.
func findLeaf(n *iec61850.ModelNode, fc iec61850.FunctionalConstraint) (iec61850.Ref, bool) {
	if n == nil {
		return iec61850.Ref{}, false
	}
	if len(n.Children) == 0 && n.FC == fc {
		return n.Reference.WithFC(fc), true
	}
	for _, child := range n.Children {
		if ref, ok := findLeaf(child, fc); ok {
			return ref, true
		}
	}
	return iec61850.Ref{}, false
}
