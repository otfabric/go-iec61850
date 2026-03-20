package iec61850

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/otfabric/go-mms"
)

// chanTransport is a channel-based in-process MMS transport for testing.
type chanTransport struct {
	send chan []byte
	recv chan []byte

	mu     sync.Mutex
	closed bool
}

func (t *chanTransport) Send(_ context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	t.mu.Unlock()

	cp := make([]byte, len(data))
	copy(cp, data)
	t.send <- cp
	return nil
}

func (t *chanTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-t.recv:
		if data == nil {
			return nil, errors.New("transport closed")
		}
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *chanTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.send)
	}
	return nil
}

func loopbackPair() (clientTransport, serverTransport mms.Transport) {
	c2s := make(chan []byte, 16)
	s2c := make(chan []byte, 16)
	cl := &chanTransport{send: c2s, recv: s2c}
	sr := &chanTransport{send: s2c, recv: c2s}
	return cl, sr
}

// iec61850Variables defines a realistic IEC 61850 MMS variable model
// for test purposes.
var iec61850Variables = map[string][]struct {
	itemID   string
	typeSpec mms.TypeSpec
}{
	"simpleIOGenericIO": {
		{itemID: "LLN0$ST$Mod$stVal", typeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}},
		{itemID: "LLN0$ST$Mod$q", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		{itemID: "LLN0$ST$Mod$t", typeSpec: mms.TypeSpec{Type: mms.ValueTypeUTCTime}},
		{itemID: "LLN0$ST$Beh$stVal", typeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}},
		{itemID: "LLN0$ST$Beh$q", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		{itemID: "LLN0$DC$NamPlt$vendor", typeSpec: mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 64}},
		{itemID: "LLN0$DC$NamPlt$swRev", typeSpec: mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 64}},
		{itemID: "GGIO1$ST$Ind1$stVal", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBoolean}},
		{itemID: "GGIO1$ST$Ind1$q", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		{itemID: "GGIO1$ST$Ind1$t", typeSpec: mms.TypeSpec{Type: mms.ValueTypeUTCTime}},
		{itemID: "GGIO1$ST$Ind2$stVal", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBoolean}},
		{itemID: "GGIO1$ST$Ind2$q", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		{itemID: "GGIO1$ST$Ind2$t", typeSpec: mms.TypeSpec{Type: mms.ValueTypeUTCTime}},
		{itemID: "GGIO1$ST$SPCSO1$stVal", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBoolean}},
		{itemID: "GGIO1$CO$SPCSO1$Oper$ctlVal", typeSpec: mms.TypeSpec{Type: mms.ValueTypeBoolean}},
	},
}

func setupTestServer(t *testing.T) *mms.Server {
	t.Helper()
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	for domain, vars := range iec61850Variables {
		if err := srv.RegisterDomain(domain); err != nil {
			t.Fatalf("register domain %q: %v", domain, err)
		}
		for _, v := range vars {
			if err := srv.RegisterVariable(mms.Variable{
				Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(v.itemID)},
				TypeSpec: v.typeSpec,
				Read: func(_ context.Context) (*mms.Value, error) {
					return v.typeSpec.DefaultValue(), nil
				},
			}); err != nil {
				t.Fatalf("register variable %q: %v", v.itemID, err)
			}
		}
	}

	return srv
}

func setupLoopback(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()

	srv := setupTestServer(t)
	clientT, serverT := loopbackPair()

	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client
}

func TestListLogicalDevices(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	lds, err := client.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}

	if len(lds) != 1 {
		t.Fatalf("got %d LDs, want 1", len(lds))
	}
	if lds[0].Name != "simpleIOGenericIO" {
		t.Errorf("LD name = %q, want %q", lds[0].Name, "simpleIOGenericIO")
	}
}

func TestListLogicalNodes(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	lns, err := client.ListLogicalNodes(ctx, "simpleIOGenericIO")
	if err != nil {
		t.Fatalf("ListLogicalNodes: %v", err)
	}

	if len(lns) != 2 {
		t.Fatalf("got %d LNs, want 2", len(lns))
	}

	names := make([]string, len(lns))
	for i, ln := range lns {
		names[i] = ln.Name
	}
	if names[0] != "GGIO1" || names[1] != "LLN0" {
		t.Errorf("LN names = %v, want [GGIO1 LLN0]", names)
	}

	for _, ln := range lns {
		if ln.LD != "simpleIOGenericIO" {
			t.Errorf("LN %q LD = %q, want %q", ln.Name, ln.LD, "simpleIOGenericIO")
		}
	}
}

func TestListLogicalNodes_EmptyLD(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.ListLogicalNodes(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty LD")
	}
}

func TestListDataObjects(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	dos, err := client.ListDataObjects(ctx, "simpleIOGenericIO", "LLN0")
	if err != nil {
		t.Fatalf("ListDataObjects: %v", err)
	}

	if len(dos) != 3 {
		t.Fatalf("got %d DOs, want 3 (Beh, Mod, NamPlt)", len(dos))
	}

	names := make([]string, len(dos))
	for i, do := range dos {
		names[i] = do.Name
	}
	if names[0] != "Beh" || names[1] != "Mod" || names[2] != "NamPlt" {
		t.Errorf("DO names = %v, want [Beh Mod NamPlt]", names)
	}
}

func TestListDataObjects_GGIO1(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	dos, err := client.ListDataObjects(ctx, "simpleIOGenericIO", "GGIO1")
	if err != nil {
		t.Fatalf("ListDataObjects: %v", err)
	}

	if len(dos) != 3 {
		t.Fatalf("got %d DOs, want 3 (Ind1, Ind2, SPCSO1)", len(dos))
	}
}

func TestListChildren(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod"}}
	das, err := client.ListChildren(ctx, ref)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}

	if len(das) != 3 {
		t.Fatalf("got %d children, want 3 (q, stVal, t)", len(das))
	}

	names := make([]string, len(das))
	for i, da := range das {
		names[i] = da.Name
	}
	if names[0] != "q" || names[1] != "stVal" || names[2] != "t" {
		t.Errorf("DA names = %v, want [q stVal t]", names)
	}
}

func TestListChildren_EmptyRef(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.ListChildren(ctx, Ref{})
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestTree(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	if tree.Name != "root" {
		t.Errorf("root name = %q, want %q", tree.Name, "root")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root has %d children, want 1 (simpleIOGenericIO)", len(tree.Children))
	}

	ldNode := tree.Children[0]
	if ldNode.Name != "simpleIOGenericIO" {
		t.Errorf("LD name = %q", ldNode.Name)
	}
	if len(ldNode.Children) != 2 {
		t.Fatalf("LD has %d LN children, want 2", len(ldNode.Children))
	}

	var lln0Node *ModelNode
	for _, ln := range ldNode.Children {
		if ln.Name == "LLN0" {
			lln0Node = ln
			break
		}
	}
	if lln0Node == nil {
		t.Fatal("LLN0 not found in tree")
	}
	if len(lln0Node.Children) != 3 {
		t.Fatalf("LLN0 has %d DO children, want 3", len(lln0Node.Children))
	}

	var modNode *ModelNode
	for _, do := range lln0Node.Children {
		if do.Name == "Mod" {
			modNode = do
			break
		}
	}
	if modNode == nil {
		t.Fatal("Mod not found under LLN0")
	}
	if len(modNode.Children) != 3 {
		t.Fatalf("Mod has %d DA children, want 3 (q, stVal, t)", len(modNode.Children))
	}
}

func TestFindPaths(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	results, err := client.FindPaths(ctx, FindQuery{
		Pattern: "simpleIOGenericIO/GGIO1.Ind*",
	})
	if err != nil {
		t.Fatalf("FindPaths: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("FindPaths returned no results")
	}

	for _, ref := range results {
		if ref.LD != "simpleIOGenericIO" || ref.LN != "GGIO1" {
			t.Errorf("unexpected ref: %s", ref.String())
		}
		if len(ref.Path) == 0 || (ref.Path[0] != "Ind1" && ref.Path[0] != "Ind2") {
			t.Errorf("unexpected path: %v", ref.Path)
		}
	}
}

func TestFindPaths_WithFC(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	results, err := client.FindPaths(ctx, FindQuery{
		Pattern: "simpleIOGenericIO/LLN0.*",
		FC:      FCDC,
	})
	if err != nil {
		t.Fatalf("FindPaths: %v", err)
	}

	for _, ref := range results {
		if ref.FC != FCDC {
			t.Errorf("expected FC=DC, got %q for %s", ref.FC, ref.String())
		}
	}

	if len(results) == 0 {
		t.Error("expected at least one DC result for LLN0")
	}
}

func TestFindPaths_EmptyPattern(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.FindPaths(ctx, FindQuery{})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestLogicalNode_Ref(t *testing.T) {
	ln := LogicalNode{Name: "LLN0", LD: "LD1"}
	ref := ln.Ref()
	if ref.LD != "LD1" || ref.LN != "LLN0" {
		t.Errorf("Ref() = %+v", ref)
	}
}

func TestGetVariableType(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	ts, err := client.GetVariableType(ctx, ref)
	if err != nil {
		t.Fatalf("GetVariableType: %v", err)
	}
	if ts.Type != mms.ValueTypeInteger {
		t.Errorf("type = %v, want Integer", ts.Type)
	}
}

func TestGetVariableType_NotObject(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", FC: FCST}
	_, err := client.GetVariableType(ctx, ref)
	if err == nil {
		t.Fatal("expected error for non-object ref (no path)")
	}
	var refErr *ReferenceError
	if !errors.As(err, &refErr) {
		t.Errorf("expected ReferenceError, got %T: %v", err, err)
	}
}

func TestGetVariableType_NoFC(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}}
	_, err := client.GetVariableType(ctx, ref)
	if err == nil {
		t.Fatal("expected error for ref without FC")
	}
	var refErr *ReferenceError
	if !errors.As(err, &refErr) {
		t.Errorf("expected ReferenceError, got %T: %v", err, err)
	}
}

func TestFindPaths_InvalidPattern(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.FindPaths(ctx, FindQuery{Pattern: "["})
	if err == nil {
		t.Fatal("expected error for invalid glob pattern")
	}
}

func TestFindPaths_Regex(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	results, err := client.FindPaths(ctx, FindQuery{
		Pattern:   `.*GGIO1\.Ind[12]\.stVal`,
		MatchMode: MatchRegex,
	})
	if err != nil {
		t.Fatalf("FindPaths regex: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, ref := range results {
		if ref.LN != "GGIO1" {
			t.Errorf("unexpected LN: %s", ref.LN)
		}
		last := ref.Path[len(ref.Path)-1]
		if last != "stVal" {
			t.Errorf("expected stVal, got %s", last)
		}
	}
}

func TestFindPaths_InvalidRegex(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.FindPaths(ctx, FindQuery{
		Pattern:   "[invalid",
		MatchMode: MatchRegex,
	})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestTreeWithOptions_LDFilter(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.TreeWithOptions(ctx, TreeOptions{
		LDFilter: "simpleIOGenericIO",
	})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	if len(tree.Children) != 1 {
		t.Fatalf("got %d LD children, want 1", len(tree.Children))
	}
	if tree.Children[0].Name != "simpleIOGenericIO" {
		t.Errorf("LD name = %q", tree.Children[0].Name)
	}
}

func TestTreeWithOptions_LDFilter_NoMatch(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	_, err := client.TreeWithOptions(ctx, TreeOptions{
		LDFilter: "nonexistent",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTreeWithOptions_MaxDepth1(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.TreeWithOptions(ctx, TreeOptions{MaxDepth: 1})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	if len(tree.Children) != 1 {
		t.Fatalf("got %d LD children, want 1", len(tree.Children))
	}
	if len(tree.Children[0].Children) != 0 {
		t.Errorf("LD should have no children at depth 1, got %d", len(tree.Children[0].Children))
	}
}

func TestTreeWithOptions_MaxDepth2(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.TreeWithOptions(ctx, TreeOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	ldNode := tree.Children[0]
	if len(ldNode.Children) != 2 {
		t.Fatalf("LD should have 2 LN children, got %d", len(ldNode.Children))
	}
	for _, lnNode := range ldNode.Children {
		if len(lnNode.Children) != 0 {
			t.Errorf("LN %q should have no children at depth 2, got %d", lnNode.Name, len(lnNode.Children))
		}
	}
}

func TestTreeWithOptions_MaxDepth3(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.TreeWithOptions(ctx, TreeOptions{MaxDepth: 3})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	ldNode := tree.Children[0]
	for _, lnNode := range ldNode.Children {
		if len(lnNode.Children) == 0 {
			t.Errorf("LN %q should have DO children at depth 3", lnNode.Name)
		}
		for _, doNode := range lnNode.Children {
			if len(doNode.Children) != 0 {
				t.Errorf("DO %q should have no children at depth 3, got %d", doNode.Name, len(doNode.Children))
			}
		}
	}
}

func TestTreeWithOptions_IncludeFCs(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.TreeWithOptions(ctx, TreeOptions{IncludeFCs: true})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	ldNode := tree.Children[0]
	var lln0 *ModelNode
	for _, ln := range ldNode.Children {
		if ln.Name == "LLN0" {
			lln0 = ln
			break
		}
	}
	if lln0 == nil {
		t.Fatal("LLN0 not found")
	}

	if len(lln0.FCs) == 0 {
		t.Error("LLN0 should have FCs annotated")
	}

	var modNode *ModelNode
	for _, do := range lln0.Children {
		if do.Name == "Mod" {
			modNode = do
			break
		}
	}
	if modNode == nil {
		t.Fatal("Mod not found under LLN0")
	}
	if len(modNode.FCs) != 1 || modNode.FCs[0] != FCST {
		t.Errorf("Mod FCs = %v, want [ST]", modNode.FCs)
	}
	if modNode.FC != FCST {
		t.Errorf("Mod FC = %q, want ST", modNode.FC)
	}
}

func TestTreeWithOptions_IncludeFCs_MultiFCNode(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	tree, err := client.TreeWithOptions(ctx, TreeOptions{IncludeFCs: true})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	ldNode := tree.Children[0]
	var ggio1 *ModelNode
	for _, ln := range ldNode.Children {
		if ln.Name == "GGIO1" {
			ggio1 = ln
			break
		}
	}
	if ggio1 == nil {
		t.Fatal("GGIO1 not found")
	}

	if len(ggio1.FCs) < 2 {
		t.Errorf("GGIO1 FCs = %v, expected at least [CO, ST]", ggio1.FCs)
	}
	if ggio1.FC != "" {
		t.Errorf("GGIO1 FC should be empty (multi-FC), got %q", ggio1.FC)
	}
}

func TestBrowseMethods_ClosedClient(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"ListLogicalDevices", func() error { _, err := client.ListLogicalDevices(ctx); return err }},
		{"ListLogicalNodes", func() error { _, err := client.ListLogicalNodes(ctx, "LD1"); return err }},
		{"ListDataObjects", func() error { _, err := client.ListDataObjects(ctx, "LD1", "LLN0"); return err }},
		{"ListChildren", func() error {
			_, err := client.ListChildren(ctx, Ref{LD: "LD1", LN: "LLN0"})
			return err
		}},
		{"Tree", func() error { _, err := client.Tree(ctx); return err }},
		{"FindPaths", func() error { _, err := client.FindPaths(ctx, FindQuery{Pattern: "*"}); return err }},
		{"GetVariableType", func() error {
			_, err := client.GetVariableType(ctx, Ref{LD: "LD1", LN: "LLN0", Path: []string{"Mod"}, FC: FCST})
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if !errors.Is(err, ErrClosed) {
				t.Errorf("got %v, want ErrClosed", err)
			}
		})
	}
}

func TestClient_MMS(t *testing.T) {
	client := setupLoopback(t)
	raw := client.MMS()
	if raw == nil {
		t.Fatal("MMS() returned nil")
	}
}

func TestClient_Abort(t *testing.T) {
	client := setupLoopback(t)
	ctx := context.Background()

	if err := client.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// After abort, operations should fail.
	_, err := client.ListLogicalDevices(ctx)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("after Abort: got %v, want ErrClosed", err)
	}

	// Abort is idempotent.
	if err := client.Abort(ctx); err != nil {
		t.Errorf("second Abort: %v", err)
	}
}

func TestDial_InvalidAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Dial(ctx, "127.0.0.1:0", DialOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context dial")
	}
}

func TestDiscardHandler(t *testing.T) {
	h := discardHandler{}

	if h.Enabled(context.Background(), 0) {
		t.Error("Enabled should return false")
	}
	if err := h.Handle(context.Background(), slog.Record{}); err != nil {
		t.Errorf("Handle: %v", err)
	}
	if h2 := h.WithAttrs(nil); h2 != h {
		t.Error("WithAttrs should return same handler")
	}
	if h3 := h.WithGroup("g"); h3 != h {
		t.Error("WithGroup should return same handler")
	}
}

func TestTree_FCCollision(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	for _, v := range []struct {
		itemID string
		ts     mms.TypeSpec
	}{
		{"MMXU1$MX$TotW$mag$f", mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
		{"MMXU1$MX$TotW$q", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
		{"MMXU1$MX$TotW$t", mms.TypeSpec{Type: mms.ValueTypeUTCTime}},
		{"MMXU1$ST$TotW$stVal", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32}},
		{"MMXU1$CF$TotW$d", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 64}},
	} {
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: mms.ItemID(v.itemID)},
			TypeSpec: v.ts,
			Read:     func(_ context.Context) (*mms.Value, error) { return v.ts.DefaultValue(), nil },
		}); err != nil {
			t.Fatalf("register %q: %v", v.itemID, err)
		}
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	tree, err := client.TreeWithOptions(ctx, TreeOptions{IncludeFCs: true})
	if err != nil {
		t.Fatalf("TreeWithOptions: %v", err)
	}

	if len(tree.Children) != 1 {
		t.Fatalf("got %d LDs, want 1", len(tree.Children))
	}

	ldNode := tree.Children[0]
	if len(ldNode.Children) != 1 {
		t.Fatalf("got %d LNs, want 1 (MMXU1)", len(ldNode.Children))
	}

	lnNode := ldNode.Children[0]
	if lnNode.Name != "MMXU1" {
		t.Errorf("LN name = %q, want MMXU1", lnNode.Name)
	}

	// MMXU1 should have FCs: CF, MX, ST
	if len(lnNode.FCs) < 3 {
		t.Errorf("LN FCs = %v, want at least CF, MX, ST", lnNode.FCs)
	}

	// TotW should appear once as a merged DO, with FCs CF, MX, ST
	if len(lnNode.Children) != 1 {
		t.Fatalf("got %d DOs, want 1 (TotW)", len(lnNode.Children))
	}

	totW := lnNode.Children[0]
	if totW.Name != "TotW" {
		t.Errorf("DO name = %q, want TotW", totW.Name)
	}

	if len(totW.FCs) < 2 {
		t.Errorf("TotW FCs = %v, expected multiple FCs (at least MX and ST)", totW.FCs)
	}

	// Ensure FC is not set when multiple FCs apply (ambiguous)
	if len(totW.FCs) > 1 && totW.FC != "" {
		t.Errorf("TotW.FC should be empty when multiple FCs apply, got %q", totW.FC)
	}
}

func TestListChildren_FCCollision_MergedView(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	// Register the same DA name "stVal" under both ST and MX
	for _, v := range []struct {
		itemID string
		ts     mms.TypeSpec
	}{
		{"GGIO1$ST$Ind1$stVal", mms.TypeSpec{Type: mms.ValueTypeBoolean}},
		{"GGIO1$MX$Ind1$stVal", mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
		{"GGIO1$ST$Ind1$q", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}},
	} {
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: mms.ItemID(v.itemID)},
			TypeSpec: v.ts,
			Read:     func(_ context.Context) (*mms.Value, error) { return v.ts.DefaultValue(), nil },
		}); err != nil {
			t.Fatalf("register %q: %v", v.itemID, err)
		}
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	children, err := client.ListChildren(ctx, Ref{LD: "LD1", LN: "GGIO1", Path: []string{"Ind1"}})
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}

	// "stVal" appears under both ST and MX, but ListChildren merges
	// and should return it only once.
	nameSet := make(map[string]bool)
	for _, c := range children {
		if nameSet[c.Name] {
			t.Errorf("duplicate child name %q — ListChildren should merge across FCs", c.Name)
		}
		nameSet[c.Name] = true
	}

	if !nameSet["stVal"] {
		t.Error("expected 'stVal' in merged children")
	}
	if !nameSet["q"] {
		t.Error("expected 'q' in merged children")
	}
}
