// SPDX-License-Identifier: MIT

package iec61850

import (
	"log/slog"
	"testing"

	"github.com/otfabric/go-mms"
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

	f.Fuzz(func(t *testing.T, data []byte) {
		v := mms.NewBitString(data)
		q, err := DecodeQuality(v)
		if err != nil {
			return
		}
		_ = q.String()
		_ = q.Validity()
		_ = q.IsGood()
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
		_, _ = decodeReportIndication(values, logger)
	})
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
		v := mms.NewBitString(data)
		opts := decodeOptFlds(v)
		for bit := OptFlds(1); bit < 1<<15; bit <<= 1 {
			_ = opts.Has(bit)
		}
		_, _ = decodeOptFldsStrict(v)
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
		v := mms.NewBitString(data)
		ops := decodeTrgOps(v)
		_ = ops.Has(TrgOpDataChanged)
		_ = ops.Has(TrgOpQualityChanged)
		_ = ops.Has(TrgOpDataUpdate)
		_ = ops.Has(TrgOpIntegrity)
		_ = ops.Has(TrgOpGI)
		_, _ = decodeTrgOpsStrict(v)
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
			for bit := uint8(0); bit < 3; bit++ {
				idx := base + int(bit)
				if idx < len(values) && (m>>bit)&1 == 1 {
					values[idx] = nil
				}
			}
		}
		_, _ = decodeReportIndication(values, logger)
	})
}
