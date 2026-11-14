// SPDX-License-Identifier: MIT

package iec61850

import (
	"fmt"
	"time"

	"github.com/otfabric/go-mms"
)

// DecodeReport decodes the access results of an MMS InformationReport that
// carries an IEC 61850 report (the report format of IEC 61850-8-1): RptID, OptFlds, the
// optional fields OptFlds announces in their standard order (SeqNum,
// TimeOfEntry, DatSet, BufOvfl, EntryID, ConfRev, SubSeqNum and
// MoreSegmentsFollow), the inclusion bit string, then per included member
// its data reference (with OptFldDataRef), its value, and its reason for
// inclusion (with OptFldReasonCode).
//
// It is for reports a caller did not receive through a subscription, such
// as those of a passive capture decoded with go-mms (mms.DecodeTSDU), and it
// is strict: a missing or nil value, a field of the wrong MMS type, an
// OptFlds or inclusion bit string that is not one, an inclusion bit string
// with more members than the values that follow can hold (checked before
// any member is decoded), and values left over after the last member are
// a *ReportError. It keeps no state and sends nothing. A Client decoding
// the reports it subscribed to stays lenient about field types, as before.
func DecodeReport(values []*mms.Value) (*ReportIndication, error) {
	ri := &ReportIndication{}
	idx := 0
	fail := func(format string, args ...any) error {
		return &ReportError{Message: fmt.Sprintf(format, args...)}
	}
	next := func(field string) (*mms.Value, error) {
		if idx >= len(values) {
			return nil, fail("%s is missing: the report has %d values", field, len(values))
		}
		v := values[idx]
		idx++
		if v == nil {
			return nil, fail("%s (value %d) is nil", field, idx)
		}
		return v, nil
	}
	wrong := func(field string, v *mms.Value) error {
		return fail("%s (value %d) is a %v", field, idx, v.Type())
	}

	v, err := next("RptID")
	if err != nil {
		return nil, err
	}
	var ok bool
	if ri.RptID, ok = v.VisibleString(); !ok {
		return nil, wrong("RptID", v)
	}
	if v, err = next("OptFlds"); err != nil {
		return nil, err
	}
	if ri.OptFlds, err = decodeOptFldsStrict(v); err != nil {
		return nil, fail("OptFlds (value %d): %v", idx, err)
	}

	if ri.OptFlds.Has(OptFldSeqNum) {
		if v, err = next("SeqNum"); err != nil {
			return nil, err
		}
		if ri.SeqNum, ok = v.Uint32(); !ok {
			return nil, wrong("SeqNum", v)
		}
	}
	if ri.OptFlds.Has(OptFldTimeStamp) {
		if v, err = next("TimeOfEntry"); err != nil {
			return nil, err
		}
		if t, ok := v.UTCTime(); ok {
			ri.Timestamp = t
		} else if ms, ok := v.BinaryTime(); ok {
			ri.Timestamp = time.UnixMilli(ms)
		} else {
			return nil, wrong("TimeOfEntry", v)
		}
	}
	if ri.OptFlds.Has(OptFldDataSet) {
		if v, err = next("DatSet"); err != nil {
			return nil, err
		}
		if ri.DatSet, ok = v.VisibleString(); !ok {
			return nil, wrong("DatSet", v)
		}
	}
	if ri.OptFlds.Has(OptFldBufOvfl) {
		if v, err = next("BufOvfl"); err != nil {
			return nil, err
		}
		if ri.BufOvfl, ok = v.Bool(); !ok {
			return nil, wrong("BufOvfl", v)
		}
	}
	if ri.OptFlds.Has(OptFldEntryID) {
		if v, err = next("EntryID"); err != nil {
			return nil, err
		}
		bs, ok := v.OctetString()
		if !ok {
			return nil, wrong("EntryID", v)
		}
		ri.EntryID = append([]byte(nil), bs...)
	}
	if ri.OptFlds.Has(OptFldConfRev) {
		if v, err = next("ConfRev"); err != nil {
			return nil, err
		}
		if ri.ConfRev, ok = v.Uint32(); !ok {
			return nil, wrong("ConfRev", v)
		}
	}
	if ri.OptFlds.Has(OptFldSegmentation) {
		if v, err = next("SubSeqNum"); err != nil {
			return nil, err
		}
		if ri.SubSeqNum, ok = v.Uint32(); !ok {
			return nil, wrong("SubSeqNum", v)
		}
		if v, err = next("MoreSegmentsFollow"); err != nil {
			return nil, err
		}
		if ri.MoreSegments, ok = v.Bool(); !ok {
			return nil, wrong("MoreSegmentsFollow", v)
		}
	}

	if v, err = next("Inclusion"); err != nil {
		return nil, err
	}
	data, ok := v.BitString()
	if !ok {
		return nil, wrong("Inclusion", v)
	}
	bitLen, _ := v.BitStringLength()
	if bitLen > len(data)*8 {
		return nil, fail("Inclusion (value %d) announces %d bits in %d bytes", idx, bitLen, len(data))
	}
	ri.Inclusion = make([]bool, bitLen)
	included := 0
	for i := range bitLen {
		if data[i/8]&(1<<uint(7-i%8)) != 0 {
			ri.Inclusion[i] = true
			included++
		}
	}
	if !membersFit(included, len(values)-idx, ri.OptFlds) {
		return nil, fail("Inclusion (value %d) announces %d members but %d values follow", idx, included, len(values)-idx)
	}

	if ri.OptFlds.Has(OptFldDataRef) {
		ri.DataReferences = make([]string, 0, included)
		for range included {
			if v, err = next("data reference"); err != nil {
				return nil, err
			}
			s, ok := v.VisibleString()
			if !ok {
				return nil, wrong("data reference", v)
			}
			ri.DataReferences = append(ri.DataReferences, s)
		}
	}
	ri.Values = make([]*Value, 0, included)
	for range included {
		if v, err = next("member value"); err != nil {
			return nil, err
		}
		ri.Values = append(ri.Values, NewValue(v))
	}
	if ri.OptFlds.Has(OptFldReasonCode) {
		ri.ReasonCodes = make([]ReasonCode, 0, included)
		for range included {
			if v, err = next("reason for inclusion"); err != nil {
				return nil, err
			}
			if _, ok := v.BitString(); !ok {
				return nil, wrong("reason for inclusion", v)
			}
			ri.ReasonCodes = append(ri.ReasonCodes, decodeReasonCode(v))
		}
	}
	if idx != len(values) {
		return nil, fail("%d values left over after the last member", len(values)-idx)
	}
	return ri, nil
}

// membersFit reports whether included members fit in the remaining report
// values: each member takes one value, one data reference more with
// OptFldDataRef and one reason code more with OptFldReasonCode. The
// decoders check it before they size the member lists, so that their size
// follows the values received.
func membersFit(included, remaining int, o OptFlds) bool {
	per := 1
	if o.Has(OptFldDataRef) {
		per++
	}
	if o.Has(OptFldReasonCode) {
		per++
	}
	return included <= remaining/per
}
