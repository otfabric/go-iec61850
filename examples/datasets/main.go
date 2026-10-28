// SPDX-License-Identifier: MIT

// Command datasets works with IEC 61850 data sets (MMS named variable
// lists) on a server.
//
// It lists the data sets of a logical device, reads the directory and
// the values of each, creates a data set at run time from two
// attributes, reads it in one request and deletes it again. Members
// that cannot be read come back with a per-member error instead of
// failing the whole read.
//
// Usage:
//
//	datasets [-addr 127.0.0.1:102] [-ld LD1]
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
	mms "github.com/otfabric/go-mms"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	ld := flag.String("ld", "LD1", "logical device (MMS domain)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := run(ctx, *addr, *ld, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, ld string, out io.Writer) error {
	c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.Background()) }()

	names, err := c.ListDataSets(ctx, ld)
	if err != nil {
		return err
	}
	for _, name := range names {
		ds, err := c.GetDataSet(ctx, ld, name)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s (%d members, deletable=%v)\n", ds.Reference, len(ds.Members), ds.Deletable)
		if err := printValues(ctx, c, ld, name, out); err != nil {
			return err
		}
	}

	// Create a data set at run time. Its members are functionally
	// constrained data attributes (FCDA) of the same server.
	const dyn = "LLN0$dsExample"
	var members []iec61850.DataSetMember
	for _, s := range []string{ld + "/GGIO1.Ind1.stVal[ST]", ld + "/MMXU1.PhV.phsA.cVal.mag.f[MX]"} {
		ref, err := iec61850.ParseRef(s)
		if err != nil {
			return err
		}
		members = append(members, iec61850.DataSetMember{Ref: ref})
	}
	if err := c.CreateDataSet(ctx, ld, dyn, members); err != nil {
		return fmt.Errorf("create %s: %w", dyn, err)
	}
	fmt.Fprintf(out, "created %s/%s\n", ld, dyn)
	if err := printValues(ctx, c, ld, dyn, out); err != nil {
		return err
	}
	if err := c.DeleteDataSet(ctx, ld, dyn); err != nil {
		return fmt.Errorf("delete %s: %w", dyn, err)
	}
	fmt.Fprintf(out, "deleted %s/%s\n", ld, dyn)

	// The data set is gone now: GetDataSet wraps iec61850.ErrNotFound
	// (and the server's *mms.ServiceError, class access,
	// object-non-existent).
	_, err = c.GetDataSet(ctx, ld, dyn)
	var se *mms.ServiceError
	switch {
	case errors.Is(err, iec61850.ErrNotFound) && errors.As(err, &se):
		fmt.Fprintf(out, "%s/%s: not found (%s), as expected\n", ld, dyn, se.CodeName())
	case err != nil:
		return err
	default:
		return fmt.Errorf("%s/%s still exists after delete", ld, dyn)
	}
	return nil
}

func printValues(ctx context.Context, c *iec61850.Client, ld, name string, out io.Writer) error {
	vals, err := c.ReadDataSet(ctx, ld, name)
	if err != nil {
		return err
	}
	for _, v := range vals {
		if v.Err != nil {
			fmt.Fprintf(out, "  %s: %v\n", v.Member.Ref, v.Err)
			continue
		}
		fmt.Fprintf(out, "  %s = %v\n", v.Member.Ref, v.Value)
	}
	return nil
}
