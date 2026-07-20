// SPDX-License-Identifier: MIT

// Package index provides a shared, indexed view of a parsed SCL model.
//
// An [Index] is built once from a normalized [scl.SCL] document and
// gives O(1) lookup for IEDs, access points, logical devices, logical
// nodes, data type templates, datasets, report controls, and
// connected access points. Duplicate or ambiguous entries are reported
// as diagnostics rather than silently overwritten.
package index

// AccessPointKey uniquely identifies an access point within an IED.
type AccessPointKey struct {
	IED string
	AP  string
}

// LDeviceKey uniquely identifies a logical device within an IED.
type LDeviceKey struct {
	IED    string
	LDInst string
}

// LNKey uniquely identifies a logical node within a logical device.
type LNKey struct {
	IED     string
	LDInst  string
	Prefix  string
	LNClass string
	Inst    string
}

// DataSetKey uniquely identifies a dataset by its owning LN and name.
type DataSetKey struct {
	IED     string
	LDInst  string
	Prefix  string
	LNClass string
	LNInst  string
	Name    string
}

// ControlKey uniquely identifies a control block by its owning LN and name.
type ControlKey struct {
	IED     string
	LDInst  string
	Prefix  string
	LNClass string
	LNInst  string
	Name    string
}
