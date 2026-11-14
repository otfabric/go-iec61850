// SPDX-License-Identifier: MIT

package scl

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/otfabric/go-iec61850/scl/internal/diagname"
)

// ErrLimitExceeded is wrapped by the error of a parse whose document
// goes beyond a limit of [ParseOptions] ([ParseOptions.MaxBytes],
// [ParseOptions.MaxDepth]), and by the error or diagnostic of a type
// expansion that goes beyond [DefaultMaxExpandedNodes],
// [DefaultMaxTypeDepth] or the limit given in its options
// ([FlattenOptions.MaxExpandedNodes], [FlattenOptions.MaxTypeDepth]). Test it
// with [errors.Is]. SECURITY.md lists every limit and its default.
var ErrLimitExceeded = errors.New("scl: limit exceeded")

// Defaults of the limits of [ParseOptions] and [FlattenOptions]. Each
// applies when its option is zero; an option raises or lowers it.
const (
	// DefaultMaxBytes is the largest document [ParseBytes],
	// [ParseFileOpts], [Parse] and [ParseWithOptions] read: 1 GiB.
	// Substation configuration documents are a few hundred megabytes
	// at the most.
	DefaultMaxBytes int64 = 1 << 30

	// DefaultMaxDepth is the deepest element nesting a document may
	// have, the root element being at depth 1, and also for content
	// that is skipped (unknown elements, the content of Private): 1024.
	// The SCL schema nests its elements about a dozen levels deep.
	DefaultMaxDepth = 1024

	// DefaultMaxExpandedNodes is the number of data object and data
	// attribute instances (DO, SDO, DA and BDA, each counted once per
	// place in the model it is expanded to) one expansion of the type
	// templates of a document may produce: 33,554,432. The vendor
	// documents of the test data expand to at most 20 nodes per KiB of
	// XML and to about 32,000 nodes for their largest IED; this is more
	// than a document of [DefaultMaxBytes] gives at that density, and
	// more than a thousand such IEDs.
	DefaultMaxExpandedNodes = 1 << 25

	// DefaultMaxDiagnosticName is the longest name, in bytes, that a
	// diagnostic quotes from the document; a longer one is cut at a
	// character boundary and ends with "…". Names of the SCL schema
	// are at most 64 characters long.
	DefaultMaxDiagnosticName = diagname.Max

	// DefaultMaxTypeDepth is the number of type references one path of
	// a type expansion may follow: 64. A data object's DOType is the
	// first, each SDO's DOType and each Struct DA's or BDA's DAType below
	// it one more. The common data classes of IEC 61850-7-3 and the
	// documents of the test data nest them less than ten deep.
	DefaultMaxTypeDepth = 64
)

// parseLimits are the limits of one parse, with the defaults applied.
type parseLimits struct {
	maxBytes int64
	maxDepth int
	nameCap  int
}

func (o ParseOptions) limits() parseLimits {
	l := parseLimits{maxBytes: o.MaxBytes, maxDepth: o.MaxDepth, nameCap: o.MaxDiagnosticName}
	if l.maxBytes <= 0 {
		l.maxBytes = DefaultMaxBytes
	}
	if l.maxDepth <= 0 {
		l.maxDepth = DefaultMaxDepth
	}
	if l.nameCap <= 0 {
		l.nameCap = DefaultMaxDiagnosticName
	}
	return l
}

// sizeError is the error for a document of more than max bytes.
func sizeError(max int64) error {
	return fmt.Errorf("%w: the document is larger than %d bytes (ParseOptions.MaxBytes)", ErrLimitExceeded, max)
}

// diagSink collects the diagnostics of a parse or a validation: at most
// max of them (none when max is 0), each quoting names of at most
// nameCap bytes.
type diagSink struct {
	diags   []Diagnostic
	max     int
	nameCap int
}

func newDiagSink(max, nameCap int) *diagSink {
	if max < 0 {
		max = 0
	}
	if nameCap <= 0 {
		nameCap = DefaultMaxDiagnosticName
	}
	return &diagSink{max: max, nameCap: nameCap}
}

// full reports whether the sink takes no further diagnostic.
func (s *diagSink) full() bool { return s.max > 0 && len(s.diags) >= s.max }

func (s *diagSink) add(d Diagnostic) {
	if !s.full() {
		s.diags = append(s.diags, d)
	}
}

// name returns n as a diagnostic quotes it: cut to the sink's name
// length at a character boundary, marked with "…" when it was cut.
func (s *diagSink) name(n string) string { return capName(n, s.nameCap) }

// countMessage is the message of an invalid-count diagnostic: the count
// attribute and the parse error, both quoting the attribute cut to the
// sink's name length.
func (s *diagSink) countMessage(count string, err error) string {
	if ne, ok := errors.AsType[*strconv.NumError](err); ok {
		c := *ne
		c.Num = s.name(c.Num)
		err = &c
	}
	return fmt.Sprintf("invalid count %q: %v", s.name(count), err)
}

func capName(n string, max int) string { return diagname.Cap(n, max) }
