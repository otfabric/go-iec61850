// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"slices"
	"testing"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// dataRefModel is testReportSCL with the data-reference optional field set
// on both report control blocks.
func dataRefModel() *servermodel.Model {
	m := testReportSCL()
	for i := range m.LogicalDevices[0].LogicalNodes[0].Reports {
		r := &m.LogicalDevices[0].LogicalNodes[0].Reports[i]
		r.OptFlds.DataRef = true
		r.OptFlds.DataSet = true
	}
	return m
}

// A report whose OptFlds has data-reference carries one data reference per
// included member, between the inclusion bitstring and the values
// (IEC 61850-8-1 report format). Before the fix the server set the bit and
// left the references out, which an independent client (cB-Sigma/iec61850)
// refused as malformed.
func TestReport_DataReferencesWhenOptFldsAsks(t *testing.T) {
	srv, err := NewServer(dataRefModel(), ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	engine := srv.EnableReports()
	defer engine.Stop()

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub, err := c.SubscribeReport(ctx, "rpt_urcb01", SubscribeReportOptions{
		LD: "LD1", RCBItemID: "LLN0$RP$urcb01", AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	srv.SetValue(ctx, servermodel.StoreKey("LD1", "LLN0$ST$Mod$stVal"), mms.NewInteger(7))
	r := recvReport(t, sub)
	if !r.OptFlds.Has(OptFldDataRef) {
		t.Fatalf("OptFlds %v has no data-reference", r.OptFlds)
	}
	if want := []string{"LD1/LLN0$ST$Mod$stVal"}; !slices.Equal(r.DataReferences, want) {
		t.Errorf("DataReferences = %q, want %q", r.DataReferences, want)
	}
	if len(r.Values) != 1 {
		t.Fatalf("values = %d, want 1", len(r.Values))
	}
	if v, err := r.Values[0].Int64(); err != nil || v != 7 {
		t.Errorf("value = %d (%v), want 7", v, err)
	}
}

// A buffered entry replayed to the enabling client carries the data
// references as well.
func TestBRCB_ReplayCarriesDataReferences(t *testing.T) {
	srv, err := NewServer(dataRefModel(), ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	engine := srv.EnableReports()
	defer engine.Stop()

	bufferBRCBEntries(t, srv, engine, 1)

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	sub, err := c.SubscribeReport(ctx, "rpt_brcb01", SubscribeReportOptions{
		LD: "LD1", RCBItemID: "LLN0$BR$brcb01", AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	r := recvReport(t, sub)
	if want := []string{"LD1/LLN0$ST$Mod$stVal"}; !slices.Equal(r.DataReferences, want) {
		t.Errorf("DataReferences = %q, want %q", r.DataReferences, want)
	}
}

// The encoder puts the references of the included members only, in data
// set order, after the inclusion bitstring and before the values.
func TestEncodeReportValues_DataReferenceOrder(t *testing.T) {
	vals := encodeReportValuesEx(reportParams{
		rptID:     "r",
		optFlds:   OptFldDataRef | OptFldReasonCode,
		inclusion: []bool{true, false, true},
		dataRefs:  []string{"LD/A$ST$x", "LD/B$ST$y", "LD/C$ST$z"},
		reasons:   []ReasonCode{ReasonDataChanged, 0, ReasonGI},
		values:    []*mms.Value{mms.NewInteger(1), mms.NewInteger(2), mms.NewInteger(3)},
	})
	// RptID, OptFlds, Inclusion, 2 references, 2 values, 2 reasons.
	if len(vals) != 9 {
		t.Fatalf("got %d values, want 9", len(vals))
	}
	for i, want := range []string{"LD/A$ST$x", "LD/C$ST$z"} {
		if got, ok := vals[3+i].VisibleString(); !ok || got != want {
			t.Errorf("reference %d = %q (%v), want %q", i, got, ok, want)
		}
	}
	for i, want := range []int64{1, 3} {
		if got, ok := vals[5+i].Int64(); !ok || got != want {
			t.Errorf("value %d = %d (%v), want %d", i, got, ok, want)
		}
	}
}
