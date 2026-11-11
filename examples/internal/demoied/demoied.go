// SPDX-License-Identifier: MIT

// Package demoied holds the demo IED model shared by the examples
// (demo.icd) and a loopback server the example smoke tests run the
// clients against.
package demoied

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"net"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/scl"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ICD is the demo IED's SCL (demo.icd).
//
//go:embed demo.icd
var ICD []byte

// Names in demo.icd. Domain is the MMS domain the server gives LD: the
// IED name followed by the instance (IEC 61850-8-1).
const (
	IED    = "DemoIED"
	LD     = "LD1"
	Domain = IED + LD
)

// SCL parses ICD.
func SCL() (*scl.SCL, error) { return scl.Parse(bytes.NewReader(ICD)) }

// NewServer builds a server for the demo IED with reports, journals,
// setting groups and file services (Files) enabled. GOOSE and SV are
// left to the caller, which owns the Ethernet transport.
func NewServer(opts iec61850.ServerOptions) (*iec61850.Server, error) {
	doc, err := SCL()
	if err != nil {
		return nil, err
	}
	model, err := iec61850.NewServerModelFromSCL(doc, IED, "")
	if err != nil {
		return nil, err
	}
	// Journal reads over MMS are served from the provider given to
	// the MMS server; EnableJournals adopts it.
	if opts.MMS.JournalProvider == nil {
		opts.MMS.JournalProvider = iec61850.NewMemoryJournalProvider()
	}
	if opts.FileProvider == nil && opts.MMS.FileProvider == nil {
		opts.FileProvider = Files{"demo.icd": ICD}
	}
	srv, err := iec61850.NewServer(model, opts)
	if err != nil {
		return nil, err
	}
	// Initial process values (SCL DAI values are loaded the same way).
	srv.ValueStore().Set(Domain+"/MMXU1$MX$PhV$phsA$cVal$mag$f", mms.NewFloat(0))
	srv.EnableReports()
	srv.EnableJournals()
	// The setting groups' values come from demo.icd (Val sGroup).
	srv.EnableSettingGroups(iec61850.SettingGroupHandler{})
	return srv, nil
}

// Serve starts srv on a loopback port and returns its address. The
// server is closed when t ends.
func Serve(t testing.TB, srv *iec61850.Server) string {
	t.Helper()
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.ListenAndServe(ctx, ln); err != nil && ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		srv.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return ln.Addr().String()
}

// Start builds the demo server and serves it on loopback.
func Start(t testing.TB) (*iec61850.Server, string) {
	t.Helper()
	srv, err := NewServer(iec61850.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return srv, Serve(t, srv)
}
