// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestValue_TypeMismatchAndNilGuards(t *testing.T) {
	wrong := BoolValue(true)
	for _, fn := range []func() error{
		func() error { _, err := wrong.Int64(); return err },
		func() error { _, err := wrong.Uint32(); return err },
		func() error { _, err := wrong.Uint64(); return err },
		func() error { _, err := wrong.Float32(); return err },
		func() error { _, err := wrong.Float64(); return err },
		func() error { _, err := wrong.OctetString(); return err },
	} {
		if err := fn(); err == nil {
			t.Fatal("expected type mismatch")
		}
	}

	var nilV *Value
	if nilV.IsStructure() || nilV.IsArray() {
		t.Fatal("nil value guards")
	}
	empty := &Value{}
	if empty.IsStructure() || empty.IsArray() {
		t.Fatal("nil mmsVal guards")
	}
	if err := nilV.typeError("x"); err == nil {
		t.Fatal("nil typeError")
	}
}

func TestClient_IEDNameHelpers(t *testing.T) {
	c := &Client{iedName: "IED1"}
	if got := c.ldDomain("LD0"); got != "IED1LD0" {
		t.Fatalf("ldDomain=%q", got)
	}
	if got := (&Client{}).ldDomain("LD0"); got != "LD0" {
		t.Fatalf("ldDomain no prefix=%q", got)
	}

	ref := Ref{LD: "LD0", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	dom, item, err := c.refToMMS(ref)
	if err != nil || dom != "IED1LD0" || item == "" {
		t.Fatalf("refToMMS: %q %q %v", dom, item, err)
	}
	if _, _, err := c.refToMMS(Ref{}); err == nil {
		t.Fatal("invalid ref")
	}
	if got := c.stripIEDPrefix("IED1LD0"); got != "LD0" {
		t.Fatalf("strip=%q", got)
	}
	if got := c.stripIEDPrefix("OTHER"); got != "OTHER" {
		t.Fatalf("strip no match=%q", got)
	}
	if got := (&Client{}).stripIEDPrefix("IED1LD0"); got != "IED1LD0" {
		t.Fatalf("strip empty ied=%q", got)
	}
}

func TestWrite_Edges(t *testing.T) {
	client := setupWritableLoopback(t)
	ctx := context.Background()

	// Validate failure (illegal separator in LD).
	if err := client.Write(ctx, Ref{LD: "bad/ld", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}, mms.NewInteger(1)); err == nil {
		t.Fatal("validate")
	}
	// Incomplete object ref.
	if err := client.Write(ctx, Ref{LD: "x"}, mms.NewInteger(1)); err == nil {
		t.Fatal("invalid ref")
	}
	// Closed client.
	_ = client.Close(ctx)
	ref := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}
	if err := client.Write(ctx, ref, mms.NewInteger(1)); err == nil {
		t.Fatal("closed")
	}

	// Fresh client for MMS write failure (unknown object).
	client = setupWritableLoopback(t)
	bad := Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"NoSuch", "stVal"}, FC: FCST}
	if err := client.Write(ctx, bad, mms.NewInteger(1)); err == nil {
		t.Fatal("expected MMS write error")
	}
}

func TestParseCOAndRCBStoreKeys(t *testing.T) {
	if _, _, _, ok := parseCOStoreKey("GGIO1$ST$Ind1$stVal"); ok {
		t.Fatal("non-CO")
	}
	if _, _, _, ok := parseCOStoreKey("GGIO1$CO$"); ok {
		t.Fatal("empty after CO")
	}
	if _, _, _, ok := parseCOStoreKey("GGIO1$CO$SPCSO1"); ok {
		t.Fatal("too few segs")
	}
	if _, _, _, ok := parseCOStoreKey("GGIO1$CO$SPCSO1$Oper$ctlVal"); ok {
		t.Fatal("sub-BDA should not match")
	}
	ln, path, sub, ok := parseCOStoreKey("GGIO1$CO$SPCSO1$Oper")
	if !ok || ln != "GGIO1" || sub != "Oper" || len(path) != 2 {
		t.Fatalf("%q %v %q %v", ln, path, sub, ok)
	}
	for _, svc := range []string{"SBO", "SBOw", "Cancel"} {
		_, _, got, ok := parseCOStoreKey("GGIO1$CO$SPCSO1$" + svc)
		if !ok || got != svc {
			t.Fatalf("%s: %q %v", svc, got, ok)
		}
	}

	if _, _, ok := parseRCBStoreKey("LLN0$ST$Mod$stVal"); ok {
		t.Fatal("non-RCB")
	}
	if _, _, ok := parseRCBStoreKey("LLN0$RP$urcbA"); ok {
		t.Fatal("no subfield")
	}
	rcb, sub, ok := parseRCBStoreKey("LLN0$RP$urcbA$RptEna")
	if !ok || rcb != "LLN0$RP$urcbA" || sub != "RptEna" {
		t.Fatalf("%q %q %v", rcb, sub, ok)
	}
	rcb, sub, ok = parseRCBStoreKey("LLN0$BR$brcbB$DatSet")
	if !ok || rcb != "LLN0$BR$brcbB" || sub != "DatSet" {
		t.Fatalf("BR: %q %q %v", rcb, sub, ok)
	}
}

func TestWriteInterceptor_BadKey_CO_SGCB(t *testing.T) {
	ctx := context.Background()

	// Bad store key (no LD/item separator) — interceptor returns (false, nil).
	model := testControlSCL()
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handled, err := srv.store.CallInterceptorForTest(ctx, "noshslash", mms.NewBoolean(true))
	if handled || err != nil {
		t.Fatalf("bad key: handled=%v err=%v", handled, err)
	}

	// CO path: registered control Oper write is dispatched.
	var operated bool
	if err := srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{
		OnOperate: func(_ context.Context, _ ControlRequest) error {
			operated = true
			return nil
		},
	}); err != nil {
		t.Fatalf("RegisterControl: %v", err)
	}
	operVal := buildOper(OperateParams{CtlVal: BoolCtlVal(true), CtlNum: 1})
	handled, err = srv.store.CallInterceptorForTest(ctx, "LD1/GGIO1$CO$SPCSO1$Oper", operVal)
	if err != nil {
		t.Fatalf("CO interceptor: %v", err)
	}
	if !handled {
		t.Fatal("CO write should be handled")
	}
	if !operated {
		t.Fatal("OnOperate should have been called via interceptor")
	}

	// SGCB path via EnableSettingGroups.
	sgModel := testSGCBModel()
	sgSrv, err := NewServer(sgModel, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer SGCB: %v", err)
	}
	sgSrv.EnableSettingGroups(SettingGroupHandler{})
	handled, err = sgSrv.store.CallInterceptorForTest(ctx, "LD1/LLN0$SP$SGCB$EditSG", mms.NewUnsigned(2))
	if err != nil {
		t.Fatalf("SGCB interceptor: %v", err)
	}
	if !handled {
		t.Fatal("SGCB write should be handled")
	}
	if got := sgSrv.SettingGroupEngine().GetEditSettingGroup("LD1"); got != 2 {
		t.Errorf("EditSG = %d, want 2", got)
	}
}
