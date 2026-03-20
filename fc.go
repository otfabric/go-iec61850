package iec61850

import "fmt"

// FunctionalConstraint identifies the purpose of a data attribute
// within the IEC 61850 data model. Each FC classifies what kind of
// information the attribute carries (status, measured value, setpoint,
// configuration, etc.).
type FunctionalConstraint string

const (
	// FCST is Status information.
	FCST FunctionalConstraint = "ST"
	// FCMX is Measurands (analog values).
	FCMX FunctionalConstraint = "MX"
	// FCSP is Setpoint.
	FCSP FunctionalConstraint = "SP"
	// FCSV is Substitution.
	FCSV FunctionalConstraint = "SV"
	// FCCF is Configuration.
	FCCF FunctionalConstraint = "CF"
	// FCDC is Description.
	FCDC FunctionalConstraint = "DC"
	// FCSG is Setting group.
	FCSG FunctionalConstraint = "SG"
	// FCSE is Setting group editable.
	FCSE FunctionalConstraint = "SE"
	// FCSR is Service response / service tracking.
	FCSR FunctionalConstraint = "SR"
	// FCOR is Operate received.
	FCOR FunctionalConstraint = "OR"
	// FCBL is Blocking.
	FCBL FunctionalConstraint = "BL"
	// FCEX is Extended definition.
	FCEX FunctionalConstraint = "EX"
	// FCCO is Control.
	FCCO FunctionalConstraint = "CO"
	// FCUS is Unicast SV.
	FCUS FunctionalConstraint = "US"
	// FCMS is Multicast SV.
	FCMS FunctionalConstraint = "MS"
	// FCRP is Unbuffered report.
	FCRP FunctionalConstraint = "RP"
	// FCBR is Buffered report.
	FCBR FunctionalConstraint = "BR"
	// FCLG is Log control blocks.
	FCLG FunctionalConstraint = "LG"
	// FCGO is GOOSE control blocks.
	FCGO FunctionalConstraint = "GO"
)

// ParseFC parses a string as a functional constraint.
// Returns [ErrInvalidFunctionalConstraint] if the string is not a
// recognized FC value.
func ParseFC(s string) (FunctionalConstraint, error) {
	fc := FunctionalConstraint(s)
	if !fc.IsValid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidFunctionalConstraint, s)
	}
	return fc, nil
}

// IsValid reports whether fc is a recognized functional constraint.
func (fc FunctionalConstraint) IsValid() bool {
	switch fc {
	case FCST, FCMX, FCSP, FCSV, FCCF, FCDC, FCSG, FCSE,
		FCSR, FCOR, FCBL, FCEX, FCCO, FCUS, FCMS, FCRP,
		FCBR, FCLG, FCGO:
		return true
	default:
		return false
	}
}

// Description returns a human-readable description of the functional
// constraint (e.g., "Status" for ST). Returns "Unknown" for
// unrecognized values.
func (fc FunctionalConstraint) Description() string {
	switch fc {
	case FCST:
		return "Status"
	case FCMX:
		return "Measurands"
	case FCSP:
		return "Setpoint"
	case FCSV:
		return "Substitution"
	case FCCF:
		return "Configuration"
	case FCDC:
		return "Description"
	case FCSG:
		return "Setting group"
	case FCSE:
		return "Setting group editable"
	case FCSR:
		return "Service response"
	case FCOR:
		return "Operate received"
	case FCBL:
		return "Blocking"
	case FCEX:
		return "Extended definition"
	case FCCO:
		return "Control"
	case FCUS:
		return "Unicast SV"
	case FCMS:
		return "Multicast SV"
	case FCRP:
		return "Unbuffered report"
	case FCBR:
		return "Buffered report"
	case FCLG:
		return "Log control"
	case FCGO:
		return "GOOSE control"
	default:
		return "Unknown"
	}
}

// String returns the two-letter FC code.
func (fc FunctionalConstraint) String() string {
	return string(fc)
}

// allFCs is the canonical list of FC values, allocated once.
var allFCs = []FunctionalConstraint{
	FCST, FCMX, FCSP, FCSV, FCCF, FCDC, FCSG, FCSE,
	FCSR, FCOR, FCBL, FCEX, FCCO, FCUS, FCMS, FCRP,
	FCBR, FCLG, FCGO,
}

// AllFunctionalConstraints returns all valid functional constraint
// values in a stable, specification-defined order. The returned
// slice is a copy — callers may modify it freely.
func AllFunctionalConstraints() []FunctionalConstraint {
	cp := make([]FunctionalConstraint, len(allFCs))
	copy(cp, allFCs)
	return cp
}
