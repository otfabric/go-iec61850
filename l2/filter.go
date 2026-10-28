// SPDX-License-Identifier: MIT

package l2

// bpfInsn is a classic BPF instruction, converted to the OS-specific
// type by each raw backend.
type bpfInsn struct {
	code   uint16
	jt, jf uint8
	k      uint32
}

// Classic BPF opcodes used by etherTypeFilter.
const (
	bpfLdhAbs = 0x28 // BPF_LD | BPF_H | BPF_ABS
	bpfJeqK   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
	bpfRetK   = 0x06 // BPF_RET | BPF_K
)

// maxEtherTypes bounds the EtherType list so that every jump offset of
// the generated program fits in a byte.
const maxEtherTypes = 32

// etherTypes returns the configured EtherTypes, defaulting to GOOSE.
func etherTypes(opts TransportOptions) ([]uint16, error) {
	if len(opts.EtherTypes) == 0 {
		return []uint16{EtherTypeGOOSE}, nil
	}
	if len(opts.EtherTypes) > maxEtherTypes {
		return nil, &ConfigError{Field: "EtherTypes", Message: "too many EtherTypes"}
	}
	for _, et := range opts.EtherTypes {
		if et < 0x0600 || et == EtherTypeVLAN {
			return nil, &ConfigError{Field: "EtherTypes", Message: "invalid EtherType"}
		}
	}
	return opts.EtherTypes, nil
}

// etherTypeFilter accepts untagged and single 802.1Q-tagged frames whose
// EtherType is one of types (1 <= len(types) <= maxEtherTypes):
//
//	ldh [12]
//	jeq #t0, accept ... jeq #tN-1, accept
//	jeq #0x8100, next, reject
//	ldh [16]
//	jeq #t0, accept ... jeq #tN-1, accept
//	reject: ret #0
//	accept: ret #65535
func etherTypeFilter(types []uint16) []bpfInsn {
	n := len(types)
	// Layout: 0 ldh; 1..n jeq; n+1 jeq vlan; n+2 ldh; n+3..2n+2 jeq;
	// 2n+3 reject; 2n+4 accept.
	reject, accept := 2*n+3, 2*n+4
	prog := make([]bpfInsn, 0, 2*n+5)
	prog = append(prog, bpfInsn{code: bpfLdhAbs, k: 12})
	for _, et := range types {
		pc := len(prog)
		prog = append(prog, bpfInsn{code: bpfJeqK, jt: uint8(accept - pc - 1), k: uint32(et)})
	}
	pc := len(prog)
	prog = append(prog, bpfInsn{code: bpfJeqK, jf: uint8(reject - pc - 1), k: uint32(EtherTypeVLAN)})
	prog = append(prog, bpfInsn{code: bpfLdhAbs, k: 16})
	for i, et := range types {
		pc := len(prog)
		jf := uint8(0)
		if i == n-1 {
			jf = uint8(reject - pc - 1)
		}
		prog = append(prog, bpfInsn{code: bpfJeqK, jt: uint8(accept - pc - 1), jf: jf, k: uint32(et)})
	}
	prog = append(prog, bpfInsn{code: bpfRetK, k: 0}, bpfInsn{code: bpfRetK, k: 0xffff})
	return prog
}
