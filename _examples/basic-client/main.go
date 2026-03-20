// Command basic-client demonstrates connecting to an IEC 61850 server,
// listing logical devices and nodes, and reading the first browsed
// data attribute.
//
// NOTE: findFirstLeaf is a best-effort heuristic that picks any leaf
// with a single FC. It skips common non-readable attributes but may
// still select control, configuration, or vendor-specific attributes
// that are not readable on a given server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/otfabric/go-iec61850"
)

func main() {
	addr := "127.0.0.1:102"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{
		Logger: logger,
	})
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer client.Close(context.Background())

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		log.Fatalf("list LDs: %v", err)
	}

	for _, ld := range devices {
		fmt.Printf("LD: %s\n", ld.Name)

		nodes, err := client.ListLogicalNodes(ctx, ld.Name)
		if err != nil {
			log.Printf("  list LNs: %v", err)
			continue
		}
		for _, ln := range nodes {
			fmt.Printf("  LN: %s\n", ln.Name)
		}
	}

	if len(devices) == 0 {
		fmt.Println("no logical devices found")
		return
	}

	root, err := client.TreeWithOptions(ctx, iec61850.TreeOptions{
		IncludeFCs: true,
	})
	if err != nil {
		log.Fatalf("tree: %v", err)
	}

	ref, ok := findFirstLeaf(root)
	if !ok {
		fmt.Println("no readable data attributes found in tree")
		return
	}

	val, err := client.Read(ctx, ref)
	if err != nil {
		if errors.Is(err, iec61850.ErrNotFound) || errors.Is(err, iec61850.ErrDataAccess) {
			log.Printf("read %s: %v (attribute may not be readable on this server)", ref, err)
		} else {
			log.Fatalf("read %s: %v", ref, err)
		}
		return
	}
	fmt.Printf("\n%s = %s\n", ref, val)
}

// skipAttributes lists attribute names that are structurally present
// but rarely readable in isolation on many servers.
var skipAttributes = map[string]bool{
	"q": true, "t": true,
	"ctlModel": true, "ctlVal": true, "operTm": true,
}

// findFirstLeaf is a best-effort heuristic: it picks the first tree
// leaf that has exactly one FC and whose name is not in skipAttributes.
// This may still select attributes that are not readable on a given
// server. For production use, filter by FC (e.g. ST or MX) and check
// the attribute type before reading.
func findFirstLeaf(node *iec61850.ModelNode) (iec61850.Ref, bool) {
	if node == nil {
		return iec61850.Ref{}, false
	}
	if len(node.Children) == 0 && node.Reference.LD != "" {
		if skipAttributes[node.Name] {
			return iec61850.Ref{}, false
		}
		if node.FC != "" {
			return node.Reference.WithFC(node.FC), true
		}
		if len(node.FCs) == 1 {
			return node.Reference.WithFC(node.FCs[0]), true
		}
		return iec61850.Ref{}, false
	}
	for _, child := range node.Children {
		if ref, ok := findFirstLeaf(child); ok {
			return ref, true
		}
	}
	return iec61850.Ref{}, false
}
