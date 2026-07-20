// SPDX-License-Identifier: MIT

package servermodel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateConfig_Basic(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	var buf bytes.Buffer
	if err := GenerateConfig(&buf, m); err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	var cfg ServerConfig
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}

	if len(cfg.LogicalDevices) != 1 {
		t.Fatalf("LDs = %d, want 1", len(cfg.LogicalDevices))
	}
	if cfg.LogicalDevices[0].Name != "LD1" {
		t.Errorf("LD name = %q, want LD1", cfg.LogicalDevices[0].Name)
	}

	lns := cfg.LogicalDevices[0].LogicalNodes
	if len(lns) != 2 {
		t.Fatalf("LNs = %d, want 2", len(lns))
	}
	if lns[0].Name != "LLN0" {
		t.Errorf("LN0 name = %q, want LLN0", lns[0].Name)
	}
	if len(lns[0].DataSets) != 1 {
		t.Errorf("datasets = %d, want 1", len(lns[0].DataSets))
	}
	if len(lns[0].Reports) != 1 {
		t.Errorf("reports = %d, want 1", len(lns[0].Reports))
	}
}

func TestGenerateConfig_Roundtrip(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	var buf bytes.Buffer
	if err := GenerateConfig(&buf, m); err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	var cfg ServerConfig
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}

	rpt := cfg.LogicalDevices[0].LogicalNodes[0].Reports[0]
	if rpt.Name != "brcbEvents01" {
		t.Errorf("report name = %q, want brcbEvents01", rpt.Name)
	}
	if rpt.ConfRev != 1 {
		t.Errorf("confRev = %d, want 1", rpt.ConfRev)
	}
	if !rpt.Buffered {
		t.Error("expected buffered")
	}
}

func TestGenerateMMS_Basic(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	var buf bytes.Buffer
	if err := GenerateMMS(&buf, m); err != nil {
		t.Fatalf("GenerateMMS: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "domain: LD1") {
		t.Error("expected 'domain: LD1' in output")
	}
	if !strings.Contains(out, "LLN0$ST$Mod$stVal") {
		t.Error("expected LLN0$ST$Mod$stVal variable in output")
	}
	if !strings.Contains(out, "nvl: LD1/LLN0$dsEvents") {
		t.Error("expected NVL dsEvents in output")
	}
}

func TestGenerateConfig_Empty(t *testing.T) {
	m := &Model{}
	var buf bytes.Buffer
	if err := GenerateConfig(&buf, m); err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	var cfg ServerConfig
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}

	if cfg.LogicalDevices != nil {
		t.Errorf("expected nil LDs for empty model, got %d", len(cfg.LogicalDevices))
	}
}

func TestGenerateConfig_NilModel(t *testing.T) {
	var buf bytes.Buffer
	err := GenerateConfig(&buf, nil)
	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if !strings.Contains(err.Error(), "nil model") {
		t.Errorf("error = %q, want 'nil model'", err)
	}
}

func TestGenerateMMS_NilModel(t *testing.T) {
	var buf bytes.Buffer
	err := GenerateMMS(&buf, nil)
	if err == nil {
		t.Fatal("expected error for nil model")
	}
	if !strings.Contains(err.Error(), "nil model") {
		t.Errorf("error = %q, want 'nil model'", err)
	}
}
