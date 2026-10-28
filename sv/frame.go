// SPDX-License-Identifier: MIT

package sv

import (
	"encoding/binary"
	"strconv"
	"time"

	"github.com/otfabric/go-iec61850/l2"
)

// Ethernet and SV framing constants (IEC 61850-9-2).
const (
	// EtherType is the Sampled Values EtherType.
	EtherType = l2.EtherTypeSV

	// EtherTypeVLAN is the IEEE 802.1Q tag protocol identifier.
	EtherTypeVLAN = l2.EtherTypeVLAN

	// SimulationBit is the bit in Reserved1 that marks a simulated
	// message (IEC 61850-9-2 Ed2.1 "S" bit).
	SimulationBit uint16 = 0x8000

	// MaxPayload is the maximum Ethernet payload (after the EtherType)
	// produced by the encoder.
	MaxPayload = 1500

	// minFrame is the minimum Ethernet frame length without FCS; the
	// encoder pads shorter frames.
	minFrame = 60

	headerLen = 8 // APPID + Length + Reserved1 + Reserved2
)

// BER tags of the savPdu (IEC 61850-9-2 clause 8.5).
const (
	tagSavPDU   byte = 0x60 // [APPLICATION 0] constructed
	tagNoASDU   byte = 0x80 // [0] INTEGER
	tagSecPrim  byte = 0x81 // [1] security, primitive form
	tagSecCons  byte = 0xa1 // [1] security, constructed form
	tagASDUSeq  byte = 0xa2 // [2] SEQUENCE OF ASDU
	tagSequence byte = 0x30 // ASDU ::= SEQUENCE

	tagSvID       byte = 0x80
	tagDatSet     byte = 0x81
	tagSmpCnt     byte = 0x82
	tagConfRev    byte = 0x83
	tagRefrTm     byte = 0x84
	tagSmpSynch   byte = 0x85
	tagSmpRate    byte = 0x86
	tagSample     byte = 0x87
	tagSmpMod     byte = 0x88
	tagGmIdentity byte = 0x89
)

// SmpSynch values (IEC 61850-9-2 Ed2.1): 0 not synchronized, 1 local
// area clock, 2 global area clock; 5-254 identify a local clock by ID.
const (
	SmpSynchNone   uint8 = 0
	SmpSynchLocal  uint8 = 1
	SmpSynchGlobal uint8 = 2
)

// SmpMod is the sample mode of smpRate (IEC 61850-7-2).
type SmpMod uint16

// Sample modes.
const (
	// SmpPerPeriod: smpRate is samples per nominal period (the default).
	SmpPerPeriod SmpMod = 0
	// SmpPerSec: smpRate is samples per second.
	SmpPerSec SmpMod = 1
	// SecPerSmp: smpRate is seconds per sample.
	SecPerSmp SmpMod = 2
)

// String returns the SCL name of the mode.
func (m SmpMod) String() string {
	switch m {
	case SmpPerPeriod:
		return "SmpPerPeriod"
	case SmpPerSec:
		return "SmpPerSec"
	case SecPerSmp:
		return "SecPerSmp"
	default:
		return "SmpMod(" + strconv.Itoa(int(m)) + ")"
	}
}

// Frame is a decoded or to-be-encoded SV Ethernet frame.
type Frame struct {
	// Dst and Src are the Ethernet destination and source addresses.
	Dst, Src MAC

	// VLAN is the 802.1Q tag, or nil for an untagged frame.
	VLAN *VLAN

	// AppID is the application identifier (SV range 0x4000-0x7FFF).
	AppID uint16

	// Simulation reports the Reserved1 simulation ("S") bit.
	Simulation bool

	// Reserved1 holds the remaining Reserved1 bits (the simulation bit
	// is masked off and reported in Simulation).
	Reserved1 uint16

	// Reserved2 holds the Reserved2 field verbatim.
	Reserved2 uint16

	// PDU is the savPdu.
	PDU PDU

	// strs interns svID and datSet strings across DecodeInto calls, so
	// that frames of several interleaved streams decode without
	// allocating.
	strs map[string]string
}

// maxInterned bounds the per-frame string intern table.
const maxInterned = 256

// PDU is the savPdu (IEC 61850-9-2 clause 8.5).
type PDU struct {
	// NoASDU is the number of ASDUs. On encode, zero means len(ASDUs);
	// a non-zero value is sent verbatim (for negative tests). Decoding
	// rejects a value that differs from the number of ASDUs.
	NoASDU int

	// Security is the raw content of the optional [1] security field.
	// It is carried but not interpreted.
	Security []byte

	// ASDUs holds the application service data units, 1..65535.
	ASDUs []ASDU
}

// ASDU is one sampled value message.
type ASDU struct {
	// SvID is the sampled value identifier (MsvID/UsvID).
	SvID string

	// DatSet is the dataset reference "<LD>/LLN0$<ds>"; empty when
	// absent (optional, OptFlds dataSet).
	DatSet string

	// SmpCnt is the sample counter.
	SmpCnt uint16

	// ConfRev is the configuration revision.
	ConfRev uint32

	// RefrTm is the refresh time (optional, OptFlds refreshTime),
	// valid when HasRefrTm. RefrTmQuality is its TimeQuality octet.
	RefrTm        time.Time
	RefrTmQuality uint8
	HasRefrTm     bool

	// SmpSynch is the synchronization state (see SmpSynchNone et al.).
	SmpSynch uint8

	// SmpRate is the sample rate (optional, OptFlds sampleRate), valid
	// when HasSmpRate; its unit is given by SmpMod.
	SmpRate    uint16
	HasSmpRate bool

	// Sample is the dataset in the 9-2 binary encoding. Frames decoded
	// with [DecodeInto] alias the input buffer.
	Sample []byte

	// SmpMod is the sample mode (optional, Ed2), valid when HasSmpMod.
	SmpMod    SmpMod
	HasSmpMod bool

	// GmIdentity is the grandmaster clock identity (optional, Ed2.1),
	// valid when HasGmIdentity.
	GmIdentity    [8]byte
	HasGmIdentity bool
}

// Limits bounds the decoder.
type Limits struct {
	// MaxASDUs is the maximum number of ASDUs per frame.
	MaxASDUs int

	// MaxSample is the maximum sample size in octets.
	MaxSample int
}

// DefaultLimits are the limits used by [Decode] and [DecodePDU].
var DefaultLimits = Limits{MaxASDUs: 64, MaxSample: MaxPayload}

// Encode returns the wire encoding of f. Frames shorter than 60 octets
// are zero-padded. It fails with [ErrLimitExceeded] if the Ethernet
// payload would exceed [MaxPayload] and with [ErrInvalidValue] for
// fields that cannot be encoded.
func Encode(f *Frame) ([]byte, error) { return AppendFrame(nil, f) }

// AppendFrame appends the wire encoding of f to dst. It does not
// allocate when dst has enough capacity.
func AppendFrame(dst []byte, f *Frame) ([]byte, error) {
	if f == nil {
		return dst, errValue("nil frame")
	}
	apduLen, err := pduLen(&f.PDU)
	if err != nil {
		return dst, err
	}
	payload := headerLen + apduLen
	if payload > MaxPayload {
		return dst, errLimit("SV payload of %d octets exceeds %d", payload, MaxPayload)
	}
	start := len(dst)
	dst = append(dst, f.Dst[:]...)
	dst = append(dst, f.Src[:]...)
	if f.VLAN != nil {
		tci, ok := f.VLAN.TCI()
		if !ok {
			return dst[:start], errValue("VLAN priority %d / id %d out of range", f.VLAN.Priority, f.VLAN.ID)
		}
		dst = binary.BigEndian.AppendUint16(dst, EtherTypeVLAN)
		dst = binary.BigEndian.AppendUint16(dst, tci)
	}
	dst = binary.BigEndian.AppendUint16(dst, EtherType)
	dst = binary.BigEndian.AppendUint16(dst, f.AppID)
	dst = binary.BigEndian.AppendUint16(dst, uint16(payload))
	r1 := f.Reserved1 &^ SimulationBit
	if f.Simulation {
		r1 |= SimulationBit
	}
	dst = binary.BigEndian.AppendUint16(dst, r1)
	dst = binary.BigEndian.AppendUint16(dst, f.Reserved2)
	dst = appendPDU(dst, &f.PDU)
	for len(dst)-start < minFrame {
		dst = append(dst, 0)
	}
	return dst, nil
}

// EncodePDU returns the BER encoding of the savPdu ([APPLICATION 0]).
func EncodePDU(p *PDU) ([]byte, error) {
	n, err := pduLen(p)
	if err != nil {
		return nil, err
	}
	return appendPDU(make([]byte, 0, n), p), nil
}

func asduContentLen(a *ASDU) int {
	n := tlvLen(len(a.SvID)) + tlvLen(2) + tlvLen(4) + tlvLen(1) + tlvLen(len(a.Sample))
	if a.DatSet != "" {
		n += tlvLen(len(a.DatSet))
	}
	if a.HasRefrTm {
		n += tlvLen(8)
	}
	if a.HasSmpRate {
		n += tlvLen(2)
	}
	if a.HasSmpMod {
		n += tlvLen(2)
	}
	if a.HasGmIdentity {
		n += tlvLen(8)
	}
	return n
}

func noASDU(p *PDU) int {
	if p.NoASDU != 0 {
		return p.NoASDU
	}
	return len(p.ASDUs)
}

// pduLen validates p and returns its encoded size.
func pduLen(p *PDU) (int, error) {
	if len(p.ASDUs) == 0 {
		return 0, errValue("savPdu without ASDU")
	}
	if n := noASDU(p); n < 0 || n > 0xffff {
		return 0, errValue("noASDU %d out of range", n)
	}
	seq := 0
	for i := range p.ASDUs {
		a := &p.ASDUs[i]
		if a.SvID == "" {
			return 0, errValue("ASDU %d: empty svID", i)
		}
		if !isVisible(a.SvID) || !isVisible(a.DatSet) {
			return 0, errValue("ASDU %d: svID/datSet must be VisibleString", i)
		}
		seq += tlvLen(asduContentLen(a))
	}
	c := tlvLen(unsignedLen(uint32(noASDU(p)))) + tlvLen(seq)
	if p.Security != nil {
		c += tlvLen(len(p.Security))
	}
	return tlvLen(c), nil
}

func appendPDU(b []byte, p *PDU) []byte {
	seq := 0
	for i := range p.ASDUs {
		seq += tlvLen(asduContentLen(&p.ASDUs[i]))
	}
	var nbuf [5]byte
	n := unsignedBytes(nbuf[:0], uint32(noASDU(p)))
	c := tlvLen(len(n)) + tlvLen(seq)
	if p.Security != nil {
		c += tlvLen(len(p.Security))
	}
	b = appendHdr(b, tagSavPDU, c)
	b = appendHdr(b, tagNoASDU, len(n))
	b = append(b, n...)
	if p.Security != nil {
		b = appendHdr(b, tagSecPrim, len(p.Security))
		b = append(b, p.Security...)
	}
	b = appendHdr(b, tagASDUSeq, seq)
	for i := range p.ASDUs {
		b = appendASDU(b, &p.ASDUs[i])
	}
	return b
}

func appendASDU(b []byte, a *ASDU) []byte {
	b = appendHdr(b, tagSequence, asduContentLen(a))
	b = appendHdr(b, tagSvID, len(a.SvID))
	b = append(b, a.SvID...)
	if a.DatSet != "" {
		b = appendHdr(b, tagDatSet, len(a.DatSet))
		b = append(b, a.DatSet...)
	}
	b = appendHdr(b, tagSmpCnt, 2)
	b = binary.BigEndian.AppendUint16(b, a.SmpCnt)
	b = appendHdr(b, tagConfRev, 4)
	b = binary.BigEndian.AppendUint32(b, a.ConfRev)
	if a.HasRefrTm {
		b = appendHdr(b, tagRefrTm, 8)
		b = appendUTCTime(b, a.RefrTm, a.RefrTmQuality)
	}
	b = appendHdr(b, tagSmpSynch, 1)
	b = append(b, a.SmpSynch)
	if a.HasSmpRate {
		b = appendHdr(b, tagSmpRate, 2)
		b = binary.BigEndian.AppendUint16(b, a.SmpRate)
	}
	b = appendHdr(b, tagSample, len(a.Sample))
	b = append(b, a.Sample...)
	if a.HasSmpMod {
		b = appendHdr(b, tagSmpMod, 2)
		b = binary.BigEndian.AppendUint16(b, uint16(a.SmpMod))
	}
	if a.HasGmIdentity {
		b = appendHdr(b, tagGmIdentity, 8)
		b = append(b, a.GmIdentity[:]...)
	}
	return b
}

// Decode strictly decodes an SV Ethernet frame (without FCS) using
// [DefaultLimits]. The returned frame does not alias b.
func Decode(b []byte) (*Frame, error) {
	f := &Frame{}
	if err := DecodeInto(f, b, DefaultLimits); err != nil {
		return nil, err
	}
	f.strs = nil
	f.PDU.Security = clone(f.PDU.Security)
	for i := range f.PDU.ASDUs {
		f.PDU.ASDUs[i].Sample = clone(f.PDU.ASDUs[i].Sample)
	}
	return f, nil
}

// DecodeInto strictly decodes an SV Ethernet frame into f, reusing f's
// storage: the ASDU slice, the VLAN tag and svID/datSet strings that did
// not change are reused, and Sample and Security alias b. Steady-state
// decoding of a stream therefore does not allocate. On error f holds
// partial results.
func DecodeInto(f *Frame, b []byte, lim Limits) error {
	if len(b) < 14 {
		return decodeErr(ErrInvalidFrame, "ethernet", 0, "frame of %d octets is too short", len(b))
	}
	copy(f.Dst[:], b[0:6])
	copy(f.Src[:], b[6:12])
	pos := 12
	et := binary.BigEndian.Uint16(b[pos:])
	pos += 2
	if et == EtherTypeVLAN {
		if len(b) < pos+4 {
			return decodeErr(ErrInvalidFrame, "802.1Q", pos, "truncated VLAN tag")
		}
		if f.VLAN == nil {
			f.VLAN = new(VLAN)
		}
		*f.VLAN = l2.VLANFromTCI(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
		et = binary.BigEndian.Uint16(b[pos:])
		pos += 2
	} else {
		f.VLAN = nil
	}
	if et != EtherType {
		return decodeErr(ErrNotSV, "ethertype", pos-2, "EtherType 0x%04x", et)
	}
	if len(b) < pos+headerLen {
		return decodeErr(ErrInvalidFrame, "header", pos, "truncated SV header")
	}
	f.AppID = binary.BigEndian.Uint16(b[pos:])
	length := int(binary.BigEndian.Uint16(b[pos+2:]))
	r1 := binary.BigEndian.Uint16(b[pos+4:])
	f.Simulation = r1&SimulationBit != 0
	f.Reserved1 = r1 &^ SimulationBit
	f.Reserved2 = binary.BigEndian.Uint16(b[pos+6:])
	if length < headerLen {
		return decodeErr(ErrInvalidLength, "length", pos+2, "length %d below header size", length)
	}
	if pos+length > len(b) {
		return decodeErr(ErrInvalidLength, "length", pos+2, "length %d exceeds frame (%d octets available)", length, len(b)-pos)
	}
	apduStart := pos + headerLen
	if f.strs == nil {
		f.strs = map[string]string{}
	}
	return decodeAPDU(b[apduStart:pos+length], apduStart, lim, &f.PDU, f.strs)
}

// DecodePDU strictly decodes a savPdu (starting at the [APPLICATION 0]
// tag) using [DefaultLimits]. The returned PDU does not alias b.
func DecodePDU(b []byte) (*PDU, error) {
	p := &PDU{}
	if err := decodeAPDU(b, 0, DefaultLimits, p, nil); err != nil {
		return nil, err
	}
	p.Security = clone(p.Security)
	for i := range p.ASDUs {
		p.ASDUs[i].Sample = clone(p.ASDUs[i].Sample)
	}
	return p, nil
}

func decodeAPDU(b []byte, base int, lim Limits, p *PDU, strs map[string]string) error {
	if len(b) == 0 {
		return decodeErr(ErrInvalidLength, "apdu", base, "empty APDU")
	}
	top, err := readTLV(b, 0, base, "savPdu")
	if err != nil {
		return err
	}
	if top.tag != tagSavPDU {
		return decodeErr(ErrInvalidPDU, "apdu", base, "unexpected APDU tag 0x%02x", top.tag)
	}
	if top.next != len(b) {
		return decodeErr(ErrInvalidLength, "apdu", base+top.next, "%d octets after savPdu", len(b)-top.next)
	}
	c := top.content
	cbase := base + top.next - len(c)

	e, err := readTLV(c, 0, cbase, "noASDU")
	if err != nil {
		return err
	}
	if e.tag != tagNoASDU {
		return decodeErr(ErrInvalidPDU, "noASDU", cbase+e.off, "missing noASDU (tag 0x%02x)", e.tag)
	}
	n, err := decodeUint(e.content, "noASDU", cbase+e.off)
	if err != nil {
		return err
	}
	if n < 1 || n > 0xffff {
		return decodeErr(ErrInvalidPDU, "noASDU", cbase+e.off, "noASDU %d out of range", n)
	}
	if lim.MaxASDUs > 0 && int(n) > lim.MaxASDUs {
		return decodeErr(ErrLimitExceeded, "noASDU", cbase+e.off, "noASDU %d exceeds limit %d", n, lim.MaxASDUs)
	}
	p.NoASDU = int(n)
	pos := e.next

	e, err = readTLV(c, pos, cbase, "asdu")
	if err != nil {
		return err
	}
	p.Security = nil
	if e.tag == tagSecPrim || e.tag == tagSecCons {
		p.Security = e.content
		pos = e.next
		if e, err = readTLV(c, pos, cbase, "asdu"); err != nil {
			return err
		}
	}
	if e.tag != tagASDUSeq {
		return decodeErr(ErrInvalidPDU, "asdu", cbase+e.off, "unexpected field tag 0x%02x", e.tag)
	}
	if e.next != len(c) {
		return decodeErr(ErrInvalidPDU, "savPdu", cbase+e.next, "%d octets after the ASDU sequence", len(c)-e.next)
	}
	seq := e.content
	sbase := cbase + e.next - len(seq)
	if cap(p.ASDUs) < p.NoASDU {
		p.ASDUs = make([]ASDU, 0, p.NoASDU)
	}
	p.ASDUs = p.ASDUs[:0]
	spos := 0
	for spos < len(seq) {
		if len(p.ASDUs) == p.NoASDU {
			return decodeErr(ErrInvalidPDU, "noASDU", cbase, "more ASDUs than noASDU %d", p.NoASDU)
		}
		a, err := readTLV(seq, spos, sbase, "ASDU")
		if err != nil {
			return err
		}
		if a.tag != tagSequence {
			return decodeErr(ErrInvalidPDU, "ASDU", sbase+a.off, "unexpected tag 0x%02x", a.tag)
		}
		// Reuse the previous element at this index (strings compare
		// without allocating).
		i := len(p.ASDUs)
		p.ASDUs = p.ASDUs[:i+1]
		if err := decodeASDU(a.content, sbase+a.next-len(a.content), lim, &p.ASDUs[i], strs); err != nil {
			return err
		}
		spos = a.next
	}
	if len(p.ASDUs) != p.NoASDU {
		return decodeErr(ErrInvalidPDU, "noASDU", cbase, "noASDU %d but %d ASDUs", p.NoASDU, len(p.ASDUs))
	}
	return nil
}

var asduTags = [...]byte{tagSvID, tagDatSet, tagSmpCnt, tagConfRev, tagRefrTm, tagSmpSynch, tagSmpRate, tagSample, tagSmpMod, tagGmIdentity}
var asduNames = [...]string{"svID", "datSet", "smpCnt", "confRev", "refrTm", "smpSynch", "smpRate", "sample", "smpMod", "gmIdentity"}

// mandatory fields: svID, smpCnt, confRev, smpSynch, sample.
const asduMandatory = 1<<0 | 1<<2 | 1<<3 | 1<<5 | 1<<7

func decodeASDU(c []byte, base int, lim Limits, a *ASDU, strs map[string]string) error {
	pos := 0
	last := -1
	var seen uint16
	a.HasRefrTm, a.HasSmpRate, a.HasSmpMod, a.HasGmIdentity = false, false, false, false
	datSet := false
	for pos < len(c) {
		e, err := readTLV(c, pos, base, "ASDU")
		if err != nil {
			return err
		}
		off := base + e.off
		idx := int(e.tag) - int(tagSvID)
		if e.tag < tagSvID || idx >= len(asduTags) {
			return decodeErr(ErrInvalidPDU, "ASDU", off, "unexpected field tag 0x%02x", e.tag)
		}
		name := asduNames[idx]
		if idx <= last {
			return decodeErr(ErrInvalidPDU, name, off, "duplicate or out-of-order field")
		}
		last = idx
		seen |= 1 << idx
		v := e.content
		switch e.tag {
		case tagSvID:
			if len(v) == 0 {
				return decodeErr(ErrInvalidPDU, name, off, "empty svID")
			}
			if err := setVisible(&a.SvID, v, strs, name, off); err != nil {
				return err
			}
		case tagDatSet:
			if err := setVisible(&a.DatSet, v, strs, name, off); err != nil {
				return err
			}
			datSet = true
		case tagSmpCnt:
			if len(v) != 2 {
				return sizeErr(name, off, 2, len(v))
			}
			a.SmpCnt = binary.BigEndian.Uint16(v)
		case tagConfRev:
			if len(v) != 4 {
				return sizeErr(name, off, 4, len(v))
			}
			a.ConfRev = binary.BigEndian.Uint32(v)
		case tagRefrTm:
			if len(v) != 8 {
				return sizeErr(name, off, 8, len(v))
			}
			a.RefrTm, a.RefrTmQuality = decodeUTCTime(v)
			a.HasRefrTm = true
		case tagSmpSynch:
			if len(v) != 1 {
				return sizeErr(name, off, 1, len(v))
			}
			a.SmpSynch = v[0]
		case tagSmpRate:
			if len(v) != 2 {
				return sizeErr(name, off, 2, len(v))
			}
			a.SmpRate = binary.BigEndian.Uint16(v)
			a.HasSmpRate = true
		case tagSample:
			if lim.MaxSample > 0 && len(v) > lim.MaxSample {
				return decodeErr(ErrLimitExceeded, name, off, "sample of %d octets exceeds limit %d", len(v), lim.MaxSample)
			}
			a.Sample = v
		case tagSmpMod:
			// INT16U; some publishers send a single octet.
			switch len(v) {
			case 1:
				a.SmpMod = SmpMod(v[0])
			case 2:
				a.SmpMod = SmpMod(binary.BigEndian.Uint16(v))
			default:
				return sizeErr(name, off, 2, len(v))
			}
			a.HasSmpMod = true
		case tagGmIdentity:
			if len(v) != 8 {
				return sizeErr(name, off, 8, len(v))
			}
			copy(a.GmIdentity[:], v)
			a.HasGmIdentity = true
		}
		pos = e.next
	}
	if !datSet {
		a.DatSet = ""
	}
	if seen&asduMandatory != asduMandatory {
		for i := range asduNames {
			if asduMandatory&(1<<i) != 0 && seen&(1<<i) == 0 {
				return decodeErr(ErrInvalidPDU, asduNames[i], base, "missing mandatory field")
			}
		}
	}
	return nil
}

func sizeErr(field string, off, want, got int) error {
	return decodeErr(ErrInvalidPDU, field, off, "%d octets, want %d", got, want)
}

// setVisible stores c in *dst unless it already holds the same text,
// reusing an interned copy from strs when there is one, so that known
// identifiers do not allocate.
func setVisible(dst *string, c []byte, strs map[string]string, field string, off int) error {
	if *dst == string(c) {
		return nil
	}
	if s, ok := strs[string(c)]; ok {
		*dst = s
		return nil
	}
	for _, ch := range c {
		if ch < 0x20 || ch > 0x7e {
			return decodeErr(ErrInvalidPDU, field, off, "non-visible character 0x%02x", ch)
		}
	}
	*dst = string(c)
	if strs != nil && len(strs) < maxInterned {
		strs[*dst] = *dst
	}
	return nil
}

func isVisible(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}

// String returns a short description of the frame for logs.
func (f *Frame) String() string {
	s := "SV " + f.Src.String() + " > " + f.Dst.String() + " appid=0x" + strconv.FormatUint(uint64(f.AppID), 16)
	for i := range f.PDU.ASDUs {
		a := &f.PDU.ASDUs[i]
		s += " [" + a.SvID + " smpCnt=" + strconv.Itoa(int(a.SmpCnt)) + "]"
	}
	return s
}
