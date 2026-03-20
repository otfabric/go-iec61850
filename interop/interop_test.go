//go:build interop

package interop

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850"
)

func serverAddr() string {
	if addr := os.Getenv("IEC61850_INTEROP_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:102"
}

func skipIfDisabled(t *testing.T) {
	t.Helper()
	if os.Getenv("IEC61850_INTEROP_SKIP") == "1" {
		t.Skip("IEC61850_INTEROP_SKIP=1")
	}
}

func dialTestServer(t *testing.T) *iec61850.Client {
	t.Helper()
	skipIfDisabled(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := iec61850.Dial(ctx, serverAddr(), iec61850.DialOptions{})
	if err != nil {
		t.Fatalf("dial %s: %v", serverAddr(), err)
	}
	t.Cleanup(func() {
		_ = client.Close(context.Background())
	})
	return client
}

func TestInterop_BrowseLogicalDevices(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("expected at least one logical device")
	}
	for _, ld := range devices {
		t.Logf("LD: %s", ld.Name)
	}
}

func TestInterop_BrowseLogicalNodes(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("expected at least one logical device")
	}

	ld := devices[0].Name
	nodes, err := client.ListLogicalNodes(ctx, ld)
	if err != nil {
		t.Fatalf("ListLogicalNodes(%q): %v", ld, err)
	}
	if len(nodes) == 0 {
		t.Fatalf("expected at least one logical node in %q", ld)
	}

	hasLLN0 := false
	for _, ln := range nodes {
		t.Logf("  LN: %s", ln.Name)
		if ln.Name == "LLN0" {
			hasLLN0 = true
		}
	}
	if !hasLLN0 {
		t.Errorf("expected LLN0 in %q", ld)
	}
}

func TestInterop_ReadSingleValue(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("ListLogicalDevices: %v (count=%d)", err, len(devices))
	}

	ref, err := iec61850.ParseRef(devices[0].Name + "/LLN0.Mod.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	val, err := client.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read(%s): %v", ref, err)
	}
	t.Logf("Read %s = %s", ref, val)
}

func TestInterop_WriteSingleValue(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("ListLogicalDevices: %v (count=%d)", err, len(devices))
	}

	ld := devices[0].Name

	// Try writing a common writable attribute. This may fail on
	// read-only servers — the test records the result.
	ref, err := iec61850.ParseRef(ld + "/LLN0.Mod.ctlVal[CO]")
	if err != nil {
		t.Skipf("ParseRef: %v (server may not have this attribute)", err)
	}

	writeErr := client.Write(ctx, ref, mms.NewBoolean(true))
	t.Logf("Write %s result: %v", ref, writeErr)
}

func TestInterop_ListDataSets(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("ListLogicalDevices: %v", err)
	}

	ld := devices[0].Name
	names, err := client.ListDataSets(ctx, ld)
	if err != nil {
		t.Fatalf("ListDataSets(%q): %v", ld, err)
	}
	t.Logf("DataSets in %q: %d", ld, len(names))
	for _, name := range names {
		t.Logf("  DS: %s", name)
	}
}

func TestInterop_ReadDataSet(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("ListLogicalDevices: %v", err)
	}

	ld := devices[0].Name
	names, err := client.ListDataSets(ctx, ld)
	if err != nil || len(names) == 0 {
		t.Skipf("no datasets in %q: %v", ld, err)
	}

	results, err := client.ReadDataSet(ctx, ld, names[0])
	if err != nil {
		t.Fatalf("ReadDataSet(%q, %q): %v", ld, names[0], err)
	}
	t.Logf("ReadDataSet %q/%q: %d values", ld, names[0], len(results))
	for i, r := range results {
		if r.Err != nil {
			t.Logf("  [%d] error: %v", i, r.Err)
		} else {
			t.Logf("  [%d] %s = %s", i, r.Member.ItemID, r.Value)
		}
	}
}

func TestInterop_ListReports(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("ListLogicalDevices: %v", err)
	}

	ld := devices[0].Name
	rcbs, err := client.ListReports(ctx, ld)
	if err != nil {
		t.Fatalf("ListReports(%q): %v", ld, err)
	}
	t.Logf("Reports in %q: %d", ld, len(rcbs))
	for _, name := range rcbs {
		t.Logf("  RCB: %s", name)
	}
}

func TestInterop_GetReportControlBlock(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	devices, err := client.ListLogicalDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("ListLogicalDevices: %v", err)
	}

	ld := devices[0].Name
	rcbs, err := client.ListReports(ctx, ld)
	if err != nil || len(rcbs) == 0 {
		t.Skipf("no RCBs in %q: %v", ld, err)
	}

	rcb, err := client.GetReportControlBlock(ctx, ld, rcbs[0])
	if err != nil {
		t.Fatalf("GetReportControlBlock(%q, %q): %v", ld, rcbs[0], err)
	}
	t.Logf("RCB: ref=%s type=%s rptID=%s datSet=%s confRev=%d",
		rcb.Reference, rcb.Type, rcb.RptID, rcb.DatSet, rcb.ConfRev)
	t.Logf("  OptFlds=%s TrgOps=%s BufTm=%d IntgPd=%d",
		rcb.OptFlds, rcb.TrgOps, rcb.BufTm, rcb.IntgPd)
}

func TestInterop_ListFiles(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	files, err := client.ListFiles(ctx, "/")
	if err != nil {
		t.Logf("ListFiles: %v (server may not support files)", err)
		return
	}
	t.Logf("Files: %d", len(files))
	for _, f := range files {
		t.Logf("  %s (%d bytes)", f.Name, f.Size)
	}
}

func TestInterop_TreeBrowse(t *testing.T) {
	client := dialTestServer(t)
	ctx := context.Background()

	root, err := client.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	count := 0
	for _, ld := range root.Children {
		for _, ln := range ld.Children {
			count += len(ln.Children)
		}
	}
	t.Logf("Tree: %d LDs, %d total data objects", len(root.Children), count)

	if len(root.Children) == 0 {
		t.Error("expected at least one LD in tree")
	}
}
