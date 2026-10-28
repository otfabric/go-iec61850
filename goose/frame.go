// SPDX-License-Identifier: MIT

package goose

import (
	"encoding/binary"
	"strconv"
	"time"

	"github.com/otfabric/go-mms"
)

// Ethernet and GOOSE framing constants (IEC 61850-8-1 Annex C).
const (
	// EtherType is the GOOSE EtherType.
	EtherType uint16 = 0x88B8

	// EtherTypeVLAN is the IEEE 802.1Q tag protocol identifier.
	EtherTypeVLAN uint16 = 0x8100

	// SimulationBit is the bit in Reserved1 that marks a simulated
	// message (IEC 61850-8-1 Ed2.1 "S" bit).
	SimulationBit uint16 = 0x8000

	// MaxPayload is the maximum Ethernet payload (after the EtherType)
	// produced by the encoder.
	MaxPayload = 1500

	// minFrame is the minimum Ethernet frame length without FCS; the
	// encoder pads shorter frames.
	minFrame = 60

	headerLen = 8 // APPID + Length + Reserved1 + Reserved2
)

// APDU tags.
const (
	tagGSEMngtPDU byte = 0x60 // [APPLICATION 0]
	tagGoosePDU   byte = 0x61 // [APPLICATION 1]
)

// goosePdu field tags (context-specific, IMPLICIT).
const (
	tagGocbRef      byte = 0x80
	tagTAL          byte = 0x81
	tagDatSet       byte = 0x82
	tagGoID         byte = 0x83
	tagT            byte = 0x84
	tagStNum        byte = 0x85
	tagSqNum        byte = 0x86
	tagSimulation   byte = 0x87
	tagConfRev      byte = 0x88
	tagNdsCom       byte = 0x89
	tagNumEntries   byte = 0x8a
	tagAllData      byte = 0xab
	tagSecurity     byte = 0x8c
	tagSecurityCons byte = 0xac
)

// VLAN is an IEEE 802.1Q tag.
type VLAN struct {
	// Priority is the 3-bit priority code point (0-7). IEC 61850-8-1
	// recommends 4 for GOOSE.
	Priority uint8

	// DEI is the drop eligible indicator.
	DEI bool

	// ID is the 12-bit VLAN identifier (0-4095). 0 means priority
	// tagged only.
	ID uint16
}

// Frame is a decoded or to-be-encoded GOOSE Ethernet frame.
type Frame struct {
	// Dst and Src are the Ethernet destination and source addresses.
	Dst, Src MAC

	// VLAN is the 802.1Q tag, or nil for an untagged frame.
	VLAN *VLAN

	// AppID is the application identifier (GOOSE range 0x0000-0x3FFF).
	AppID uint16

	// Simulation reports the Reserved1 simulation ("S") bit.
	Simulation bool

	// Reserved1 holds the remaining Reserved1 bits (the simulation bit
	// is masked off and reported in Simulation).
	Reserved1 uint16

	// Reserved2 holds the Reserved2 field verbatim.
	Reserved2 uint16

	// PDU is the goosePdu.
	PDU PDU
}

// PDU is the IECGoosePdu (IEC 61850-8-1 Annex A).
type PDU struct {
	// GocbRef is the GoCB reference, e.g. "IED1LD0/LLN0$GO$gcb1".
	GocbRef string

	// TimeAllowedToLive is the time in milliseconds the subscriber
	// shall wait for the next message before declaring the stream
	// lost.
	TimeAllowedToLive uint32

	// DatSet is the dataset reference, e.g. "IED1LD0/LLN0$ds1".
	DatSet string

	// GoID is the GOOSE identifier (optional on the wire; omitted when
	// empty).
	GoID string

	// T is the time of the last state change (stNum increment).
	T time.Time

	// TimeQuality is the TimeQuality octet of T.
	TimeQuality uint8

	// StNum is the state number, incremented on every data change.
	StNum uint32

	// SqNum is the sequence number, incremented on every retransmission
	// and reset to 0 on a state change.
	SqNum uint32

	// Test is the goosePdu [7] BOOLEAN ("test" in Ed1, "simulation" in
	// Ed2). Encoded always.
	Test bool

	// ConfRev is the configuration revision of the dataset.
	ConfRev uint32

	// NdsCom ("needs commissioning") is encoded always.
	NdsCom bool

	// NumDatSetEntries is the number of top-level allData entries. On
	// encode, zero means len(AllData); a non-zero value is sent
	// verbatim (useful for negative tests).
	NumDatSetEntries uint32

	// AllData holds the dataset member values.
	AllData []*mms.Value

	// Security is the raw content of the optional [12] security field.
	// It is carried but not interpreted.
	Security []byte
}

// Encode returns the wire encoding of f. Frames shorter than 60 octets
// are zero-padded. It fails with [ErrLimitExceeded] if the Ethernet
// payload would exceed [MaxPayload] and with [ErrInvalidValue] for
// values that cannot be encoded.
func Encode(f *Frame) ([]byte, error) { return AppendFrame(nil, f) }

// AppendFrame appends the wire encoding of f to dst.
func AppendFrame(dst []byte, f *Frame) ([]byte, error) {
	if f == nil {
		return dst, errValue("nil frame")
	}
	apdu, err := EncodePDU(&f.PDU)
	if err != nil {
		return dst, err
	}
	payload := headerLen + len(apdu)
	if payload > MaxPayload {
		return dst, errLimit("GOOSE payload of %d octets exceeds %d", payload, MaxPayload)
	}
	start := len(dst)
	dst = append(dst, f.Dst[:]...)
	dst = append(dst, f.Src[:]...)
	if f.VLAN != nil {
		if f.VLAN.Priority > 7 || f.VLAN.ID > 0x0fff {
			return dst[:start], errValue("VLAN priority %d / id %d out of range", f.VLAN.Priority, f.VLAN.ID)
		}
		tci := uint16(f.VLAN.Priority)<<13 | f.VLAN.ID
		if f.VLAN.DEI {
			tci |= 0x1000
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
	dst = append(dst, apdu...)
	for len(dst)-start < minFrame {
		dst = append(dst, 0)
	}
	return dst, nil
}

// EncodePDU returns the BER encoding of the goosePdu APDU
// ([APPLICATION 1]).
func EncodePDU(p *PDU) ([]byte, error) {
	var allData []byte
	var err error
	for i, v := range p.AllData {
		if allData, err = appendData(allData, v, 1); err != nil {
			return nil, &EncodeError{Field: "allData", Index: i, Err: err}
		}
	}
	n := p.NumDatSetEntries
	if n == 0 {
		n = uint32(len(p.AllData))
	}
	c := make([]byte, 0, 64+len(p.GocbRef)+len(p.DatSet)+len(p.GoID)+len(allData))
	c = appendTLV(c, tagGocbRef, []byte(p.GocbRef))
	c = appendTLV(c, tagTAL, appendUnsigned(nil, uint64(p.TimeAllowedToLive)))
	c = appendTLV(c, tagDatSet, []byte(p.DatSet))
	if p.GoID != "" {
		c = appendTLV(c, tagGoID, []byte(p.GoID))
	}
	c = appendTLV(c, tagT, encodeUTCTime(p.T, p.TimeQuality))
	c = appendTLV(c, tagStNum, appendUnsigned(nil, uint64(p.StNum)))
	c = appendTLV(c, tagSqNum, appendUnsigned(nil, uint64(p.SqNum)))
	c = appendTLV(c, tagSimulation, []byte{boolOctet(p.Test)})
	c = appendTLV(c, tagConfRev, appendUnsigned(nil, uint64(p.ConfRev)))
	c = appendTLV(c, tagNdsCom, []byte{boolOctet(p.NdsCom)})
	c = appendTLV(c, tagNumEntries, appendUnsigned(nil, uint64(n)))
	c = appendTLV(c, tagAllData, allData)
	if p.Security != nil {
		c = appendTLV(c, tagSecurity, p.Security)
	}
	out := make([]byte, 0, 1+lengthSize(len(c))+len(c))
	return appendTLV(out, tagGoosePDU, c), nil
}

// EncodeError reports which part of a PDU could not be encoded.
type EncodeError struct {
	Field string
	Index int
	Err   error
}

func (e *EncodeError) Error() string {
	return "goose: encode " + e.Field + "[" + strconv.Itoa(e.Index) + "]: " + e.Err.Error()
}

func (e *EncodeError) Unwrap() error { return e.Err }

func boolOctet(b bool) byte {
	if b {
		return 0xff
	}
	return 0
}

// Decode strictly decodes a GOOSE Ethernet frame (without FCS) using
// [DefaultLimits]. Returned strings and values do not alias b.
func Decode(b []byte) (*Frame, error) { return DecodeWithLimits(b, DefaultLimits) }

// DecodeWithLimits is [Decode] with explicit decoder limits.
func DecodeWithLimits(b []byte, lim Limits) (*Frame, error) {
	f := &Frame{}
	if len(b) < 14 {
		return nil, decodeErr(ErrInvalidFrame, "ethernet", 0, "frame of %d octets is too short", len(b))
	}
	copy(f.Dst[:], b[0:6])
	copy(f.Src[:], b[6:12])
	pos := 12
	et := binary.BigEndian.Uint16(b[pos:])
	pos += 2
	if et == EtherTypeVLAN {
		if len(b) < pos+4 {
			return nil, decodeErr(ErrInvalidFrame, "802.1Q", pos, "truncated VLAN tag")
		}
		tci := binary.BigEndian.Uint16(b[pos:])
		f.VLAN = &VLAN{Priority: uint8(tci >> 13), DEI: tci&0x1000 != 0, ID: tci & 0x0fff}
		pos += 2
		et = binary.BigEndian.Uint16(b[pos:])
		pos += 2
	}
	if et != EtherType {
		return nil, decodeErr(ErrNotGOOSE, "ethertype", pos-2, "EtherType 0x%04x", et)
	}
	if len(b) < pos+headerLen {
		return nil, decodeErr(ErrInvalidFrame, "header", pos, "truncated GOOSE header")
	}
	f.AppID = binary.BigEndian.Uint16(b[pos:])
	length := int(binary.BigEndian.Uint16(b[pos+2:]))
	r1 := binary.BigEndian.Uint16(b[pos+4:])
	f.Simulation = r1&SimulationBit != 0
	f.Reserved1 = r1 &^ SimulationBit
	f.Reserved2 = binary.BigEndian.Uint16(b[pos+6:])
	if length < headerLen {
		return nil, decodeErr(ErrInvalidLength, "length", pos+2, "length %d below header size", length)
	}
	if pos+length > len(b) {
		return nil, decodeErr(ErrInvalidLength, "length", pos+2, "length %d exceeds frame (%d octets available)", length, len(b)-pos)
	}
	apduStart := pos + headerLen
	apdu := b[apduStart : pos+length]
	if err := decodeAPDU(apdu, apduStart, lim, &f.PDU); err != nil {
		return nil, err
	}
	return f, nil
}

// DecodePDU strictly decodes a goosePdu APDU (starting at the
// [APPLICATION 1] tag) using [DefaultLimits].
func DecodePDU(b []byte) (*PDU, error) {
	p := &PDU{}
	if err := decodeAPDU(b, 0, DefaultLimits, p); err != nil {
		return nil, err
	}
	return p, nil
}

func decodeAPDU(b []byte, base int, lim Limits, p *PDU) error {
	if len(b) == 0 {
		return decodeErr(ErrInvalidLength, "apdu", base, "empty APDU")
	}
	top, err := readTLV(b, 0, base, "goosePdu")
	if err != nil {
		return err
	}
	switch top.tag {
	case tagGoosePDU:
	case tagGSEMngtPDU:
		return decodeErr(ErrUnsupportedPDU, "apdu", base, "GSE management PDU")
	default:
		return decodeErr(ErrInvalidPDU, "apdu", base, "unexpected APDU tag 0x%02x", top.tag)
	}
	if top.next != len(b) {
		return decodeErr(ErrInvalidLength, "apdu", base+top.next, "%d octets after goosePdu", len(b)-top.next)
	}
	c := top.content
	cbase := base + top.next - len(c)
	pos := 0
	last := -1 // index into fieldOrder of the last decoded field
	var seen uint16
	for pos < len(c) {
		e, err := readTLV(c, pos, cbase, "goosePdu")
		if err != nil {
			return err
		}
		idx := fieldIndex(e.tag)
		off := cbase + e.off
		if idx < 0 {
			return decodeErr(ErrInvalidPDU, "goosePdu", off, "unexpected field tag 0x%02x", e.tag)
		}
		if idx <= last {
			return decodeErr(ErrInvalidPDU, fieldNames[idx], off, "duplicate or out-of-order field")
		}
		last = idx
		seen |= 1 << idx
		name := fieldNames[idx]
		switch e.tag {
		case tagGocbRef:
			p.GocbRef, err = visibleString(e.content, name, off)
		case tagTAL:
			p.TimeAllowedToLive, err = decodeUint32(e.content, name, off)
		case tagDatSet:
			p.DatSet, err = visibleString(e.content, name, off)
		case tagGoID:
			p.GoID, err = visibleString(e.content, name, off)
		case tagT:
			if len(e.content) != 8 {
				err = decodeErr(ErrInvalidPDU, name, off, "UtcTime of %d octets", len(e.content))
			} else {
				p.T, p.TimeQuality = decodeUTCTime(e.content)
			}
		case tagStNum:
			p.StNum, err = decodeUint32(e.content, name, off)
		case tagSqNum:
			p.SqNum, err = decodeUint32(e.content, name, off)
		case tagSimulation:
			p.Test, err = decodeBool(e.content, name, off)
		case tagConfRev:
			p.ConfRev, err = decodeUint32(e.content, name, off)
		case tagNdsCom:
			p.NdsCom, err = decodeBool(e.content, name, off)
		case tagNumEntries:
			p.NumDatSetEntries, err = decodeUint32(e.content, name, off)
		case tagAllData:
			d := dataDecoder{lim: lim}
			p.AllData, err = d.decodeSeq(e.content, cbase+e.next-len(e.content), 1)
		case tagSecurity, tagSecurityCons:
			p.Security = append([]byte{}, e.content...)
		}
		if err != nil {
			return err
		}
		pos = e.next
	}
	for i, mandatory := range fieldMandatory {
		if mandatory && seen&(1<<i) == 0 {
			return decodeErr(ErrInvalidPDU, fieldNames[i], base, "missing mandatory field")
		}
	}
	return nil
}

var fieldTags = []byte{tagGocbRef, tagTAL, tagDatSet, tagGoID, tagT, tagStNum, tagSqNum, tagSimulation, tagConfRev, tagNdsCom, tagNumEntries, tagAllData, tagSecurity}
var fieldNames = []string{"gocbRef", "timeAllowedToLive", "datSet", "goID", "t", "stNum", "sqNum", "simulation", "confRev", "ndsCom", "numDatSetEntries", "allData", "security"}
var fieldMandatory = []bool{true, true, true, false, true, true, true, false, true, false, true, true, false}

func fieldIndex(tag byte) int {
	if tag == tagSecurityCons {
		return 12
	}
	for i, t := range fieldTags {
		if t == tag {
			return i
		}
	}
	return -1
}

func visibleString(c []byte, field string, off int) (string, error) {
	for _, ch := range c {
		if ch < 0x20 || ch > 0x7e {
			return "", decodeErr(ErrInvalidPDU, field, off, "non-visible character 0x%02x", ch)
		}
	}
	return string(c), nil
}
