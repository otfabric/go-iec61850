// SPDX-License-Identifier: MIT

// Command ied-client is an end-to-end example of how a production
// IEC 61850 client application is structured using go-iec61850.
//
// It demonstrates the intended application lifecycle:
//
//  1. Connect to a server and identify it.
//  2. Discover the data model.
//  3. Read one MX (measurement) value.
//  4. Subscribe to a URCB report and print incoming indications.
//  5. Handle SIGINT for graceful shutdown.
//
// Reconnect: this example does not implement automatic reconnection.
// The explicit model is intentional — reconnect policy belongs in the
// application, not the library. A real application would wrap the main
// loop in a retry loop with back-off, re-discover the model, and
// re-subscribe to reports after each successful re-dial.
//
// Usage:
//
//	go run ./examples/ied-client [addr]          # default: 127.0.0.1:102
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

func main() {
	addr := "127.0.0.1:102"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Honour SIGINT / Ctrl-C for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// -----------------------------------------------------------------------
	// 1. Connect and identify
	// -----------------------------------------------------------------------

	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dialCancel()

	client, err := iec61850.Dial(dialCtx, addr, iec61850.DialOptions{
		Logger: logger,
	})
	if err != nil {
		log.Fatalf("dial %s: %v", addr, err)
	}
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			logger.Warn("close", "err", err)
		}
	}()

	id, err := client.MMS().Identify(dialCtx)
	if err != nil {
		log.Printf("identify: %v (server may not support Identify)", err)
	} else {
		fmt.Printf("Connected to %s %s (revision %s)\n", id.Vendor, id.Model, id.Revision)
	}

	// -----------------------------------------------------------------------
	// 2. Discover data model
	// -----------------------------------------------------------------------

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		log.Fatalf("list LDs: %v", err)
	}
	if len(devices) == 0 {
		log.Fatal("server has no logical devices")
	}

	fmt.Printf("\nLogical devices (%d):\n", len(devices))
	for _, ld := range devices {
		fmt.Printf("  %s\n", ld.Name)
		nodes, err := client.ListLogicalNodes(ctx, ld.Name)
		if err != nil {
			logger.Warn("list LNs", "ld", ld.Name, "err", err)
			continue
		}
		for _, ln := range nodes {
			fmt.Printf("    %s\n", ln.Name)
		}
	}

	// -----------------------------------------------------------------------
	// 3. Read one MX measurement from the first LD
	// -----------------------------------------------------------------------

	ld := devices[0].Name
	if ref, ok := findMXLeaf(ctx, client, ld); ok {
		val, err := client.Read(ctx, ref)
		if err != nil {
			logger.Warn("read", "ref", ref, "err", err)
		} else {
			fmt.Printf("\nMX sample: %s = %s\n", ref, val)
		}
	}

	// -----------------------------------------------------------------------
	// 4. Subscribe to URCB reports
	// -----------------------------------------------------------------------

	reports, err := client.ListReports(ctx, ld)
	if err != nil {
		if errors.Is(err, iec61850.ErrUnsupportedService) || errors.Is(err, iec61850.ErrNotFound) {
			fmt.Println("\nServer does not expose report control blocks — skipping subscription.")
		} else {
			log.Fatalf("list reports: %v", err)
		}
		<-ctx.Done()
		return
	}

	// Find the first URCB (unbuffered). ListReports returns item IDs like "LLN0$RP$urcb01".
	var rcbItemID string
	for _, itemID := range reports {
		if strings.Contains(itemID, "$RP$") {
			rcbItemID = itemID
			break
		}
	}

	if rcbItemID == "" {
		fmt.Println("\nNo URCB found on server — skipping subscription.")
		<-ctx.Done()
		return
	}

	// Extract short RCB name for the subscription ID (last segment after $RP$).
	rptID := rcbItemID
	if idx := strings.LastIndex(rcbItemID, "$"); idx >= 0 {
		rptID = rcbItemID[idx+1:]
	}

	sub, err := client.SubscribeReport(ctx, rptID, iec61850.SubscribeReportOptions{
		LD:             ld,
		RCBItemID:      rcbItemID,
		AutoEnable:     true,
		GIOnSubscribe:  true,
		QueueSize:      64,
		OverflowPolicy: iec61850.OverflowDropNewest,
	})
	if err != nil {
		log.Fatalf("subscribe %s: %v", rcbItemID, err)
	}
	defer sub.Close() //nolint:errcheck

	fmt.Printf("\nSubscribed to %s — waiting for reports (Ctrl-C to stop)...\n", rcbItemID)

	for {
		select {
		case rpt, ok := <-sub.Reports():
			if !ok {
				fmt.Println("subscription closed")
				return
			}
			printReport(rpt)

		case <-ctx.Done():
			fmt.Println("\nShutting down.")
			return
		}
	}
}

// findMXLeaf traverses the server model looking for the first MX-FC
// leaf attribute.
func findMXLeaf(ctx context.Context, c *iec61850.Client, _ string) (iec61850.Ref, bool) {
	root, err := c.TreeWithOptions(ctx, iec61850.TreeOptions{IncludeFCs: true})
	if err != nil {
		return iec61850.Ref{}, false
	}
	return searchMX(root)
}

func searchMX(n *iec61850.ModelNode) (iec61850.Ref, bool) {
	if n == nil {
		return iec61850.Ref{}, false
	}
	if len(n.Children) == 0 && n.FC == "MX" {
		return n.Reference.WithFC("MX"), true
	}
	for _, child := range n.Children {
		if ref, ok := searchMX(child); ok {
			return ref, true
		}
	}
	return iec61850.Ref{}, false
}

func printReport(rpt *iec61850.ReportIndication) {
	fmt.Printf("[report] seq=%d", rpt.SeqNum)
	if !rpt.Timestamp.IsZero() {
		fmt.Printf(" t=%s", rpt.Timestamp.Format(time.RFC3339Nano))
	}
	for i, v := range rpt.Values {
		reason := ""
		if i < len(rpt.ReasonCodes) {
			reason = fmt.Sprintf(" reason=%v", rpt.ReasonCodes[i])
		}
		fmt.Printf("\n  [%d]%s %v", i, reason, v)
	}
	fmt.Println()
}
