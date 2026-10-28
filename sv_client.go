// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/sv"
)

// MSVCB is a multicast Sampled Values control block as read over MMS
// (IEC 61850-8-1 MSVCB, functional constraint MS).
type MSVCB struct {
	// Reference is "LD/LLN0$MS$name" (bare LD name, without the IED
	// prefix).
	Reference string

	SvEna   bool
	MsvID   string
	DatSet  string
	ConfRev uint32
	SmpRate uint16
	OptFlds sv.OptFlds

	// SmpMod is SmpPerPeriod when the server omits it (Edition 1).
	SmpMod    sv.SmpMod
	HasSmpMod bool

	// DstAddress is the publisher's destination address.
	DstAddress SvCBDstAddress

	// NoASDU is zero when the server omits it.
	NoASDU uint16
}

// SvCBDstAddress is the DstAddress attribute of an SV control block.
type SvCBDstAddress struct {
	Addr     sv.MAC
	Priority uint8
	VID      uint16
	AppID    uint16
}

// MSVCBUpdate selects the writable MSVCB attributes to change. Nil
// fields are left unchanged. SvEna is written last so that the other
// changes (allowed only while disabled) take effect first.
type MSVCBUpdate struct {
	MsvID   *string
	DatSet  *string
	SmpRate *uint16
	OptFlds *sv.OptFlds
	SmpMod  *sv.SmpMod
	SvEna   *bool
}

// GetMSVCB reads a multicast SV control block. msvcbItemID is the MMS
// item, e.g. "LLN0$MS$MSVCB01".
func (c *Client) GetMSVCB(ctx context.Context, ld, msvcbItemID string) (*MSVCB, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" || msvcbItemID == "" {
		return nil, fmt.Errorf("iec61850: get MSVCB: %w: empty logical device or item", ErrInvalidArgument)
	}
	res, err := c.mmsClient.Read(ctx, mms.ReadRequest{DomainID: c.ldDomain(ld), ItemID: mms.ItemID(msvcbItemID)})
	if err != nil {
		return nil, fmt.Errorf("iec61850: get MSVCB %s/%s: %w", ld, msvcbItemID, err)
	}
	if res == nil || res.Value == nil {
		return nil, fmt.Errorf("iec61850: get MSVCB %s/%s: %w: missing value", ld, msvcbItemID, ErrProtocol)
	}
	cb, err := decodeMSVCB(ld+"/"+msvcbItemID, res.Value)
	if err != nil {
		return nil, fmt.Errorf("iec61850: get MSVCB %s/%s: %w", ld, msvcbItemID, err)
	}
	c.logger.Debug("iec61850: get MSVCB", "ref", cb.Reference, "svEna", cb.SvEna)
	return cb, nil
}

// SetMSVCB writes the selected MSVCB attributes, SvEna last.
func (c *Client) SetMSVCB(ctx context.Context, ld, msvcbItemID string, upd MSVCBUpdate) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" || msvcbItemID == "" {
		return fmt.Errorf("iec61850: set MSVCB: %w: empty logical device or item", ErrInvalidArgument)
	}
	type fw struct {
		name string
		v    *mms.Value
	}
	var writes []fw
	if upd.MsvID != nil {
		writes = append(writes, fw{"MsvID", mms.NewVisibleString(*upd.MsvID)})
	}
	if upd.DatSet != nil {
		writes = append(writes, fw{"DatSet", mms.NewVisibleString(*upd.DatSet)})
	}
	if upd.SmpRate != nil {
		writes = append(writes, fw{"SmpRate", mms.NewUnsigned(uint64(*upd.SmpRate))})
	}
	if upd.OptFlds != nil {
		writes = append(writes, fw{"OptFlds", servermodel.SvOptFlds(*upd.OptFlds).BitString()})
	}
	if upd.SmpMod != nil {
		writes = append(writes, fw{"SmpMod", mms.NewInteger(int64(*upd.SmpMod))})
	}
	if upd.SvEna != nil {
		writes = append(writes, fw{"SvEna", mms.NewBoolean(*upd.SvEna)})
	}
	for _, w := range writes {
		if _, err := c.mmsClient.Write(ctx, mms.WriteRequest{
			DomainID: c.ldDomain(ld), ItemID: mms.ItemID(msvcbItemID + "$" + w.name), Value: w.v,
		}); err != nil {
			return fmt.Errorf("iec61850: set MSVCB %s/%s.%s: %w", ld, msvcbItemID, w.name, err)
		}
	}
	c.logger.Debug("iec61850: set MSVCB", "ld", ld, "msvcb", msvcbItemID, "fields", len(writes))
	return nil
}

// decodeMSVCB decodes the positional MSVCB structure: SvEna, MsvID,
// DatSet, ConfRev, SmpRate, OptFlds, [SmpMod,] DstAddress{Addr,
// PRIORITY, VID, APPID}[, noASDU]. SmpMod is absent in Edition 1.
func decodeMSVCB(ref string, v *mms.Value) (*MSVCB, error) {
	bad := func(format string, args ...any) error {
		return &DecodeError{Ref: ref, Type: "MSVCB", Message: fmt.Sprintf(format, args...), Wrapped: ErrProtocol}
	}
	e, ok := v.Structure()
	if !ok || len(e) < 7 {
		return nil, bad("expected structure with at least 7 elements, got %v", v)
	}
	cb := &MSVCB{Reference: ref}
	okAll := true
	get := func(ok bool) { okAll = okAll && ok }
	cb.SvEna, ok = e[0].Bool()
	get(ok)
	cb.MsvID, ok = e[1].VisibleString()
	get(ok)
	cb.DatSet, ok = e[2].VisibleString()
	get(ok)
	cr, ok := cbUint(e[3])
	cb.ConfRev = uint32(cr)
	get(ok)
	sr, ok := cbUint(e[4])
	cb.SmpRate = uint16(sr)
	get(ok)
	opts, ok := servermodel.SvOptFldsFromValue(e[5])
	cb.OptFlds = sv.OptFlds(opts)
	get(ok)
	if !okAll {
		return nil, bad("unexpected attribute types")
	}
	i := 6
	if m, ok := e[i].Int64(); ok {
		cb.SmpMod, cb.HasSmpMod = sv.SmpMod(m), true
		i++
	} else if m, ok := e[i].Uint64(); ok {
		cb.SmpMod, cb.HasSmpMod = sv.SmpMod(m), true
		i++
	}
	if i >= len(e) {
		return nil, bad("missing DstAddress")
	}
	dst, ok := e[i].Structure()
	if !ok || len(dst) < 4 {
		return nil, bad("DstAddress: expected structure of 4 elements")
	}
	addr, ok := dst[0].OctetString()
	if !ok || len(addr) != 6 {
		return nil, bad("DstAddress.Addr: expected 6 octets")
	}
	copy(cb.DstAddress.Addr[:], addr)
	p, ok1 := cbUint(dst[1])
	vid, ok2 := cbUint(dst[2])
	app, ok3 := cbUint(dst[3])
	if !ok1 || !ok2 || !ok3 {
		return nil, bad("DstAddress: unexpected attribute types")
	}
	cb.DstAddress.Priority, cb.DstAddress.VID, cb.DstAddress.AppID = uint8(p), uint16(vid), uint16(app)
	if i+1 < len(e) {
		n, _ := cbUint(e[i+1])
		cb.NoASDU = uint16(n)
	}
	return cb, nil
}

// cbUint reads an unsigned control-block attribute. Some servers
// (libiec61850's MSVCB ConfRev, SmpRate and noASDU) encode them as MMS
// Integer; a non-negative Integer is accepted.
func cbUint(v *mms.Value) (uint64, bool) {
	if u, ok := v.Uint64(); ok {
		return u, true
	}
	if i, ok := v.Int64(); ok && i >= 0 {
		return uint64(i), true
	}
	return 0, false
}
