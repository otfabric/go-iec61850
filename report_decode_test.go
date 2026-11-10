// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// TestDecodeReportRoundTrip: what the server's report engine encodes,
// DecodeReport decodes, for every optional field, and as the client's own
// decoder does.
func TestDecodeReportRoundTrip(t *testing.T) {
	all := OptFldSeqNum | OptFldTimeStamp | OptFldReasonCode | OptFldDataSet | OptFldDataRef |
		OptFldBufOvfl | OptFldEntryID | OptFldConfRev | OptFldSegmentation
	for _, opt := range []OptFlds{0, OptFldSeqNum | OptFldDataSet, all} {
		p := reportParams{
			rptID: "LD0/LLN0$BR$brcb01", optFlds: opt, seqNum: 7, confRev: 3,
			datSet: "LD0/LLN0$Events", bufOvfl: true, entryID: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			inclusion: []bool{true, false, true},
			dataRefs:  []string{"LD0/GGIO1$ST$Ind1", "LD0/GGIO1$ST$Ind2", "LD0/MMXU1$MX$Hz"},
			reasons:   []ReasonCode{ReasonDataChanged, 0, ReasonGI},
			values:    []*mms.Value{mms.NewBoolean(true), mms.NewBoolean(false), mms.NewFloat(50)},
		}
		values := encodeReportValuesEx(p)
		ri, err := DecodeReport(values)
		if err != nil {
			t.Fatalf("OptFlds %v: %v", opt, err)
		}
		if ri.RptID != p.rptID || ri.OptFlds != opt || !slices.Equal(ri.Inclusion, p.inclusion) || len(ri.Values) != 2 {
			t.Fatalf("OptFlds %v: %+v", opt, ri)
		}
		if f, _ := ri.Values[1].MMS().Float64(); f != 50 {
			t.Errorf("OptFlds %v: second value %v", opt, ri.Values[1].MMS())
		}
		if opt == all {
			if ri.SeqNum != 7 || ri.ConfRev != 3 || ri.DatSet != p.datSet || !ri.BufOvfl ||
				!slices.Equal(ri.EntryID, p.entryID) || ri.Timestamp.IsZero() ||
				!slices.Equal(ri.DataReferences, []string{p.dataRefs[0], p.dataRefs[2]}) ||
				!slices.Equal(ri.ReasonCodes, []ReasonCode{ReasonDataChanged, ReasonGI}) {
				t.Errorf("all fields: %+v", ri)
			}
		}
		lenient, err := decodeReportIndication(values, nil)
		if err != nil || lenient.RptID != ri.RptID || lenient.SeqNum != ri.SeqNum || len(lenient.Values) != len(ri.Values) ||
			!slices.Equal(lenient.ReasonCodes, ri.ReasonCodes) || !slices.Equal(lenient.DataReferences, ri.DataReferences) {
			t.Errorf("OptFlds %v: the client's decoder gives %+v, %v", opt, lenient, err)
		}
	}
}

func TestDecodeReportUTCTime(t *testing.T) {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	values := []*mms.Value{
		mms.NewVisibleString("R"), encodeOptFlds(OptFldTimeStamp),
		mms.NewUTCTime(at), mms.NewBitStringWithLength([]byte{0x80}, 1), mms.NewInteger(1),
	}
	ri, err := DecodeReport(values)
	if err != nil || !ri.Timestamp.Equal(at) {
		t.Fatalf("%+v, %v", ri, err)
	}
}

// TestDecodeReportErrors: every field of the wrong type, missing or nil,
// and values left over, are a *ReportError.
func TestDecodeReportErrors(t *testing.T) {
	str, num, flag := mms.NewVisibleString("x"), mms.NewUnsigned(1), mms.NewBoolean(true)
	incl := mms.NewBitStringWithLength([]byte{0x80}, 1) // one member
	opt := func(o OptFlds, rest ...*mms.Value) []*mms.Value {
		return append([]*mms.Value{mms.NewVisibleString("R"), encodeOptFlds(o)}, rest...)
	}
	for name, values := range map[string][]*mms.Value{
		"empty":             nil,
		"RptID nil":         {nil},
		"RptID type":        {num, encodeOptFlds(0)},
		"no OptFlds":        {str},
		"OptFlds type":      {str, num},
		"OptFlds short":     {str, mms.NewBitStringWithLength([]byte{0}, 8)},
		"SeqNum":            opt(OptFldSeqNum, str),
		"SeqNum missing":    opt(OptFldSeqNum),
		"TimeOfEntry":       opt(OptFldTimeStamp, str),
		"DatSet":            opt(OptFldDataSet, num),
		"BufOvfl":           opt(OptFldBufOvfl, num),
		"EntryID":           opt(OptFldEntryID, num),
		"ConfRev":           opt(OptFldConfRev, str),
		"SubSeqNum":         opt(OptFldSegmentation, str, flag),
		"MoreSegments":      opt(OptFldSegmentation, num, num),
		"MoreSegments miss": opt(OptFldSegmentation, num),
		"TimeOfEntry miss":  opt(OptFldTimeStamp),
		"DatSet missing":    opt(OptFldDataSet),
		"BufOvfl missing":   opt(OptFldBufOvfl),
		"EntryID missing":   opt(OptFldEntryID),
		"ConfRev missing":   opt(OptFldConfRev),
		"SubSeqNum missing": opt(OptFldSegmentation),
		"no Inclusion":      opt(0),
		"Inclusion type":    opt(0, num),
		"Inclusion bits":    opt(0, mms.NewBitStringWithLength([]byte{0x80}, 9)),
		"data ref type":     opt(OptFldDataRef, incl, num, num),
		"data ref missing":  opt(OptFldDataRef, incl),
		"value missing":     opt(0, incl),
		"value nil":         opt(0, incl, nil),
		"reason type":       opt(OptFldReasonCode, incl, num, num),
		"reason missing":    opt(OptFldReasonCode, incl, num),
		"left over":         opt(0, incl, num, num),
	} {
		ri, err := DecodeReport(values)
		var re *ReportError
		if !errors.As(err, &re) || ri != nil {
			t.Errorf("%s: %+v, %v", name, ri, err)
		}
	}
}

// FuzzDecodeReport: DecodeReport never panics and, when it succeeds, its
// lists agree with the inclusion bit string.
func FuzzDecodeReport(f *testing.F) {
	f.Add([]byte{0, 1, 'R', 4, 16, 0x7f, 0xc0, 2, 5})
	f.Add([]byte{0, 1, 'R', 4, 16, 0x08, 0x00, 4, 3, 0xa0, 0, 4, 'L', 'D', '/', 'A', 1, 1, 1, 2})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		ri, err := DecodeReport(fuzzValues(b))
		if err != nil {
			return
		}
		included := 0
		for _, in := range ri.Inclusion {
			if in {
				included++
			}
		}
		if len(ri.Values) != included ||
			(ri.OptFlds.Has(OptFldDataRef) && len(ri.DataReferences) != included) ||
			(ri.OptFlds.Has(OptFldReasonCode) && len(ri.ReasonCodes) != included) {
			t.Fatalf("%d included, %d values, %d references, %d reasons",
				included, len(ri.Values), len(ri.DataReferences), len(ri.ReasonCodes))
		}
	})
}

// fuzzValues builds MMS values from fuzz bytes: a kind byte, then a length
// or number byte, then for strings and bit strings their bytes.
func fuzzValues(b []byte) []*mms.Value {
	var out []*mms.Value
	take := func(n int) []byte {
		n = min(n, len(b))
		p := b[:n]
		b = b[n:]
		return p
	}
	for len(b) > 0 && len(out) < 64 {
		kind := take(1)[0]
		var n int
		if len(b) > 0 {
			n = int(take(1)[0])
		}
		switch kind % 8 {
		case 0:
			out = append(out, mms.NewVisibleString(string(take(n%32))))
		case 1:
			out = append(out, mms.NewInteger(int64(n)-128))
		case 2:
			out = append(out, mms.NewUnsigned(uint64(n)))
		case 3:
			out = append(out, mms.NewBoolean(n%2 == 1))
		case 4:
			bits := n % 80
			out = append(out, mms.NewBitStringWithLength(append([]byte(nil), take((bits+7)/8)...), bits))
		case 5:
			out = append(out, mms.NewOctetString(append([]byte(nil), take(n%16)...)))
		case 6:
			out = append(out, mms.NewBinaryTime(int64(n)))
		default:
			out = append(out, nil)
		}
	}
	return out
}
