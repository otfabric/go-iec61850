// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	mms "github.com/otfabric/go-mms"
)

// oddItemsClient connects to an MMS server whose single domain contains a
// standard item, an item with a non-standard FC and a malformed item ID
// (empty FC segment), returning a client with the given options and a
// counter of item-list fetches.
func oddItemsClient(t *testing.T, opts ClientOptions) (*Client, *atomic.Int64) {
	t.Helper()
	ctx := context.Background()
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain("LDX"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"LLN0$ST$Mod$stVal", "GGIO1$ZZ$Odd$stVal", "GGIO1$$bad"} {
		ts := mms.TypeSpec{Type: mms.ValueTypeBoolean}
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LDX", ItemID: mms.ItemID(item)},
			TypeSpec: ts,
			Read:     func(context.Context) (*mms.Value, error) { return ts.DefaultValue(), nil },
		}); err != nil {
			t.Fatalf("register %q: %v", item, err)
		}
	}
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	c, err := NewClient(mmsClient, opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var fetches atomic.Int64
	c.fetchItemsFn = func(ctx2 context.Context, ld string) ([]string, error) {
		fetches.Add(1)
		return c.fetchItems(ctx2, ld)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })
	return c, &fetches
}

func refStrings(refs []Ref) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}

func TestFindPaths_SkipsMalformedItemsWhenLenient(t *testing.T) {
	c, _ := oddItemsClient(t, ClientOptions{})
	refs, err := c.FindPaths(context.Background(), FindQuery{Pattern: "LDX/*"})
	if err != nil {
		t.Fatalf("FindPaths: %v", err)
	}
	got := refStrings(refs)
	want := []string{"LDX/GGIO1.Odd.stVal[ZZ]", "LDX/LLN0.Mod.stVal[ST]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestFindPaths_StrictRejectsMalformedItems(t *testing.T) {
	c, _ := oddItemsClient(t, ClientOptions{Strictness: StrictnessOptions{RejectUnknownFC: true}})
	_, err := c.FindPaths(context.Background(), FindQuery{Pattern: "*"})
	if err == nil {
		t.Fatal("expected error for malformed item in strict mode")
	}
	if !strings.Contains(err.Error(), "empty functional constraint") {
		t.Errorf("error = %v", err)
	}
}

func TestFindPaths_LazyCacheReusesParsedRefs(t *testing.T) {
	c, fetches := oddItemsClient(t, ClientOptions{Cache: CacheLazy})
	ctx := context.Background()

	first, err := c.FindPaths(ctx, FindQuery{Pattern: "LDX/LLN0*"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.FindPaths(ctx, FindQuery{Pattern: "LDX/GGIO1*", FC: "ZZ"})
	if err != nil {
		t.Fatal(err)
	}
	if got := refStrings(first); !reflect.DeepEqual(got, []string{"LDX/LLN0.Mod.stVal[ST]"}) {
		t.Errorf("first = %v", got)
	}
	if got := refStrings(second); !reflect.DeepEqual(got, []string{"LDX/GGIO1.Odd.stVal[ZZ]"}) {
		t.Errorf("second = %v", got)
	}
	if n := fetches.Load(); n != 1 {
		t.Errorf("item list fetched %d times, want 1 (parsed refs should be cached)", n)
	}

	// Filtering by a different LD skips the domain entirely.
	none, err := c.FindPaths(ctx, FindQuery{Pattern: "*", LDFilter: "OTHER"})
	if err != nil || len(none) != 0 {
		t.Errorf("LDFilter=OTHER: %v, %v", none, err)
	}
}

func TestConvertFCs(t *testing.T) {
	in := []string{"ST", "ZZ", "MX"}

	lenient := convertFCs(in, false, nil)
	if want := []FunctionalConstraint{"ST", "ZZ", "MX"}; !reflect.DeepEqual(lenient, want) {
		t.Errorf("lenient = %v, want %v", lenient, want)
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	strict := convertFCs(in, true, logger)
	if want := []FunctionalConstraint{FCST, FCMX}; !reflect.DeepEqual(strict, want) {
		t.Errorf("strict = %v, want %v", strict, want)
	}
	if !strings.Contains(logBuf.String(), "dropping unknown FC") || !strings.Contains(logBuf.String(), "fc=ZZ") {
		t.Errorf("expected debug log for dropped FC, got %q", logBuf.String())
	}

	// A nil logger must be tolerated in strict mode.
	if got := convertFCs([]string{"ZZ"}, true, nil); len(got) != 0 {
		t.Errorf("strict with nil logger = %v, want empty", got)
	}
	if got := convertFCs(nil, false, nil); len(got) != 0 {
		t.Errorf("nil input = %v", got)
	}
}

func TestTreeWithOptions_StrictDropsUnknownFCs(t *testing.T) {
	items := []string{"GGIO1$ST$Ind$stVal", "GGIO1$ZZ$Ind$x"}
	nodes := buildLDTreeWithOptions("LD1", items, TreeOptions{IncludeFCs: true}, true, nil)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 LN", len(nodes))
	}
	if !reflect.DeepEqual(nodes[0].FCs, []FunctionalConstraint{FCST}) || nodes[0].FC != FCST {
		t.Errorf("LN FCs = %v (FC=%q), want only ST", nodes[0].FCs, nodes[0].FC)
	}

	lenient := buildLDTreeWithOptions("LD1", items, TreeOptions{IncludeFCs: true}, false, nil)
	if len(lenient[0].FCs) != 2 || lenient[0].FC != "" {
		t.Errorf("lenient LN FCs = %v (FC=%q), want ST and ZZ with no single FC", lenient[0].FCs, lenient[0].FC)
	}
}
