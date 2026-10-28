// SPDX-License-Identifier: MIT

// Command sv-subscriber receives a Sampled Values stream configured from
// SCL and reports its health.
//
// The subscription filters on the control block's destination MAC,
// APPID and svID and validates confRev, smpRate and the sample size
// against the SCL. Every -stats interval it prints the stream counters
// (frames, ASDUs, lost, duplicates, out of order, measured rate) and,
// for a 9-2LE dataset, the RMS values over the last interval computed
// in the zero-allocation OnASDU callback. Supervision events (start,
// timeout, restored, synchronisation change, gaps, invalid ASDUs) are
// printed as they occur.
//
// Usage:
//
//	sv-subscriber -iface eth0 [-scl station.scd -ied IED1 -ld MU01 -msvcb MSVCB01] [-stats 1s]
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
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
)

func main() {
	iface := flag.String("iface", "", "network interface (required)")
	sclPath := flag.String("scl", "", "SCL file (default: the demo IED)")
	ied := flag.String("ied", demoied.IED, "IED name")
	ld := flag.String("ld", demoied.LD, "LD instance")
	msvcb := flag.String("msvcb", "MSVCB01", "SampledValueControl name")
	stats := flag.Duration("stats", time.Second, "report interval")
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
	cb, err := sv.ControlBlockFromSCL(doc, *ied, *ld, *msvcb)
	if err != nil {
		log.Fatal(err)
	}
	tr, err := sv.Open(*iface, sv.TransportOptions{Multicast: []sv.MAC{cb.Dst}})
	if err != nil {
		if errors.Is(err, sv.ErrPermission) {
			log.Fatalf("%v (needs CAP_NET_RAW on Linux, /dev/bpf access on macOS)", err)
		}
		log.Fatal(err)
	}
	defer func() { _ = tr.Close() }()

	if err := run(ctx, tr, cb, *stats, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// rms accumulates the sum of squares of the eight 9-2LE channels.
type rms struct {
	mu  sync.Mutex
	n   int
	sum [8]float64
}

func (r *rms) add(le *sv.LESample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	for i := 0; i < 4; i++ {
		a, v := sv.Amps(le.Current[i].Value), sv.Volts(le.Voltage[i].Value)
		r.sum[i] += a * a
		r.sum[4+i] += v * v
	}
}

// take returns the RMS values since the last call and resets.
func (r *rms) take() (vals [8]float64, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range vals {
		if r.n > 0 {
			vals[i] = math.Sqrt(r.sum[i] / float64(r.n))
		}
	}
	n = r.n
	r.n, r.sum = 0, [8]float64{}
	return vals, n
}

// run subscribes to cb on tr and reports until ctx is done.
func run(ctx context.Context, tr sv.Transport, cb *sv.ControlBlock, every time.Duration, out io.Writer) error {
	s, err := sv.NewSubscriber(tr, sv.SubscriberConfig{})
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	le := len(cb.Layout) == len(sv.LELayout)
	var acc rms
	cfg := cb.SubscriptionConfig() // filters and validation from SCL
	if le {
		// OnASDU runs on the receive goroutine for every ASDU; the
		// sample is only valid during the call, so decode it here.
		cfg.OnASDU = func(smp *sv.Sample) {
			var s sv.LESample
			if sv.DecodeLE(smp.ASDU.Sample, &s) == nil {
				acc.add(&s)
			}
		}
	}
	sub, err := s.Subscribe(cfg)
	if err != nil {
		return err
	}

	runErr := make(chan error, 1)
	go func() { runErr <- s.Run(ctx) }()
	fmt.Fprintf(out, "subscribed to %s (svID %s, APPID %#04x, %s)\n", cb.SvCBRef, cb.SvID, cb.AppID, cb.Dst)

	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			report(out, sub, &acc, le)
			return nil
		case err := <-runErr:
			if errors.Is(err, sv.ErrClosed) {
				return nil
			}
			return err
		case ev := <-sub.Events():
			switch ev.Kind {
			case sv.EventInvalid:
				// ev.Err is a *sv.ValidationError wrapping the mismatch.
				fmt.Fprintf(out, "invalid ASDU from %s: %v\n", ev.Stream, ev.Err)
			case sv.EventGap:
				fmt.Fprintf(out, "gap: %d samples lost before smpCnt %d\n", ev.Lost, ev.SmpCnt)
			default:
				fmt.Fprintf(out, "%s: %s smpCnt=%d smpSynch=%d\n", ev.Kind, ev.Stream, ev.SmpCnt, ev.SmpSynch)
			}
		case <-tick.C:
			report(out, sub, &acc, le)
		}
	}
}

func report(out io.Writer, sub *sv.Subscription, acc *rms, le bool) {
	for _, st := range sub.Stats() {
		fmt.Fprintf(out, "stats %s: %s, %d frames, %d ASDUs, %d lost, %d duplicates, %d out of order, %.0f samples/s, smpSynch %d\n",
			st.Stream, st.Status, st.Frames, st.ASDUs, st.Lost, st.Duplicates, st.OutOfOrder, st.Rate, st.SmpSynch)
	}
	if !le {
		return
	}
	if v, n := acc.take(); n > 0 {
		fmt.Fprintf(out, "rms over %d samples: IA %.1f IB %.1f IC %.1f IN %.1f A, UA %.0f UB %.0f UC %.0f UN %.0f V\n",
			n, v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7])
	}
}
