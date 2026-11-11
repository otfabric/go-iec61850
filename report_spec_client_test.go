// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// brcbValue builds a GetBRCBValues structure in IEC 61850-8-1 Table 61
// order, followed by the given optional trailing components.
func brcbValue(trailing ...*mms.Value) *mms.Value {
	elems := []*mms.Value{
		mms.NewVisibleString("rid"),
		mms.NewBoolean(false),
		mms.NewVisibleString("LD1/LLN0$ds"),
		mms.NewUnsigned(1),
		encodeOptFlds(OptFldSeqNum),
		mms.NewUnsigned(0),
		mms.NewUnsigned(0),
		encodeTrgOps(TrgOpDataChanged),
		mms.NewUnsigned(0),
		mms.NewBoolean(false),
		mms.NewBoolean(false),
		mms.NewOctetString(make([]byte, 8)),
		mms.NewBinaryTime(0),
	}
	return mms.NewStructure(append(elems, trailing...))
}

// TestSpec_8_1_17_1_2_DecodeRCB_OwnerWithoutResvTms: ResvTms and Owner are
// both optional (IEC 61850-7-2 Table 37 c3/c4); an Owner without ResvTms
// is not consumed as ResvTms (RPT-24).
func TestSpec_8_1_17_1_2_DecodeRCB_OwnerWithoutResvTms(t *testing.T) {
	owner := []byte{192, 168, 0, 7}
	rcb, err := decodeRCB("LD1", "LLN0$BR$brcb01", RCBBuffered, brcbValue(mms.NewOctetString(owner)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rcb.Owner, owner) || rcb.ResvTms != 0 {
		t.Errorf("Owner = %v ResvTms = %d, want %v / 0", rcb.Owner, rcb.ResvTms, owner)
	}
}

// TestSpec_8_1_Table61_DecodeRCB_ResvTms: ResvTms is INT16 in IEC 61850-7-2
// Table 37 and Unsigned in IEC 61850-8-1 Table 61; both decode.
func TestSpec_8_1_Table61_DecodeRCB_ResvTms(t *testing.T) {
	for name, v := range map[string]*mms.Value{
		"unsigned": mms.NewUnsigned(30),
		"integer":  mms.NewInteger(30),
	} {
		t.Run(name, func(t *testing.T) {
			rcb, err := decodeRCB("LD1", "LLN0$BR$brcb01", RCBBuffered, brcbValue(v, mms.NewOctetString([]byte{1})))
			if err != nil {
				t.Fatal(err)
			}
			if rcb.ResvTms != 30 || !bytes.Equal(rcb.Owner, []byte{1}) {
				t.Errorf("ResvTms = %d Owner = %v", rcb.ResvTms, rcb.Owner)
			}
		})
	}
	rcb, err := decodeRCB("LD1", "LLN0$BR$brcb01", RCBBuffered, brcbValue(mms.NewInteger(-1)))
	if err != nil || rcb.ResvTms != -1 {
		t.Errorf("ResvTms -1: %v %v", rcb, err)
	}
}

// TestSpec_8_1_8_1_3_10_DecodeReasonCode_ApplicationTrigger: bit 6 of the
// ReasonCode bitstring is application-trigger (RPT-25).
func TestSpec_8_1_8_1_3_10_DecodeReasonCode_ApplicationTrigger(t *testing.T) {
	v := mms.NewBitStringWithLength([]byte{0x02}, 7)
	if got := decodeReasonCode(v); got != ReasonApplicationTrigger {
		t.Errorf("decodeReasonCode(0000001) = %v, want application-trigger", got)
	}
	if bits, _ := encodeReasonCode(ReasonApplicationTrigger).BitString(); bits[0] != 0x02 {
		t.Errorf("encodeReasonCode(application-trigger) = %x, want 02", bits)
	}
}

// segment returns a segmented report value list for segRpt with SeqNum 3.
func segment(sub uint32, more bool, incl *mms.Value, vals ...*mms.Value) *mms.InformationReportRequest {
	return &mms.InformationReportRequest{
		ListName: &mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: "RPT"},
		Values: append([]*mms.Value{
			mms.NewVisibleString("segRpt"),
			encodeOptFlds(OptFldSeqNum | OptFldSegmentation | OptFldReasonCode),
			mms.NewUnsigned(3),
			mms.NewUnsigned(uint64(sub)),
			mms.NewBoolean(more),
			incl,
		}, vals...),
	}
}

// TestSpec_8_1_17_2_1_SegmentedReport_InclusionMerged: every segment
// carries a full-width inclusion bitstring; the assembled report ORs them
// and lists values in data set order (RPT-22).
func TestSpec_8_1_17_2_1_SegmentedReport_InclusionMerged(t *testing.T) {
	client, srv := setupReportLoopback(t)
	ctx := context.Background()
	sub, err := client.SubscribeReport(ctx, "segRpt", SubscribeReportOptions{QueueSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()

	dchg := encodeReasonCode(ReasonDataChanged)
	gi := encodeReasonCode(ReasonGI)
	_ = srv.Broadcast(ctx, segment(0, true, mms.NewBitStringWithLength([]byte{0xC0}, 3),
		mms.NewInteger(1), mms.NewInteger(2), dchg, gi))
	_ = srv.Broadcast(ctx, segment(1, false, mms.NewBitStringWithLength([]byte{0x20}, 3),
		mms.NewInteger(3), gi))

	select {
	case ri := <-sub.Reports():
		if !reflect.DeepEqual(ri.Inclusion, []bool{true, true, true}) {
			t.Errorf("Inclusion = %v, want [true true true]", ri.Inclusion)
		}
		if len(ri.Values) != 3 {
			t.Fatalf("values = %d, want 3", len(ri.Values))
		}
		for i, want := range []int64{1, 2, 3} {
			if got, _ := ri.Values[i].Int64(); got != want {
				t.Errorf("value[%d] = %d, want %d", i, got, want)
			}
		}
		if want := []ReasonCode{ReasonDataChanged, ReasonGI, ReasonGI}; !reflect.DeepEqual(ri.ReasonCodes, want) {
			t.Errorf("reasons = %v, want %v", ri.ReasonCodes, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no assembled report")
	}
}

// TestSegmentedReport_OverlapOrWidthMismatchNotMerged: a member in two
// segments, or segments of different widths, are not a conformant
// report; the buffer is dropped and only the first segment delivered.
func TestSegmentedReport_OverlapOrWidthMismatchNotMerged(t *testing.T) {
	for name, second := range map[string]*mms.Value{
		"overlap": mms.NewBitStringWithLength([]byte{0x80}, 2),
		"width":   mms.NewBitStringWithLength([]byte{0x40}, 3),
	} {
		t.Run(name, func(t *testing.T) {
			client, srv := setupReportLoopback(t)
			ctx := context.Background()
			sub, err := client.SubscribeReport(ctx, "segRpt", SubscribeReportOptions{QueueSize: 8})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sub.Close() }()
			r := encodeReasonCode(ReasonGI)
			_ = srv.Broadcast(ctx, segment(0, true, mms.NewBitStringWithLength([]byte{0x80}, 2), mms.NewInteger(1), r))
			_ = srv.Broadcast(ctx, segment(1, false, second, mms.NewInteger(2), r))
			select {
			case ri := <-sub.Reports():
				if len(ri.Values) != 1 {
					t.Errorf("values = %d, want 1 (first segment only)", len(ri.Values))
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no report")
			}
		})
	}
}
