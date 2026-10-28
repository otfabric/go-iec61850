// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	mms "github.com/otfabric/go-mms"
)

// rcbWriteRecorder is an MMS server exposing every RCB sub-attribute of
// LD1/LLN0$BR$brcb01 as a writable variable that records the order and
// values of client writes.
type rcbWriteRecorder struct {
	mu     sync.Mutex
	order  []string
	values map[string]*mms.Value
	failOn string
}

func newRCBWriteRecorder(t *testing.T) (*Client, *rcbWriteRecorder) {
	t.Helper()
	ctx := context.Background()
	rec := &rcbWriteRecorder{values: map[string]*mms.Value{}}
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain("LD1"); err != nil {
		t.Fatal(err)
	}
	attrs := map[string]mms.TypeSpec{
		"RptID":    {Type: mms.ValueTypeVisibleString, Size: 129},
		"RptEna":   {Type: mms.ValueTypeBoolean},
		"DatSet":   {Type: mms.ValueTypeVisibleString, Size: 129},
		"OptFlds":  {Type: mms.ValueTypeBitString, Size: 10},
		"BufTm":    {Type: mms.ValueTypeUnsigned, Size: 32},
		"TrgOps":   {Type: mms.ValueTypeBitString, Size: 6},
		"IntgPd":   {Type: mms.ValueTypeUnsigned, Size: 32},
		"GI":       {Type: mms.ValueTypeBoolean},
		"Resv":     {Type: mms.ValueTypeBoolean},
		"PurgeBuf": {Type: mms.ValueTypeBoolean},
		"EntryID":  {Type: mms.ValueTypeOctetString, Size: 8},
		"ResvTms":  {Type: mms.ValueTypeInteger, Size: 16},
	}
	for name, ts := range attrs {
		name, ts := name, ts
		if err := srv.RegisterVariable(mms.Variable{
			Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "LD1", ItemID: mms.ItemID("LLN0$BR$brcb01$" + name)},
			TypeSpec: ts,
			Read:     func(context.Context) (*mms.Value, error) { return ts.DefaultValue(), nil },
			Write: func(_ context.Context, v *mms.Value) error {
				rec.mu.Lock()
				defer rec.mu.Unlock()
				if rec.failOn == name {
					return errors.New("refused")
				}
				rec.order = append(rec.order, name)
				rec.values[name] = v
				return nil
			},
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	clientT, serverT := loopbackPair()
	go func() { _ = srv.Serve(ctx, serverT) }()
	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })
	return c, rec
}

func TestSetReportControlBlock_AllFieldsWrittenWithRptEnaLast(t *testing.T) {
	c, rec := newRCBWriteRecorder(t)
	all := RCBFieldRptID | RCBFieldRptEna | RCBFieldDatSet | RCBFieldOptFlds | RCBFieldBufTm |
		RCBFieldTrgOps | RCBFieldIntgPd | RCBFieldGI | RCBFieldResv | RCBFieldPurgeBuf |
		RCBFieldEntryID | RCBFieldResvTms
	err := c.SetReportControlBlock(context.Background(), "LD1", "LLN0$BR$brcb01", RCBUpdate{
		Fields:   all,
		RptID:    "rid",
		RptEna:   true,
		DatSet:   "LD1/LLN0$ds",
		OptFlds:  OptFldSeqNum | OptFldEntryID,
		BufTm:    250,
		TrgOps:   TrgOpDataChanged | TrgOpGI,
		IntgPd:   5000,
		GI:       true,
		Resv:     true,
		PurgeBuf: true,
		EntryID:  []byte{0, 0, 0, 0, 0, 0, 0, 7},
		ResvTms:  30,
	})
	if err != nil {
		t.Fatalf("SetReportControlBlock: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	wantOrder := []string{"RptID", "DatSet", "OptFlds", "BufTm", "TrgOps", "IntgPd", "GI",
		"Resv", "PurgeBuf", "EntryID", "ResvTms", "RptEna"}
	if !reflect.DeepEqual(rec.order, wantOrder) {
		t.Fatalf("write order = %v, want %v", rec.order, wantOrder)
	}
	if s, _ := rec.values["RptID"].VisibleString(); s != "rid" {
		t.Errorf("RptID = %q", s)
	}
	if s, _ := rec.values["DatSet"].VisibleString(); s != "LD1/LLN0$ds" {
		t.Errorf("DatSet = %q", s)
	}
	if got := decodeOptFlds(rec.values["OptFlds"]); got != OptFldSeqNum|OptFldEntryID {
		t.Errorf("OptFlds = %v", got)
	}
	if n, _ := rec.values["BufTm"].Uint32(); n != 250 {
		t.Errorf("BufTm = %d", n)
	}
	if n, _ := rec.values["IntgPd"].Uint32(); n != 5000 {
		t.Errorf("IntgPd = %d", n)
	}
	for _, b := range []string{"GI", "Resv", "PurgeBuf", "RptEna"} {
		if v, _ := rec.values[b].Bool(); !v {
			t.Errorf("%s = false, want true", b)
		}
	}
	if eid, _ := rec.values["EntryID"].OctetString(); decodeEntryID(eid) != 7 {
		t.Errorf("EntryID = %x", eid)
	}
	if n, _ := rec.values["ResvTms"].Int64(); n != 30 {
		t.Errorf("ResvTms = %d", n)
	}
}

func TestSetReportControlBlock_StopsAtFirstFailedWrite(t *testing.T) {
	c, rec := newRCBWriteRecorder(t)
	rec.failOn = "TrgOps"
	err := c.SetReportControlBlock(context.Background(), "LD1", "LLN0$BR$brcb01", RCBUpdate{
		Fields: RCBFieldBufTm | RCBFieldTrgOps | RCBFieldRptEna,
		BufTm:  1,
		RptEna: true,
	})
	if err == nil || !strings.Contains(err.Error(), "LD1/LLN0$BR$brcb01.TrgOps") {
		t.Fatalf("err = %v, want failure naming TrgOps", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !reflect.DeepEqual(rec.order, []string{"BufTm"}) {
		t.Errorf("writes = %v; RptEna must not be written after a failed config write", rec.order)
	}
}

func TestSetReportControlBlock_ArgumentValidation(t *testing.T) {
	c, rec := newRCBWriteRecorder(t)
	ctx := context.Background()
	if err := c.SetReportControlBlock(ctx, "", "x", RCBUpdate{Fields: RCBFieldGI}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty LD: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, "LD1", "", RCBUpdate{Fields: RCBFieldGI}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty RCB: %v", err)
	}
	if err := c.SetReportControlBlock(ctx, "LD1", "LLN0$BR$brcb01", RCBUpdate{}); err != nil {
		t.Errorf("no fields should be a no-op, got %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.order) != 0 {
		t.Errorf("unexpected writes: %v", rec.order)
	}
}
