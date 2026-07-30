// SPDX-License-Identifier: MIT

package index

import (
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

func TestFindGSEControlAndSMVControl(t *testing.T) {
	s := &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "S1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD0",
						LN0: &scl.LN{
							LNClass: "LLN0",
							GSEControls: []scl.GSEControl{
								{Name: "gcb1", AppID: "app1"},
							},
							SMVControls: []scl.SMVControl{
								{Name: "smv1", SmvID: "sv1"},
							},
						},
					}},
				},
			}},
		}},
	}
	idx, _ := Build(s)

	gc := idx.FindGSEControl("IED1", "LD0", "", "LLN0", "", "gcb1")
	if gc == nil || gc.AppID != "app1" {
		t.Fatalf("FindGSEControl = %+v", gc)
	}
	if idx.FindGSEControl("IED1", "LD0", "", "LLN0", "", "missing") != nil {
		t.Error("expected nil for missing GSEControl")
	}

	sv := idx.FindSMVControl("IED1", "LD0", "", "LLN0", "", "smv1")
	if sv == nil || sv.SmvID != "sv1" {
		t.Fatalf("FindSMVControl = %+v", sv)
	}
	if idx.FindSMVControl("IED1", "LD0", "", "LLN0", "", "missing") != nil {
		t.Error("expected nil for missing SMVControl")
	}
}
