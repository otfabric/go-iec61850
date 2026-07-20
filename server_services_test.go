// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestServerOptions_Identity(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{
		Identity: &ServerIdentity{
			Vendor:   "TestVendor",
			Model:    "TestModel",
			Revision: "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	caps := srv.Capabilities()
	if !caps.Identify {
		t.Error("Capabilities().Identify should be true when Identity is set")
	}
	if !caps.Variables {
		t.Error("Capabilities().Variables should always be true")
	}
}

func TestServerOptions_FileProvider(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{
		FileProvider: &nullFileProvider{},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	caps := srv.Capabilities()
	if !caps.Files {
		t.Error("Capabilities().Files should be true when FileProvider is set")
	}
}

func TestServerOptions_FileProvider_MMS_Precedence(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	directFP := &nullFileProvider{}
	srv, err := NewServer(model, ServerOptions{
		FileProvider: &nullFileProvider{},
		MMS: mms.ServerOptions{
			FileProvider: directFP,
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	caps := srv.Capabilities()
	if !caps.Files {
		t.Error("Capabilities().Files should be true")
	}
}

func TestCapabilities_Default(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	caps := srv.Capabilities()
	if !caps.Variables {
		t.Error("Variables should always be true")
	}
	if caps.DataSets {
		t.Error("DataSets should be false for minimal model")
	}
	if caps.Reports {
		t.Error("Reports should be false before EnableReports")
	}
	if caps.Controls {
		t.Error("Controls should be false before RegisterControl")
	}
	if caps.SettingGroups {
		t.Error("SettingGroups should be false before EnableSettingGroups")
	}
	if caps.Journals {
		t.Error("Journals should be false before EnableJournals")
	}
	if caps.Files {
		t.Error("Files should be false without FileProvider")
	}
	if caps.Identify {
		t.Error("Identify should be false without Identity")
	}
}

func TestCapabilities_WithReports(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	caps := srv.Capabilities()
	if !caps.Reports {
		t.Error("Reports should be true after EnableReports")
	}
	if !caps.DataSets {
		t.Error("DataSets should be true for report model")
	}
}

func TestCapabilities_WithControls(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)

	_ = srv.RegisterControl("LD1", "LLN0.Mod", CtlModelDirectNormal, ControlHandler{
		OnOperate: func(_ context.Context, _ ControlRequest) error { return nil },
	})

	caps := srv.Capabilities()
	if !caps.Controls {
		t.Error("Controls should be true after RegisterControl")
	}
}

func TestServerOptions_ConnectionHooks(t *testing.T) {
	var connectCount, disconnectCount atomic.Int32

	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{
		OnConnect: func(e ConnectionEvent) {
			connectCount.Add(1)
		},
		OnDisconnect: func(e ConnectionEvent) {
			disconnectCount.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	_ = mmsClient.Close(ctx)

	<-done

	if got := connectCount.Load(); got != 1 {
		t.Errorf("OnConnect called %d times, want 1", got)
	}
	if got := disconnectCount.Load(); got != 1 {
		t.Errorf("OnDisconnect called %d times, want 1", got)
	}
}

func TestServer_HandleIdentify(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if srv.Capabilities().Identify {
		t.Error("Identify should be false initially")
	}

	srv.HandleIdentify(ServerIdentity{
		Vendor: "PostInit", Model: "M1", Revision: "2.0",
	})

	if !srv.Capabilities().Identify {
		t.Error("Identify should be true after HandleIdentify")
	}
}

func TestServer_IdentifyRoundTrip(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{
		Identity: &ServerIdentity{
			Vendor:   "TestVendor",
			Model:    "TestModel",
			Revision: "1.2.3",
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()

	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	defer func() { _ = mmsClient.Close(ctx) }()

	id, err := mmsClient.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Vendor != "TestVendor" {
		t.Errorf("Vendor = %q, want TestVendor", id.Vendor)
	}
	if id.Model != "TestModel" {
		t.Errorf("Model = %q, want TestModel", id.Model)
	}
	if id.Revision != "1.2.3" {
		t.Errorf("Revision = %q, want 1.2.3", id.Revision)
	}
}

func TestServer_StatusRoundTrip(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()

	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	defer func() { _ = mmsClient.Close(ctx) }()

	st, err := mmsClient.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Logical != mms.VMDLogicalStatusStateChangesAllowed {
		t.Errorf("Logical = %v, want StateChangesAllowed", st.Logical)
	}
	if st.Physical != mms.VMDPhysicalStatusOperational {
		t.Errorf("Physical = %v, want Operational", st.Physical)
	}
}

func TestServiceCapabilities_String(t *testing.T) {
	tests := []struct {
		name string
		caps ServiceCapabilities
		want string
	}{
		{
			name: "variables_only",
			caps: ServiceCapabilities{Variables: true},
			want: "ServiceCapabilities(variables)",
		},
		{
			name: "all",
			caps: ServiceCapabilities{
				Variables: true, DataSets: true, Reports: true,
				Controls: true, SettingGroups: true, Journals: true,
				Files: true, Identify: true,
			},
			want: "ServiceCapabilities(variables, datasets, reports, controls, setting-groups, journals, files, identify)",
		},
		{
			name: "partial",
			caps: ServiceCapabilities{
				Variables: true, Reports: true, Files: true,
			},
			want: "ServiceCapabilities(variables, reports, files)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.caps.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceCapabilities_String_None(t *testing.T) {
	caps := ServiceCapabilities{}
	caps.Variables = false
	if got := caps.String(); got != "ServiceCapabilities(none)" {
		t.Errorf("String() = %q, want ServiceCapabilities(none)", got)
	}
}

// nullFileProvider is a minimal FileProvider for testing capability detection.
type nullFileProvider struct{}

func (nullFileProvider) List(_ context.Context, _ mms.FileListRequest) (*mms.FileListResult, error) {
	return &mms.FileListResult{}, nil
}

func (nullFileProvider) Open(_ context.Context, _ string) (mms.FileHandle, mms.FileAttributes, error) {
	return 0, mms.FileAttributes{}, nil
}

func (nullFileProvider) Read(_ context.Context, _ mms.FileHandle, _ int) ([]byte, bool, error) {
	return nil, false, nil
}

func (nullFileProvider) Close(_ context.Context, _ mms.FileHandle) error {
	return nil
}

func (nullFileProvider) Delete(_ context.Context, _ string) error {
	return nil
}

func (nullFileProvider) Rename(_ context.Context, _, _ string) error {
	return nil
}

func (nullFileProvider) ObtainFile(_ context.Context, _, _ string) error {
	return nil
}
