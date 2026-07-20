// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"encoding/binary"
	"log/slog"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// testReportSCL returns an SCL with a BRCB and URCB for testing the report engine.
func testReportSCL() *servermodel.Model {
	return &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []servermodel.DataObject{{
					Name: "Mod",
					Attributes: []servermodel.DataAttribute{
						{Name: "stVal", FC: "ST", BType: "INT32"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					},
				}},
				DataSets: []servermodel.DataSetDef{{
					Name: "dsTest",
					Members: []servermodel.DataSetMemberDef{
						{LNName: "LLN0", DOPath: "Mod.stVal", FC: "ST"},
					},
				}},
				Reports: []servermodel.ReportDef{
					{
						Name:     "brcb01",
						RptID:    "rpt_brcb01",
						DatSet:   "dsTest",
						ConfRev:  1,
						Buffered: true,
						BufTime:  0,
						IntgPd:   0,
						TrgOps:   servermodel.TrgOpsDef{Dchg: true, Qchg: true, GI: true},
						OptFlds: servermodel.OptFieldsDef{
							SeqNum:     true,
							ReasonCode: true,
							ConfigRef:  true,
						},
					},
					{
						Name:     "urcb01",
						RptID:    "rpt_urcb01",
						DatSet:   "dsTest",
						ConfRev:  1,
						Buffered: false,
						IntgPd:   0,
						TrgOps:   servermodel.TrgOpsDef{Dchg: true, GI: true},
						OptFlds: servermodel.OptFieldsDef{
							SeqNum:     true,
							ReasonCode: true,
						},
					},
				},
			}},
		}},
	}
}

func newTestServer(t *testing.T, model *servermodel.Model) *Server {
	t.Helper()
	return newTestServerOpts(t, model, true)
}

func newTestServerOpts(t *testing.T, model *servermodel.Model, validate bool) *Server {
	t.Helper()

	if validate {
		if err := model.Validate(); err != nil {
			t.Fatalf("model.Validate: %v", err)
		}
	}

	mmsSrv := mms.NewServer(mms.ServerOptions{})

	vs, err := servermodel.RegisterModel(mmsSrv, model, nil)
	if err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	return &Server{
		logger:   noopLogger(),
		model:    model,
		store:    vs,
		mms:      mmsSrv,
		controls: make(map[string]*controlRegistration),
	}
}

func TestReportEngine_EnableDisable(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	if len(engine.rcbs) != 2 {
		t.Fatalf("expected 2 RCBs, got %d", len(engine.rcbs))
	}

	ctx := context.Background()

	// Enable the BRCB.
	err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true))
	if err != nil {
		t.Fatalf("enable BRCB: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	if !rt.enabled {
		t.Error("BRCB should be enabled")
	}
	if rt.rptID != "rpt_brcb01" {
		t.Errorf("rptID = %q, want rpt_brcb01", rt.rptID)
	}
	if rt.datSet != "LD1/LLN0$dsTest" {
		t.Errorf("datSet = %q, want LD1/LLN0$dsTest", rt.datSet)
	}
	if len(rt.memberKeys) != 1 {
		t.Errorf("memberKeys = %d, want 1", len(rt.memberKeys))
	}
	rt.mu.Unlock()

	// Disable it.
	err = engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(false))
	if err != nil {
		t.Fatalf("disable BRCB: %v", err)
	}

	rt.mu.Lock()
	if rt.enabled {
		t.Error("BRCB should be disabled")
	}
	rt.mu.Unlock()
}

func TestReportEngine_URCB_Reserve(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	// Reserve the URCB.
	err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$RP$urcb01", "Resv", mms.NewBoolean(true))
	if err != nil {
		t.Fatalf("reserve URCB: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$RP$urcb01"]
	rt.mu.Lock()
	if !rt.reserved {
		t.Error("URCB should be reserved")
	}
	rt.mu.Unlock()

	// Release.
	err = engine.HandleRCBWrite(ctx, "LD1", "LLN0$RP$urcb01", "Resv", mms.NewBoolean(false))
	if err != nil {
		t.Fatalf("release URCB: %v", err)
	}

	rt.mu.Lock()
	if rt.reserved {
		t.Error("URCB should not be reserved")
	}
	rt.mu.Unlock()
}

func TestReportEngine_ResvNotApplicableToBRCB(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$brcb01", "Resv", mms.NewBoolean(true))
	if err == nil {
		t.Fatal("expected error for Resv on BRCB")
	}
}

func TestReportEngine_DataChange(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	// Enable the BRCB.
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	prevSeq := rt.seqNum
	rt.mu.Unlock()

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(ctx, storeKey, mms.NewInteger(42))

	rt.mu.Lock()
	newSeq := rt.seqNum
	rt.mu.Unlock()

	if newSeq <= prevSeq {
		t.Errorf("seqNum should have incremented: got %d, prev %d", newSeq, prevSeq)
	}
}

func TestReportEngine_GI(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	// Enable the BRCB.
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	prevSeq := rt.seqNum
	rt.mu.Unlock()

	// Trigger GI.
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "GI", mms.NewBoolean(true)); err != nil {
		t.Fatalf("GI: %v", err)
	}

	rt.mu.Lock()
	newSeq := rt.seqNum
	rt.mu.Unlock()

	if newSeq <= prevSeq {
		t.Errorf("seqNum should have incremented after GI: got %d, prev %d", newSeq, prevSeq)
	}

	// GI flag should be reset.
	sk := servermodel.StoreKey("LD1", "LLN0$BR$brcb01$GI")
	gi := srv.store.Get(sk)
	if gi != nil {
		if b, ok := gi.Bool(); ok && b {
			t.Error("GI flag should be reset to false after trigger")
		}
	}
}

func TestReportEngine_GI_Disabled(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	prevSeq := rt.seqNum
	rt.mu.Unlock()

	// GI without enable should be a no-op.
	if err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$brcb01", "GI", mms.NewBoolean(true)); err != nil {
		t.Fatalf("GI: %v", err)
	}

	rt.mu.Lock()
	newSeq := rt.seqNum
	rt.mu.Unlock()

	if newSeq != prevSeq {
		t.Errorf("seqNum should not change when disabled: got %d, prev %d", newSeq, prevSeq)
	}
}

func TestReportEngine_IntegrityPeriod(t *testing.T) {
	model := testReportSCL()
	model.LogicalDevices[0].LogicalNodes[0].Reports[0].IntgPd = 50
	model.LogicalDevices[0].LogicalNodes[0].Reports[0].TrgOps.Period = true

	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]

	// Wait for at least one integrity period to fire.
	time.Sleep(150 * time.Millisecond)

	rt.mu.Lock()
	seq := rt.seqNum
	rt.mu.Unlock()

	if seq == 0 {
		t.Error("expected at least one integrity report to have been generated")
	}
}

func TestReportEngine_BufferedQueue(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")

	// Generate 5 data changes.
	for i := 0; i < 5; i++ {
		srv.SetValue(ctx, storeKey, mms.NewInteger(int64(i+1)))
	}

	rt.mu.Lock()
	qLen := len(rt.bufQueue)
	lastEntryID := rt.entryID
	rt.mu.Unlock()

	if qLen != 5 {
		t.Errorf("buffer queue length = %d, want 5", qLen)
	}
	if lastEntryID != 5 {
		t.Errorf("entryID = %d, want 5", lastEntryID)
	}
}

func TestReportEngine_BufferOverflow(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.bufMax = 3

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")

	// Generate 5 data changes, buffer should cap at 3.
	for i := 0; i < 5; i++ {
		srv.SetValue(ctx, storeKey, mms.NewInteger(int64(i+100)))
	}

	rt.mu.Lock()
	qLen := len(rt.bufQueue)
	rt.mu.Unlock()

	if qLen != 3 {
		t.Errorf("buffer queue length = %d, want 3 (overflow trim)", qLen)
	}
}

func TestReportEngine_PurgeBuf(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(ctx, storeKey, mms.NewInteger(99))

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	if len(rt.bufQueue) == 0 {
		t.Error("buffer should have entries before purge")
	}
	rt.mu.Unlock()

	// Purge buffer.
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "PurgeBuf", mms.NewBoolean(true)); err != nil {
		t.Fatalf("purge: %v", err)
	}

	rt.mu.Lock()
	if len(rt.bufQueue) != 0 {
		t.Errorf("buffer should be empty after purge, got %d entries", len(rt.bufQueue))
	}
	rt.mu.Unlock()
}

func TestReportEngine_NoChangeNoReport(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	currentVal := srv.store.Get(storeKey)

	rt.mu.Lock()
	prevSeq := rt.seqNum
	rt.mu.Unlock()

	// Set the same value — no data change should occur.
	if currentVal != nil {
		srv.SetValue(ctx, storeKey, currentVal)
	}

	rt.mu.Lock()
	newSeq := rt.seqNum
	rt.mu.Unlock()

	if newSeq != prevSeq {
		t.Errorf("seqNum changed on identical value write: %d -> %d", prevSeq, newSeq)
	}
}

func TestReportEngine_UnknownRCB(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$unknown", "RptEna", mms.NewBoolean(true))
	if err != nil {
		t.Fatalf("unknown RCB should not error (no-op): %v", err)
	}
}

func TestReportEngine_DoubleEnable(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("double enable should be no-op: %v", err)
	}
}

func TestReportEngine_StopIdempotent(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()

	engine.Stop()
	engine.Stop() // should not panic
}

func TestValuesEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b *mms.Value
		want bool
	}{
		{"nil==nil", nil, nil, true},
		{"nil!=val", nil, mms.NewBoolean(true), false},
		{"val!=nil", mms.NewBoolean(true), nil, false},
		{"bool_eq", mms.NewBoolean(true), mms.NewBoolean(true), true},
		{"bool_ne", mms.NewBoolean(true), mms.NewBoolean(false), false},
		{"int_eq", mms.NewInteger(42), mms.NewInteger(42), true},
		{"int_ne", mms.NewInteger(42), mms.NewInteger(43), false},
		{"uint_eq", mms.NewUnsigned(7), mms.NewUnsigned(7), true},
		{"uint_ne", mms.NewUnsigned(7), mms.NewUnsigned(8), false},
		{"string_eq", mms.NewVisibleString("hi"), mms.NewVisibleString("hi"), true},
		{"string_ne", mms.NewVisibleString("hi"), mms.NewVisibleString("bye"), false},
		{"type_mismatch", mms.NewBoolean(true), mms.NewInteger(1), false},
		{"octet_eq", mms.NewOctetString([]byte{1, 2}), mms.NewOctetString([]byte{1, 2}), true},
		{"octet_ne", mms.NewOctetString([]byte{1, 2}), mms.NewOctetString([]byte{1, 3}), false},
		{"bits_eq",
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			true,
		},
		{"bits_ne",
			mms.NewBitStringWithLength([]byte{0x80}, 1),
			mms.NewBitStringWithLength([]byte{0x00}, 1),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valuesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("valuesEqual = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeReportValues(t *testing.T) {
	optFlds := OptFldSeqNum | OptFldReasonCode | OptFldConfRev
	inclusion := []bool{true, false, true}
	reasons := []ReasonCode{ReasonDataChanged, 0, ReasonGI}
	values := []*mms.Value{
		mms.NewInteger(10),
		mms.NewInteger(20),
		mms.NewInteger(30),
	}

	result := encodeReportValues("rpt01", optFlds, 5, 1, "ds01", inclusion, reasons, values)

	// RptID
	rptID, ok := result[0].VisibleString()
	if !ok || rptID != "rpt01" {
		t.Errorf("RptID = %q, want rpt01", rptID)
	}

	// OptFlds (bitstring)
	if result[1].Type() != mms.ValueTypeBitString {
		t.Error("OptFlds should be bitstring")
	}

	// SeqNum
	seq, ok := result[2].Uint32()
	if !ok || seq != 5 {
		t.Errorf("SeqNum = %d, want 5", seq)
	}

	// ConfRev (since SeqNum is present, next optional field)
	confRev, ok := result[3].Uint32()
	if !ok || confRev != 1 {
		t.Errorf("ConfRev = %d, want 1", confRev)
	}

	// Verify 2 included values present (indices 0 and 2).
	// After SubSeqNum, MoreSegments, Inclusion, there should be included values.
}

func TestEncodeInclusion(t *testing.T) {
	inclusion := []bool{true, false, true, true, false, false, false, false, true}
	v := encodeInclusion(inclusion)

	bits, ok := v.BitString()
	if !ok {
		t.Fatal("expected bitstring")
	}

	// First byte: bits 0,2,3 set -> 10110000 = 0xB0
	if bits[0] != 0xB0 {
		t.Errorf("first byte = 0x%02x, want 0xB0", bits[0])
	}
	// Second byte: bit 8 set -> 10000000 = 0x80
	if bits[1] != 0x80 {
		t.Errorf("second byte = 0x%02x, want 0x80", bits[1])
	}
}

func TestEncodeEntryID(t *testing.T) {
	id := encodeEntryID(0x0102030405060708)
	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if len(id) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(id))
	}
	for i := range expected {
		if id[i] != expected[i] {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, id[i], expected[i])
		}
	}
}

func TestEncodeReasonCode(t *testing.T) {
	v := encodeReasonCode(ReasonDataChanged | ReasonGI)
	bits, ok := v.BitString()
	if !ok {
		t.Fatal("expected bitstring")
	}
	// DataChanged = bit 1 (0x40), GI = bit 5 (0x04)
	if bits[0] != 0x44 {
		t.Errorf("reason bits = 0x%02x, want 0x44", bits[0])
	}
}

func TestReportEngine_SequenceNumbering(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")

	for i := 0; i < 10; i++ {
		srv.SetValue(ctx, storeKey, mms.NewInteger(int64(i*100)))
	}

	rt.mu.Lock()
	seq := rt.seqNum
	rt.mu.Unlock()

	if seq != 10 {
		t.Errorf("seqNum = %d, want 10", seq)
	}
}

func TestReportEngine_EntryIDEncoding(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(ctx, storeKey, mms.NewInteger(1))

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	if len(rt.bufQueue) == 0 {
		rt.mu.Unlock()
		t.Fatal("expected buffered entry")
	}
	entry := rt.bufQueue[0]
	rt.mu.Unlock()

	eid := binary.BigEndian.Uint64(entry.entryID)
	if eid != 1 {
		t.Errorf("first entryID = %d, want 1", eid)
	}

	// Check store also updated.
	sk := servermodel.StoreKey("LD1", "LLN0$BR$brcb01$EntryID")
	v := srv.store.Get(sk)
	if v == nil {
		t.Fatal("EntryID not set in store")
	}
}

func TestReportEngine_EnableWithBadDataset(t *testing.T) {
	model := testReportSCL()
	model.LogicalDevices[0].LogicalNodes[0].Reports[0].DatSet = "nonexistent"

	srv := newTestServerOpts(t, model, false)
	engine := srv.EnableReports()
	defer engine.Stop()

	err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true))
	if err == nil {
		t.Fatal("expected error for nonexistent dataset")
	}
}

func TestReportEngine_SqNumInStore(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(ctx, storeKey, mms.NewInteger(7))

	sk := servermodel.StoreKey("LD1", "LLN0$BR$brcb01$SqNum")
	v := srv.store.Get(sk)
	if v == nil {
		t.Fatal("SqNum not set in store")
	}
	seq, ok := v.Uint32()
	if !ok || seq != 1 {
		t.Errorf("SqNum in store = %d, want 1", seq)
	}
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
