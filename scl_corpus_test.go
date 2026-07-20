// SPDX-License-Identifier: MIT

// scl_corpus_test.go tests NewServerModelFromSCL against a variety of SCL
// structures that cover edge cases: multi-LN, multi-LD, SDO chains, enum types,
// DATypes with BDAs, mixed CDCs, and nested type hierarchies.
package iec61850

import (
	"context"
	"strings"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// ────────────────────────────────────────────────────────────────────────────
// helpers — shared SCL corpus builders
// ────────────────────────────────────────────────────────────────────────────

func sclMultiLN() *scl.SCL {
	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0: &scl.LN{
							LNClass: "LLN0",
							Inst:    "",
							LNType:  "LNT_LLN0",
						},
						LNs: []scl.LN{
							{LNClass: "GGIO", Inst: "1", LNType: "LNT_GGIO"},
							{LNClass: "GGIO", Inst: "2", LNType: "LNT_GGIO"},
							{LNClass: "GGIO", Inst: "3", LNType: "LNT_GGIO"},
							{LNClass: "MMXU", Inst: "1", LNType: "LNT_MMXU"},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{
				{ID: "LNT_LLN0", LNClass: "LLN0", DOs: []scl.DO{{Name: "Mod", Type: "DOT_INS"}}},
				{ID: "LNT_GGIO", LNClass: "GGIO", DOs: []scl.DO{
					{Name: "Ind1", Type: "DOT_SPS"},
					{Name: "Ind2", Type: "DOT_SPS"},
				}},
				{ID: "LNT_MMXU", LNClass: "MMXU", DOs: []scl.DO{
					{Name: "TotW", Type: "DOT_MV"},
				}},
			},
			DOTypes: []scl.DOType{
				{ID: "DOT_INS", CDC: "INS", DAs: []scl.DA{
					{Name: "stVal", FC: "ST", BType: "INT32"},
					{Name: "q", FC: "ST", BType: "Quality"},
					{Name: "t", FC: "ST", BType: "Timestamp"},
				}},
				{ID: "DOT_SPS", CDC: "SPS", DAs: []scl.DA{
					{Name: "stVal", FC: "ST", BType: "BOOLEAN"},
					{Name: "q", FC: "ST", BType: "Quality"},
					{Name: "t", FC: "ST", BType: "Timestamp"},
				}},
				{ID: "DOT_MV", CDC: "MV", DAs: []scl.DA{
					{Name: "mag", FC: "MX", BType: "Struct", Type: "DAT_ANALOGUE_VALUE"},
					{Name: "q", FC: "MX", BType: "Quality"},
					{Name: "t", FC: "MX", BType: "Timestamp"},
				}},
			},
			DATypes: []scl.DAType{{
				ID: "DAT_ANALOGUE_VALUE",
				BDAs: []scl.BDA{
					{Name: "f", BType: "FLOAT32"},
					{Name: "i", BType: "INT32"},
				},
			}},
		},
	}
}

func sclMultiLD() *scl.SCL {
	lnType := scl.LNodeType{
		ID:      "LNT_LLN0_BASIC",
		LNClass: "LLN0",
		DOs:     []scl.DO{{Name: "Mod", Type: "DOT_INS_BASIC"}},
	}
	doType := scl.DOType{
		ID:  "DOT_INS_BASIC",
		CDC: "INS",
		DAs: []scl.DA{
			{Name: "stVal", FC: "ST", BType: "INT32"},
			{Name: "q", FC: "ST", BType: "Quality"},
			{Name: "t", FC: "ST", BType: "Timestamp"},
		},
	}

	makeLDevice := func(inst string) scl.LDevice {
		return scl.LDevice{
			Inst: inst,
			LN0:  &scl.LN{LNClass: "LLN0", Inst: "", LNType: "LNT_LLN0_BASIC"},
		}
	}

	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{
						makeLDevice("Protection"),
						makeLDevice("Measurement"),
						makeLDevice("Control"),
						makeLDevice("Supervision"),
					},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{lnType},
			DOTypes:    []scl.DOType{doType},
		},
	}
}

func sclWithSDOChain() *scl.SCL {
	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0:  &scl.LN{LNClass: "LLN0", Inst: "", LNType: "LNT_LLN0_SDO"},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID:      "LNT_LLN0_SDO",
				LNClass: "LLN0",
				DOs: []scl.DO{
					{Name: "Pos", Type: "DOT_DPC"},
					{Name: "Beh", Type: "DOT_INS_SDO"},
				},
			}},
			DOTypes: []scl.DOType{
				{
					ID:  "DOT_DPC",
					CDC: "DPC",
					DAs: []scl.DA{
						{Name: "stVal", FC: "ST", BType: "Dbpos"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					},
					SDOs: []scl.SDO{
						{Name: "Oper", Type: "DOT_CTRL_DPC"},
					},
				},
				{
					ID:  "DOT_CTRL_DPC",
					CDC: "DPC",
					DAs: []scl.DA{
						{Name: "ctlVal", FC: "CO", BType: "Dbpos"},
						{Name: "ctlNum", FC: "CO", BType: "INT8U"},
					},
				},
				{
					ID:  "DOT_INS_SDO",
					CDC: "INS",
					DAs: []scl.DA{
						{Name: "stVal", FC: "ST", BType: "INT32"},
						{Name: "q", FC: "ST", BType: "Quality"},
						{Name: "t", FC: "ST", BType: "Timestamp"},
					},
				},
			},
		},
	}
}

func sclWithEnumDAs() *scl.SCL {
	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0:  &scl.LN{LNClass: "LLN0", Inst: "", LNType: "LNT_ENUM"},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID:      "LNT_ENUM",
				LNClass: "LLN0",
				DOs:     []scl.DO{{Name: "Mod", Type: "DOT_ENS"}},
			}},
			DOTypes: []scl.DOType{{
				ID:  "DOT_ENS",
				CDC: "ENS",
				DAs: []scl.DA{
					{Name: "stVal", FC: "ST", BType: "Enum", Type: "ET_HEALTH"},
					{Name: "q", FC: "ST", BType: "Quality"},
					{Name: "t", FC: "ST", BType: "Timestamp"},
				},
			}},
			EnumTypes: []scl.EnumType{{
				ID: "ET_HEALTH",
				Vals: []scl.EnumVal{
					{Ord: 1, Value: "Ok"},
					{Ord: 2, Value: "Warning"},
					{Ord: 3, Value: "Alarm"},
				},
			}},
		},
	}
}

// dialLoopback creates an IEC 61850 client over the loopback transport pair.
func dialLoopback(t *testing.T, ctx context.Context, srv *Server) *Client {
	t.Helper()
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	c, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })
	return c
}

// ────────────────────────────────────────────────────────────────────────────
// Tests
// ────────────────────────────────────────────────────────────────────────────

func TestSCLCorpus_MultiLN_ServerModel(t *testing.T) {
	model, err := NewServerModelFromSCL(sclMultiLN(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	lds, err := c.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
	if len(lds) != 1 {
		t.Errorf("expected 1 LD, got %d", len(lds))
	}

	// The LD should expose LLN0, GGIO1, GGIO2, GGIO3, MMXU1.
	lns, err := c.ListLogicalNodes(ctx, "LD1")
	if err != nil {
		t.Fatalf("ListLogicalNodes: %v", err)
	}
	wantPrefixes := []string{"LLN0", "GGIO1", "GGIO2", "GGIO3", "MMXU1"}
	for _, want := range wantPrefixes {
		found := false
		for _, ln := range lns {
			if strings.HasPrefix(string(ln.Name), want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected LN with prefix %q in LD1, not found (lns: %v)", want, lns)
		}
	}
}

func TestSCLCorpus_MultiLD_ServerModel(t *testing.T) {
	model, err := NewServerModelFromSCL(sclMultiLD(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	lds, err := c.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
	wantLDs := []string{"Protection", "Measurement", "Control", "Supervision"}
	if len(lds) != len(wantLDs) {
		t.Errorf("expected %d LDs, got %d: %v", len(wantLDs), len(lds), lds)
	}
}

func TestSCLCorpus_SDOChain_ServerModel(t *testing.T) {
	model, err := NewServerModelFromSCL(sclWithSDOChain(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL with SDO chain: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	items, err := c.ListLogicalNodes(ctx, "LD1")
	if err != nil {
		t.Fatalf("ListLogicalNodes: %v", err)
	}
	if len(items) == 0 {
		t.Error("expected non-empty LN list for SDO model")
	}
}

func TestSCLCorpus_EnumDA_ServerModel(t *testing.T) {
	model, err := NewServerModelFromSCL(sclWithEnumDAs(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL with enum DAs: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	// stVal of an ENS is an enum; read it and verify it doesn't error.
	ref, err := ParseRef("LD1/LLN0.Mod.stVal[ST]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	_, err = c.Read(ctx, ref)
	if err != nil {
		t.Errorf("Read ENS stVal: %v", err)
	}
}

func TestSCLCorpus_DAType_BDA_ServerModel(t *testing.T) {
	model, err := NewServerModelFromSCL(sclMultiLN(), "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL (DAType/BDA): %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	// MMXU1.TotW.mag is a struct (AnalogueValue with f + i sub-fields).
	ref, err := ParseRef("LD1/MMXU1.TotW.mag[MX]")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	val, err := c.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read MMXU1.TotW.mag: %v", err)
	}
	if val == nil {
		t.Error("Read returned nil value for MMXU1.TotW.mag")
	}
}

// TestSCLCorpus_MultiLN_Reports verifies that report control blocks can be
// enabled across multiple LNs within the same LD.
func TestSCLCorpus_MultiLN_Reports(t *testing.T) {

	// Build SCL with URCB on LLN0 of the multi-LN model.
	s := sclMultiLN()
	s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.Reports = []scl.ReportControl{{
		Name:    "urcb01",
		RptID:   "rpt_multi_ln",
		DatSet:  "dsGGIO",
		ConfRev: 1,
		TrgOps:  scl.TrgOps{Dchg: true, GI: true},
		OptFields: scl.OptFields{
			SeqNum:     true,
			TimeStamp:  true,
			ReasonCode: true,
		},
	}}
	s.IEDs[0].AccessPoints[0].Server.LDevices[0].LN0.DataSets = []scl.DataSet{{
		Name: "dsGGIO",
		FCDAs: []scl.FCDA{
			{LNClass: "GGIO", LNInst: "1", DOName: "Ind1", DAName: "stVal", FC: "ST"},
			{LNClass: "GGIO", LNInst: "2", DOName: "Ind1", DAName: "stVal", FC: "ST"},
		},
	}}

	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	re := srv.EnableReports()
	defer re.Stop()

	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)

	sub, err := c.SubscribeReport(ctx, "rpt_multi_ln", SubscribeReportOptions{
		LD:            "LD1",
		RCBItemID:     "LLN0$RP$urcb01",
		AutoEnable:    true,
		GIOnSubscribe: true,
	})
	if err != nil {
		t.Fatalf("SubscribeReport: %v", err)
	}
	defer func() { _ = sub.Close() }()

	select {
	case rpt := <-sub.Reports():
		t.Logf("received GI report: %d values", len(rpt.Values))
		if len(rpt.Values) < 2 {
			t.Errorf("expected at least 2 dataset members (GGIO1+GGIO2), got %d", len(rpt.Values))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GI report not received")
	}
}
