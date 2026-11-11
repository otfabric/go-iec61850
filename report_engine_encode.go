// SPDX-License-Identifier: MIT

package iec61850

import (
	"slices"
	"time"

	"github.com/otfabric/go-mms"
)

// defaultReportMaxPDU is the report size limit when the MMS server has no
// MaxPDUSize configured (the go-mms default).
const defaultReportMaxPDU = 65000

// reportPDUOverhead bounds the MMS framing around the listOfAccessResult
// of an InformationReport: unconfirmed-PDU [3], informationReport [0],
// variableListName [1] vmd-specific "RPT" and listOfAccessResult [0],
// each with a tag and up to a 5-octet length.
const reportPDUOverhead = 32

// reportParams holds all parameters needed to encode a report.
type reportParams struct {
	rptID   string
	optFlds OptFlds
	seqNum  uint32
	confRev uint32
	datSet  string
	bufOvfl bool
	entryID []byte // nil for URCB
	// timeOfEntry is the creation time of the entry (IEC 61850-7-2
	// §17.2.3.2.2.9); zero means now.
	timeOfEntry time.Time
	// subSeqNum and moreSegments are encoded when optFlds has the
	// segmentation bit (IEC 61850-8-1 §17.2.1 Table 64).
	subSeqNum    uint32
	moreSegments bool
	inclusion    []bool
	// dataRefs are the data references of the data set members, in
	// order: "<LD>/<LN>$<FC>$<DO>[$<DA>...]" (IEC 61850-8-1 report
	// format, the MMS form of the member reference).
	dataRefs []string
	reasons  []ReasonCode
	values   []*mms.Value
}

// encodeReportValues builds the flat MMS value list for an IEC 61850
// InformationReport following the standard field order:
//
//	RptID, OptFlds, [SeqNum], [TimeStamp], [DatSet], [BufOvfl],
//	[EntryID], [ConfRev], SubSeqNum, MoreSegments, Inclusion,
//	[DataRef...], Values..., [ReasonCode...]
func encodeReportValues(
	rptID string,
	optFlds OptFlds,
	seqNum, confRev uint32,
	datSet string,
	inclusion []bool,
	reasons []ReasonCode,
	values []*mms.Value,
) []*mms.Value {
	return encodeReportValuesEx(reportParams{
		rptID:     rptID,
		optFlds:   optFlds,
		seqNum:    seqNum,
		confRev:   confRev,
		datSet:    datSet,
		inclusion: inclusion,
		reasons:   reasons,
		values:    values,
	})
}

// encodeReportValuesEx encodes one InformationReport (or one segment of
// it) in the IEC 61850-8-1 Table 64 order.
func encodeReportValuesEx(p reportParams) []*mms.Value {
	// A member without a value cannot be encoded with its data set type
	// (IEC 61850-8-1 §17.2.1 "AccessResult for value(s)"): it is left out
	// of the inclusion bitstring rather than sent as a substitute.
	for i, in := range p.inclusion {
		if in && (i >= len(p.values) || p.values[i] == nil) {
			p.inclusion = slices.Clone(p.inclusion)
			for k := i; k < len(p.inclusion); k++ {
				if p.inclusion[k] && (k >= len(p.values) || p.values[k] == nil) {
					p.inclusion[k] = false
				}
			}
			break
		}
	}

	var result []*mms.Value

	result = append(result, mms.NewVisibleString(p.rptID))
	result = append(result, encodeOptFlds(p.optFlds))

	if p.optFlds.Has(OptFldSeqNum) {
		result = append(result, mms.NewUnsigned(uint64(p.seqNum)))
	}

	if p.optFlds.Has(OptFldTimeStamp) {
		t := p.timeOfEntry
		if t.IsZero() {
			t = time.Now()
		}
		result = append(result, mms.NewBinaryTime(t.UTC().UnixMilli()))
	}

	if p.optFlds.Has(OptFldDataSet) {
		result = append(result, mms.NewVisibleString(p.datSet))
	}

	if p.optFlds.Has(OptFldBufOvfl) {
		result = append(result, mms.NewBoolean(p.bufOvfl))
	}

	if p.optFlds.Has(OptFldEntryID) {
		eid := p.entryID
		if eid == nil {
			eid = make([]byte, 8)
		}
		result = append(result, mms.NewOctetString(eid))
	}

	if p.optFlds.Has(OptFldConfRev) {
		result = append(result, mms.NewUnsigned(uint64(p.confRev)))
	}

	// SubSeqNum + MoreSegmentsFollow, present when the segmentation bit
	// is set (IEC 61850-8-1 §17.2.1).
	if p.optFlds.Has(OptFldSegmentation) {
		result = append(result, mms.NewUnsigned(uint64(p.subSeqNum)))
		result = append(result, mms.NewBoolean(p.moreSegments))
	}

	result = append(result, encodeInclusion(p.inclusion))

	// One data reference per included member, between the inclusion
	// bitstring and the values, when OptFlds asks for them.
	if p.optFlds.Has(OptFldDataRef) {
		for i, incl := range p.inclusion {
			if !incl {
				continue
			}
			ref := ""
			if i < len(p.dataRefs) {
				ref = p.dataRefs[i]
			}
			result = append(result, mms.NewVisibleString(ref))
		}
	}

	for i, incl := range p.inclusion {
		if incl {
			result = append(result, p.values[i])
		}
	}

	if p.optFlds.Has(OptFldReasonCode) {
		for i, incl := range p.inclusion {
			if incl && i < len(p.reasons) {
				result = append(result, encodeReasonCode(p.reasons[i]))
			}
		}
	}

	return result
}

// formatReport encodes p as one InformationReport when it fits maxPDU,
// otherwise as segments: "For the case of long reports that do not fit
// into one message, a single report shall be divided into subreports.
// Each segment – of one report – shall be numbered with the same sequence
// number and a unique SubSqNum" starting at 0 (IEC 61850-7-2
// §17.2.3.2.2.5). Each segment carries the full-width inclusion bitstring
// with the bits of its own members (IEC 61850-8-1 §17.2.1), the
// segmentation bit in the reported OptFlds and MoreSegmentsFollow TRUE
// on all but the last. It returns nil when a single member does not fit.
func formatReport(p reportParams, maxPDU int) [][]*mms.Value {
	budget := maxPDU - reportPDUOverhead
	p.optFlds &^= OptFldSegmentation
	whole := encodeReportValuesEx(p)
	if valuesLen(whole) <= budget {
		return [][]*mms.Value{whole}
	}

	// Fixed part of a segment: the header with segmentation fields and
	// an inclusion bitstring with no member.
	seg := p
	seg.optFlds |= OptFldSegmentation
	seg.subSeqNum = 1 << 16 // upper bound of the SubSeqNum encoding
	seg.moreSegments = true
	seg.inclusion = make([]bool, len(p.inclusion))
	fixed := valuesLen(encodeReportValuesEx(seg))
	room := budget - fixed

	var groups [][]int
	var cur []int
	used := 0
	for i, in := range p.inclusion {
		if !in {
			continue
		}
		cost := memberCost(p, i)
		if cost > room {
			return nil
		}
		if used+cost > room && len(cur) > 0 {
			groups = append(groups, cur)
			cur, used = nil, 0
		}
		cur = append(cur, i)
		used += cost
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}

	out := make([][]*mms.Value, len(groups))
	for g, members := range groups {
		s := p
		s.optFlds |= OptFldSegmentation
		s.subSeqNum = uint32(g)
		s.moreSegments = g < len(groups)-1
		s.inclusion = make([]bool, len(p.inclusion))
		for _, i := range members {
			s.inclusion[i] = true
		}
		out[g] = encodeReportValuesEx(s)
	}
	return out
}

// memberCost is the encoded size of member i in a report: its data
// reference, value and reason code as requested by OptFlds.
func memberCost(p reportParams, i int) int {
	n := dataLen(p.values[i])
	if p.optFlds.Has(OptFldDataRef) {
		ref := ""
		if i < len(p.dataRefs) {
			ref = p.dataRefs[i]
		}
		n += tlvLen(len(ref))
	}
	if p.optFlds.Has(OptFldReasonCode) {
		n += tlvLen(2)
	}
	return n
}

// valuesLen is an upper bound of the encoded size of an AccessResult list.
func valuesLen(vals []*mms.Value) int {
	n := 0
	for _, v := range vals {
		n += dataLen(v)
	}
	return n
}

// berLenLen is the size of a BER definite length.
func berLenLen(n int) int {
	if n < 0x80 {
		return 1
	}
	c := 1
	for ; n > 0; n >>= 8 {
		c++
	}
	return c
}

// tlvLen is the size of a BER TLV with a one-octet tag.
func tlvLen(content int) int { return 1 + berLenLen(content) + content }

// dataLen is an upper bound of the BER size of v as MMS Data (ISO 9506-2
// Data CHOICE); integers and floats are counted at their largest form.
func dataLen(v *mms.Value) int {
	if v == nil {
		return 2
	}
	switch v.Type() {
	case mms.ValueTypeBoolean:
		return 3
	case mms.ValueTypeInteger, mms.ValueTypeUnsigned, mms.ValueTypeFloat:
		return tlvLen(9)
	case mms.ValueTypeBitString:
		b, _ := v.BitString()
		return tlvLen(1 + len(b))
	case mms.ValueTypeOctetString:
		b, _ := v.OctetString()
		return tlvLen(len(b))
	case mms.ValueTypeVisibleString:
		s, _ := v.VisibleString()
		return tlvLen(len(s))
	case mms.ValueTypeMmsString:
		s, _ := v.MmsString()
		return tlvLen(len(s))
	case mms.ValueTypeBinaryTime:
		return tlvLen(6)
	case mms.ValueTypeUTCTime:
		return tlvLen(8)
	case mms.ValueTypeStructure:
		elems, _ := v.Structure()
		return tlvLen(valuesLen(elems))
	case mms.ValueTypeArray:
		elems, _ := v.ArrayElements()
		return tlvLen(valuesLen(elems))
	default:
		return tlvLen(16)
	}
}

// encodeInclusion encodes the inclusion bitmap as an MMS bitstring.
func encodeInclusion(inclusion []bool) *mms.Value {
	bitLen := len(inclusion)
	byteLen := (bitLen + 7) / 8
	bits := make([]byte, byteLen)
	for i, incl := range inclusion {
		if incl {
			bits[i/8] |= 0x80 >> (uint(i) % 8)
		}
	}
	return mms.NewBitStringWithLength(bits, bitLen)
}

// encodeReasonCode encodes a ReasonCode as a 7-bit bitstring
// (IEC 61850-8-1 §8.1.3.10: bit 0 reserved, 1 data-change, 2
// quality-change, 3 data-update, 4 integrity, 5 general-interrogation, 6
// application-trigger).
func encodeReasonCode(r ReasonCode) *mms.Value {
	b := byte(0)
	if r&ReasonDataChanged != 0 {
		b |= 0x40
	}
	if r&ReasonQualityChanged != 0 {
		b |= 0x20
	}
	if r&ReasonDataUpdate != 0 {
		b |= 0x10
	}
	if r&ReasonIntegrity != 0 {
		b |= 0x08
	}
	if r&ReasonGI != 0 {
		b |= 0x04
	}
	if r&ReasonApplicationTrigger != 0 {
		b |= 0x02
	}
	return mms.NewBitStringWithLength([]byte{b}, 7)
}
