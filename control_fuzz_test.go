// SPDX-License-Identifier: MIT

package iec61850

import (
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// fuzzControlMembers builds the members of an Oper/SBOw/Cancel structure
// from fuzz bytes: per member a kind byte and a number byte.
func fuzzControlMembers(b []byte) []*mms.Value {
	var out []*mms.Value
	for len(b) >= 2 && len(out) < 10 {
		kind, n := b[0], b[1]
		b = b[2:]
		switch kind % 8 {
		case 0:
			out = append(out, mms.NewBoolean(n%2 == 1))
		case 1:
			out = append(out, mms.NewUTCTime(time.Unix(int64(n)*1e6, 0)))
		case 2:
			out = append(out, mms.NewUnsigned(uint64(n)*3))
		case 3:
			out = append(out, mms.NewStructure([]*mms.Value{mms.NewInteger(int64(n) - 128), mms.NewOctetString(make([]byte, n%80))}))
		case 4:
			out = append(out, mms.NewBitStringWithLength([]byte{n}, int(n%9)))
		case 5:
			out = append(out, mms.NewInteger(int64(n)))
		case 6:
			out = append(out, mms.NewStructure([]*mms.Value{mms.NewBoolean(true)}))
		default:
			out = append(out, mms.NewVisibleString(string(rune('a'+n%26))))
		}
	}
	return out
}

// FuzzDecodeControlRequest: the strict Oper/SBOw/Cancel decoder (IEC
// 61850-8-1 Tables 85, 88, 90; SRV-02) never panics, and what it accepts
// has the layout of the service.
func FuzzDecodeControlRequest(f *testing.F) {
	f.Add(byte(0), []byte{0, 1, 3, 3, 2, 1, 1, 0, 0, 0, 4, 0x80})
	f.Add(byte(1), []byte{0, 1, 1, 9, 3, 3, 2, 1, 1, 0, 0, 1, 4, 0x42})
	f.Add(byte(2), []byte{0, 1, 3, 3, 2, 1, 1, 0, 0, 0})
	f.Add(byte(2), []byte{})
	ops := []string{"Oper", "SBOw", "Cancel", "SBO"}
	f.Fuzz(func(t *testing.T, op byte, b []byte) {
		operation := ops[int(op)%len(ops)]
		members := fuzzControlMembers(b)
		req, err := decodeControlRequest("LD/LN.DO", operation, mms.NewStructure(members))
		if err != nil {
			return
		}
		base := 6
		if operation == "Cancel" {
			base = 5
		}
		if len(members) != base && len(members) != base+1 {
			t.Fatalf("%s with %d members accepted", operation, len(members))
		}
		if req.CtlVal != members[0] || len(req.Origin.OrIdent) > 64 {
			t.Fatalf("decoded %+v", req)
		}
		if len(members) == base && !req.OperTm.IsZero() {
			t.Fatal("OperTm without an operTm member")
		}
	})
}
