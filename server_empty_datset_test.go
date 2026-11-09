// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// libiec61850's substitution example has an unbuffered RCB with datSet="",
// which SCL allows: by default the server refuses the model; with
// AllowEmptyReportDatSet it serves it, and enabling the RCB is refused
// until a data set is configured.
func TestServer_AllowEmptyReportDatSet(t *testing.T) {
	doc, err := scl.ParseFile("scl/testdata/examples/server_example_substitution/substitution_example.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "IED1", "AP1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewServer(model, ServerOptions{}); err == nil || !strings.Contains(err.Error(), "has empty DatSet") {
		t.Fatalf("NewServer without the option = %v, want an empty DatSet error", err)
	}

	srv, err := NewServer(model, ServerOptions{AllowEmptyReportDatSet: true})
	if err != nil {
		t.Fatalf("NewServer with the option: %v", err)
	}
	t.Cleanup(srv.Close)
	srv.EnableReports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewClient(mc, ClientOptions{})
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	ld := "IED1LD1"
	rcbs, err := c.ListReports(ctx, ld)
	if err != nil || len(rcbs) == 0 {
		t.Fatalf("ListReports = %v, %v", rcbs, err)
	}
	var item string
	for _, r := range rcbs {
		if strings.HasSuffix(r, "$urcb") {
			item = r
		}
	}
	if item == "" {
		t.Fatalf("urcb not listed: %v", rcbs)
	}
	rcb, err := c.GetReportControlBlock(ctx, ld, item)
	if err != nil {
		t.Fatal(err)
	}
	if rcb.DatSet != "" {
		t.Errorf("DatSet = %q, want empty", rcb.DatSet)
	}
	err = c.SetReportControlBlock(ctx, ld, item, RCBUpdate{Fields: RCBFieldRptEna, RptEna: true})
	if err == nil {
		t.Error("enabling an RCB without a data set succeeded")
	}
}
