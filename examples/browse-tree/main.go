// Command browse-tree demonstrates building and printing the IEC 61850
// model tree from a server, optionally with FC annotations.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
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

	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{
		Cache: iec61850.CacheLazy,
	})
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer client.Close(context.Background()) //nolint:errcheck

	tree, err := client.TreeWithOptions(ctx, iec61850.TreeOptions{
		MaxDepth:   5,
		IncludeFCs: true,
	})
	if err != nil {
		log.Fatalf("tree: %v", err)
	}

	printNode(tree, 0)
}

func printNode(node *iec61850.ModelNode, depth int) {
	indent := strings.Repeat("  ", depth)
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
	fmt.Printf("%s%s%s\n", indent, node.Name, fc)
	for _, child := range node.Children {
		printNode(child, depth+1)
	}
}
