// SPDX-License-Identifier: MIT

package goose

// bpfInsn is a classic BPF instruction, converted to the OS-specific
// type by each raw backend.
type bpfInsn struct {
	code   uint16
	jt, jf uint8
	k      uint32
}

// gooseFilter accepts untagged and single 802.1Q-tagged GOOSE frames.
//
//	0: ldh [12]
//	1: jeq #0x88b8, 5, 2
//	2: jeq #0x8100, 3, 6
//	3: ldh [16]
//	4: jeq #0x88b8, 5, 6
//	5: ret #65535
//	6: ret #0
var gooseFilter = []bpfInsn{
	{code: 0x28, k: 12},
	{code: 0x15, jt: 3, jf: 0, k: uint32(EtherType)},
	{code: 0x15, jt: 0, jf: 3, k: uint32(EtherTypeVLAN)},
	{code: 0x28, k: 16},
	{code: 0x15, jt: 0, jf: 1, k: uint32(EtherType)},
	{code: 0x06, k: 0xffff},
	{code: 0x06, k: 0},
}
