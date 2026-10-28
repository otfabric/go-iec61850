// SPDX-License-Identifier: MIT

// Command goose-publisher publishes a GOOSE control block configured
// from SCL and toggles its first boolean member periodically.
//
// The control block's address (destination MAC, APPID, VLAN), times
// (MinTime, MaxTime), references and dataset come from the SCL file;
// the publisher sends the dataset's initial values and then a new
// state (stNum+1) on every toggle, retransmitting in between.
//
// Usage:
//
//	goose-publisher -iface eth0 [-scl station.scd -ied IED1 -ld LD0 -gocb gcb1] [-interval 2s]
//
// Without -scl it uses the demo IED (examples/internal/demoied/demo.icd).
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

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
	mms "github.com/otfabric/go-mms"
)

func main() {
	iface := flag.String("iface", "", "network interface (required)")
	sclPath := flag.String("scl", "", "SCL file (default: the demo IED)")
	ied := flag.String("ied", demoied.IED, "IED name")
	ld := flag.String("ld", demoied.LD, "LD instance")
	gocb := flag.String("gocb", "gcb01", "GSEControl name")
	interval := flag.Duration("interval", 2*time.Second, "time between state changes")
	flag.Parse()
	if *iface == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cb, err := loadControlBlock(*sclPath, *ied, *ld, *gocb)
	if err != nil {
		log.Fatal(err)
	}
	tr, err := goose.Open(*iface, goose.TransportOptions{})
	if err != nil {
		if errors.Is(err, goose.ErrPermission) {
			log.Fatalf("%v (needs CAP_NET_RAW on Linux, /dev/bpf access on macOS)", err)
		}
		log.Fatal(err)
	}
	defer func() { _ = tr.Close() }()

	if err := run(ctx, tr, cb, *interval, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// loadControlBlock resolves the GoCB from SCL.
func loadControlBlock(path, ied, ld, name string) (*goose.ControlBlock, error) {
	var doc *scl.SCL
	var err error
	if path == "" {
		doc, err = demoied.SCL()
	} else {
		doc, err = scl.ParseFile(path)
	}
	if err != nil {
		return nil, err
	}
	return goose.ControlBlockFromSCL(doc, ied, ld, name)
}

// run publishes cb on tr until ctx is done, toggling the first boolean
// member every interval.
func run(ctx context.Context, tr goose.Transport, cb *goose.ControlBlock, interval time.Duration, out io.Writer) error {
	cfg, err := cb.PublisherConfig() // address, references, times, zero values
	if err != nil {
		return err
	}
	cfg.Src = tr.HardwareAddr()
	pub, err := goose.NewPublisher(tr, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()

	values := cfg.Values
	toggle := -1
	for i, v := range values {
		if _, ok := v.Bool(); ok {
			toggle = i
			break
		}
	}
	if toggle < 0 {
		return fmt.Errorf("%s: dataset %s has no boolean member to toggle", cb.GocbRef, cb.DatSet)
	}

	done := make(chan error, 1)
	go func() { done <- pub.Run(ctx) }()
	fmt.Fprintf(out, "publishing %s (APPID %#04x to %s) on %s\n", cb.GocbRef, cb.AppID, cb.Dst, tr.HardwareAddr())

	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			st := pub.State()
			fmt.Fprintf(out, "stopped: stNum %d, %d frames\n", st.StNum, st.Sent)
			return nil
		case err := <-done:
			return err
		case <-tick.C:
			b, _ := values[toggle].Bool()
			values[toggle] = mms.NewBoolean(!b)
			if err := pub.Publish(values); err != nil {
				return err
			}
			fmt.Fprintf(out, "stNum %d: %s = %v\n", pub.State().StNum, cb.Members[toggle].Ref, !b)
		}
	}
}
