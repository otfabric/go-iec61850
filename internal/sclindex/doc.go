// SPDX-License-Identifier: MIT

// Package sclindex is reserved for centralizing SCL DataTypeTemplates
// lookup and cycle-detection logic that is currently duplicated across
// the servermodel and (future) client-side model expansion packages.
//
// Planned contents:
//   - Indexed access to DOType, DAType, EnumType, and LNodeType by id.
//   - Cycle-safe recursive expansion of DO/DA hierarchies.
//   - Shared helpers for DAI/SDI override resolution.
//
// TODO: migrate indexDOTypes, indexDATypes, indexEnumTypes and related
// expansion functions from internal/servermodel/fromscl.go into this
// package once a second consumer (e.g. client-side SCL model) exists.
package sclindex
