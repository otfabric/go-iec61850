package iec61850

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms"
)

// Tests targeting functions at 0% coverage to raise overall coverage.

func TestHasDuplicateRefs(t *testing.T) {
	r1, _ := ParseRef("LD/LN.DO.DA[ST]")
	r2, _ := ParseRef("LD/LN.DO.DB[ST]")

	if HasDuplicateRefs(nil) {
		t.Error("nil should have no duplicates")
	}
	if HasDuplicateRefs([]Ref{}) {
		t.Error("empty should have no duplicates")
	}
	if HasDuplicateRefs([]Ref{r1}) {
		t.Error("single element should have no duplicates")
	}
	if HasDuplicateRefs([]Ref{r1, r2}) {
		t.Error("distinct refs should have no duplicates")
	}
	if !HasDuplicateRefs([]Ref{r1, r2, r1}) {
		t.Error("should detect duplicate")
	}
}

func TestParsedRefsCache(t *testing.T) {
	mc := newModelCache(CacheLazy)

	if _, ok := mc.getParsedRefs("LD1"); ok {
		t.Error("expected miss for uncached LD")
	}

	refs := []Ref{{LD: "LD1", LN: "LLN0"}}
	mc.setParsedRefs("LD1", refs)

	got, ok := mc.getParsedRefs("LD1")
	if !ok {
		t.Fatal("expected hit")
	}
	if len(got) != 1 || got[0].LD != "LD1" {
		t.Errorf("unexpected cached refs: %v", got)
	}

	mc.invalidateAll()
	if _, ok := mc.getParsedRefs("LD1"); ok {
		t.Error("expected miss after invalidateAll")
	}
}

func TestParseRefStrict(t *testing.T) {
	_, err := ParseRefStrict("LD/LN.DO.DA[ST]")
	if err != nil {
		t.Fatalf("valid short ref: %v", err)
	}

	long := make([]byte, 130)
	for i := range long {
		long[i] = 'A'
	}
	_, err = ParseRefStrict(string(long))
	if err == nil {
		t.Fatal("expected error for >129 char ref")
	}
}

func TestDataAccessError(t *testing.T) {
	e := &DataAccessError{Ref: "LD/LN.DO[ST]", ErrorCode: 3, Operation: "read"}
	s := e.Error()
	if s == "" {
		t.Error("empty error string")
	}
	if !errors.Is(e, ErrDataAccess) {
		t.Error("should unwrap to ErrDataAccess")
	}
}

func TestCheckConditions_Has(t *testing.T) {
	c := CheckSynchroCheck | CheckInterlockCheck
	if !c.Has(CheckSynchroCheck) {
		t.Error("should have synchro")
	}
	if !c.Has(CheckInterlockCheck) {
		t.Error("should have interlock")
	}
	var empty CheckConditions
	if empty.Has(CheckSynchroCheck) {
		t.Error("empty should not have synchro")
	}
}

func TestEnumCtlVal(t *testing.T) {
	v := EnumCtlVal(42)
	if v == nil {
		t.Fatal("nil value")
	}
	i, ok := v.Int64()
	if !ok || i != 42 {
		t.Errorf("expected 42, got %d", i)
	}
}

func TestBspCtlVal(t *testing.T) {
	v := BspCtlVal([]byte{0xC0}, 2)
	if v == nil {
		t.Fatal("nil value")
	}
	bits, ok := v.BitString()
	if !ok || len(bits) == 0 {
		t.Error("expected bitstring")
	}
	if bits[0] != 0xC0 {
		t.Errorf("expected 0xC0, got 0x%02x", bits[0])
	}
}

func TestControlStoreKey(t *testing.T) {
	s := &Server{}

	// Valid ref.
	key := s.controlStoreKey("LD1/GGIO1.SPCSO1")
	if key == "" {
		t.Error("expected non-empty store key")
	}
	if key != "LD1/GGIO1$ST$SPCSO1$stVal" {
		t.Errorf("unexpected key: %s", key)
	}

	// Missing slash.
	if s.controlStoreKey("noslash") != "" {
		t.Error("expected empty for missing /")
	}

	// Missing dot.
	if s.controlStoreKey("LD1/nodot") != "" {
		t.Error("expected empty for missing .")
	}
}

func TestReportEngine_ReportEngineAccessor(t *testing.T) {
	s := &Server{}
	if s.ReportEngine() != nil {
		t.Error("should be nil before EnableReports")
	}
}

func TestReportIndication_Clone(t *testing.T) {
	ri := &ReportIndication{
		RptID:     "rpt01",
		SeqNum:    1,
		EntryID:   []byte{1, 2, 3},
		Inclusion: []bool{true, false},
		Values: []*Value{
			NewValue(mms.NewBoolean(true)),
		},
		DataReferences: []string{"ref1"},
		ReasonCodes:    []ReasonCode{ReasonDataChanged},
	}

	c := ri.clone()
	if c.RptID != ri.RptID {
		t.Error("RptID should match")
	}

	// Modify clone slices — should not affect original.
	c.EntryID[0] = 99
	if ri.EntryID[0] == 99 {
		t.Error("EntryID should be independent")
	}
	c.Inclusion[0] = false
	if !ri.Inclusion[0] {
		t.Error("Inclusion should be independent")
	}
	c.DataReferences[0] = "modified"
	if ri.DataReferences[0] == "modified" {
		t.Error("DataReferences should be independent")
	}
	c.ReasonCodes[0] = ReasonGI
	if ri.ReasonCodes[0] != ReasonDataChanged {
		t.Error("ReasonCodes should be independent")
	}
}

func TestValuesEqual_UTCTime(t *testing.T) {
	now := mms.NewUTCTime(zeroTime)
	if !valuesEqual(now, mms.NewUTCTime(zeroTime)) {
		t.Error("same UTC time should be equal")
	}
}

func TestValuesEqual_BinaryTime(t *testing.T) {
	a := mms.NewBinaryTime(12345)
	b := mms.NewBinaryTime(12345)
	if !valuesEqual(a, b) {
		t.Error("same binary time should be equal")
	}
	c := mms.NewBinaryTime(99999)
	if valuesEqual(a, c) {
		t.Error("different binary time should not be equal")
	}
}

func TestValuesEqual_Structure(t *testing.T) {
	a := mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)})
	b := mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)})
	if !valuesEqual(a, b) {
		t.Error("same structures should be equal")
	}

	c := mms.NewStructure([]*mms.Value{mms.NewInteger(2), mms.NewBoolean(true)})
	if valuesEqual(a, c) {
		t.Error("different structures should not be equal")
	}

	d := mms.NewStructure([]*mms.Value{mms.NewInteger(1)})
	if valuesEqual(a, d) {
		t.Error("different length structures should not be equal")
	}
}

func TestQualityChanged_InStructure(t *testing.T) {
	q1 := mms.NewBitStringWithLength([]byte{0x00, 0x00}, 13)
	q2 := mms.NewBitStringWithLength([]byte{0x04, 0x00}, 13)

	s1 := mms.NewStructure([]*mms.Value{mms.NewInteger(0), q1})
	s2 := mms.NewStructure([]*mms.Value{mms.NewInteger(0), q2})

	if !qualityChanged(s1, s2) {
		t.Error("should detect quality change in structure")
	}
	if qualityChanged(s1, s1) {
		t.Error("same structure should not detect change")
	}
}

func TestParseRCBStoreKey(t *testing.T) {
	tests := []struct {
		input     string
		wantRCB   string
		wantField string
		wantOK    bool
	}{
		{"LLN0$BR$brcb01$RptEna", "LLN0$BR$brcb01", "RptEna", true},
		{"LLN0$RP$urcb01$GI", "LLN0$RP$urcb01", "GI", true},
		{"LLN0$BR$brcb01$OptFlds", "LLN0$BR$brcb01", "OptFlds", true},
		{"LLN0$ST$Mod$stVal", "", "", false},
		{"LLN0$SP$SGCB$ActSG", "", "", false},
		{"noprefix", "", "", false},
	}
	for _, tt := range tests {
		rcb, field, ok := parseRCBStoreKey(tt.input)
		if ok != tt.wantOK || rcb != tt.wantRCB || field != tt.wantField {
			t.Errorf("parseRCBStoreKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.input, rcb, field, ok, tt.wantRCB, tt.wantField, tt.wantOK)
		}
	}
}

func TestParseSGCBStoreKey(t *testing.T) {
	tests := []struct {
		input     string
		wantField string
		wantOK    bool
	}{
		{"LLN0$SP$SGCB$ActSG", "ActSG", true},
		{"LLN0$SP$SGCB$EditSG", "EditSG", true},
		{"LLN0$SP$SGCB$CnfEdit", "CnfEdit", true},
		{"LLN0$BR$brcb01$RptEna", "", false},
		{"LLN0$ST$Mod$stVal", "", false},
	}
	for _, tt := range tests {
		field, ok := parseSGCBStoreKey(tt.input)
		if ok != tt.wantOK || field != tt.wantField {
			t.Errorf("parseSGCBStoreKey(%q) = (%q, %v), want (%q, %v)",
				tt.input, field, ok, tt.wantField, tt.wantOK)
		}
	}
}

func TestWriteInterceptor_RCBDispatch(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	storeKey := "LD1/LLN0$BR$brcb01$RptEna"
	srv.store.Set(storeKey, mms.NewBoolean(true))
	handled, err := srv.store.CallInterceptorForTest(ctx, storeKey, mms.NewBoolean(true))
	if err != nil {
		t.Fatalf("interceptor error: %v", err)
	}
	if !handled {
		t.Fatal("interceptor should handle RCB writes")
	}
}

func TestWriteInterceptor_NonRCB_ReportsEnabled(t *testing.T) {
	// With the report engine active, the interceptor handles regular DA writes
	// to send dchg notifications. It must return (true, nil) so the DA Write
	// handler knows notification was dispatched, and must not return any error.
	model := testReportSCL()
	srv := newTestServer(t, model)
	srv.EnableReports()

	ctx := context.Background()
	// Simulate what the DA Write handler does: store the value first, then
	// call the interceptor for notification.
	storeKey := "LD1/LLN0$ST$Mod$stVal"
	srv.store.Set(storeKey, mms.NewInteger(1))
	handled, err := srv.store.CallInterceptorForTest(ctx, storeKey, mms.NewInteger(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When reports are enabled the interceptor claims the notification
	// (returns handled=true) so the DA Write handler can return immediately.
	if !handled {
		t.Fatal("interceptor should handle DA writes when reports are enabled")
	}
}

func TestWriteInterceptor_NonRCB_ReportsDisabled(t *testing.T) {
	// Without the report engine, the interceptor leaves regular DA writes
	// to the normal store path (returns handled=false).
	model := testReportSCL()
	srv := newTestServer(t, model)
	// Note: EnableReports() is intentionally NOT called.

	ctx := context.Background()
	handled, err := srv.store.CallInterceptorForTest(ctx, "LD1/LLN0$ST$Mod$stVal", mms.NewInteger(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatal("non-RCB keys should not be handled when reports are disabled")
	}
}
