// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"math"

	"github.com/otfabric/go-mms"
)

// SettingGroupInfo holds the state of a Setting Group Control Block
// (SGCB) as defined in IEC 61850-7-2 §16.
//
// The SGCB is a special control block under LLN0 at FC=SP that
// governs multiple setting groups. Each setting group provides a
// complete set of SE-constrained data attribute values; one group
// is "active" (read by clients via FC=SG) and one may be under
// edit (written via FC=SE).
//
// # MMS mapping
//
// The SGCB is mapped to MMS as "LLN0$SP$SGCB" with the following
// structure members in order:
//
//	[0] NumOfSG    (Unsigned8)  — total number of setting groups
//	[1] ActSG      (Unsigned8)  — currently active setting group
//	[2] EditSG     (Unsigned8)  — currently selected edit group (0 = none)
//	[3] CnfEdit    (BOOLEAN)    — confirm-edit flag
//	[4] LActTm     (UTCTime)    — last activation timestamp
//	[5] ResvTms    (Unsigned16) — reservation timeout in seconds (optional)
type SettingGroupInfo struct {
	// NumOfSGs is the total number of setting groups (1-based).
	NumOfSGs uint8

	// ActSG is the currently active setting group number (1-based).
	ActSG uint8

	// EditSG is the group currently selected for editing (0 = none).
	EditSG uint8

	// CnfEdit is true when the server has confirmed the edit.
	CnfEdit bool

	// LActTm is the time of the last SelectActiveSG (IEC 61850-7-2
	// 16.2.2.7), with its TimeQuality. Zero when the server sent a value
	// that is not a UtcTime.
	LActTm Timestamp

	// ResvTms is the reservation timeout in seconds (0 = no timeout).
	// This field is optional; not all servers expose it.
	ResvTms uint16
}

// sgcbItemID is the MMS item ID for the SGCB under LLN0.
const sgcbItemID = "LLN0$SP$SGCB"

// GetSettingGroupInfo reads the Setting Group Control Block (SGCB)
// from the given logical device and returns the current state.
//
// The SGCB is located at LLN0$SP$SGCB in every LD that supports
// setting groups. Returns an error if the SGCB cannot be read or
// decoded.
func (c *Client) GetSettingGroupInfo(ctx context.Context, ld string) (*SettingGroupInfo, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: get SGCB: %w: empty logical device name", ErrInvalidArgument)
	}

	result, err := c.mmsClient.Read(ctx, mms.ReadRequest{
		DomainID: c.ldDomain(ld),
		ItemID:   mms.ItemID(sgcbItemID),
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: get SGCB %s: %w", ld, err)
	}
	if result == nil || result.Value == nil {
		return nil, fmt.Errorf("iec61850: get SGCB %s: empty response", ld)
	}

	return decodeSGCB(result.Value)
}

// decodeSGCB decodes the MMS structure into a SettingGroupInfo.
func decodeSGCB(v *mms.Value) (*SettingGroupInfo, error) {
	members, ok := v.Structure()
	if !ok || len(members) < 5 {
		return nil, fmt.Errorf("iec61850: SGCB decode: expected structure with >=5 members, got %d", len(members))
	}

	info := &SettingGroupInfo{}

	// NumOfSG, ActSG and EditSG are INT8U and ResvTms INT16U (IEC
	// 61850-7-2 clause 16, SGCB class); a larger value from the server is
	// rejected rather than truncated to a different group number.
	var err error
	if info.NumOfSGs, err = sgcbUint8(members[0], "NumOfSGs"); err != nil {
		return nil, err
	}
	if info.ActSG, err = sgcbUint8(members[1], "ActSG"); err != nil {
		return nil, err
	}
	if info.EditSG, err = sgcbUint8(members[2], "EditSG"); err != nil {
		return nil, err
	}
	if b, ok := members[3].Bool(); ok {
		info.CnfEdit = b
	} else {
		return nil, fmt.Errorf("iec61850: SGCB decode: CnfEdit: expected boolean, got %s", members[3].Type())
	}
	// members[4] is LActTm (UtcTime, IEC 61850-8-1 Table 60).
	if ts, err := DecodeTimestamp(members[4]); err == nil {
		info.LActTm = ts
	}
	if len(members) > 5 {
		if u, ok := members[5].Uint32(); ok {
			if u > math.MaxUint16 {
				return nil, fmt.Errorf("iec61850: SGCB decode: ResvTms %d exceeds INT16U", u)
			}
			info.ResvTms = uint16(u)
		}
	}

	return info, nil
}

// sgcbUint8 decodes an INT8U SGCB member.
func sgcbUint8(v *mms.Value, name string) (uint8, error) {
	u, ok := v.Uint32()
	if !ok {
		return 0, fmt.Errorf("iec61850: SGCB decode: %s: expected unsigned, got %s", name, v.Type())
	}
	if u > math.MaxUint8 {
		return 0, fmt.Errorf("iec61850: SGCB decode: %s %d exceeds INT8U", name, u)
	}
	return uint8(u), nil
}

// sgcbObjectName returns an MMS domain-specific ObjectName for the SGCB.
func sgcbObjectName(domain mms.DomainID) mms.ObjectName {
	return mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: domain,
		ItemID: mms.ItemID(sgcbItemID),
	}
}

// writeSGCB writes one SGCB member as its own named variable,
// LLN0$SP$SGCB$<member> (IEC 61850-8-1 8.1.3.4.4.2, as libiec61850
// clients do). Before v1.17.0 the client wrote the SGCB with alternate
// access, which a server sees as a write of the whole structure.
func (c *Client) writeSGCB(ctx context.Context, ld, member string, v *mms.Value) error {
	_, err := c.mmsClient.Write(ctx, mms.WriteRequest{
		DomainID: c.ldDomain(ld),
		ItemID:   mms.ItemID(sgcbItemID + "$" + member),
		Value:    v,
	})
	return err
}

// SelectActiveSG writes a new active setting group number to the
// SGCB. The server activates the specified group, making its SE
// values available via FC=SG reads.
//
// The sg parameter must be in [1, NumOfSGs]. Use
// [Client.GetSettingGroupInfo] to discover the valid range.
func (c *Client) SelectActiveSG(ctx context.Context, ld string, sg uint8) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return fmt.Errorf("iec61850: select active SG: %w: empty logical device name", ErrInvalidArgument)
	}
	if sg == 0 {
		return fmt.Errorf("iec61850: select active SG: %w: setting group must be >= 1", ErrInvalidArgument)
	}

	err := c.writeSGCB(ctx, ld, "ActSG", mms.NewUnsigned(uint64(sg)))
	if err != nil {
		return fmt.Errorf("iec61850: select active SG %s group %d: %w", ld, sg, err)
	}

	c.logger.Debug("iec61850: select active SG", "ld", ld, "sg", sg)
	return nil
}

// SelectEditSG selects a setting group for editing. This reserves
// the edit session; the server sets EditSG in the SGCB to the
// requested group number. Only one client may hold an edit session
// at a time.
//
// After selecting, use [Client.GetEditSGValue] and
// [Client.SetEditSGValue] to read/write SE-constrained values for
// the edit group. Call [Client.ConfirmEditSG] to commit changes.
//
// The sg parameter is in [1, NumOfSGs], or 0 to cancel the edit and
// release the reservation (IEC 61850-7-2 16.3.3.1); see
// [Client.CancelEditSG]. Before v1.17.0, 0 was refused.
func (c *Client) SelectEditSG(ctx context.Context, ld string, sg uint8) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return fmt.Errorf("iec61850: select edit SG: %w: empty logical device name", ErrInvalidArgument)
	}

	err := c.writeSGCB(ctx, ld, "EditSG", mms.NewUnsigned(uint64(sg)))
	if err != nil {
		return fmt.Errorf("iec61850: select edit SG %s group %d: %w", ld, sg, err)
	}

	c.logger.Debug("iec61850: select edit SG", "ld", ld, "sg", sg)
	return nil
}

// CancelEditSG cancels the edit session this client holds: SelectEditSG
// with SettingGroupNumber 0 (IEC 61850-7-2 16.3.3.1).
func (c *Client) CancelEditSG(ctx context.Context, ld string) error {
	return c.SelectEditSG(ctx, ld, 0)
}

// ConfirmEditSG confirms the current edit session by writing
// CnfEdit=true to the SGCB. The server copies the edited SE values
// into the edit group's storage and clears the edit session.
//
// This must be called after [Client.SelectEditSG] and any
// [Client.SetEditSGValue] calls to commit the changes.
func (c *Client) ConfirmEditSG(ctx context.Context, ld string) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return fmt.Errorf("iec61850: confirm edit SG: %w: empty logical device name", ErrInvalidArgument)
	}

	err := c.writeSGCB(ctx, ld, "CnfEdit", mms.NewBoolean(true))
	if err != nil {
		return fmt.Errorf("iec61850: confirm edit SG %s: %w", ld, err)
	}

	c.logger.Debug("iec61850: confirm edit SG", "ld", ld)
	return nil
}

// GetEditSGValue reads a data attribute value from the current
// edit setting group (FC=SE). The ref must include LD, LN, and
// data path — the FC is forced to SE.
//
// An edit session must be active (via [Client.SelectEditSG]) for
// the server to return edit-group values.
func (c *Client) GetEditSGValue(ctx context.Context, ref Ref) (*Value, error) {
	seRef := ref
	seRef.FC = FCSE
	return c.Read(ctx, seRef)
}

// SetEditSGValue writes a data attribute value into the current
// edit setting group (FC=SE). The ref must include LD, LN, and
// data path — the FC is forced to SE.
//
// An edit session must be active (via [Client.SelectEditSG]).
// The change is not committed until [Client.ConfirmEditSG] is called.
func (c *Client) SetEditSGValue(ctx context.Context, ref Ref, value *mms.Value) error {
	seRef := ref
	seRef.FC = FCSE
	return c.Write(ctx, seRef, value)
}

// GetActiveSGValue reads a data attribute value from the currently
// active setting group (FC=SG).
//
// The ref must include LD, LN, and data path — the FC is forced
// to SG. This returns the active group's value regardless of any
// ongoing edit session.
func (c *Client) GetActiveSGValue(ctx context.Context, ref Ref) (*Value, error) {
	sgRef := ref
	sgRef.FC = FCSG
	return c.Read(ctx, sgRef)
}
