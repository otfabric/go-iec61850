// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/otfabric/go-iec61850/internal/mapping"
)

// referenceLDTree is the tree built by rescanning the item list per node
// with the mapping helpers: the one-pass build must give the same tree.
func referenceLDTree(ld string, items []string, opts TreeOptions, rejectUnknownFC bool) []*ModelNode {
	var sub func(node *ModelNode, ln string, basePath []string)
	sub = func(node *ModelNode, ln string, basePath []string) {
		if opts.MaxDepth > 0 && 2+len(basePath) >= opts.MaxDepth {
			return
		}
		for _, name := range mapping.ExtractChildren(items, ln, basePath) {
			childPath := append(append([]string{}, basePath...), name)
			child := &ModelNode{Name: name, Reference: Ref{LD: ld, LN: ln, Path: childPath}}
			if opts.IncludeFCs {
				child.FCs = convertFCs(mapping.ExtractFCsForPath(items, ln, childPath), rejectUnknownFC, nil)
				if len(child.FCs) == 1 {
					child.FC = child.FCs[0]
				}
			}
			sub(child, ln, childPath)
			node.Children = append(node.Children, child)
		}
	}
	var out []*ModelNode
	for _, ln := range mapping.ExtractLogicalNodes(items) {
		lnNode := &ModelNode{Name: ln, Reference: Ref{LD: ld, LN: ln}}
		if opts.IncludeFCs {
			lnNode.FCs = convertFCs(mapping.ExtractFCsForLN(items, ln), rejectUnknownFC, nil)
			if len(lnNode.FCs) == 1 {
				lnNode.FC = lnNode.FCs[0]
			}
		}
		out = append(out, lnNode)
		if opts.MaxDepth > 0 && opts.MaxDepth <= 2 {
			continue
		}
		for _, do := range mapping.ExtractDataObjects(items, ln) {
			doNode := &ModelNode{Name: do, Reference: Ref{LD: ld, LN: ln, Path: []string{do}}}
			if opts.IncludeFCs {
				doNode.FCs = convertFCs(mapping.ExtractFCsForPath(items, ln, []string{do}), rejectUnknownFC, nil)
				if len(doNode.FCs) == 1 {
					doNode.FC = doNode.FCs[0]
				}
			}
			if opts.MaxDepth <= 0 || opts.MaxDepth > 3 {
				sub(doNode, ln, []string{do})
			}
			lnNode.Children = append(lnNode.Children, doNode)
		}
	}
	if out == nil {
		out = []*ModelNode{}
	}
	return out
}

// The one-pass build gives the tree of the per-node rescan for every
// option, with control blocks, the SGCB, unknown FCs, a control block
// name shared with a data object and malformed items.
func TestBuildLDTree_SameAsRescan(t *testing.T) {
	items := []string{
		"LLN0$ST$Mod$stVal", "LLN0$ST$Mod$q", "LLN0$CF$Mod$ctlModel", "LLN0$CO$Mod$Oper$ctlVal",
		"LLN0$BR$brcb01$RptEna", "LLN0$RP$urcb01$RptID", "LLN0$SP$SGCB$ActSG", "LLN0$LG$Log1$LogEna",
		"LLN0$GO$gcb01$GoEna", "LLN0$MS$msv01$SvEna", "LLN0$US$usv01$SvEna", "LLN0$GS$gs$x",
		"LLN0$ST$Beh$stVal", "LLN0$ST$brcb01$stVal", "LLN0$RP$Mod$Shared",
		"MMXU1$MX$TotW$mag$f", "MMXU1$MX$TotW$q", "MMXU1$MX$TotW$t", "MMXU1$DC$TotW$d",
		"MMXU1$XX$Odd$v", "MMXU1$MX", "PlainVariable", "Bad$$x", "$MX$x", "LN$MX$a$$b", "LN$MX$a$b$c$d$e",
		"XCBR1$ST$Pos$stVal", "XCBR1$CO$Pos$Oper$ctlVal", "XCBR1$CO$Pos$Oper$origin$orCat",
	}
	for _, opts := range []TreeOptions{
		{}, {IncludeFCs: true}, {MaxDepth: 1}, {MaxDepth: 2, IncludeFCs: true}, {MaxDepth: 3, IncludeFCs: true},
		{MaxDepth: 4}, {MaxDepth: 5, IncludeFCs: true},
	} {
		for _, reject := range []bool{false, true} {
			got, err := buildLDTree(context.Background(), "LD1", items, opts, reject, nil)
			if err != nil {
				t.Fatal(err)
			}
			if want := referenceLDTree("LD1", items, opts, reject); !reflect.DeepEqual(got, want) {
				t.Errorf("opts %+v reject %v: one-pass tree differs from the rescan", opts, reject)
			}
		}
	}
	if got := buildLDTreeWithOptions("LD1", nil, TreeOptions{}, false, nil); len(got) != 0 {
		t.Errorf("empty list: %d nodes", len(got))
	}
}

// flatItems returns n items under one LN, all at the DO level, as one
// peer can answer GetNameList.
func flatItems(n int) []string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("GGIO1$ST$Ind%07d$stVal", i)
	}
	return items
}

// The build cost of a flat list is linear in its size: four times the
// items take about four times the allocations, not sixteen times.
func TestBuildLDTree_LinearInItems(t *testing.T) {
	allocs := func(n int) float64 {
		items := flatItems(n)
		return testing.AllocsPerRun(1, func() {
			if _, err := buildLDTree(context.Background(), "LD1", items, TreeOptions{IncludeFCs: true}, false, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	small, large := allocs(2000), allocs(8000)
	if ratio := large / small; ratio > 6 {
		t.Errorf("allocations grow %.1fx for 4x the items (%.0f -> %.0f), want linear", ratio, small, large)
	}
}

// The build ends with the context's error, before it starts and while
// it indexes or emits the nodes.
func TestBuildLDTree_ContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := flatItems(3 * treeCtxEvery)
	if _, err := buildLDTree(ctx, "LD1", items, TreeOptions{}, false, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled before the build: err = %v, want context.Canceled", err)
	}

	// Cancelled at the second check, while the DO nodes (2049 items) or
	// the DA nodes below them (1500 items) are emitted.
	for _, n := range []int{2049, 1500} {
		ctx := &cancelAfter{Context: context.Background(), after: 1}
		if _, err := buildLDTree(ctx, "LD1", flatItems(n), TreeOptions{}, false, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("%d items, cancelled during the build: err = %v, want context.Canceled", n, err)
		}
	}
	ctx3 := &cancelAfter{Context: context.Background(), after: 1}
	if _, err := buildLDTree(ctx3, "LD1", []string{"GGIO1$ST$Ind$stVal"}, TreeOptions{}, false, nil); err != nil {
		t.Fatalf("a small list: err = %v", err)
	}
}

// cancelAfter is a context whose Err reports context.Canceled from its
// after+1-th call on.
type cancelAfter struct {
	context.Context
	after, calls int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.after {
		return context.Canceled
	}
	return nil
}

// Tree returns the context's error when the context has ended before
// the build.
func TestTree_ContextEndsDuringBuild(t *testing.T) {
	client := setupLoopback(t)
	client.fetchLDsFn = func(context.Context) ([]LogicalDevice, error) {
		return []LogicalDevice{{Name: "LD1"}}, nil
	}
	client.fetchItemsFn = func(context.Context, string) ([]string, error) { return flatItems(10), nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if tree, err := client.Tree(ctx); !errors.Is(err, context.Canceled) || tree != nil {
		t.Fatalf("tree %v, err %v; want nil, context.Canceled", tree, err)
	}
}

// The item list returned from the fetch is the caller's own copy, as the
// logical device list is: changing it does not change what the fetch
// shared with other callers or stored.
func TestCachedItems_ReturnsCopy(t *testing.T) {
	for _, strategy := range []CacheStrategy{CacheExplicit, CacheLazy} {
		client, _ := setupCachedLoopback(t, strategy)
		shared := []string{"GGIO1$ST$Ind1$stVal"}
		client.fetchItemsFn = func(context.Context, string) ([]string, error) { return shared, nil }
		items, err := client.cachedItems(context.Background(), "LD1")
		if err != nil {
			t.Fatal(err)
		}
		items[0] = "changed"
		if shared[0] != "GGIO1$ST$Ind1$stVal" {
			t.Errorf("strategy %v: the caller's change reached the shared list", strategy)
		}
		if again, _ := client.cachedItems(context.Background(), "LD1"); again[0] != "GGIO1$ST$Ind1$stVal" {
			t.Errorf("strategy %v: the caller's change reached the cache: %v", strategy, again)
		}
	}
}
