// SPDX-License-Identifier: MIT

// Command svcb-client reads and reconfigures a multicast Sampled Values
// control block (MSVCB) over MMS.
//
// It reads the MSVCB (SvEna, MsvID, DatSet, ConfRev, SmpRate, OptFlds,
// SmpMod, DstAddress, noASDU), then disables the stream, sets a new
// sample rate and asks for the optional smpRate and refresh-time fields
// in every ASDU, and enables it again: one SetMSVCB call, which writes
// SvEna last because the other attributes are writable only while the
// MSVCB is disabled.
//
// Usage:
//
//	svcb-client [-addr 127.0.0.1:102] [-ld LD1] [-msvcb LLN0$MS$MSVCB01] [-rate 96]
//
// Run it against examples/ied-server started with -iface.
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
	"github.com/otfabric/go-iec61850/sv"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ld := flag.String("ld", "LD1", "logical device (MMS domain)")
	msvcb := flag.String("msvcb", "LLN0$MS$MSVCB01", "MSVCB item ID")
	rate := flag.Uint("rate", 96, "new SmpRate")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := run(ctx, *addr, *ld, *msvcb, uint16(*rate), os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, ld, msvcb string, rate uint16, out io.Writer) error {
	c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.Background()) }()

	cb, err := c.GetMSVCB(ctx, ld, msvcb)
	if err != nil {
		if errors.Is(err, iec61850.ErrNotFound) {
			return fmt.Errorf("no MSVCB %s in %s: %w", msvcb, ld, err)
		}
		return err
	}
	printMSVCB(out, cb)

	opts := cb.OptFlds
	opts.SampleRate, opts.RefreshTime = true, true
	off, on := false, true
	if cb.SvEna {
		if err := c.SetMSVCB(ctx, ld, msvcb, iec61850.MSVCBUpdate{SvEna: &off}); err != nil {
			return fmt.Errorf("disable: %w", err)
		}
	}
	err = c.SetMSVCB(ctx, ld, msvcb, iec61850.MSVCBUpdate{SmpRate: &rate, OptFlds: &opts, SvEna: &on})
	if err != nil {
		return fmt.Errorf("reconfigure: %w", err)
	}

	cb, err = c.GetMSVCB(ctx, ld, msvcb)
	if err != nil {
		return err
	}
	printMSVCB(out, cb)
	return nil
}

func printMSVCB(out io.Writer, cb *iec61850.MSVCB) {
	smpMod := "not present (Edition 1)"
	if cb.HasSmpMod {
		smpMod = cb.SmpMod.String()
	}
	fmt.Fprintf(out, "%s: SvEna=%v MsvID=%q DatSet=%s ConfRev=%d\n", cb.Reference, cb.SvEna, cb.MsvID, cb.DatSet, cb.ConfRev)
	fmt.Fprintf(out, "  SmpRate=%d SmpMod=%s (%.0f samples/s at 50 Hz) noASDU=%d OptFlds=%+v\n",
		cb.SmpRate, smpMod, sv.SamplesPerSecond(cb.SmpRate, cb.SmpMod, 50), cb.NoASDU, cb.OptFlds)
	fmt.Fprintf(out, "  DstAddress=%s APPID=%#04x VID=%d priority=%d\n",
		cb.DstAddress.Addr, cb.DstAddress.AppID, cb.DstAddress.VID, cb.DstAddress.Priority)
}
