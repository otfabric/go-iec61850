// Command reports demonstrates subscribing to an IEC 61850 buffered
// report and printing incoming indications.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/otfabric/go-iec61850"
)

func main() {
	addr := "127.0.0.1:102"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()
	client, err := iec61850.Dial(dialCtx, addr, iec61850.DialOptions{})
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer client.Close(context.Background()) //nolint:errcheck

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		log.Fatalf("list LDs: %v", err)
	}
	if len(devices) == 0 {
		log.Fatal("no logical devices found")
	}
	ld := devices[0].Name

	reports, err := client.ListReports(ctx, ld)
	if err != nil {
		if errors.Is(err, iec61850.ErrUnsupportedService) || errors.Is(err, iec61850.ErrNotFound) {
			log.Printf("list reports: %v (server may not support reporting)", err)
			return
		}
		log.Fatalf("list reports: %v", err)
	}

	fmt.Printf("Found %d report(s) in %s:\n", len(reports), ld)
	for _, r := range reports {
		fmt.Printf("  %s\n", r)
	}

	if len(reports) == 0 {
		fmt.Println("no reports found in", ld)
		return
	}

	rcb, err := client.GetReportControlBlock(ctx, ld, reports[0])
	if err != nil {
		if errors.Is(err, iec61850.ErrNotFound) || errors.Is(err, iec61850.ErrDataAccess) {
			log.Printf("get RCB %s: %v (RCB may not be accessible)", reports[0], err)
			return
		}
		log.Fatalf("get RCB: %v", err)
	}
	fmt.Printf("\nSubscribing to %s (RptID=%s, DatSet=%s)\n", reports[0], rcb.RptID, rcb.DatSet)

	sub, err := client.SubscribeReport(ctx, rcb.RptID, iec61850.SubscribeReportOptions{
		QueueSize:     64,
		AutoEnable:    true,
		GIOnSubscribe: true,
		LD:            ld,
		RCBItemID:     reports[0],
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	fmt.Println("Waiting for reports (Ctrl-C to stop)...")
	for {
		select {
		case ri, ok := <-sub.Reports():
			if !ok {
				fmt.Println("subscription closed")
				return
			}
			fmt.Printf("\nReport RptID=%s SeqNum=%d\n", ri.RptID, ri.SeqNum)
			fmt.Printf("  OptFlds: %s\n", ri.OptFlds)
			fmt.Printf("  Values:  %d\n", len(ri.Values))
			for i, v := range ri.Values {
				if v != nil {
					fmt.Printf("    [%d] %s\n", i, v)
				}
			}
		case <-ctx.Done():
			fmt.Println("\ninterrupted")
			return
		}
	}
}
