// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
)

// --- Report field masks and trigger options ---

// OptFlds represents the optional fields bit mask of a report control
// block. Each bit controls whether the corresponding field is included
// in generated reports.
type OptFlds uint16

// Optional field flags for [OptFlds].
const (
	OptFldSeqNum       OptFlds = 1 << 0 // Sequence number
	OptFldTimeStamp    OptFlds = 1 << 1 // Report timestamp
	OptFldReasonCode   OptFlds = 1 << 2 // Inclusion reason per member
	OptFldDataSet      OptFlds = 1 << 3 // Dataset reference
	OptFldDataRef      OptFlds = 1 << 4 // Data references
	OptFldBufOvfl      OptFlds = 1 << 5 // Buffer overflow flag
	OptFldEntryID      OptFlds = 1 << 6 // Entry ID (BRCB)
	OptFldConfRev      OptFlds = 1 << 7 // Configuration revision
	OptFldSegmentation OptFlds = 1 << 8 // Segmentation info
)

// Has reports whether the specified optional field flag is set.
func (o OptFlds) Has(flag OptFlds) bool { return o&flag != 0 }

// String returns a comma-separated list of set optional field names.
func (o OptFlds) String() string {
	var parts []string
	flags := []struct {
		f    OptFlds
		name string
	}{
		{OptFldSeqNum, "seq-num"},
		{OptFldTimeStamp, "timestamp"},
		{OptFldReasonCode, "reason-code"},
		{OptFldDataSet, "data-set"},
		{OptFldDataRef, "data-ref"},
		{OptFldBufOvfl, "buf-ovfl"},
		{OptFldEntryID, "entry-id"},
		{OptFldConfRev, "conf-rev"},
		{OptFldSegmentation, "segmentation"},
	}
	for _, fl := range flags {
		if o.Has(fl.f) {
			parts = append(parts, fl.name)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

// TrgOps represents the trigger options bit mask of a report control
// block.
type TrgOps uint8

// Trigger option flags for [TrgOps].
const (
	TrgOpDataChanged    TrgOps = 1 << 0 // Data change
	TrgOpQualityChanged TrgOps = 1 << 1 // Quality change
	TrgOpDataUpdate     TrgOps = 1 << 2 // Data update
	TrgOpIntegrity      TrgOps = 1 << 3 // Integrity period
	TrgOpGI             TrgOps = 1 << 4 // General interrogation
)

// Has reports whether the specified trigger option is set.
func (t TrgOps) Has(flag TrgOps) bool { return t&flag != 0 }

// String returns a comma-separated list of set trigger option names.
func (t TrgOps) String() string {
	var parts []string
	flags := []struct {
		f    TrgOps
		name string
	}{
		{TrgOpDataChanged, "data-changed"},
		{TrgOpQualityChanged, "quality-changed"},
		{TrgOpDataUpdate, "data-update"},
		{TrgOpIntegrity, "integrity"},
		{TrgOpGI, "GI"},
	}
	for _, fl := range flags {
		if t.Has(fl.f) {
			parts = append(parts, fl.name)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

// ReasonCode represents the reason for inclusion of a data set member
// in a report.
type ReasonCode uint8

// Reason code values for report member inclusion.
const (
	ReasonDataChanged    ReasonCode = 1 << 0 // Included due to data change
	ReasonQualityChanged ReasonCode = 1 << 1 // Included due to quality change
	ReasonDataUpdate     ReasonCode = 1 << 2 // Included due to data update
	ReasonIntegrity      ReasonCode = 1 << 3 // Included due to integrity scan
	ReasonGI             ReasonCode = 1 << 4 // Included due to GI
)

// String returns a comma-separated list of set reason flags, matching
// the multi-bit style used by [OptFlds.String] and [TrgOps.String].
func (r ReasonCode) String() string {
	if r == 0 {
		return "not-included"
	}
	var parts []string
	flags := []struct {
		f    ReasonCode
		name string
	}{
		{ReasonDataChanged, "data-change"},
		{ReasonQualityChanged, "quality-change"},
		{ReasonDataUpdate, "data-update"},
		{ReasonIntegrity, "integrity"},
		{ReasonGI, "GI"},
	}
	for _, fl := range flags {
		if r&fl.f != 0 {
			parts = append(parts, fl.name)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("reason(%d)", r)
	}
	return strings.Join(parts, ",")
}

// --- Report Control Block ---

// RCBType distinguishes buffered from unbuffered report control blocks.
type RCBType int

const (
	// RCBBuffered is a buffered report control block (BRCB, FC=BR).
	RCBBuffered RCBType = iota
	// RCBUnbuffered is an unbuffered report control block (URCB, FC=RP).
	RCBUnbuffered
)

// String returns "BRCB" or "URCB".
func (t RCBType) String() string {
	if t == RCBBuffered {
		return "BRCB"
	}
	return "URCB"
}

// FC returns the functional constraint for this RCB type.
func (t RCBType) FC() FunctionalConstraint {
	if t == RCBBuffered {
		return FCBR
	}
	return FCRP
}

// ReportControlBlock holds the decoded attributes of a report control
// block read from a server.
type ReportControlBlock struct {
	// Reference is a display-only pseudo-reference derived from the
	// MMS item ID by replacing '$' with '.' (e.g. "LD/LLN0.BR.brcb01").
	// This is not a proper IEC 61850 object reference because BR/RP
	// is a functional constraint, not a path component. Use the LD
	// and MMS item ID from the original [Client.GetReportControlBlock]
	// call for programmatic access.
	Reference string

	// Type is BRCB or URCB.
	Type RCBType

	// RptID is the report identifier.
	RptID string

	// RptEna indicates whether the report is enabled.
	RptEna bool

	// DatSet is the data set reference.
	DatSet string

	// ConfRev is the configuration revision number.
	ConfRev uint32

	// OptFlds is the optional fields bit mask.
	OptFlds OptFlds

	// BufTm is the buffer time in milliseconds.
	BufTm uint32

	// SqNum is the current sequence number.
	SqNum uint32

	// TrgOps is the trigger options bit mask.
	TrgOps TrgOps

	// IntgPd is the integrity period in milliseconds.
	IntgPd uint32

	// GI indicates a general interrogation request.
	GI bool

	// Resv is the reservation flag (URCB only).
	Resv bool

	// ResvTms is the reservation time in ms (BRCB only, optional).
	ResvTms int32

	// EntryID is the entry identifier (BRCB only).
	EntryID []byte

	// PurgeBuf indicates a purge buffer request (BRCB only).
	PurgeBuf bool

	// Owner is the client owner identifier (optional, edition 2+).
	Owner []byte
}

// RCB attribute names as they appear in MMS item IDs.
const (
	rcbAttrRptID    = "RptID"
	rcbAttrRptEna   = "RptEna"
	rcbAttrDatSet   = "DatSet"
	rcbAttrConfRev  = "ConfRev"
	rcbAttrOptFlds  = "OptFlds"
	rcbAttrBufTm    = "BufTm"
	rcbAttrSqNum    = "SqNum"
	rcbAttrTrgOps   = "TrgOps"
	rcbAttrIntgPd   = "IntgPd"
	rcbAttrGI       = "GI"
	rcbAttrResv     = "Resv"
	rcbAttrResvTms  = "ResvTms"
	rcbAttrEntryID  = "EntryID"
	rcbAttrPurgeBuf = "PurgeBuf"
	rcbAttrOwner    = "Owner"
)

// ListReports returns the names of all report control blocks in the
// specified logical device.
//
// Discovery is by MMS item-name pattern: an item ID with exactly three
// '$'-separated segments where the second segment is "BR" or "RP" is
// treated as a RCB. This is a heuristic — it does not perform semantic
// verification (e.g., reading RCB attributes) to confirm the variable
// is actually a report control block.
//
// When [StrictnessOptions.VerifyReportCandidates] is true, each
// candidate is read from the server and decoded; items that fail to
// decode as a valid RCB structure are excluded. This is equivalent to
// calling [Client.ListReportsVerified].
//
// The results include both buffered (BRCB, FC=BR) and unbuffered
// (URCB, FC=RP) report control blocks. Each returned name is an MMS
// item ID like "LLN0$BR$brcbName01" or "LLN0$RP$urcbName01".
//
// Results are sorted alphabetically by name for deterministic output.
func (c *Client) ListReports(ctx context.Context, ld string) ([]string, error) {
	rcbs, err := c.listReportCandidates(ctx, ld)
	if err != nil {
		return nil, err
	}

	if c.opts.Strictness.VerifyReportCandidates && len(rcbs) > 0 {
		rcbs = c.verifyReportCandidates(ctx, ld, rcbs)
	}

	sort.Strings(rcbs)
	c.logger.Debug("iec61850: list reports", "ld", ld, "count", len(rcbs))
	return rcbs, nil
}

// listReportCandidates returns heuristic RCB names without verification.
func (c *Client) listReportCandidates(ctx context.Context, ld string) ([]string, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: list reports: %w: empty logical device name", ErrInvalidArgument)
	}

	allNames, err := c.mmsClient.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    c.ldDomain(ld),
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: list reports for %q: %w", ld, err)
	}

	var rcbs []string
	for _, name := range allNames {
		if isRCBItemID(name) {
			rcbs = append(rcbs, name)
		}
	}
	return rcbs, nil
}

// verifyReportCandidates reads each candidate from the server and
// returns only those that decode as valid RCB structures.
func (c *Client) verifyReportCandidates(ctx context.Context, ld string, candidates []string) []string {
	verified := make([]string, 0, len(candidates))
	for _, rcbItemID := range candidates {
		if _, readErr := c.GetReportControlBlock(ctx, ld, rcbItemID); readErr != nil {
			c.logger.Debug("iec61850: report candidate failed verification",
				"ld", ld, "rcb", rcbItemID, "error", readErr)
			continue
		}
		verified = append(verified, rcbItemID)
	}
	return verified
}

// isRCBItemID returns true if the MMS item ID matches the heuristic
// IEC 61850 RCB naming pattern (exactly LN$BR$name or LN$RP$name,
// 3 '$'-separated segments).
func isRCBItemID(itemID string) bool {
	parts := strings.Split(itemID, "$")
	if len(parts) != 3 {
		return false
	}
	return parts[1] == "BR" || parts[1] == "RP"
}

// ListReportsVerified returns verified report control block names by
// reading each heuristic candidate from the server and confirming it
// decodes as a valid RCB structure. This is slower than [ListReports]
// (one MMS read per candidate) but eliminates false positives from
// naming collisions.
//
// Candidates that fail to read or decode are silently excluded from
// the result and logged at Debug level.
func (c *Client) ListReportsVerified(ctx context.Context, ld string) ([]string, error) {
	candidates, err := c.listReportCandidates(ctx, ld)
	if err != nil {
		return nil, err
	}

	verified := c.verifyReportCandidates(ctx, ld, candidates)
	c.logger.Debug("iec61850: list reports verified", "ld", ld,
		"candidates", len(candidates), "verified", len(verified))
	return verified, nil
}

// rcbTypeFromItemID determines the RCB type from an MMS item ID.
func rcbTypeFromItemID(itemID string) RCBType {
	parts := strings.SplitN(itemID, "$", 3)
	if len(parts) >= 2 && parts[1] == "BR" {
		return RCBBuffered
	}
	return RCBUnbuffered
}

// GetReportControlBlock reads all attributes of a report control block
// from the server.
//
// The ld parameter is the logical device (MMS domain) and rcbItemID is
// the RCB's MMS item ID (e.g. "LLN0$BR$brcb01" or "LLN0$RP$urcb01").
func (c *Client) GetReportControlBlock(ctx context.Context, ld, rcbItemID string) (*ReportControlBlock, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: get RCB: %w: empty logical device name", ErrInvalidArgument)
	}
	if rcbItemID == "" {
		return nil, fmt.Errorf("iec61850: get RCB: %w: empty RCB item ID", ErrInvalidArgument)
	}

	result, err := c.mmsClient.Read(ctx, mms.ReadRequest{
		DomainID: c.ldDomain(ld),
		ItemID:   mms.ItemID(rcbItemID),
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: get RCB %s/%s: %w", ld, rcbItemID, err)
	}

	if result == nil || result.Value == nil {
		return nil, fmt.Errorf("iec61850: get RCB %s/%s: missing value in response", ld, rcbItemID)
	}

	rcbType := rcbTypeFromItemID(rcbItemID)
	rcb, err := decodeRCB(ld, rcbItemID, rcbType, result.Value)
	if err != nil {
		return nil, fmt.Errorf("iec61850: get RCB %s/%s: %w", ld, rcbItemID, err)
	}

	c.logger.Debug("iec61850: get RCB", "ref", rcb.Reference, "type", rcb.Type)
	return rcb, nil
}

const (
	minBRCBElements = 13 // RptID..GI + PurgeBuf + EntryID + TimeOfEntry
	minURCBElements = 11 // RptID..GI + Resv
)

// decodeRCB decodes a ReportControlBlock from an MMS structure value.
// decodeRCB decodes an MMS structure into a [ReportControlBlock].
//
// The decoder assumes fixed positional field ordering as defined by
// IEC 61850-8-1:
//   - URCB: RptID, RptEna, Resv, DatSet, ConfRev, OptFlds, BufTm, SqNum, TrgOps, IntgPd, GI
//   - BRCB: RptID, RptEna, DatSet, ConfRev, OptFlds, BufTm, SqNum, TrgOps, IntgPd, GI, PurgeBuf, EntryID, TimeOfEntry
//
// Note that URCB places Resv at position 2 (after RptEna, before DatSet).
func decodeRCB(ld, rcbItemID string, rcbType RCBType, v *mms.Value) (*ReportControlBlock, error) {
	elems, ok := v.Structure()
	if !ok {
		return nil, &ReportError{
			RCBRef:  ld + "/" + rcbItemID,
			Message: fmt.Sprintf("expected structure, got %v", v.Type()),
		}
	}

	minElems := minURCBElements
	if rcbType == RCBBuffered {
		minElems = minBRCBElements
	}
	if len(elems) < minElems {
		return nil, &ReportError{
			RCBRef:  ld + "/" + rcbItemID,
			Message: fmt.Sprintf("%s structure too short: got %d elements, need at least %d", rcbType, len(elems), minElems),
		}
	}

	rcbRef := ld + "/" + strings.ReplaceAll(rcbItemID, "$", ".")

	rcb := &ReportControlBlock{
		Reference: rcbRef,
		Type:      rcbType,
	}

	idx := 0
	get := func() *mms.Value {
		if idx < len(elems) {
			val := elems[idx]
			idx++
			return val
		}
		return nil
	}

	mustString := func(name string) (string, error) {
		val := get()
		if val == nil {
			return "", &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("missing required field %s", name)}
		}
		s, ok := val.VisibleString()
		if !ok {
			return "", &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("required field %s: expected VisibleString, got %v", name, val.Type())}
		}
		return s, nil
	}
	mustBool := func(name string) (bool, error) {
		val := get()
		if val == nil {
			return false, &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("missing required field %s", name)}
		}
		b, ok := val.Bool()
		if !ok {
			return false, &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("required field %s: expected Boolean, got %v", name, val.Type())}
		}
		return b, nil
	}
	mustUint32 := func(name string) (uint32, error) {
		val := get()
		if val == nil {
			return 0, &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("missing required field %s", name)}
		}
		u, ok := val.Uint32()
		if !ok {
			return 0, &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("required field %s: expected Unsigned, got %v", name, val.Type())}
		}
		return u, nil
	}

	var err error
	if rcb.RptID, err = mustString("RptID"); err != nil {
		return nil, err
	}
	if rcb.RptEna, err = mustBool("RptEna"); err != nil {
		return nil, err
	}
	// IEC 61850-8-1: URCB has Resv immediately after RptEna (position 2),
	// BRCB does not have Resv in this position.
	if rcbType == RCBUnbuffered {
		if val := get(); val != nil {
			if b, ok := val.Bool(); ok {
				rcb.Resv = b
			}
		}
	}
	if rcb.DatSet, err = mustString("DatSet"); err != nil {
		return nil, err
	}
	if rcb.ConfRev, err = mustUint32("ConfRev"); err != nil {
		return nil, err
	}
	{
		val := get()
		if val == nil {
			return nil, &ReportError{RCBRef: rcbRef, Message: "missing required field OptFlds"}
		}
		o, err := decodeOptFldsStrict(val)
		if err != nil {
			return nil, &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("required field OptFlds: %v", err)}
		}
		rcb.OptFlds = o
	}
	if val := get(); val != nil {
		if u, ok := val.Uint32(); ok {
			rcb.BufTm = u
		}
	}
	if val := get(); val != nil {
		if u, ok := val.Uint32(); ok {
			rcb.SqNum = u
		}
	}
	{
		val := get()
		if val == nil {
			return nil, &ReportError{RCBRef: rcbRef, Message: "missing required field TrgOps"}
		}
		t, err := decodeTrgOpsStrict(val)
		if err != nil {
			return nil, &ReportError{RCBRef: rcbRef, Message: fmt.Sprintf("required field TrgOps: %v", err)}
		}
		rcb.TrgOps = t
	}
	if val := get(); val != nil {
		if u, ok := val.Uint32(); ok {
			rcb.IntgPd = u
		}
	}
	if val := get(); val != nil {
		if b, ok := val.Bool(); ok {
			rcb.GI = b
		}
	}

	if rcbType == RCBBuffered {
		if val := get(); val != nil {
			if b, ok := val.Bool(); ok {
				rcb.PurgeBuf = b
			}
		}
		if val := get(); val != nil {
			if bs, ok := val.OctetString(); ok {
				rcb.EntryID = append([]byte(nil), bs...)
			}
		}
		// TimeOfEntry skipped (binary time, not always present)
		_ = get()

		if idx < len(elems) {
			if val := get(); val != nil {
				if i, ok := val.Int32(); ok {
					rcb.ResvTms = i
				}
			}
		}
	}

	if idx < len(elems) {
		if val := get(); val != nil {
			if bs, ok := val.OctetString(); ok {
				rcb.Owner = append([]byte(nil), bs...)
			}
		}
	}

	return rcb, nil
}

// decodeOptFlds decodes OptFlds from an MMS bit string value.
func decodeOptFlds(v *mms.Value) OptFlds {
	o, _ := decodeOptFldsStrict(v)
	return o
}

func decodeOptFldsStrict(v *mms.Value) (OptFlds, error) {
	data, ok := v.BitString()
	if !ok {
		return 0, fmt.Errorf("expected BitString, got %v", v.Type())
	}
	if len(data) < 2 {
		return 0, fmt.Errorf("BitString too short (%d bytes, need 2)", len(data))
	}
	if bl, ok := v.BitStringLength(); ok && bl < 10 {
		return 0, fmt.Errorf("OptFlds BitString too short (%d bits, need 10)", bl)
	}
	var o uint16
	// IEC 61850: bit 0 is reserved; bits 1-9 map to internal bits 0-8.
	for wireBit := 1; wireBit <= 9; wireBit++ {
		byteIdx := wireBit / 8
		bitInByte := uint(7 - (wireBit % 8))
		if data[byteIdx]&(1<<bitInByte) != 0 {
			o |= 1 << uint(wireBit-1)
		}
	}
	return OptFlds(o), nil
}

// decodeTrgOps decodes TrgOps from an MMS bit string value.
func decodeTrgOps(v *mms.Value) TrgOps {
	t, _ := decodeTrgOpsStrict(v)
	return t
}

func decodeTrgOpsStrict(v *mms.Value) (TrgOps, error) {
	data, ok := v.BitString()
	if !ok {
		return 0, fmt.Errorf("expected BitString, got %v", v.Type())
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("BitString is empty")
	}
	if bl, ok := v.BitStringLength(); ok && bl < 6 {
		return 0, fmt.Errorf("TrgOps BitString too short (%d bits, need 6)", bl)
	}
	var t uint8
	for bit := 0; bit < 6; bit++ {
		byteIdx := bit / 8
		if byteIdx >= len(data) {
			break
		}
		bitInByte := uint(7 - (bit % 8))
		if data[byteIdx]&(1<<bitInByte) != 0 {
			t |= 1 << uint(bit)
		}
	}
	// IEC 61850 TrgOps bit 0 is reserved (always 0). The MMS encoding
	// starts at bit 0, so the logical trigger bits (dchg, qchg, dupd,
	// integrity, gi) occupy bit positions 1–5. The >> 1 shifts them
	// down so the Go TrgOps constants start at bit 0.
	return TrgOps(t >> 1), nil
}

// encodeOptFlds encodes OptFlds to an MMS bit string value.
//
// IEC 61850 OptFlds BIT STRING (10 bits, big-endian):
//
//	bit 0: reserved (always 0)
//	bit 1: sequence-number      → OptFldSeqNum      (internal bit 0)
//	bit 2: report-time-stamp    → OptFldTimeStamp   (internal bit 1)
//	bit 3: reason-for-inclusion → OptFldReasonCode  (internal bit 2)
//	bit 4: data-set-name        → OptFldDataSet     (internal bit 3)
//	bit 5: data-reference       → OptFldDataRef     (internal bit 4)
//	bit 6: buffer-overflow      → OptFldBufOvfl     (internal bit 5)
//	bit 7: entry-id             → OptFldEntryID     (internal bit 6)
//	bit 8: conf-revision        → OptFldConfRev     (internal bit 7)
//	bit 9: segmentation         → OptFldSegmentation (internal bit 8)
func encodeOptFlds(o OptFlds) *mms.Value {
	data := make([]byte, 2)
	for internalBit := 0; internalBit < 9; internalBit++ {
		if o&OptFlds(1<<uint(internalBit)) != 0 {
			wireBit := internalBit + 1 // IEC 61850 bit 0 is reserved
			byteIdx := wireBit / 8
			bitInByte := uint(7 - (wireBit % 8))
			data[byteIdx] |= 1 << bitInByte
		}
	}
	return mms.NewBitStringWithLength(data, 10)
}

// encodeTrgOps encodes TrgOps to an MMS bit string value.
func encodeTrgOps(t TrgOps) *mms.Value {
	data := make([]byte, 1)
	shifted := uint8(t) << 1
	for bit := 0; bit < 6; bit++ {
		if shifted&(1<<uint(bit)) != 0 {
			bitInByte := uint(7 - (bit % 8))
			data[0] |= 1 << bitInByte
		}
	}
	return mms.NewBitStringWithLength(data, 6)
}

// --- RCB write operations ---

// RCBFieldMask specifies which RCB fields to write in a
// [Client.SetReportControlBlock] call. Only fields with the
// corresponding mask bit set are written to the server.
type RCBFieldMask uint32

// RCB field mask constants for [RCBUpdate].
const (
	RCBFieldRptID    RCBFieldMask = 1 << 0  // Write RptID
	RCBFieldRptEna   RCBFieldMask = 1 << 1  // Write RptEna
	RCBFieldDatSet   RCBFieldMask = 1 << 2  // Write DatSet
	RCBFieldOptFlds  RCBFieldMask = 1 << 3  // Write OptFlds
	RCBFieldBufTm    RCBFieldMask = 1 << 4  // Write BufTm
	RCBFieldTrgOps   RCBFieldMask = 1 << 5  // Write TrgOps
	RCBFieldIntgPd   RCBFieldMask = 1 << 6  // Write IntgPd
	RCBFieldGI       RCBFieldMask = 1 << 7  // Write GI
	RCBFieldResv     RCBFieldMask = 1 << 8  // Write Resv (URCB)
	RCBFieldPurgeBuf RCBFieldMask = 1 << 9  // Write PurgeBuf (BRCB)
	RCBFieldEntryID  RCBFieldMask = 1 << 10 // Write EntryID (BRCB)
	RCBFieldResvTms  RCBFieldMask = 1 << 11 // Write ResvTms (URCB)
)

// RCBUpdate specifies the RCB fields to write and their new values.
type RCBUpdate struct {
	// Fields selects which attributes to write.
	Fields RCBFieldMask

	RptID    string
	RptEna   bool
	DatSet   string
	OptFlds  OptFlds
	BufTm    uint32
	TrgOps   TrgOps
	IntgPd   uint32
	GI       bool
	Resv     bool
	PurgeBuf bool
	EntryID  []byte
	ResvTms  int32
}

// SetReportControlBlock writes selected attributes of a report control
// block. Only fields specified by [RCBUpdate.Fields] are sent to the
// server.
//
// This mask-based approach prevents accidental partial writes that could
// leave the RCB in an inconsistent state.
func (c *Client) SetReportControlBlock(ctx context.Context, ld, rcbItemID string, update RCBUpdate) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return fmt.Errorf("iec61850: set RCB: %w: empty logical device name", ErrInvalidArgument)
	}
	if rcbItemID == "" {
		return fmt.Errorf("iec61850: set RCB: %w: empty RCB item ID", ErrInvalidArgument)
	}
	if update.Fields == 0 {
		return nil
	}

	type fieldWrite struct {
		component string
		value     *mms.Value
	}

	// Configuration fields are written first. RptEna is always written
	// last (if requested) to ensure all other attributes are in place
	// before enabling the report — this ordering is an IEC 61850
	// invariant that must not be reordered.
	var writes []fieldWrite

	if update.Fields&RCBFieldRptID != 0 {
		writes = append(writes, fieldWrite{rcbAttrRptID, mms.NewVisibleString(update.RptID)})
	}
	if update.Fields&RCBFieldDatSet != 0 {
		writes = append(writes, fieldWrite{rcbAttrDatSet, mms.NewVisibleString(update.DatSet)})
	}
	if update.Fields&RCBFieldOptFlds != 0 {
		writes = append(writes, fieldWrite{rcbAttrOptFlds, encodeOptFlds(update.OptFlds)})
	}
	if update.Fields&RCBFieldBufTm != 0 {
		writes = append(writes, fieldWrite{rcbAttrBufTm, mms.NewUnsigned(uint64(update.BufTm))})
	}
	if update.Fields&RCBFieldTrgOps != 0 {
		writes = append(writes, fieldWrite{rcbAttrTrgOps, encodeTrgOps(update.TrgOps)})
	}
	if update.Fields&RCBFieldIntgPd != 0 {
		writes = append(writes, fieldWrite{rcbAttrIntgPd, mms.NewUnsigned(uint64(update.IntgPd))})
	}
	if update.Fields&RCBFieldGI != 0 {
		writes = append(writes, fieldWrite{rcbAttrGI, mms.NewBoolean(update.GI)})
	}
	if update.Fields&RCBFieldResv != 0 {
		writes = append(writes, fieldWrite{rcbAttrResv, mms.NewBoolean(update.Resv)})
	}
	if update.Fields&RCBFieldPurgeBuf != 0 {
		writes = append(writes, fieldWrite{rcbAttrPurgeBuf, mms.NewBoolean(update.PurgeBuf)})
	}
	if update.Fields&RCBFieldEntryID != 0 {
		writes = append(writes, fieldWrite{rcbAttrEntryID, mms.NewOctetString(update.EntryID)})
	}
	if update.Fields&RCBFieldResvTms != 0 {
		writes = append(writes, fieldWrite{rcbAttrResvTms, mms.NewInteger(int64(update.ResvTms))})
	}

	// RptEna MUST be last: enabling the report before configuration
	// fields are set can cause the server to send reports with stale
	// or incomplete parameters.
	if update.Fields&RCBFieldRptEna != 0 {
		writes = append(writes, fieldWrite{rcbAttrRptEna, mms.NewBoolean(update.RptEna)})
	}

	for _, w := range writes {
		attrItemID := rcbItemID + "$" + w.component
		if _, err := c.mmsClient.Write(ctx, mms.WriteRequest{
			DomainID: c.ldDomain(ld),
			ItemID:   mms.ItemID(attrItemID),
			Value:    w.value,
		}); err != nil {
			return fmt.Errorf("iec61850: set RCB %s/%s.%s: %w", ld, rcbItemID, w.component, err)
		}
	}

	c.logger.Debug("iec61850: set RCB", "ld", ld, "rcb", rcbItemID, "fields", len(writes))
	return nil
}

// --- Report indication (incoming report) ---

// ReportIndication represents a decoded IEC 61850 report received
// from the server via an InformationReport.
type ReportIndication struct {
	// RptID is the report identifier.
	RptID string

	// OptFlds is the optional fields mask from this report.
	OptFlds OptFlds

	// SeqNum is the sequence number (present when OptFldSeqNum is set).
	SeqNum uint32

	// SubSeqNum is the sub-sequence number for segmented reports.
	SubSeqNum uint32

	// MoreSegments indicates additional report segments follow.
	MoreSegments bool

	// DatSet is the data set reference (present when OptFldDataSet is set).
	DatSet string

	// BufOvfl indicates buffer overflow (present when OptFldBufOvfl is set).
	BufOvfl bool

	// EntryID is the entry identifier (present when OptFldEntryID is set).
	EntryID []byte

	// ConfRev is the configuration revision (present when OptFldConfRev is set).
	ConfRev uint32

	// Timestamp is the report timestamp (present when OptFldTimeStamp
	// is set). Decoded from UTCTime or BinaryTime depending on the
	// server's encoding.
	Timestamp time.Time

	// Inclusion is a bitmask indicating which data set members are
	// included in this report.
	Inclusion []bool

	// DataReferences contains data references for included members
	// (present when OptFldDataRef is set).
	DataReferences []string

	// Values contains the values for included members, in order of
	// inclusion. Only entries where Inclusion[i] is true have values.
	Values []*Value

	// ReasonCodes contains the reason for inclusion for each included
	// member (present when OptFldReasonCode is set).
	ReasonCodes []ReasonCode
}

// clone returns a shallow copy of the ReportIndication with independent
// slices for Values, DataReferences, ReasonCodes, Inclusion, and EntryID.
// The Value pointers themselves are shared; this is sufficient to allow
// safe append/delete on the slices without affecting other subscribers.
func (ri *ReportIndication) clone() *ReportIndication {
	c := *ri
	if ri.EntryID != nil {
		c.EntryID = append([]byte(nil), ri.EntryID...)
	}
	if ri.Inclusion != nil {
		c.Inclusion = append([]bool(nil), ri.Inclusion...)
	}
	if ri.DataReferences != nil {
		c.DataReferences = append([]string(nil), ri.DataReferences...)
	}
	if ri.Values != nil {
		c.Values = append([]*Value(nil), ri.Values...)
	}
	if ri.ReasonCodes != nil {
		c.ReasonCodes = append([]ReasonCode(nil), ri.ReasonCodes...)
	}
	return &c
}

// decodeReportIndication decodes an IEC 61850 report from an MMS
// InformationReport's values. The values follow the standard report
// field order defined in IEC 61850-7-2. Optional fields that are
// present but fail type extraction are logged at debug level and
// treated as zero-valued rather than failing the entire decode.
func decodeReportIndication(values []*mms.Value, logger *slog.Logger) (*ReportIndication, error) {
	if len(values) == 0 {
		return nil, &ReportError{Message: "empty report values"}
	}

	ri := &ReportIndication{}
	idx := 0

	next := func() *mms.Value {
		if idx < len(values) {
			v := values[idx]
			idx++
			return v
		}
		return nil
	}

	// RptID (element 0)
	if v := next(); v != nil {
		if s, ok := v.VisibleString(); ok {
			ri.RptID = s
		}
	} else {
		return nil, &ReportError{Message: "missing RptID"}
	}

	// OptFlds (element 1)
	if v := next(); v != nil {
		ri.OptFlds = decodeOptFlds(v)
	} else {
		return nil, &ReportError{Message: "missing OptFlds"}
	}

	logFieldFail := func(field string, v *mms.Value) {
		if logger != nil {
			logger.Debug("iec61850: report field decode failed",
				"rptID", ri.RptID, "field", field, "type", v.Type())
		}
	}

	// Optional fields based on OptFlds, in standard order
	if ri.OptFlds.Has(OptFldSeqNum) {
		if v := next(); v != nil {
			if u, ok := v.Uint32(); ok {
				ri.SeqNum = u
			} else {
				logFieldFail("SeqNum", v)
			}
		}
	}

	if ri.OptFlds.Has(OptFldTimeStamp) {
		if v := next(); v != nil {
			if t, ok := v.UTCTime(); ok {
				ri.Timestamp = t
			} else if ms, ok := v.BinaryTime(); ok {
				ri.Timestamp = time.UnixMilli(ms)
			} else {
				logFieldFail("TimeStamp", v)
			}
		}
	}

	if ri.OptFlds.Has(OptFldDataSet) {
		if v := next(); v != nil {
			if s, ok := v.VisibleString(); ok {
				ri.DatSet = s
			} else {
				logFieldFail("DatSet", v)
			}
		}
	}

	if ri.OptFlds.Has(OptFldBufOvfl) {
		if v := next(); v != nil {
			if b, ok := v.Bool(); ok {
				ri.BufOvfl = b
			} else {
				logFieldFail("BufOvfl", v)
			}
		}
	}

	if ri.OptFlds.Has(OptFldEntryID) {
		if v := next(); v != nil {
			if bs, ok := v.OctetString(); ok {
				ri.EntryID = append([]byte(nil), bs...)
			} else {
				logFieldFail("EntryID", v)
			}
		}
	}

	if ri.OptFlds.Has(OptFldConfRev) {
		if v := next(); v != nil {
			if u, ok := v.Uint32(); ok {
				ri.ConfRev = u
			} else {
				logFieldFail("ConfRev", v)
			}
		}
	}

	if ri.OptFlds.Has(OptFldSegmentation) {
		if v := next(); v != nil {
			if u, ok := v.Uint32(); ok {
				ri.SubSeqNum = u
			}
		}
		if v := next(); v != nil {
			if b, ok := v.Bool(); ok {
				ri.MoreSegments = b
			}
		}
	}

	// Inclusion bitstring
	var inclusionCount int
	if v := next(); v != nil {
		data, ok := v.BitString()
		if ok {
			bitLen, _ := v.BitStringLength()
			ri.Inclusion = make([]bool, bitLen)
			for i := 0; i < bitLen; i++ {
				byteIdx := i / 8
				if byteIdx < len(data) {
					bitInByte := uint(7 - (i % 8))
					ri.Inclusion[i] = data[byteIdx]&(1<<bitInByte) != 0
				}
			}
			for _, inc := range ri.Inclusion {
				if inc {
					inclusionCount++
				}
			}
		}
	}

	// Data references (if OptFldDataRef)
	if ri.OptFlds.Has(OptFldDataRef) {
		ri.DataReferences = make([]string, 0, inclusionCount)
		for i := 0; i < inclusionCount; i++ {
			if v := next(); v != nil {
				if s, ok := v.VisibleString(); ok {
					ri.DataReferences = append(ri.DataReferences, s)
				}
			}
		}
	}

	// Data values (one per included member)
	ri.Values = make([]*Value, 0, inclusionCount)
	for i := 0; i < inclusionCount; i++ {
		if v := next(); v != nil {
			ri.Values = append(ri.Values, NewValue(v))
		}
	}

	// Reason codes (if OptFldReasonCode)
	if ri.OptFlds.Has(OptFldReasonCode) {
		ri.ReasonCodes = make([]ReasonCode, 0, inclusionCount)
		for i := 0; i < inclusionCount; i++ {
			if v := next(); v != nil {
				ri.ReasonCodes = append(ri.ReasonCodes, decodeReasonCode(v))
			}
		}
	}

	if len(ri.Values) != inclusionCount {
		return nil, &ReportError{
			Message: fmt.Sprintf("inclusion bitmap has %d members but decoded %d values",
				inclusionCount, len(ri.Values)),
		}
	}
	if ri.OptFlds.Has(OptFldDataRef) && len(ri.DataReferences) != inclusionCount {
		return nil, &ReportError{
			Message: fmt.Sprintf("inclusion bitmap has %d members but decoded %d data references",
				inclusionCount, len(ri.DataReferences)),
		}
	}
	if ri.OptFlds.Has(OptFldReasonCode) && len(ri.ReasonCodes) != inclusionCount {
		return nil, &ReportError{
			Message: fmt.Sprintf("inclusion bitmap has %d members but decoded %d reason codes",
				inclusionCount, len(ri.ReasonCodes)),
		}
	}

	return ri, nil
}

// decodeReasonCode decodes a reason-for-inclusion bit string.
func decodeReasonCode(v *mms.Value) ReasonCode {
	data, ok := v.BitString()
	if !ok || len(data) == 0 {
		return 0
	}
	var r uint8
	for bit := 0; bit < 7; bit++ {
		byteIdx := bit / 8
		if byteIdx >= len(data) {
			break
		}
		bitInByte := uint(7 - (bit % 8))
		if data[byteIdx]&(1<<bitInByte) != 0 {
			r |= 1 << uint(bit)
		}
	}
	// IEC 61850 ReasonCode bit 0 is reserved. The MMS bit string
	// encodes the logical reason bits (dchg, qchg, dupd, integrity,
	// gi, apptrigger) starting at position 1. The >> 1 normalises
	// them so the Go ReasonCode constants start at bit 0.
	return ReasonCode(r >> 1)
}

// --- GI and URCB operations ---

// TriggerGI triggers a General Interrogation on a report control block.
//
// For BRCBs, this writes GI=true to the RCB. The server responds by
// sending a report containing all data set members with reason=GI.
//
// For URCBs, the same mechanism applies but the RCB must be enabled
// and reserved (if applicable) first.
func (c *Client) TriggerGI(ctx context.Context, ld, rcbItemID string) error {
	return c.SetReportControlBlock(ctx, ld, rcbItemID, RCBUpdate{
		Fields: RCBFieldGI,
		GI:     true,
	})
}

// ReserveURCB reserves an unbuffered report control block for
// exclusive use by this client.
//
// Reservation is required before configuring or enabling a URCB.
// The reservation remains until explicitly released via
// [Client.ReleaseURCB] or the connection is closed.
func (c *Client) ReserveURCB(ctx context.Context, ld, rcbItemID string) error {
	return c.SetReportControlBlock(ctx, ld, rcbItemID, RCBUpdate{
		Fields: RCBFieldResv,
		Resv:   true,
	})
}

// ReleaseURCB releases a previously reserved unbuffered report
// control block.
//
// The caller should disable the report (RptEna=false) before
// releasing to avoid undefined behavior.
func (c *Client) ReleaseURCB(ctx context.Context, ld, rcbItemID string) error {
	return c.SetReportControlBlock(ctx, ld, rcbItemID, RCBUpdate{
		Fields: RCBFieldResv,
		Resv:   false,
	})
}

// --- Subscription engine ---

// OverflowPolicy controls what happens when a subscription's report
// channel is full.
//
// # URCB vs BRCB loss semantics
//
// For URCB (unbuffered) reports, report loss at the application queue
// level is expected and accepted — the server makes no durability promise.
// When [OverflowDropNewest] or [OverflowDropOldest] is used, lost reports
// are silently discarded and the client will not observe a gap indicator.
// This matches the standard URCB contract: reports are best-effort.
//
// For BRCB (buffered) reports, application queue overflow is a different
// concern from the server-side BRCB buffer overflow:
//
//   - Server-side BRCB buffer overflow occurs when the in-memory buffer
//     (default: 1000 entries) fills up before any client enables the BRCB.
//     The server drops the oldest entry and sets BufOvfl=true in the next
//     delivered report so the client knows a gap exists.
//
//   - Client-side application queue overflow (this type) occurs when the
//     client's in-process channel (QueueSize) fills up while reports are
//     being delivered. The chosen OverflowPolicy applies here; the server
//     is unaware of this loss. For BRCB use cases that require no loss,
//     set QueueSize large enough and consume reports promptly, or use
//     [OverflowBlock].
//
// # Choosing a policy
//
//   - [OverflowDropNewest]: default. Safe for all uses. Report loss is
//     visible in SeqNum gaps.
//   - [OverflowDropOldest]: prefer fresher data; older values are discarded.
//   - [OverflowBlock]: zero-loss delivery. Blocks report dispatch for all
//     subscriptions on this connection if the channel is full.
//   - [OverflowCallback]: custom handling (e.g., metrics, reconnect trigger).
type OverflowPolicy int

const (
	// OverflowDropNewest drops the incoming report when the channel
	// is full (default). A warning is logged.
	OverflowDropNewest OverflowPolicy = iota

	// OverflowDropOldest discards the oldest buffered report to
	// make room for the new one.
	OverflowDropOldest

	// OverflowBlock blocks the dispatcher until space is available.
	// Use with caution: this can block all report delivery.
	OverflowBlock

	// OverflowCallback invokes the OnOverflow callback (if set)
	// and drops the report. If no callback is set, behaves like
	// [OverflowDropNewest].
	OverflowCallback
)

// ReportSubscription represents an active subscription to IEC 61850
// reports. Reports are delivered to the channel returned by [Reports].
//
// Call [ReportSubscription.Close] to unsubscribe and release resources.
// Close is idempotent and safe to call from any goroutine.
type ReportSubscription struct {
	mu           sync.Mutex
	closed       bool
	ch           chan *ReportIndication
	done         chan struct{}  // closed on Close; guards OverflowBlock send
	delivering   sync.WaitGroup // tracks in-flight OverflowBlock delivers
	cancelFn     func()
	client       *Client
	rptID        string
	matcher      *reportMatcher
	overflow     OverflowPolicy
	onOverflow   func(*ReportIndication)
	cloneReports bool

	// Lifecycle state for Close() cleanup. Set during SubscribeReport.
	didReserve  bool
	didEnable   bool
	lifecycleLD string
	lifecycleID string // RCB MMS item ID

	// Segmented report reassembly state. Protected by mu.
	// Keyed by (RptID, SeqNum) to avoid collisions when a
	// subscription pattern matches multiple report sources
	// sharing the same sequence space.
	segBuf    []*ReportIndication
	segRptID  string
	segSeqNum uint32
	segActive bool
}

// Reports returns a read-only channel that delivers decoded report
// indications. The channel is closed when the subscription is closed
// or the client connection is terminated.
func (s *ReportSubscription) Reports() <-chan *ReportIndication {
	return s.ch
}

// Close terminates the report subscription. It is idempotent and safe
// to call from any goroutine.
//
// If the subscription was created with lifecycle options (AutoEnable,
// ReserveURCB), Close performs best-effort remote cleanup: disable the
// RCB (RptEna=false), then release the URCB reservation. Remote
// cleanup errors are logged but never prevent local shutdown.
//
// After Close, the [Reports] channel is closed and no further reports
// are delivered.
func (s *ReportSubscription) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()

	// Unregister from the client dispatch table first (under client
	// lock) so no dispatcher goroutine can obtain this subscription
	// after this point.
	s.client.unregisterSubscription(s)

	// Best-effort remote cleanup: disable, then release reservation.
	s.remoteCleanup()

	// Wait for any in-flight OverflowBlock sends to exit the select
	// before closing the channel.
	s.delivering.Wait()
	close(s.ch)

	if s.cancelFn != nil {
		s.cancelFn()
	}

	return nil
}

// remoteCleanup performs best-effort remote RCB cleanup. Errors are
// logged and never propagated.
//
// When the parent client is already closed/aborted, cleanup errors
// are expected (the connection may be gone) and are logged at Debug
// level rather than Warn to avoid noisy failure logs during normal
// shutdown.
func (s *ReportSubscription) remoteCleanup() {
	if s.lifecycleLD == "" || s.lifecycleID == "" {
		return
	}

	ctx := context.Background()
	logLevel := slog.LevelWarn
	if s.client.isClosing() {
		logLevel = slog.LevelDebug
	}

	if s.didEnable {
		if err := s.client.SetReportControlBlock(ctx, s.lifecycleLD, s.lifecycleID, RCBUpdate{
			Fields: RCBFieldRptEna,
			RptEna: false,
		}); err != nil {
			s.client.logger.Log(ctx, logLevel, "iec61850: subscription close: failed to disable RCB",
				"ld", s.lifecycleLD, "rcb", s.lifecycleID, "error", err)
		}
	}

	if s.didReserve {
		if err := s.client.ReleaseURCB(ctx, s.lifecycleLD, s.lifecycleID); err != nil {
			s.client.logger.Log(ctx, logLevel, "iec61850: subscription close: failed to release URCB",
				"ld", s.lifecycleLD, "rcb", s.lifecycleID, "error", err)
		}
	}
}

// RptMatchMode selects how incoming report IDs are matched against
// the subscription's RptID pattern.
type RptMatchMode int

const (
	// RptMatchExact matches the RptID exactly (default).
	RptMatchExact RptMatchMode = iota

	// RptMatchGlob uses [path.Match] glob semantics. '*' matches
	// any characters within a segment, '?' matches one character.
	RptMatchGlob
)

// SubscribeReportOptions configures a report subscription.
type SubscribeReportOptions struct {
	// QueueSize is the buffer size for the report channel. Defaults
	// to 64 if zero.
	QueueSize int

	// OverflowPolicy controls behavior when the channel is full.
	// Default is [OverflowDropNewest].
	OverflowPolicy OverflowPolicy

	// OnOverflow is called when a report is dropped due to overflow
	// (only used with [OverflowCallback]). Called from the dispatch
	// goroutine — must not block.
	OnOverflow func(*ReportIndication)

	// MatchMode selects how the RptID is matched. Default is exact.
	MatchMode RptMatchMode

	// CloneReports, when true, delivers a shallow copy of each
	// [ReportIndication] to this subscription instead of sharing
	// a pointer with other subscriptions. This allows safe mutation
	// of Values, DataReferences, and ReasonCodes slices without
	// affecting other subscribers.
	CloneReports bool

	// AutoEnable, when true, enables the RCB (RptEna=true) as part
	// of subscribing. Requires LD and RCBItemID to be set.
	AutoEnable bool

	// GIOnSubscribe, when true, triggers a General Interrogation
	// after enabling. Requires AutoEnable or that the RCB is
	// already enabled.
	GIOnSubscribe bool

	// ReserveURCB, when true, reserves the URCB before enabling.
	// Only meaningful for unbuffered RCBs.
	ReserveURCB bool

	// LD is the logical device for lifecycle operations
	// (AutoEnable, GIOnSubscribe, ReserveURCB). Required when
	// any lifecycle option is set.
	LD string

	// RCBItemID is the RCB MMS item ID for lifecycle operations.
	// Required when any lifecycle option is set.
	RCBItemID string
}

// SubscribeReport creates a subscription that delivers decoded IEC 61850
// reports matching the specified RptID pattern.
//
// The caller must read from the [ReportSubscription.Reports] channel to
// avoid backpressure. Overflow behavior is controlled by
// [SubscribeReportOptions.OverflowPolicy].
//
// Lifecycle options (AutoEnable, GIOnSubscribe, ReserveURCB) allow
// the subscription to configure and enable the RCB automatically.
// These require LD and RCBItemID to be set in options. The caller can
// still configure RCBs manually via [Client.SetReportControlBlock] and
// subscribe passively by not setting any lifecycle options.
//
// Call [ReportSubscription.Close] when done. The subscription is also
// closed when the client is closed.
//
// Lifecycle operations are not atomic. If subscription setup fails
// partway through, some RCB state (e.g., Resv, DatSet) may already
// have been changed on the server. Rollback is best-effort: the
// library attempts to undo prior writes, but network or server
// errors during cleanup are logged and otherwise ignored.
//
// SetReportControlBlock write ordering: configuration fields are
// written first, RptEna is always written last. This is an IEC 61850
// invariant — the library does not promise atomic RCB updates. See
// [Client.SetReportControlBlock] for details.
func (c *Client) SubscribeReport(ctx context.Context, rptID string, opts SubscribeReportOptions) (*ReportSubscription, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if rptID == "" {
		return nil, fmt.Errorf("iec61850: subscribe report: %w: empty RptID", ErrInvalidArgument)
	}

	queueSize := opts.QueueSize
	if queueSize <= 0 {
		queueSize = 64
	}

	m, err := newReportMatcher(opts.MatchMode, rptID)
	if err != nil {
		return nil, fmt.Errorf("iec61850: subscribe report: %w", err)
	}

	var reserved, enabled bool

	// Lifecycle: reserve URCB if requested
	if opts.ReserveURCB {
		if opts.LD == "" || opts.RCBItemID == "" {
			return nil, fmt.Errorf("iec61850: subscribe report: ReserveURCB requires LD and RCBItemID")
		}
		if err := c.ReserveURCB(ctx, opts.LD, opts.RCBItemID); err != nil {
			return nil, fmt.Errorf("iec61850: subscribe report: reserve URCB: %w", err)
		}
		reserved = true
	}

	rollback := func() {
		if enabled {
			_ = c.SetReportControlBlock(ctx, opts.LD, opts.RCBItemID, RCBUpdate{
				Fields: RCBFieldRptEna,
				RptEna: false,
			})
		}
		if reserved {
			_ = c.ReleaseURCB(ctx, opts.LD, opts.RCBItemID)
		}
	}

	// Lifecycle: enable RCB if requested
	if opts.AutoEnable {
		if opts.LD == "" || opts.RCBItemID == "" {
			rollback()
			return nil, fmt.Errorf("iec61850: subscribe report: AutoEnable requires LD and RCBItemID")
		}
		if err := c.SetReportControlBlock(ctx, opts.LD, opts.RCBItemID, RCBUpdate{
			Fields: RCBFieldRptEna,
			RptEna: true,
		}); err != nil {
			rollback()
			return nil, fmt.Errorf("iec61850: subscribe report: auto-enable: %w", err)
		}
		enabled = true
	}

	ctx, cancel := context.WithCancel(ctx)

	sub := &ReportSubscription{
		ch:           make(chan *ReportIndication, queueSize),
		done:         make(chan struct{}),
		cancelFn:     cancel,
		client:       c,
		rptID:        rptID,
		matcher:      m,
		overflow:     opts.OverflowPolicy,
		onOverflow:   opts.OnOverflow,
		cloneReports: opts.CloneReports,
		didReserve:   reserved,
		didEnable:    enabled,
		lifecycleLD:  opts.LD,
		lifecycleID:  opts.RCBItemID,
	}

	if !c.registerSubscription(rptID, sub) {
		cancel()
		close(sub.done)
		close(sub.ch)
		rollback()
		return nil, ErrClosed
	}

	// Lifecycle: trigger GI if requested.
	// After registration, sub.Close() handles all cleanup (remote
	// disable + release), so rollback() is not called here.
	if opts.GIOnSubscribe {
		if opts.LD == "" || opts.RCBItemID == "" {
			_ = sub.Close()
			return nil, fmt.Errorf("iec61850: subscribe report: GIOnSubscribe requires LD and RCBItemID")
		}
		if err := c.TriggerGI(ctx, opts.LD, opts.RCBItemID); err != nil {
			_ = sub.Close()
			return nil, fmt.Errorf("iec61850: subscribe report: GI: %w", err)
		}
	}

	go func() {
		<-ctx.Done()
		_ = sub.Close()
	}()

	c.logger.Debug("iec61850: subscribe report", "rptID", rptID, "queueSize", queueSize,
		"matchMode", opts.MatchMode, "overflow", opts.OverflowPolicy)
	return sub, nil
}

// --- subscription dispatch on Client ---

// initReportDispatch ensures the report handler is installed on the
// underlying MMS client. Called lazily on first subscription.
func (c *Client) initReportDispatch() {
	c.reportOnce.Do(func() {
		c.reportSubs = make(map[string][]*ReportSubscription)
		c.mmsClient.OnInformationReport(c.handleInformationReport)
	})
}

// registerSubscription adds a subscription to the dispatch table.
// Returns false if the client has been closed (reportSubs is nil).
func (c *Client) registerSubscription(rptID string, sub *ReportSubscription) bool {
	c.initReportDispatch()
	c.reportMu.Lock()
	defer c.reportMu.Unlock()
	if c.reportSubs == nil {
		return false
	}
	c.reportSubs[rptID] = append(c.reportSubs[rptID], sub)
	return true
}

// unregisterSubscription removes a specific subscription from the
// dispatch table. If multiple subscriptions share the same key, only
// the matching pointer is removed.
func (c *Client) unregisterSubscription(sub *ReportSubscription) {
	c.reportMu.Lock()
	defer c.reportMu.Unlock()
	if c.reportSubs == nil {
		return
	}
	key := sub.rptID
	subs := c.reportSubs[key]
	for i, s := range subs {
		if s == sub {
			c.reportSubs[key] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(c.reportSubs[key]) == 0 {
		delete(c.reportSubs, key)
	}
}

// handleInformationReport is the MMS-level callback installed on the
// MMS client. It decodes incoming reports and dispatches them to all
// matching subscriptions.
// handleInformationReport decodes incoming report indications and
// dispatches them to matching subscriptions.
//
// IMPORTANT: for non-segmented reports, the same *ReportIndication
// pointer is delivered to every matching subscription. Subscribers
// must treat received reports as immutable. Mutating a delivered
// report (e.g., modifying Values or Inclusion slices) will corrupt
// data for other subscribers. If mutation is needed, copy the report
// first.
func (c *Client) handleInformationReport(report *mms.InformationReportIndication) {
	if report == nil || len(report.Values) == 0 {
		c.logger.Debug("iec61850: received empty information report")
		return
	}

	ri, err := decodeReportIndication(report.Values, c.logger)
	if err != nil {
		c.logger.Warn("iec61850: failed to decode report", "error", err)
		return
	}

	c.reportMu.RLock()
	matches := c.findSubscriptions(ri.RptID)
	c.reportMu.RUnlock()

	if len(matches) == 0 {
		c.logger.Debug("iec61850: no subscription for report", "rptID", ri.RptID)
		return
	}

	for _, sub := range matches {
		delivered := ri
		if delivered.OptFlds.Has(OptFldSegmentation) {
			assembled := sub.handleSegment(delivered)
			if assembled == nil {
				continue
			}
			delivered = assembled
		}
		if sub.cloneReports {
			delivered = delivered.clone()
		}
		sub.deliver(delivered, c.logger)
	}
}

// findSubscriptions returns all subscriptions matching the given rptID.
// Checks exact-key matches first, then glob matches across all keys.
func (c *Client) findSubscriptions(rptID string) []*ReportSubscription {
	var result []*ReportSubscription

	// Exact-key match
	if subs, ok := c.reportSubs[rptID]; ok {
		result = append(result, subs...)
	}

	// Glob matches (skip exact-key entries already collected)
	for key, subs := range c.reportSubs {
		if key == rptID {
			continue
		}
		for _, sub := range subs {
			if sub.matcher != nil && sub.matcher.matches(rptID) {
				result = append(result, sub)
			}
		}
	}

	return result
}

// deliver sends a report to the subscription channel, applying the
// configured overflow policy.
//
// For [OverflowBlock], the lock is released before the blocking send
// to avoid stalling Close/shutdown while the channel is full.
func (s *ReportSubscription) deliver(ri *ReportIndication, logger interface{ Warn(string, ...any) }) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	switch s.overflow {
	case OverflowDropNewest:
		select {
		case s.ch <- ri:
		default:
			logger.Warn("iec61850: report dropped (queue full)", "rptID", ri.RptID)
		}
		s.mu.Unlock()

	case OverflowDropOldest:
		for {
			select {
			case s.ch <- ri:
				s.mu.Unlock()
				return
			default:
			}
			select {
			case <-s.ch:
				logger.Warn("iec61850: report dropped (oldest evicted)", "rptID", ri.RptID)
			default:
			}
		}

	case OverflowBlock:
		ch := s.ch
		done := s.done
		s.delivering.Add(1)
		s.mu.Unlock()
		select {
		case ch <- ri:
		case <-done:
		}
		s.delivering.Done()
		return

	case OverflowCallback:
		select {
		case s.ch <- ri:
		default:
			if s.onOverflow != nil {
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Warn("iec61850: OnOverflow callback panicked", "panic", r, "rptID", ri.RptID)
						}
					}()
					s.onOverflow(ri)
				}()
			} else {
				logger.Warn("iec61850: report dropped (queue full, no callback)", "rptID", ri.RptID)
			}
		}
		s.mu.Unlock()
	}
}

// --- Segmented report reassembly ---

// handleSegment buffers segmented report parts and returns the
// assembled report when all segments have arrived. Returns nil
// while segments are still pending.
//
// Sequence mismatches and buffer resets are logged as warnings.
func (s *ReportSubscription) handleSegment(ri *ReportIndication) *ReportIndication {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ri.SubSeqNum == 0 {
		if s.segActive {
			s.client.logger.Warn("iec61850: segment buffer reset (new sequence started)",
				"rptID", ri.RptID, "prevSeq", s.segSeqNum, "newSeq", ri.SeqNum,
				"buffered", len(s.segBuf))
		}
		s.segBuf = []*ReportIndication{ri}
		s.segRptID = ri.RptID
		s.segSeqNum = ri.SeqNum
		s.segActive = true
		if !ri.MoreSegments {
			s.segActive = false
			return ri
		}
		return nil
	}

	if !s.segActive || ri.SeqNum != s.segSeqNum || ri.RptID != s.segRptID {
		s.client.logger.Warn("iec61850: segment sequence mismatch (resetting buffer)",
			"rptID", ri.RptID, "expected", s.segSeqNum, "got", ri.SeqNum,
			"active", s.segActive, "buffered", len(s.segBuf))
		s.segBuf = []*ReportIndication{ri}
		s.segRptID = ri.RptID
		s.segSeqNum = ri.SeqNum
		s.segActive = true
		if !ri.MoreSegments {
			s.segActive = false
			return ri
		}
		return nil
	}

	expectedSubSeq := uint32(len(s.segBuf))
	if ri.SubSeqNum != expectedSubSeq {
		s.client.logger.Warn("iec61850: non-contiguous SubSeqNum",
			"rptID", ri.RptID, "expected", expectedSubSeq, "got", ri.SubSeqNum)
	}

	s.segBuf = append(s.segBuf, ri)

	if ri.MoreSegments {
		return nil
	}

	assembled := s.assembleSegments()
	s.segBuf = nil
	s.segActive = false
	return assembled
}

// assembleSegments combines buffered segment reports into a single
// logical report. Validates that metadata (RptID, DatSet, ConfRev,
// OptFlds) is consistent across all segments; inconsistent segments
// are dropped with a warning and reassembly falls back to the first
// segment only.
func (s *ReportSubscription) assembleSegments() *ReportIndication {
	if len(s.segBuf) == 0 {
		return nil
	}

	first := s.segBuf[0]

	for i := 1; i < len(s.segBuf); i++ {
		seg := s.segBuf[i]
		if seg.RptID != first.RptID ||
			seg.DatSet != first.DatSet ||
			seg.ConfRev != first.ConfRev ||
			seg.OptFlds != first.OptFlds {
			s.client.logger.Warn("iec61850: segment metadata mismatch, dropping buffer",
				"rptID", first.RptID,
				"segment", i,
				"mismatchRptID", seg.RptID != first.RptID,
				"mismatchDatSet", seg.DatSet != first.DatSet,
				"mismatchConfRev", seg.ConfRev != first.ConfRev,
				"mismatchOptFlds", seg.OptFlds != first.OptFlds)
			return first
		}
	}

	result := &ReportIndication{
		RptID:     first.RptID,
		OptFlds:   first.OptFlds,
		SeqNum:    first.SeqNum,
		DatSet:    first.DatSet,
		BufOvfl:   first.BufOvfl,
		EntryID:   append([]byte(nil), first.EntryID...),
		ConfRev:   first.ConfRev,
		Timestamp: first.Timestamp,
	}

	hasDataRefs := first.OptFlds.Has(OptFldDataRef)
	hasReasonCodes := first.OptFlds.Has(OptFldReasonCode)

	for _, seg := range s.segBuf {
		segIncCount := 0
		for _, inc := range seg.Inclusion {
			if inc {
				segIncCount++
			}
		}

		if len(seg.Values) != segIncCount {
			s.client.logger.Warn("iec61850: segment cardinality mismatch (values vs inclusion), dropping buffer",
				"rptID", first.RptID, "expected", segIncCount, "gotValues", len(seg.Values))
			return first
		}
		if hasDataRefs && len(seg.DataReferences) != segIncCount {
			s.client.logger.Warn("iec61850: segment cardinality mismatch (data refs vs inclusion), dropping buffer",
				"rptID", first.RptID, "expected", segIncCount, "gotRefs", len(seg.DataReferences))
			return first
		}
		if hasReasonCodes && len(seg.ReasonCodes) != segIncCount {
			s.client.logger.Warn("iec61850: segment cardinality mismatch (reason codes vs inclusion), dropping buffer",
				"rptID", first.RptID, "expected", segIncCount, "gotReasons", len(seg.ReasonCodes))
			return first
		}

		result.Inclusion = append(result.Inclusion, seg.Inclusion...)
		result.Values = append(result.Values, seg.Values...)
		result.ReasonCodes = append(result.ReasonCodes, seg.ReasonCodes...)
		result.DataReferences = append(result.DataReferences, seg.DataReferences...)
	}

	return result
}

// --- Report matching ---

type reportMatcher struct {
	mode    RptMatchMode
	pattern string
}

func newReportMatcher(mode RptMatchMode, pattern string) (*reportMatcher, error) {
	switch mode {
	case RptMatchExact:
		return &reportMatcher{mode: RptMatchExact, pattern: pattern}, nil
	case RptMatchGlob:
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
		return &reportMatcher{mode: RptMatchGlob, pattern: pattern}, nil
	default:
		return nil, fmt.Errorf("unknown report match mode %d", mode)
	}
}

func (m *reportMatcher) matches(rptID string) bool {
	switch m.mode {
	case RptMatchExact:
		return rptID == m.pattern
	case RptMatchGlob:
		ok, _ := path.Match(m.pattern, rptID)
		return ok
	default:
		return false
	}
}
