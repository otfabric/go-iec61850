// SPDX-License-Identifier: MIT

package scl

import (
	"math"
	"testing"

	raw17 "github.com/otfabric/go-iec61850/scl/internal/raw/v17"
	rawb "github.com/otfabric/go-iec61850/scl/internal/raw/v2007b"
	rawb4 "github.com/otfabric/go-iec61850/scl/internal/raw/v2007b4"
	rawc5 "github.com/otfabric/go-iec61850/scl/internal/raw/v2007c5"
)

// TestConvSettingControlBounds: numOfSGs and actSG are INT8U in the SGCB.
// Before the fix numOfSGs="256" became 0 and actSG="257" became 1
// without any diagnostic.
func TestConvSettingControlBounds(t *testing.T) {
	sink := newDiagSink(0, 0)
	sc := convSettingControl(255, 255, math.MaxUint16, sink)
	if len(sink.diags) != 0 || sc.NumOfSGs != 255 || sc.ActSG != 255 || sc.ResvTms != math.MaxUint16 {
		t.Fatalf("max values: %+v %v", sc, sink.diags)
	}
	sc = convSettingControl(256, 257, 5, sink)
	if sc.NumOfSGs != 0 || sc.ActSG != 0 || sc.ResvTms != 5 {
		t.Errorf("out of range values not left zero: %+v", sc)
	}
	if len(sink.diags) != 2 {
		t.Fatalf("diagnostics: %v", sink.diags)
	}
	for i, attr := range []string{"numOfSGs", "actSG"} {
		d := sink.diags[i]
		if d.Severity != DiagError || d.Code != "invalid-setting-control" || d.Path != "LN0.SettingControl@"+attr {
			t.Errorf("diag %d: %+v", i, d)
		}
	}
	sink = newDiagSink(0, 0)
	convSettingControl(math.MaxUint32, 1, 0, sink)
	if len(sink.diags) != 1 {
		t.Errorf("max uint32: %v", sink.diags)
	}
}

// TestConvLN0SettingControlRange runs every schema version's LN0
// converter with an out-of-range numOfSGs.
func TestConvLN0SettingControlRange(t *testing.T) {
	for name, conv := range map[string]func(*diagSink) LN{
		"v17": func(d *diagSink) LN {
			return convLN017(raw17.LN0{LnClass: "LLN0", SettingControl: &raw17.SettingControl{NumOfSGs: 256, ActSG: 1}}, d)
		},
		"2007B": func(d *diagSink) LN {
			return convLN0B(rawb.LN0{LnClass: "LLN0", SettingControl: &rawb.SettingControl{NumOfSGs: 256, ActSG: 1, ResvTms: 9}}, d)
		},
		"2007B4": func(d *diagSink) LN {
			return convLN0B4(rawb4.LN0{LnClass: "LLN0", SettingControl: &rawb4.SettingControl{NumOfSGs: 256, ActSG: 1, ResvTms: 9}}, d)
		},
		"2007C5": func(d *diagSink) LN {
			return convLN02007c5(rawc5.LN0{LnClass: "LLN0", SettingControl: &rawc5.SettingControl{NumOfSGs: 256, ActSG: 1, ResvTms: 9}}, d)
		},
	} {
		sink := newDiagSink(0, 0)
		ln := conv(sink)
		diags := sink.diags
		if ln.SettingControl == nil || ln.SettingControl.NumOfSGs != 0 || ln.SettingControl.ActSG != 1 {
			t.Errorf("%s: %+v", name, ln.SettingControl)
		}
		if name != "v17" && ln.SettingControl.ResvTms != 9 {
			t.Errorf("%s: ResvTms %d", name, ln.SettingControl.ResvTms)
		}
		if len(diags) != 1 || diags[0].Code != "invalid-setting-control" {
			t.Errorf("%s: diagnostics %v", name, diags)
		}
	}
}
