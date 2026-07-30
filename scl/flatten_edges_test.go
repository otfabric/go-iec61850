// SPDX-License-Identifier: MIT

package scl

import "testing"

func TestExportGSEControls(t *testing.T) {
	s := &SCL{
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{
				{Name: "AP0"}, // Server nil → skipped
				{
					Name: "S1",
					Server: &Server{
						LDevices: []LDevice{
							{Inst: "LD0"}, // LN0 nil → skipped
							{
								Inst: "LD1",
								LN0: &LN{
									LNClass: "LLN0",
									GSEControls: []GSEControl{{
										Name: "gcb1", AppID: "app1", Type: "GOOSE",
										DatSet: "ds1", ConfRev: 2, Desc: "goose",
									}},
								},
							},
						},
					},
				},
			},
		}},
	}

	rows := ExportGSEControls(s)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.IED != "IED1" || r.AccessPoint != "S1" || r.LD != "LD1" {
		t.Errorf("location = %s/%s/%s", r.IED, r.AccessPoint, r.LD)
	}
	if r.Name != "gcb1" || r.AppID != "app1" || r.Type != "GOOSE" || r.DatSet != "ds1" || r.ConfRev != 2 || r.Desc != "goose" {
		t.Errorf("unexpected row: %+v", r)
	}

	if n := len(ExportGSEControls(&SCL{})); n != 0 {
		t.Errorf("empty SCL: got %d rows", n)
	}
}

func TestExportSMVControls(t *testing.T) {
	s := &SCL{
		IEDs: []IED{{
			Name: "IED1",
			AccessPoints: []AccessPoint{
				{Name: "AP0"},
				{
					Name: "S1",
					Server: &Server{
						LDevices: []LDevice{
							{Inst: "LD0"},
							{
								Inst: "LD1",
								LN0: &LN{
									LNClass: "LLN0",
									SMVControls: []SMVControl{{
										Name: "smv1", SmvID: "sv1", DatSet: "ds1",
										SmpRate: 4800, NofASDU: 1, Multicast: true,
										ConfRev: 3, Desc: "sv",
									}},
								},
							},
						},
					},
				},
			},
		}},
	}

	rows := ExportSMVControls(s)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Name != "smv1" || r.SmvID != "sv1" || !r.Multicast || r.SmpRate != 4800 || r.NofASDU != 1 {
		t.Errorf("unexpected row: %+v", r)
	}
	if r.IED != "IED1" || r.AccessPoint != "S1" || r.LD != "LD1" || r.DatSet != "ds1" || r.ConfRev != 3 {
		t.Errorf("unexpected location/meta: %+v", r)
	}
}

func TestExportConnectedAPs(t *testing.T) {
	if rows := ExportConnectedAPs(&SCL{}); len(rows) != 0 {
		t.Fatalf("nil Communication: got %d rows", len(rows))
	}

	s := &SCL{
		Communication: &Communication{
			SubNetworks: []SubNetwork{{
				Name: "Net1",
				ConnectedAPs: []ConnectedAP{{
					IEDName: "IED1",
					APName:  "S1",
					GSEs:    []GSEAddress{{CBName: "g1"}, {CBName: "g2"}},
					SMVs:    []SMVAddress{{CBName: "s1"}},
				}},
			}},
		},
	}
	rows := ExportConnectedAPs(s)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.SubNetwork != "Net1" || r.IEDName != "IED1" || r.APName != "S1" {
		t.Errorf("unexpected row: %+v", r)
	}
	if r.GSECount != 2 || r.SMVCount != 1 {
		t.Errorf("counts GSE=%d SMV=%d", r.GSECount, r.SMVCount)
	}
}
