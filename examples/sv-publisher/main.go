// SPDX-License-Identifier: MIT

// Command sv-publisher is a software merging unit: it publishes a 9-2LE
// Sampled Values stream with a synthetic three-phase waveform.
//
// The stream (destination MAC, APPID, VLAN, svID, confRev, smpRate,
// noASDU, optional fields) comes from an SCL SampledValueControl. The
// publisher paces samples on an absolute schedule (4000 samples/s for
// 80 samples per period at 50 Hz) and calls a Source function for every
// sample, which fills the eight 9-2LE channels: currents IA, IB, IC,
// IN (1 mA per count) and voltages UA, UB, UC, UN (10 mV per count).
//
// Usage:
//
//	sv-publisher -iface eth0 [-scl station.scd -ied IED1 -ld MU01 -msvcb MSVCB01]
//	             [-freq 50] [-amps 100] [-volts 63500] [-align]
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
	"syscall"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
)

// Waveform is the synthetic signal: balanced three-phase currents and
// voltages (RMS values) at Freq.
type Waveform struct {
	Freq  float64 // Hz
	Amps  float64 // phase current, A RMS
	Volts float64 // phase-to-neutral voltage, V RMS
}

// Sample returns the 9-2LE sample at time t (seconds since the start).
func (w Waveform) Sample(t float64) sv.LESample {
	var s sv.LESample
	for ph := 0; ph < 3; ph++ {
		angle := 2*math.Pi*w.Freq*t - float64(ph)*2*math.Pi/3
		s.Current[ph].Value = sv.AmpsCount(w.Amps * math.Sqrt2 * math.Sin(angle-math.Pi/6)) // 30° lagging
		s.Voltage[ph].Value = sv.VoltsCount(w.Volts * math.Sqrt2 * math.Sin(angle))
	}
	// Neutral: the (zero) sum of the phases; Q stays QualityGood.
	s.Current[3].Value = s.Current[0].Value + s.Current[1].Value + s.Current[2].Value
	s.Voltage[3].Value = s.Voltage[0].Value + s.Voltage[1].Value + s.Voltage[2].Value
	return s
}

func main() {
	iface := flag.String("iface", "", "network interface (required)")
	sclPath := flag.String("scl", "", "SCL file (default: the demo IED)")
	ied := flag.String("ied", demoied.IED, "IED name")
	ld := flag.String("ld", demoied.LD, "LD instance")
	msvcb := flag.String("msvcb", "MSVCB01", "SampledValueControl name")
	freq := flag.Float64("freq", 50, "signal frequency, Hz")
	amps := flag.Float64("amps", 100, "phase current, A RMS")
	volts := flag.Float64("volts", 63500, "phase voltage, V RMS")
	align := flag.Bool("align", false, "start at a whole second with smpCnt 0")
	flag.Parse()
	if *iface == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cb, err := loadControlBlock(*sclPath, *ied, *ld, *msvcb)
	if err != nil {
		log.Fatal(err)
	}
	tr, err := sv.Open(*iface, sv.TransportOptions{})
	if err != nil {
		if errors.Is(err, sv.ErrPermission) {
			log.Fatalf("%v (needs CAP_NET_RAW on Linux, /dev/bpf access on macOS)", err)
		}
		log.Fatal(err)
	}
	defer func() { _ = tr.Close() }()

	w := Waveform{Freq: *freq, Amps: *amps, Volts: *volts}
	if err := run(ctx, tr, cb, w, *align, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func loadControlBlock(path, ied, ld, name string) (*sv.ControlBlock, error) {
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
	cb, err := sv.ControlBlockFromSCL(doc, ied, ld, name)
	if err != nil {
		return nil, err
	}
	if len(cb.Layout) != len(sv.LELayout) {
		return nil, fmt.Errorf("%s: dataset %s is not the 9-2LE layout", cb.SvCBRef, cb.DatSet)
	}
	return cb, nil
}

// run publishes the waveform as cb until ctx is done.
func run(ctx context.Context, tr sv.Transport, cb *sv.ControlBlock, w Waveform, align bool, out io.Writer) error {
	cfg, err := cb.PublisherConfig()
	if err != nil {
		return err
	}
	cfg.Src = tr.HardwareAddr()
	cfg.SmpSynch = sv.SmpSynchLocal // no PTP/1PPS reference here
	cfg.NominalFrequency = w.Freq
	cfg.AlignSecond = align
	cfg.Source = func(info sv.SampleInfo, dst []byte) []byte {
		le := w.Sample(float64(info.Index) / rate(cfg))
		return le.Append(dst)
	}
	pub, err := sv.NewPublisher(tr, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()

	fmt.Fprintf(out, "publishing %s (svID %s, APPID %#04x to %s): %.0f samples/s, %d ASDU per frame\n",
		cb.SvCBRef, cfg.SvID, cfg.AppID, cfg.Dst, rate(cfg), cfg.NoASDU)

	done := make(chan error, 1)
	go func() { done <- pub.Run(ctx) }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			st := pub.State()
			fmt.Fprintf(out, "stopped: %d samples in %d frames, %d skipped\n", st.Samples, st.Frames, st.Skipped)
			if err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		case <-tick.C:
			st := pub.State()
			fmt.Fprintf(out, "%d frames, %.0f samples/s, %d late, %d skipped, %d send errors\n",
				st.Frames, st.Rate, st.Late, st.Skipped, st.SendErrors)
		}
	}
}

// rate is the configured number of samples per second.
func rate(cfg sv.PublisherConfig) float64 {
	return sv.SamplesPerSecond(cfg.SmpRate, cfg.SmpMod, cfg.NominalFrequency)
}
