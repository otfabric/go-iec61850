// SPDX-License-Identifier: MIT

// Command setting-groups walks through the IEC 61850-7-2 setting group
// services from a client.
//
// It reads the setting group control block (SGCB: NumOfSGs, ActSG,
// EditSG, CnfEdit), reads a setting of the active group (FC SG), edits
// that setting in group 2 (SelectEditSG, write FC SE, ConfirmEditSG),
// switches the active group to 2 (SelectActiveSG) and reads the setting
// again.
//
// Usage:
//
//	setting-groups [-addr 127.0.0.1:102] [-ld LD1] [-setting PTOC1.StrVal.setMag.f] [-value 175]
//
// Run it against examples/ied-server, whose demo IED has two setting
// groups with PTOC1.StrVal.setMag.f as a setting
// (examples/internal/demoied).
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
	mms "github.com/otfabric/go-mms"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ld := flag.String("ld", "LD1", "logical device (MMS domain)")
	setting := flag.String("setting", "PTOC1.StrVal.setMag.f", "setting (LN.DO.DA, FLOAT32)")
	value := flag.Float64("value", 175, "new value of the setting in group 2")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := run(ctx, *addr, *ld, *setting, *value, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, ld, setting string, value float64, out io.Writer) error {
	ref, err := iec61850.ParseRef(ld + "/" + setting) // FC SG or SE is added by the helpers
	if err != nil {
		return err
	}

	c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.Background()) }()

	info, err := c.GetSettingGroupInfo(ctx, ld)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "SGCB: %d groups, active %d, editing %d\n", info.NumOfSGs, info.ActSG, info.EditSG)
	if info.NumOfSGs < 2 {
		return fmt.Errorf("%s has %d setting group(s); the example needs 2", ld, info.NumOfSGs)
	}
	if err := printActive(ctx, c, ref, out); err != nil {
		return err
	}

	// Edit group 2: reserve it, write the edit buffer (FC SE), confirm.
	if err := c.SelectEditSG(ctx, ld, 2); err != nil {
		return fmt.Errorf("select group 2 for editing: %w", err)
	}
	old, err := c.GetEditSGValue(ctx, ref)
	if err != nil {
		return err
	}
	if err := c.SetEditSGValue(ctx, ref, mms.NewFloat(value)); err != nil {
		// Leave the edit session by selecting group 0.
		_ = c.SelectEditSG(ctx, ld, 0)
		return fmt.Errorf("write %s[SE]: %w", ref, err)
	}
	if err := c.ConfirmEditSG(ctx, ld); err != nil {
		return fmt.Errorf("confirm: %w", err)
	}
	fmt.Fprintf(out, "group 2: %s %v -> %v\n", setting, old, value)

	// Activate group 2.
	if err := c.SelectActiveSG(ctx, ld, 2); err != nil {
		if errors.Is(err, iec61850.ErrDataAccess) {
			return fmt.Errorf("the server refused group 2: %w", err)
		}
		return err
	}
	if info, err = c.GetSettingGroupInfo(ctx, ld); err != nil {
		return err
	}
	fmt.Fprintf(out, "SGCB: active %d, editing %d\n", info.ActSG, info.EditSG)
	return printActive(ctx, c, ref, out)
}

func printActive(ctx context.Context, c *iec61850.Client, ref iec61850.Ref, out io.Writer) error {
	v, err := c.GetActiveSGValue(ctx, ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "active %s = %v\n", ref, v)
	return nil
}
