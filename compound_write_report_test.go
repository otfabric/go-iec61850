// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// A client write of a constructed FC=SP attribute that is a data set
// member reports the new value: the write sets the leaves and the report
// engine is told of each (IEC 61850-7-2 §17.2.3.2.3.2, dchg). Before
// v1.18.0 the value stayed under the compound key and nothing reported.
// Each leaf is stored and notified in turn, as by Server.SetValue, so a
// leaf without a trigger option does not hide the change of the next.
func TestServer_CompoundWriteReports(t *testing.T) {
	m := &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
		Name: "LD1",
		LogicalNodes: []servermodel.LogicalNode{{
			Name: "LLN0", LNClass: "LLN0",
			DataObjects: []servermodel.DataObject{{Name: "Set", Attributes: []servermodel.DataAttribute{
				{Name: "setMag", FC: "SP", Children: []servermodel.DataAttribute{
					// f has no trigger option: its change alone is no
					// notification, and must not hide the change of i.
					{Name: "f", FC: "SP", BType: "FLOAT32"},
					{Name: "i", FC: "SP", BType: "INT32", Dchg: true},
				}},
			}}},
			DataSets: []servermodel.DataSetDef{{Name: "ds", Members: []servermodel.DataSetMemberDef{
				{LNName: "LLN0", DOPath: "Set.setMag", FC: "SP"},
			}}},
			Reports: []servermodel.ReportDef{{Name: "urcb01", RptID: "rpt", DatSet: "ds", ConfRev: 1,
				TrgOps: servermodel.TrgOpsDef{Dchg: true}}},
		}},
	}}}
	srv, err := NewServer(m, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableReports()
	t.Cleanup(srv.Close)
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub := subscribeRCB(t, c, "rpt", "LLN0$RP$urcb01", true)

	want := mms.NewStructure([]*mms.Value{mms.NewFloat(1.5), mms.NewInteger(42)})
	if err := rawWrite(c, "LLN0$SP$Set$setMag", want); err != nil {
		t.Fatal(err)
	}
	r := recvReport(t, sub)
	if len(r.Values) != 1 || !r.Values[0].MMS().Equal(want) {
		t.Errorf("report values = %v, want [%v]", r.Values, want)
	}
	if i, _ := rawRead(t, c, "LLN0$SP$Set$setMag$i").Int64(); i != 42 {
		t.Errorf("setMag.i = %d, want 42", i)
	}
}
