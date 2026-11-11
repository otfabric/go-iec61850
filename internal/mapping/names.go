// SPDX-License-Identifier: MIT

// Package mapping provides internal helpers for translating between
// MMS named variables and IEC 61850 model structure.
package mapping

import (
	"slices"
	"sort"
	"strings"
)

// ExtractLogicalNodes returns the unique logical node names from a list
// of MMS item IDs within a domain. Only item IDs that follow the
// IEC 61850 convention (LN$FC$path..., at least two $-delimited
// segments) are considered. Plain MMS variables without $ separators
// are ignored.
//
// The returned names are sorted alphabetically.
func ExtractLogicalNodes(items []string) []string {
	seen := make(map[string]struct{}, len(items)/4)
	for _, item := range items {
		pv, ok := ParseItemID(item)
		if !ok {
			continue
		}
		seen[pv.LN] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ParsedVariable represents a single MMS variable name decomposed into
// IEC 61850 components.
type ParsedVariable struct {
	LN   string
	FC   string
	Path []string // DO/DA path segments after LN$FC
}

// ParseItemID decomposes an MMS item ID into its IEC 61850 components.
// Returns ok=false if the item ID has fewer than 2 segments (LN + FC),
// or if any segment (LN, FC, or path component) is empty.
func ParseItemID(itemID string) (ParsedVariable, bool) {
	parts := strings.Split(itemID, "$")
	if len(parts) < 2 {
		return ParsedVariable{}, false
	}

	ln := parts[0]
	fc := parts[1]
	if ln == "" || fc == "" {
		return ParsedVariable{}, false
	}

	var path []string
	if len(parts) > 2 {
		path = parts[2:]
		if slices.Contains(path, "") {
			return ParsedVariable{}, false
		}
	}

	return ParsedVariable{LN: ln, FC: fc, Path: path}, true
}

// controlBlockFCs are the FC components that hold control blocks, not
// data (IEC 61850-8-1 Figure 3/4: RP, BR URCB/BRCB, LG LCB, GO, GS GoCB
// and GsCB, MS, US MSVCB/USVCB).
var controlBlockFCs = map[string]bool{"RP": true, "BR": true, "LG": true, "GO": true, "GS": true, "MS": true, "US": true}

// IsControlBlockItem reports whether a parsed item is (part of) a control
// block rather than a data object: an item of a control block FC, or the
// SGCB of LLN0 (FC SP, IEC 61850-8-1 Table 60).
func IsControlBlockItem(pv ParsedVariable) bool {
	return controlBlockFCs[pv.FC] || pv.FC == "SP" && pv.LN == "LLN0" && len(pv.Path) > 0 && pv.Path[0] == "SGCB"
}

// ExtractDataObjects returns the unique top-level data object names
// for a given logical node from a list of MMS item IDs. Only items
// matching the specified LN are considered. The DO name is the third
// $-delimited segment (position after LN$FC$). Control blocks (see
// [IsControlBlockItem]) are not data objects (IEC 61850-7-2
// GetLogicalNodeDirectory per ACSI class) and are left out.
//
// The returned names are sorted alphabetically.
func ExtractDataObjects(items []string, lnName string) []string {
	seen := make(map[string]struct{})
	prefix := lnName + "$"
	for _, item := range items {
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		pv, ok := ParseItemID(item)
		if !ok || len(pv.Path) == 0 || IsControlBlockItem(pv) {
			continue
		}
		seen[pv.Path[0]] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ExtractChildren returns the unique direct child names at a given
// path depth for a specific LN. The basePath identifies the parent
// (e.g., ["Mod"] for children of LN$FC$Mod). Only items that extend
// beyond basePath by exactly one segment are included. At the LN level
// (empty basePath) control blocks are left out, as in
// [ExtractDataObjects]; below a name, its items are listed.
//
// The returned names are sorted alphabetically.
func ExtractChildren(items []string, lnName string, basePath []string) []string {
	seen := make(map[string]struct{})
	prefix := lnName + "$"
	targetDepth := len(basePath) + 1

	for _, item := range items {
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		pv, ok := ParseItemID(item)
		if !ok || len(pv.Path) < targetDepth || len(basePath) == 0 && IsControlBlockItem(pv) {
			continue
		}
		if !pathHasPrefix(pv.Path, basePath) {
			continue
		}
		seen[pv.Path[len(basePath)]] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GroupByFC groups MMS item IDs by functional constraint for a given LN.
// Returns a map from FC string to the list of item IDs with that FC.
func GroupByFC(items []string, lnName string) map[string][]string {
	groups := make(map[string][]string)
	prefix := lnName + "$"
	for _, item := range items {
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		pv, ok := ParseItemID(item)
		if !ok {
			continue
		}
		groups[pv.FC] = append(groups[pv.FC], item)
	}
	return groups
}

// ExtractFCsForLN returns the unique FCs observed for a given LN
// across all its MMS item IDs. The returned strings are sorted.
func ExtractFCsForLN(items []string, lnName string) []string {
	seen := make(map[string]struct{})
	prefix := lnName + "$"
	for _, item := range items {
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		pv, ok := ParseItemID(item)
		if !ok {
			continue
		}
		seen[pv.FC] = struct{}{}
	}
	fcs := make([]string, 0, len(seen))
	for fc := range seen {
		fcs = append(fcs, fc)
	}
	sort.Strings(fcs)
	return fcs
}

// ExtractFCsForPath returns the unique FCs observed for items at or
// under a given path within a LN. The returned strings are sorted.
func ExtractFCsForPath(items []string, lnName string, basePath []string) []string {
	seen := make(map[string]struct{})
	prefix := lnName + "$"
	for _, item := range items {
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		pv, ok := ParseItemID(item)
		if !ok || len(pv.Path) < len(basePath) {
			continue
		}
		if !pathHasPrefix(pv.Path, basePath) {
			continue
		}
		seen[pv.FC] = struct{}{}
	}
	fcs := make([]string, 0, len(seen))
	for fc := range seen {
		fcs = append(fcs, fc)
	}
	sort.Strings(fcs)
	return fcs
}

func pathHasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if path[i] != p {
			return false
		}
	}
	return true
}
