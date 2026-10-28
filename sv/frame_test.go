// SPDX-License-Identifier: MIT

package sv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	testDst = MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}
	testSrc = MAC{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
)

func leSample(seed int32) []byte {
	var s LESample
	for i := range s.Current {
		s.Current[i] = Point{Value: seed + int32(i), Q: QualityGood}
		s.Voltage[i] = Point{Value: -seed - int32(i), Q: QualityTest}
	}
	return s.Append(nil)
}

func fullFrame() *Frame {
	gm := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	return &Frame{
		Dst: testDst, Src: testSrc, VLAN: &VLAN{Priority: 4, ID: 0x123, DEI: true}, AppID: 0x4000,
		Simulation: true, Reserved1: 0x0001, Reserved2: 0xbeef,
		PDU: PDU{
			Security: []byte{0xde, 0xad},
			ASDUs: []ASDU{
				{
					SvID: "MU01", DatSet: "IED1MU/LLN0$PhsMeas1", SmpCnt: 3999, ConfRev: 1,
					RefrTm: time.Date(2026, 10, 28, 12, 0, 0, 500_000_000, time.UTC), RefrTmQuality: 0x0a, HasRefrTm: true,
					SmpSynch: SmpSynchGlobal, SmpRate: 80, HasSmpRate: true, Sample: leSample(1),
					SmpMod: SmpPerPeriod, HasSmpMod: true, GmIdentity: gm, HasGmIdentity: true,
				},
				{SvID: "MU01", SmpCnt: 0, ConfRev: 1, SmpSynch: SmpSynchGlobal, Sample: leSample(2)},
			},
		},
	}
}

func TestFrameRoundTrip(t *testing.T) {
	f := fullFrame()
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if et, _ := binary.BigEndian.Uint16(raw[16:]), 0; et != EtherType {
		t.Fatalf("ethertype %04x", et)
	}
	g, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if g.Dst != f.Dst || g.Src != f.Src || *g.VLAN != *f.VLAN || g.AppID != f.AppID || !g.Simulation ||
		g.Reserved1 != 1 || g.Reserved2 != 0xbeef || !bytes.Equal(g.PDU.Security, f.PDU.Security) || g.PDU.NoASDU != 2 {
		t.Fatalf("header: %+v", g)
	}
	for i := range f.PDU.ASDUs {
		a, b := f.PDU.ASDUs[i], g.PDU.ASDUs[i]
		if a.SvID != b.SvID || a.DatSet != b.DatSet || a.SmpCnt != b.SmpCnt || a.ConfRev != b.ConfRev ||
			a.HasRefrTm != b.HasRefrTm || !a.RefrTm.Equal(b.RefrTm) || a.RefrTmQuality != b.RefrTmQuality ||
			a.SmpSynch != b.SmpSynch || a.HasSmpRate != b.HasSmpRate || a.SmpRate != b.SmpRate ||
			!bytes.Equal(a.Sample, b.Sample) || a.HasSmpMod != b.HasSmpMod || a.SmpMod != b.SmpMod ||
			a.HasGmIdentity != b.HasGmIdentity || a.GmIdentity != b.GmIdentity {
			t.Errorf("ASDU %d:\n got %+v\nwant %+v", i, b, a)
		}
	}
	// Decode does not alias the input.
	raw[len(raw)-1] ^= 0xff
	if g.PDU.ASDUs[1].Sample[len(g.PDU.ASDUs[1].Sample)-1] == raw[len(raw)-1] {
		t.Error("Decode aliases its input")
	}
	// Re-encoding is stable.
	raw2, _ := Encode(g)
	raw[len(raw)-1] ^= 0xff
	if !bytes.Equal(raw, raw2) {
		t.Error("re-encoding differs")
	}
	if !strings.Contains(g.String(), "MU01 smpCnt=3999") {
		t.Errorf("String: %s", g)
	}
}

func TestEncodeMinimalPadding(t *testing.T) {
	f := &Frame{Dst: testDst, AppID: 0x4001, PDU: PDU{ASDUs: []ASDU{{SvID: "x", Sample: []byte{1}}}}}
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != minFrame {
		t.Errorf("len %d, want padded to %d", len(raw), minFrame)
	}
	g, err := Decode(raw)
	if err != nil || g.VLAN != nil || g.PDU.ASDUs[0].DatSet != "" || g.PDU.Security != nil {
		t.Fatalf("%v %+v", err, g)
	}
	p, err := EncodePDU(&f.PDU)
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodePDU(p)
	if err != nil || q.ASDUs[0].SvID != "x" {
		t.Fatalf("DecodePDU: %v", err)
	}
	// Large frames use multi-octet BER lengths.
	big := &Frame{Dst: testDst, PDU: PDU{ASDUs: []ASDU{{SvID: "b", Sample: make([]byte, 300)}}}}
	raw, err = Encode(big)
	if err != nil {
		t.Fatal(err)
	}
	if g, err := Decode(raw); err != nil || len(g.PDU.ASDUs[0].Sample) != 300 {
		t.Fatalf("big: %v", err)
	}
}

func TestEncodeErrors(t *testing.T) {
	cases := []struct {
		name string
		f    *Frame
		want error
	}{
		{"nil", nil, ErrInvalidValue},
		{"no asdu", &Frame{}, ErrInvalidValue},
		{"empty svID", &Frame{PDU: PDU{ASDUs: []ASDU{{}}}}, ErrInvalidValue},
		{"bad svID", &Frame{PDU: PDU{ASDUs: []ASDU{{SvID: "a\x01"}}}}, ErrInvalidValue},
		{"bad noASDU", &Frame{PDU: PDU{NoASDU: 70000, ASDUs: []ASDU{{SvID: "a"}}}}, ErrInvalidValue},
		{"vlan", &Frame{VLAN: &VLAN{Priority: 9}, PDU: PDU{ASDUs: []ASDU{{SvID: "a"}}}}, ErrInvalidValue},
		{"too big", &Frame{PDU: PDU{ASDUs: []ASDU{{SvID: "a", Sample: make([]byte, 1600)}}}}, ErrLimitExceeded},
	}
	for _, c := range cases {
		if _, err := Encode(c.f); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := EncodePDU(&PDU{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("EncodePDU: %v", err)
	}
}

// raw frame builder for negative decode tests.
func rawFrame(apdu []byte) []byte {
	b := append([]byte(nil), testDst[:]...)
	b = append(b, testSrc[:]...)
	b = binary.BigEndian.AppendUint16(b, EtherType)
	b = binary.BigEndian.AppendUint16(b, 0x4000)
	b = binary.BigEndian.AppendUint16(b, uint16(headerLen+len(apdu)))
	b = append(b, 0, 0, 0, 0)
	return append(b, apdu...)
}

func tlvBytes(tag byte, c []byte) []byte { return append(appendHdr(nil, tag, len(c)), c...) }

func asduBytes(fields ...[]byte) []byte {
	var c []byte
	for _, f := range fields {
		c = append(c, f...)
	}
	return tlvBytes(tagSequence, c)
}

func savPdu(no []byte, asdus ...[]byte) []byte {
	var seq []byte
	for _, a := range asdus {
		seq = append(seq, a...)
	}
	return tlvBytes(tagSavPDU, append(tlvBytes(tagNoASDU, no), tlvBytes(tagASDUSeq, seq)...))
}

var (
	fSvID  = tlvBytes(tagSvID, []byte("MU"))
	fCnt   = tlvBytes(tagSmpCnt, []byte{0, 1})
	fConf  = tlvBytes(tagConfRev, []byte{0, 0, 0, 1})
	fSynch = tlvBytes(tagSmpSynch, []byte{1})
	fSmp   = tlvBytes(tagSample, []byte{1, 2, 3, 4})
	goodA  = asduBytes(fSvID, fCnt, fConf, fSynch, fSmp)
)

func TestDecodeErrors(t *testing.T) {
	good := rawFrame(savPdu([]byte{1}, goodA))
	if _, err := Decode(good); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	tagged := append(append(append([]byte(nil), good[:12]...), 0x81, 0x00, 0x80, 0x01), good[12:]...)
	if f, err := Decode(tagged); err != nil || f.VLAN == nil || f.VLAN.Priority != 4 || f.VLAN.ID != 1 {
		t.Fatalf("tagged: %v", err)
	}

	ip := append(append([]byte(nil), good[:12]...), 0x08, 0x00)
	ip = append(ip, make([]byte, 50)...)
	badLen := append([]byte(nil), good...)
	binary.BigEndian.PutUint16(badLen[16:], 4)
	longLen := append([]byte(nil), good...)
	binary.BigEndian.PutUint16(longLen[16:], 1000)

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"short", good[:10], ErrInvalidFrame},
		{"vlan truncated", append(append([]byte(nil), good[:12]...), 0x81, 0x00, 0x80), ErrInvalidFrame},
		{"not sv", ip, ErrNotSV},
		{"header truncated", good[:18], ErrInvalidFrame},
		{"length below header", badLen, ErrInvalidLength},
		{"length beyond frame", longLen, ErrInvalidLength},
		{"empty apdu", rawFrame(nil), ErrInvalidLength},
		{"wrong apdu tag", rawFrame(tlvBytes(0x61, nil)), ErrInvalidPDU},
		{"trailing after pdu", rawFrame(append(savPdu([]byte{1}, goodA), 0, 0)), ErrInvalidLength},
		{"missing noASDU", rawFrame(tlvBytes(tagSavPDU, tlvBytes(tagASDUSeq, goodA))), ErrInvalidPDU},
		{"noASDU zero", rawFrame(savPdu([]byte{0}, goodA)), ErrInvalidPDU},
		{"noASDU negative", rawFrame(savPdu([]byte{0xff}, goodA)), ErrInvalidPDU},
		{"noASDU huge", rawFrame(savPdu([]byte{1, 0, 0, 0, 0, 0}, goodA)), ErrInvalidPDU},
		{"noASDU above limit", rawFrame(savPdu([]byte{100}, goodA)), ErrLimitExceeded},
		{"noASDU mismatch low", rawFrame(savPdu([]byte{2}, goodA)), ErrInvalidPDU},
		{"noASDU mismatch high", rawFrame(savPdu([]byte{1}, goodA, goodA)), ErrInvalidPDU},
		{"asdu seq missing", rawFrame(tlvBytes(tagSavPDU, tlvBytes(tagNoASDU, []byte{1}))), ErrInvalidPDU},
		{"unexpected pdu field", rawFrame(tlvBytes(tagSavPDU, append(tlvBytes(tagNoASDU, []byte{1}), tlvBytes(0x85, nil)...))), ErrInvalidPDU},
		{"trailing after seq", rawFrame(tlvBytes(tagSavPDU, append(append(tlvBytes(tagNoASDU, []byte{1}), tlvBytes(tagASDUSeq, goodA)...), 0x05, 0x00))), ErrInvalidPDU},
		{"asdu not sequence", rawFrame(savPdu([]byte{1}, tlvBytes(0x31, nil))), ErrInvalidPDU},
		{"missing svID", rawFrame(savPdu([]byte{1}, asduBytes(fCnt, fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"missing sample", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch))), ErrInvalidPDU},
		{"empty svID", rawFrame(savPdu([]byte{1}, asduBytes(tlvBytes(tagSvID, nil), fCnt, fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"bad svID char", rawFrame(savPdu([]byte{1}, asduBytes(tlvBytes(tagSvID, []byte{0x07}), fCnt, fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"bad datSet char", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, tlvBytes(tagDatSet, []byte{0x80}), fCnt, fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"out of order", rawFrame(savPdu([]byte{1}, asduBytes(fCnt, fSvID, fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"duplicate", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fSvID, fCnt, fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"unknown field", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch, fSmp, tlvBytes(0x8a, nil)))), ErrInvalidPDU},
		{"low tag", rawFrame(savPdu([]byte{1}, asduBytes(tlvBytes(0x04, nil)))), ErrInvalidPDU},
		{"smpCnt size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, tlvBytes(tagSmpCnt, []byte{1}), fConf, fSynch, fSmp))), ErrInvalidPDU},
		{"confRev size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, tlvBytes(tagConfRev, []byte{1}), fSynch, fSmp))), ErrInvalidPDU},
		{"refrTm size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, tlvBytes(tagRefrTm, []byte{1}), fSynch, fSmp))), ErrInvalidPDU},
		{"smpSynch size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, tlvBytes(tagSmpSynch, nil), fSmp))), ErrInvalidPDU},
		{"smpRate size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch, tlvBytes(tagSmpRate, []byte{1}), fSmp))), ErrInvalidPDU},
		{"smpMod size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch, fSmp, tlvBytes(tagSmpMod, []byte{0, 0, 1})))), ErrInvalidPDU},
		{"gmIdentity size", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch, fSmp, tlvBytes(tagGmIdentity, []byte{1})))), ErrInvalidPDU},
		{"sample over limit", rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch, tlvBytes(tagSample, make([]byte, 1501))))), ErrLimitExceeded},
		{"truncated tag", rawFrame(tlvBytes(tagSavPDU, []byte{0x80, 0x01, 0x01, 0xa2, 0x02, 0x30})), ErrInvalidPDU},
		{"multi-octet tag", rawFrame([]byte{0x7f, 0x01, 0x00}), ErrInvalidPDU},
		{"missing length", rawFrame([]byte{0x60}), ErrInvalidPDU},
		{"indefinite length", rawFrame([]byte{0x60, 0x80, 0x00, 0x00}), ErrInvalidPDU},
		{"length too wide", rawFrame([]byte{0x60, 0x85, 0, 0, 0, 0, 1}), ErrInvalidPDU},
		{"length truncated", rawFrame([]byte{0x60, 0x82, 0x01}), ErrInvalidPDU},
		{"length overrun", rawFrame([]byte{0x60, 0x82, 0x01, 0x00}), ErrInvalidPDU},
	}
	for _, c := range cases {
		_, err := Decode(c.raw)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
			continue
		}
		var de *DecodeError
		if !errors.As(err, &de) || de.Error() == "" {
			t.Errorf("%s: not a DecodeError: %T", c.name, err)
		}
	}
	if _, err := DecodePDU(nil); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("DecodePDU(nil): %v", err)
	}
	if _, err := DecodePDU(savPdu([]byte{1}, asduBytes(fSvID))); !errors.Is(err, ErrInvalidPDU) {
		t.Errorf("DecodePDU: %v", err)
	}
}

func TestDecodeIntoReuse(t *testing.T) {
	a := fullFrame()
	raw, _ := Encode(a)
	var f Frame
	if err := DecodeInto(&f, raw, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	// Samples alias the input.
	if len(f.PDU.ASDUs[0].Sample) == 0 || !bytes.Contains(raw, f.PDU.ASDUs[0].Sample) {
		t.Fatal("sample")
	}
	// An untagged frame without optional fields clears the previous ones.
	b := &Frame{Dst: testDst, PDU: PDU{ASDUs: []ASDU{{SvID: "MU01", Sample: []byte{9}}}}}
	raw2, _ := Encode(b)
	if err := DecodeInto(&f, raw2, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	g := f.PDU.ASDUs[0]
	if f.VLAN != nil || f.PDU.Security != nil || len(f.PDU.ASDUs) != 1 || g.DatSet != "" || g.HasRefrTm || g.HasSmpRate || g.HasSmpMod || g.HasGmIdentity {
		t.Fatalf("stale fields: %+v", f)
	}
	// Steady-state decoding allocates nothing.
	if err := DecodeInto(&f, raw, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(200, func() {
		if err := DecodeInto(&f, raw, DefaultLimits); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Errorf("DecodeInto allocates %.1f per frame", allocs)
	}
}

// TestSmpModOneOctet accepts the single-octet smpMod some publishers
// send (the encoder writes INT16U).
func TestSmpModOneOctet(t *testing.T) {
	f, err := Decode(rawFrame(savPdu([]byte{1}, asduBytes(fSvID, fCnt, fConf, fSynch, fSmp, tlvBytes(tagSmpMod, []byte{1})))))
	if err != nil || !f.PDU.ASDUs[0].HasSmpMod || f.PDU.ASDUs[0].SmpMod != SmpPerSec {
		t.Fatalf("%v %+v", err, f)
	}
}

func TestSmpModString(t *testing.T) {
	for m, s := range map[SmpMod]string{SmpPerPeriod: "SmpPerPeriod", SmpPerSec: "SmpPerSec", SecPerSmp: "SecPerSmp", 7: "SmpMod(7)"} {
		if m.String() != s {
			t.Errorf("%d: %s", m, m)
		}
	}
}

func TestErrorTypes(t *testing.T) {
	ve := &ValidationError{SvID: "a", Field: "confRev", Want: "1", Got: "2", Err: ErrConfRevMismatch}
	ce := &ConfigError{Field: "f", Message: "m"}
	de := &DecodeError{Offset: 3, Message: "m", Err: ErrInvalidPDU}
	for _, e := range []error{ve, ce, de} {
		if e.Error() == "" {
			t.Errorf("%T", e)
		}
	}
	if !errors.Is(ve, ErrValidation) || !errors.Is(ve, ErrConfRevMismatch) || !errors.Is(ce, ErrInvalidConfig) || !errors.Is(de, ErrInvalidPDU) {
		t.Error("unwrap")
	}
	if !errors.Is(ErrClosed, ErrClosed) || ErrInvalidConfig == nil {
		t.Error("sentinels")
	}
}
