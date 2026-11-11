// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/go-mms"
)

var specDSMembers = []DataSetMember{
	{Ref: Ref{LD: ctlLD, LN: "GGIO1", Path: []string{"SetInt1", "setVal"}, FC: FCSP}},
	{Ref: Ref{LD: ctlLD, LN: "GGIO1", Path: []string{"SPS1", "stVal"}, FC: FCST}},
}

// DS-01, DS-03: a non-persistent data set of the association (IEC
// 61850-7-2 13.2.2.2 "@DataSetName", IEC 61850-8-1 8.1.3.2.2 aa-specific)
// and SetDataSetValues (IEC 61850-7-2 13.3.3, IEC 61850-8-1 8.1.3.4.3.3).
func TestSpec_7_2_13_AssociationDataSet(t *testing.T) {
	srv := controlServer(t, ControlHandler{})
	a, b := controlClient(t, srv), controlClient(t, srv)
	ctx := waitCtx(t)

	if err := a.CreateDataSet(ctx, "", "@dsA", specDSMembers); err != nil {
		t.Fatalf("CreateDataSet @dsA: %v", err)
	}
	ds, err := a.GetDataSet(ctx, "", "@dsA")
	if err != nil || len(ds.Members) != 2 || ds.Reference != "@dsA" || !ds.Deletable {
		t.Fatalf("GetDataSet = %+v, %v", ds, err)
	}
	if _, err := b.GetDataSet(ctx, "", "@dsA"); err == nil {
		t.Error("another association sees @dsA")
	}

	errs, err := a.WriteDataSet(ctx, "", "@dsA", []*mms.Value{mms.NewInteger(17), mms.NewBoolean(true)})
	if err != nil || len(errs) != 2 {
		t.Fatalf("WriteDataSet = %v, %v", errs, err)
	}
	if errs[0] != nil {
		t.Errorf("SP member: %v", errs[0])
	}
	var dae *DataAccessError
	if !errors.As(errs[1], &dae) || dae.ErrorCode == 0 {
		t.Errorf("ST member write = %v, want a data access error", errs[1])
	}
	vals, err := a.ReadDataSet(ctx, "", "@dsA")
	if err != nil || len(vals) != 2 || vals[0].Err != nil {
		t.Fatalf("ReadDataSet = %+v, %v", vals, err)
	}
	if i, _ := vals[0].Value.MMS().Int64(); i != 17 {
		t.Errorf("setVal = %d, want 17", i)
	}

	if err := a.DeleteDataSet(ctx, "", "@dsA"); err != nil {
		t.Fatalf("DeleteDataSet: %v", err)
	}
	if _, err := a.GetDataSet(ctx, "", "@dsA"); err == nil {
		t.Error("@dsA still there after delete")
	}

	// A persistent data set by name, and SetDataSetValues on it.
	if err := a.CreateDataSet(ctx, ctlLD, "LLN0$dsP", specDSMembers[:1]); err != nil {
		t.Fatal(err)
	}
	if errs, err := b.WriteDataSet(ctx, ctlLD, "LLN0$dsP", []*mms.Value{mms.NewInteger(5)}); err != nil || errs[0] != nil {
		t.Errorf("WriteDataSet persistent = %v, %v", errs, err)
	}
}

// DS-02: CreateDataSet checks the name and members before any request
// (IEC 61850-7-2 22.2; IEC 61850-8-1 8.1.3.2.2, 7.3.1 NOTE 2).
func TestSpec_7_2_22_2_CreateDataSetValidation(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	ctx := waitCtx(t)
	long := strings.Repeat("a", 33)
	bad := DataSetMember{Ref: Ref{LD: ctlLD, LN: "GGIO1", Path: []string{"SPS1", "stVal"}, FC: "XX"}}
	longItem := DataSetMember{DomainID: ctlLD, ItemID: "GGIO1$ST$" + strings.Repeat("x", 60)}
	for name, tc := range map[string]struct {
		ld, ds  string
		members []DataSetMember
	}{
		"33 characters":    {ctlLD, "LLN0$" + long, specDSMembers},
		"digit first":      {ctlLD, "LLN0$1ds", specDSMembers},
		"bad character":    {ctlLD, "LLN0$ds-1", specDSMembers},
		"@ digit first":    {"", "@9ds", specDSMembers},
		"empty after @":    {"", "@", specDSMembers},
		"empty ld":         {"", "LLN0$ds", specDSMembers},
		"FC XX":            {ctlLD, "LLN0$ds", []DataSetMember{bad}},
		"member item > 64": {ctlLD, "LLN0$ds", []DataSetMember{longItem}},
	} {
		if err := c.CreateDataSet(ctx, tc.ld, tc.ds, tc.members); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: %v, want ErrInvalidArgument", name, err)
		}
	}
	for _, f := range []func() error{
		func() error { _, err := c.WriteDataSet(ctx, ctlLD, "LLN0$ds", nil); return err },
		func() error {
			_, err := c.WriteDataSet(ctx, "", "LLN0$ds", []*mms.Value{mms.NewBoolean(true)})
			return err
		},
		func() error { return c.DeleteDataSet(ctx, "", "") },
		func() error { _, err := c.ReadDataSet(ctx, "", "@"); return err },
	} {
		if err := f(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%v, want ErrInvalidArgument", err)
		}
	}
	if _, err := c.WriteDataSet(ctx, ctlLD, "LLN0$none", []*mms.Value{mms.NewBoolean(true)}); err == nil {
		t.Error("WriteDataSet of a missing data set succeeded")
	}
}
