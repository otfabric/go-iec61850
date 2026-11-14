// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
)

func FuzzParseRef(f *testing.F) {
	f.Add("LD1/LLN0.Mod.stVal[ST]")
	f.Add("simpleIOGenericIO/GGIO1.Ind1.stVal[ST]")
	f.Add("LD1/LLN0")
	f.Add("LD1")
	f.Add("")
	f.Add("a/b.c.d.e.f.g.h[MX]")
	f.Add("LD/LN.DO$DA[ST]")
	f.Add("/LN.DO[ST]")
	f.Add("LD/")
	f.Add("LD/LN.DO[INVALID]")
	f.Add("LD/LN.DO.DA.Sub1.Sub2.Sub3[CF]")

	f.Fuzz(func(t *testing.T, input string) {
		ref, err := ParseRef(input)
		if err != nil {
			return
		}
		s := ref.String()
		if s == "" {
			t.Error("ParseRef succeeded but String() returned empty")
		}
		_ = ref.Validate()
		_ = ref.IsLD()
		_ = ref.IsLN()
		_ = ref.IsObject()
		_ = ref.HasPath()
		_ = ref.Depth()
		_, _ = ref.Parent()
		_, _, _ = ref.ToMMS()
	})
}

func FuzzParseFC(f *testing.F) {
	for _, fc := range AllFunctionalConstraints() {
		f.Add(string(fc))
	}
	f.Add("")
	f.Add("XX")
	f.Add("st")
	f.Add("STATUS")
	f.Add("S")

	f.Fuzz(func(t *testing.T, input string) {
		fc, err := ParseFC(input)
		if err != nil {
			return
		}
		if !fc.IsValid() {
			t.Errorf("ParseFC(%q) returned %q but IsValid() is false", input, fc)
		}
		_ = fc.Description()
		_ = fc.String()
	})
}

func FuzzDecodeQuality(f *testing.F) {
	f.Add([]byte{0x00, 0x00})
	f.Add([]byte{0xFF, 0xFF})
	f.Add([]byte{0x00, 0x04})
	f.Add([]byte{0x80, 0x00})
	f.Add([]byte{0x00})
	f.Add([]byte{})

	// Every bit string decodes; bit i of the quality is bit i of the
	// string (MSB first) for the first 13 conveyed bits and clear beyond
	// them; the quality re-encodes to itself.
	f.Fuzz(func(t *testing.T, data []byte) {
		lengths := []int{len(data) * 8}
		if len(data) > 0 {
			lengths = append(lengths, len(data)*8-int(data[0]%8))
		}
		for _, bitLen := range lengths {
			q, err := DecodeQuality(mms.NewBitStringWithLength(data, bitLen))
			if err != nil {
				t.Fatalf("bit string of %d bits: %v", bitLen, err)
			}
			for i := range 16 {
				want := i < qualityBitLength && i < bitLen && data[i/8]&(0x80>>(i%8)) != 0
				if got := q&(1<<i) != 0; got != want {
					t.Fatalf("% x (%d bits): quality bit %d = %v, want %v", data, bitLen, i, got, want)
				}
			}
			back, err := DecodeQuality(EncodeQuality(q))
			if err != nil || back != q {
				t.Fatalf("EncodeQuality(%#x) decodes to %#x, %v", uint16(q), uint16(back), err)
			}
			_ = q.String()
			_ = q.Validity()
			_ = q.IsGood()
		}
	})
}

func FuzzNewValue(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(-1))
	f.Add(int64(42))
	f.Add(int64(1<<31 - 1))
	f.Add(int64(-1 << 31))

	f.Fuzz(func(t *testing.T, i int64) {
		v := NewValue(mms.NewInteger(i))
		if v == nil {
			t.Fatal("NewValue returned nil for non-nil input")
		}
		_ = v.String()
		_ = v.Type()

		got, err := v.Int64()
		if err != nil {
			t.Fatalf("Int64 error for integer value: %v", err)
		}
		if got != i {
			t.Fatalf("Int64 = %d, want %d", got, i)
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// FuzzDecodeReportIndication
// ────────────────────────────────────────────────────────────────────────────

// FuzzDecodeReportIndication exercises the report decoder with arbitrary
// []*mms.Value slices. The invariant is simply: the function must not panic.
//
// Covers:
//   - empty / nil slices
//   - truncated sequences (missing RptID, missing OptFlds, etc.)
//   - wrong value types for each field
//   - invalid bit-string lengths for OptFlds / TrgOps / inclusion
//   - oversized inclusion bit-strings
func FuzzDecodeReportIndication(f *testing.F) {
	logger := slog.Default()

	makeReport := func(rptID string, optFldsBits []byte, seqNum uint32,
		subSeqNum uint32, confRev uint32, trgOps []byte, entryID []byte,
		inclBits []byte, vals []byte,
	) []*mms.Value {
		var v []*mms.Value
		v = append(v, mms.NewVisibleString(rptID))
		v = append(v, mms.NewBitStringWithLength(optFldsBits, 10))
		v = append(v, mms.NewUnsigned(uint64(seqNum)))
		v = append(v, mms.NewOctetString(entryID))
		v = append(v, mms.NewUnsigned(uint64(confRev)))
		v = append(v, mms.NewBitStringWithLength(trgOps, 6))
		v = append(v, mms.NewUnsigned(uint64(subSeqNum)))
		v = append(v, mms.NewBoolean(subSeqNum > 0))
		var inclLen int
		if len(inclBits) > 0 {
			inclLen = len(inclBits) * 8
		}
		v = append(v, mms.NewBitStringWithLength(inclBits, inclLen))
		for _, b := range vals {
			v = append(v, mms.NewInteger(int64(b)))
		}
		return v
	}

	// Seed: minimal valid report with one dataset member.
	f.Add("rpt01", []byte{0x3c, 0x00}, uint32(1), uint32(0), uint32(1),
		[]byte{0x30}, []byte{0, 0, 0, 0, 0, 0, 0, 1},
		[]byte{0x80},
		[]byte{0x42},
	)
	// Seed: empty values.
	f.Add("", []byte{}, uint32(0), uint32(0), uint32(0),
		[]byte{}, []byte{}, []byte{}, []byte{},
	)
	// Seed: all-zeros optional fields.
	f.Add("test", []byte{0x00, 0x00}, uint32(0), uint32(0), uint32(1),
		[]byte{0x00}, []byte{0, 0, 0, 0, 0, 0, 0, 0},
		[]byte{0x00}, []byte{},
	)
	// Seed: all-ones optional fields, oversized inclusion.
	f.Add("bigopt", []byte{0xff, 0xff}, uint32(0xffffffff), uint32(0xff), uint32(0),
		[]byte{0xff}, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		[]byte{0xff, 0xff, 0xff, 0xff},
		[]byte{0x01, 0x02, 0x03},
	)

	f.Fuzz(func(t *testing.T, rptID string, optFldsBits []byte, seqNum, subSeqNum, confRev uint32,
		trgOps, entryID, inclBits, vals []byte,
	) {
		values := makeReport(rptID, optFldsBits, seqNum, subSeqNum, confRev, trgOps, entryID, inclBits, vals)
		ri, err := decodeReportIndication(values, logger)
		checkReportIndication(t, values, ri, err)
		if err == nil && ri.RptID != rptID {
			t.Fatalf("RptID %q, want %q", ri.RptID, rptID)
		}
	})
}

// checkReportIndication asserts what holds for any decode of values: an
// error is a *ReportError, and an accepted report has one value per set
// inclusion bit, one data reference and one reason code per value when
// its OptFlds announce them, and no more members than values received.
func checkReportIndication(t *testing.T, values []*mms.Value, ri *ReportIndication, err error) {
	t.Helper()
	if err != nil {
		var re *ReportError
		if !errors.As(err, &re) || ri != nil {
			t.Fatalf("error %T %v with report %v: want a *ReportError and no report", err, err, ri != nil)
		}
		return
	}
	members := 0
	for _, in := range ri.Inclusion {
		if in {
			members++
		}
	}
	if len(ri.Values) != members || members > len(values) {
		t.Fatalf("%d inclusion bits set, %d values decoded, %d values received", members, len(ri.Values), len(values))
	}
	if ri.OptFlds.Has(OptFldDataRef) != (ri.DataReferences != nil) || (ri.DataReferences != nil && len(ri.DataReferences) != members) {
		t.Fatalf("OptFlds %v: %d data references for %d members", ri.OptFlds, len(ri.DataReferences), members)
	}
	if ri.OptFlds.Has(OptFldReasonCode) != (ri.ReasonCodes != nil) || (ri.ReasonCodes != nil && len(ri.ReasonCodes) != members) {
		t.Fatalf("OptFlds %v: %d reason codes for %d members", ri.OptFlds, len(ri.ReasonCodes), members)
	}
	for i, v := range ri.Values {
		if v == nil {
			t.Fatalf("value %d is nil", i)
		}
	}
}

// FuzzDecodeOptFlds exercises the OptFlds bit-string decoder with arbitrary bytes.
func FuzzDecodeOptFlds(f *testing.F) {
	f.Add([]byte{0x3c, 0x00})
	f.Add([]byte{0xff, 0xff})
	f.Add([]byte{0x00, 0x00})
	f.Add([]byte{})
	f.Add([]byte{0x01})
	f.Add([]byte{0x00, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		// The strict decoder refuses fewer than 10 bits; an accepted value
		// has only the nine OptFlds bits (wire bits 1..9), equals what the
		// lenient decoder returns, and re-encodes to itself.
		v := mms.NewBitString(data)
		opts := decodeOptFlds(v)
		for bit := OptFlds(1); bit < 1<<15; bit <<= 1 {
			_ = opts.Has(bit)
		}
		strict, err := decodeOptFldsStrict(v)
		if (err != nil) != (len(data) < 2) {
			t.Fatalf("% x: strict error %v", data, err)
		}
		if err != nil {
			if opts != 0 {
				t.Fatalf("% x: lenient %v after a strict error", data, opts)
			}
			return
		}
		if strict != opts || strict >= 1<<9 {
			t.Fatalf("% x: strict %#x, lenient %#x", data, uint16(strict), uint16(opts))
		}
		for wire := 1; wire <= 9; wire++ {
			if got, want := strict&(1<<(wire-1)) != 0, data[wire/8]&(0x80>>(wire%8)) != 0; got != want {
				t.Fatalf("% x: wire bit %d decoded %v, want %v", data, wire, got, want)
			}
		}
		if back := decodeOptFlds(encodeOptFlds(strict)); back != strict {
			t.Fatalf("encodeOptFlds(%#x) decodes to %#x", uint16(strict), uint16(back))
		}
	})
}

// FuzzDecodeTrgOps exercises the TrgOps bit-string decoder.
func FuzzDecodeTrgOps(f *testing.F) {
	f.Add([]byte{0x30})
	f.Add([]byte{0xff})
	f.Add([]byte{0x00})
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x02})

	f.Fuzz(func(t *testing.T, data []byte) {
		// The strict decoder refuses an empty string; an accepted value
		// has only the five trigger bits (wire bits 1..5), equals what the
		// lenient decoder returns, and re-encodes to itself.
		v := mms.NewBitString(data)
		ops := decodeTrgOps(v)
		_ = ops.Has(TrgOpDataChanged)
		_ = ops.Has(TrgOpQualityChanged)
		_ = ops.Has(TrgOpDataUpdate)
		_ = ops.Has(TrgOpIntegrity)
		_ = ops.Has(TrgOpGI)
		strict, err := decodeTrgOpsStrict(v)
		if (err != nil) != (len(data) == 0) {
			t.Fatalf("% x: strict error %v", data, err)
		}
		if err != nil {
			if ops != 0 {
				t.Fatalf("% x: lenient %v after a strict error", data, ops)
			}
			return
		}
		if strict != ops || strict >= 1<<5 {
			t.Fatalf("% x: strict %#x, lenient %#x", data, uint8(strict), uint8(ops))
		}
		for wire := 1; wire <= 5; wire++ {
			if got, want := strict&(1<<(wire-1)) != 0, data[0]&(0x80>>wire) != 0; got != want {
				t.Fatalf("% x: wire bit %d decoded %v, want %v", data, wire, got, want)
			}
		}
		if back := decodeTrgOps(encodeTrgOps(strict)); back != strict {
			t.Fatalf("encodeTrgOps(%#x) decodes to %#x", uint8(strict), uint8(back))
		}
	})
}

// FuzzDecodeReportIndication_NilValues exercises the decoder when values
// contain nil entries (must not panic).
func FuzzDecodeReportIndication_NilValues(f *testing.F) {
	logger := slog.Default()

	f.Add(uint8(0), uint8(3), uint8(7))

	f.Fuzz(func(t *testing.T, nilMask1, nilMask2, nilMask3 uint8) {
		values := []*mms.Value{
			mms.NewVisibleString("rpt01"),
			mms.NewBitStringWithLength([]byte{0x3c, 0x00}, 10),
			mms.NewUnsigned(1),
			mms.NewOctetString([]byte{0, 0, 0, 0, 0, 0, 0, 1}),
			mms.NewUnsigned(1),
			mms.NewBitStringWithLength([]byte{0x30}, 6),
			mms.NewUnsigned(0),
			mms.NewBoolean(false),
			mms.NewBitStringWithLength([]byte{0x80}, 8),
			mms.NewInteger(42),
		}
		for i, m := range []uint8{nilMask1, nilMask2, nilMask3} {
			base := i * 3
			for bit := range uint8(3) {
				idx := base + int(bit)
				if idx < len(values) && (m>>bit)&1 == 1 {
					values[idx] = nil
				}
			}
		}
		ri, err := decodeReportIndication(values, logger)
		checkReportIndication(t, values, ri, err)
		// RptID, OptFlds and the one member's value are required.
		if (values[0] == nil || values[1] == nil || values[9] == nil) && err == nil {
			t.Fatalf("nil mask %03b %03b %03b: report accepted without RptID, OptFlds or its value", nilMask1, nilMask2, nilMask3)
		}
	})
}

// FuzzDecodeGoCB feeds arbitrary MMS Data (BER, decoded with the GOOSE
// allData decoder) to the GoCB mapper.
func FuzzDecodeGoCB(f *testing.F) {
	seed := mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true), mms.NewVisibleString("id"), mms.NewVisibleString("LD/LLN0$ds"), mms.NewUnsigned(1), mms.NewBoolean(false),
		mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewUnsigned(4), mms.NewUnsigned(0), mms.NewUnsigned(1)}),
		mms.NewUnsigned(10), mms.NewUnsigned(1000), mms.NewBoolean(false),
	})
	b, err := goose.EncodeData(seed)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := goose.DecodeData(b)
		if err != nil {
			return
		}
		cb, err := decodeGoCB("LD/LLN0$GO$g", v)
		if err == nil && cb == nil {
			t.Fatal("nil GoCB without error")
		}
		if err != nil && !errors.Is(err, ErrProtocol) {
			t.Fatalf("error does not wrap ErrProtocol: %v", err)
		}
	})
}
