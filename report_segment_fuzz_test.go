// SPDX-License-Identifier: MIT

package iec61850

import (
	"io"
	"log/slog"
	"testing"

	"github.com/otfabric/go-mms"
)

// FuzzAssembleSegments feeds arbitrary segment sequences to the client
// reassembly (IEC 61850-8-1 §17.2.1, RPT-22): it must not panic, and an
// assembled report has as many values as inclusion bits.
func FuzzAssembleSegments(f *testing.F) {
	f.Add(uint8(3), []byte{0xC0, 0x20}, []byte{0, 1})
	f.Add(uint8(2), []byte{0x80, 0x80}, []byte{0, 1})
	f.Add(uint8(9), []byte{0xFF, 0x80, 0x00, 0x00}, []byte{0, 1, 1})
	f.Add(uint8(0), []byte{}, []byte{0})
	f.Fuzz(func(t *testing.T, width uint8, bits []byte, subs []byte) {
		if len(subs) > 16 {
			subs = subs[:16]
		}
		sub := &ReportSubscription{client: &Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
		bytesPer := (int(width) + 7) / 8
		for k, s := range subs {
			incl := make([]bool, width)
			n := 0
			for i := range incl {
				off := k*bytesPer + i/8
				if off < len(bits) && bits[off]&(0x80>>(i%8)) != 0 {
					incl[i] = true
					n++
				}
			}
			ri := &ReportIndication{
				RptID:        "r",
				OptFlds:      OptFldSeqNum | OptFldSegmentation | OptFldReasonCode | OptFldDataRef,
				SubSeqNum:    uint32(s),
				MoreSegments: k < len(subs)-1,
				Inclusion:    incl,
			}
			for range n {
				ri.Values = append(ri.Values, NewValue(mms.NewInteger(int64(k))))
				ri.DataReferences = append(ri.DataReferences, "ref")
				ri.ReasonCodes = append(ri.ReasonCodes, ReasonGI)
			}
			out := sub.handleSegment(ri)
			if out == nil {
				continue
			}
			c := 0
			for _, in := range out.Inclusion {
				if in {
					c++
				}
			}
			if c != len(out.Values) {
				t.Fatalf("assembled: %d inclusion bits, %d values", c, len(out.Values))
			}
		}
	})
}

// FuzzDecodeRCB decodes RCB structures whose trailing components
// (ResvTms, Owner, RPT-24) and lengths are arbitrary.
func FuzzDecodeRCB(f *testing.F) {
	f.Add(true, int64(-1), []byte{1, 2}, uint8(15))
	f.Add(false, int64(5), []byte{}, uint8(3))
	f.Add(true, int64(1<<40), []byte{0xff}, uint8(0))
	f.Fuzz(func(t *testing.T, buffered bool, resv int64, owner []byte, n uint8) {
		v := brcbValue(mms.NewInteger(resv), mms.NewUnsigned(uint64(resv)), mms.NewOctetString(owner), mms.NewBoolean(true))
		elems, _ := v.Structure()
		if int(n) < len(elems) {
			elems = elems[:n]
		}
		typ := RCBUnbuffered
		if buffered {
			typ = RCBBuffered
		}
		_, _ = decodeRCB("LD1", "LLN0$BR$x", typ, mms.NewStructure(elems))
	})
}
