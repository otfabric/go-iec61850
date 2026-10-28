// SPDX-License-Identifier: MIT

// Command ied-server is an end-to-end IEC 61850 server application: the
// demo IED (examples/internal/demoied/demo.icd) with every service the
// other examples use.
//
// It shows the lifecycle of a typical server:
//
//  1. Build the model from SCL (NewServerModelFromSCL) and create the
//     server with identity, logging and connection hooks.
//  2. Enable reports, journals, setting groups and file services
//     (demoied.NewServer) and, with -iface, GOOSE and Sampled Values.
//  3. Register the direct-operate control GGIO1.SPCSO1, whose handler
//     updates GGIO1.SPCSO1.stVal.
//  4. Serve MMS and update process values: MMXU1.PhV.phsA every
//     -interval (reported, logged), GGIO1.Ind1 every fifth interval
//     (published by GOOSE), ITCTR1.AmpSv (in the SV stream).
//  5. Shut down on SIGINT/SIGTERM: Close stops the engines and ends the
//     associations.
//
// Usage:
//
//	ied-server [-addr :102] [-iface eth0] [-interval 2s]
//
// To serve your own SCL, build the model with
// iec61850.NewServerModelFromSCL(doc, iedName, apName) as
// examples/internal/demoied does.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/sv"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// Store keys ("<LD>/<MMS item ID>") of the simulated process values.
const (
	keyPhV    = demoied.LD + "/MMXU1$MX$PhV$phsA$cVal$mag$f"
	keyInd1   = demoied.LD + "/GGIO1$ST$Ind1$stVal"
	keySPCSO1 = demoied.LD + "/GGIO1$ST$SPCSO1$stVal"
	keyIA     = demoied.LD + "/ITCTR1$MX$AmpSv$instMag$i"
)

type config struct {
	Listener *iso.Listener
	GOOSE    goose.Transport // nil: no GOOSE
	SV       sv.Transport    // nil: no SV
	Interval time.Duration
	Logger   *slog.Logger
}

func main() {
	addr := flag.String("addr", ":102", "MMS listen address")
	iface := flag.String("iface", "", "network interface for GOOSE and SV (optional)")
	interval := flag.Duration("interval", 2*time.Second, "process value update interval")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config{Interval: *interval, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	if *iface != "" {
		gt, err := goose.Open(*iface, goose.TransportOptions{})
		if errors.Is(err, goose.ErrPermission) {
			log.Fatalf("%v (needs CAP_NET_RAW on Linux, /dev/bpf access on macOS)", err)
		} else if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = gt.Close() }()
		st, err := sv.Open(*iface, sv.TransportOptions{})
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = st.Close() }()
		cfg.GOOSE, cfg.SV = gt, st
	}
	ln, err := iso.Listen(*addr)
	if err != nil {
		log.Fatal(err)
	}
	cfg.Listener = ln

	if err := run(ctx, cfg, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cfg config, out io.Writer) error {
	srv, err := demoied.NewServer(iec61850.ServerOptions{
		Identity: &iec61850.ServerIdentity{Vendor: "OTFabric", Model: "DemoIED", Revision: "1.0"},
		Logger:   cfg.Logger,
		OnConnect: func(iec61850.ConnectionEvent) {
			cfg.Logger.Info("client connected")
		},
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	if err := srv.RegisterControl(demoied.LD, "GGIO1.SPCSO1", iec61850.CtlModelDirectNormal, iec61850.ControlHandler{
		OnOperate: func(ctx context.Context, req iec61850.ControlRequest) error {
			on, _ := req.CtlVal.Bool()
			srv.SetValue(ctx, keySPCSO1, mms.NewBoolean(on))
			fmt.Fprintf(out, "SPCSO1 operated: %v\n", on)
			return nil
		},
	}); err != nil {
		return err
	}
	if cfg.GOOSE != nil {
		if _, err := srv.EnableGOOSE(iec61850.GOOSEOptions{Transport: cfg.GOOSE, Logger: cfg.Logger}); err != nil {
			return err
		}
	}
	if cfg.SV != nil {
		if _, err := srv.EnableSV(iec61850.SVOptions{Transport: cfg.SV, SmpSynch: sv.SmpSynchLocal, Logger: cfg.Logger}); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "serving %s on %s (%s)\n", demoied.IED, cfg.Listener.Addr(), srv.Capabilities())

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe(ctx, cfg.Listener) }()

	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	for n := 1; ; n++ {
		select {
		case <-ctx.Done():
			if err := <-serveErr; err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			fmt.Fprintln(out, "shut down")
			return nil
		case err := <-serveErr:
			return err
		case <-tick.C:
			phv := 230 + 2*math.Sin(float64(n)/5)
			srv.SetValue(ctx, keyPhV, mms.NewFloat(phv))
			srv.SetValue(ctx, keyIA, mms.NewInteger(int64(sv.AmpsCount(100*math.Sin(float64(n))))))
			if n%5 == 0 {
				srv.SetValue(ctx, keyInd1, mms.NewBoolean(n%10 == 0))
			}
		}
	}
}
