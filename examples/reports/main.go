// SPDX-License-Identifier: MIT

// Command reports subscribes to an IEC 61850 report control block and
// prints the reports it receives.
//
// It reads the RCB (RptID, DatSet, TrgOps, OptFlds), reserves and
// enables it with a general interrogation, and prints each report with
// its sequence number, reason codes and values. Without -rcb it uses the
// first RCB of the first logical device.
//
// Usage:
//
//	reports [-addr 127.0.0.1:102] [-ld LD1] [-rcb LLN0$RP$urcb01] [-count 0]
//
// Run it against examples/ied-server, which updates its measurement
// every two seconds.
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
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ld := flag.String("ld", "", "logical device (default: the first)")
	rcb := flag.String("rcb", "", "RCB item ID (default: the first)")
	count := flag.Int("count", 0, "stop after this many reports (0 = until interrupted)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *ld, *rcb, *count, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, ld, rcbItem string, count int, out io.Writer) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	client, err := iec61850.Dial(dialCtx, addr, iec61850.DialOptions{})
	cancel()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()

	if ld == "" {
		devices, err := client.ListLogicalDevices(ctx)
		if err != nil {
			return err
		}
		if len(devices) == 0 {
			return errors.New("server has no logical devices")
		}
		ld = devices[0].Name
	}
	if rcbItem == "" {
		rcbs, err := client.ListReports(ctx, ld)
		if err != nil {
			return err
		}
		if len(rcbs) == 0 {
			return fmt.Errorf("%s has no report control blocks", ld)
		}
		rcbItem = rcbs[0]
	}

	rcb, err := client.GetReportControlBlock(ctx, ld, rcbItem)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s/%s: RptID=%s DatSet=%s\n", ld, rcbItem, rcb.RptID, rcb.DatSet)

	// Reports are routed by RptID; AutoEnable reserves (URCB) and
	// enables the RCB, GIOnSubscribe asks for the current values.
	sub, err := client.SubscribeReport(ctx, rcb.RptID, iec61850.SubscribeReportOptions{
		LD:             ld,
		RCBItemID:      rcbItem,
		AutoEnable:     true,
		GIOnSubscribe:  true,
		QueueSize:      64,
		OverflowPolicy: iec61850.OverflowDropOldest,
	})
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()

	for n := 0; count == 0 || n < count; n++ {
		select {
		case <-ctx.Done():
			return nil
		case rpt, ok := <-sub.Reports():
			if !ok {
				return errors.New("subscription closed")
			}
			printReport(out, rpt)
		}
	}
	return nil
}

func printReport(out io.Writer, rpt *iec61850.ReportIndication) {
	fmt.Fprintf(out, "report %s seq=%d", rpt.RptID, rpt.SeqNum)
	if !rpt.Timestamp.IsZero() {
		fmt.Fprintf(out, " t=%s", rpt.Timestamp.Format(time.RFC3339Nano))
	}
	fmt.Fprintln(out)
	for i, v := range rpt.Values {
		reason := ""
		if i < len(rpt.ReasonCodes) {
			reason = fmt.Sprintf(" (%v)", rpt.ReasonCodes[i])
		}
		fmt.Fprintf(out, "  [%d] %v%s\n", i, v, reason)
	}
}
