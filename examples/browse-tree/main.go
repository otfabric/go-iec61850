// SPDX-License-Identifier: MIT

// Command browse-tree prints the IEC 61850 model tree of a server
// (logical devices, logical nodes, data objects and attributes) with
// their functional constraints.
//
// Usage:
//
//	browse-tree [-addr 127.0.0.1:102] [-depth 5] [-ld LD1]
//
// Run it against examples/ied-server or any IEC 61850 server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	depth := flag.Int("depth", 5, "maximum depth (0 = unlimited)")
	ld := flag.String("ld", "", "only this logical device")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := run(ctx, *addr, iec61850.TreeOptions{MaxDepth: *depth, LDFilter: *ld, IncludeFCs: true}, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr string, opts iec61850.TreeOptions, out io.Writer) error {
	// CacheLazy keeps the discovered model, so a later read of the same
	// node does not query the server again.
	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{Cache: iec61850.CacheLazy})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()

	tree, err := client.TreeWithOptions(ctx, opts)
	if err != nil {
		return err
	}
	printNode(out, tree, 0)
	return nil
}

func printNode(out io.Writer, node *iec61850.ModelNode, depth int) {
	fc := ""
	if node.FC != "" {
		fc = fmt.Sprintf(" [%s]", node.FC)
	} else if len(node.FCs) > 0 {
		fcs := make([]string, len(node.FCs))
		for i, f := range node.FCs {
			fcs[i] = string(f)
		}
		fc = fmt.Sprintf(" [%s]", strings.Join(fcs, ","))
	}
	fmt.Fprintf(out, "%s%s%s\n", strings.Repeat("  ", depth), node.Name, fc)
	for _, child := range node.Children {
		printNode(out, child, depth+1)
	}
}
