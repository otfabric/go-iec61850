// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/otfabric/go-mms"
)

// memberLists are the OptFlds combinations that change how many values a
// member takes: one value, one data reference more with OptFldDataRef and
// one reason code more with OptFldReasonCode.
var memberLists = []OptFlds{0, OptFldDataRef, OptFldReasonCode, OptFldDataRef | OptFldReasonCode}

// membersReport builds report values whose inclusion bit string has bits
// bits, all set, followed by the member lists that OptFlds o announces for
// members members, cut after their first follow values.
func membersReport(o OptFlds, bits, members, follow int) []*mms.Value {
	incl := make([]byte, (bits+7)/8)
	for i := range bits {
		incl[i/8] |= 1 << uint(7-i%8)
	}
	values := []*mms.Value{mms.NewVisibleString("R"), encodeOptFlds(o), mms.NewBitStringWithLength(incl, bits)}
	var lists []*mms.Value
	for i := range members {
		if o.Has(OptFldDataRef) {
			lists = append(lists, mms.NewVisibleString(fmt.Sprintf("LD0/GGIO1$ST$Ind%d", i)))
		}
	}
	for range members {
		lists = append(lists, mms.NewBoolean(true))
	}
	for range members {
		if o.Has(OptFldReasonCode) {
			lists = append(lists, mms.NewBitStringWithLength([]byte{0x40}, 7))
		}
	}
	return append(values, lists[:follow]...)
}

// perMember is the number of values one member takes with OptFlds o.
func perMember(o OptFlds) int {
	n := 1
	if o.Has(OptFldDataRef) {
		n++
	}
	if o.Has(OptFldReasonCode) {
		n++
	}
	return n
}

// TestDecodeReport_MemberCountBoundary: a report whose values hold every
// member decodes; one value fewer, or one member more than the values
// hold, is a *ReportError, in DecodeReport and in the client's decoder.
func TestDecodeReport_MemberCountBoundary(t *testing.T) {
	const members = 3
	for _, o := range memberLists {
		per := perMember(o)
		for _, tc := range []struct {
			name         string
			bits, follow int
			ok           bool
		}{
			{"every member present", members, members * per, true},
			{"one value fewer", members, members*per - 1, false},
			{"one member more than the values hold", members + 1, members * per, false},
			{"no member, no value", 0, 0, true},
			{"one member, no value", 1, 0, false},
		} {
			values := membersReport(o, tc.bits, members, tc.follow)
			strict, serr := DecodeReport(values)
			lenient, lerr := decodeReportIndication(values, nil)
			for decoder, err := range map[string]error{"DecodeReport": serr, "client decoder": lerr} {
				if tc.ok && err != nil {
					t.Errorf("OptFlds %v, %s: %s: %v", o, tc.name, decoder, err)
				}
				var re *ReportError
				if !tc.ok && !errors.As(err, &re) {
					t.Errorf("OptFlds %v, %s: %s: err = %v, want a *ReportError", o, tc.name, decoder, err)
				}
			}
			if tc.ok && (len(strict.Values) != tc.bits || len(lenient.Values) != tc.bits) {
				t.Errorf("OptFlds %v, %s: %d and %d values, want %d", o, tc.name, len(strict.Values), len(lenient.Values), tc.bits)
			}
		}
	}
}

// TestDecodeReport_MemberListsSizedByValuesReceived: the member lists are
// sized by the values received. A bit string that announces more members
// than the values that follow can hold is rejected before the lists are
// sized, so a decode allocates about the inclusion bit string itself (one
// bool per bit) and not a list entry per announced member.
func TestDecodeReport_MemberListsSizedByValuesReceived(t *testing.T) {
	const bits = 64000 // 8000 octets, every bit set, no member follows
	// The Inclusion slice takes one byte per bit; the rest is a few small
	// allocations. One pointer per announced member would be 8 bytes each.
	const budget = bits + 32<<10
	for _, o := range memberLists {
		values := membersReport(o, bits, 0, 0)
		for decoder, decode := range map[string]func() error{
			"DecodeReport": func() error { _, err := DecodeReport(values); return err },
			"client decoder": func() error {
				_, err := decodeReportIndication(values, nil)
				return err
			},
		} {
			var re *ReportError
			if err := decode(); !errors.As(err, &re) {
				t.Fatalf("OptFlds %v: %s: err = %v, want a *ReportError", o, decoder, err)
			}
			if got := allocatedBytes(func() { _ = decode() }); got > budget {
				t.Errorf("OptFlds %v: %s allocated %d bytes for a bit string of %d bits and no member, budget %d",
					o, decoder, got, bits, budget)
			}
		}
	}
}

// allocatedBytes is the smallest number of bytes f allocated in five runs.
// The test that uses it does not run in parallel.
func allocatedBytes(f func()) uint64 {
	best := uint64(math.MaxUint64)
	for range 5 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		f()
		runtime.ReadMemStats(&after)
		best = min(best, after.TotalAlloc-before.TotalAlloc)
	}
	return best
}
