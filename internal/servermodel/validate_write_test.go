// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

func daeCode(err error) mms.DataAccessErrorCode {
	var dae *mms.DataAccessError
	if errors.As(err, &dae) {
		return dae.Code
	}
	return mms.DataAccessErrorNone
}

// IEC 61850-8-1 8.1.3.4.4.2 / Table 23: a value of the wrong type or
// size is type-inconsistent.
func TestSpec_8_1_8_1_3_4_4_2_CheckWriteValue(t *testing.T) {
	oper := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: []mms.TypeSpecElement{
		{Name: "ctlVal", Type: mms.TypeSpec{Type: mms.ValueTypeBoolean}},
		{Name: "ctlNum", Type: mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 8}},
	}}
	arr := mms.TypeSpec{Type: mms.ValueTypeArray, Count: 2, Element: &mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}}
	tests := []struct {
		name string
		val  *mms.Value
		ts   mms.TypeSpec
		want mms.DataAccessErrorCode
	}{
		{"nil", nil, btypeToMMS["INT32"], mms.DataAccessErrorTypeInconsistent},
		{"bool into int32", mms.NewBoolean(true), btypeToMMS["INT32"], mms.DataAccessErrorTypeInconsistent},
		{"int8 range", mms.NewInteger(128), btypeToMMS["INT8"], mms.DataAccessErrorTypeInconsistent},
		{"int8 min", mms.NewInteger(-128), btypeToMMS["INT8"], mms.DataAccessErrorNone},
		{"int8u range", mms.NewUnsigned(256), btypeToMMS["INT8U"], mms.DataAccessErrorTypeInconsistent},
		{"int24u ok", mms.NewUnsigned(1 << 23), btypeToMMS["INT24U"], mms.DataAccessErrorNone},
		{"string too long", mms.NewVisibleString(strings.Repeat("x", 65)), btypeToMMS["VisString64"], mms.DataAccessErrorTypeInconsistent},
		{"bit-string length", mms.NewBitStringWithLength([]byte{0x80}, 3), btypeToMMS["Dbpos"], mms.DataAccessErrorTypeInconsistent},
		{"struct ok", mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewUnsigned(3)}), oper, mms.DataAccessErrorNone},
		{"struct count", mms.NewStructure([]*mms.Value{mms.NewBoolean(true)}), oper, mms.DataAccessErrorTypeInconsistent},
		{"struct member type", mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewVisibleString("5")}), oper, mms.DataAccessErrorTypeInconsistent},
		{"struct member range", mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewUnsigned(300)}), oper, mms.DataAccessErrorTypeInconsistent},
		{"array ok", mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}), arr, mms.DataAccessErrorNone},
		{"array count", mms.NewArray([]*mms.Value{mms.NewInteger(1)}), arr, mms.DataAccessErrorTypeInconsistent},
		{"array element", mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)}), arr, mms.DataAccessErrorTypeInconsistent},
		{"named type", mms.NewBoolean(true), mms.TypeSpec{TypeName: &mms.ObjectName{ItemID: "T"}}, mms.DataAccessErrorNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckWriteValue(tc.val, tc.ts)
			if got := daeCode(err); got != tc.want {
				t.Errorf("code = %v (%v), want %v", got, err, tc.want)
			}
		})
	}
}

// IEC 61850-8-1 8.1.2.3: a CODED ENUM is a bit-string sized for its
// largest value; Tcmd (stop | lower | higher | reserved) is BIT STRING(2).
func TestSpec_8_1_8_1_2_3_TcmdIsTwoBitBitString(t *testing.T) {
	if ts := btypeToMMS["Tcmd"]; ts.Type != mms.ValueTypeBitString || ts.Size != 2 {
		t.Fatalf("Tcmd = %+v, want BIT STRING(2)", ts)
	}
	for lit, want := range map[string]byte{"stop": 0x00, "lower": 0x40, "higher": 0x80, "reserved": 0xc0, "2": 0x80, "0": 0x00} {
		v, err := parseInitialValue("Tcmd", lit, nil)
		if err != nil {
			t.Fatalf("%s: %v", lit, err)
		}
		b, _ := v.BitString()
		if n, _ := v.BitStringLength(); n != 2 || b[0] != want {
			t.Errorf("%s = %x/%d, want %x/2", lit, b, n, want)
		}
	}
	for _, bad := range []string{"4", "up", "-1"} {
		if _, err := parseInitialValue("Tcmd", bad, nil); err == nil {
			t.Errorf("Tcmd %q accepted", bad)
		}
	}
}

// The remaining IEC 61850-6 basic types register, and take initial values.
func TestSpec_6_BasicTypesMapped(t *testing.T) {
	for _, bt := range []string{"INT24", "INT24U", "Currency", "PhyComAddr", "SvOptFlds", "EntryID"} {
		if _, ok := btypeToMMS[bt]; !ok {
			t.Errorf("bType %s not mapped", bt)
		}
	}
	cases := map[string]string{"INT24": "-5", "INT24U": "5", "Currency": "EUR", "SvOptFlds": "1", "EntryID": "0102030405060708"}
	for bt, lit := range cases {
		v, err := parseInitialValue(bt, lit, nil)
		if err != nil {
			t.Errorf("%s %q: %v", bt, lit, err)
			continue
		}
		if err := CheckWriteValue(v, btypeToMMS[bt]); err != nil {
			t.Errorf("%s initial value does not fit its type: %v", bt, err)
		}
	}
	if ts := btypeToMMS["PhyComAddr"]; len(ts.Elements) != 4 || ts.DefaultValue() == nil {
		t.Errorf("PhyComAddr = %+v", ts)
	}
}

// Writes over MMS: refusals carry the IEC 61850-8-1 Table 23 code; CO
// components and SBO are not writable (IEC 61850-8-1 20.5-20.8), the
// whole Oper is.
func TestSpec_8_1_Table23_WriteRefusalCodes(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10},
	})
	m := &Model{LogicalDevices: []LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []LogicalNode{{
			Name: "GGIO1", LNClass: "GGIO",
			DataObjects: []DataObject{{Name: "Do1", Attributes: []DataAttribute{
				{Name: "num", FC: "CF", BType: "INT32"},
				{Name: "str", FC: "DC", BType: "VisString64"},
				{Name: "ctlModel", FC: "CF", BType: "Enum", EnumValues: []int{0, 1, 2, 3, 4}},
				{Name: "cmp", FC: "SP", Children: []DataAttribute{{Name: "i", BType: "INT8"}}},
				{Name: "SBO", FC: "CO", BType: "VisString65"},
				{Name: "Oper", FC: "CO", Children: []DataAttribute{
					{Name: "ctlVal", BType: "BOOLEAN"},
					{Name: "origin", Children: []DataAttribute{{Name: "orCat", BType: "Enum"}, {Name: "orIdent", BType: "Octet64"}}},
				}},
			}}},
		}},
	}}}
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	var intercepted []string
	vs.SetWriteInterceptor(func(_ context.Context, key string, _ *mms.Value) (bool, error) {
		intercepted = append(intercepted, key)
		return false, nil
	})
	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	oper := mms.NewStructure([]*mms.Value{mms.NewBoolean(true), mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString(nil)})})
	tests := []struct {
		item string
		val  *mms.Value
		want mms.DataAccessErrorCode
	}{
		{"GGIO1$CF$Do1$num", mms.NewBoolean(true), mms.DataAccessErrorTypeInconsistent},
		{"GGIO1$DC$Do1$str", mms.NewVisibleString(strings.Repeat("x", 70)), mms.DataAccessErrorTypeInconsistent},
		{"GGIO1$CF$Do1$ctlModel", mms.NewInteger(9), mms.DataAccessErrorObjectValueInvalid},
		{"GGIO1$CF$Do1$ctlModel", mms.NewInteger(2), mms.DataAccessErrorNone},
		{"GGIO1$SP$Do1$cmp", mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}), mms.DataAccessErrorTypeInconsistent},
		{"GGIO1$SP$Do1$cmp", mms.NewStructure([]*mms.Value{mms.NewInteger(1)}), mms.DataAccessErrorNone},
		{"GGIO1$CO$Do1$SBO", mms.NewVisibleString("x"), mms.DataAccessErrorObjectAccessDenied},
		{"GGIO1$CO$Do1$Oper$ctlVal", mms.NewBoolean(true), mms.DataAccessErrorObjectAccessDenied},
		{"GGIO1$CO$Do1$Oper$origin", mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString(nil)}), mms.DataAccessErrorObjectAccessDenied},
		{"GGIO1$CO$Do1$Oper", mms.NewStructure([]*mms.Value{mms.NewBoolean(true)}), mms.DataAccessErrorTypeInconsistent},
		{"GGIO1$CO$Do1$Oper", oper, mms.DataAccessErrorNone},
	}
	for _, tc := range tests {
		_, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: mms.ItemID(tc.item), Value: tc.val})
		if got := daeCode(err); got != tc.want {
			t.Errorf("%s: code %v (%v), want %v", tc.item, got, err, tc.want)
		}
	}
	if got := vs.Get(StoreKey("LD1", "GGIO1$CO$Do1$Oper")); got == nil {
		t.Error("accepted Oper not stored")
	}
	if !strings.Contains(strings.Join(intercepted, " "), "GGIO1$CO$Do1$Oper") {
		t.Errorf("interceptor not called for Oper: %v", intercepted)
	}
}

// A CO write the interceptor (the control model) refuses is not stored.
func TestRegister_RefusedControlWriteNotStored(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10},
	})
	m := &Model{LogicalDevices: []LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []LogicalNode{{
			Name: "GGIO1", LNClass: "GGIO",
			DataObjects: []DataObject{{Name: "Do1", Attributes: []DataAttribute{
				{Name: "Oper", FC: "CO", Children: []DataAttribute{{Name: "ctlVal", BType: "BOOLEAN"}}},
			}}},
		}},
	}}}
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	vs.SetWriteInterceptor(func(context.Context, string, *mms.Value) (bool, error) {
		return true, &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
	})
	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })
	_, err = client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: "GGIO1$CO$Do1$Oper", Value: mms.NewStructure([]*mms.Value{mms.NewBoolean(true)})})
	if daeCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Fatalf("write = %v", err)
	}
	if vs.Get(StoreKey("LD1", "GGIO1$CO$Do1$Oper")) != nil {
		t.Error("refused Oper stored")
	}
}

// IEC 61850-8-1 8.1.3.4.4.2: a write that changes a read-only component
// of the SGCB is object-access-denied; an unchanged EditSG write still
// reaches the setting-group engine (a re-select, IEC 61850-7-2 16.3.3.1).
func TestSpec_8_1_SGCBWholeStructureWrite(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10},
	})
	m := &Model{LogicalDevices: []LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			DataObjects: []DataObject{{Name: "Set1", Attributes: []DataAttribute{
				{Name: "setVal", FC: "SE", BType: "INT32"},
				{Name: "cmp", FC: "SE", Children: []DataAttribute{{Name: "i", BType: "INT32"}}},
			}}},
			SettingGroup: &SettingGroupDef{NumOfSGs: 3, ActSG: 1},
		}},
	}}}
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	vs.SetWriteInterceptor(func(_ context.Context, key string, _ *mms.Value) (bool, error) {
		seen = append(seen, key)
		return false, nil
	})
	editAllowed := false
	var guarded []string
	vs.SetEditGuard(func(_ context.Context, key string, write bool) error {
		guarded = append(guarded, key)
		if !editAllowed {
			return &mms.DataAccessError{Code: mms.DataAccessErrorTemporarilyUnavailable}
		}
		return nil
	})
	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	read, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$SP$SGCB"})
	if err != nil {
		t.Fatal(err)
	}
	elems, _ := read.Value.Structure()
	// Written back unchanged (LActTm went over the wire): only EditSG
	// reaches the interceptor.
	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: "LLN0$SP$SGCB", Value: read.Value}); err != nil {
		t.Fatalf("unchanged write: %v", err)
	}
	if len(seen) != 1 || !strings.HasSuffix(seen[0], "$EditSG") {
		t.Errorf("intercepted = %v, want EditSG only", seen)
	}
	changed := append([]*mms.Value(nil), elems...)
	changed[0] = mms.NewUnsigned(9) // NumOfSG
	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: "LLN0$SP$SGCB", Value: mms.NewStructure(changed)}); daeCode(err) != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("NumOfSG change = %v, want object-access-denied", err)
	}

	// FC=SE leaf and compound accesses consult the edit guard first.
	for _, item := range []string{"LLN0$SE$Set1$setVal", "LLN0$SE$Set1$cmp"} {
		if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: mms.ItemID(item)}); daeCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("%s read refused = %v", item, err)
		}
	}
	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: "LLN0$SE$Set1$setVal", Value: mms.NewInteger(3)}); daeCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("SE write refused = %v", err)
	}
	if vs.Get(StoreKey("LD1", "LLN0$SE$Set1$setVal")) != nil {
		t.Error("refused SE write stored")
	}
	editAllowed = true
	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "LD1", ItemID: "LLN0$SE$Set1$cmp", Value: mms.NewStructure([]*mms.Value{mms.NewInteger(3)})}); err != nil {
		t.Errorf("SE compound write: %v", err)
	}
	if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$SE$Set1$cmp"}); err != nil {
		t.Errorf("SE compound read: %v", err)
	}
	if len(guarded) < 5 {
		t.Errorf("edit guard calls = %v", guarded)
	}
	if sameSGCBMember(mms.NewUTCTime(time.Unix(5, 0)), mms.NewBoolean(true)) {
		t.Error("UtcTime equal to a boolean")
	}
}
