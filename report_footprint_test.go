// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/otfabric/go-mms"
)

// The fixed part of the estimate covers the real size of a decoded value:
// the mms.Value, the Value wrapper and the pointers to both.
func TestValueFootprint_UpperBound(t *testing.T) {
	var p *Value
	size := unsafe.Sizeof(mms.Value{}) + unsafe.Sizeof(Value{}) + 2*unsafe.Sizeof(p)
	if size > valueFootprintBase {
		t.Fatalf("a decoded value takes %d bytes, the estimate counts %d", size, valueFootprintBase)
	}
	arcs := make([]int, 1000)
	cases := []struct {
		name string
		v    *mms.Value
		min  int
	}{
		{"object identifier", mms.NewObjectIdentifier(arcs), 1000 * int(unsafe.Sizeof(arcs[0]))},
		{"boolean array", mms.NewBooleanArray(make([]byte, 1000), 8000), 1000},
		{"bit string", mms.NewBitStringWithLength(make([]byte, 1000), 8000), 1000},
		{"octet string", mms.NewOctetString(make([]byte, 1000)), 1000},
		{"visible string", mms.NewVisibleString(string(make([]byte, 1000))), 1000},
		{"boolean", mms.NewBoolean(true), 0},
	}
	for _, tc := range cases {
		if got := valueFootprint(tc.v, 0); got < valueFootprintBase+tc.min {
			t.Errorf("%s: estimate %d, want >= %d", tc.name, got, valueFootprintBase+tc.min)
		}
	}
	if got := valueFootprint(nil, 0); got != 0 {
		t.Errorf("nil: estimate %d, want 0", got)
	}
}

// Segments carrying object identifiers or boolean arrays count their
// decoded content against MaxSegmentBytes: a peer sending large ones
// reaches the budget and the partial report is dropped.
func TestReportSegments_VariableLengthValuesCounted(t *testing.T) {
	const arcs = 7000 // a 7000-octet OID on the wire, 56000 bytes decoded
	cases := []struct {
		name string
		v    func() *mms.Value
	}{
		{"object identifier", func() *mms.Value { return mms.NewObjectIdentifier(make([]int, arcs)) }},
		{"boolean array", func() *mms.Value { return mms.NewBooleanArray(make([]byte, 8*arcs), 64*arcs) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := segmentSubscription(1<<20, 0)
			sent := 0
			for i := range uint32(64) {
				ri := segmentOf(5, i, true, 4, 0)
				ri.Values = []*Value{NewValue(tc.v())}
				s.handleSegment(ri)
				sent++
				if s.Stats().PartialReportsDropped == 1 {
					break
				}
			}
			if got := s.Stats().PartialReportsDropped; got != 1 {
				t.Fatalf("PartialReportsDropped = %d after %d segments of 56000 decoded bytes, want 1", got, sent)
			}
			if sent > 1<<20/(8*arcs)+1 {
				t.Errorf("dropped after %d segments, want <= %d", sent, 1<<20/(8*arcs)+1)
			}
			if n := buffered(s); n != 0 {
				t.Errorf("%d segments still buffered after the drop", n)
			}
		})
	}
}

// An inclusion bit string that announces more bits than its octets hold
// is refused by the client's decoder, as by DecodeReport.
func TestDecodeReportIndication_InclusionLongerThanData(t *testing.T) {
	for _, bits := range []int{9, 64, -1} {
		values := membersReport(0, 1, 1, 1)
		values[2] = mms.NewBitStringWithLength([]byte{0x80}, bits)
		var re *ReportError
		if _, err := decodeReportIndication(values, nil); !errors.As(err, &re) {
			t.Errorf("%d bits in 1 octet: err = %v, want a *ReportError", bits, err)
		}
	}
	values := membersReport(0, 8, 8, 8)
	if ri, err := decodeReportIndication(values, nil); err != nil || len(ri.Inclusion) != 8 {
		t.Fatalf("8 bits in 1 octet: err %v", err)
	}
}
