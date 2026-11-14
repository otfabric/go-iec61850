// SPDX-License-Identifier: MIT

package sv

import (
	"bytes"
	"testing"
	"time"
)

func fuzzSeedFrame(f *testing.F, noASDU int) []byte {
	f.Helper()
	fr := &Frame{Dst: testDst, Src: testSrc, VLAN: &VLAN{Priority: 4, ID: 1}, AppID: 0x4000, Simulation: true}
	fr.PDU.Security = []byte{1, 2}
	for i := range noASDU {
		fr.PDU.ASDUs = append(fr.PDU.ASDUs, ASDU{
			SvID: "MU01", DatSet: "IED1MU/LLN0$PhsMeas1", SmpCnt: uint16(i), ConfRev: 1,
			RefrTm: time.Unix(1700000000, 0), HasRefrTm: true, SmpSynch: 2, SmpRate: 80, HasSmpRate: true,
			SmpMod: SmpPerPeriod, HasSmpMod: true, HasGmIdentity: true, GmIdentity: [8]byte{1},
			Sample: leSample(int32(i)),
		})
	}
	b, err := Encode(fr)
	if err != nil {
		f.Fatal(err)
	}
	return b
}

// FuzzDecode checks that Decode never panics, that DecodeInto agrees
// with it, and that every accepted frame survives a re-encode.
func FuzzDecode(f *testing.F) {
	f.Add(fuzzSeedFrame(f, 1))
	f.Add(fuzzSeedFrame(f, 8))
	f.Add(make([]byte, 60))
	f.Add([]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 0x88, 0xba, 0x40, 0, 0, 9, 0, 0, 0, 0, 0x60})
	var reuse Frame
	f.Fuzz(func(t *testing.T, b []byte) {
		fr, err := Decode(b)
		err2 := DecodeInto(&reuse, b, DefaultLimits)
		if (err == nil) != (err2 == nil) {
			t.Fatalf("Decode %v, DecodeInto %v", err, err2)
		}
		if err != nil {
			return
		}
		p0, _ := EncodePDU(&reuse.PDU)
		enc, err := Encode(fr)
		if err != nil {
			return // e.g. a frame that only fits with padding removed
		}
		fr2, err := Decode(enc)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		p1, _ := EncodePDU(&fr.PDU)
		p2, _ := EncodePDU(&fr2.PDU)
		if !bytes.Equal(p1, p2) || !bytes.Equal(p0, p1) {
			t.Fatal("PDU not stable across round trip")
		}
	})
}

// FuzzSubscriber feeds arbitrary frames to a subscriber whose stream
// state carries over from input to input, and each input twice to a
// fresh subscriber. On the fresh one, a frame the decoder refuses creates
// no stream; an accepted frame counts each of its ASDUs once per feed, in
// at most as many streams as it has ASDUs, each stream counted as one
// frame on the first feed, with no more invalid, duplicate or
// out-of-order ASDUs than ASDUs.
func FuzzSubscriber(f *testing.F) {
	f.Add(fuzzSeedFrame(f, 1))
	f.Add(fuzzSeedFrame(f, 4))
	s, _ := NewSubscriber(nil, SubscriberConfig{AcceptSimulation: true})
	sub, _ := s.Subscribe(SubscriptionConfig{Layout: LELayout, OnASDU: func(smp *Sample) {
		var le LESample
		_ = DecodeLE(smp.ASDU.Sample, &le)
	}})
	f.Fuzz(func(t *testing.T, b []byte) {
		s.HandleFrame(b)
		s.CheckTimeouts()
		drainEvents(sub)

		fs, err := NewSubscriber(nil, SubscriberConfig{AcceptSimulation: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = fs.Close() }()
		fsub, err := fs.Subscribe(SubscriptionConfig{Layout: LELayout})
		if err != nil {
			t.Fatal(err)
		}
		var fr Frame
		decErr := DecodeInto(&fr, b, DefaultLimits)
		fs.HandleFrame(b)
		first := fsub.Stats()
		if decErr != nil {
			if len(first) != 0 {
				t.Fatalf("frame refused by the decoder (%v) created %d streams", decErr, len(first))
			}
			return
		}
		n := uint64(len(fr.PDU.ASDUs))
		var total uint64
		for _, st := range first {
			total += st.ASDUs
			if st.Frames != 1 {
				t.Fatalf("stream %v: Frames = %d after one frame", st.Stream, st.Frames)
			}
			if st.Invalid+st.Duplicates+st.OutOfOrder > st.ASDUs {
				t.Fatalf("stream %v: %d invalid, %d duplicate, %d out of order of %d ASDUs",
					st.Stream, st.Invalid, st.Duplicates, st.OutOfOrder, st.ASDUs)
			}
		}
		if total != n || uint64(len(first)) > n {
			t.Fatalf("%d ASDUs in %d streams counted for a frame of %d ASDUs", total, len(first), n)
		}
		fs.HandleFrame(b)
		second := fsub.Stats()
		if len(second) != len(first) {
			t.Fatalf("the same frame again: %d streams, then %d", len(first), len(second))
		}
		for i := range second {
			if second[i].Stream != first[i].Stream || second[i].ASDUs != 2*first[i].ASDUs {
				t.Fatalf("stream %v: %d ASDUs after the frame twice, %d after once",
					second[i].Stream, second[i].ASDUs, first[i].ASDUs)
			}
		}
		drainEvents(fsub)
	})
}

// FuzzLayoutDecode decodes arbitrary sample octets against an arbitrary
// layout (one Kind per octet of kinds). Layout.Decode, DecodeLE and the
// zero-allocation accessors must never panic, and every accepted
// sample must re-encode, after one normalising pass (BOOLEAN non-zero
// octets, NaN payloads), to a stable encoding of the same size.
func FuzzLayoutDecode(f *testing.F) {
	all := make([]byte, 0, int(kindCount))
	for k := KindBoolean; k < kindCount; k++ {
		all = append(all, byte(k))
	}
	f.Add(all, make([]byte, Layout(kindsOf(all)).Size()))
	f.Add([]byte{byte(KindInt8), byte(KindInt64), byte(KindTimestamp)},
		[]byte{0x80, 0x80, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x0a})
	f.Add([]byte{byte(KindDbpos), byte(KindBoolean)}, []byte{0, 0, 0, 4, 2})
	f.Add([]byte{byte(KindInt32), byte(KindQuality)}, leSample(-1)[:8])
	f.Fuzz(func(t *testing.T, kinds, sample []byte) {
		if len(kinds) > 64 {
			return
		}
		l := Layout(kindsOf(kinds))
		if len(sample) == LESampleSize {
			var le LESample
			if err := DecodeLE(sample, &le); err != nil || !bytes.Equal(le.Append(nil), sample) {
				t.Fatalf("9-2LE round trip: %v", err)
			}
		}
		if l.Validate() != nil {
			return
		}
		for _, off := range l.Offsets() {
			if off+8 <= len(sample) {
				_ = Int32At(sample, off)
				_ = QualityAt(sample, off)
				_ = Float32At(sample, off)
				_, _ = TimestampAt(sample, off)
			}
		}
		vals, err := l.Decode(sample)
		if err != nil {
			return
		}
		enc, err := l.Encode(nil, vals)
		if err != nil {
			t.Fatalf("re-encode %v: %v", l, err)
		}
		if len(enc) != len(sample) {
			t.Fatalf("size %d, want %d", len(enc), len(sample))
		}
		vals2, err := l.Decode(enc)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		enc2, err := l.Encode(nil, vals2)
		if err != nil || !bytes.Equal(enc, enc2) {
			t.Fatalf("unstable: % x / % x %v", enc, enc2, err)
		}
	})
}

func kindsOf(b []byte) []Kind {
	ks := make([]Kind, len(b))
	for i, x := range b {
		ks[i] = Kind(x)
	}
	return ks
}
