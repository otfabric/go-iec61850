// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// --- M16.1 Concurrency tests ---

func TestReportEngine_ConcurrentSetValue(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			srv.SetValue(ctx, storeKey, mms.NewInteger(int64(val)))
		}(i)
	}
	wg.Wait()

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	seq := rt.seqNum
	rt.mu.Unlock()

	if seq == 0 {
		t.Error("expected some reports to be generated")
	}
}

func TestReportEngine_ConcurrentEnableDisable(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			enable := i%2 == 0
			_ = engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(enable))
		}(i)
	}
	wg.Wait()
}

func TestReportEngine_ConcurrentGI(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "GI", mms.NewBoolean(true))
		}()
	}
	wg.Wait()
}

func TestControl_ConcurrentSBOOperate(t *testing.T) {
	model := testControlSCL()
	srv := newTestServer(t, model)

	var operateCount int32
	var mu sync.Mutex

	_ = srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBOEnhanced, ControlHandler{
		OnSelect: func(_ context.Context, _ ControlRequest) error {
			return nil
		},
		OnOperate: func(_ context.Context, _ ControlRequest) error {
			mu.Lock()
			operateCount++
			mu.Unlock()
			return nil
		},
	})

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			origin := Origin{OrCat: OrCatBayControl, OrIdent: []byte(fmt.Sprintf("op%d", i))}
			selectVal := mms.NewStructure([]*mms.Value{
				mms.NewBoolean(true),
				mms.NewUTCTime(time.Now()),
				mms.NewStructure([]*mms.Value{mms.NewInteger(int64(origin.OrCat)), mms.NewOctetString(origin.OrIdent)}),
				mms.NewUnsigned(uint64(i)),
				mms.NewUTCTime(time.Time{}),
				mms.NewBoolean(false),
				mms.NewBitStringWithLength([]byte{0x00}, 2),
			})
			_ = srv.handleControlWrite(ctx, "LD1", "GGIO1", []string{"SPCSO1", "SBOw"}, "SBOw", selectVal)

			operVal := mms.NewStructure([]*mms.Value{
				mms.NewBoolean(true),
				mms.NewUTCTime(time.Now()),
				mms.NewStructure([]*mms.Value{mms.NewInteger(int64(origin.OrCat)), mms.NewOctetString(origin.OrIdent)}),
				mms.NewUnsigned(uint64(i)),
				mms.NewUTCTime(time.Time{}),
				mms.NewBoolean(false),
				mms.NewBitStringWithLength([]byte{0x00}, 2),
			})
			_ = srv.handleControlWrite(ctx, "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
		}(i)
	}
	wg.Wait()
}

func TestSettingGroup_ConcurrentWrites(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)

	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sg := uint8(i%3 + 1)
			_ = srv.sgEngine.HandleSGCBWrite(ctx, "LD1", "ActSG", mms.NewUnsigned(uint64(sg)))
		}(i)
	}
	wg.Wait()

	got := srv.sgEngine.GetActiveSettingGroup("LD1")
	if got == 0 || got > 3 {
		t.Errorf("active SG out of range: %d", got)
	}
}

func TestJournalProvider_ConcurrentAddRead(t *testing.T) {
	p := NewMemoryJournalProvider(WithMaxEntries(100))
	p.RegisterJournal("LD1", "journal1")

	ctx := context.Background()
	now := time.Now()

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p.AddEntry("LD1", "journal1", now.Add(time.Duration(i)*time.Millisecond), []mms.JournalVariable{
				{Tag: fmt.Sprintf("tag%d", i), Value: mms.NewInteger(int64(i))},
			})
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.ReadTimeRange(ctx, "LD1", "journal1", now.Add(-time.Second), now.Add(time.Minute), 50)
		}()
	}
	wg.Wait()

	count := p.EntryCount("LD1", "journal1")
	if count != 20 {
		t.Errorf("expected 20 entries, got %d", count)
	}
}

// --- M16.2 Failure-injection tests ---

func TestReportEngine_BufferOverflow_Concurrent(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()
	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.bufMax = 5

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			srv.SetValue(ctx, storeKey, mms.NewInteger(int64(v)))
		}(i)
	}
	wg.Wait()

	rt.mu.Lock()
	qLen := len(rt.bufQueue)
	rt.mu.Unlock()

	if qLen > 5 {
		t.Errorf("buffer should be capped at 5, got %d", qLen)
	}
}

func TestReportEngine_InvalidSubfield(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()
	err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "UnknownField", mms.NewBoolean(true))
	if err != nil {
		t.Errorf("unknown subfield should be silently ignored, got: %v", err)
	}
}

func TestReportEngine_InvalidRptEnaType(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewInteger(42))
	if err == nil {
		t.Error("expected error for non-boolean RptEna")
	}
}

func TestReportEngine_InvalidGIType(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$brcb01", "GI", mms.NewInteger(1))
	if err == nil {
		t.Error("expected error for non-boolean GI")
	}
}

func TestReportEngine_InvalidResvType(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$RP$urcb01", "Resv", mms.NewInteger(1))
	if err == nil {
		t.Error("expected error for non-boolean Resv")
	}
}

func TestReportEngine_EnableDisableRapid(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_ = engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true))
		_ = engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(false))
	}
}

func TestReportEngine_StopThenSetValue(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()

	ctx := context.Background()
	_ = engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true))

	engine.Stop()

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(ctx, storeKey, mms.NewInteger(42))
}

func TestSettingGroup_ActSGOutOfRange(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()

	err := srv.sgEngine.HandleSGCBWrite(ctx, "LD1", "ActSG", mms.NewUnsigned(0))
	if err == nil {
		t.Error("expected error for ActSG=0")
	}

	err = srv.sgEngine.HandleSGCBWrite(ctx, "LD1", "ActSG", mms.NewUnsigned(99))
	if err == nil {
		t.Error("expected error for ActSG out of range")
	}
}

func TestSettingGroup_EditSGOutOfRange(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	err := srv.sgEngine.HandleSGCBWrite(context.Background(), "LD1", "EditSG", mms.NewUnsigned(99))
	if err == nil {
		t.Error("expected error for EditSG out of range")
	}
}

func TestSettingGroup_ConfirmWithoutEdit(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	err := srv.sgEngine.HandleSGCBWrite(context.Background(), "LD1", "CnfEdit", mms.NewBoolean(true))
	if err == nil {
		t.Error("expected error for CnfEdit without active edit session")
	}
}

func TestSettingGroup_DoubleEditReject(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	ctx := context.Background()

	if err := srv.sgEngine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(1)); err != nil {
		t.Fatalf("first edit: %v", err)
	}

	err := srv.sgEngine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(2))
	if err == nil {
		t.Error("expected error for second edit while first is active")
	}

	if err := srv.sgEngine.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(0)); err != nil {
		t.Fatalf("release edit: %v", err)
	}
}

func TestSettingGroup_UnknownLD(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	err := srv.sgEngine.HandleSGCBWrite(context.Background(), "NoSuchLD", "ActSG", mms.NewUnsigned(1))
	if err == nil {
		t.Error("expected error for unknown LD")
	}
}

func TestSettingGroup_ReadOnlySubfield(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	err := srv.sgEngine.HandleSGCBWrite(context.Background(), "LD1", "NumOfSGs", mms.NewUnsigned(5))
	if err == nil {
		t.Error("expected error for write to read-only subfield")
	}
}

func TestSettingGroup_InvalidActSGType(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)
	srv.EnableSettingGroups(SettingGroupHandler{})

	err := srv.sgEngine.HandleSGCBWrite(context.Background(), "LD1", "ActSG", mms.NewBoolean(true))
	if err == nil {
		t.Error("expected error for non-unsigned ActSG")
	}
}

func TestSettingGroup_HandlerReject(t *testing.T) {
	model := testSGCBModel()
	srv := newTestServer(t, model)

	srv.EnableSettingGroups(SettingGroupHandler{
		OnActiveSGChanged: func(_ context.Context, _ string, _ uint8) error {
			return fmt.Errorf("application rejected")
		},
	})

	err := srv.sgEngine.HandleSGCBWrite(context.Background(), "LD1", "ActSG", mms.NewUnsigned(2))
	if err == nil {
		t.Error("expected error when handler rejects")
	}
}

func TestJournalProvider_Overflow(t *testing.T) {
	p := NewMemoryJournalProvider(WithMaxEntries(5))
	p.RegisterJournal("LD1", "j1")

	now := time.Now()
	for i := 0; i < 20; i++ {
		p.AddEntry("LD1", "j1", now.Add(time.Duration(i)*time.Millisecond), []mms.JournalVariable{
			{Tag: "v", Value: mms.NewInteger(int64(i))},
		})
	}

	count := p.EntryCount("LD1", "j1")
	if count != 5 {
		t.Errorf("expected 5 entries after overflow, got %d", count)
	}
}

func TestJournalProvider_EmptyJournal(t *testing.T) {
	p := NewMemoryJournalProvider()
	p.RegisterJournal("LD1", "j1")

	ctx := context.Background()
	result, err := p.ReadTimeRange(ctx, "LD1", "j1", time.Now().Add(-time.Hour), time.Now(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result.Entries))
	}
	if result.MoreFollows {
		t.Error("MoreFollows should be false for empty journal")
	}
}

func TestJournalProvider_NonexistentJournal(t *testing.T) {
	p := NewMemoryJournalProvider()

	ctx := context.Background()
	result, err := p.ReadTimeRange(ctx, "LD1", "nonexistent", time.Now().Add(-time.Hour), time.Now(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result.Entries))
	}
}

func TestServer_Close_Idempotent(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	_ = engine

	srv.Close()
	srv.Close()
	srv.Close()
}

func TestServer_Close_WithoutEngines(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	srv.Close()
}

func TestServer_SetValue_WithoutEngines(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(context.Background(), storeKey, mms.NewInteger(42))

	got := srv.store.Get(storeKey)
	if got == nil {
		t.Fatal("value should be stored")
	}
	v, ok := got.Int64()
	if !ok || v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestControl_OperateWithoutSelect_SBO(t *testing.T) {
	model := testControlSCL()
	srv := newTestServer(t, model)

	_ = srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelSBONormal, ControlHandler{
		OnOperate: func(_ context.Context, _ ControlRequest) error {
			return nil
		},
	})

	operVal := mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true),
		mms.NewUTCTime(time.Now()),
		mms.NewStructure([]*mms.Value{
			mms.NewInteger(int64(OrCatBayControl)),
			mms.NewOctetString([]byte("op1")),
		}),
		mms.NewUnsigned(1),
		mms.NewUTCTime(time.Time{}),
		mms.NewBoolean(false),
		mms.NewBitStringWithLength([]byte{0x00}, 2),
	})

	err := srv.handleControlWrite(context.Background(), "LD1", "GGIO1", []string{"SPCSO1", "Oper"}, "Oper", operVal)
	if err == nil {
		t.Error("expected error for operate without prior select in SBO model")
	}
}

func TestControl_RegisterDuplicate(t *testing.T) {
	model := testControlSCL()
	srv := newTestServer(t, model)

	if err := srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{}); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{})
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestControl_RegisterStatusOnly(t *testing.T) {
	model := testControlSCL()
	srv := newTestServer(t, model)

	err := srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelStatusOnly, ControlHandler{})
	if err == nil {
		t.Error("expected error for status-only ctlModel")
	}
}

func TestControl_RegisterEmptyArgs(t *testing.T) {
	model := testControlSCL()
	srv := newTestServer(t, model)

	if err := srv.RegisterControl("", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{}); err == nil {
		t.Error("expected error for empty ldName")
	}
	if err := srv.RegisterControl("LD1", "", CtlModelDirectNormal, ControlHandler{}); err == nil {
		t.Error("expected error for empty doRef")
	}
}

// --- M16.3 Integration/conformance tests ---

func TestServer_FullLifecycle(t *testing.T) {
	s := testServerSCLWithBRCB()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("model: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{
		Identity: &ServerIdentity{
			Vendor: "TestCo", Model: "Srv1", Revision: "1.0",
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	engine := srv.EnableReports()
	_ = engine

	caps := srv.Capabilities()
	if !caps.Variables {
		t.Error("expected Variables capability")
	}
	if !caps.Reports {
		t.Error("expected Reports capability")
	}
	if !caps.Identify {
		t.Error("expected Identify capability")
	}
	if !caps.DataSets {
		t.Error("expected DataSets capability")
	}

	ctx := context.Background()
	clientT, serverT := loopbackPair()

	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	defer func() { _ = mmsClient.Close(ctx) }()

	id, err := mmsClient.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.Vendor != "TestCo" {
		t.Errorf("Vendor = %q, want TestCo", id.Vendor)
	}

	st, err := mmsClient.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Logical != mms.VMDLogicalStatusStateChangesAllowed {
		t.Errorf("unexpected logical status: %v", st.Logical)
	}
}

func TestServer_SetValue_TriggersReportAndJournal(t *testing.T) {
	model := testReportSCLWithLog()
	srv := newTestServer(t, model)

	engine := srv.EnableReports()
	defer engine.Stop()

	jp := NewMemoryJournalProvider()
	je := srv.EnableJournals(WithJournalProvider(jp))

	ctx := context.Background()

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcb01", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	storeKey := servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	srv.SetValue(ctx, storeKey, mms.NewInteger(99))

	rt := engine.rcbs["LD1/LLN0$BR$brcb01"]
	rt.mu.Lock()
	seq := rt.seqNum
	rt.mu.Unlock()

	if seq == 0 {
		t.Error("expected report to be generated")
	}

	count := je.Provider().EntryCount("LD1", "LLN0$log1")
	if count == 0 {
		t.Error("expected journal entry to be generated")
	}
}

// --- Test helpers for M16 ---

func testControlSCL() *servermodel.Model {
	return &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{
				{
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
				},
				{
					Name:    "GGIO1",
					LNClass: "GGIO",
					DataObjects: []servermodel.DataObject{{
						Name: "SPCSO1",
						Attributes: []servermodel.DataAttribute{
							{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
							{Name: "q", FC: "ST", BType: "Quality"},
							{Name: "t", FC: "ST", BType: "Timestamp"},
							{Name: "ctlVal", FC: "CO", BType: "BOOLEAN"},
						},
					}},
				},
			},
		}},
	}
}

func testSGCBModel() *servermodel.Model {
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
				SettingGroup: &servermodel.SettingGroupDef{
					NumOfSGs: 3,
					ActSG:    1,
				},
			}},
		}},
	}
}

func TestInterceptor_NoRecursion(t *testing.T) {
	model := testReportSCL()
	srv := newTestServer(t, model)
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()

	var interceptorCallCount int
	srv.store.SetWriteInterceptor(func(_ context.Context, storeKey string, _ *mms.Value) (bool, error) {
		interceptorCallCount++
		srv.store.Set(storeKey+".echo", mms.NewBoolean(true))
		return false, nil
	})

	storeKey := "LD1/LLN0$BR$brcb01$RptEna"
	srv.store.Set(storeKey, mms.NewBoolean(true))
	_, _ = srv.store.CallInterceptorForTest(ctx, storeKey, mms.NewBoolean(true))

	if interceptorCallCount != 1 {
		t.Errorf("interceptor called %d times, expected exactly 1 (no recursion)", interceptorCallCount)
	}

	echo := srv.store.Get(storeKey + ".echo")
	if echo == nil {
		t.Error("echo store write should have completed")
	}
}

func TestServer_Close_ClearsEngines(t *testing.T) {
	model := testReportSCLWithLog()
	srv := newTestServer(t, model)
	_ = srv.EnableReports()

	jp := NewMemoryJournalProvider()
	_ = srv.EnableJournals(WithJournalProvider(jp))

	srv.EnableSettingGroups(SettingGroupHandler{})

	if srv.ReportEngine() == nil {
		t.Fatal("report engine should be set before Close")
	}
	if srv.JournalEngine() == nil {
		t.Fatal("journal engine should be set before Close")
	}
	if srv.SettingGroupEngine() == nil {
		t.Fatal("SG engine should be set before Close")
	}

	srv.Close()

	if srv.JournalEngine() != nil {
		t.Error("journal engine should be nil after Close")
	}
	if srv.SettingGroupEngine() != nil {
		t.Error("SG engine should be nil after Close")
	}
}

func testReportSCLWithLog() *servermodel.Model {
	m := testReportSCL()
	m.LogicalDevices[0].LogicalNodes[0].Logs = []servermodel.LogDef{
		{Name: "log1"},
	}
	return m
}
