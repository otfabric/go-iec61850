package iec61850

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

func setupDataSetLoopback(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	domain := "simpleIOGenericIO"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	vars := []struct {
		itemID   string
		typeSpec mms.TypeSpec
		value    *mms.Value
	}{
		{"LLN0$ST$Mod$stVal", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}, mms.NewInteger(1)},
		{"LLN0$ST$Mod$q", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 13}, mms.NewBitStringWithLength([]byte{0, 0}, 13)},
		{"GGIO1$ST$Ind1$stVal", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(true)},
	}

	for _, v := range vars {
		capturedVal := v.value
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(v.itemID)},
			TypeSpec: v.typeSpec,
			Read: func(_ context.Context) (*mms.Value, error) {
				return capturedVal, nil
			},
		}); err != nil {
			t.Fatalf("register variable %q: %v", v.itemID, err)
		}
	}

	if err := srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: mms.DomainID(domain),
			ItemID: "LLN0$dataset1",
		},
		Deletable: false,
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"}},
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$q"}},
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "GGIO1$ST$Ind1$stVal"}},
		},
	}); err != nil {
		t.Fatalf("register NVL: %v", err)
	}

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

func TestListDataSets(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	names, err := client.ListDataSets(ctx, "simpleIOGenericIO")
	if err != nil {
		t.Fatalf("ListDataSets: %v", err)
	}

	if len(names) != 1 {
		t.Fatalf("got %d data sets, want 1", len(names))
	}
	if names[0] != "LLN0$dataset1" {
		t.Errorf("name = %q, want %q", names[0], "LLN0$dataset1")
	}
}

func TestListDataSets_EmptyLD(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	_, err := client.ListDataSets(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty LD")
	}
}

func TestGetDataSet(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	ds, err := client.GetDataSet(ctx, "simpleIOGenericIO", "LLN0$dataset1")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}

	if ds.Reference != "simpleIOGenericIO/LLN0.dataset1" {
		t.Errorf("Reference = %q, want %q", ds.Reference, "simpleIOGenericIO/LLN0.dataset1")
	}
	if ds.Deletable {
		t.Error("Deletable should be false")
	}
	if len(ds.Members) != 3 {
		t.Fatalf("got %d members, want 3", len(ds.Members))
	}

	if ds.Members[0].DomainID != "simpleIOGenericIO" {
		t.Errorf("member[0].DomainID = %q", ds.Members[0].DomainID)
	}
	if ds.Members[0].ItemID != "LLN0$ST$Mod$stVal" {
		t.Errorf("member[0].ItemID = %q", ds.Members[0].ItemID)
	}
	if ds.Members[0].Ref.LN != "LLN0" {
		t.Errorf("member[0].Ref.LN = %q, want LLN0", ds.Members[0].Ref.LN)
	}
	if ds.Members[0].Ref.FC != FCST {
		t.Errorf("member[0].Ref.FC = %q, want ST", ds.Members[0].Ref.FC)
	}
}

func TestReadDataSet(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	results, err := client.ReadDataSet(ctx, "simpleIOGenericIO", "LLN0$dataset1")
	if err != nil {
		t.Fatalf("ReadDataSet: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Member 0: integer
	if results[0].Err != nil {
		t.Errorf("results[0].Err: %v", results[0].Err)
	}
	if results[0].Value == nil {
		t.Fatal("results[0].Value is nil")
	}
	i, err := results[0].Value.Int64()
	if err != nil {
		t.Fatalf("results[0] Int64: %v", err)
	}
	if i != 1 {
		t.Errorf("results[0] = %d, want 1", i)
	}

	// Member 1: bit string (quality)
	if results[1].Err != nil {
		t.Errorf("results[1].Err: %v", results[1].Err)
	}
	if results[1].Value == nil {
		t.Fatal("results[1].Value is nil")
	}

	// Member 2: boolean
	if results[2].Err != nil {
		t.Errorf("results[2].Err: %v", results[2].Err)
	}
	if results[2].Value == nil {
		t.Fatal("results[2].Value is nil")
	}
	b, err := results[2].Value.Bool()
	if err != nil {
		t.Fatalf("results[2] Bool: %v", err)
	}
	if !b {
		t.Error("results[2] = false, want true")
	}
}

func TestListDataSets_ClosedClient(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ListDataSets(ctx, "simpleIOGenericIO")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestGetDataSet_ClosedClient(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.GetDataSet(ctx, "simpleIOGenericIO", "LLN0$dataset1")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestReadDataSet_ClosedClient(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ReadDataSet(ctx, "simpleIOGenericIO", "LLN0$dataset1")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestCreateDataSet(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	members := []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST}},
	}

	err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsDynamic", members)
	if err != nil {
		t.Fatalf("CreateDataSet: %v", err)
	}

	ds, err := client.GetDataSet(ctx, "simpleIOGenericIO", "LLN0$dsDynamic")
	if err != nil {
		t.Fatalf("GetDataSet after create: %v", err)
	}

	if len(ds.Members) != 2 {
		t.Errorf("got %d members, want 2", len(ds.Members))
	}
	if !ds.Deletable {
		t.Error("dynamic data set should be deletable")
	}
}

func TestDeleteDataSet(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	members := []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
	}

	if err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsToDelete", members); err != nil {
		t.Fatalf("CreateDataSet: %v", err)
	}

	if err := client.DeleteDataSet(ctx, "simpleIOGenericIO", "LLN0$dsToDelete"); err != nil {
		t.Fatalf("DeleteDataSet: %v", err)
	}
}

func TestCreateDataSet_EmptyLD(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	err := client.CreateDataSet(ctx, "", "LLN0$ds", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
	})
	if err == nil {
		t.Fatal("expected error for empty LD")
	}
}

func TestCreateDataSet_NoMembers(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$ds", nil)
	if err == nil {
		t.Fatal("expected error for no members")
	}
}

func TestDeleteDataSet_EmptyName(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	err := client.DeleteDataSet(ctx, "simpleIOGenericIO", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateDataSet_WithRawDomainItemID(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	members := []DataSetMember{
		{DomainID: "simpleIOGenericIO", ItemID: "LLN0$ST$Mod$stVal"},
	}

	err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsRaw", members)
	if err != nil {
		t.Fatalf("CreateDataSet with raw IDs: %v", err)
	}
}

func TestGetDataSet_Cached(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	domain := "testLD"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
	}); err != nil {
		t.Fatalf("register variable: %v", err)
	}

	if err := srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$cached"},
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"}},
		},
	}); err != nil {
		t.Fatalf("register NVL: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{Cache: CacheLazy})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	// First call fetches from server
	ds1, err := client.GetDataSet(ctx, domain, "LLN0$cached")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}

	// Second call should hit cache
	ds2, err := client.GetDataSet(ctx, domain, "LLN0$cached")
	if err != nil {
		t.Fatalf("GetDataSet cached: %v", err)
	}

	if ds1.Reference != ds2.Reference {
		t.Errorf("cached reference mismatch: %q vs %q", ds1.Reference, ds2.Reference)
	}

	// Invalidate and re-fetch
	client.InvalidateCache()
	ds3, err := client.GetDataSet(ctx, domain, "LLN0$cached")
	if err != nil {
		t.Fatalf("GetDataSet after invalidate: %v", err)
	}
	if ds3.Reference != ds1.Reference {
		t.Errorf("re-fetched reference mismatch: %q", ds3.Reference)
	}
}

func TestGetDataSet_CachedMutationSafe(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5,
			MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
		},
	})
	domain := "testLD"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
	}); err != nil {
		t.Fatalf("register variable: %v", err)
	}
	if err := srv.RegisterNamedVariableList(mms.NamedVariableList{
		Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$cached"},
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"}},
		},
	}); err != nil {
		t.Fatalf("register NVL: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client, err := NewClient(mmsClient, ClientOptions{Cache: CacheLazy})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	ds1, err := client.GetDataSet(ctx, domain, "LLN0$cached")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}

	originalLen := len(ds1.Members)
	ds1.Members = append(ds1.Members, DataSetMember{DomainID: "mutated"})
	ds1.Reference = "mutated"

	ds2, err := client.GetDataSet(ctx, domain, "LLN0$cached")
	if err != nil {
		t.Fatalf("GetDataSet cached: %v", err)
	}

	if ds2.Reference == "mutated" {
		t.Error("mutation leaked into cache: Reference")
	}
	if len(ds2.Members) != originalLen {
		t.Errorf("mutation leaked into cache: Members len %d vs original %d", len(ds2.Members), originalLen)
	}

	// Verify nested slice (Ref.Path) is also independent.
	ds3, err := client.GetDataSet(ctx, domain, "LLN0$cached")
	if err != nil {
		t.Fatalf("GetDataSet for path test: %v", err)
	}
	if len(ds3.Members) > 0 && len(ds3.Members[0].Ref.Path) > 0 {
		original := ds3.Members[0].Ref.Path[0]
		ds3.Members[0].Ref.Path[0] = "mutated"
		ds3.Members[0].DomainID = "mutated"

		ds4, err := client.GetDataSet(ctx, domain, "LLN0$cached")
		if err != nil {
			t.Fatalf("GetDataSet after path mutation: %v", err)
		}
		if ds4.Members[0].Ref.Path[0] != original {
			t.Errorf("Ref.Path mutation leaked: got %q, want %q", ds4.Members[0].Ref.Path[0], original)
		}
		if ds4.Members[0].DomainID == "mutated" {
			t.Error("DomainID mutation leaked into cache")
		}
	}
}

func TestCreateDataSet_RejectsLNOnlyRef(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsTest", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", FC: FCST}},
	})
	if err == nil {
		t.Fatal("expected error for LN-only ref (no data path)")
	}
}

func TestCreateDataSet_RejectsMissingFC(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsTest", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}}},
	})
	if err == nil {
		t.Fatal("expected error for missing FC")
	}
}

func TestCreateDataSet_CrossLDMember(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	for _, d := range []string{"LD1", "LD2"} {
		if err := srv.RegisterDomain(d); err != nil {
			t.Fatalf("register domain %q: %v", d, err)
		}
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
	}); err != nil {
		t.Fatalf("register variable LD1: %v", err)
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD2", ItemID: "GGIO1$ST$Ind1$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeBoolean},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewBoolean(true), nil },
	}); err != nil {
		t.Fatalf("register variable LD2: %v", err)
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

	// Create a dataset in LD1 that references a member in LD2 (cross-LD).
	members := []DataSetMember{
		{Ref: Ref{LD: "LD1", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
		{Ref: Ref{LD: "LD2", LN: "GGIO1", Path: []string{"Ind1", "stVal"}, FC: FCST}},
	}

	err = client.CreateDataSet(ctx, "LD1", "LLN0$dsCrossLD", members)
	if err != nil {
		t.Fatalf("CreateDataSet with cross-LD member: %v", err)
	}

	ds, err := client.GetDataSet(ctx, "LD1", "LLN0$dsCrossLD")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}
	if len(ds.Members) != 2 {
		t.Fatalf("got %d members, want 2", len(ds.Members))
	}
	if ds.Members[1].DomainID != "LD2" {
		t.Errorf("cross-LD member DomainID = %q, want LD2", ds.Members[1].DomainID)
	}
}

func TestCreateDataSet_CrossLDRawDomainID(t *testing.T) {
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	for _, d := range []string{"LD1", "LD2"} {
		if err := srv.RegisterDomain(d); err != nil {
			t.Fatalf("register domain %q: %v", d, err)
		}
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD2", ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read:     func(_ context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
	}); err != nil {
		t.Fatalf("register variable LD2: %v", err)
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

	members := []DataSetMember{
		{DomainID: "LD2", ItemID: "LLN0$ST$Mod$stVal"},
	}

	err = client.CreateDataSet(ctx, "LD1", "LLN0$dsRawCrossLD", members)
	if err != nil {
		t.Fatalf("CreateDataSet with cross-LD raw member: %v", err)
	}
}

func TestCreateDataSet_EmptyLDDefaultsToOwner(t *testing.T) {
	client := setupDataSetLoopback(t)
	ctx := context.Background()

	members := []DataSetMember{
		{Ref: Ref{LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
	}

	err := client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsDefault", members)
	if err != nil {
		t.Fatalf("CreateDataSet with empty LD: %v", err)
	}

	ds, err := client.GetDataSet(ctx, "simpleIOGenericIO", "LLN0$dsDefault")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}
	if len(ds.Members) != 1 {
		t.Fatalf("got %d members, want 1", len(ds.Members))
	}
	if ds.Members[0].DomainID != "simpleIOGenericIO" {
		t.Errorf("member DomainID = %q, want simpleIOGenericIO", ds.Members[0].DomainID)
	}
}
