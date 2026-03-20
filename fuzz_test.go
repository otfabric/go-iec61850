package iec61850

import (
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
