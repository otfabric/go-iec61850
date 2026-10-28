// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

func TestValuesEqual_ExtendedTypes(t *testing.T) {
	t0 := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name string
		a, b *mms.Value
		want bool
	}{
		{"float_eq", mms.NewFloat(1.5), mms.NewFloat(1.5), true},
		{"float_ne", mms.NewFloat(1.5), mms.NewFloat(2.5), false},
		{"mmsstring_eq", mms.NewMmsString("äb"), mms.NewMmsString("äb"), true},
		{"mmsstring_ne", mms.NewMmsString("äb"), mms.NewMmsString("ab"), false},
		{"utc_eq", mms.NewUTCTime(t0), mms.NewUTCTime(t0), true},
		{"utc_ne", mms.NewUTCTime(t0), mms.NewUTCTime(t0.Add(time.Second)), false},
		{"bintime_eq", mms.NewBinaryTime(1000), mms.NewBinaryTime(1000), true},
		{"bintime_ne", mms.NewBinaryTime(1000), mms.NewBinaryTime(2000), false},
		{"octet_len_ne", mms.NewOctetString([]byte{1}), mms.NewOctetString([]byte{1, 2}), false},
		{"struct_eq",
			mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)}),
			mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)}),
			true},
		{"struct_elem_ne",
			mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(true)}),
			mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewBoolean(false)}),
			false},
		{"struct_len_ne",
			mms.NewStructure([]*mms.Value{mms.NewInteger(1)}),
			mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}),
			false},
		{"array_eq",
			mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}),
			mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}),
			true},
		{"array_elem_ne",
			mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}),
			mms.NewArray([]*mms.Value{mms.NewInteger(1), mms.NewInteger(3)}),
			false},
		{"array_len_ne",
			mms.NewArray([]*mms.Value{mms.NewInteger(1)}),
			mms.NewArray(nil),
			false},
		// Types without a dedicated comparison are conservatively unequal
		// so a change is always reported.
		{"unsupported_type", mms.NewBCD(5), mms.NewBCD(5), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valuesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("valuesEqual = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeReportValuesEx_AllOptionalFields(t *testing.T) {
	all := OptFldSeqNum | OptFldTimeStamp | OptFldReasonCode | OptFldDataSet |
		OptFldBufOvfl | OptFldEntryID | OptFldConfRev | OptFldSegmentation
	before := time.Now().UTC().UnixMilli()
	vals := encodeReportValuesEx(reportParams{
		rptID:     "rpt",
		optFlds:   all,
		seqNum:    9,
		confRev:   3,
		datSet:    "LD1/LLN0$ds",
		bufOvfl:   true,
		entryID:   nil, // URCB-style: encoded as 8 zero bytes
		inclusion: []bool{true, false, true},
		reasons:   []ReasonCode{ReasonDataChanged, ReasonGI, ReasonGI | ReasonIntegrity},
		values:    []*mms.Value{mms.NewInteger(7), mms.NewInteger(8), nil},
	})
	after := time.Now().UTC().UnixMilli()

	// RptID, OptFlds, SeqNum, TimeStamp, DatSet, BufOvfl, EntryID, ConfRev,
	// SubSeqNum, MoreSegments, Inclusion, 2 values, 2 reasons.
	if len(vals) != 15 {
		t.Fatalf("len = %d, want 15", len(vals))
	}
	if s, _ := vals[0].VisibleString(); s != "rpt" {
		t.Errorf("RptID = %q", s)
	}
	if got := decodeOptFlds(vals[1]); got != all {
		t.Errorf("OptFlds round-trip = %v, want %v", got, all)
	}
	if n, _ := vals[2].Uint32(); n != 9 {
		t.Errorf("SeqNum = %d", n)
	}
	if ms, ok := vals[3].BinaryTime(); !ok || ms < before || ms > after {
		t.Errorf("TimeStamp = %d (ok=%v), want within [%d,%d]", ms, ok, before, after)
	}
	if s, _ := vals[4].VisibleString(); s != "LD1/LLN0$ds" {
		t.Errorf("DatSet = %q", s)
	}
	if b, _ := vals[5].Bool(); !b {
		t.Error("BufOvfl should be true")
	}
	if eid, _ := vals[6].OctetString(); !bytes.Equal(eid, make([]byte, 8)) {
		t.Errorf("EntryID = %x, want 8 zero bytes", eid)
	}
	if n, _ := vals[7].Uint32(); n != 3 {
		t.Errorf("ConfRev = %d", n)
	}
	if n, _ := vals[8].Uint32(); n != 0 {
		t.Errorf("SubSeqNum = %d", n)
	}
	if b, _ := vals[9].Bool(); b {
		t.Error("MoreSegments should be false")
	}
	if bits, _ := vals[10].BitString(); len(bits) != 1 || bits[0] != 0xA0 {
		t.Errorf("Inclusion = %x, want a0", bits)
	}
	if n, _ := vals[11].Int64(); n != 7 {
		t.Errorf("value[0] = %d", n)
	}
	// A nil included value is substituted with boolean false.
	if vals[12].Type() != mms.ValueTypeBoolean {
		t.Errorf("nil value substitute type = %v", vals[12].Type())
	}
	if bits, _ := vals[13].BitString(); len(bits) != 1 || bits[0] != 0x40 {
		t.Errorf("reason[0] = %x, want 40 (dchg)", bits)
	}
	if bits, _ := vals[14].BitString(); len(bits) != 1 || bits[0] != 0x0C {
		t.Errorf("reason[2] = %x, want 0c (integrity|gi)", bits)
	}
}

func TestEncodeReportValuesEx_ExplicitEntryID(t *testing.T) {
	eid := encodeEntryID(42)
	vals := encodeReportValuesEx(reportParams{
		rptID:     "r",
		optFlds:   OptFldEntryID,
		entryID:   eid,
		inclusion: []bool{true},
		values:    []*mms.Value{mms.NewBoolean(true)},
	})
	// RptID, OptFlds, EntryID, Inclusion, value.
	if len(vals) != 5 {
		t.Fatalf("len = %d, want 5", len(vals))
	}
	got, _ := vals[2].OctetString()
	if decodeEntryID(got) != 42 {
		t.Errorf("EntryID = %x, want 42", got)
	}
}
