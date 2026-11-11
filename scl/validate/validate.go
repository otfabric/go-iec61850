// SPDX-License-Identifier: MIT

// Package validate provides semantic validation of a parsed SCL model.
//
// Validation is split into independent passes that each check a
// specific aspect of the model. All passes accept the normalized
// [scl.SCL] model and a shared [index.Index].
package validate

import (
	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Options controls which validation passes are executed.
// A zero-value Options enables all passes.
type Options struct {
	SkipTemplates     bool
	SkipIEDs          bool
	SkipCommunication bool
	SkipAddresses     bool
	SkipDatasets      bool
	SkipControls      bool
	SkipTopology      bool
	SkipRequired      bool

	// Kind, when set, checks the content the document kind requires
	// ([scl.ValidateKind]).
	Kind scl.DocumentKind
}

// All runs every validation pass and returns the combined diagnostics.
// The index build diagnostics (duplicates) are included first, followed
// by template, IED, communication, address, dataset, control, topology
// and required-attribute checks.
func All(s *scl.SCL, idx *index.Index, indexDiags []scl.Diagnostic) []scl.Diagnostic {
	return WithOptions(s, idx, indexDiags, Options{})
}

// WithOptions runs validation passes selected by opts.
func WithOptions(s *scl.SCL, idx *index.Index, indexDiags []scl.Diagnostic, opts Options) []scl.Diagnostic {
	var diags []scl.Diagnostic
	diags = append(diags, indexDiags...)
	if !opts.SkipTemplates {
		diags = append(diags, Templates(s, idx)...)
	}
	if !opts.SkipIEDs {
		diags = append(diags, IEDs(s, idx)...)
	}
	if !opts.SkipCommunication {
		diags = append(diags, Communication(s, idx)...)
	}
	if !opts.SkipAddresses {
		diags = append(diags, Addresses(s, idx)...)
	}
	if !opts.SkipDatasets {
		diags = append(diags, Datasets(s, idx)...)
	}
	if !opts.SkipControls {
		diags = append(diags, Controls(s, idx)...)
	}
	if !opts.SkipTopology {
		diags = append(diags, Topology(s, idx)...)
	}
	if !opts.SkipRequired {
		diags = append(diags, scl.ValidateRequired(s)...)
	}
	if opts.Kind != "" {
		diags = append(diags, scl.ValidateKind(s, opts.Kind)...)
	}
	return diags
}
