// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
)

// GoCB is a GOOSE control block as read over MMS (IEC 61850-8-1 GoCB,
// functional constraint GO).
type GoCB struct {
	// Reference is "LD/LLN0$GO$name" (bare LD name, without the IED
	// prefix).
	Reference string

	GoEna   bool
	GoID    string
	DatSet  string
	ConfRev uint32
	NdsCom  bool

	// DstAddress is the publisher's destination address.
	DstAddress GoCBDstAddress

	// MinTime, MaxTime and FixedOffs are optional in the MMS structure;
	// HasTimes reports whether MinTime/MaxTime were present.
	MinTime, MaxTime uint32
	HasTimes         bool
	FixedOffs        bool
}

// GoCBDstAddress is the DstAddress attribute of a GoCB.
type GoCBDstAddress struct {
	Addr     goose.MAC
	Priority uint8
	VID      uint16
	AppID    uint16
}

// GoCBUpdate selects the writable GoCB attributes to change. Nil
// fields are left unchanged. GoEna is written last so that GoID and
// DatSet changes (allowed only while disabled) take effect first.
type GoCBUpdate struct {
	GoID   *string
	DatSet *string
	GoEna  *bool
}

// GetGoCB reads a GOOSE control block. gocbItemID is the MMS item,
// e.g. "LLN0$GO$gcb01".
func (c *Client) GetGoCB(ctx context.Context, ld, gocbItemID string) (*GoCB, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" || gocbItemID == "" {
		return nil, fmt.Errorf("iec61850: get GoCB: %w: empty logical device or item", ErrInvalidArgument)
	}
	res, err := c.mmsClient.Read(ctx, mms.ReadRequest{DomainID: c.ldDomain(ld), ItemID: mms.ItemID(gocbItemID)})
	if err != nil {
		return nil, fmt.Errorf("iec61850: get GoCB %s/%s: %w", ld, gocbItemID, err)
	}
	if res == nil || res.Value == nil {
		return nil, fmt.Errorf("iec61850: get GoCB %s/%s: %w: missing value", ld, gocbItemID, ErrProtocol)
	}
	cb, err := decodeGoCB(ld+"/"+gocbItemID, res.Value)
	if err != nil {
		return nil, fmt.Errorf("iec61850: get GoCB %s/%s: %w", ld, gocbItemID, err)
	}
	c.logger.Debug("iec61850: get GoCB", "ref", cb.Reference, "goEna", cb.GoEna)
	return cb, nil
}

// SetGoCB writes the selected GoCB attributes, GoEna last.
func (c *Client) SetGoCB(ctx context.Context, ld, gocbItemID string, upd GoCBUpdate) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" || gocbItemID == "" {
		return fmt.Errorf("iec61850: set GoCB: %w: empty logical device or item", ErrInvalidArgument)
	}
	type fw struct {
		name string
		v    *mms.Value
	}
	var writes []fw
	if upd.GoID != nil {
		writes = append(writes, fw{"GoID", mms.NewVisibleString(*upd.GoID)})
	}
	if upd.DatSet != nil {
		writes = append(writes, fw{"DatSet", mms.NewVisibleString(*upd.DatSet)})
	}
	if upd.GoEna != nil {
		writes = append(writes, fw{"GoEna", mms.NewBoolean(*upd.GoEna)})
	}
	for _, w := range writes {
		if _, err := c.mmsClient.Write(ctx, mms.WriteRequest{
			DomainID: c.ldDomain(ld), ItemID: mms.ItemID(gocbItemID + "$" + w.name), Value: w.v,
		}); err != nil {
			return fmt.Errorf("iec61850: set GoCB %s/%s.%s: %w", ld, gocbItemID, w.name, err)
		}
	}
	c.logger.Debug("iec61850: set GoCB", "ld", ld, "gocb", gocbItemID, "fields", len(writes))
	return nil
}

// decodeGoCB decodes the positional GoCB structure: GoEna, GoID,
// DatSet, ConfRev, NdsCom, DstAddress{Addr, PRIORITY, VID, APPID}
// [, MinTime, MaxTime, FixedOffs].
func decodeGoCB(ref string, v *mms.Value) (*GoCB, error) {
	bad := func(format string, args ...any) error {
		return &DecodeError{Ref: ref, Type: "GoCB", Message: fmt.Sprintf(format, args...), Wrapped: ErrProtocol}
	}
	e, ok := v.Structure()
	if !ok || len(e) < 6 {
		return nil, bad("expected structure with at least 6 elements, got %v", v)
	}
	cb := &GoCB{Reference: ref}
	var okAll = true
	get := func(ok bool) { okAll = okAll && ok }
	var b bool
	b, ok = e[0].Bool()
	cb.GoEna = b
	get(ok)
	cb.GoID, ok = e[1].VisibleString()
	get(ok)
	cb.DatSet, ok = e[2].VisibleString()
	get(ok)
	var u uint64
	u, ok = cbUint(e[3])
	cb.ConfRev = uint32(u)
	get(ok)
	cb.NdsCom, ok = e[4].Bool()
	get(ok)
	if !okAll {
		return nil, bad("unexpected attribute types")
	}
	dst, ok := e[5].Structure()
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
	if len(e) >= 8 {
		mn, ok1 := cbUint(e[6])
		mx, ok2 := cbUint(e[7])
		if ok1 && ok2 {
			cb.MinTime, cb.MaxTime, cb.HasTimes = uint32(mn), uint32(mx), true
		}
	}
	if len(e) >= 9 {
		cb.FixedOffs, _ = e[8].Bool()
	}
	return cb, nil
}
