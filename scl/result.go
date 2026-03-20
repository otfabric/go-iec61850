package scl

// DocumentKind describes the type of an SCL document based on its
// file extension or content heuristics.
type DocumentKind string

const (
	KindUnknown DocumentKind = "unknown"
	KindSCD     DocumentKind = "scd"
	KindCID     DocumentKind = "cid"
	KindICD     DocumentKind = "icd"
	KindIID     DocumentKind = "iid"
	KindSSD     DocumentKind = "ssd"
)

// DiagSeverity indicates the importance of a diagnostic message.
type DiagSeverity string

const (
	DiagError   DiagSeverity = "error"
	DiagWarning DiagSeverity = "warning"
	DiagInfo    DiagSeverity = "info"
)

// Diagnostic represents a single issue found during parsing or
// validation.
type Diagnostic struct {
	Severity DiagSeverity
	Code     string
	Path     string
	Message  string
}

// Result holds the output of a full parse operation, including
// the normalized model, version information, diagnostics, and
// document kind.
type Result struct {
	Version     VersionInfo
	Kind        DocumentKind
	Document    *SCL
	Diagnostics []Diagnostic
}

// HasErrors returns true if any diagnostic has error severity.
func (r *Result) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == DiagError {
			return true
		}
	}
	return false
}

// DetectKind infers the document kind from the model's content.
// This is more reliable than file-extension heuristics.
//
// Classification rules:
//   - SSD: substations present but no IEDs
//   - ICD: exactly 1 IED, no communication bindings
//   - IID: exactly 1 IED, no communication bindings (same as ICD)
//   - CID: exactly 1 IED, has communication bindings
//   - SCD: multiple IEDs, or 1+ IEDs with substations
//   - unknown: empty or unclassifiable
func DetectKind(s *SCL) DocumentKind {
	nIEDs := len(s.IEDs)
	hasSub := len(s.Substations) > 0
	hasComm := s.Communication != nil && len(s.Communication.SubNetworks) > 0

	switch {
	case nIEDs == 0 && hasSub:
		return KindSSD
	case nIEDs == 0:
		return KindUnknown
	case nIEDs == 1 && !hasComm:
		return KindICD
	case nIEDs == 1 && hasComm && !hasSub:
		return KindCID
	case nIEDs == 1 && hasComm && hasSub:
		return KindSCD
	case nIEDs > 1:
		return KindSCD
	default:
		return KindUnknown
	}
}

// ParseOptions controls the behavior of [ParseBytes] and [ParseFileOpts].
type ParseOptions struct {
	// ValidateSemantic runs semantic validation after parsing and
	// appends any findings to the result diagnostics.
	ValidateSemantic bool

	// Strict, when true, causes parsing to return an error if any
	// diagnostic has error severity (including semantic validation
	// errors when ValidateSemantic is enabled).
	Strict bool

	// Kind overrides automatic document kind detection.
	// When empty, the kind is inferred from the file extension
	// or content.
	Kind DocumentKind

	// MaxDiagnostics, when > 0, truncates the diagnostic list
	// to at most this many entries.
	MaxDiagnostics int
}
