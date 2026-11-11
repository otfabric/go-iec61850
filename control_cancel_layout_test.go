// SPDX-License-Identifier: MIT

package iec61850

import (
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// IEC 61850-8-1 Table 88 defines Cancel without Check, but SCL files
// commonly reuse the Oper type for Cancel (the iec61850-interop fixture
// does). The server checks a Cancel against its own model, so the client
// follows the declared Cancel type: with a Check member it sends one.
func TestSpec_8_1_Table88_CancelFollowsServerType(t *testing.T) {
	doc, err := scl.ParseFile("interop/testdata/interop.icd")
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
	if err := srv.RegisterControl(ctlLD, "GGIO1.SPCSO3", CtlModelSBOEnhanced, ControlHandler{}); err != nil {
		t.Fatal(err)
	}
	c := controlClient(t, srv)
	ctx := waitCtx(t)
	ref := Ref{LD: ctlLD, LN: "GGIO1", Path: []string{"SPCSO3"}}
	if err := c.SelectWithValue(ctx, ref, OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 7}); err != nil {
		t.Fatalf("SelectWithValue: %v", err)
	}
	if err := c.Cancel(ctx, ref, CancelParams{CtlVal: BoolCtlVal(true), CtlNum: 7}); err != nil {
		t.Fatalf("Cancel with the Oper-typed Cancel of the model: %v", err)
	}
}
