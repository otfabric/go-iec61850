// SPDX-License-Identifier: MIT

package scl

import (
	"testing"

	raw "github.com/otfabric/go-iec61850/scl/internal/raw/v17"
)

func TestConvV17_PrivateSMVSettingControl(t *testing.T) {
	var diags []Diagnostic
	ap := convAP17(raw.AccessPoint{
		Name: "AP1",
		Private: []raw.Private{{
			Type: "vendor", Source: "src", InnerXML: "<x/>",
		}},
		Server: &raw.Server{
			LDevice: []raw.LDevice{{
				Inst: "LD1",
				Desc: "ld",
				Private: []raw.Private{{
					Type: "ldPriv",
				}},
				LN0: raw.LN0{
					LnClass: "LLN0",
					LnType:  "T0",
					Private: []raw.Private{{Type: "ln0Priv"}},
					SampledValueControl: []raw.SampledValueControl{{
						Name: "smv1", SmvID: "SV1", DatSet: "ds1",
						ConfRev: 1, SmpRate: 4800, NofASDU: 1, Multicast: true,
					}},
					SettingControl: &raw.SettingControl{NumOfSGs: 4, ActSG: 1},
					LogControl:     []raw.LogControl{{Name: "log1", Desc: "d"}},
				},
				LN: []raw.LN{{
					Prefix:     "Q",
					LnClass:    "GGIO",
					Inst:       1,
					LnType:     "T1",
					Desc:       "ln",
					Private:    []raw.Private{{Type: "lnPriv"}},
					LogControl: []raw.LogControl{{Name: "lnLog", Desc: "d"}},
					DOI:        []raw.DOI{{Name: "Ind1"}},
				}},
			}},
		},
	}, &diags)

	if len(ap.Private) != 1 || ap.Private[0].Type != "vendor" {
		t.Fatalf("AP private: %+v", ap.Private)
	}
	if ap.Server == nil || len(ap.Server.LDevices) != 1 {
		t.Fatal("expected LD")
	}
	ld := ap.Server.LDevices[0]
	if len(ld.Private) != 1 {
		t.Fatalf("LD private: %+v", ld.Private)
	}
	if ld.LN0 == nil || ld.LN0.SettingControl == nil || ld.LN0.SettingControl.NumOfSGs != 4 {
		t.Fatalf("SettingControl: %+v", ld.LN0)
	}
	if len(ld.LN0.SMVControls) != 1 || ld.LN0.SMVControls[0].Name != "smv1" {
		t.Fatalf("SMV: %+v", ld.LN0.SMVControls)
	}
	if len(ld.LN0.Logs) != 1 || ld.LN0.Logs[0].Name != "log1" {
		t.Fatalf("Logs: %+v", ld.LN0.Logs)
	}
	if len(ld.LN0.Private) != 1 {
		t.Fatalf("LN0 private: %+v", ld.LN0.Private)
	}
	if len(ld.LNs) != 1 || ld.LNs[0].Prefix != "Q" || len(ld.LNs[0].Private) != 1 {
		t.Fatalf("LN: %+v", ld.LNs)
	}
}
