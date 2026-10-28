// SPDX-License-Identifier: MIT

// Command basic-client connects to an IEC 61850 server, lists its
// logical devices and nodes, and reads the first measurement (FC MX) or
// status (FC ST) attribute it finds.
//
// Usage:
//
//	basic-client [-addr 127.0.0.1:102]
//
// Run it against examples/ied-server or any IEC 61850 server.
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
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := run(ctx, *addr, logger, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr string, logger *slog.Logger, out io.Writer) error {
	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{Logger: logger})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return errors.New("server has no logical devices")
	}
	for _, ld := range devices {
		fmt.Fprintf(out, "LD %s\n", ld.Name)
		nodes, err := client.ListLogicalNodes(ctx, ld.Name)
		if err != nil {
			return err
		}
		for _, ln := range nodes {
			fmt.Fprintf(out, "  LN %s\n", ln.Name)
		}
	}

	root, err := client.TreeWithOptions(ctx, iec61850.TreeOptions{IncludeFCs: true})
	if err != nil {
		return err
	}
	ref, ok := findLeaf(root, "MX")
	if !ok {
		ref, ok = findLeaf(root, "ST")
	}
	if !ok {
		fmt.Fprintln(out, "no MX or ST attribute found")
		return nil
	}
	val, err := client.Read(ctx, ref)
	switch {
	case errors.Is(err, iec61850.ErrDataAccess):
		// The server answered, but refused this attribute.
		fmt.Fprintf(out, "%s: %v\n", ref, err)
	case err != nil:
		return err
	default:
		fmt.Fprintf(out, "%s = %v\n", ref, val)
	}
	return nil
}

// findLeaf returns the first leaf attribute with functional constraint fc.
func findLeaf(n *iec61850.ModelNode, fc iec61850.FunctionalConstraint) (iec61850.Ref, bool) {
	if n == nil {
		return iec61850.Ref{}, false
	}
	if len(n.Children) == 0 && n.FC == fc {
		return n.Reference.WithFC(fc), true
	}
	for _, child := range n.Children {
		if ref, ok := findLeaf(child, fc); ok {
			return ref, true
		}
	}
	return iec61850.Ref{}, false
}
