// SPDX-License-Identifier: MIT

package goose

import (
	"errors"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// ControlBlocksFromSCL expands the models of all IEDs of a document
// within one limit: at the limit both IEDs are read; one node fewer
// leaves the second out, with an error that wraps ErrSCL and
// scl.ErrLimitExceeded, and the blocks of the first are returned.
func TestControlBlocksFromSCL_ExpansionBudget(t *testing.T) {
	doc := loadSCL(t, "testdata/interop-goose.cid")
	withBudget := func(n int) ([]ControlBlock, error) {
		return ControlBlocksFromSCLWithOptions(doc, SCLOptions{MaxExpandedNodes: n})
	}
	one, err := ControlBlocksFromSCL(doc)
	if err != nil || len(one) == 0 {
		t.Fatalf("default limit: %d blocks, %v", len(one), err)
	}
	if neg, err := withBudget(-1); err != nil || len(neg) != len(one) {
		t.Fatalf("limit -1 (the default): %d blocks, %v", len(neg), err)
	}
	// The nodes of one IED: the smallest budget without a limit error.
	lo, hi := 1, 1<<20
	for lo < hi {
		mid := (lo + hi) / 2
		if _, err := withBudget(mid); errors.Is(err, scl.ErrLimitExceeded) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	perIED := lo
	if perIED <= 1 {
		t.Fatalf("a limit of 1 node builds the models: the expansion is not limited")
	}
	if _, err := withBudget(perIED - 1); !errors.Is(err, scl.ErrLimitExceeded) || !errors.Is(err, ErrSCL) {
		t.Fatalf("budget %d for %d nodes: %v", perIED-1, perIED, err)
	}

	second := doc.IEDs[0]
	second.Name += "COPY"
	doc.IEDs = append(doc.IEDs[:1:1], second)
	for _, tc := range []struct {
		budget, blocks int
		fail           bool
	}{
		{2 * perIED, 2 * len(one), false},
		{2*perIED - 1, len(one), true},
		{perIED, len(one), true},
	} {
		cbs, err := withBudget(tc.budget)
		if got := errors.Is(err, scl.ErrLimitExceeded); got != tc.fail || len(cbs) != tc.blocks {
			t.Errorf("budget %d, two IEDs of %d nodes: %d blocks, %v; want %d blocks, limit error %v", tc.budget, perIED, len(cbs), err, tc.blocks, tc.fail)
		}
	}
}
