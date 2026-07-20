//go:build interop

// SPDX-License-Identifier: MIT

// Phase D2 — dataset error behavior tests.
//
// These tests verify that reading a non-existent dataset returns an error
// for both the libiec61850 server and the go-iec61850 server, and that the
// association remains usable after each error.
//
// Note: TestBeanClient_DS_ReadAllMembers already exists in dataset_depth_test.go
// and is therefore not duplicated here.
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// TestLibIECClient_DS_UnknownDataSet reads a dataset that does not exist on
// the libiec61850 server and verifies that an error is returned.
func TestLibIECClient_DS_UnknownDataSet(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	_, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$nosuchds")
	if err == nil {
		t.Error("expected error reading non-existent dataset on libiec61850 server, got nil")
	} else {
		t.Logf("libiec61850 server correctly rejected unknown dataset: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, readErr := c.ReadRaw(ctx, okRef); readErr != nil {
		t.Errorf("association broken after unknown dataset read: %v", readErr)
	}
}

// TestGoServer_DS_UnknownDataSet reads a dataset that does not exist on the
// go-iec61850 server and verifies that an error is returned.
func TestGoServer_DS_UnknownDataSet(t *testing.T) {
	srv := startGoIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(ctx)

	_, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$nosuchds")
	if err == nil {
		t.Error("expected error reading non-existent dataset on go server, got nil")
	} else {
		t.Logf("go server correctly rejected unknown dataset: %v", err)
	}

	// Association must survive.
	okRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	if _, readErr := c.ReadRaw(ctx, okRef); readErr != nil {
		t.Errorf("go server: association broken after unknown dataset read: %v", readErr)
	}
}
