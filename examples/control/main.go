// SPDX-License-Identifier: MIT

// Command control demonstrates IEC 61850 control operations:
// reading the control model, performing direct-operate, and
// select-before-operate workflows.
//
// Usage:
//
//	go run . [addr] [LD/LN.DO]
//
// Example:
//
//	go run . 127.0.0.1:102 simpleIOGenericIO/GGIO1.SPCSO1
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/otfabric/go-iec61850"
)

func main() {
	addr := "127.0.0.1:102"
	controlDO := ""

	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	if len(os.Args) > 2 {
		controlDO = os.Args[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer client.Close(context.Background()) //nolint:errcheck

	if controlDO == "" {
		fmt.Println("No control DO specified. Browsing for controllable objects...")
		browseControls(ctx, client)
		return
	}

	ref, err := iec61850.ParseRef(controlDO)
	if err != nil {
		log.Fatalf("parse ref: %v", err)
	}
	ref.FC = "" // control APIs manage FC automatically

	ctlModel, err := client.ReadCtlModel(ctx, ref)
	if err != nil {
		if errors.Is(err, iec61850.ErrNotFound) || errors.Is(err, iec61850.ErrDataAccess) {
			log.Printf("read ctlModel %s: %v (object may not be controllable)", ref, err)
			return
		}
		log.Fatalf("read ctlModel: %v", err)
	}
	fmt.Printf("Control model for %s: %s\n", ref, ctlModel)

	if !ctlModel.IsControllable() {
		fmt.Println("Object is status-only, not controllable.")
		return
	}

	switch {
	case ctlModel.IsSBO() && ctlModel.IsEnhanced():
		fmt.Println("\nPerforming select-with-value (SBOw) + operate...")
		params := iec61850.OperateParams{
			CtlVal: iec61850.BoolCtlVal(true),
		}
		if err := client.SelectWithValue(ctx, ref, params); err != nil {
			log.Printf("select-with-value: %v", err)
			return
		}
		fmt.Println("  Select accepted.")

		if err := client.Operate(ctx, ref, params); err != nil {
			log.Printf("operate: %v", err)
			return
		}
		fmt.Println("  Operate accepted.")

	case ctlModel.IsSBO():
		fmt.Println("\nPerforming select (SBO) + operate...")
		selected, err := client.Select(ctx, ref)
		if err != nil {
			log.Printf("select: %v", err)
			return
		}
		fmt.Printf("  Selected: %s\n", selected)

		if err := client.Operate(ctx, ref, iec61850.OperateParams{
			CtlVal: iec61850.BoolCtlVal(true),
		}); err != nil {
			log.Printf("operate: %v", err)
			return
		}
		fmt.Println("  Operate accepted.")

	default:
		fmt.Println("\nPerforming direct-operate...")
		if err := client.Operate(ctx, ref, iec61850.OperateParams{
			CtlVal: iec61850.BoolCtlVal(true),
		}); err != nil {
			log.Printf("operate: %v", err)
			readLastError(ctx, client, ref)
			return
		}
		fmt.Println("  Operate accepted.")
	}
}

func browseControls(ctx context.Context, client *iec61850.Client) {
	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		log.Fatalf("list LDs: %v", err)
	}

	for _, ld := range devices {
		nodes, err := client.ListLogicalNodes(ctx, ld.Name)
		if err != nil {
			log.Printf("  list LNs for %s: %v", ld.Name, err)
			continue
		}
		for _, ln := range nodes {
			dos, err := client.ListDataObjects(ctx, ld.Name, ln.Name)
			if err != nil {
				continue
			}
			for _, do := range dos {
				ref := iec61850.Ref{LD: ld.Name, LN: ln.Name, Path: []string{do.Name}}
				ctlModel, err := client.ReadCtlModel(ctx, ref)
				if err != nil {
					continue
				}
				if ctlModel.IsControllable() {
					fmt.Printf("  %s/%s.%s  ctlModel=%s\n", ld.Name, ln.Name, do.Name, ctlModel)
				}
			}
		}
	}
}

func readLastError(ctx context.Context, client *iec61850.Client, ref iec61850.Ref) {
	lae, err := client.ReadLastApplError(ctx, ref)
	if err != nil {
		return
	}
	fmt.Printf("  LastApplError: obj=%s error=%d cause=%s origin=%s\n",
		lae.CntrlObj, lae.Error, lae.AddCause, lae.Origin.OrCat)
}
