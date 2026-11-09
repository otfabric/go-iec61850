// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// fcdReportModel has a data set with a data-object member (Mod[ST], an
// FCD) and an attribute member below it (Mod.stVal[ST], an FCDA), and a
// BRCB on that data set.
func fcdReportModel() *servermodel.Model {
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
						{Name: "ctlModel", FC: "CF", BType: "INT32"},
					},
				}},
				DataSets: []servermodel.DataSetDef{{
					Name: "dsFCD",
					Members: []servermodel.DataSetMemberDef{
						{LNName: "LLN0", DOPath: "Mod", FC: "ST"},
						{LNName: "LLN0", DOPath: "Mod.stVal", FC: "ST"},
					},
				}},
				Reports: []servermodel.ReportDef{{
					Name:     "brcbFCD",
					RptID:    "rpt_fcd",
					DatSet:   "dsFCD",
					ConfRev:  1,
					Buffered: true,
					TrgOps:   servermodel.TrgOpsDef{Dchg: true, Qchg: true, GI: true},
					OptFlds:  servermodel.OptFieldsDef{SeqNum: true, ReasonCode: true},
				}},
			}},
		}},
	}
}

func enableFCDReport(t *testing.T) (*Server, *ReportEngine, *rcbRuntime) {
	t.Helper()
	srv := newTestServer(t, fcdReportModel())
	engine := srv.EnableReports()
	t.Cleanup(engine.Stop)
	if err := engine.HandleRCBWrite(context.Background(), "LD1", "LLN0$BR$brcbFCD", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	return srv, engine, engine.rcbs["LD1/LLN0$BR$brcbFCD"]
}

func lastEntry(t *testing.T, rt *rcbRuntime) *bufferedEntry {
	t.Helper()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.bufQueue) == 0 {
		t.Fatal("no buffered report")
	}
	return rt.bufQueue[len(rt.bufQueue)-1]
}

func queueLen(rt *rcbRuntime) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.bufQueue)
}

// A data-object member is reported as the structure of its attributes
// of the member's FC (IEC 61850-8-1, 14.2.2), not as a missing value.
func TestReportEngine_FCDMemberGI(t *testing.T) {
	srv, engine, rt := enableFCDReport(t)
	ctx := context.Background()
	srv.SetValue(ctx, servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal"), mms.NewInteger(3))

	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcbFCD", "GI", mms.NewBoolean(true)); err != nil {
		t.Fatalf("GI: %v", err)
	}
	e := lastEntry(t, rt)
	if !e.inclusion[0] || !e.inclusion[1] || e.reasons[0] != ReasonGI {
		t.Fatalf("GI entry: inclusion %v reasons %v", e.inclusion, e.reasons)
	}
	fields, ok := e.values[0].Structure()
	if !ok || len(fields) != 3 {
		t.Fatalf("Mod[ST] = %v, want a structure of stVal, q, t", e.values[0])
	}
	if v, _ := fields[0].Int64(); v != 3 {
		t.Errorf("Mod[ST].stVal = %v, want 3", fields[0])
	}
	if fields[1].Type() != mms.ValueTypeBitString {
		t.Errorf("Mod[ST].q type = %v, want a bit string", fields[1].Type())
	}
	if v, _ := e.values[1].Int64(); v != 3 {
		t.Errorf("Mod.stVal[ST] = %v, want 3", e.values[1])
	}
}

// A change below a data-object member triggers a report for it, and for
// every other member holding the changed attribute.
func TestReportEngine_FCDMemberDataChange(t *testing.T) {
	srv, _, rt := enableFCDReport(t)
	srv.SetValue(context.Background(), servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal"), mms.NewInteger(5))

	e := lastEntry(t, rt)
	if !e.inclusion[0] || !e.inclusion[1] {
		t.Fatalf("inclusion = %v, want both members", e.inclusion)
	}
	if e.reasons[0]&ReasonDataChanged == 0 || e.reasons[1]&ReasonDataChanged == 0 {
		t.Errorf("reasons = %v, want data change for both", e.reasons)
	}
	fields, _ := e.values[0].Structure()
	if len(fields) != 3 {
		t.Fatalf("Mod[ST] = %v", e.values[0])
	}
	if v, _ := fields[0].Int64(); v != 5 {
		t.Errorf("Mod[ST].stVal = %v, want 5", fields[0])
	}
}

// A quality change concerns only the member that holds the quality.
func TestReportEngine_FCDMemberQualityChange(t *testing.T) {
	srv, _, rt := enableFCDReport(t)
	q := mms.NewBitStringWithLength([]byte{0x40, 0x00}, 13)
	srv.SetValue(context.Background(), servermodel.StoreKey("LD1", "LLN0$ST$Mod$q"), q)

	e := lastEntry(t, rt)
	if !e.inclusion[0] || e.inclusion[1] {
		t.Fatalf("inclusion = %v, want only Mod[ST]", e.inclusion)
	}
	if e.reasons[0]&ReasonQualityChanged == 0 {
		t.Errorf("reason = %v, want quality change", e.reasons[0])
	}
}

// An attribute of another FC is not part of the member and triggers
// nothing; neither does writing an unchanged value.
func TestReportEngine_FCDMemberUnrelated(t *testing.T) {
	srv, _, rt := enableFCDReport(t)
	ctx := context.Background()
	srv.SetValue(ctx, servermodel.StoreKey("LD1", "LLN0$CF$Mod$ctlModel"), mms.NewInteger(1))
	srv.SetValue(ctx, servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal"), mms.NewInteger(0))
	if n := queueLen(rt); n != 0 {
		t.Errorf("%d reports, want none", n)
	}
}

// A member the model cannot resolve keeps the exact-key lookup.
func TestReportEngine_UnresolvedMember(t *testing.T) {
	model := fcdReportModel()
	srv := newTestServerOpts(t, model, false)
	ln := &model.LogicalDevices[0].LogicalNodes[0]
	ln.DataSets[0].Members = append(ln.DataSets[0].Members,
		servermodel.DataSetMemberDef{LNName: "LLN0", DOPath: "Nope", FC: "ST"})
	engine := srv.EnableReports()
	t.Cleanup(engine.Stop)
	ctx := context.Background()
	if err := engine.HandleRCBWrite(ctx, "LD1", "LLN0$BR$brcbFCD", "RptEna", mms.NewBoolean(true)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	rt := engine.rcbs["LD1/LLN0$BR$brcbFCD"]
	if rt.memberNodes[2] != nil {
		t.Fatalf("unresolvable member resolved: %+v", rt.memberNodes[2])
	}
	key := servermodel.StoreKey("LD1", "LLN0$ST$Nope")
	srv.SetValue(ctx, key, mms.NewInteger(9))
	e := lastEntry(t, rt)
	if !e.inclusion[2] {
		t.Fatalf("inclusion = %v, want the unresolved member", e.inclusion)
	}
	if v, _ := e.values[2].Int64(); v != 9 {
		t.Errorf("value = %v, want 9", e.values[2])
	}
}
