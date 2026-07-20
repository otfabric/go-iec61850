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
// command. Bit 0 = synchrocheck, Bit 1 = interlockCheck.
type CheckConditions uint8

const (
	CheckSynchroCheck   CheckConditions = 1 << 0
	CheckInterlockCheck CheckConditions = 1 << 1
)

// Has reports whether the given check bit is set.
func (c CheckConditions) Has(flag CheckConditions) bool { return c&flag != 0 }

// toMMS encodes Check as a 2-bit bitstring.
func (c CheckConditions) toMMS() *mms.Value {
	return mms.NewBitStringWithLength([]byte{byte(c) << 6}, 2)
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

	// CtlNum is the control sequence number. For enhanced security
	// models, this must be unique per select/operate cycle. When
	// zero, the library auto-increments an internal counter.
	CtlNum uint8

	// OperTm is the scheduled operation time. A zero value means
	// "operate now" (no timed command).
	OperTm time.Time

	// Test, when true, sets the Test bit to indicate a test command
	// that should not cause physical action.
	Test bool

	// Check specifies synchrocheck/interlockCheck conditions.
	Check CheckConditions
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
	// original Operate.
	CtlNum uint8

	// OperTm is the scheduled operation time from the original
	// Operate. A zero value means the original was immediate.
	OperTm time.Time
}

// AddCause represents the AdditionalCause field in LastApplError,
// indicating why a control command was refused or failed.
type AddCause int

const (
	AddCauseUnknown               AddCause = 0
	AddCauseNotSupported          AddCause = 1
	AddCauseBlocked               AddCause = 2
	AddCauseSelectFailed          AddCause = 3
	AddCauseInvalidPosition       AddCause = 4
	AddCausePositionReached       AddCause = 5
	AddCauseParameterChange       AddCause = 6
	AddCauseStepLimit             AddCause = 7
	AddCauseBlockedBySwitch       AddCause = 8
	AddCauseBlockedByInterlocking AddCause = 9
	AddCauseBlockedBySynchrocheck AddCause = 10
	AddCauseCommandAlreadyExec    AddCause = 11
	AddCauseBlockedByHealth       AddCause = 12
	AddCause1of1                  AddCause = 13
	AddCauseAbort                 AddCause = 14
	AddCauseTimeLimit             AddCause = 15
	AddCauseBlockedByMode         AddCause = 16
	AddCauseBlockedByProcess      AddCause = 17
)

// String returns the additional cause name.
func (c AddCause) String() string {
	switch c {
	case AddCauseUnknown:
		return "unknown"
	case AddCauseNotSupported:
		return "not-supported"
	case AddCauseBlocked:
		return "blocked-by-switching-hierarchy"
	case AddCauseSelectFailed:
		return "select-failed"
	case AddCauseInvalidPosition:
		return "invalid-position"
	case AddCausePositionReached:
		return "position-reached"
	case AddCauseParameterChange:
		return "parameter-change-in-execution"
	case AddCauseStepLimit:
		return "step-limit"
	case AddCauseBlockedBySwitch:
		return "blocked-by-other"
	case AddCauseBlockedByInterlocking:
		return "blocked-by-interlocking"
	case AddCauseBlockedBySynchrocheck:
		return "blocked-by-synchrocheck"
	case AddCauseCommandAlreadyExec:
		return "command-already-in-execution"
	case AddCauseBlockedByHealth:
		return "blocked-by-health"
	case AddCause1of1:
		return "1-of-n-control"
	case AddCauseAbort:
		return "abort"
	case AddCauseTimeLimit:
		return "time-limit-over"
	case AddCauseBlockedByMode:
		return "blocked-by-mode"
	case AddCauseBlockedByProcess:
		return "blocked-by-process"
	default:
		return fmt.Sprintf("addCause(%d)", int(c))
	}
}

// LastApplError contains the decoded result of a LastApplError read,
// providing server-side failure reason for a control command.
type LastApplError struct {
	// CntrlObj is the object reference of the control that failed.
	CntrlObj string

	// Error is the IEC 61850 service error class.
	Error int

	// Origin is the originator that issued the failed command.
	Origin Origin

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

// DpCtlVal creates a Dbpos (double-point) control value using
// a two-bit bitstring: 01=off, 10=on.
func DpCtlVal(on bool) *mms.Value {
	if on {
		return mms.NewBitStringWithLength([]byte{0x80}, 2) // 10
	}
	return mms.NewBitStringWithLength([]byte{0x40}, 2) // 01
}

// buildOper constructs the Oper MMS structure for direct-operate
// or select-with-value (SBOw). The structure matches:
//
//	Oper ::= SEQUENCE {
//	    ctlVal     <type>,
//	    operTm     UTC-Time,
//	    origin     Origin,
//	    ctlNum     INT8U,
//	    T          UTC-Time,
//	    Test       BOOLEAN,
//	    Check      BIT STRING (SIZE(2))
//	}
func buildOper(p OperateParams) *mms.Value {
	origin := p.Origin
	if origin == nil {
		origin = &Origin{OrCat: OrCatRemoteControl}
	}

	// operTm is optional: only include it when a future activation time is
	// requested. Many servers (libiec61850, iec61850bean) define OperSPC/DPC
	// without operTm and will reject a 7-element structure with TypeInconsistent.
	if !p.OperTm.IsZero() {
		return mms.NewStructure([]*mms.Value{
			p.CtlVal,
			mms.NewUTCTime(p.OperTm),
			origin.toMMS(),
			mms.NewUnsigned(uint64(p.CtlNum)),
			mms.NewUTCTime(time.Now().UTC()),
			mms.NewBoolean(p.Test),
			p.Check.toMMS(),
		})
	}

	return mms.NewStructure([]*mms.Value{
		p.CtlVal,
		origin.toMMS(),
		mms.NewUnsigned(uint64(p.CtlNum)),
		mms.NewUTCTime(time.Now().UTC()),
		mms.NewBoolean(p.Test),
		p.Check.toMMS(),
	})
}

// buildCancel constructs the Cancel MMS structure:
//
//	Cancel ::= SEQUENCE {
//	    ctlVal     <type>,
//	    operTm     UTC-Time,
//	    origin     Origin,
//	    ctlNum     INT8U,
//	    T          UTC-Time,
//	    Test       BOOLEAN (always false for cancel)
//	}
func buildCancel(p CancelParams) *mms.Value {
	origin := p.Origin
	if origin == nil {
		origin = &Origin{OrCat: OrCatRemoteControl}
	}

	if !p.OperTm.IsZero() {
		return mms.NewStructure([]*mms.Value{
			p.CtlVal,
			mms.NewUTCTime(p.OperTm),
			origin.toMMS(),
			mms.NewUnsigned(uint64(p.CtlNum)),
			mms.NewUTCTime(time.Now().UTC()),
			mms.NewBoolean(false),
		})
	}

	return mms.NewStructure([]*mms.Value{
		p.CtlVal,
		origin.toMMS(),
		mms.NewUnsigned(uint64(p.CtlNum)),
		mms.NewUTCTime(time.Now().UTC()),
		mms.NewBoolean(false),
	})
}
