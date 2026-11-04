// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// TestE2E_SCLCorpusReadEveryFC serves every IED of every SCL file in the
// test corpus (vendor CIDs, the interop models, the examples) and reads
// each logical node's functional-constraint structure (LN$FC, IEC
// 61850-8-1 clause 8.1.3.2) over an MMS association before anything is
// written. Every read must return a value: an unwritten attribute is
// answered with its type's default, which must be encodable. go-mms
// v1.3.0 refused the default of a UtcTime, so a read of LLN0$ST (Mod.t)
// failed with object-access-unsupported; this test reads every type the
// corpus uses.
func TestE2E_SCLCorpusReadEveryFC(t *testing.T) {
	var files []string
	for _, root := range []string{"scl/testdata", "interop/testdata"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".cid", ".icd", ".iid", ".scd":
				if filepath.Base(p) != "broken.scd" {
					files = append(files, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) < 10 {
		t.Fatalf("corpus has %d SCL files, want at least 10", len(files))
	}
	if testing.Short() {
		files = files[:3]
	}

	served := 0
	for _, path := range files {
		doc, err := scl.ParseFile(path)
		if err != nil {
			if strings.Contains(err.Error(), "unsupported schema version") {
				t.Logf("%s: skipped, schema version not supported (KNOWN_LIMITATIONS.md): %v", path, err)
				continue
			}
			t.Fatalf("%s: parse: %v", path, err)
		}
		for _, ied := range doc.IEDs {
			for _, ap := range ied.AccessPoints {
				if ap.Server == nil || len(ap.Server.LDevices) == 0 {
					continue
				}
				served++
				t.Run(filepath.Base(path)+"/"+ied.Name+"/"+ap.Name, func(t *testing.T) {
					t.Parallel()
					model, err := NewServerModelFromSCL(doc, ied.Name, ap.Name)
					if err != nil {
						t.Fatalf("NewServerModelFromSCL: %v", err)
					}
					readEveryFC(t, model)
				})
			}
		}
	}
	if served == 0 {
		t.Fatal("no model of the corpus was served")
	}
}

func readEveryFC(t *testing.T, model *servermodel.Model) {
	t.Helper()
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	c := dialModel(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	domains, err := c.GetNameListAll(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClassDomain})
	if err != nil {
		t.Fatalf("domains: %v", err)
	}
	if len(domains) == 0 {
		t.Fatal("server lists no logical device")
	}
	reads := 0
	for _, dom := range domains {
		names, err := c.GetNameListAll(ctx, mms.NameListRequest{
			ObjectClass: mms.ObjectClassNamedVariable,
			Scope:       mms.ObjectScopeDomain,
			DomainID:    mms.DomainID(dom),
		})
		if err != nil {
			t.Fatalf("%s: variables: %v", dom, err)
		}
		for _, n := range names {
			if strings.Count(n, "$") != 1 {
				continue // read LN$FC: it holds every attribute below it
			}
			res, err := c.ReadMultiple(ctx, []mms.ObjectName{{
				Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(dom), ItemID: mms.ItemID(n),
			}})
			if err != nil {
				t.Fatalf("%s/%s: %v", dom, n, err)
			}
			if len(res) != 1 || res[0].Value == nil {
				code := mms.DataAccessErrorCode(-1)
				if len(res) == 1 {
					code = res[0].ErrorCode
				}
				t.Errorf("%s/%s: no value, data access error %v", dom, n, code)
				continue
			}
			reads++
		}
	}
	if reads == 0 {
		t.Fatal("no LN$FC variable read")
	}
}

// dialModel serves model on an in-memory transport pair and returns the
// MMS client of an association with it.
func dialModel(t *testing.T, srv *Server) *mms.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	c, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("associate: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer ccancel()
		_ = c.Close(cctx)
	})
	return c
}

// TestE2E_SCLModelRejectsHostileWrites: on a server built from SCL, a
// client must not overwrite status or measurements (functional
// constraints ST and MX are read-only, IEC 61850-7-2 clause 6.4 / Table
// of functional constraints), nor write a value of the wrong type. A
// refused write leaves the value as it was.
func TestE2E_SCLModelRejectsHostileWrites(t *testing.T) {
	doc, err := scl.ParseFile("interop/testdata/interop.icd")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "AP1")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	c := dialModel(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const dom = "InteropLD"

	write := func(item string, v *mms.Value) error {
		_, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: mms.ItemID(item), Value: v})
		return err
	}
	read := func(item string) *mms.Value {
		res, err := c.ReadMultiple(ctx, []mms.ObjectName{{Scope: mms.ObjectScopeDomain, Domain: dom, ItemID: mms.ItemID(item)}})
		if err != nil || len(res) != 1 || res[0].Value == nil {
			t.Fatalf("read %s: %v %+v", item, err, res)
		}
		return res[0].Value
	}

	for _, tc := range []struct {
		item string
		v    *mms.Value
	}{
		{"GGIO1$ST$SPCSO1$stVal", mms.NewBoolean(true)},                    // status
		{"LLN0$ST$Mod$stVal", mms.NewInteger(5)},                           // status
		{"GGIO1$ST$SPCSO1$q", mms.NewBitStringWithLength([]byte{0, 0}, 13)}, // quality
		{"LLN0$ST$Mod$t", mms.NewUTCTime(time.Unix(1, 0))},                 // timestamp
	} {
		before := read(tc.item)
		if err := write(tc.item, tc.v); err == nil {
			t.Errorf("write to read-only %s accepted", tc.item)
		}
		if after := read(tc.item); !after.Equal(before) {
			t.Errorf("%s changed by a refused write: %v -> %v", tc.item, before, after)
		}
	}
	// Wrong type to a writable configuration attribute.
	if err := write("LLN0$CF$Mod$ctlModel", mms.NewVisibleString("direct")); err == nil {
		t.Error("write of a VisibleString to an Enum accepted")
	}
}
