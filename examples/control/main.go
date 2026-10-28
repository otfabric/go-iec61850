// SPDX-License-Identifier: MIT

// Command control performs IEC 61850 control operations on a
// controllable data object.
//
// It reads the object's control model (ctlModel) and then operates it
// as that model requires: direct operate, select (SBO) then operate, or
// select-with-value (SBOw) then operate for enhanced security. A failed
// operate is reported with the server's LastApplError (AddCause).
// Without -do it lists the controllable objects of the server.
//
// Usage:
//
//	control [-addr 127.0.0.1:102] [-do LD1/GGIO1.SPCSO1] [-value true]
//
// Run it against examples/ied-server.
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
	do := flag.String("do", "", "controllable data object, LD/LN.DO (default: list them)")
	value := flag.Bool("value", true, "ctlVal")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := run(ctx, *addr, *do, *value, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, do string, value bool, out io.Writer) error {
	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()

	if do == "" {
		return listControls(ctx, client, out)
	}
	ref, err := iec61850.ParseRef(do)
	if err != nil {
		return err
	}
	ref.FC = "" // the control services choose the FC (CO, ST, CF)

	ctlModel, err := client.ReadCtlModel(ctx, ref)
	if err != nil {
		return fmt.Errorf("read ctlModel of %s: %w", ref, err)
	}
	fmt.Fprintf(out, "%s: ctlModel %s\n", ref, ctlModel)
	if !ctlModel.IsControllable() {
		return fmt.Errorf("%s is status-only", ref)
	}

	params := iec61850.OperateParams{CtlVal: iec61850.BoolCtlVal(value)}
	switch {
	case ctlModel.IsSBO() && ctlModel.IsEnhanced():
		if err := client.SelectWithValue(ctx, ref, params); err != nil {
			return report(ctx, client, ref, "select-with-value", err, out)
		}
		fmt.Fprintln(out, "selected (SBOw)")
	case ctlModel.IsSBO():
		if _, err := client.Select(ctx, ref); err != nil {
			return report(ctx, client, ref, "select", err, out)
		}
		fmt.Fprintln(out, "selected (SBO)")
	}
	if err := client.Operate(ctx, ref, params); err != nil {
		return report(ctx, client, ref, "operate", err, out)
	}
	fmt.Fprintf(out, "operated: ctlVal=%v\n", value)
	return nil
}

// report explains a failed control service: the error class, and the
// server's LastApplError when it sent one.
func report(ctx context.Context, client *iec61850.Client, ref iec61850.Ref, op string, err error, out io.Writer) error {
	var ce *iec61850.ControlError
	if errors.As(err, &ce) {
		fmt.Fprintf(out, "%s refused: AddCause %s\n", op, ce.AddCause)
	}
	if lae, lerr := client.ReadLastApplError(ctx, ref); lerr == nil {
		fmt.Fprintf(out, "LastApplError: error=%d AddCause=%s\n", lae.Error, lae.AddCause)
	}
	return fmt.Errorf("%s %s: %w", op, ref, err)
}

// listControls prints the data objects with a controllable ctlModel.
func listControls(ctx context.Context, client *iec61850.Client, out io.Writer) error {
	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		return err
	}
	for _, ld := range devices {
		nodes, err := client.ListLogicalNodes(ctx, ld.Name)
		if err != nil {
			return err
		}
		for _, ln := range nodes {
			dos, err := client.ListDataObjects(ctx, ld.Name, ln.Name)
			if err != nil {
				return err
			}
			for _, do := range dos {
				ref := iec61850.Ref{LD: ld.Name, LN: ln.Name, Path: []string{do.Name}}
				ctlModel, err := client.ReadCtlModel(ctx, ref)
				if err != nil {
					continue // no ctlModel: not a control object
				}
				if ctlModel.IsControllable() {
					fmt.Fprintf(out, "%s ctlModel=%s\n", ref, ctlModel)
				}
			}
		}
	}
	return nil
}
