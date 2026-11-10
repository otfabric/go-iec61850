// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// Spec-verification tests of the server report engine against
// IEC 61850-7-2 §17.2 and IEC 61850-8-1 §17.1/§17.2.

const (
	specBRCB = "LLN0$BR$brcb01"
	specURCB = "LLN0$RP$urcb01"
)

var (
	specModKey = servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal")
	specBehKey = servermodel.StoreKey("LD1", "LLN0$ST$Beh$stVal")
)

// specModel has a BRCB and a URCB on dsTest {Mod.stVal}, and a second
// data set dsTwo {Mod.stVal, Beh.stVal}. Both RCBs ask for every optional
// field, including BufOvfl and EntryID on the URCB (which it must ignore).
func specModel() *servermodel.Model {
	opt := servermodel.OptFieldsDef{
		SeqNum: true, TimeStamp: true, ReasonCode: true, DataSet: true,
		EntryID: true, ConfigRef: true, BufOvfl: true,
	}
	return &servermodel.Model{
		LogicalDevices: []servermodel.LogicalDevice{{
			Name: "LD1",
			LogicalNodes: []servermodel.LogicalNode{{
				Name:    "LLN0",
				LNClass: "LLN0",
				DataObjects: []servermodel.DataObject{
					{Name: "Mod", Attributes: []servermodel.DataAttribute{
						{Name: "stVal", FC: "ST", BType: "INT32"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					}},
					{Name: "Beh", Attributes: []servermodel.DataAttribute{
						{Name: "stVal", FC: "ST", BType: "INT32"},
					}},
				},
				DataSets: []servermodel.DataSetDef{
					{Name: "dsTest", Members: []servermodel.DataSetMemberDef{
						{LNName: "LLN0", DOPath: "Mod.stVal", FC: "ST"},
					}},
					{Name: "dsTwo", Members: []servermodel.DataSetMemberDef{
						{LNName: "LLN0", DOPath: "Mod.stVal", FC: "ST"},
						{LNName: "LLN0", DOPath: "Beh.stVal", FC: "ST"},
					}},
				},
				Reports: []servermodel.ReportDef{
					{Name: "brcb01", RptID: "rpt_brcb01", DatSet: "dsTest", ConfRev: 1, Buffered: true,
						TrgOps: servermodel.TrgOpsDef{Dchg: true, GI: true}, OptFlds: opt},
					{Name: "urcb01", RptID: "rpt_urcb01", DatSet: "dsTest", ConfRev: 1,
						TrgOps: servermodel.TrgOpsDef{Dchg: true, GI: true}, OptFlds: opt},
				},
			}},
		}},
	}
}

func newSpecServer(t *testing.T, opts ServerOptions) (*Server, *ReportEngine) {
	t.Helper()
	srv, err := NewServer(specModel(), opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	engine := srv.EnableReports()
	t.Cleanup(srv.Close)
	return srv, engine
}

func subscribeRCB(t *testing.T, c *Client, rptID, rcb string, enable bool) *ReportSubscription {
	t.Helper()
	opts := SubscribeReportOptions{QueueSize: 1024}
	if enable {
		opts.LD, opts.RCBItemID, opts.AutoEnable = "LD1", rcb, true
	}
	sub, err := c.SubscribeReport(context.Background(), rptID, opts)
	if err != nil {
		t.Fatalf("SubscribeReport %s: %v", rcb, err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

func expectNoReport(t *testing.T, sub *ReportSubscription, d time.Duration) {
	t.Helper()
	select {
	case r := <-sub.Reports():
		t.Fatalf("unexpected report SqNum=%d EntryID=%x", r.SeqNum, r.EntryID)
	case <-time.After(d):
	}
}

func rawWrite(c *Client, item string, v *mms.Value) error {
	_, err := c.mmsClient.Write(context.Background(), mms.WriteRequest{DomainID: "LD1", ItemID: mms.ItemID(item), Value: v})
	return err
}

func rawRead(t *testing.T, c *Client, item string) *mms.Value {
	t.Helper()
	res, err := c.mmsClient.Read(context.Background(), mms.ReadRequest{DomainID: "LD1", ItemID: mms.ItemID(item)})
	if err != nil || res == nil || res.Value == nil {
		t.Fatalf("read %s: %v", item, err)
	}
	return res.Value
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func rcbOwner(engine *ReportEngine, rcb string) *mms.ServerConn {
	rt := engine.rcbs["LD1/"+rcb]
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.owner
}

// TestSpec_7_2_17_2_1_BRCBReportsOnlyToEnablingAssociation: "That client
// exclusively shall 'own' that instance and shall receive reports from
// that instance" (RPT-01).
func TestSpec_7_2_17_2_1_BRCBReportsOnlyToEnablingAssociation(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	a := dialLoopback(t, ctx, srv)
	b := dialLoopback(t, ctx, srv)
	subA := subscribeRCB(t, a, "rpt_brcb01", specBRCB, true)
	subB := subscribeRCB(t, b, "rpt_brcb01", "", false)

	srv.SetValue(ctx, specModKey, mms.NewInteger(5))
	recvReport(t, subA)
	expectNoReport(t, subA, 300*time.Millisecond)
	expectNoReport(t, subB, 200*time.Millisecond)
}

// TestSpec_7_2_17_2_4_5_URCBImplicitReservation: "If the attribute Resv
// is not set to TRUE, then setting the attribute RptEna to TRUE reserves
// the instance implicitly"; "Other clients shall not be allowed to set
// any attribute of that URCB"; reserved by another client maps to
// temporarily-unavailable (8-1 §8.1.3.4.4.2) (RPT-03, RPT-18).
func TestSpec_7_2_17_2_4_5_URCBImplicitReservation(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	a := dialLoopback(t, ctx, srv)
	b := dialLoopback(t, ctx, srv)
	subA := subscribeRCB(t, a, "rpt_urcb01", specURCB, true)

	if resv, _ := rawRead(t, b, specURCB+"$Resv").Bool(); !resv {
		t.Error("Resv should read TRUE after an implicit reservation")
	}
	for _, w := range []struct {
		attr string
		v    *mms.Value
	}{
		{"DatSet", mms.NewVisibleString("LD1/LLN0$dsTwo")},
		{"GI", mms.NewBoolean(true)},
		{"RptEna", mms.NewBoolean(false)},
		{"Resv", mms.NewBoolean(true)},
		{"IntgPd", mms.NewUnsigned(1000)},
	} {
		err := rawWrite(b, specURCB+"$"+w.attr, w.v)
		if code := dataAccessCode(err); code != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("B writes %s: err = %v, want temporarily-unavailable", w.attr, err)
		}
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(9))
	r := recvReport(t, subA)
	if v, _ := r.Values[0].Int64(); v != 9 {
		t.Errorf("A report value = %d, want 9", v)
	}
}

// TestSpec_7_2_TableC3_BRCBFirstEnablerOwns: without ResvTms "the
// reservation of the control block shall occur ... by the client that
// performs the first SetBRCBValues with RptEna=TRUE" (Table 37 c3).
func TestSpec_7_2_TableC3_BRCBFirstEnablerOwns(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	a := dialLoopback(t, ctx, srv)
	b := dialLoopback(t, ctx, srv)
	subA := subscribeRCB(t, a, "rpt_brcb01", specBRCB, true)
	for _, attr := range []string{"RptEna", "GI"} {
		v := mms.NewBoolean(attr == "GI")
		if code := dataAccessCode(rawWrite(b, specBRCB+"$"+attr, v)); code != mms.DataAccessErrorTemporarilyUnavailable {
			t.Errorf("B writes %s: code %v, want temporarily-unavailable", attr, code)
		}
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(3))
	recvReport(t, subA)
}

// TestSpec_7_2_17_2_4_4_URCBAssociationLossDisablesAndReleases: "If the
// two-party-application-association to the client over which URCB has
// been enabled is lost, the server shall set the attribute RptEna to
// FALSE" (RPT-02).
func TestSpec_7_2_17_2_4_4_URCBAssociationLossDisablesAndReleases(t *testing.T) {
	srv, engine := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	a := dialLoopback(t, ctx, srv)
	if err := a.SetReportControlBlock(ctx, "LD1", specURCB, RCBUpdate{
		Fields: RCBFieldResv | RCBFieldRptEna, Resv: true, RptEna: true,
	}); err != nil {
		t.Fatal(err)
	}
	dropAssociation(a)
	waitUntil(t, "release", func() bool { return rcbOwner(engine, specURCB) == nil })

	b := dialLoopback(t, ctx, srv)
	rcb, err := b.GetReportControlBlock(ctx, "LD1", specURCB)
	if err != nil {
		t.Fatal(err)
	}
	if rcb.RptEna || rcb.Resv {
		t.Errorf("after association loss RptEna=%v Resv=%v, want false/false", rcb.RptEna, rcb.Resv)
	}
	if err := b.SetReportControlBlock(ctx, "LD1", specURCB, RCBUpdate{Fields: RCBFieldResv, Resv: true}); err != nil {
		t.Fatalf("B reserve: %v", err)
	}
	sub := subscribeRCB(t, b, "rpt_urcb01", specURCB, true)
	srv.SetValue(ctx, specModKey, mms.NewInteger(4))
	recvReport(t, sub)
}

// TestSpec_7_2_17_2_2_5_BRCBAssociationLossThenReplay: "When an
// association is lost, the state shall transition to disabled"
// (Figure 24) and the BRCB keeps buffering (§17.2.3.2.3.6) (RPT-02,
// RPT-08).
func TestSpec_7_2_17_2_2_5_BRCBAssociationLossThenReplay(t *testing.T) {
	srv, engine := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	a := dialLoopback(t, ctx, srv)
	if err := a.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldRptEna, RptEna: true}); err != nil {
		t.Fatal(err)
	}
	dropAssociation(a)
	waitUntil(t, "release", func() bool { return rcbOwner(engine, specBRCB) == nil })
	if ena, _ := srv.store.Get(servermodel.StoreKey("LD1", specBRCB+"$RptEna")).Bool(); ena {
		t.Fatal("RptEna still TRUE after association loss")
	}
	for i := 1; i <= 3; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
	}
	b := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, b, "rpt_brcb01", specBRCB, true)
	for i := 1; i <= 3; i++ {
		r := recvReport(t, sub)
		if got := decodeEntryID(r.EntryID); got != uint64(i) {
			t.Errorf("#%d EntryID = %d", i, got)
		}
		if v, _ := r.Values[0].Int64(); v != int64(i) {
			t.Errorf("#%d value = %d", i, v)
		}
	}
}

// TestSpec_8_1_Table61_63_WriteWhileEnabledRejected: attributes marked
// "May not be written when RptEna=TRUE" fail with temporarily-unavailable
// (8-1 §8.1.3.4.4.2) and keep their value (RPT-04).
func TestSpec_8_1_Table61_63_WriteWhileEnabledRejected(t *testing.T) {
	writes := map[string]*mms.Value{
		"RptID":   mms.NewVisibleString("other"),
		"DatSet":  mms.NewVisibleString("LD1/LLN0$dsTwo"),
		"OptFlds": encodeOptFlds(OptFldSeqNum),
		"BufTm":   mms.NewUnsigned(50),
		"TrgOps":  encodeTrgOps(TrgOpDataUpdate),
		"IntgPd":  mms.NewUnsigned(500),
	}
	for _, tc := range []struct {
		rcb   string
		extra map[string]*mms.Value
	}{
		{specBRCB, map[string]*mms.Value{"PurgeBuf": mms.NewBoolean(true), "EntryID": mms.NewOctetString(encodeEntryID(1))}},
		{specURCB, map[string]*mms.Value{"Resv": mms.NewBoolean(false)}},
	} {
		t.Run(tc.rcb, func(t *testing.T) {
			srv, _ := newSpecServer(t, ServerOptions{})
			ctx := context.Background()
			c := dialLoopback(t, ctx, srv)
			if err := c.SetReportControlBlock(ctx, "LD1", tc.rcb, RCBUpdate{Fields: RCBFieldRptEna, RptEna: true}); err != nil {
				t.Fatal(err)
			}
			all := map[string]*mms.Value{}
			for k, v := range writes {
				all[k] = v
			}
			for k, v := range tc.extra {
				all[k] = v
			}
			for attr, v := range all {
				item := tc.rcb + "$" + attr
				before := rawRead(t, c, item)
				err := rawWrite(c, item, v)
				if code := dataAccessCode(err); code != mms.DataAccessErrorTemporarilyUnavailable {
					t.Errorf("%s: err = %v, want temporarily-unavailable", attr, err)
				}
				if after := rawRead(t, c, item); !valuesEqual(before, after) {
					t.Errorf("%s changed by a refused write", attr)
				}
			}
		})
	}
}

// TestSpec_8_1_Table61_ReadOnlyAttributesDenied: ConfRev, SqNum and
// TimeOfEntry "May not be written": object-access-denied (8-1
// §8.1.3.4.4.2) (RPT-05).
func TestSpec_8_1_Table61_ReadOnlyAttributesDenied(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	for attr, v := range map[string]*mms.Value{
		"ConfRev":     mms.NewUnsigned(99),
		"SqNum":       mms.NewUnsigned(7),
		"TimeOfEntry": mms.NewBinaryTime(time.Now().UnixMilli()),
	} {
		if code := dataAccessCode(rawWrite(c, specBRCB+"$"+attr, v)); code != mms.DataAccessErrorObjectAccessDenied {
			t.Errorf("%s: code %v, want object-access-denied", attr, code)
		}
	}
	if code := dataAccessCode(rawWrite(c, specURCB+"$ConfRev", mms.NewUnsigned(99))); code != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("URCB ConfRev: code %v", code)
	}
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	if r := recvReport(t, sub); r.ConfRev != 1 {
		t.Errorf("report ConfRev = %d, want 1", r.ConfRev)
	}
}

// TestSpec_8_1_8_1_3_4_4_2_StructureWriteDenied: the whole RCB has
// read-only components, so a write of the structure is denied (RPT-19).
func TestSpec_8_1_8_1_3_4_4_2_StructureWriteDenied(t *testing.T) {
	srv, engine := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	v := rawRead(t, c, specBRCB)
	if code := dataAccessCode(rawWrite(c, specBRCB, v)); code != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("structure write: code %v, want object-access-denied", code)
	}
	rt := engine.rcbs["LD1/"+specBRCB]
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.enabled {
		t.Error("structure write enabled the RCB")
	}
}

// TestSpec_8_1_8_1_3_4_4_2_RCBErrorCodes: wrong type → type-inconsistent,
// RptEna with a NULL DatSet → object-value-invalid (8-1 §17.1.2, Table 23)
// (RPT-18).
func TestSpec_8_1_8_1_3_4_4_2_RCBErrorCodes(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	if code := dataAccessCode(rawWrite(c, specURCB+"$RptEna", mms.NewInteger(1))); code != mms.DataAccessErrorTypeInconsistent {
		t.Errorf("RptEna integer: code %v, want type-inconsistent", code)
	}
	if err := rawWrite(c, specURCB+"$DatSet", mms.NewVisibleString("")); err != nil {
		t.Fatalf("DatSet NULL: %v", err)
	}
	if code := dataAccessCode(rawWrite(c, specURCB+"$RptEna", mms.NewBoolean(true))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("RptEna with NULL DatSet: code %v, want object-value-invalid", code)
	}
	if ena, _ := rawRead(t, c, specURCB+"$RptEna").Bool(); ena {
		t.Error("RptEna TRUE after a refused enable")
	}
}

// TestSpec_7_2_17_2_2_10_SqNumStartsAtZeroAndWraps: "Upon that
// transition, the value of SqNum shall be set to zero (0) ... When the
// maximum value of SqNum is obtained, the next value shall be a value of
// zero (0)"; INT8U for a URCB (§17.2.5.2), INT16U for a BRCB (RPT-06).
func TestSpec_7_2_17_2_2_10_SqNumStartsAtZeroAndWraps(t *testing.T) {
	srv, engine := newSpecServer(t, ServerOptions{})
	ctx := context.Background()

	step := func(rcb string, n int, start uint32) []uint32 {
		rt := engine.rcbs["LD1/"+rcb]
		if err := engine.HandleRCBWrite(ctx, "LD1", rcb, "RptEna", mms.NewBoolean(true)); err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		rt.mu.Lock()
		if rt.seqNum != 0 {
			t.Errorf("%s: SqNum after enable = %d, want 0", rcb, rt.seqNum)
		}
		rt.seqNum = start
		rt.sendStop = stop
		rt.mu.Unlock()
		for i := range n {
			srv.SetValue(ctx, specModKey, mms.NewInteger(int64(1000+i)))
		}
		var got []uint32
		for range n {
			segs, ok := engine.nextReport(rt, stop)
			if !ok {
				t.Fatalf("%s: no report", rcb)
			}
			sq, _ := segs[0][2].Uint32() // RptID, OptFlds, SqNum
			got = append(got, sq)
		}
		rt.mu.Lock()
		rt.sendStop = nil
		rt.mu.Unlock()
		if err := engine.HandleRCBWrite(ctx, "LD1", rcb, "RptEna", mms.NewBoolean(false)); err != nil {
			t.Fatal(err)
		}
		return got
	}

	got := step(specURCB, 257, 0)
	for i, sq := range got {
		if want := uint32(i % 256); sq != want {
			t.Fatalf("URCB report %d SqNum = %d, want %d", i, sq, want)
		}
	}
	// Re-enable: the FALSE→TRUE transition sets SqNum to 0 again.
	if got := step(specURCB, 1, 0); got[0] != 0 {
		t.Errorf("URCB after re-enable SqNum = %d, want 0", got[0])
	}
	if got := step(specBRCB, 3, 65534); got[0] != 65534 || got[1] != 65535 || got[2] != 0 {
		t.Errorf("BRCB SqNum = %v, want [65534 65535 0]", got)
	}
}

// TestSpec_8_1_Table61_SqNumTypes: SqNum is INT16U on a BRCB and INT8U on
// a URCB (7-2 Table 37, §17.2.4.1).
func TestSpec_8_1_Table61_SqNumTypes(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	for rcb, bits := range map[string]int{specBRCB: 16, specURCB: 8} {
		attrs, err := c.mmsClient.GetVariableAccessAttributes(ctx, mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: mms.ItemID(rcb + "$SqNum")})
		if err != nil {
			t.Fatal(err)
		}
		if attrs.TypeSpec.Type != mms.ValueTypeUnsigned || attrs.TypeSpec.Size != bits {
			t.Errorf("%s SqNum type = %v/%d, want unsigned/%d", rcb, attrs.TypeSpec.Type, attrs.TypeSpec.Size, bits)
		}
	}
}

// TestSpec_7_2_17_2_3_2_2_8_BufOvflNotSetForTransmittedDiscard: discarding
// an entry already transmitted "shall not" set BufOvfl (Figure 30)
// (RPT-07).
func TestSpec_7_2_17_2_3_2_2_8_BufOvflNotSetForTransmittedDiscard(t *testing.T) {
	srv, engine := newSpecServer(t, ServerOptions{})
	engine.SetRCBBufMax("LD1", specBRCB, 2)
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	for i := 1; i <= 5; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
		if r := recvReport(t, sub); r.BufOvfl {
			t.Errorf("report %d BufOvfl = true", i)
		}
	}
}

// TestSpec_7_2_17_2_3_2_2_8_BufOvflOnceAfterLoss: entries discarded while
// disabled are a possible loss: BufOvfl=TRUE "in the first report that is
// sent after the transition from disabled to enabled. Subsequent reports
// shall have BufOvfl = FALSE" (RPT-07, RPT-08).
func TestSpec_7_2_17_2_3_2_2_8_BufOvflOnceAfterLoss(t *testing.T) {
	srv, engine := newSpecServer(t, ServerOptions{})
	engine.SetRCBBufMax("LD1", specBRCB, 2)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
	}
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	for i, want := range []struct {
		id   uint64
		ovfl bool
	}{{4, true}, {5, false}} {
		r := recvReport(t, sub)
		if got := decodeEntryID(r.EntryID); got != want.id || r.BufOvfl != want.ovfl {
			t.Errorf("#%d EntryID=%d BufOvfl=%v, want %d/%v", i, got, r.BufOvfl, want.id, want.ovfl)
		}
	}
	expectNoReport(t, sub, 300*time.Millisecond)
}

// TestSpec_7_2_17_2_3_2_3_6_BuffersWhileDisabled: "The BRCB shall buffer
// entries ... during all states" and buffering starts at power-up
// (Figure 24) (RPT-08).
func TestSpec_7_2_17_2_3_2_3_6_BuffersWhileDisabled(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
	}
	c := dialLoopback(t, ctx, srv)
	if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldEntryID, EntryID: make([]byte, 8)}); err != nil {
		t.Fatal(err)
	}
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	for i := 1; i <= 3; i++ {
		r := recvReport(t, sub)
		if got := decodeEntryID(r.EntryID); got != uint64(i) || r.SeqNum != uint32(i-1) || r.BufOvfl {
			t.Errorf("#%d EntryID=%d SqNum=%d BufOvfl=%v", i, got, r.SeqNum, r.BufOvfl)
		}
	}
}

// TestSpec_7_2_17_2_2_16_ReplayPreservesTimeOfEntry: "TimeOfEntry shall
// specify the time when the EntryID was created" (§17.2.3.2.2.9) (RPT-09).
func TestSpec_7_2_17_2_2_16_ReplayPreservesTimeOfEntry(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	before := time.Now().Truncate(time.Millisecond)
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	after := time.Now()
	time.Sleep(1100 * time.Millisecond)
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	r := recvReport(t, sub)
	if r.Timestamp.Before(before) || r.Timestamp.After(after) {
		t.Errorf("TimeOfEntry = %v, want within [%v, %v]", r.Timestamp, before, after)
	}
}

// TestSpec_7_2_17_2_3_2_3_6_ReplayThenLiveSqNumContiguous: buffered
// entries go first and "the BRCB shall use the sequence ... numbers so
// that no gaps occur" (RPT-09, RPT-10).
func TestSpec_7_2_17_2_3_2_3_6_ReplayThenLiveSqNumContiguous(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
	}
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	srv.SetValue(ctx, specModKey, mms.NewInteger(4))
	for i := 0; i < 4; i++ {
		r := recvReport(t, sub)
		if r.SeqNum != uint32(i) || decodeEntryID(r.EntryID) != uint64(i+1) {
			t.Errorf("#%d SqNum=%d EntryID=%d", i, r.SeqNum, decodeEntryID(r.EntryID))
		}
	}
}

// TestSpec_7_2_17_2_3_2_3_5_URCBBurstOrder: "shall send all reports in
// the time sequence order in which the related entries have been
// created" (RPT-10).
func TestSpec_7_2_17_2_3_2_3_5_URCBBurstOrder(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt_urcb01", specURCB, true)
	const n = 300
	for i := range n {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i+1)))
	}
	for i := range n {
		r := recvReport(t, sub)
		if r.SeqNum != uint32(i%256) {
			t.Fatalf("report %d SqNum = %d, want %d", i, r.SeqNum, i%256)
		}
		if v, _ := r.Values[0].Int64(); v != int64(i+1) {
			t.Fatalf("report %d value = %d, want %d", i, v, i+1)
		}
	}
}

// TestSpec_7_2_17_2_5_2_URCBNoEntryIDOrBufOvfl: "regardless of the value
// of OptFlds.EntryID, no EntryID shall be included in the report" and
// "The value returned for OptFlds.buffer-overflow and OptFlds.entryID
// shall always be false" (8-1 Table 63) (RPT-11).
func TestSpec_7_2_17_2_5_2_URCBNoEntryIDOrBufOvfl(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	all := OptFldSeqNum | OptFldTimeStamp | OptFldReasonCode | OptFldDataSet | OptFldDataRef |
		OptFldBufOvfl | OptFldEntryID | OptFldConfRev | OptFldSegmentation
	if err := c.SetReportControlBlock(ctx, "LD1", specURCB, RCBUpdate{Fields: RCBFieldOptFlds, OptFlds: all}); err != nil {
		t.Fatal(err)
	}
	rcb, err := c.GetReportControlBlock(ctx, "LD1", specURCB)
	if err != nil {
		t.Fatal(err)
	}
	if rcb.OptFlds.Has(OptFldBufOvfl) || rcb.OptFlds.Has(OptFldEntryID) || rcb.OptFlds.Has(OptFldSegmentation) {
		t.Errorf("URCB OptFlds read back %v", rcb.OptFlds)
	}
	sub := subscribeRCB(t, c, "rpt_urcb01", specURCB, true)
	srv.SetValue(ctx, specModKey, mms.NewInteger(77))
	r := recvReport(t, sub)
	if r.OptFlds.Has(OptFldBufOvfl) || r.OptFlds.Has(OptFldEntryID) || r.EntryID != nil {
		t.Errorf("URCB report OptFlds=%v EntryID=%x", r.OptFlds, r.EntryID)
	}
	if v, _ := r.Values[0].Int64(); v != 77 || r.ConfRev != 1 {
		t.Errorf("URCB report value=%d ConfRev=%d", v, r.ConfRev)
	}
}

// TestSpec_7_2_17_2_2_5_EntryIDResync: an EntryID not in the queue is
// refused (parameter-value-inappropriate → object-value-invalid), a
// wrong size is type-inconsistent (8-1 §8.1.3.5), while disabled EntryID
// reads the newest entry (§17.2.2.15), and a known EntryID resynchronises
// after it (RPT-12).
func TestSpec_7_2_17_2_2_5_EntryIDResync(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
	}
	c := dialLoopback(t, ctx, srv)
	if bs, _ := rawRead(t, c, specBRCB+"$EntryID").OctetString(); decodeEntryID(bs) != 3 {
		t.Errorf("disabled EntryID = %x, want newest (3)", bs)
	}
	if code := dataAccessCode(rawWrite(c, specBRCB+"$EntryID", mms.NewOctetString(encodeEntryID(999)))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("unknown EntryID: code %v, want object-value-invalid", code)
	}
	if code := dataAccessCode(rawWrite(c, specBRCB+"$EntryID", mms.NewOctetString([]byte{1, 2, 3}))); code != mms.DataAccessErrorTypeInconsistent {
		t.Errorf("3-octet EntryID: code %v, want type-inconsistent", code)
	}
	if err := rawWrite(c, specBRCB+"$EntryID", mms.NewOctetString(encodeEntryID(1))); err != nil {
		t.Fatalf("known EntryID: %v", err)
	}
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	for _, want := range []uint64{2, 3} {
		if r := recvReport(t, sub); decodeEntryID(r.EntryID) != want || r.BufOvfl {
			t.Errorf("EntryID = %d BufOvfl=%v, want %d/false", decodeEntryID(r.EntryID), r.BufOvfl, want)
		}
	}
}

// TestSpec_7_2_17_2_2_6_DatSet: a non-existent DatSet fails and the value
// stays unchanged; a change increments ConfRev (§17.2.2.7) and purges the
// buffer (8-1 §17.1.2); PurgeBuf reads FALSE after a purge (§17.2.2.14)
// (RPT-13).
func TestSpec_7_2_17_2_2_6_DatSet(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		srv.SetValue(ctx, specModKey, mms.NewInteger(int64(i)))
	}
	c := dialLoopback(t, ctx, srv)
	if code := dataAccessCode(rawWrite(c, specBRCB+"$DatSet", mms.NewVisibleString("LD1/LLN0$nope"))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("unknown DatSet: code %v, want object-value-invalid", code)
	}
	if s, _ := rawRead(t, c, specBRCB+"$DatSet").VisibleString(); s != "LD1/LLN0$dsTest" {
		t.Errorf("DatSet after refused write = %q", s)
	}
	if err := rawWrite(c, specBRCB+"$DatSet", mms.NewVisibleString("LD1/LLN0$dsTwo")); err != nil {
		t.Fatal(err)
	}
	if n, _ := rawRead(t, c, specBRCB+"$ConfRev").Uint32(); n != 2 {
		t.Errorf("ConfRev = %d, want 2", n)
	}
	if err := rawWrite(c, specBRCB+"$PurgeBuf", mms.NewBoolean(true)); err != nil {
		t.Fatal(err)
	}
	if b, _ := rawRead(t, c, specBRCB+"$PurgeBuf").Bool(); b {
		t.Error("PurgeBuf reads TRUE after the purge")
	}
	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	expectNoReport(t, sub, 400*time.Millisecond)
	srv.SetValue(ctx, specBehKey, mms.NewInteger(8))
	r := recvReport(t, sub)
	if len(r.Inclusion) != 2 || !r.Inclusion[1] || r.ConfRev != 2 {
		t.Errorf("report inclusion=%v ConfRev=%d", r.Inclusion, r.ConfRev)
	}
	if decodeEntryID(r.EntryID) != 4 {
		t.Errorf("EntryID = %d, want 4 (IDs continue after a purge)", decodeEntryID(r.EntryID))
	}
}

// TestSpec_7_2_17_2_2_13_GI: TrgOps.GI=FALSE → positive response, no
// report, GI reads FALSE; a GI is not replayed after re-enable (it is not
// one of the buffered trigger options, §17.2.3.2.3.6) (RPT-15).
func TestSpec_7_2_17_2_2_13_GI(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	sub := subscribeRCB(t, c, "rpt_brcb01", specBRCB, true)
	if err := c.TriggerGI(ctx, "LD1", specBRCB); err != nil {
		t.Fatal(err)
	}
	if r := recvReport(t, sub); r.ReasonCodes[0] != ReasonGI {
		t.Fatalf("GI report reason = %v", r.ReasonCodes)
	}
	srv.SetValue(ctx, specModKey, mms.NewInteger(5))
	recvReport(t, sub)
	if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldRptEna}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldRptEna, RptEna: true}); err != nil {
		t.Fatal(err)
	}
	r := recvReport(t, sub)
	if r.ReasonCodes[0] == ReasonGI || r.SeqNum != 0 {
		t.Errorf("replay after re-enable: reasons=%v SqNum=%d, want the dchg entry with SqNum 0", r.ReasonCodes, r.SeqNum)
	}
	expectNoReport(t, sub, 300*time.Millisecond)

	// TrgOps without GI.
	if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{Fields: RCBFieldRptEna}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReportControlBlock(ctx, "LD1", specBRCB, RCBUpdate{
		Fields: RCBFieldRptEna | RCBFieldTrgOps, TrgOps: TrgOpDataChanged, RptEna: true,
	}); err != nil {
		t.Fatal(err)
	}
	// The TrgOps change purged the buffer (8-1 §17.1.2), so nothing is
	// replayed.
	if err := c.TriggerGI(ctx, "LD1", specBRCB); err != nil {
		t.Fatalf("GI with TrgOps.GI=FALSE: %v", err)
	}
	if gi, _ := rawRead(t, c, specBRCB+"$GI").Bool(); gi {
		t.Error("GI reads TRUE")
	}
	expectNoReport(t, sub, 400*time.Millisecond)
}

// TestSpec_7_2_17_2_2_9_BufTmAggregates: notifications within BufTm are
// combined into one report; a second notification of the same member
// sends the pending report first (RPT-14).
func TestSpec_7_2_17_2_2_9_BufTmAggregates(t *testing.T) {
	srv, _ := newSpecServer(t, ServerOptions{})
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	if err := c.SetReportControlBlock(ctx, "LD1", specURCB, RCBUpdate{
		Fields: RCBFieldDatSet | RCBFieldBufTm, DatSet: "LD1/LLN0$dsTwo", BufTm: 300,
	}); err != nil {
		t.Fatal(err)
	}
	sub := subscribeRCB(t, c, "rpt_urcb01", specURCB, true)
	srv.SetValue(ctx, specModKey, mms.NewInteger(1))
	time.Sleep(20 * time.Millisecond)
	srv.SetValue(ctx, specBehKey, mms.NewInteger(2))
	r := recvReport(t, sub)
	if len(r.Values) != 2 || !r.Inclusion[0] || !r.Inclusion[1] {
		t.Fatalf("aggregated report inclusion = %v values = %d", r.Inclusion, len(r.Values))
	}
	expectNoReport(t, sub, 500*time.Millisecond)

	srv.SetValue(ctx, specModKey, mms.NewInteger(3))
	srv.SetValue(ctx, specModKey, mms.NewInteger(4))
	for _, want := range []int64{3, 4} {
		r := recvReport(t, sub)
		if v, _ := r.Values[0].Int64(); v != want || len(r.Values) != 1 {
			t.Errorf("report values = %v, want [%d]", r.Values, want)
		}
	}
}

// TestSpec_7_2_17_2_3_2_3_3_IntegrityBufferedWhileDisabled: integrity
// entries are buffered in all states (§17.2.3.2.3.6) (RPT-08).
func TestSpec_7_2_17_2_3_2_3_3_IntegrityBufferedWhileDisabled(t *testing.T) {
	m := specModel()
	rpt := &m.LogicalDevices[0].LogicalNodes[0].Reports[0]
	rpt.IntgPd = 30
	rpt.TrgOps.Period = true
	srv, err := NewServer(m, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	engine := srv.EnableReports()
	defer srv.Close()
	rt := engine.rcbs["LD1/"+specBRCB]
	waitUntil(t, "integrity entry", func() bool {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return len(rt.bufQueue) > 0 && rt.bufQueue[0].reasons[0] == ReasonIntegrity
	})
}

// segmentationModel has a BRCB over n members with data references, so a
// GI report exceeds a small PDU.
func segmentationModel(n int) *servermodel.Model {
	var dos []servermodel.DataObject
	var members []servermodel.DataSetMemberDef
	for i := range n {
		name := fmt.Sprintf("Ind%d", i+1)
		dos = append(dos, servermodel.DataObject{Name: name, Attributes: []servermodel.DataAttribute{
			{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
		}})
		members = append(members, servermodel.DataSetMemberDef{LNName: "GGIO1", DOPath: name + ".stVal", FC: "ST"})
	}
	return &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []servermodel.LogicalNode{
			{Name: "LLN0", LNClass: "LLN0",
				DataSets: []servermodel.DataSetDef{{Name: "dsBig", Members: members}},
				Reports: []servermodel.ReportDef{{Name: "brcb01", RptID: "big", DatSet: "dsBig", ConfRev: 1, Buffered: true,
					TrgOps:  servermodel.TrgOpsDef{Dchg: true, GI: true},
					OptFlds: servermodel.OptFieldsDef{SeqNum: true, DataRef: true, ReasonCode: true, EntryID: true}}},
			},
			{Name: "GGIO1", LNClass: "GGIO", DataObjects: dos},
		},
	}}}
}

// TestSpec_7_2_17_2_3_2_2_5_SegmentsLargeGI: a report that does not fit
// the PDU is divided into subreports with the same SqNum, SubSqNum
// 0..k-1, MoreSegmentsFollow on all but the last and a full-width
// inclusion bitstring in each (8-1 §17.2.1) (RPT-17).
func TestSpec_7_2_17_2_3_2_2_5_SegmentsLargeGI(t *testing.T) {
	const n = 150
	p := reportParams{rptID: "big", optFlds: OptFldSeqNum | OptFldDataRef | OptFldReasonCode, seqNum: 7}
	for i := range n {
		p.inclusion = append(p.inclusion, true)
		p.values = append(p.values, mms.NewBoolean(i%2 == 0))
		p.reasons = append(p.reasons, ReasonGI)
		p.dataRefs = append(p.dataRefs, fmt.Sprintf("LD1/GGIO1$ST$Ind%d$stVal", i+1))
	}
	segs := formatReport(p, 1000)
	if len(segs) < 2 {
		t.Fatalf("segments = %d, want > 1", len(segs))
	}
	covered := make([]bool, n)
	for k, s := range segs {
		if l := valuesLen(s); l > 1000-reportPDUOverhead {
			t.Errorf("segment %d size %d exceeds the PDU budget", k, l)
		}
		if o := decodeOptFlds(s[1]); !o.Has(OptFldSegmentation) {
			t.Errorf("segment %d OptFlds %v without segmentation", k, o)
		}
		if sq, _ := s[2].Uint32(); sq != 7 {
			t.Errorf("segment %d SqNum = %d", k, sq)
		}
		if sub, _ := s[3].Uint32(); sub != uint32(k) {
			t.Errorf("segment %d SubSeqNum = %d", k, sub)
		}
		if more, _ := s[4].Bool(); more != (k < len(segs)-1) {
			t.Errorf("segment %d MoreSegmentsFollow = %v", k, more)
		}
		bits, _ := s[5].BitStringLength()
		if bits != n {
			t.Errorf("segment %d inclusion width = %d, want %d", k, bits, n)
		}
		raw, _ := s[5].BitString()
		for i := range n {
			if raw[i/8]&(0x80>>(i%8)) != 0 {
				if covered[i] {
					t.Errorf("member %d in two segments", i)
				}
				covered[i] = true
			}
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Errorf("member %d in no segment", i)
		}
	}
	// A report that fits is not segmented.
	small := p
	small.inclusion = make([]bool, n)
	small.inclusion[0] = true
	if segs := formatReport(small, 1000); len(segs) != 1 || decodeOptFlds(segs[0][1]).Has(OptFldSegmentation) {
		t.Errorf("small report: %d segments", len(segs))
	}
	// A member larger than the PDU cannot be sent.
	if segs := formatReport(p, 40); segs != nil {
		t.Errorf("tiny PDU: %d segments, want none", len(segs))
	}

	// End to end: the client reassembles the GI from the segments.
	srv, err := NewServer(segmentationModel(n), ServerOptions{MMS: mms.ServerOptions{MMS: mms.ServerMMSOptions{MaxPDUSize: 2000}}})
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableReports()
	defer srv.Close()
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "big", specBRCB, true)
	if err := c.TriggerGI(ctx, "LD1", specBRCB); err != nil {
		t.Fatal(err)
	}
	r := recvReport(t, sub)
	if len(r.Inclusion) != n || len(r.Values) != n || len(r.DataReferences) != n {
		t.Fatalf("assembled GI: inclusion=%d values=%d refs=%d, want %d", len(r.Inclusion), len(r.Values), len(r.DataReferences), n)
	}
	if r.DataReferences[n-1] != fmt.Sprintf("LD1/GGIO1$ST$Ind%d$stVal", n) {
		t.Errorf("last data reference = %q", r.DataReferences[n-1])
	}
}

// dropAssociation closes the MMS association under c (a Client made with
// NewClient does not own it).
func dropAssociation(c *Client) {
	_ = c.Close(context.Background())
	_ = c.mmsClient.Close(context.Background())
}
