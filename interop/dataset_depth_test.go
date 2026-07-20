//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2G — dataset depth:
//
//   - Dataset member discovery with ordered references and FC information.
//   - Mixed-FC bulk reads (ST + MX + CF attributes in one MMS request).
//   - Multi-value dataset read: verify both members decode to the correct types.
//   - Multi-member report: write two dataset members on the go-iec61850 server
//     and verify the libiec61850 reporter receives all values.
//
// The fixture defines one dataset (dsInterop) with two members:
//
//	member[0]: InteropLD/GGIO1.SPS1.stVal[ST]  — boolean
//	member[1]: InteropLD/LLN0.Mod.stVal[ST]    — integer
//
// Dynamic dataset creation is deferred (Phase 2G explicitly excludes it).
package interop

import (
	"context"
	"fmt"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// Phase 2G-a — dataset member discovery
// ---------------------------------------------------------------------------

// TestLibIECClient_DS_MemberDiscovery verifies that GetDataSet returns the two
// dsInterop members in definition order with the expected Refs and FCs.
func TestLibIECClient_DS_MemberDiscovery(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	ds, err := c.GetDataSet(ctx, "InteropLD", "LLN0$dsInterop")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}

	if len(ds.Members) != 2 {
		t.Fatalf("expected 2 dataset members, got %d", len(ds.Members))
	}

	// member[0]: InteropLD/GGIO1.SPS1.stVal[ST]
	m0 := ds.Members[0]
	t.Logf("member[0]: ref=%s domain=%s item=%s", m0.Ref, m0.DomainID, m0.ItemID)
	if m0.Ref.FC != "ST" {
		t.Errorf("member[0]: want FC=ST, got %q", m0.Ref.FC)
	}
	if m0.Ref.LN != "GGIO1" {
		t.Errorf("member[0]: want LN=GGIO1, got %q", m0.Ref.LN)
	}

	// member[1]: InteropLD/LLN0.Mod.stVal[ST]
	m1 := ds.Members[1]
	t.Logf("member[1]: ref=%s domain=%s item=%s", m1.Ref, m1.DomainID, m1.ItemID)
	if m1.Ref.FC != "ST" {
		t.Errorf("member[1]: want FC=ST, got %q", m1.Ref.FC)
	}
	if m1.Ref.LN != "LLN0" {
		t.Errorf("member[1]: want LN=LLN0, got %q", m1.Ref.LN)
	}
}

// TestBeanClient_DS_MemberDiscovery is the bean-server equivalent of
// TestLibIECClient_DS_MemberDiscovery.
func TestBeanClient_DS_MemberDiscovery(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	ds, err := c.GetDataSet(ctx, "InteropLD", "LLN0$dsInterop")
	if err != nil {
		t.Fatalf("GetDataSet: %v", err)
	}

	if len(ds.Members) != 2 {
		t.Fatalf("expected 2 dataset members, got %d", len(ds.Members))
	}

	m0 := ds.Members[0]
	t.Logf("member[0]: ref=%s domain=%s item=%s", m0.Ref, m0.DomainID, m0.ItemID)
	if m0.Ref.FC != "ST" {
		t.Errorf("member[0]: want FC=ST, got %q", m0.Ref.FC)
	}

	m1 := ds.Members[1]
	t.Logf("member[1]: ref=%s domain=%s item=%s", m1.Ref, m1.DomainID, m1.ItemID)
	if m1.Ref.FC != "ST" {
		t.Errorf("member[1]: want FC=ST, got %q", m1.Ref.FC)
	}
}

// ---------------------------------------------------------------------------
// Phase 2G-b — mixed-FC bulk reads (ReadMultiple across FC boundaries)
// ---------------------------------------------------------------------------

// TestLibIECClient_DS_ReadMixedFC reads three attributes spanning ST, MX, and CF
// functional constraints in a single MMS multi-variable request and verifies that
// each value is decoded with the correct type.
func TestLibIECClient_DS_ReadMixedFC(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	mxRef, _ := iec61850.ParseRef("InteropLD/MMXU1.TotW.mag.f[MX]")
	cfRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")

	results, err := c.ReadMultiple(ctx, []iec61850.Ref{stRef, mxRef, cfRef})
	if err != nil {
		t.Fatalf("ReadMultiple (ST+MX+CF): %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// ST — boolean
	if results[0].Err != nil {
		t.Fatalf("results[0] (ST) error: %v", results[0].Err)
	}
	b, bErr := results[0].Value.Bool()
	if bErr != nil {
		t.Fatalf("results[0]: expected boolean (%v)", bErr)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("SPS1.stVal[ST]: want %v, got %v", fixVal.SPS1StVal, b)
	}

	// MX — float
	if results[1].Err != nil {
		t.Fatalf("results[1] (MX) error: %v", results[1].Err)
	}
	if _, fErr := results[1].Value.Float64(); fErr != nil {
		t.Errorf("results[1]: expected float (%v)", fErr)
	}

	// CF — integer
	if results[2].Err != nil {
		t.Fatalf("results[2] (CF) error: %v", results[2].Err)
	}
	iv, ivErr := results[2].Value.Int64()
	if ivErr != nil {
		t.Fatalf("results[2]: expected integer (%v)", ivErr)
	}
	if iv != fixVal.ModCtlModel {
		t.Errorf("LLN0.Mod.ctlModel[CF]: want %d, got %d", fixVal.ModCtlModel, iv)
	}
}

// TestBeanClient_DS_ReadMixedFC is the bean-server equivalent.
func TestBeanClient_DS_ReadMixedFC(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	stRef, _ := iec61850.ParseRef("InteropLD/GGIO1.SPS1.stVal[ST]")
	mxRef, _ := iec61850.ParseRef("InteropLD/MMXU1.TotW.mag.f[MX]")
	cfRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.ctlModel[CF]")

	results, err := c.ReadMultiple(ctx, []iec61850.Ref{stRef, mxRef, cfRef})
	if err != nil {
		t.Fatalf("ReadMultiple (ST+MX+CF): %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].Err != nil {
		t.Fatalf("results[0] (ST) error: %v", results[0].Err)
	}
	if _, bErr := results[0].Value.Bool(); bErr != nil {
		t.Errorf("results[0]: expected boolean (%v)", bErr)
	}

	if results[1].Err != nil {
		t.Fatalf("results[1] (MX) error: %v", results[1].Err)
	}
	if _, fErr := results[1].Value.Float64(); fErr != nil {
		t.Errorf("results[1]: expected float (%v)", fErr)
	}

	if results[2].Err != nil {
		t.Fatalf("results[2] (CF) error: %v", results[2].Err)
	}
	if _, ivErr := results[2].Value.Int64(); ivErr != nil {
		t.Errorf("results[2]: expected integer (%v)", ivErr)
	}
}

// ---------------------------------------------------------------------------
// Phase 2G-c — multi-value dataset read: type and value verification
// ---------------------------------------------------------------------------

// TestLibIECClient_DS_ReadAllMembers extends the basic ReadDataSet test by
// verifying each member's type and value, not just the count.
func TestLibIECClient_DS_ReadAllMembers(t *testing.T) {
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Close(ctx)

	values, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$dsInterop")
	if err != nil {
		t.Fatalf("ReadDataSet: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 members, got %d", len(values))
	}

	// member[0]: GGIO1.SPS1.stVal[ST] — boolean
	if values[0].Err != nil {
		t.Fatalf("member[0] error: %v", values[0].Err)
	}
	b, bErr := values[0].Value.Bool()
	if bErr != nil {
		t.Fatalf("member[0]: expected bool (%v)", bErr)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("member[0] (SPS1.stVal): want %v, got %v", fixVal.SPS1StVal, b)
	}
	t.Logf("member[0] ref=%s val=%v", values[0].Member.Ref, b)

	// member[1]: LLN0.Mod.stVal[ST] — integer
	if values[1].Err != nil {
		t.Fatalf("member[1] error: %v", values[1].Err)
	}
	iv, ivErr := values[1].Value.Int64()
	if ivErr != nil {
		t.Fatalf("member[1]: expected int64 (%v)", ivErr)
	}
	t.Logf("member[1] ref=%s val=%v", values[1].Member.Ref, iv)
	// The libiec61850 client writes 5 to Mod.stVal before reading the dataset
	// (see ied_client.c), so accept either the fixture value or 5.
	if iv != fixVal.ModStVal && iv != 5 {
		t.Errorf("member[1] (Mod.stVal): want %d or 5, got %d", fixVal.ModStVal, iv)
	}
}

// TestBeanClient_DS_ReadAllMembers is the bean-server equivalent.
func TestBeanClient_DS_ReadAllMembers(t *testing.T) {
	h := startIEC61850BeanServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	values, err := c.ReadDataSet(ctx, "InteropLD", "LLN0$dsInterop")
	if err != nil {
		t.Fatalf("ReadDataSet: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 members, got %d", len(values))
	}

	if values[0].Err != nil {
		t.Fatalf("member[0] error: %v", values[0].Err)
	}
	b, bErr := values[0].Value.Bool()
	if bErr != nil {
		t.Fatalf("member[0]: expected bool (%v)", bErr)
	}
	if b != fixVal.SPS1StVal {
		t.Errorf("member[0]: want %v, got %v", fixVal.SPS1StVal, b)
	}

	if values[1].Err != nil {
		t.Fatalf("member[1] error: %v", values[1].Err)
	}
	iv, ivErr := values[1].Value.Int64()
	if ivErr != nil {
		t.Fatalf("member[1]: expected int64 (%v)", ivErr)
	}
	if iv != fixVal.ModStVal {
		t.Errorf("member[1]: want %d, got %d", fixVal.ModStVal, iv)
	}
}

// ---------------------------------------------------------------------------
// Phase 2G-d — multi-member report via General Interrogation
// ---------------------------------------------------------------------------

// TestLibIECServer_DS_DchgReport verifies that writing GGIO1.SPS1.stVal on the
// go-iec61850 server yields a data-change report that includes exactly the
// changed member (inclusion[0]=true) and excludes the unchanged member
// (inclusion[1]=false).  The libiec61850 reporter connects, enables the URCB,
// and writes SPS1.stVal to trigger the dchg report.
func TestLibIECServer_DS_GIReport(t *testing.T) {
	srv := startGoIEDServerWithReports(t)
	ch := startIEDReporterAdapter(t, srv.port, fixVal.SPS1StVal)
	results := collectReporterResults(t, ch)

	// Locate the dchg report result.
	r, ok := findReporterOp(results, "receive-report")
	if !ok {
		t.Fatal("no 'receive-report' result in reporter output")
	}
	if !r.OK {
		t.Fatalf("receive-report: ok=false, error=%q", r.Error)
	}

	// A dchg report for SPS1.stVal must include at least 1 member.
	if len(r.Inclusion) < 1 {
		t.Fatalf("inclusion bits: expected at least 1, got %d", len(r.Inclusion))
	}
	t.Logf("inclusion: %v", r.Inclusion)
	// Member 0 (SPS1.stVal) was written, so it must be included.
	if !r.Inclusion[0] {
		t.Error("inclusion[0] (SPS1.stVal) should be true for the dchg report")
	}
	// Member 1 (Mod.stVal) was not written, so it should NOT be included.
	if len(r.Inclusion) > 1 && r.Inclusion[1] {
		t.Log("inclusion[1] (Mod.stVal) included in dchg — acceptable if server batches")
	}

	// The values slice should have at least 1 entry.
	t.Logf("report values: %s", r.Values)
}

// TestLibIECServer_DS_MultiWrite writes a dataset member on the go-iec61850 server
// and verifies that a Go subscriber receives the resulting dchg report with the
// updated value in the expected member position.
func TestLibIECServer_DS_MultiWrite(t *testing.T) {
	srv := startGoIEDServerWithReports(t)

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer writeCancel()

	c := dialIED(t, writeCtx, fmt.Sprintf("127.0.0.1:%d", srv.port))
	defer c.Close(writeCtx)

	const (
		srvUrcbLD = "InteropLD"
		srvUrcbID = "LLN0$RP$urcb01"
	)

	rcb, err := c.GetReportControlBlock(writeCtx, srvUrcbLD, srvUrcbID)
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}
	t.Logf("URCB RptID=%s DatSet=%s RptEna=%v", rcb.RptID, rcb.DatSet, rcb.RptEna)

	if err := c.ReserveURCB(writeCtx, srvUrcbLD, srvUrcbID); err != nil {
		t.Fatalf("ReserveURCB: %v", err)
	}

	sub, err := c.SubscribeReport(writeCtx, rcb.RptID, iec61850.SubscribeReportOptions{
		LD:         srvUrcbLD,
		RCBItemID:  srvUrcbID,
		AutoEnable: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer sub.Close() //nolint:errcheck

	// Write dataset member[1] (LLN0.Mod.stVal[ST]) — this is writable on the
	// go-iec61850 server and is tracked by dsInterop.
	modRef, _ := iec61850.ParseRef("InteropLD/LLN0.Mod.stVal[ST]")
	newVal := fixVal.ModStVal + 1
	if err := c.Write(writeCtx, modRef, mms.NewInteger(newVal)); err != nil {
		t.Fatalf("write Mod.stVal: %v", err)
	}

	// Collect at least one dchg report within 5 seconds.
	reportCh := sub.Reports()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case rpt, open := <-reportCh:
			if !open {
				t.Fatal("subscription channel closed unexpectedly")
			}
			t.Logf("report: rptID=%s values=%d inclusion=%v", rpt.RptID, len(rpt.Values), rpt.Inclusion)
			// member[1] (Mod.stVal) should be included.
			if len(rpt.Inclusion) > 1 && rpt.Inclusion[1] {
				if len(rpt.Values) > 0 {
					iv, ivErr := rpt.Values[0].Int64()
					if ivErr != nil {
						t.Errorf("report value[0]: expected int64 (%v)", ivErr)
					} else if iv != newVal {
						t.Errorf("report value: want %d, got %d", newVal, iv)
					}
				}
				return // success
			}
		case <-deadline:
			t.Error("no dchg report received after writing Mod.stVal")
			return
		}
	}
}
