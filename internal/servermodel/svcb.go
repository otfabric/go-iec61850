// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"fmt"
	"math"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// SvCBDef defines a multicast Sampled Values control block (SCL
// SampledValueControl plus the SMV address of its ConnectedAP).
type SvCBDef struct {
	// Name is the control block name (e.g. "MSVCB01").
	Name string

	// SvID is the SCL smvID attribute (MsvID).
	SvID string

	// DatSet is the dataset name (local to the LN).
	DatSet string

	// ConfRev is the configuration revision.
	ConfRev uint32

	// SmpRate is the sample rate in units of SmpMod.
	SmpRate uint16

	// SmpMod is 0 (samples per nominal period), 1 (samples per second)
	// or 2 (seconds per sample).
	SmpMod uint8

	// NoASDU is the number of ASDUs per frame (SCL nofASDU).
	NoASDU uint16

	// OptFlds are the optional ASDU fields (MSVCB OptFlds).
	OptFlds SvOptFlds

	// HasAddress is true when a valid SMV address was found.
	HasAddress bool

	// HasVLAN is true when the address specifies VLAN-ID or
	// VLAN-PRIORITY.
	HasVLAN bool

	// DstAddr, AppID, VLANID and VLANPriority are the SMV address.
	DstAddr      [6]byte
	AppID        uint16
	VLANID       uint16
	VLANPriority uint8
}

// SvOptFlds is the OptFlds attribute of an SvCB (IEC 61850-8-1): bit 0
// refresh-time, 1 sample-synchronized (reserved since Edition 2), 2
// sample-rate, 3 data-set, 4 security.
type SvOptFlds struct {
	RefreshTime, SampleSynchronized, SampleRate, DataSet, Security bool
}

// BitString returns the 5-bit MMS encoding.
func (o SvOptFlds) BitString() *mms.Value {
	var b byte
	for i, on := range []bool{o.RefreshTime, o.SampleSynchronized, o.SampleRate, o.DataSet, o.Security} {
		if on {
			b |= 0x80 >> i
		}
	}
	return mms.NewBitStringWithLength([]byte{b}, 5)
}

// SvOptFldsFromValue decodes an OptFlds bit string; ok is false for
// another type.
func SvOptFldsFromValue(v *mms.Value) (SvOptFlds, bool) {
	b, ok := v.BitString()
	if !ok {
		return SvOptFlds{}, false
	}
	bit := func(i int) bool { return len(b) > 0 && b[0]&(0x80>>i) != 0 }
	return SvOptFlds{RefreshTime: bit(0), SampleSynchronized: bit(1), SampleRate: bit(2), DataSet: bit(3), Security: bit(4)}, true
}

// SmpModFromSCL maps the SCL smpMod attribute to the MSVCB SmpMod
// value. ok is false for an unknown value.
func SmpModFromSCL(s string) (uint8, bool) {
	switch s {
	case "", "SmpPerPeriod":
		return 0, true
	case "SmpPerSec":
		return 1, true
	case "SecPerSmp":
		return 2, true
	}
	return 0, false
}

// convertSvCB builds an SvCBDef from an SCL SampledValueControl and its
// SMV address (nil when the Communication section has none). Problems
// are reported as warnings.
func convertSvCB(sc *scl.SMVControl, smv *scl.SMVAddress, where string, warnings *[]string) SvCBDef {
	def := SvCBDef{
		Name: sc.Name, SvID: sc.SmvID, DatSet: sc.DatSet, ConfRev: sc.ConfRev,
		NoASDU: 1,
		OptFlds: SvOptFlds{
			RefreshTime: sc.SmvOpts.RefreshTime, SampleSynchronized: sc.SmvOpts.SampleSynchronized,
			SampleRate: sc.SmvOpts.SampleRate, DataSet: sc.SmvOpts.DataSet, Security: sc.SmvOpts.Security,
		},
	}
	warn := func(format string, args ...any) {
		if warnings != nil {
			*warnings = append(*warnings, where+": "+fmt.Sprintf(format, args...))
		}
	}
	if sc.SmpRate > math.MaxUint16 {
		warn("SampledValueControl %q: smpRate %d out of range", sc.Name, sc.SmpRate)
	} else {
		def.SmpRate = uint16(sc.SmpRate)
	}
	if sc.NofASDU > 0 && sc.NofASDU <= math.MaxUint16 {
		def.NoASDU = uint16(sc.NofASDU)
	} else if sc.NofASDU != 0 {
		warn("SampledValueControl %q: nofASDU %d out of range", sc.Name, sc.NofASDU)
	}
	var ok bool
	if def.SmpMod, ok = SmpModFromSCL(sc.SmpMod); !ok {
		warn("SampledValueControl %q: unknown smpMod %q", sc.Name, sc.SmpMod)
	}
	if smv == nil {
		warn("SampledValueControl %q has no SMV address in the Communication section", sc.Name)
		return def
	}
	addr, err := ParseGSEAddress(smv.Address)
	if err != nil {
		warn("SMV %q: %v", sc.Name, err)
		return def
	}
	def.HasAddress = true
	def.DstAddr, def.AppID, def.VLANID, def.VLANPriority, def.HasVLAN = addr.Dst, addr.AppID, addr.VLANID, addr.VLANPriority, addr.HasVLAN
	return def
}

// SvCBWritableFields lists the MSVCB attributes a client may write.
// All other attributes are read-only (object-access-denied).
var SvCBWritableFields = map[string]bool{"SvEna": true, "MsvID": true, "DatSet": true, "SmpRate": true, "OptFlds": true, "SmpMod": true}

// SvCBItemID returns the MMS item ID of an MSVCB ("LLN0$MS$MSVCB01").
func SvCBItemID(lnName, cbName string) string { return lnName + "$MS$" + cbName }

// MSVCB MMS field layout (IEC 61850-8-1): SvEna, MsvID, DatSet,
// ConfRev, SmpRate, OptFlds, SmpMod, DstAddress{Addr, PRIORITY, VID,
// APPID}, noASDU.
func buildSvCBFields(d *SvCBDef, ldName, lnName string) []rcbField {
	dst := mms.NewStructure([]*mms.Value{
		mms.NewOctetString(d.DstAddr[:]),
		mms.NewUnsigned(uint64(d.VLANPriority)),
		mms.NewUnsigned(uint64(d.VLANID)),
		mms.NewUnsigned(uint64(d.AppID)),
	})
	return []rcbField{
		{"SvEna", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(false)},
		{"MsvID", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(d.SvID)},
		{"DatSet", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(QualifiedDatSet(ldName, lnName, d.DatSet))},
		{"ConfRev", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(d.ConfRev))},
		{"SmpRate", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}, mms.NewUnsigned(uint64(d.SmpRate))},
		{"OptFlds", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 5}, d.OptFlds.BitString()},
		{"SmpMod", mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 8}, mms.NewInteger(int64(d.SmpMod))},
		{"DstAddress", dstAddressTS, dst},
		{"noASDU", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 16}, mms.NewUnsigned(uint64(d.NoASDU))},
	}
}

// registerSvCB registers the MSVCB structure and its attributes; see
// registerGoCB for the write semantics.
func registerSvCB(srv *mms.Server, vs *ValueStore, ldName, lnName string, d *SvCBDef) error {
	return registerCtrlBlock(srv, vs, ldName, SvCBItemID(lnName, d.Name), "SvCB", buildSvCBFields(d, ldName, lnName), SvCBWritableFields)
}

// registerMSGroup registers the "LN$MS" FC-group variable. ok is false
// when the LN has no SvCBs.
func registerMSGroup(srv *mms.Server, vs *ValueStore, ldName string, ln *LogicalNode) (mms.TypeSpecElement, func(context.Context) (*mms.Value, error), bool, error) {
	names := make([]string, len(ln.SvCBs))
	structs := make([]func() (mms.TypeSpec, func(context.Context) (*mms.Value, error)), len(ln.SvCBs))
	for i := range ln.SvCBs {
		d := &ln.SvCBs[i]
		names[i] = d.Name
		structs[i] = func() (mms.TypeSpec, func(context.Context) (*mms.Value, error)) {
			return cbStruct(vs, buildSvCBFields(d, ldName, ln.Name), ldName, SvCBItemID(ln.Name, d.Name))
		}
	}
	return registerFCGroup(srv, ldName, ln.Name, "MS", names, structs)
}
