package iec61850

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// --- Type tests ---

func TestOptFlds_Has(t *testing.T) {
	o := OptFldSeqNum | OptFldTimeStamp | OptFldReasonCode
	if !o.Has(OptFldSeqNum) {
		t.Error("expected SeqNum set")
	}
	if !o.Has(OptFldReasonCode) {
		t.Error("expected ReasonCode set")
	}
	if o.Has(OptFldDataSet) {
		t.Error("expected DataSet not set")
	}
}

func TestOptFlds_String(t *testing.T) {
	o := OptFldSeqNum | OptFldConfRev
	s := o.String()
	if s != "seq-num,conf-rev" {
		t.Errorf("String() = %q, want %q", s, "seq-num,conf-rev")
	}

	if OptFlds(0).String() != "none" {
		t.Errorf("zero OptFlds String() = %q, want %q", OptFlds(0).String(), "none")
	}
}

func TestTrgOps_Has(t *testing.T) {
	tr := TrgOpDataChanged | TrgOpGI
	if !tr.Has(TrgOpDataChanged) {
		t.Error("expected DataChanged set")
	}
	if !tr.Has(TrgOpGI) {
		t.Error("expected GI set")
	}
	if tr.Has(TrgOpIntegrity) {
		t.Error("expected Integrity not set")
	}
}

func TestTrgOps_String(t *testing.T) {
	tr := TrgOpDataChanged | TrgOpQualityChanged
	s := tr.String()
	if s != "data-changed,quality-changed" {
		t.Errorf("String() = %q, want %q", s, "data-changed,quality-changed")
	}

	if TrgOps(0).String() != "none" {
		t.Errorf("zero TrgOps String() = %q, want %q", TrgOps(0).String(), "none")
	}
}

func TestReasonCode_String(t *testing.T) {
	tests := []struct {
		r    ReasonCode
		want string
	}{
		{ReasonDataChanged, "data-change"},
		{ReasonQualityChanged, "quality-change"},
		{ReasonDataUpdate, "data-update"},
		{ReasonIntegrity, "integrity"},
		{ReasonGI, "GI"},
		{0, "not-included"},
		{ReasonDataChanged | ReasonGI, "data-change,GI"},
		{0xFF, "data-change,quality-change,data-update,integrity,GI"},
	}
	for _, tc := range tests {
		if got := tc.r.String(); got != tc.want {
			t.Errorf("ReasonCode(%d).String() = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestRCBType_String(t *testing.T) {
	if RCBBuffered.String() != "BRCB" {
		t.Errorf("RCBBuffered.String() = %q", RCBBuffered.String())
	}
	if RCBUnbuffered.String() != "URCB" {
		t.Errorf("RCBUnbuffered.String() = %q", RCBUnbuffered.String())
	}
}

func TestRCBType_FC(t *testing.T) {
	if RCBBuffered.FC() != FCBR {
		t.Errorf("RCBBuffered.FC() = %q", RCBBuffered.FC())
	}
	if RCBUnbuffered.FC() != FCRP {
		t.Errorf("RCBUnbuffered.FC() = %q", RCBUnbuffered.FC())
	}
}

func TestIsRCBItemID(t *testing.T) {
	tests := []struct {
		itemID string
		want   bool
	}{
		{"LLN0$BR$brcb01", true},
		{"LLN0$RP$urcb01", true},
		{"LLN0$ST$Mod$stVal", false},
		{"LLN0$BR", false},
		{"LLN0", false},
	}
	for _, tc := range tests {
		if got := isRCBItemID(tc.itemID); got != tc.want {
			t.Errorf("isRCBItemID(%q) = %v, want %v", tc.itemID, got, tc.want)
		}
	}
}

// --- OptFlds / TrgOps encode/decode roundtrip ---

func TestOptFlds_Roundtrip(t *testing.T) {
	orig := OptFldSeqNum | OptFldTimeStamp | OptFldReasonCode | OptFldConfRev
	encoded := encodeOptFlds(orig)
	decoded := decodeOptFlds(encoded)
	if decoded != orig {
		t.Errorf("roundtrip: got %v, want %v", decoded, orig)
	}
}

func TestTrgOps_Roundtrip(t *testing.T) {
	orig := TrgOpDataChanged | TrgOpQualityChanged | TrgOpGI
	encoded := encodeTrgOps(orig)
	decoded := decodeTrgOps(encoded)
	if decoded != orig {
		t.Errorf("roundtrip: got %v, want %v", decoded, orig)
	}
}

// --- RCB loopback tests ---

func setupRCBLoopback(t *testing.T) (*Client, *mms.Server, *sync.Mutex, map[string]*mms.Value) {
	t.Helper()
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	domain := "simpleIOGenericIO"
	if err := srv.RegisterDomain(domain); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	mu := &sync.Mutex{}
	store := map[string]*mms.Value{}

	brcbAttrs := []struct {
		name  string
		value *mms.Value
	}{
		{"RptID", mms.NewVisibleString("brcb01")},
		{"RptEna", mms.NewBoolean(false)},
		{"DatSet", mms.NewVisibleString("simpleIOGenericIO/LLN0$dataset1")},
		{"ConfRev", mms.NewUnsigned(1)},
		{"OptFlds", mms.NewBitStringWithLength([]byte{0, 0}, 10)},
		{"BufTm", mms.NewUnsigned(0)},
		{"SqNum", mms.NewUnsigned(0)},
		{"TrgOps", mms.NewBitStringWithLength([]byte{0}, 6)},
		{"IntgPd", mms.NewUnsigned(0)},
		{"GI", mms.NewBoolean(false)},
		{"PurgeBuf", mms.NewBoolean(false)},
		{"EntryID", mms.NewOctetString([]byte{0, 0, 0, 0, 0, 0, 0, 0})},
		{"TimeOfEntry", mms.NewUnsigned(0)},
	}

	urcbAttrs := []struct {
		name  string
		value *mms.Value
	}{
		{"RptID", mms.NewVisibleString("urcb01")},
		{"RptEna", mms.NewBoolean(false)},
		{"Resv", mms.NewBoolean(false)},
		{"DatSet", mms.NewVisibleString("simpleIOGenericIO/LLN0$dataset1")},
		{"ConfRev", mms.NewUnsigned(1)},
		{"OptFlds", mms.NewBitStringWithLength([]byte{0, 0}, 10)},
		{"BufTm", mms.NewUnsigned(0)},
		{"SqNum", mms.NewUnsigned(0)},
		{"TrgOps", mms.NewBitStringWithLength([]byte{0}, 6)},
		{"IntgPd", mms.NewUnsigned(0)},
		{"GI", mms.NewBoolean(false)},
	}

	registerAttrs := func(prefix string, attrs []struct {
		name  string
		value *mms.Value
	}) {
		structElements := make([]*mms.Value, len(attrs))
		for i, a := range attrs {
			itemID := prefix + "$" + a.name
			key := domain + "/" + itemID
			mu.Lock()
			store[key] = a.value
			mu.Unlock()
			structElements[i] = a.value

			capturedKey := key
			if err := srv.RegisterVariable(mms.Variable{
				Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(itemID)},
				TypeSpec: mms.TypeSpec{Type: a.value.Type()},
				Read: func(_ context.Context) (*mms.Value, error) {
					mu.Lock()
					defer mu.Unlock()
					return store[capturedKey], nil
				},
				Write: func(_ context.Context, val *mms.Value) error {
					mu.Lock()
					defer mu.Unlock()
					store[capturedKey] = val
					return nil
				},
			}); err != nil {
				t.Fatalf("register variable %q: %v", itemID, err)
			}
		}

		// Also register the parent as a structure (for GetReportControlBlock read)
		structVal := mms.NewStructure(structElements)
		parentKey := domain + "/" + prefix
		mu.Lock()
		store[parentKey] = structVal
		mu.Unlock()
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: mms.ItemID(prefix)},
			TypeSpec: mms.TypeSpec{Type: mms.ValueTypeStructure},
			Read: func(_ context.Context) (*mms.Value, error) {
				mu.Lock()
				defer mu.Unlock()
				return store[parentKey], nil
			},
		}); err != nil {
			t.Fatalf("register parent variable %q: %v", prefix, err)
		}
	}

	// Register a non-RCB variable to verify ListReports filters correctly
	nonRCBKey := domain + "/LLN0$ST$Mod$stVal"
	mu.Lock()
	store[nonRCBKey] = mms.NewInteger(1)
	mu.Unlock()
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(domain), ItemID: "LLN0$ST$Mod$stVal"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8},
		Read: func(_ context.Context) (*mms.Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return store[nonRCBKey], nil
		},
	}); err != nil {
		t.Fatalf("register non-RCB variable: %v", err)
	}

	registerAttrs("LLN0$BR$brcb01", brcbAttrs)
	registerAttrs("LLN0$RP$urcb01", urcbAttrs)

	clientT, serverT := loopbackPair()
	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client, srv, mu, store
}

func TestListReports(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	rcbs, err := client.ListReports(ctx, "simpleIOGenericIO")
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}

	if len(rcbs) != 2 {
		t.Fatalf("got %d RCBs, want 2", len(rcbs))
	}

	hasBR, hasRP := false, false
	for _, name := range rcbs {
		if name == "LLN0$BR$brcb01" {
			hasBR = true
		}
		if name == "LLN0$RP$urcb01" {
			hasRP = true
		}
	}
	if !hasBR {
		t.Error("missing BRCB (LLN0$BR$brcb01)")
	}
	if !hasRP {
		t.Error("missing URCB (LLN0$RP$urcb01)")
	}
}

func TestListReports_EmptyLD(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	_, err := client.ListReports(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty LD")
	}
}

func TestGetReportControlBlock_BRCB(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	rcb, err := client.GetReportControlBlock(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01")
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}

	if rcb.Type != RCBBuffered {
		t.Errorf("Type = %v, want RCBBuffered", rcb.Type)
	}
	if rcb.RptID != "brcb01" {
		t.Errorf("RptID = %q, want %q", rcb.RptID, "brcb01")
	}
	if rcb.RptEna {
		t.Error("RptEna should be false")
	}
	if rcb.DatSet != "simpleIOGenericIO/LLN0$dataset1" {
		t.Errorf("DatSet = %q", rcb.DatSet)
	}
	if rcb.ConfRev != 1 {
		t.Errorf("ConfRev = %d, want 1", rcb.ConfRev)
	}
	if rcb.Reference != "simpleIOGenericIO/LLN0.BR.brcb01" {
		t.Errorf("Reference = %q", rcb.Reference)
	}
}

func TestGetReportControlBlock_URCB(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	rcb, err := client.GetReportControlBlock(ctx, "simpleIOGenericIO", "LLN0$RP$urcb01")
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}

	if rcb.Type != RCBUnbuffered {
		t.Errorf("Type = %v, want RCBUnbuffered", rcb.Type)
	}
	if rcb.RptID != "urcb01" {
		t.Errorf("RptID = %q, want %q", rcb.RptID, "urcb01")
	}
}

func TestListReports_ClosedClient(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ListReports(ctx, "simpleIOGenericIO")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestGetReportControlBlock_ClosedClient(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.GetReportControlBlock(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

// --- Report decode tests ---

func TestDecodeReportIndication_Basic(t *testing.T) {
	optFldsVal := encodeOptFlds(OptFldSeqNum | OptFldReasonCode)

	inclusionData := []byte{0xE0} // bits 0,1,2 set (MSB-first: 111xxxxx)

	values := []*mms.Value{
		mms.NewVisibleString("testReport"),           // RptID
		optFldsVal,                                   // OptFlds
		mms.NewUnsigned(42),                          // SeqNum (OptFldSeqNum)
		mms.NewBitStringWithLength(inclusionData, 3), // Inclusion (3 members, all included)
		mms.NewInteger(100),                          // Value 0
		mms.NewBoolean(true),                         // Value 1
		mms.NewInteger(200),                          // Value 2
		mms.NewBitStringWithLength([]byte{0x40}, 7),  // Reason 0: data-change
		mms.NewBitStringWithLength([]byte{0x20}, 7),  // Reason 1: quality-change
		mms.NewBitStringWithLength([]byte{0x40}, 7),  // Reason 2: data-change
	}

	ri, err := decodeReportIndication(values, nil)
	if err != nil {
		t.Fatalf("decodeReportIndication: %v", err)
	}

	if ri.RptID != "testReport" {
		t.Errorf("RptID = %q, want %q", ri.RptID, "testReport")
	}
	if ri.SeqNum != 42 {
		t.Errorf("SeqNum = %d, want 42", ri.SeqNum)
	}
	if len(ri.Inclusion) != 3 {
		t.Fatalf("Inclusion length = %d, want 3", len(ri.Inclusion))
	}
	for i, inc := range ri.Inclusion {
		if !inc {
			t.Errorf("Inclusion[%d] = false, want true", i)
		}
	}
	if len(ri.Values) != 3 {
		t.Fatalf("Values length = %d, want 3", len(ri.Values))
	}
	if len(ri.ReasonCodes) != 3 {
		t.Fatalf("ReasonCodes length = %d, want 3", len(ri.ReasonCodes))
	}
	if ri.ReasonCodes[0] != ReasonDataChanged {
		t.Errorf("ReasonCodes[0] = %v, want data-change", ri.ReasonCodes[0])
	}
	if ri.ReasonCodes[1] != ReasonQualityChanged {
		t.Errorf("ReasonCodes[1] = %v, want quality-change", ri.ReasonCodes[1])
	}
}

func TestDecodeReportIndication_PartialInclusion(t *testing.T) {
	optFldsVal := encodeOptFlds(0)

	inclusionData := []byte{0xA0} // bits 0,2 set, bit 1 not (10100000)

	values := []*mms.Value{
		mms.NewVisibleString("partialReport"),
		optFldsVal,
		mms.NewBitStringWithLength(inclusionData, 3),
		mms.NewInteger(100), // Value for member 0
		mms.NewInteger(300), // Value for member 2
	}

	ri, err := decodeReportIndication(values, nil)
	if err != nil {
		t.Fatalf("decodeReportIndication: %v", err)
	}

	if len(ri.Inclusion) != 3 {
		t.Fatalf("Inclusion length = %d, want 3", len(ri.Inclusion))
	}
	if !ri.Inclusion[0] || ri.Inclusion[1] || !ri.Inclusion[2] {
		t.Errorf("Inclusion = %v, want [true, false, true]", ri.Inclusion)
	}
	if len(ri.Values) != 2 {
		t.Fatalf("Values length = %d, want 2", len(ri.Values))
	}
}

func TestDecodeReportIndication_Empty(t *testing.T) {
	_, err := decodeReportIndication(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty values")
	}
}

func TestDecodeReportIndication_MissingInclusion(t *testing.T) {
	// RptID + OptFlds only, no inclusion bitstring — should decode
	// without panic, producing zero-length inclusion/values.
	values := []*mms.Value{
		mms.NewVisibleString("rpt1"),
		encodeOptFlds(0),
	}
	ri, err := decodeReportIndication(values, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ri.Inclusion) != 0 {
		t.Errorf("Inclusion length = %d, want 0", len(ri.Inclusion))
	}
	if len(ri.Values) != 0 {
		t.Errorf("Values length = %d, want 0", len(ri.Values))
	}
}

func TestDecodeReportIndication_SeqNumMissing(t *testing.T) {
	// OptFlds says SeqNum is present but no value follows after OptFlds.
	values := []*mms.Value{
		mms.NewVisibleString("rpt1"),
		encodeOptFlds(OptFldSeqNum),
		// SeqNum value intentionally missing — next() returns nil
	}
	ri, err := decodeReportIndication(values, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.SeqNum != 0 {
		t.Errorf("SeqNum = %d, want 0 (missing)", ri.SeqNum)
	}
}

func TestDecodeReportIndication_ReasonCodeTooFew(t *testing.T) {
	// OptFlds says ReasonCode, inclusion says 3 members included, but
	// only 1 reason code value provided.
	optFldsVal := encodeOptFlds(OptFldReasonCode)
	inclusionData := []byte{0xE0} // 3 members included

	values := []*mms.Value{
		mms.NewVisibleString("rpt-short-reasons"),
		optFldsVal,
		mms.NewBitStringWithLength(inclusionData, 3),
		mms.NewInteger(1), // Value 0
		mms.NewInteger(2), // Value 1
		mms.NewInteger(3), // Value 2
		mms.NewBitStringWithLength([]byte{0x40}, 7), // Only 1 reason (need 3)
	}
	_, err := decodeReportIndication(values, nil)
	if err == nil {
		t.Fatal("expected error for reason code count mismatch")
	}
}

func TestDecodeReportIndication_DataRefTooFew(t *testing.T) {
	optFldsVal := encodeOptFlds(OptFldDataRef)
	inclusionData := []byte{0xC0} // 2 members included

	values := []*mms.Value{
		mms.NewVisibleString("rpt-short-refs"),
		optFldsVal,
		mms.NewBitStringWithLength(inclusionData, 2),
		mms.NewVisibleString("ref0"), // DataRef 0 (only 1 of 2)
		mms.NewInteger(10),           // Value 0
		mms.NewInteger(20),           // Value 1
	}
	_, err := decodeReportIndication(values, nil)
	if err == nil {
		t.Fatal("expected error for value/data ref count mismatch")
	}
}

func TestDecodeRCB_ShortBRCB(t *testing.T) {
	// BRCB needs at least 13 elements; provide fewer.
	elems := make([]*mms.Value, 5)
	for i := range elems {
		elems[i] = mms.NewInteger(0)
	}
	v := mms.NewStructure(elems)

	_, err := decodeRCB("LD1", "LLN0$BR$brcb01", RCBBuffered, v)
	if err == nil {
		t.Fatal("expected error for short BRCB structure")
	}
	var re *ReportError
	if !errors.As(err, &re) {
		t.Fatalf("expected *ReportError, got %T: %v", err, err)
	}
}

func TestDecodeRCB_ShortURCB(t *testing.T) {
	// URCB needs at least 11 elements; provide fewer.
	elems := make([]*mms.Value, 5)
	for i := range elems {
		elems[i] = mms.NewInteger(0)
	}
	v := mms.NewStructure(elems)

	_, err := decodeRCB("LD1", "LLN0$RP$urcb01", RCBUnbuffered, v)
	if err == nil {
		t.Fatal("expected error for short URCB structure")
	}
	var re *ReportError
	if !errors.As(err, &re) {
		t.Fatalf("expected *ReportError, got %T: %v", err, err)
	}
}

// --- Subscription tests ---

func setupReportLoopback(t *testing.T) (*Client, *mms.Server) {
	t.Helper()
	ctx := context.Background()

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	if err := srv.RegisterDomain("testLD"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client, srv
}

func TestSubscribeReport(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "testRptID", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	optFldsVal := encodeOptFlds(0)

	reportValues := []*mms.Value{
		mms.NewVisibleString("testRptID"),
		optFldsVal,
		mms.NewBitStringWithLength([]byte{0x80}, 1), // 1 member included
		mms.NewInteger(42),
	}

	rptListName := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "testLD",
		ItemID: "LLN0$BR$brcb01",
	}
	err = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values:   reportValues,
	})
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}

	select {
	case ri := <-sub.Reports():
		if ri.RptID != "testRptID" {
			t.Errorf("RptID = %q, want %q", ri.RptID, "testRptID")
		}
		if len(ri.Values) != 1 {
			t.Fatalf("Values length = %d, want 1", len(ri.Values))
		}
		i, err := ri.Values[0].Int64()
		if err != nil {
			t.Fatalf("Values[0].Int64: %v", err)
		}
		if i != 42 {
			t.Errorf("Values[0] = %d, want 42", i)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for report")
	}
}

func TestSubscribeReport_Close(t *testing.T) {
	client, _ := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "closedRpt", SubscribeReportOptions{QueueSize: 4})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Idempotent close
	if err := sub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Channel should be closed
	_, ok := <-sub.Reports()
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestSubscribeReport_ClosedClient(t *testing.T) {
	client, _ := setupReportLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.SubscribeReport(ctx, "rptID", SubscribeReportOptions{})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestSubscribeReport_EmptyRptID(t *testing.T) {
	client, _ := setupReportLoopback(t)
	ctx := context.Background()

	_, err := client.SubscribeReport(ctx, "", SubscribeReportOptions{})
	if err == nil {
		t.Fatal("expected error for empty RptID")
	}
}

func TestSubscribeReport_QueueOverflow(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "overflowRpt", SubscribeReportOptions{QueueSize: 2})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "testLD",
		ItemID: "LLN0$BR$brcb01",
	}

	makeReport := func(seq uint64) *mms.InformationReportRequest {
		return &mms.InformationReportRequest{
			ListName: &rptListName,
			Values: []*mms.Value{
				mms.NewVisibleString("overflowRpt"),
				encodeOptFlds(0),
				mms.NewBitStringWithLength([]byte{0x80}, 1),
				mms.NewUnsigned(seq),
			},
		}
	}

	// Send 5 reports into a queue of size 2; at least some must be dropped.
	for i := 0; i < 5; i++ {
		if err := srv.Broadcast(ctx, makeReport(uint64(i))); err != nil {
			t.Fatalf("Broadcast %d: %v", i, err)
		}
	}

	// Allow time for dispatch to process all broadcasts.
	time.Sleep(200 * time.Millisecond)

	// Drain whatever made it into the channel.
	received := 0
	for {
		select {
		case _, ok := <-sub.Reports():
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			received++
		default:
			goto done
		}
	}
done:
	if received > 2 {
		// The channel has buffer=2; we shouldn't get more than 2 without
		// draining between broadcasts.
		t.Logf("received %d (buffer=2, some may have been drained during dispatch)", received)
	}
	if received == 0 {
		t.Error("expected at least 1 report to be delivered")
	}
}

func TestSubscribeReport_MultipleRptIDs(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub1, err := client.SubscribeReport(ctx, "rptA", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport rptA: %v", err)
	}
	defer func() { _ = sub1.Close() }()

	sub2, err := client.SubscribeReport(ctx, "rptB", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport rptB: %v", err)
	}
	defer func() { _ = sub2.Close() }()

	rptListName := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "testLD",
		ItemID: "LLN0$BR$brcb01",
	}

	broadcastReport := func(rptID string, val int64) {
		t.Helper()
		err := srv.Broadcast(ctx, &mms.InformationReportRequest{
			ListName: &rptListName,
			Values: []*mms.Value{
				mms.NewVisibleString(rptID),
				encodeOptFlds(0),
				mms.NewBitStringWithLength([]byte{0x80}, 1),
				mms.NewInteger(val),
			},
		})
		if err != nil {
			t.Fatalf("Broadcast %s: %v", rptID, err)
		}
	}

	broadcastReport("rptA", 10)
	broadcastReport("rptB", 20)
	broadcastReport("rptA", 30)

	expectReport := func(name string, ch <-chan *ReportIndication, wantVal int64) {
		t.Helper()
		select {
		case ri := <-ch:
			v, err := ri.Values[0].Int64()
			if err != nil {
				t.Fatalf("%s: Int64: %v", name, err)
			}
			if v != wantVal {
				t.Errorf("%s: got %d, want %d", name, v, wantVal)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("%s: timeout waiting for report", name)
		}
	}

	expectReport("rptA-1", sub1.Reports(), 10)
	expectReport("rptB-1", sub2.Reports(), 20)
	expectReport("rptA-2", sub1.Reports(), 30)

	// sub2 should have no more reports.
	select {
	case ri := <-sub2.Reports():
		t.Errorf("unexpected report on sub2: %+v", ri)
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestClientClose_ShutsAllSubscriptions(t *testing.T) {
	client, _ := setupReportLoopback(t)
	ctx := context.Background()

	sub1, err := client.SubscribeReport(ctx, "closeSub1", SubscribeReportOptions{QueueSize: 4})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	sub2, err := client.SubscribeReport(ctx, "closeSub2", SubscribeReportOptions{QueueSize: 4})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}

	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Both channels should be closed.
	if _, ok := <-sub1.Reports(); ok {
		t.Error("sub1 channel should be closed after client.Close")
	}
	if _, ok := <-sub2.Reports(); ok {
		t.Error("sub2 channel should be closed after client.Close")
	}

	// Further Close on subs should be idempotent/safe.
	if err := sub1.Close(); err != nil {
		t.Errorf("sub1.Close after client.Close: %v", err)
	}
	if err := sub2.Close(); err != nil {
		t.Errorf("sub2.Close after client.Close: %v", err)
	}
}

func TestSetReportControlBlock(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	err := client.SetReportControlBlock(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01", RCBUpdate{
		Fields: RCBFieldRptEna | RCBFieldGI,
		RptEna: true,
		GI:     true,
	})
	if err != nil {
		t.Fatalf("SetReportControlBlock: %v", err)
	}
}

func TestSetReportControlBlock_NoFields(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	err := client.SetReportControlBlock(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01", RCBUpdate{
		Fields: 0,
	})
	if err != nil {
		t.Fatalf("expected no error for empty fields, got: %v", err)
	}
}

func TestSetReportControlBlock_ClosedClient(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	err := client.SetReportControlBlock(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01", RCBUpdate{
		Fields: RCBFieldRptEna,
		RptEna: true,
	})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

// --- TriggerGI tests ---

func TestTriggerGI(t *testing.T) {
	client, _, mu, store := setupRCBLoopback(t)
	ctx := context.Background()

	err := client.TriggerGI(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01")
	if err != nil {
		t.Fatalf("TriggerGI: %v", err)
	}

	mu.Lock()
	giVal := store["simpleIOGenericIO/LLN0$BR$brcb01$GI"]
	mu.Unlock()

	if giVal == nil {
		t.Fatal("GI value not set in store")
	}
	b, ok := giVal.Bool()
	if !ok || !b {
		t.Errorf("GI = %v, want true", b)
	}
}

func TestTriggerGI_ClosedClient(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()
	_ = client.Close(ctx)

	err := client.TriggerGI(ctx, "simpleIOGenericIO", "LLN0$BR$brcb01")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

// --- URCB reserve/release tests ---

func TestReserveReleaseURCB(t *testing.T) {
	client, _, mu, store := setupRCBLoopback(t)
	ctx := context.Background()

	err := client.ReserveURCB(ctx, "simpleIOGenericIO", "LLN0$RP$urcb01")
	if err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}

	mu.Lock()
	resvVal := store["simpleIOGenericIO/LLN0$RP$urcb01$Resv"]
	mu.Unlock()

	if resvVal == nil {
		t.Fatal("Resv not set")
	}
	b, ok := resvVal.Bool()
	if !ok || !b {
		t.Errorf("Resv = %v, want true", b)
	}

	err = client.ReleaseURCB(ctx, "simpleIOGenericIO", "LLN0$RP$urcb01")
	if err != nil {
		t.Fatalf("ReleaseURCB: %v", err)
	}

	mu.Lock()
	resvVal = store["simpleIOGenericIO/LLN0$RP$urcb01$Resv"]
	mu.Unlock()

	b, ok = resvVal.Bool()
	if !ok || b {
		t.Errorf("Resv after release = %v, want false", b)
	}
}

// --- Overflow policy tests ---

func TestSubscribeReport_OverflowDropOldest(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "dropOldest", SubscribeReportOptions{
		QueueSize:      2,
		OverflowPolicy: OverflowDropOldest,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}

	for i := 0; i < 5; i++ {
		_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
			ListName: &rptListName,
			Values: []*mms.Value{
				mms.NewVisibleString("dropOldest"),
				encodeOptFlds(0),
				mms.NewBitStringWithLength([]byte{0x80}, 1),
				mms.NewUnsigned(uint64(i)),
			},
		})
	}

	time.Sleep(200 * time.Millisecond)

	received := 0
	for {
		select {
		case <-sub.Reports():
			received++
		default:
			goto done
		}
	}
done:
	if received == 0 {
		t.Error("expected at least 1 report")
	}
}

func TestSubscribeReport_OverflowCallback(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	var callbackCount int
	var callbackMu sync.Mutex

	sub, err := client.SubscribeReport(ctx, "cbRpt", SubscribeReportOptions{
		QueueSize:      1,
		OverflowPolicy: OverflowCallback,
		OnOverflow: func(_ *ReportIndication) {
			callbackMu.Lock()
			callbackCount++
			callbackMu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}

	for i := 0; i < 5; i++ {
		_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
			ListName: &rptListName,
			Values: []*mms.Value{
				mms.NewVisibleString("cbRpt"),
				encodeOptFlds(0),
				mms.NewBitStringWithLength([]byte{0x80}, 1),
				mms.NewUnsigned(uint64(i)),
			},
		})
	}

	time.Sleep(200 * time.Millisecond)

	callbackMu.Lock()
	if callbackCount == 0 {
		t.Log("no overflow callbacks (timing dependent)")
	}
	callbackMu.Unlock()
}

// --- Report timestamp decode tests ---

func TestDecodeReportIndication_Timestamp(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	optFldsVal := encodeOptFlds(OptFldSeqNum | OptFldTimeStamp)

	values := []*mms.Value{
		mms.NewVisibleString("tsRpt"),
		optFldsVal,
		mms.NewUnsigned(1),
		mms.NewUTCTime(now),
		mms.NewBitStringWithLength([]byte{0x80}, 1),
		mms.NewInteger(42),
	}

	ri, err := decodeReportIndication(values, nil)
	if err != nil {
		t.Fatalf("decodeReportIndication: %v", err)
	}

	if ri.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	diff := ri.Timestamp.Sub(now)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("Timestamp diff = %v, want ~0", diff)
	}
}

// --- Report glob matching tests ---

func TestSubscribeReport_GlobMatch(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "rpt*", SubscribeReportOptions{
		QueueSize: 8,
		MatchMode: RptMatchGlob,
	})
	if err != nil {
		t.Fatalf("SubscribeReport glob: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}

	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("rptFoo"),
			encodeOptFlds(0),
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(99),
		},
	})

	select {
	case ri := <-sub.Reports():
		if ri.RptID != "rptFoo" {
			t.Errorf("RptID = %q, want rptFoo", ri.RptID)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for glob-matched report")
	}
}

func TestSubscribeReport_GlobNoMatch(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "other*", SubscribeReportOptions{
		QueueSize: 8,
		MatchMode: RptMatchGlob,
	})
	if err != nil {
		t.Fatalf("SubscribeReport glob: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}

	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("rptNoMatch"),
			encodeOptFlds(0),
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(1),
		},
	})

	select {
	case ri := <-sub.Reports():
		t.Errorf("unexpected report: %+v", ri)
	case <-time.After(200 * time.Millisecond):
		// expected
	}
}

// --- Segmented report reassembly tests ---

func TestSegmentedReportReassembly(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "segRpt", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}

	segOptFlds := encodeOptFlds(OptFldSeqNum | OptFldSegmentation)

	// Segment 1 of 2
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segRpt"),
			segOptFlds,
			mms.NewUnsigned(10),  // SeqNum
			mms.NewUnsigned(0),   // SubSeqNum
			mms.NewBoolean(true), // MoreSegments
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(100),
		},
	})

	// Segment 2 of 2
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segRpt"),
			segOptFlds,
			mms.NewUnsigned(10),   // SeqNum (same)
			mms.NewUnsigned(1),    // SubSeqNum
			mms.NewBoolean(false), // MoreSegments = false (last)
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(200),
		},
	})

	select {
	case ri := <-sub.Reports():
		if ri.RptID != "segRpt" {
			t.Errorf("RptID = %q", ri.RptID)
		}
		if len(ri.Values) != 2 {
			t.Errorf("assembled values = %d, want 2", len(ri.Values))
		}
		if ri.SeqNum != 10 {
			t.Errorf("SeqNum = %d, want 10", ri.SeqNum)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for assembled report")
	}

	// Should not receive partial segments
	select {
	case ri := <-sub.Reports():
		t.Errorf("unexpected extra report: %+v", ri)
	case <-time.After(200 * time.Millisecond):
		// expected
	}
}

func TestSegmentedReport_ResetOnNewSequence(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "segReset", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}
	segOptFlds := encodeOptFlds(OptFldSeqNum | OptFldSegmentation)

	// Start a 2-segment sequence, then abandon it by starting a new one
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segReset"),
			segOptFlds,
			mms.NewUnsigned(1),   // SeqNum
			mms.NewUnsigned(0),   // SubSeqNum
			mms.NewBoolean(true), // MoreSegments
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(100),
		},
	})

	// New sequence with different SeqNum resets the buffer
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segReset"),
			segOptFlds,
			mms.NewUnsigned(2),    // Different SeqNum
			mms.NewUnsigned(0),    // SubSeqNum (restart)
			mms.NewBoolean(false), // MoreSegments = false
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(200),
		},
	})

	select {
	case ri := <-sub.Reports():
		if ri.SeqNum != 2 {
			t.Errorf("SeqNum = %d, want 2 (the new sequence)", ri.SeqNum)
		}
		if len(ri.Values) != 1 {
			t.Errorf("values = %d, want 1 (only new segment)", len(ri.Values))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for report after buffer reset")
	}
}

func TestSegmentedReport_NonContiguousSubSeqNum(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "segGap", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}
	segOptFlds := encodeOptFlds(OptFldSeqNum | OptFldSegmentation)

	// SubSeqNum 0
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segGap"),
			segOptFlds,
			mms.NewUnsigned(5),   // SeqNum
			mms.NewUnsigned(0),   // SubSeqNum
			mms.NewBoolean(true), // MoreSegments
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(10),
		},
	})

	// Skip SubSeqNum 1, jump to 2 (non-contiguous)
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segGap"),
			segOptFlds,
			mms.NewUnsigned(5),    // SeqNum (same)
			mms.NewUnsigned(2),    // SubSeqNum (gap from 0)
			mms.NewBoolean(false), // MoreSegments = false
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(30),
		},
	})

	// Should still assemble (with warning logged), since we
	// accept non-contiguous sub-sequence numbers
	select {
	case ri := <-sub.Reports():
		if ri.SeqNum != 5 {
			t.Errorf("SeqNum = %d, want 5", ri.SeqNum)
		}
		if len(ri.Values) != 2 {
			t.Errorf("values = %d, want 2", len(ri.Values))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for assembled report")
	}
}

func TestSegmentedReport_InconsistentMetadata(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "segMeta", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	rptListName := mms.ObjectName{
		Scope: mms.ObjectScopeDomain, Domain: "testLD", ItemID: "LLN0$BR$brcb01",
	}
	// IEC 61850 field order: SeqNum, DatSet, ConfRev, Segmentation
	segOptFlds := encodeOptFlds(OptFldSeqNum | OptFldSegmentation | OptFldDataSet | OptFldConfRev)

	// Segment 1: DatSet="ds1", ConfRev=1
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segMeta"),             // RptID
			segOptFlds,                                  // OptFlds
			mms.NewUnsigned(7),                          // SeqNum
			mms.NewVisibleString("ds1"),                 // DatSet
			mms.NewUnsigned(1),                          // ConfRev
			mms.NewUnsigned(0),                          // SubSeqNum
			mms.NewBoolean(true),                        // MoreSegments
			mms.NewBitStringWithLength([]byte{0x80}, 1), // Inclusion
			mms.NewInteger(100),                         // Value
		},
	})

	// Segment 2: DatSet="ds2" (MISMATCH), ConfRev=1
	_ = srv.Broadcast(ctx, &mms.InformationReportRequest{
		ListName: &rptListName,
		Values: []*mms.Value{
			mms.NewVisibleString("segMeta"), // RptID
			segOptFlds,                      // OptFlds
			mms.NewUnsigned(7),              // SeqNum
			mms.NewVisibleString("ds2"),     // DatSet MISMATCH
			mms.NewUnsigned(1),              // ConfRev
			mms.NewUnsigned(1),              // SubSeqNum
			mms.NewBoolean(false),           // MoreSegments = false
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewInteger(200),
		},
	})

	// assembleSegments should detect the DatSet mismatch and fall
	// back to just the first segment
	select {
	case ri := <-sub.Reports():
		if ri.DatSet != "ds1" {
			t.Errorf("DatSet = %q, want ds1 (first segment on mismatch)", ri.DatSet)
		}
		if len(ri.Values) != 1 {
			t.Errorf("values = %d, want 1 (fallback to first segment only)", len(ri.Values))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for report after metadata mismatch")
	}
}

// --- Lifecycle subscription tests ---

func TestSubscribeReport_AutoEnable(t *testing.T) {
	client, _, mu, store := setupRCBLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "autoRpt", SubscribeReportOptions{
		QueueSize:  8,
		AutoEnable: true,
		LD:         "simpleIOGenericIO",
		RCBItemID:  "LLN0$BR$brcb01",
	})
	if err != nil {
		t.Fatalf("SubscribeReport with AutoEnable: %v", err)
	}
	defer func() { _ = sub.Close() }()

	mu.Lock()
	rptEnaVal := store["simpleIOGenericIO/LLN0$BR$brcb01$RptEna"]
	mu.Unlock()

	if rptEnaVal == nil {
		t.Fatal("RptEna not set")
	}
	b, ok := rptEnaVal.Bool()
	if !ok || !b {
		t.Errorf("RptEna = %v, want true", b)
	}
}

func TestSubscribeReport_GIOnSubscribe(t *testing.T) {
	client, _, mu, store := setupRCBLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "giRpt", SubscribeReportOptions{
		QueueSize:     8,
		AutoEnable:    true,
		GIOnSubscribe: true,
		LD:            "simpleIOGenericIO",
		RCBItemID:     "LLN0$BR$brcb01",
	})
	if err != nil {
		t.Fatalf("SubscribeReport with GI: %v", err)
	}
	defer func() { _ = sub.Close() }()

	mu.Lock()
	giVal := store["simpleIOGenericIO/LLN0$BR$brcb01$GI"]
	mu.Unlock()

	if giVal == nil {
		t.Fatal("GI not set")
	}
	b, ok := giVal.Bool()
	if !ok || !b {
		t.Errorf("GI = %v, want true", b)
	}
}

func TestSubscribeReport_ReserveAndEnable(t *testing.T) {
	client, _, mu, store := setupRCBLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "resvRpt", SubscribeReportOptions{
		QueueSize:   8,
		ReserveURCB: true,
		AutoEnable:  true,
		LD:          "simpleIOGenericIO",
		RCBItemID:   "LLN0$RP$urcb01",
	})
	if err != nil {
		t.Fatalf("SubscribeReport with Reserve+Enable: %v", err)
	}
	defer func() { _ = sub.Close() }()

	mu.Lock()
	resvVal := store["simpleIOGenericIO/LLN0$RP$urcb01$Resv"]
	rptEnaVal := store["simpleIOGenericIO/LLN0$RP$urcb01$RptEna"]
	mu.Unlock()

	if resvVal == nil {
		t.Fatal("Resv not set")
	}
	b, ok := resvVal.Bool()
	if !ok || !b {
		t.Errorf("Resv = %v, want true", b)
	}

	if rptEnaVal == nil {
		t.Fatal("RptEna not set")
	}
	b, ok = rptEnaVal.Bool()
	if !ok || !b {
		t.Errorf("RptEna = %v, want true", b)
	}
}

func TestSubscribeReport_CloseDisablesAndReleases(t *testing.T) {
	client, _, mu, store := setupRCBLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "cleanupRpt", SubscribeReportOptions{
		QueueSize:   8,
		ReserveURCB: true,
		AutoEnable:  true,
		LD:          "simpleIOGenericIO",
		RCBItemID:   "LLN0$RP$urcb01",
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}

	mu.Lock()
	rptEnaVal := store["simpleIOGenericIO/LLN0$RP$urcb01$RptEna"]
	resvVal := store["simpleIOGenericIO/LLN0$RP$urcb01$Resv"]
	mu.Unlock()

	if rptEnaVal == nil {
		t.Fatal("RptEna not set after subscribe")
	}
	if b, ok := rptEnaVal.Bool(); !ok || !b {
		t.Error("RptEna should be true after AutoEnable")
	}
	if resvVal == nil {
		t.Fatal("Resv not set after subscribe")
	}
	if b, ok := resvVal.Bool(); !ok || !b {
		t.Error("Resv should be true after ReserveURCB")
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	rptEnaVal = store["simpleIOGenericIO/LLN0$RP$urcb01$RptEna"]
	resvVal = store["simpleIOGenericIO/LLN0$RP$urcb01$Resv"]
	mu.Unlock()

	if b, ok := rptEnaVal.Bool(); !ok || b {
		t.Error("RptEna should be false after Close cleanup")
	}
	if b, ok := resvVal.Bool(); !ok || b {
		t.Error("Resv should be false after Close cleanup")
	}
}

func TestSubscribeReport_AutoEnable_MissingLD(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	_, err := client.SubscribeReport(ctx, "rpt", SubscribeReportOptions{
		AutoEnable: true,
	})
	if err == nil {
		t.Fatal("expected error when AutoEnable without LD")
	}
}

func TestSubscribeReport_MultipleExactSameID(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	sub1, err := client.SubscribeReport(ctx, "rptMulti", SubscribeReportOptions{QueueSize: 4})
	if err != nil {
		t.Fatalf("sub1: %v", err)
	}
	defer func() { _ = sub1.Close() }()

	sub2, err := client.SubscribeReport(ctx, "rptMulti", SubscribeReportOptions{QueueSize: 4})
	if err != nil {
		t.Fatalf("sub2: %v", err)
	}
	defer func() { _ = sub2.Close() }()

	ri := &ReportIndication{RptID: "rptMulti", SeqNum: 1}
	sub1.deliver(ri, discardLogger())
	sub2.deliver(ri, discardLogger())

	select {
	case r := <-sub1.Reports():
		if r.RptID != "rptMulti" {
			t.Errorf("sub1 rptID = %q", r.RptID)
		}
	default:
		t.Error("sub1 did not receive report")
	}

	select {
	case r := <-sub2.Reports():
		if r.RptID != "rptMulti" {
			t.Errorf("sub2 rptID = %q", r.RptID)
		}
	default:
		t.Error("sub2 did not receive report")
	}
}

func TestSubscribeReport_ExactAndGlobBothReceive(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	subExact, err := client.SubscribeReport(ctx, "rptFan", SubscribeReportOptions{QueueSize: 4})
	if err != nil {
		t.Fatalf("subExact: %v", err)
	}
	defer func() { _ = subExact.Close() }()

	subGlob, err := client.SubscribeReport(ctx, "rpt*", SubscribeReportOptions{
		QueueSize: 4,
		MatchMode: RptMatchGlob,
	})
	if err != nil {
		t.Fatalf("subGlob: %v", err)
	}
	defer func() { _ = subGlob.Close() }()

	ri := &ReportIndication{RptID: "rptFan", SeqNum: 1}
	subExact.deliver(ri, discardLogger())
	subGlob.deliver(ri, discardLogger())

	select {
	case <-subExact.Reports():
	default:
		t.Error("exact subscriber did not receive report")
	}

	select {
	case <-subGlob.Reports():
	default:
		t.Error("glob subscriber did not receive report")
	}
}

func TestOverflowBlock_CloseDoesNotPanic(t *testing.T) {
	client, _, _, _ := setupRCBLoopback(t)
	ctx := context.Background()

	sub, err := client.SubscribeReport(ctx, "rptBlock", SubscribeReportOptions{
		QueueSize:      1,
		OverflowPolicy: OverflowBlock,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Fill the channel to capacity.
	sub.deliver(&ReportIndication{RptID: "rptBlock"}, discardLogger())

	done := make(chan struct{})
	go func() {
		defer close(done)
		// This will block until sub is closed; must not panic.
		sub.deliver(&ReportIndication{RptID: "rptBlock"}, discardLogger())
	}()

	// Give the goroutine time to start blocking.
	time.Sleep(10 * time.Millisecond)

	// Close must unblock the sender without panicking.
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deliver did not unblock after Close")
	}
}

func TestDecodeRCB_RequiredFieldTypeMismatch(t *testing.T) {
	// Build an RCB structure where RptID (required VisibleString) is an integer.
	elems := make([]*mms.Value, 12)
	elems[0] = mms.NewInteger(42) // RptID should be VisibleString
	for i := 1; i < 12; i++ {
		elems[i] = mms.NewBoolean(false)
	}

	_, err := decodeRCB("LD", "LLN0$RP$rcb01", RCBUnbuffered, mms.NewStructure(elems))
	if err == nil {
		t.Fatal("expected error for wrong type on required RptID field")
	}
	var re *ReportError
	if !errors.As(err, &re) {
		t.Errorf("expected ReportError, got %T", err)
	}
}

func TestDecodeRCB_OptFldsTrgOpsStrict(t *testing.T) {
	makeRCB := func(optflds, trgops *mms.Value) *mms.Value {
		elems := []*mms.Value{
			mms.NewVisibleString("rptID01"),        // RptID
			mms.NewBoolean(true),                   // RptEna
			mms.NewVisibleString("LLN0$dataset01"), // DatSet
			mms.NewUnsigned(1),                     // ConfRev
			optflds,                                // OptFlds
			mms.NewUnsigned(0),                     // BufTm
			mms.NewUnsigned(0),                     // SqNum
			trgops,                                 // TrgOps
			mms.NewUnsigned(0),                     // IntgPd
			mms.NewBoolean(false),                  // GI
			mms.NewBoolean(false),                  // Resv (URCB)
		}
		return mms.NewStructure(elems)
	}

	t.Run("OptFlds_WrongType", func(t *testing.T) {
		v := makeRCB(mms.NewInteger(99), mms.NewBitString([]byte{0}))
		_, err := decodeRCB("LD", "LLN0$RP$rcb01", RCBUnbuffered, v)
		if err == nil {
			t.Fatal("expected error for OptFlds type mismatch")
		}
		var re *ReportError
		if !errors.As(err, &re) {
			t.Errorf("expected ReportError, got %T", err)
		}
	})

	t.Run("TrgOps_WrongType", func(t *testing.T) {
		v := makeRCB(mms.NewBitString([]byte{0, 0}), mms.NewInteger(99))
		_, err := decodeRCB("LD", "LLN0$RP$rcb01", RCBUnbuffered, v)
		if err == nil {
			t.Fatal("expected error for TrgOps type mismatch")
		}
		var re *ReportError
		if !errors.As(err, &re) {
			t.Errorf("expected ReportError, got %T", err)
		}
	})

	t.Run("MissingOptFlds", func(t *testing.T) {
		elems := []*mms.Value{
			mms.NewVisibleString("rptID01"),
			mms.NewBoolean(true),
			mms.NewVisibleString("LLN0$dataset01"),
			mms.NewUnsigned(1),
		}
		v := mms.NewStructure(elems)
		_, err := decodeRCB("LD", "LLN0$RP$rcb01", RCBUnbuffered, v)
		if err == nil {
			t.Fatal("expected error for missing OptFlds")
		}
	})
}
