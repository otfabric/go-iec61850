// SPDX-License-Identifier: MIT

package iec61850

import (
	"fmt"
	"time"

	"github.com/otfabric/go-mms"
)

// CtlModel represents the IEC 61850 control model (ctlModel attribute).
// It determines whether the controllable data object uses direct
// operation, select-before-operate, or enhanced security variants.
type CtlModel int

const (
	// CtlModelStatusOnly means the data object is not controllable.
	CtlModelStatusOnly CtlModel = 0

	// CtlModelDirectNormal is direct-operate with normal security.
	// The client writes to Oper without a prior Select.
	CtlModelDirectNormal CtlModel = 1

	// CtlModelSBONormal is select-before-operate with normal security.
	// The client reads SBO (select), then writes to Oper.
	CtlModelSBONormal CtlModel = 2

	// CtlModelDirectEnhanced is direct-operate with enhanced security.
	// Like CtlModelDirectNormal, but the server validates origin,
	// ctlNum, and timestamps for command verification.
	CtlModelDirectEnhanced CtlModel = 3

	// CtlModelSBOEnhanced is select-before-operate with enhanced
	// security. The client writes to SBOw (select-with-value), then
	// writes to Oper, with full origin/ctlNum/timestamp verification.
	CtlModelSBOEnhanced CtlModel = 4
)

// String returns a human-readable control model name.
func (m CtlModel) String() string {
	switch m {
	case CtlModelStatusOnly:
		return "status-only"
	case CtlModelDirectNormal:
		return "direct-with-normal-security"
	case CtlModelSBONormal:
		return "sbo-with-normal-security"
	case CtlModelDirectEnhanced:
		return "direct-with-enhanced-security"
	case CtlModelSBOEnhanced:
		return "sbo-with-enhanced-security"
	default:
		return fmt.Sprintf("ctlModel(%d)", int(m))
	}
}

// IsControllable reports whether this control model supports any
// control operation (i.e., is not status-only).
func (m CtlModel) IsControllable() bool { return m != CtlModelStatusOnly }

// IsSBO reports whether this control model uses select-before-operate.
func (m CtlModel) IsSBO() bool { return m == CtlModelSBONormal || m == CtlModelSBOEnhanced }

// IsEnhanced reports whether this control model uses enhanced security
// (origin/ctlNum/timestamp verification).
func (m CtlModel) IsEnhanced() bool { return m == CtlModelDirectEnhanced || m == CtlModelSBOEnhanced }

// OrCat represents the originator category (orCat) in the Origin
// structure of IEC 61850 control commands.
type OrCat int

const (
	OrCatNotSupported     OrCat = 0
	OrCatBayControl       OrCat = 1
	OrCatStationControl   OrCat = 2
	OrCatRemoteControl    OrCat = 3
	OrCatAutomaticBay     OrCat = 4
	OrCatAutomaticStation OrCat = 5
	OrCatAutomaticRemote  OrCat = 6
	OrCatMaintenance      OrCat = 7
	OrCatProcess          OrCat = 8
)

// String returns the originator category name.
func (c OrCat) String() string {
	switch c {
	case OrCatNotSupported:
		return "not-supported"
	case OrCatBayControl:
		return "bay-control"
	case OrCatStationControl:
		return "station-control"
	case OrCatRemoteControl:
		return "remote-control"
	case OrCatAutomaticBay:
		return "automatic-bay"
	case OrCatAutomaticStation:
		return "automatic-station"
	case OrCatAutomaticRemote:
		return "automatic-remote"
	case OrCatMaintenance:
		return "maintenance"
	case OrCatProcess:
		return "process"
	default:
		return fmt.Sprintf("orCat(%d)", int(c))
	}
}

// Origin identifies the originator of a control command.
type Origin struct {
	// OrCat is the originator category.
	OrCat OrCat

	// OrIdent is the originator identifier (application-specific,
	// typically an IP address or operator ID).
	OrIdent []byte
}

// toMMS encodes the Origin as an MMS structure {orCat, orIdent}.
func (o Origin) toMMS() *mms.Value {
	return mms.NewStructure([]*mms.Value{
		mms.NewInteger(int64(o.OrCat)),
		mms.NewOctetString(o.OrIdent),
	})
}

// CheckConditions represents the Check bitstring in a control
// command: BIT STRING { synchrocheck (0), interlock-check (1) } (IEC
// 61850-8-1), bit 0 being the most significant bit of the first octet, as
// libiec61850 reads it (MmsValue_getBitStringBit). The constants are
// flags of this type, not wire positions.
type CheckConditions uint8

const (
	CheckSynchroCheck   CheckConditions = 1 << 0
	CheckInterlockCheck CheckConditions = 1 << 1
)

// Has reports whether the given check bit is set.
func (c CheckConditions) Has(flag CheckConditions) bool { return c&flag != 0 }

// Wire positions of the Check bits: bit 0 (synchrocheck) is 0x80, bit 1
// (interlock-check) is 0x40.
const (
	checkBitSynchro   = 0x80
	checkBitInterlock = 0x40
)

// toMMS encodes Check as a 2-bit bitstring. Before v1.10.1 the flags were
// shifted into the opposite positions.
func (c CheckConditions) toMMS() *mms.Value {
	var b byte
	if c.Has(CheckSynchroCheck) {
		b |= checkBitSynchro
	}
	if c.Has(CheckInterlockCheck) {
		b |= checkBitInterlock
	}
	return mms.NewBitStringWithLength([]byte{b}, 2)
}

// checkFromMMS decodes the first octet of a Check bitstring.
func checkFromMMS(b byte) CheckConditions {
	var c CheckConditions
	if b&checkBitSynchro != 0 {
		c |= CheckSynchroCheck
	}
	if b&checkBitInterlock != 0 {
		c |= CheckInterlockCheck
	}
	return c
}

// OperateParams contains all parameters for an IEC 61850 control
// Operate or SelectWithValue command.
//
// At minimum, CtlVal must be set. For enhanced security models,
// Origin, CtlNum, and timestamps are also required. The library
// fills in sensible defaults for unset fields when possible.
type OperateParams struct {
	// CtlVal is the control value to write. Required.
	// Use [BoolCtlVal], [IntCtlVal], [FloatCtlVal], [EnumCtlVal],
	// or [StringCtlVal] to construct it.
	CtlVal *mms.Value

	// Origin identifies the command originator. When nil, a default
	// Origin with OrCatRemoteControl and empty OrIdent is used.
	Origin *Origin

	// CtlNum is the control sequence number (INT8U, 0..255). All
	// commands of one control sequence carry the same ctlNum (IEC
	// 61850-7-2 20.5.2.4). Zero, with CtlNumSet false, means unset: the
	// library uses the ctlNum of the last SelectWithValue of the control
	// (Operate), or the next value of an auto-incrementing counter.
	CtlNum uint8

	// CtlNumSet sends CtlNum as given, zero included.
	CtlNumSet bool

	// OperTm is the scheduled operation time. A zero value means
	// "operate now" (no timed command); the library then sends operTm
	// all zero when the server's Oper type has it (IEC 61850-8-1
	// 20.10.2.2).
	OperTm time.Time

	// Test, when true, sets the Test bit to indicate a test command
	// that should not cause physical action.
	Test bool

	// Check specifies synchrocheck/interlockCheck conditions.
	Check CheckConditions

	// TQuality, when set, is the TimeQuality of the T member (the time
	// the command was sent, IEC 61850-7-2 20.5.2.5; IEC 61850-8-1 Table
	// 32). Nil sends go-mms's default: synchronized, 10 bits of
	// accuracy (0x0a).
	TQuality *TimeQuality
}

// CancelParams contains parameters for an IEC 61850 Cancel command.
type CancelParams struct {
	// CtlVal is the control value from the original Operate that
	// is being cancelled. Required.
	CtlVal *mms.Value

	// Origin identifies the command originator. When nil, a default
	// Origin with OrCatRemoteControl and empty OrIdent is used.
	Origin *Origin

	// CtlNum is the control sequence number that matches the
	// original Operate or SelectWithValue. Zero, with CtlNumSet false,
	// means the ctlNum of the last SelectWithValue of the control.
	CtlNum uint8

	// CtlNumSet sends CtlNum as given, zero included.
	CtlNumSet bool

	// OperTm is the scheduled operation time from the original
	// Operate. A zero value means the original was immediate.
	OperTm time.Time

	// Test is the test status of the control sequence being cancelled
	// (IEC 61850-7-2 20.5.2.6: every command of a sequence carries the
	// same test status; IEC 61850-8-1 Table 88). Before v1.17.0 a
	// Cancel always sent FALSE.
	Test bool

	// TQuality, when set, is the TimeQuality of the T member; see
	// [OperateParams.TQuality].
	TQuality *TimeQuality
}

// AddCause represents the AdditionalCause field in LastApplError,
// indicating why a control command was refused or failed. The values are
// the AddCause enumeration of IEC 61850-7-2 (control model, clause 20),
// as libiec61850 encodes them on the wire.
type AddCause int

// AddCause values (IEC 61850-7-2, clause 20).
const (
	AddCauseUnknown                     AddCause = 0
	AddCauseNotSupported                AddCause = 1
	AddCauseBlocked                     AddCause = 2 // blocked by switching hierarchy
	AddCauseSelectFailed                AddCause = 3
	AddCauseInvalidPosition             AddCause = 4
	AddCausePositionReached             AddCause = 5
	AddCauseParameterChange             AddCause = 6
	AddCauseStepLimit                   AddCause = 7
	AddCauseBlockedByMode               AddCause = 8
	AddCauseBlockedByProcess            AddCause = 9
	AddCauseBlockedByInterlocking       AddCause = 10
	AddCauseBlockedBySynchrocheck       AddCause = 11
	AddCauseCommandAlreadyExec          AddCause = 12
	AddCauseBlockedByHealth             AddCause = 13
	AddCause1ofN                        AddCause = 14
	AddCauseAbortionByCancel            AddCause = 15
	AddCauseTimeLimit                   AddCause = 16
	AddCauseAbortionByTrip              AddCause = 17
	AddCauseObjectNotSelected           AddCause = 18
	AddCauseObjectAlreadySelected       AddCause = 19
	AddCauseNoAccessAuthority           AddCause = 20
	AddCauseEndedWithOvershoot          AddCause = 21
	AddCauseAbortionDueToDeviation      AddCause = 22
	AddCauseAbortionByCommunicationLoss AddCause = 23
	AddCauseBlockedByCommand            AddCause = 24
	AddCauseNone                        AddCause = 25
	AddCauseInconsistentParameters      AddCause = 26
	AddCauseLockedByOtherClient         AddCause = 27
)

// Deprecated AddCause names. Before v1.10.0 the values from 8 on did
// not follow IEC 61850-7-2; these names map to the standard values.
const (
	// Deprecated: no such cause exists; use [AddCauseBlocked]
	// (blocked by switching hierarchy).
	AddCauseBlockedBySwitch = AddCauseBlocked
	// Deprecated: use [AddCause1ofN].
	AddCause1of1 = AddCause1ofN
	// Deprecated: use [AddCauseAbortionByCancel].
	AddCauseAbort = AddCauseAbortionByCancel
)

var addCauseNames = [...]string{
	"unknown", "not-supported", "blocked-by-switching-hierarchy", "select-failed",
	"invalid-position", "position-reached", "parameter-change-in-execution", "step-limit",
	"blocked-by-mode", "blocked-by-process", "blocked-by-interlocking", "blocked-by-synchrocheck",
	"command-already-in-execution", "blocked-by-health", "1-of-n-control", "abortion-by-cancel",
	"time-limit-over", "abortion-by-trip", "object-not-selected", "object-already-selected",
	"no-access-authority", "ended-with-overshoot", "abortion-due-to-deviation",
	"abortion-by-communication-loss", "blocked-by-command", "none", "inconsistent-parameters",
	"locked-by-other-client",
}

// String returns the additional cause name as IEC 61850-7-2 spells it.
func (c AddCause) String() string {
	if c >= 0 && int(c) < len(addCauseNames) {
		return addCauseNames[c]
	}
	return fmt.Sprintf("addCause(%d)", int(c))
}

// LastApplError is the reason a server gives for a refused or failed
// control (IEC 61850-8-1, LastApplError): sent as an InformationReport
// before the negative response to a control write, and as part of a
// negative CommandTermination.
type LastApplError struct {
	// CntrlObj is the MMS reference of the control attribute that failed,
	// such as "LD/GGIO1$CO$SPCSO1$Oper".
	CntrlObj string

	// Error is the service error: 0 no error, 1 unknown, 2 timeout test
	// not ok, 3 operator test not ok.
	Error int

	// Origin is the originator that issued the failed command.
	Origin Origin

	// CtlNum is the control number of the failed command. Zero when the
	// server sent the four-member layout without it.
	CtlNum uint8

	// AddCause provides the specific additional cause.
	AddCause AddCause
}

// BoolCtlVal creates a boolean control value.
func BoolCtlVal(v bool) *mms.Value { return mms.NewBoolean(v) }

// IntCtlVal creates an integer control value (for INC controllable
// integer CDCs).
func IntCtlVal(v int32) *mms.Value { return mms.NewInteger(int64(v)) }

// FloatCtlVal creates a floating-point control value (for APC
// controllable analogue CDCs).
func FloatCtlVal(v float32) *mms.Value { return mms.NewFloat(float64(v)) }

// EnumCtlVal creates an enumerated control value.
func EnumCtlVal(v int32) *mms.Value { return mms.NewInteger(int64(v)) }

// StringCtlVal creates a visible-string control value.
func StringCtlVal(v string) *mms.Value { return mms.NewVisibleString(v) }

// BspCtlVal creates a bitstring control value for BSC (binary
// step controllable) CDCs.
func BspCtlVal(bits []byte, bitLen int) *mms.Value {
	return mms.NewBitStringWithLength(bits, bitLen)
}

// DpCtlVal creates a Dbpos (double-point) value: a two-bit bitstring,
// 01=off, 10=on. That is the type of a DPC's stVal, not of its ctlVal.
//
// Deprecated: the ctlVal of a DPC is BOOLEAN (IEC 61850-7-3, DPC); a
// server refuses this value in an Oper with type-inconsistent. Use
// [DPCCtlVal] (or [BoolCtlVal]) for the ctlVal and [DbposValue] for a
// Dbpos status value.
func DpCtlVal(on bool) *mms.Value { return DbposValue(on) }

// DPCCtlVal creates the ctlVal of a double-point controllable (DPC):
// BOOLEAN, off (false) or on (true) (IEC 61850-7-3, DPC ctlVal).
func DPCCtlVal(on bool) *mms.Value { return mms.NewBoolean(on) }

// DbposValue creates a Dbpos value (IEC 61850-7-3 Dbpos, IEC 61850-8-1
// 8.1.2.1: a two-bit bitstring): 10 on (true), 01 off (false).
func DbposValue(on bool) *mms.Value {
	if on {
		return mms.NewBitStringWithLength([]byte{0x80}, 2) // 10
	}
	return mms.NewBitStringWithLength([]byte{0x40}, 2) // 01
}

// controlTime is the T member of a control command: now, with the
// TimeQuality given or go-mms's default.
func controlTime(q *TimeQuality) *mms.Value {
	now := time.Now().UTC()
	if q == nil {
		return mms.NewUTCTime(now)
	}
	return mms.NewUTCTimeWithQuality(now, encodeTimeQuality(*q))
}

// zeroUTCTime is an operTm of an immediate command: all octets zero (IEC
// 61850-8-1 20.10.2.2).
func zeroUTCTime() *mms.Value { return mms.NewUTCTimeWithQuality(time.Unix(0, 0).UTC(), 0) }

// buildOper constructs the Oper MMS structure for direct-operate or
// select-with-value (SBOw), with operTm only when p.OperTm is set (see
// [encodeOper]).
func buildOper(p OperateParams) *mms.Value { return encodeOper(p, !p.OperTm.IsZero()) }

// encodeOper constructs the Oper/SBOw structure (IEC 61850-8-1 Tables 85,
// 90):
//
//	Oper ::= SEQUENCE {
//	    ctlVal     <type>,
//	    operTm     UTC-Time OPTIONAL,  -- present iff withOperTm
//	    origin     Origin,
//	    ctlNum     INT8U,
//	    T          UTC-Time,
//	    Test       BOOLEAN,
//	    Check      BIT STRING (SIZE(2))
//	}
//
// operTm is present when the control's Oper type has it (IEC 61850-7-2
// 20.5.2.8), all zero for an immediate command. Servers whose OperSPC
// has no operTm (libiec61850, iec61850bean without time activation)
// refuse a 7-member structure with type-inconsistent.
func encodeOper(p OperateParams, withOperTm bool) *mms.Value {
	origin := p.Origin
	if origin == nil {
		origin = &Origin{OrCat: OrCatRemoteControl}
	}
	m := make([]*mms.Value, 0, 7)
	m = append(m, p.CtlVal)
	if withOperTm {
		m = append(m, operTmValue(p.OperTm))
	}
	return mms.NewStructure(append(m,
		origin.toMMS(),
		mms.NewUnsigned(uint64(p.CtlNum)),
		controlTime(p.TQuality),
		mms.NewBoolean(p.Test),
		p.Check.toMMS(),
	))
}

func operTmValue(t time.Time) *mms.Value {
	if t.IsZero() {
		return zeroUTCTime()
	}
	return mms.NewUTCTime(t)
}

// buildCancel constructs the Cancel MMS structure, with operTm only when
// p.OperTm is set (see [encodeCancel]).
func buildCancel(p CancelParams) *mms.Value { return encodeCancel(p, !p.OperTm.IsZero()) }

// encodeCancel constructs the Cancel structure (IEC 61850-8-1 Table 88):
//
//	Cancel ::= SEQUENCE {
//	    ctlVal     <type>,
//	    operTm     UTC-Time OPTIONAL,  -- present iff withOperTm
//	    origin     Origin,
//	    ctlNum     INT8U,
//	    T          UTC-Time,
//	    Test       BOOLEAN
//	}
func encodeCancel(p CancelParams, withOperTm bool) *mms.Value {
	origin := p.Origin
	if origin == nil {
		origin = &Origin{OrCat: OrCatRemoteControl}
	}
	m := make([]*mms.Value, 0, 6)
	m = append(m, p.CtlVal)
	if withOperTm {
		m = append(m, operTmValue(p.OperTm))
	}
	return mms.NewStructure(append(m,
		origin.toMMS(),
		mms.NewUnsigned(uint64(p.CtlNum)),
		controlTime(p.TQuality),
		mms.NewBoolean(p.Test),
	))
}
