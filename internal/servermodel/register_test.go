// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestRegisterModel_Basic(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	if vs == nil {
		t.Fatal("expected non-nil ValueStore")
	}
}

func TestRegisterModel_RCBDefaults(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	rptID := vs.Get(StoreKey("LD1", "LLN0$BR$brcbEvents01$RptID"))
	if rptID == nil {
		t.Fatal("expected RptID in value store")
	}
	s2, ok := rptID.VisibleString()
	if !ok {
		t.Fatal("expected VisibleString for RptID")
	}
	if s2 != "rpt01" {
		t.Errorf("RptID = %q, want rpt01", s2)
	}

	rptEna := vs.Get(StoreKey("LD1", "LLN0$BR$brcbEvents01$RptEna"))
	if rptEna == nil {
		t.Fatal("expected RptEna in value store")
	}
	b, ok := rptEna.Bool()
	if !ok || b {
		t.Error("RptEna should be false by default")
	}
}

func TestRegisterModel_RCBSubfields(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	subfields := []string{
		"RptID", "RptEna", "DatSet", "ConfRev", "OptFlds",
		"BufTm", "SqNum", "TrgOps", "IntgPd", "GI",
		"PurgeBuf", "EntryID", "TimeOfEntry",
	}
	for _, sf := range subfields {
		key := StoreKey("LD1", "LLN0$BR$brcbEvents01$"+sf)
		v := vs.Get(key)
		if v == nil {
			t.Errorf("expected subfield %s in value store", sf)
		}
	}

	datSet := vs.Get(StoreKey("LD1", "LLN0$BR$brcbEvents01$DatSet"))
	if datSet == nil {
		t.Fatal("expected DatSet value")
	}
	dsStr, ok := datSet.VisibleString()
	if !ok {
		t.Fatal("expected VisibleString for DatSet")
	}
	if dsStr != "LD1/LLN0$dsEvents" {
		t.Errorf("DatSet = %q, want LD1/LLN0$dsEvents", dsStr)
	}
}

func TestRegisterModel_DomainQualifiedKeys(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	key := StoreKey("LD1", "LLN0$ST$Mod$stVal")
	val := vs.Get(key)
	if val != nil {
		t.Log("DA value found with domain-qualified key (may be nil for default)")
	}

	rawKey := "LLN0$ST$Mod$stVal"
	rawVal := vs.Get(rawKey)
	if rawVal != nil {
		t.Error("non-domain-qualified key should NOT match any store entry")
	}
}

func TestRegisterModel_WithExistingStore(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	preStore := NewValueStore()
	preStore.Set("custom-key", mms.NewInteger(42))

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, preStore)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	if vs != preStore {
		t.Error("expected returned store to be the provided store")
	}

	v := vs.Get("custom-key")
	if v == nil {
		t.Fatal("expected pre-populated value")
	}
	i, ok := v.Int64()
	if !ok || i != 42 {
		t.Errorf("got %d, want 42", i)
	}
}

func TestRegisterModel_InitialValueSeeded(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	// GGIO1.Ind1.stVal has InitialValue "true" from DAI override.
	ggio := m.LogicalDevices[0].LogicalNodes[1]
	found := false
	for _, do := range ggio.DataObjects {
		for _, attr := range do.Attributes {
			if attr.Name == "stVal" && attr.InitialValue == "true" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected stVal with InitialValue='true' in GGIO1.Ind1")
	}

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	key := StoreKey("LD1", "GGIO1$ST$Ind1$stVal")
	val := vs.Get(key)
	if val == nil {
		t.Fatal("expected seeded value for GGIO1$ST$Ind1$stVal")
	}
	b, ok := val.Bool()
	if !ok {
		t.Fatal("expected Boolean value")
	}
	if !b {
		t.Error("expected InitialValue true, got false")
	}
}

func TestRegisterModel_RCBOptFldsFromReportDef(t *testing.T) {
	s := testSCL()
	m, err := FromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("FromSCL: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	optFldsVal := vs.Get(StoreKey("LD1", "LLN0$BR$brcbEvents01$OptFlds"))
	if optFldsVal == nil {
		t.Fatal("expected OptFlds value in store")
	}
	data, ok := optFldsVal.BitString()
	if !ok {
		t.Fatal("expected BitString for OptFlds")
	}
	// The test SCL has SeqNum=true, TimeStamp=true.
	// Per IEC 61850, bit 0 is reserved; SeqNum=bit 1, TimeStamp=bit 2.
	// In the first byte (MSB-first): bit 1 = 0x40, bit 2 = 0x20 → 0x60.
	if len(data) < 1 || data[0]&0x60 != 0x60 {
		t.Errorf("OptFlds data = %x, expected SeqNum+TimeStamp bits set (0x60)", data)
	}

	trgOpsVal := vs.Get(StoreKey("LD1", "LLN0$BR$brcbEvents01$TrgOps"))
	if trgOpsVal == nil {
		t.Fatal("expected TrgOps value in store")
	}
	trgData, ok := trgOpsVal.BitString()
	if !ok {
		t.Fatal("expected BitString for TrgOps")
	}
	// The test SCL has Dchg=true, Qchg=true → bits 1,2 should be set
	// (bit 0 is reserved).
	if len(trgData) < 1 || trgData[0]&0x60 != 0x60 {
		t.Errorf("TrgOps data = %x, expected Dchg+Qchg bits set (0x60)", trgData)
	}
}

func TestParseInitialBitString_RoundTrips(t *testing.T) {
	tests := []struct {
		name   string
		btype  string
		val    string
		bits   int
		verify func(t *testing.T, v *mms.Value)
	}{
		{
			name: "Quality_Good", btype: "Quality", val: "0", bits: 13,
			verify: func(t *testing.T, v *mms.Value) {
				data, ok := v.BitString()
				if !ok {
					t.Fatal("expected BitString")
				}
				if data[0] != 0 && data[1] != 0 {
					t.Errorf("expected all zeros, got %x", data)
				}
			},
		},
		{
			name: "Quality_Invalid", btype: "Quality", val: "2", bits: 13,
			verify: func(t *testing.T, v *mms.Value) {
				data, ok := v.BitString()
				if !ok {
					t.Fatal("expected BitString")
				}
				if data[0]&0x40 == 0 {
					t.Errorf("validity bit 1 should be set for value 2, got %x", data)
				}
			},
		},
		{
			name: "OptFlds_SeqNum", btype: "OptFlds", val: "1", bits: 10,
			verify: func(t *testing.T, v *mms.Value) {
				data, ok := v.BitString()
				if !ok {
					t.Fatal("expected BitString")
				}
				if data[0]&0x80 == 0 {
					t.Errorf("SeqNum bit (0) should be set, got %x", data)
				}
			},
		},
		{
			name: "TrgOps_Dchg", btype: "TrgOps", val: "2", bits: 6,
			verify: func(t *testing.T, v *mms.Value) {
				data, ok := v.BitString()
				if !ok {
					t.Fatal("expected BitString")
				}
				if data[0]&0x40 == 0 {
					t.Errorf("Dchg bit (1) should be set, got %x", data)
				}
			},
		},
		{
			name: "Dbpos_01", btype: "Dbpos", val: "1", bits: 2,
			verify: func(t *testing.T, v *mms.Value) {
				data, ok := v.BitString()
				if !ok {
					t.Fatal("expected BitString")
				}
				if data[0]&0x80 == 0 {
					t.Errorf("bit 0 should be set for value 1, got %x", data)
				}
			},
		},
		{
			name: "Check_Both", btype: "Check", val: "3", bits: 2,
			verify: func(t *testing.T, v *mms.Value) {
				data, ok := v.BitString()
				if !ok {
					t.Fatal("expected BitString")
				}
				if data[0]&0xC0 != 0xC0 {
					t.Errorf("both bits should be set for value 3, got %x", data)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseInitialValue(tc.btype, tc.val, nil)
			if err != nil {
				t.Fatalf("parseInitialValue(%q, %q): %v", tc.btype, tc.val, err)
			}
			bl, ok := v.BitStringLength()
			if !ok {
				t.Fatal("expected BitStringLength to be available")
			}
			if bl != tc.bits {
				t.Errorf("bit length = %d, want %d", bl, tc.bits)
			}
			tc.verify(t, v)
		})
	}
}

func TestValueStore_Concurrent(t *testing.T) {
	vs := NewValueStore()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			vs.Set("key", mms.NewInteger(int64(i)))
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		vs.Get("key")
	}
	<-done
}

// TestValueStore_WriteInterceptor_NotSet verifies that CallInterceptorForTest
// returns (false, nil) when no interceptor has been installed.
func TestValueStore_WriteInterceptor_NotSet(t *testing.T) {
	vs := NewValueStore()
	handled, err := vs.CallInterceptorForTest(context.Background(), "LD1/key", mms.NewBoolean(true))
	if handled || err != nil {
		t.Errorf("no interceptor: got (handled=%v, err=%v), want (false, nil)", handled, err)
	}
}

// TestValueStore_WriteInterceptor_Handled verifies that an interceptor
// returning (true, nil) is correctly forwarded.
func TestValueStore_WriteInterceptor_Handled(t *testing.T) {
	vs := NewValueStore()
	vs.SetWriteInterceptor(func(_ context.Context, key string, val *mms.Value) (bool, error) {
		return true, nil
	})
	handled, err := vs.CallInterceptorForTest(context.Background(), "LD1/key", mms.NewBoolean(true))
	if !handled || err != nil {
		t.Errorf("handled interceptor: got (handled=%v, err=%v), want (true, nil)", handled, err)
	}
}

// TestValueStore_WriteInterceptor_Rejected verifies that an interceptor
// returning (true, err) propagates the error.
func TestValueStore_WriteInterceptor_Rejected(t *testing.T) {
	vs := NewValueStore()
	wantErr := fmt.Errorf("access denied")
	vs.SetWriteInterceptor(func(_ context.Context, key string, val *mms.Value) (bool, error) {
		return true, wantErr
	})
	handled, err := vs.CallInterceptorForTest(context.Background(), "LD1/key", mms.NewBoolean(true))
	if !handled || err != wantErr {
		t.Errorf("rejected interceptor: got (handled=%v, err=%v), want (true, wantErr)", handled, err)
	}
}

// TestValueStore_WriteInterceptor_Passthrough verifies that (false, nil)
// from an interceptor lets the normal write path proceed.
func TestValueStore_WriteInterceptor_Passthrough(t *testing.T) {
	vs := NewValueStore()
	vs.SetWriteInterceptor(func(_ context.Context, key string, val *mms.Value) (bool, error) {
		return false, nil // pass through to normal write
	})
	handled, err := vs.CallInterceptorForTest(context.Background(), "LD1/key", mms.NewBoolean(true))
	if handled || err != nil {
		t.Errorf("passthrough interceptor: got (handled=%v, err=%v), want (false, nil)", handled, err)
	}
}

// TestValueStore_WriteInterceptor_Replace verifies that setting a new
// interceptor replaces the previous one.
func TestValueStore_WriteInterceptor_Replace(t *testing.T) {
	vs := NewValueStore()
	callCount := 0
	vs.SetWriteInterceptor(func(_ context.Context, _ string, _ *mms.Value) (bool, error) {
		callCount++
		return true, nil
	})
	vs.CallInterceptorForTest(context.Background(), "k", mms.NewBoolean(true)) //nolint:errcheck

	// Replace with a new interceptor that always rejects.
	vs.SetWriteInterceptor(func(_ context.Context, _ string, _ *mms.Value) (bool, error) {
		callCount += 10
		return true, fmt.Errorf("replaced")
	})
	_, err := vs.CallInterceptorForTest(context.Background(), "k", mms.NewBoolean(false))
	if err == nil {
		t.Fatal("replaced interceptor should return error")
	}
	if callCount != 11 { // 1 from first interceptor + 10 from replacement
		t.Errorf("callCount = %d, want 11", callCount)
	}
}

type chanTransport struct {
	send chan []byte
	recv chan []byte
}

func (t *chanTransport) Send(_ context.Context, data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	t.send <- cp
	return nil
}

func (t *chanTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case b := <-t.recv:
		return b, nil
	}
}

func (t *chanTransport) Close() error { return nil }

func loopbackPair() (mms.Transport, mms.Transport) {
	c2s := make(chan []byte, 16)
	s2c := make(chan []byte, 16)
	return &chanTransport{send: c2s, recv: s2c}, &chanTransport{send: s2c, recv: c2s}
}

func TestRegisterModel_SGCB(t *testing.T) {
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []DataObject{{
					Name: "Mod",
					Attributes: []DataAttribute{
						{Name: "stVal", FC: "ST", BType: "INT32"},
					},
				}},
				SettingGroup: &SettingGroupDef{NumOfSGs: 4, ActSG: 1, ResvTms: 30},
			}},
		}},
	}

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	for _, sf := range []string{"NumOfSGs", "ActSG", "EditSG", "CnfEdit", "LActTm", "ResvTms"} {
		if vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$"+sf)) == nil {
			t.Errorf("missing SGCB subfield %s", sf)
		}
	}

	num := vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$NumOfSGs"))
	u, ok := num.Uint32()
	if !ok || u != 4 {
		t.Errorf("NumOfSGs = %v, want 4", num)
	}
	act := vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$ActSG"))
	u, ok = act.Uint32()
	if !ok || u != 1 {
		t.Errorf("ActSG = %v, want 1", act)
	}

	// Exercise SGCB Read/Write callbacks via MMS loopback.
	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	parent, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$SP$SGCB"})
	if err != nil || parent == nil || parent.Value == nil {
		t.Fatalf("SGCB parent read: %v", err)
	}
	elems, ok := parent.Value.Structure()
	if !ok || len(elems) != 6 {
		t.Fatalf("SGCB structure len=%d ok=%v", len(elems), ok)
	}

	// DefaultValue path: clear a subfield then re-read parent.
	vs.Set(StoreKey("LD1", "LLN0$SP$SGCB$EditSG"), nil)
	if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$SP$SGCB"}); err != nil {
		t.Fatalf("SGCB parent read after clear: %v", err)
	}
	if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$SP$SGCB$EditSG"}); err != nil {
		t.Fatalf("SGCB subfield DefaultValue read: %v", err)
	}

	// Subfield write (no interceptor) stores the value.
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$SP$SGCB$EditSG", Value: mms.NewUnsigned(2),
	}); err != nil {
		t.Fatalf("EditSG write: %v", err)
	}
	if u, ok := vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$EditSG")).Uint32(); !ok || u != 2 {
		t.Errorf("store EditSG = %v", vs.Get(StoreKey("LD1", "LLN0$SP$SGCB$EditSG")))
	}

	// Interceptor claims the write.
	vs.SetWriteInterceptor(func(context.Context, string, *mms.Value) (bool, error) {
		return true, nil
	})
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$SP$SGCB$ActSG", Value: mms.NewUnsigned(3),
	}); err != nil {
		t.Fatalf("ActSG intercepted write: %v", err)
	}

	// Parent structure write.
	vs.SetWriteInterceptor(nil)
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$SP$SGCB", Value: parent.Value,
	}); err != nil {
		t.Fatalf("SGCB parent write: %v", err)
	}
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$SP$SGCB", Value: mms.NewBoolean(true),
	}); err == nil {
		t.Fatal("expected SGCB parent write type error")
	}
}

func TestMakeStructRead(t *testing.T) {
	read := makeStructRead([]func(context.Context) (*mms.Value, error){
		func(context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
		func(context.Context) (*mms.Value, error) { return mms.NewBoolean(true), nil },
	})
	v, err := read(context.Background())
	if err != nil {
		t.Fatalf("makeStructRead: %v", err)
	}
	members, ok := v.Structure()
	if !ok || len(members) != 2 {
		t.Fatalf("got %v ok=%v", v, ok)
	}

	readErr := makeStructRead([]func(context.Context) (*mms.Value, error){
		func(context.Context) (*mms.Value, error) { return nil, fmt.Errorf("boom") },
	})
	if _, err := readErr(context.Background()); err == nil {
		t.Fatal("expected error from child read")
	}
}

func TestBuildDOElemsForFC_NestedAndEmptyFC(t *testing.T) {
	vs := NewValueStore()
	obj := &DataObject{
		Name: "Parent",
		Attributes: []DataAttribute{
			{Name: "stVal", FC: "ST", BType: "INT32"},
			{Name: "other", FC: "MX", BType: "FLOAT32"},   // skipped for ST
			{Name: "inherited", FC: "", BType: "BOOLEAN"}, // empty FC → use requested fc
		},
		Children: []DataObject{{
			Name: "Child",
			Attributes: []DataAttribute{
				{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
			},
		}},
	}

	elems, reads, err := buildDOElemsForFC(vs, "LD1", "LLN0", nil, obj, "ST")
	if err != nil {
		t.Fatalf("buildDOElemsForFC: %v", err)
	}
	if len(elems) < 3 || len(reads) != len(elems) {
		t.Fatalf("elems=%d reads=%d", len(elems), len(reads))
	}

	// Invoke nested makeStructRead via child DO read.
	v, err := reads[len(reads)-1](context.Background())
	if err != nil {
		t.Fatalf("child read: %v", err)
	}
	if _, ok := v.Structure(); !ok {
		t.Fatal("expected child structure")
	}
}

func TestRegisterDA_CompoundAndUnsupported(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatal(err)
	}
	vs := NewValueStore()

	compound := &DataAttribute{
		Name: "mag",
		FC:   "MX",
		Children: []DataAttribute{
			{Name: "f", BType: "FLOAT32", InitialValue: "1.5"},
		},
	}
	if err := registerDA(srv, vs, "LD1", "MMXU1", []string{"AnIn1"}, compound); err != nil {
		t.Fatalf("registerDA compound: %v", err)
	}
	if vs.Get(StoreKey("LD1", "MMXU1$MX$AnIn1$mag$f")) == nil {
		t.Fatal("expected seeded child value")
	}

	bad := &DataAttribute{Name: "x", FC: "ST", BType: "NotARealType"}
	if err := registerDA(srv, vs, "LD1", "LLN0", []string{"Mod"}, bad); err == nil {
		t.Fatal("expected unsupported BType error")
	}
}

func TestRegisterRCB_URCBAndDefaultRptID(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatal(err)
	}
	vs := NewValueStore()

	// Empty RptID → default "LD1/LLN0$RP$urcb01"; Buffered=false → RP prefix.
	rpt := &ReportDef{
		Name:     "urcb01",
		RptID:    "",
		DatSet:   "ds1",
		ConfRev:  1,
		Buffered: false,
		TrgOps:   TrgOpsDef{Dchg: true, GI: true},
		OptFlds:  OptFieldsDef{SeqNum: true},
	}
	if err := registerRCB(srv, vs, "LD1", "LLN0", rpt); err != nil {
		t.Fatalf("registerRCB: %v", err)
	}

	rptID := vs.Get(StoreKey("LD1", "LLN0$RP$urcb01$RptID"))
	if rptID == nil {
		t.Fatal("expected RptID")
	}
	s, ok := rptID.VisibleString()
	if !ok || s != "LD1/LLN0$RP$urcb01" {
		t.Errorf("default RptID = %q, want LD1/LLN0$RP$urcb01", s)
	}
	if vs.Get(StoreKey("LD1", "LLN0$RP$urcb01$Resv")) == nil {
		t.Error("URCB should seed Resv")
	}
	if vs.Get(StoreKey("LD1", "LLN0$RP$urcb01$PurgeBuf")) != nil {
		t.Error("URCB should not seed PurgeBuf")
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	parent, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$RP$urcb01"})
	if err != nil || parent == nil || parent.Value == nil {
		t.Fatalf("URCB parent read: %v", err)
	}

	vs.Set(StoreKey("LD1", "LLN0$RP$urcb01$RptID"), nil)
	if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$RP$urcb01"}); err != nil {
		t.Fatalf("URCB DefaultValue parent read: %v", err)
	}
	if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$RP$urcb01$RptID"}); err != nil {
		t.Fatalf("URCB subfield DefaultValue: %v", err)
	}

	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$RP$urcb01$RptEna", Value: mms.NewBoolean(true),
	}); err != nil {
		t.Fatalf("RptEna write: %v", err)
	}

	key := StoreKey("LD1", "LLN0$RP$urcb01$RptEna")
	vs.SetWriteInterceptor(func(context.Context, string, *mms.Value) (bool, error) {
		return true, fmt.Errorf("denied")
	})
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$RP$urcb01$RptEna", Value: mms.NewBoolean(false),
	}); err == nil {
		t.Fatal("expected interceptor rejection")
	}
	if b, ok := vs.Get(key).Bool(); !ok || !b {
		t.Error("RptEna should restore previous true after rejection")
	}

	vs.SetWriteInterceptor(nil)
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$RP$urcb01", Value: parent.Value,
	}); err != nil {
		t.Fatalf("URCB parent write: %v", err)
	}
}

func TestRegisterDAWithFC_Compound(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	m := &Model{
		LogicalDevices: []LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []LogicalNode{{
				Name:    "MMXU1",
				LNClass: "MMXU",
				DataObjects: []DataObject{{
					Name: "AnIn1",
					Attributes: []DataAttribute{{
						Name: "mag",
						FC:   "MX",
						Children: []DataAttribute{
							{Name: "f", BType: "FLOAT32", InitialValue: "3.14"},
							{Name: "nested", FC: "", Children: []DataAttribute{
								{Name: "i", BType: "INT32", InitialValue: "7"},
							}},
						},
					}},
				}},
			}},
		}},
	}
	vs, err := RegisterModel(srv, m, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	if vs.Get(StoreKey("LD1", "MMXU1$MX$AnIn1$mag$f")) == nil {
		t.Fatal("expected mag$f")
	}
	if vs.Get(StoreKey("LD1", "MMXU1$MX$AnIn1$mag$nested$i")) == nil {
		t.Fatal("expected mag$nested$i")
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	// Compound DA read/write path via registerDAWithFC.
	compound, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "MMXU1$MX$AnIn1$mag"})
	if err != nil || compound == nil || compound.Value == nil {
		t.Fatalf("compound read: %v", err)
	}
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "MMXU1$MX$AnIn1$mag", Value: compound.Value,
	}); err != nil {
		t.Fatalf("compound write: %v", err)
	}

	// Leaf write validation / interceptor paths on registerDA.
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "MMXU1$MX$AnIn1$mag$f", Value: mms.NewFloat(1.0),
	}); err != nil {
		t.Fatalf("leaf write: %v", err)
	}
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "MMXU1$MX$AnIn1$mag$f", Value: mms.NewBoolean(true),
	}); err == nil {
		t.Fatal("expected type mismatch")
	}
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "MMXU1$MX$AnIn1$mag$f", Value: nil,
	}); err == nil {
		t.Fatal("expected nil value error")
	}

	vs.SetWriteInterceptor(func(context.Context, string, *mms.Value) (bool, error) {
		return true, fmt.Errorf("denied")
	})
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "MMXU1$MX$AnIn1$mag$f", Value: mms.NewFloat(2.0),
	}); err == nil {
		t.Fatal("expected interceptor denial")
	}
}

func TestRegisterDA_EnumAndDefaultRead(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5,
			DataStructureNestingLevel: 10,
		},
	})
	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatal(err)
	}
	vs := NewValueStore()

	attr := &DataAttribute{
		Name: "stVal", FC: "ST", BType: "Enum",
		InitialValue: "2", EnumValues: []int{1, 2, 3}, EnumNames: map[string]int{"on": 2},
	}
	if err := registerDA(srv, vs, "LD1", "LLN0", []string{"Mod"}, attr); err != nil {
		t.Fatalf("registerDA enum: %v", err)
	}

	badOrd := &DataAttribute{
		Name: "stVal", FC: "ST", BType: "Enum",
		InitialValue: "9", EnumValues: []int{1, 2},
	}
	if err := registerDA(srv, vs, "LD1", "LLN0", []string{"Bad"}, badOrd); err == nil {
		t.Fatal("expected invalid enum ordinal")
	}

	vis := &DataAttribute{Name: "vendor", FC: "DC", BType: "VisString64", InitialValue: "ok"}
	if err := registerDA(srv, vs, "LD1", "LLN0", []string{"NamPlt"}, vis); err != nil {
		t.Fatalf("vis: %v", err)
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	client, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(ctx) })

	// DefaultValue read path when store entry cleared.
	vs.Set(StoreKey("LD1", "LLN0$ST$Mod$stVal"), nil)
	if _, err := client.Read(ctx, mms.ReadRequest{DomainID: "LD1", ItemID: "LLN0$ST$Mod$stVal"}); err != nil {
		t.Fatalf("default read: %v", err)
	}

	long := strings.Repeat("a", 80)
	if _, err := client.Write(ctx, mms.WriteRequest{
		DomainID: "LD1", ItemID: "LLN0$DC$NamPlt$vendor", Value: mms.NewVisibleString(long),
	}); err == nil {
		t.Fatal("expected visible-string size validation error")
	}
}

func TestBuildAttrElemAndRead_Unsupported(t *testing.T) {
	vs := NewValueStore()
	_, _, err := buildAttrElemAndRead(vs, "LD1", "LLN0", []string{"Mod"}, &DataAttribute{
		Name: "x", FC: "ST", BType: "NotAType",
	})
	if err == nil {
		t.Fatal("expected unsupported BType")
	}

	elem, read, err := buildAttrElemAndRead(vs, "LD1", "MMXU1", []string{"AnIn1"}, &DataAttribute{
		Name: "mag",
		FC:   "MX",
		Children: []DataAttribute{
			{Name: "f", FC: "", BType: "FLOAT32"},
		},
	})
	if err != nil {
		t.Fatalf("compound: %v", err)
	}
	if elem.Type.Type != mms.ValueTypeStructure {
		t.Fatalf("type = %v", elem.Type.Type)
	}
	v, err := read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, ok := v.Structure(); !ok {
		t.Fatal("expected structure")
	}
}
