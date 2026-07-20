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
	SkipDatasets      bool
	SkipControls      bool
	SkipTopology      bool
}

// All runs every validation pass and returns the combined diagnostics.
// The index build diagnostics (duplicates) are included first, followed
// by template, IED, communication, dataset, control, and topology checks.
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
	if !opts.SkipDatasets {
		diags = append(diags, Datasets(s, idx)...)
	}
	if !opts.SkipControls {
		diags = append(diags, Controls(s, idx)...)
	}
	if !opts.SkipTopology {
		diags = append(diags, Topology(s, idx)...)
	}
	return diags
}
