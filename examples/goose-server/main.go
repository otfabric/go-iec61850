// SPDX-License-Identifier: MIT

// Command goose-server runs an IEC 61850 server that publishes its GOOSE
// control blocks on data change.
//
// The server is built from SCL (default: the demo IED). Server.EnableGOOSE
// starts a publisher for every GSEControl with a GSE address; a change of
// a dataset member, through Server.SetValue or an MMS client write,
// publishes a new state. Clients read and write the GoCB over MMS
// (examples/goose-client). The program toggles GGIO1.Ind1.stVal every
// -interval to simulate a binary input.
//
// Usage:
//
//	goose-server -iface eth0 [-addr :102] [-interval 2s]
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
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ind1 is the store key of the demo IED's GGIO1.Ind1.stVal: the MMS
// domain (the LD) and item ID.
const ind1 = demoied.LD + "/GGIO1$ST$Ind1$stVal"

func main() {
	iface := flag.String("iface", "", "network interface for GOOSE (required)")
	addr := flag.String("addr", ":102", "MMS listen address")
	interval := flag.Duration("interval", 2*time.Second, "time between input changes")
	flag.Parse()
	if *iface == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tr, err := goose.Open(*iface, goose.TransportOptions{})
	if err != nil {
		if errors.Is(err, goose.ErrPermission) {
			log.Fatalf("%v (needs CAP_NET_RAW on Linux, /dev/bpf access on macOS)", err)
		}
		log.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	ln, err := iso.Listen(*addr)
	if err != nil {
		log.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(ctx, tr, ln, *interval, logger, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run serves the demo IED on ln and publishes its GoCBs on tr until ctx
// is done.
func run(ctx context.Context, tr goose.Transport, ln *iso.Listener, interval time.Duration, logger *slog.Logger, out io.Writer) error {
	srv, err := demoied.NewServer(iec61850.ServerOptions{Logger: logger})
	if err != nil {
		return err
	}
	defer srv.Close() // also stops the GOOSE engine

	engine, err := srv.EnableGOOSE(iec61850.GOOSEOptions{Transport: tr, Logger: logger})
	if err != nil {
		return err
	}
	for _, ref := range engine.ControlBlocks() {
		fmt.Fprintf(out, "publishing %s\n", ref)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe(ctx, ln) }()
	fmt.Fprintf(out, "MMS on %s\n", ln.Addr())

	tick := time.NewTicker(interval)
	defer tick.Stop()
	state := false
	for {
		select {
		case <-ctx.Done():
			if err := <-serveErr; err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		case err := <-serveErr:
			return err
		case <-tick.C:
			state = !state
			srv.SetValue(ctx, ind1, mms.NewBoolean(state)) // -> new GOOSE state
			fmt.Fprintf(out, "Ind1 = %v\n", state)
		}
	}
}
