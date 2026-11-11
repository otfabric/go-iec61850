// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/scl"
)

// gooseServerFromCID builds a server from interop-goose.cid after the
// textual replacements in repl (old, new pairs).
func gooseServerFromCID(t *testing.T, repl ...string) *Server {
	t.Helper()
	raw, err := os.ReadFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for i := 0; i+1 < len(repl); i += 2 {
		if !strings.Contains(src, repl[i]) {
			t.Fatalf("%q not in CID", repl[i])
		}
		src = strings.Replace(src, repl[i], repl[i+1], 1)
	}
	doc, err := scl.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func gocbBool(srv *Server, field string) bool {
	b, _ := srv.store.Get(gcbBoolRef + "$" + field).Bool()
	return b
}

// TestSpec_8_1_AnnexB_UnconfiguredMAC: 00-00-00-00-00-00 means "not
// configured" (8-1 Annex B). Such a GoCB needs commissioning and does
// not stop the other GoCBs from publishing (GSE-03).
func TestSpec_8_1_AnnexB_UnconfiguredMAC(t *testing.T) {
	for name, mac := range map[string]string{"zero": "00-00-00-00-00-00", "unicast": "02-0C-CD-01-00-02"} {
		t.Run(name, func(t *testing.T) {
			srv := gooseServerFromCID(t, "01-0C-CD-01-00-02", mac)
			hub := goose.NewHub()
			rx := hub.NewPort(goose.MAC{})
			e, err := srv.EnableGOOSE(GOOSEOptions{Transport: hub.NewPort(goose.MAC{})})
			if err != nil {
				t.Fatal(err)
			}
			if !gocbBool(srv, "NdsCom") || gocbBool(srv, "GoEna") {
				t.Error("gcbBool should need commissioning")
			}
			var dae *mms.DataAccessError
			if err := e.SetEnabled(gcbBoolRef, true); !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorTemporarilyUnavailable {
				t.Errorf("enable: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			raw, err := rx.Recv(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if f, err := goose.Decode(raw); err != nil || f.PDU.GocbRef != gcbAllRef {
				t.Errorf("published %v %v", f, err)
			}
		})
	}
}

// TestSpec_7_2_18_2_1_3_ConfRevZero: with ConfRev 0, SetGoCBValues
// enabling GoEna fails (GSE-06).
func TestSpec_7_2_18_2_1_3_ConfRevZero(t *testing.T) {
	srv := gooseServerFromCID(t, `datSet="dsGooseBool"
                        confRev="1"`, `datSet="dsGooseBool"
                        confRev="0"`)
	e, err := srv.EnableGOOSE(GOOSEOptions{Transport: goose.NewHub().NewPort(goose.MAC{})})
	if err != nil {
		t.Fatal(err)
	}
	if gocbBool(srv, "GoEna") || !gocbBool(srv, "NdsCom") {
		t.Error("GoCB with ConfRev 0 enabled")
	}
	if err := e.SetEnabled(gcbBoolRef, true); err == nil {
		t.Error("GoEna TRUE accepted with ConfRev 0")
	}
	if err := e.SetEnabled(gcbAllRef, true); err != nil {
		t.Errorf("ConfRev 1: %v", err)
	}
	// Writing DatSet increments ConfRev to 1; the block is then usable.
	if _, err := e.handleWrite(gcbBoolRef+"$DatSet", mms.NewVisibleString("dsGooseBool")); err != nil {
		t.Fatal(err)
	}
	if err := e.SetEnabled(gcbBoolRef, true); err != nil {
		t.Errorf("after DatSet write: %v", err)
	}
}

// TestSpec_8_1_A3_ServerFixedOffs: a GSEControl with fixedOffs="true"
// is published with the fixed-length encoding: frame lengths do not
// change with the values (GSE-11).
func TestSpec_8_1_A3_ServerFixedOffs(t *testing.T) {
	srv := gooseServerFromCID(t, `<GSEControl name="gcbAll"`, `<GSEControl fixedOffs="true" name="gcbAll"`)
	hub := goose.NewHub()
	rx := hub.NewPort(goose.MAC{})
	if _, err := srv.EnableGOOSE(GOOSEOptions{Transport: hub.NewPort(goose.MAC{}), TimeQuality: 0x0a}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	next := func(pred func(*goose.Frame) bool) (*goose.Frame, int) {
		t.Helper()
		for {
			raw, err := rx.Recv(ctx)
			if err != nil {
				t.Fatal(err)
			}
			f, err := goose.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if f.PDU.GocbRef == gcbAllRef && pred(f) {
				return f, len(raw)
			}
		}
	}
	f1, n1 := next(func(*goose.Frame) bool { return true })
	if f1.PDU.TimeQuality != 0x0a {
		t.Errorf("TimeQuality %#x", f1.PDU.TimeQuality)
	}
	// TotVAh.actVal is INT32U: its BER encoding grows from 1 to 5 octets.
	srv.SetValue(ctx, "InteropIEDInteropLD/MMTR1$ST$TotVAh$actVal", mms.NewUnsigned(0xfedcba98))
	_, n2 := next(func(f *goose.Frame) bool { return f.PDU.StNum > f1.PDU.StNum })
	if n1 != n2 {
		t.Errorf("fixed-length frame sizes %d / %d", n1, n2)
	}
	// Without fixedOffs the same change alters the frame length.
	plain := gooseServerFromCID(t)
	hub2 := goose.NewHub()
	rx = hub2.NewPort(goose.MAC{})
	if _, err := plain.EnableGOOSE(GOOSEOptions{Transport: hub2.NewPort(goose.MAC{})}); err != nil {
		t.Fatal(err)
	}
	g1, m1 := next(func(*goose.Frame) bool { return true })
	plain.SetValue(ctx, "InteropIEDInteropLD/MMTR1$ST$TotVAh$actVal", mms.NewUnsigned(0xfedcba98))
	_, m2 := next(func(f *goose.Frame) bool { return f.PDU.StNum > g1.PDU.StNum })
	if m1 == m2 {
		t.Errorf("BER frame sizes unexpectedly equal (%d)", m1)
	}
}
