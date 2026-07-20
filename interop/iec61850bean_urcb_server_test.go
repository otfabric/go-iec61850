//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover Phase 2D-b:
//
//	iec61850bean IED reporter → go-iec61850 server (URCB reporting)
//
// The iec61850bean-ied-reporter binary mirrors the libiec61850-ied-reporter
// operation sequence (get-rcb → enable-rcb → write → receive-report →
// disable-rcb → conclude) but uses the iec61850bean Java client library.
//
// Assertions match TestLibIECServer_URCB_DataChange exactly so that both
// reporters prove identical behaviour when speaking to a go-iec61850 server.
package interop

import (
	"encoding/json"
	"testing"
)

// TestBeanServer_URCB_DataChange runs iec61850bean-ied-reporter against a
// go-iec61850 server with the report engine enabled and validates the full
// dchg report cycle.
func TestBeanServer_URCB_DataChange(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ch := startIEC61850BeanReporterAdapter(t, h.port, fixVal.SPS1StVal)

	results := collectReporterResults(t, ch)

	// 1. get-rcb must succeed and carry the expected RptID.
	rcb, ok := findReporterOp(results, "get-rcb")
	if !ok {
		t.Error("get-rcb: not found in reporter output")
	} else if !rcb.OK {
		t.Errorf("get-rcb: ok=false, error=%q", rcb.Error)
	} else if rcb.RptID != "interop_urcb01" {
		t.Errorf("get-rcb: rptID want %q, got %q", "interop_urcb01", rcb.RptID)
	}

	// 2. enable-rcb must succeed.
	if en, ok := findReporterOp(results, "enable-rcb"); !ok {
		t.Error("enable-rcb: not found")
	} else if !en.OK {
		t.Errorf("enable-rcb: ok=false, error=%q", en.Error)
	}

	// 3. write must succeed.
	if wr, ok := findReporterOp(results, "write"); !ok {
		t.Error("write: not found")
	} else if !wr.OK {
		t.Errorf("write: ok=false, error=%q", wr.Error)
	}

	// 4. receive-report is the core assertion.
	rpt, ok := findReporterOp(results, "receive-report")
	if !ok {
		t.Fatal("receive-report: not found — reporter timed out waiting for dchg report")
	}
	if !rpt.OK {
		t.Fatalf("receive-report: ok=false, error=%q", rpt.Error)
	}

	if rpt.RptID != "interop_urcb01" {
		t.Errorf("receive-report rptID: want %q, got %q", "interop_urcb01", rpt.RptID)
	}
	if rpt.SeqNum == 0 {
		t.Error("receive-report seqNum: expected non-zero")
	}

	// Inclusion: [true, false] — SPS1.stVal changed, Mod.stVal did not.
	if len(rpt.Inclusion) < 2 {
		t.Fatalf("inclusion length: want >= 2, got %d", len(rpt.Inclusion))
	}
	if !rpt.Inclusion[0] {
		t.Error("inclusion[0] (SPS1.stVal): want true (changed)")
	}
	if rpt.Inclusion[1] {
		t.Error("inclusion[1] (Mod.stVal): want false (not changed)")
	}

	// Values: first included member should be !fixVal.SPS1StVal.
	if len(rpt.Values) > 0 {
		var vals []json.RawMessage
		if err := json.Unmarshal(rpt.Values, &vals); err != nil {
			t.Fatalf("values: unmarshal: %v", err)
		}
		if len(vals) == 0 {
			t.Fatal("values array is empty")
		}
		var b bool
		if err := json.Unmarshal(vals[0], &b); err != nil {
			t.Fatalf("values[0]: expected bool, got %s", vals[0])
		}
		want := !fixVal.SPS1StVal
		if b != want {
			t.Errorf("values[0] (SPS1.stVal): want %v, got %v", want, b)
		}
	} else {
		t.Error("values: missing from receive-report")
	}

	// Reason codes: first included member must be "data-change".
	if len(rpt.Reasons) > 0 {
		if rpt.Reasons[0] != "data-change" {
			t.Errorf("reasons[0]: want %q, got %q", "data-change", rpt.Reasons[0])
		}
	} else {
		t.Error("reasons: missing from receive-report")
	}

	// 5. disable-rcb must succeed.
	if dis, ok := findReporterOp(results, "disable-rcb"); !ok {
		t.Error("disable-rcb: not found")
	} else if !dis.OK {
		t.Errorf("disable-rcb: ok=false, error=%q", dis.Error)
	}

	// 6. conclude must succeed.
	if con, ok := findReporterOp(results, "conclude"); !ok {
		t.Error("conclude: not found")
	} else if !con.OK {
		t.Errorf("conclude: ok=false, error=%q", con.Error)
	}
}
