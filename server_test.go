package iec61850

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

func testServerSCL() *scl.SCL {
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
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID:      "LNT_LLN0",
				LNClass: "LLN0",
				DOs: []scl.DO{
					{Name: "Mod", Type: "DOT_INS"},
				},
			}},
			DOTypes: []scl.DOType{{
				ID:  "DOT_INS",
				CDC: "INS",
				DAs: []scl.DA{
					{Name: "stVal", FC: "ST", BType: "INT32"},
					{Name: "q", FC: "ST", BType: "Quality"},
					{Name: "t", FC: "ST", BType: "Timestamp"},
				},
			}},
		},
	}
}

func TestNewServer_FromSCL(t *testing.T) {
	s := testServerSCL()
	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if srv.Model() == nil {
		t.Error("Model() should not be nil")
	}
	if srv.ValueStore() == nil {
		t.Error("ValueStore() should not be nil")
	}
	if srv.MMS() == nil {
		t.Error("MMS() should not be nil")
	}
}

func TestNewServer_NilModel(t *testing.T) {
	_, err := NewServer(nil, ServerOptions{})
	if err == nil {
		t.Fatal("expected error for nil model")
	}
}

func TestNewServer_InvalidModel(t *testing.T) {
	m := &servermodel.Model{}
	_, err := NewServer(m, ServerOptions{})
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestNewServerModelFromSCL_IEDNotFound(t *testing.T) {
	s := testServerSCL()
	_, err := NewServerModelFromSCL(s, "BadIED", "")
	if err == nil {
		t.Fatal("expected error for non-existent IED")
	}
}

func testServerSCLWithBRCB() *scl.SCL {
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
							DataSets: []scl.DataSet{{
								Name: "dsTest",
								FCDAs: []scl.FCDA{{
									LNClass: "LLN0",
									DOName:  "Mod",
									DAName:  "stVal",
									FC:      "ST",
								}},
							}},
							Reports: []scl.ReportControl{{
								Name:     "brcb01",
								RptID:    "rpt01",
								DatSet:   "dsTest",
								ConfRev:  1,
								Buffered: true,
								BufTime:  500,
								IntgPd:   10000,
								TrgOps:   scl.TrgOps{Dchg: true, Qchg: true, GI: true},
								OptFields: scl.OptFields{
									SeqNum:     true,
									TimeStamp:  true,
									ReasonCode: true,
									ConfigRef:  true,
								},
							}},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID:      "LNT_LLN0",
				LNClass: "LLN0",
				DOs:     []scl.DO{{Name: "Mod", Type: "DOT_INS"}},
			}},
			DOTypes: []scl.DOType{{
				ID:  "DOT_INS",
				CDC: "INS",
				DAs: []scl.DA{
					{Name: "stVal", FC: "ST", BType: "INT32"},
					{Name: "q", FC: "ST", BType: "Quality"},
					{Name: "t", FC: "ST", BType: "Timestamp"},
				},
			}},
		},
	}
}

func TestBRCB_ClientServerRoundTrip(t *testing.T) {
	ctx := context.Background()

	sclData := testServerSCLWithBRCB()
	model, err := NewServerModelFromSCL(sclData, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	clientT, serverT := loopbackPair()
	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close(ctx) }()

	rcb, err := client.GetReportControlBlock(ctx, "LD1", "LLN0$BR$brcb01")
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}

	if rcb.Type != RCBBuffered {
		t.Errorf("Type = %v, want RCBBuffered", rcb.Type)
	}
	if rcb.RptID != "rpt01" {
		t.Errorf("RptID = %q, want rpt01", rcb.RptID)
	}
	if rcb.RptEna {
		t.Error("RptEna should be false initially")
	}
	if rcb.DatSet != "LD1/LLN0$dsTest" {
		t.Errorf("DatSet = %q, want LD1/LLN0$dsTest", rcb.DatSet)
	}
	if rcb.ConfRev != 1 {
		t.Errorf("ConfRev = %d, want 1", rcb.ConfRev)
	}
	if rcb.BufTm != 500 {
		t.Errorf("BufTm = %d, want 500", rcb.BufTm)
	}
	if rcb.IntgPd != 10000 {
		t.Errorf("IntgPd = %d, want 10000", rcb.IntgPd)
	}
	if !rcb.TrgOps.Has(TrgOpDataChanged) {
		t.Error("TrgOps should have DataChanged")
	}
	if !rcb.TrgOps.Has(TrgOpQualityChanged) {
		t.Error("TrgOps should have QualityChanged")
	}
	if !rcb.TrgOps.Has(TrgOpGI) {
		t.Error("TrgOps should have GI")
	}
	if !rcb.OptFlds.Has(OptFldSeqNum) {
		t.Error("OptFlds should have SeqNum")
	}
	if !rcb.OptFlds.Has(OptFldTimeStamp) {
		t.Error("OptFlds should have TimeStamp")
	}
	if !rcb.OptFlds.Has(OptFldReasonCode) {
		t.Error("OptFlds should have ReasonCode")
	}
	if !rcb.OptFlds.Has(OptFldConfRev) {
		t.Error("OptFlds should have ConfRev")
	}
}
