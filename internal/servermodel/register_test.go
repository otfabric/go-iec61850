// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"fmt"
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
