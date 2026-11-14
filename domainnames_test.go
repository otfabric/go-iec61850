// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

// serveDomains serves the model of testServerSCLWithBRCB (IED1, LD1) built with o
// and returns the logical devices a client sees and the value of
// <domain>/LLN0$BR$brcb01$DatSet.
func serveDomains(t *testing.T, o ServerModelOptions, domain string) ([]string, string) {
	t.Helper()
	model, err := NewServerModelFromSCLWithOptions(testServerSCLWithBRCB(), "IED1", "", o)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	clientT, serverT := loopbackPair(t)
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })
	lds, err := c.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(lds))
	for i, ld := range lds {
		names[i] = ld.Name
	}
	rcb, err := c.GetReportControlBlock(ctx, domain, "LLN0$BR$brcb01")
	if err != nil {
		t.Fatalf("GetReportControlBlock %s: %v", domain, err)
	}
	return names, rcb.DatSet
}

// By default the server names the logical device as IEC 61850-8-1 does
// (IED name + instance) and the RCB's DatSet follows;
// InstanceDomainNames keeps the bare instance of go-iec61850 before v1.7.0.
func TestServerModel_DomainNames(t *testing.T) {
	for _, c := range []struct {
		name   string
		opts   ServerModelOptions
		domain string
	}{
		{"standard", ServerModelOptions{}, "IED1LD1"},
		{"instance", ServerModelOptions{InstanceDomainNames: true}, "LD1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lds, datSet := serveDomains(t, c.opts, c.domain)
			if len(lds) != 1 || lds[0] != c.domain {
				t.Errorf("logical devices %v, want [%s]", lds, c.domain)
			}
			if want := c.domain + "/LLN0$dsTest"; datSet != want {
				t.Errorf("DatSet %q, want %q", datSet, want)
			}
		})
	}
}
