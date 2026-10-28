// SPDX-License-Identifier: MIT

// Package l2 is the raw Ethernet layer shared by the IEC 61850 layer-2
// protocols: GOOSE (package goose, EtherType 0x88B8) and Sampled Values
// (package sv, EtherType 0x88BA).
//
// It provides the [Transport] interface with operating-system backends
// ([Open]: Linux AF_PACKET, macOS/BSD BPF), an in-memory [Hub] with fault
// injection for tests and simulations, and a [Demux] that lets several
// protocol stacks share one transport, each receiving only its own
// EtherTypes. [MAC] is the hardware address type used throughout.
//
// The package is pure Go (golang.org/x/sys), without cgo.
package l2
