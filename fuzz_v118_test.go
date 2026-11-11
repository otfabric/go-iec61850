// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/goose"
	"github.com/otfabric/go-iec61850/internal/mapping"
	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// Fuzz targets for the decoders and server paths of v1.18.0: setting
// group control blocks and value banks, timestamps with TimeQuality,
// LastApplError, MSVCB, the item IDs a client writes and the BRCB buffer.
// MMS Data comes from arbitrary BER through the GOOSE allData decoder,
// as in FuzzDecodeGoCB.

func encodeSeed(f *testing.F, v *mms.Value) {
	f.Helper()
	b, err := goose.EncodeData(v)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
}

// FuzzDecodeSGCB: the client's SGCB decoder (IEC 61850-8-1 Table 60)
// never panics, and what it accepts has groups within INT8U and an ActSG
// that a server can report.
func FuzzDecodeSGCB(f *testing.F) {
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(3), mms.NewUnsigned(1), mms.NewUnsigned(0), mms.NewBoolean(false),
		mms.NewUTCTime(time.Unix(1_700_000_000, 0)), mms.NewUnsigned(30),
	}))
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(256), mms.NewUnsigned(1), mms.NewUnsigned(0), mms.NewBoolean(false), mms.NewBoolean(true),
	}))
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewUnsigned(2), mms.NewUnsigned(2), mms.NewUnsigned(2), mms.NewBoolean(true),
		mms.NewInteger(0), mms.NewUnsigned(70000),
	}))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := goose.DecodeData(b)
		if err != nil {
			return
		}
		info, err := decodeSGCB(v)
		if err != nil {
			if info != nil {
				t.Fatal("SGCB and an error")
			}
			return
		}
		if info == nil {
			t.Fatal("nil SGCB without error")
		}
		members, _ := v.Structure()
		for i, got := range []uint8{info.NumOfSGs, info.ActSG, info.EditSG} {
			if u, _ := members[i].Uint64(); u != uint64(got) {
				t.Fatalf("member %d: %d decoded as %d", i, u, got)
			}
		}
	})
}

// FuzzTimestampRoundTrip: EncodeTimestamp and DecodeTimestamp keep the
// TimeQuality octet (IEC 61850-8-1 8.1.3.7) and the time to the 2^-24 s
// of the UtcTime fraction; DecodeTimestamp never panics on other Data.
func FuzzTimestampRoundTrip(f *testing.F) {
	f.Add(uint32(0), uint32(0), byte(0), []byte{})
	f.Add(uint32(1_700_000_000), uint32(999_999_999), byte(0xff), []byte{0x91, 0x08, 1, 2, 3, 4, 5, 6, 7, 0x0a})
	f.Add(uint32(math.MaxUint32), uint32(500_000_000), byte(0x2a), []byte{0x83, 0x01, 0x00})
	f.Fuzz(func(t *testing.T, sec, nsec uint32, q byte, raw []byte) {
		want := Timestamp{
			Time: time.Unix(int64(sec), int64(nsec%1_000_000_000)).UTC(),
			Quality: TimeQuality{
				LeapSecondKnown:      q&0x80 != 0,
				ClockFailure:         q&0x40 != 0,
				ClockNotSynchronized: q&0x20 != 0,
				TimeAccuracy:         int(q & 0x1f),
			},
		}
		got, err := DecodeTimestamp(EncodeTimestamp(want))
		if err != nil {
			t.Fatalf("decode of an encoded timestamp: %v", err)
		}
		if got.Quality != want.Quality {
			t.Fatalf("quality %+v, want %+v", got.Quality, want.Quality)
		}
		if d := got.Time.Sub(want.Time).Abs(); d > time.Second>>24+time.Nanosecond {
			t.Fatalf("time %v, want %v (off by %v)", got.Time, want.Time, d)
		}
		if v, err := goose.DecodeData(raw); err == nil {
			if _, err := DecodeTimestamp(v); err != nil {
				var de *DecodeError
				if !errors.As(err, &de) {
					t.Fatalf("error is not a DecodeError: %v", err)
				}
			}
		}
	})
}

// FuzzDecodeLastApplError: the strict LastApplError decoder (IEC
// 61850-8-1 20.10) never panics and never returns a partial result.
func FuzzDecodeLastApplError(f *testing.F) {
	origin := mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString([]byte("op"))})
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("LD/CSWI1.Pos"), mms.NewInteger(1), origin, mms.NewUnsigned(4), mms.NewInteger(10),
	}))
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewVisibleString("LD/CSWI1.Pos"), mms.NewInteger(0), origin, mms.NewInteger(-1),
	}))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := goose.DecodeData(b)
		if err != nil {
			return
		}
		lae, err := decodeLastApplError(v)
		if (lae == nil) == (err == nil) {
			t.Fatalf("result %v with error %v", lae, err)
		}
	})
}

// FuzzDecodeMSVCB: the MSVCB decoder (IEC 61850-9-2 Table 13/14) never
// panics and its errors wrap ErrProtocol.
func FuzzDecodeMSVCB(f *testing.F) {
	dst := mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{1, 0x0c, 0xcd, 4, 0, 1}), mms.NewUnsigned(4), mms.NewUnsigned(0), mms.NewUnsigned(0x4000)})
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewBoolean(true), mms.NewVisibleString("sv"), mms.NewVisibleString("LD/LLN0$PhsMeas1"), mms.NewUnsigned(1), mms.NewUnsigned(4000),
		mms.NewBitStringWithLength([]byte{0x40}, 5), mms.NewInteger(0), dst, mms.NewUnsigned(1),
	}))
	encodeSeed(f, mms.NewStructure([]*mms.Value{
		mms.NewBoolean(false), mms.NewVisibleString("sv"), mms.NewVisibleString(""), mms.NewUnsigned(1 << 33), mms.NewUnsigned(70000),
		mms.NewBitStringWithLength([]byte{0}, 5), dst,
	}))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := goose.DecodeData(b)
		if err != nil {
			return
		}
		cb, err := decodeMSVCB("LD/LLN0$MS$MSVCB01", v)
		if err != nil {
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error does not wrap ErrProtocol: %v", err)
			}
			return
		}
		if cb == nil {
			t.Fatal("nil MSVCB without error")
		}
	})
}

// FuzzServerItemIDs: the parsers that route a client's write by its MMS
// item ID (control, RCB, SGCB, LCB subfields and the LN$FC$path split)
// never panic and only accept an item ID they can rebuild.
func FuzzServerItemIDs(f *testing.F) {
	for _, s := range []string{
		"CSWI1$CO$Pos$Oper", "CSWI1$CO$Pos$Oper$ctlVal", "GGIO1$CO$SPCSO1$SBOw", "$CO$x$Cancel",
		"LLN0$BR$brcb01$EntryID", "LLN0$RP$urcb$RptEna", "LLN0$BR$", "LLN0$SP$SGCB$ActSG",
		"LLN0$SP$SGCB$", "LLN0$LG$log1$LogEna", "PTOC1$SE$StrVal$setMag$f", "$$", "a$b$", "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, itemID string) {
		if ln, path, sub, ok := parseCOStoreKey(itemID); ok {
			if len(path) < 2 || path[len(path)-1] != sub || ln+"$CO$"+strings.Join(path, "$") != itemID {
				t.Fatalf("CO %q -> %q %q %q", itemID, ln, path, sub)
			}
			if !slices.Contains([]string{"Oper", "SBO", "SBOw", "Cancel"}, sub) {
				t.Fatalf("CO %q: service %q", itemID, sub)
			}
		}
		if rcb, sub, ok := parseRCBStoreKey(itemID); ok {
			if rcb+"$"+sub != itemID || !strings.Contains(rcb, "$RP$") && !strings.Contains(rcb, "$BR$") {
				t.Fatalf("RCB %q -> %q %q", itemID, rcb, sub)
			}
		}
		if sub, ok := parseSGCBStoreKey(itemID); ok && !strings.HasSuffix(itemID, "$SP$SGCB$"+sub) {
			t.Fatalf("SGCB %q -> %q", itemID, sub)
		}
		if pv, ok := mapping.ParseItemID(itemID); ok {
			parts := append([]string{pv.LN, pv.FC}, pv.Path...)
			if strings.Join(parts, "$") != itemID || slices.Contains(parts, "") {
				t.Fatalf("item %q -> %+v", itemID, pv)
			}
		}
		_, _, _ = servermodel.ParseLCBItem(itemID)
	})
}

// fuzzMMSValue builds an MMS value from two fuzz bytes: a kind and a
// number, covering the types a setting group write can carry and the
// ones it must refuse.
func fuzzMMSValue(kind, n byte) *mms.Value {
	switch kind % 9 {
	case 0:
		return mms.NewUnsigned(uint64(n % 6))
	case 1:
		return mms.NewUnsigned(uint64(n) + 250)
	case 2:
		return mms.NewInteger(int64(int8(n)))
	case 3:
		return mms.NewBoolean(n%2 == 1)
	case 4:
		return mms.NewFloat(float64(n) - 100)
	case 5:
		return mms.NewVisibleString(string(rune('a' + n%26)))
	case 6:
		return mms.NewStructure([]*mms.Value{mms.NewUnsigned(uint64(n))})
	case 7:
		return mms.NewUTCTime(time.Unix(int64(n), 0))
	default:
		return nil
	}
}

// FuzzSettingGroupWrites: an arbitrary sequence of SGCB writes (ActSG,
// EditSG, CnfEdit and the read-only members), bank seeds and active group
// changes (IEC 61850-7-2 §16) never panics and keeps the SGCB consistent:
// ActSG in 1..NumOfSG, EditSG in 0..NumOfSG, and FC=SG holding the active
// group's bank.
func FuzzSettingGroupWrites(f *testing.F) {
	doc, err := scl.ParseFile("testdata/setting-groups.cid")
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte{0, 0, 2})                            // ActSG 2
	f.Add([]byte{1, 0, 3, 4, 0, 1, 2, 3, 1})          // EditSG 3, seed, CnfEdit
	f.Add([]byte{0, 1, 0, 1, 0, 0, 3, 7, 7, 5, 4, 9}) // out-of-range groups, read-only members
	f.Add([]byte{6, 0, 3, 6, 1, 1, 4, 2, 2, 0, 0, 0})
	subfields := []string{"ActSG", "EditSG", "CnfEdit", "NumOfSG", "LActTm", "ResvTms", "Unknown"}
	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 3*64 {
			ops = ops[:3*64]
		}
		model, err := NewServerModelFromSCL(doc, "SGIED", "")
		if err != nil {
			t.Fatal(err)
		}
		srv, err := NewServer(model, ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer srv.Close()
		srv.EnableSettingGroups(SettingGroupHandler{})
		e := srv.SettingGroupEngine()
		ctx := context.Background()
		num, _ := srv.ValueStore().Get(sgFixtureLD + "/LLN0$SP$SGCB$NumOfSG").Uint64()
		if num != 3 {
			t.Fatalf("NumOfSG %d", num)
		}
		for ; len(ops) >= 3; ops = ops[3:] {
			op, kind, n := ops[0], ops[1], ops[2]
			switch op % 6 {
			case 0, 1, 2:
				_ = e.HandleSGCBWrite(ctx, sgFixtureLD, subfields[int(op/6)%len(subfields)], fuzzMMSValue(kind, n))
			case 3:
				key := []string{keyStrSG, keyStrSE, sgFixtureLD + "/PTOC1$SG$Nope"}[int(kind)%3]
				_ = e.SetSettingGroupValue(ctx, key, n%5, fuzzMMSValue(4, n))
			case 4:
				_ = srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, n%5)
			default:
				_ = e.HandleSGCBWrite(ctx, "NoSuchLD", "ActSG", fuzzMMSValue(kind, n))
			}
			act, edit := e.GetActiveSettingGroup(sgFixtureLD), e.GetEditSettingGroup(sgFixtureLD)
			if act < 1 || uint64(act) > num || uint64(edit) > num {
				t.Fatalf("ActSG %d, EditSG %d of %d groups", act, edit, num)
			}
			bank, err := e.SettingGroupValue(keyStrSG, act)
			if err != nil {
				t.Fatal(err)
			}
			if live := srv.ValueStore().Get(keyStrSG); !live.Equal(bank) {
				t.Fatalf("FC=SG %v, active group %d bank %v", live, act, bank)
			}
		}
	})
}

// FuzzMemoryReportBuffer: any sequence of Append, Remove, Purge and
// SetLimits on the default BRCB buffer behaves as a bounded FIFO
// (IEC 61850-7-2 §17.2.3.2.2.8): at most maxEntries entries, at most
// maxBytes octets unless one entry alone exceeds them, oldest discarded
// first, Bytes the sum of the entries' sizes, After and Get as on a plain
// list.
func FuzzMemoryReportBuffer(f *testing.F) {
	f.Add(uint8(3), uint8(0), []byte{0, 1, 0, 2, 0, 3, 0, 4, 1, 2, 2, 0})
	f.Add(uint8(10), uint8(40), []byte{0, 30, 0, 30, 0, 200, 3, 2, 0, 5, 0, 5, 0, 5})
	f.Add(uint8(0), uint8(1), []byte{0, 0, 0, 0, 4, 0})
	f.Fuzz(func(t *testing.T, maxE, maxB uint8, ops []byte) {
		b := NewMemoryReportBuffer(int(maxE), int(maxB))
		var model []ReportEntry
		var next uint64
		limE, limB := b.Limits()
		for ; len(ops) >= 2; ops = ops[2:] {
			op, n := ops[0], ops[1]
			switch op % 5 {
			case 0, 1:
				next++
				e := ReportEntry{ID: next, Values: []*mms.Value{mms.NewOctetString(make([]byte, n))}, Inclusion: []bool{true}, Reasons: []ReasonCode{ReasonDataChanged}}
				discarded := b.Append(e)
				if len(discarded) > len(model) || !slices.EqualFunc(discarded, model[:len(discarded)], sameEntry) {
					t.Fatalf("discarded %v, oldest %v", ids(discarded), ids(model))
				}
				model = append(model[len(discarded):], e)
			case 2:
				id := uint64(n)
				b.Remove(id)
				model = slices.DeleteFunc(model, func(e ReportEntry) bool { return e.ID == id })
			case 3:
				b.SetLimits(int(n%8), int(n))
				limE, limB = b.Limits()
				continue // takes effect with the next Append
			default:
				b.Purge()
				model = nil
			}
			got := b.Entries()
			if !slices.EqualFunc(got, model, sameEntry) {
				t.Fatalf("entries %v, want %v", ids(got), ids(model))
			}
			if op%5 < 2 {
				if b.Len() > limE {
					t.Fatalf("%d entries, limit %d", b.Len(), limE)
				}
				if limB > 0 && b.Len() > 1 && b.Bytes() > limB {
					t.Fatalf("%d octets in %d entries, limit %d", b.Bytes(), b.Len(), limB)
				}
			}
			sum := 0
			for _, e := range got {
				sum += e.EncodedSize()
			}
			if sum != b.Bytes() {
				t.Fatalf("Bytes %d, entries hold %d", b.Bytes(), sum)
			}
			for _, id := range []uint64{0, uint64(n), next, next + 1, math.MaxUint64} {
				e, ok := b.After(id)
				i := slices.IndexFunc(model, func(e ReportEntry) bool { return e.ID > id })
				if ok != (i >= 0) || ok && e.ID != model[i].ID {
					t.Fatalf("After(%d) = %d %v", id, e.ID, ok)
				}
				e, ok = b.Get(id)
				j := slices.IndexFunc(model, func(e ReportEntry) bool { return e.ID == id })
				if ok != (j >= 0) || ok && e.ID != id {
					t.Fatalf("Get(%d) = %d %v", id, e.ID, ok)
				}
			}
			if o, ok := b.Oldest(); ok != (len(model) > 0) || ok && o.ID != model[0].ID {
				t.Fatalf("Oldest %d %v", o.ID, ok)
			}
			if o, ok := b.Newest(); ok != (len(model) > 0) || ok && o.ID != model[len(model)-1].ID {
				t.Fatalf("Newest %d %v", o.ID, ok)
			}
		}
	})
}

func sameEntry(a, b ReportEntry) bool { return a.ID == b.ID }
