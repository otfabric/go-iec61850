// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// A write shaped like a control command (LN$CO$DO$Oper, SBOw, Cancel) on an
// object that has no control structure must not be taken for a control:
// it is answered object-non-existent and the status is left alone, even
// when the server lets clients write read-only functional constraints.
// dscsystems/go-iec61850 before v0.6.6 ran such a write as a direct
// control and wrote its ctlVal into stVal (its #35); this keeps
// go-iec61850 from the same mistake.
func TestServer_ForgedControlWriteOnStatusObject(t *testing.T) {
	ctx := context.Background()
	doc, err := scl.ParseFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{AllowReadOnlyFCWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	const ld = "InteropIEDInteropLD"
	if err := srv.RegisterControl(ld, "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{}); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	clientT, serverT := loopbackPair(t)
	go func() { _ = srv.Serve(sctx, serverT) }()
	mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mc.Close(ctx) }()

	oper := mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true), // ctlVal
		mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString(nil)}), // origin
		mms.NewUnsigned(1),                       // ctlNum
		mms.NewUTCTime(time.Now()),               // T
		mms.NewBoolean(false),                    // Test
		mms.NewBitStringWithLength([]byte{0}, 2), // Check
	})
	for _, item := range []string{"GGIO1$CO$SPS1$Oper", "GGIO1$CO$SPS1$SBOw", "GGIO1$CO$SPS1$Cancel", "GGIO1$CO$DPS1$Oper"} {
		_, err := mc.Write(ctx, mms.WriteRequest{DomainID: ld, ItemID: mms.ItemID(item), Value: oper})
		var dae *mms.DataAccessError
		if !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectNonExistent {
			t.Errorf("write %s: err = %v, want object-non-existent", item, err)
		}
	}

	res, err := mc.ReadMultiple(ctx, []mms.ObjectName{{Scope: mms.ObjectScopeDomain, Domain: ld, ItemID: "GGIO1$ST$SPS1$stVal"}})
	if err != nil || len(res) != 1 || res[0].Value == nil {
		t.Fatalf("read SPS1.stVal: %v %v", res, err)
	}
	if b, _ := res[0].Value.Bool(); b {
		t.Error("SPS1.stVal became true: a forged Oper reached the status")
	}

	// The real control still works.
	_, err = mc.Write(ctx, mms.WriteRequest{DomainID: ld, ItemID: "GGIO1$CO$SPCSO1$Oper", Value: oper})
	if err != nil {
		t.Errorf("Oper of SPCSO1: %v", err)
	}
}
