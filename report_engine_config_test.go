// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// Configuration writes on a disabled BRCB: each attribute checks its
// type, stores the value, and a change (not a rewrite of the same
// value) purges the buffer (IEC 61850-8-1 §17.1.2).
func TestReportEngine_ConfigWrites(t *testing.T) {
	srv := newTestServer(t, testReportSCL())
	engine := srv.EnableReports()
	defer engine.Stop()
	ctx := context.Background()
	const item = "LLN0$BR$brcb01"
	write := func(attr string, v *mms.Value) error {
		return engine.HandleRCBWrite(ctx, "LD1", item, attr, v)
	}
	stored := func(attr string) *mms.Value {
		return srv.store.Get(servermodel.StoreKey("LD1", item+"$"+attr))
	}

	for _, tc := range []struct {
		attr string
		val  *mms.Value
	}{
		{"RptID", mms.NewInteger(1)},
		{"BufTm", mms.NewBoolean(true)},
		{"IntgPd", mms.NewVisibleString("x")},
		{"DatSet", mms.NewInteger(1)},
	} {
		var dae *mms.DataAccessError
		if err := write(tc.attr, tc.val); err == nil || !errors.As(err, &dae) {
			t.Errorf("%s with a wrong type: %v", tc.attr, err)
		}
	}

	if err := write("RptID", mms.NewVisibleString("newID")); err != nil {
		t.Fatal(err)
	}
	if s, _ := stored("RptID").VisibleString(); s != "newID" {
		t.Errorf("RptID = %q", s)
	}
	if err := write("RptID", mms.NewVisibleString("newID")); err != nil {
		t.Errorf("same RptID again: %v", err)
	}
	for _, attr := range []string{"BufTm", "IntgPd"} {
		for range 2 {
			if err := write(attr, mms.NewUnsigned(250)); err != nil {
				t.Fatalf("%s: %v", attr, err)
			}
		}
		if u, _ := stored(attr).Uint32(); u != 250 {
			t.Errorf("%s = %d", attr, u)
		}
	}
	if storedString(nil) != "" || storedString(mms.NewInteger(1)) != "" {
		t.Error("storedString of a non-string")
	}
	if _, ok := storedUint32(nil); ok {
		t.Error("storedUint32(nil)")
	}
}

// decodeOrigin is strict about the two members and their types.
func TestDecodeOriginStrict(t *testing.T) {
	ok := mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString([]byte("op"))})
	if o, err := decodeOrigin(ok); err != nil || o.OrCat != 3 || string(o.OrIdent) != "op" {
		t.Fatalf("%+v %v", o, err)
	}
	for name, v := range map[string]*mms.Value{
		"not a structure":  mms.NewInteger(1),
		"one member":       mms.NewStructure([]*mms.Value{mms.NewInteger(1)}),
		"orCat unsigned":   mms.NewStructure([]*mms.Value{mms.NewUnsigned(1), mms.NewOctetString(nil)}),
		"orCat too large":  mms.NewStructure([]*mms.Value{mms.NewInteger(1 << 40), mms.NewOctetString(nil)}),
		"orIdent string":   mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewVisibleString("x")}),
		"orIdent too long": mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewOctetString(make([]byte, 65))}),
	} {
		if _, err := decodeOrigin(v); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
