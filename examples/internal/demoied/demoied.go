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
	"sync"
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

// Names in demo.icd.
const (
	IED = "DemoIED"
	LD  = "LD1"
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
	srv.ValueStore().Set(LD+"/MMXU1$MX$PhV$phsA$cVal$mag$f", mms.NewFloat(0))
	srv.EnableReports()
	srv.EnableJournals()
	enableSettingGroups(srv)
	return srv, nil
}

// Setting store keys of PTOC1.StrVal.setMag.f: the active group's
// value (FC SG) and the edit buffer (FC SE).
const (
	keySG = LD + "/PTOC1$SG$StrVal$setMag$f"
	keySE = LD + "/PTOC1$SE$StrVal$setMag$f"
)

// enableSettingGroups keeps the values of the two setting groups in the
// application, as IEC 61850-7-2 leaves their storage to the server:
// activating a group loads its values into FC SG, selecting a group for
// editing loads them into FC SE, and confirming stores the edit buffer
// (and reloads SG when the edited group is the active one).
func enableSettingGroups(srv *iec61850.Server) {
	var mu sync.Mutex
	groups := map[uint8]*mms.Value{1: mms.NewFloat(100), 2: mms.NewFloat(150)}
	active := uint8(1)
	srv.ValueStore().Set(keySG, groups[active])
	srv.ValueStore().Set(keySE, groups[active])
	srv.EnableSettingGroups(iec61850.SettingGroupHandler{
		OnActiveSGChanged: func(ctx context.Context, _ string, sg uint8) error {
			mu.Lock()
			defer mu.Unlock()
			active = sg
			srv.SetValue(ctx, keySG, groups[sg])
			return nil
		},
		OnEditSGSelected: func(ctx context.Context, _ string, sg uint8) error {
			mu.Lock()
			defer mu.Unlock()
			srv.SetValue(ctx, keySE, groups[sg])
			return nil
		},
		OnConfirmEdit: func(ctx context.Context, _ string, sg uint8) error {
			mu.Lock()
			defer mu.Unlock()
			groups[sg] = srv.ValueStore().Get(keySE)
			if sg == active {
				srv.SetValue(ctx, keySG, groups[sg])
			}
			return nil
		},
	})
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
