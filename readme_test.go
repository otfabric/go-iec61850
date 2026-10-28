// SPDX-License-Identifier: MIT

package iec61850_test

// The functions above the marker line are the README.md quick-start
// snippets, character for character (TestREADMESnippets checks that);
// TestREADMESnippetsRun runs them against the demo IED of the examples.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

func readMeasurement(ctx context.Context, addr string) error {
	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{
		Retry:  mms.DefaultRetryPolicy(), // retry the connection with backoff
		Logger: slog.Default(),
	})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(ctx) }()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		return err
	}
	for _, ld := range devices {
		fmt.Println("LD", ld.Name)
	}

	ref, err := iec61850.ParseRef("LD1/MMXU1.PhV.phsA.cVal.mag.f[MX]")
	if err != nil {
		return err
	}
	val, err := client.Read(ctx, ref)
	if errors.Is(err, iec61850.ErrDataAccess) {
		return fmt.Errorf("the server refused %s: %w", ref, err)
	} else if err != nil {
		return err
	}
	fmt.Println(ref, "=", val)
	return nil
}

func serve(ctx context.Context, doc *scl.SCL, ln *iso.Listener) error {
	model, err := iec61850.NewServerModelFromSCL(doc, "DemoIED", "")
	if err != nil {
		return err
	}
	srv, err := iec61850.NewServer(model, iec61850.ServerOptions{Logger: slog.Default()})
	if err != nil {
		return err
	}
	defer srv.Close()
	srv.EnableReports()

	// Store keys are "<LD>/<MMS item ID>".
	err = srv.RegisterControl("LD1", "GGIO1.SPCSO1", iec61850.CtlModelDirectNormal, iec61850.ControlHandler{
		OnOperate: func(ctx context.Context, req iec61850.ControlRequest) error {
			on, _ := req.CtlVal.Bool()
			srv.SetValue(ctx, "LD1/GGIO1$ST$SPCSO1$stVal", mms.NewBoolean(on))
			return nil
		},
	})
	if err != nil {
		return err
	}
	// SetValue updates the model and drives reports, logs, GOOSE and SV.
	srv.SetValue(ctx, "LD1/MMXU1$MX$PhV$phsA$cVal$mag$f", mms.NewFloat(230.1))

	return srv.ListenAndServe(ctx, ln) // until ctx is done
}

func publishGOOSE(ctx context.Context, tr goose.Transport, doc *scl.SCL) error {
	cb, err := goose.ControlBlockFromSCL(doc, "DemoIED", "LD1", "gcb01")
	if err != nil {
		return err
	}
	cfg, err := cb.PublisherConfig() // address, references, times, initial values
	if err != nil {
		return err
	}
	cfg.Src = tr.HardwareAddr()
	pub, err := goose.NewPublisher(tr, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()
	go func() { _ = pub.Run(ctx) }() // sends, then retransmits from MinTime to MaxTime

	values := cfg.Values
	values[0] = mms.NewBoolean(true)            // GGIO1.Ind1.stVal
	if err := pub.Publish(values); err != nil { // a new state (stNum)
		return err
	}
	<-ctx.Done()
	return nil
}

func subscribeGOOSE(ctx context.Context, tr goose.Transport, doc *scl.SCL) error {
	cb, err := goose.ControlBlockFromSCL(doc, "DemoIED", "LD1", "gcb01")
	if err != nil {
		return err
	}
	sub, err := goose.NewSubscriber(tr, goose.SubscriberConfig{})
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	cfg := cb.SubscriptionConfig() // filters and validation from SCL
	cfg.OnlyStateChanges = true
	s, err := sub.Subscribe(cfg)
	if err != nil {
		return err
	}
	go func() { _ = sub.Run(ctx) }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-s.Events():
			switch ev.Kind {
			case goose.EventMessage:
				fmt.Println("stNum", ev.Frame.PDU.StNum, ev.Frame.PDU.AllData)
			case goose.EventTimeout:
				fmt.Println("timeAllowedToLive expired")
			case goose.EventInvalid:
				fmt.Println("invalid:", ev.Err, errors.Is(ev.Err, goose.ErrConfRevMismatch))
			default: // restarts, sequence gaps, ...
			}
		}
	}
}

func publishSV(ctx context.Context, tr sv.Transport, doc *scl.SCL) error {
	cb, err := sv.ControlBlockFromSCL(doc, "DemoIED", "LD1", "MSVCB01") // 9-2LE, 4000 samples/s
	if err != nil {
		return err
	}
	cfg, err := cb.PublisherConfig()
	if err != nil {
		return err
	}
	cfg.Src = tr.HardwareAddr()
	cfg.Source = func(info sv.SampleInfo, dst []byte) []byte { // called for every sample
		wt := 2 * math.Pi * 50 * float64(info.Index) / 4000
		var s sv.LESample
		s.Current[0].Value = sv.AmpsCount(141.4 * math.Sin(wt))
		s.Voltage[0].Value = sv.VoltsCount(89800 * math.Sin(wt))
		return s.Append(dst)
	}
	pub, err := sv.NewPublisher(tr, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()
	return pub.Run(ctx) // paced on an absolute schedule until ctx is done
}

func subscribeSV(ctx context.Context, tr sv.Transport, doc *scl.SCL) error {
	cb, err := sv.ControlBlockFromSCL(doc, "DemoIED", "LD1", "MSVCB01")
	if err != nil {
		return err
	}
	s, err := sv.NewSubscriber(tr, sv.SubscriberConfig{})
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	cfg := cb.SubscriptionConfig()      // filters and validation from SCL
	cfg.OnASDU = func(smp *sv.Sample) { // on the receive goroutine, without allocation
		var le sv.LESample
		if smp.ASDU.SmpCnt == 0 && sv.DecodeLE(smp.ASDU.Sample, &le) == nil {
			fmt.Printf("IA %.1f A, UA %.0f V\n", sv.Amps(le.Current[0].Value), sv.Volts(le.Voltage[0].Value))
		}
	}
	sub, err := s.Subscribe(cfg)
	if err != nil {
		return err
	}
	go func() {
		for ev := range sub.Events() { // start, gaps, timeouts, synch changes, invalid ASDUs
			fmt.Println(ev.Kind, ev.Stream)
		}
	}()

	err = s.Run(ctx)
	for _, st := range sub.Stats() {
		fmt.Printf("%s: %d ASDUs, %d lost, %.0f samples/s\n", st.Stream, st.ASDUs, st.Lost, st.Rate)
	}
	return err
}

// ---- end of README snippets ----

// readmeSnippets are the snippet functions, in README order.
var readmeSnippets = []string{"readMeasurement", "serve", "publishGOOSE", "subscribeGOOSE", "publishSV", "subscribeSV"}

// TestREADMESnippets checks that every snippet function appears in
// README.md as a Go code block, with tabs as four spaces.
func TestREADMESnippets(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("readme_test.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "readme_test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]string{}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			code := string(src[fset.Position(fd.Pos()).Offset:fset.Position(fd.End()).Offset])
			funcs[fd.Name.Name] = strings.ReplaceAll(code, "\t", "    ")
		}
	}
	blocks := regexp.MustCompile("(?s)```go\n(.*?)```").FindAllSubmatch(readme, -1)
	for _, name := range readmeSnippets {
		want, ok := funcs[name]
		if !ok {
			t.Fatalf("no func %s in readme_test.go", name)
		}
		found := false
		for _, b := range blocks {
			if bytes.Contains(b[1], []byte(want)) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("README.md has no Go block with func %s as in readme_test.go:\n%s", name, want)
		}
	}
}

// TestREADMESnippetsRun runs the snippets against the demo IED: the
// server on loopback with the client, GOOSE and SV on in-memory hubs.
func TestREADMESnippetsRun(t *testing.T) {
	doc, err := scl.ParseFile("examples/internal/demoied/demo.icd")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sctx, scancel := context.WithCancel(ctx)
	served := make(chan error, 1)
	go func() { served <- serve(sctx, doc, ln) }()
	if err := readMeasurement(ctx, ln.Addr().String()); err != nil {
		t.Errorf("readMeasurement: %v", err)
	}
	scancel()
	if err := <-served; err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("serve: %v", err)
	}

	run := func(name string, a, b func(context.Context) error) {
		rctx, rcancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer rcancel()
		errs := make(chan error, 2)
		go func() { errs <- a(rctx) }()
		go func() { errs <- b(rctx) }()
		for range 2 {
			if err := <-errs; err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
	gh := goose.NewHub()
	run("GOOSE",
		func(ctx context.Context) error { return subscribeGOOSE(ctx, gh.NewPort(goose.MAC{}), doc) },
		func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond) // let the subscriber start
			return publishGOOSE(ctx, gh.NewPort(goose.MAC{}), doc)
		})
	if gh.Stats().Delivered == 0 {
		t.Error("GOOSE: nothing delivered")
	}
	sh := sv.NewHub()
	run("SV",
		func(ctx context.Context) error { return subscribeSV(ctx, sh.NewPort(sv.MAC{}), doc) },
		func(ctx context.Context) error { return publishSV(ctx, sh.NewPort(sv.MAC{}), doc) })
	if sh.Stats().Delivered < 100 {
		t.Errorf("SV: %d frames delivered", sh.Stats().Delivered)
	}
}
