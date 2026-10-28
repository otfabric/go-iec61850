// SPDX-License-Identifier: MIT

// Command goose-subscriber subscribes to a GOOSE control block
// configured from SCL and supervises it.
//
// The subscription filters on the control block's destination MAC and
// APPID and validates the message against the SCL: confRev, the number
// and types of the dataset members and, unless -any-ref is set, gocbRef
// and datSet. It prints every new state with its values, supervision
// events (timeAllowedToLive expiry, restarts, sequence gaps, invalid
// messages) and a supervision summary every -stats interval.
//
// Usage:
//
//	goose-subscriber -iface eth0 [-scl station.scd -ied IED1 -ld LD0 -gocb gcb1] [-any-ref] [-stats 10s]
//
// Without -scl it uses the demo IED (examples/internal/demoied/demo.icd).
// The go-iec61850 server publishes with the bare LD as MMS domain
// (LD1/LLN0$GO$gcb01) where SCL names IED+LD (DemoIEDLD1/LLN0$GO$gcb01);
// use -any-ref against examples/goose-server.
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
	"strings"
	"syscall"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
)

func main() {
	iface := flag.String("iface", "", "network interface (required)")
	sclPath := flag.String("scl", "", "SCL file (default: the demo IED)")
	ied := flag.String("ied", demoied.IED, "IED name")
	ld := flag.String("ld", demoied.LD, "LD instance")
	gocb := flag.String("gocb", "gcb01", "GSEControl name")
	anyRef := flag.Bool("any-ref", false, "do not check gocbRef and datSet")
	stats := flag.Duration("stats", 10*time.Second, "supervision summary interval")
	flag.Parse()
	if *iface == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var doc *scl.SCL
	var err error
	if *sclPath == "" {
		doc, err = demoied.SCL()
	} else {
		doc, err = scl.ParseFile(*sclPath)
	}
	if err != nil {
		log.Fatal(err)
	}
	cb, err := goose.ControlBlockFromSCL(doc, *ied, *ld, *gocb)
	if err != nil {
		log.Fatal(err)
	}
	// Join the multicast group instead of making the interface promiscuous.
	tr, err := goose.Open(*iface, goose.TransportOptions{Multicast: []goose.MAC{cb.Dst}})
	if err != nil {
		if errors.Is(err, goose.ErrPermission) {
			log.Fatalf("%v (needs CAP_NET_RAW on Linux, /dev/bpf access on macOS)", err)
		}
		log.Fatal(err)
	}
	defer func() { _ = tr.Close() }()

	if err := run(ctx, tr, cb, *anyRef, *stats, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run subscribes to cb on tr and prints what it receives until ctx is
// done.
func run(ctx context.Context, tr goose.Transport, cb *goose.ControlBlock, anyRef bool, statsEvery time.Duration, out io.Writer) error {
	sub, err := goose.NewSubscriber(tr, goose.SubscriberConfig{})
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()

	cfg := cb.SubscriptionConfig() // Dst, APPID, gocbRef, datSet, goID, confRev, member types
	if anyRef {
		cfg.GocbRef, cfg.DatSet, cfg.GoID = "", "", ""
	}
	cfg.OnlyStateChanges = true // retransmissions only feed supervision
	s, err := sub.Subscribe(cfg)
	if err != nil {
		return err
	}

	runErr := make(chan error, 1)
	go func() { runErr <- sub.Run(ctx) }()
	fmt.Fprintf(out, "subscribed to %s (APPID %#04x, %s)\n", cb.GocbRef, cb.AppID, cb.Dst)

	tick := time.NewTicker(statsEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			printSupervision(out, s)
			return nil
		case err := <-runErr:
			if errors.Is(err, goose.ErrClosed) {
				return nil
			}
			return err
		case <-tick.C:
			printSupervision(out, s)
		case ev := <-s.Events():
			printEvent(out, cb, ev)
		}
	}
}

func printEvent(out io.Writer, cb *goose.ControlBlock, ev goose.Event) {
	switch ev.Kind {
	case goose.EventMessage:
		p := &ev.Frame.PDU
		vals := make([]string, len(p.AllData))
		for i, v := range p.AllData {
			name := fmt.Sprint(i)
			if i < len(cb.Members) {
				name = cb.Members[i].Ref
			}
			vals[i] = fmt.Sprintf("%s=%v", name, v)
		}
		fmt.Fprintf(out, "state stNum=%d from %s: %s\n", p.StNum, ev.Frame.Src, strings.Join(vals, " "))
	case goose.EventInvalid:
		// ev.Err wraps goose.ErrValidation and the specific mismatch.
		switch {
		case errors.Is(ev.Err, goose.ErrConfRevMismatch):
			fmt.Fprintf(out, "invalid: configuration revision differs from SCL: %v\n", ev.Err)
		case errors.Is(ev.Err, goose.ErrNeedsCommissioning):
			fmt.Fprintf(out, "invalid: publisher needs commissioning: %v\n", ev.Err)
		default:
			fmt.Fprintf(out, "invalid: %v\n", ev.Err)
		}
	default:
		// Timeout (timeAllowedToLive expired), restart, gaps, ...
		fmt.Fprintf(out, "%s: %s stNum=%d\n", ev.Kind, ev.State.Stream.GocbRef, ev.State.LastStNum)
	}
}

func printSupervision(out io.Writer, s *goose.Subscription) {
	for _, st := range s.Supervision() {
		fmt.Fprintf(out, "supervision %s from %s: %s, %d messages, %d state changes, %d timeouts, %d invalid\n",
			st.Stream.GocbRef, st.Stream.Src, st.Status, st.Messages, st.StateChanges, st.Timeouts, st.Invalid)
	}
}
