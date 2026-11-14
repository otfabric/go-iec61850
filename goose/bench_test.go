// SPDX-License-Identifier: MIT

package goose

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// benchFrame is an encoded GOOSE frame of stream s with members
// boolean, bit string (quality) and UTC time per data point.
func benchFrame(b testing.TB, s, points int, stNum, sqNum uint32) []byte {
	b.Helper()
	values := make([]*mms.Value, 0, 3*points)
	for range points {
		values = append(values, mms.NewBoolean(true), mms.NewBitStringWithLength([]byte{0, 0}, 13), mms.NewUTCTime(time.Unix(1700000000, 0)))
	}
	f := &Frame{
		Dst: testDst, Src: MAC{2, 0, 0, 0, byte(s >> 8), byte(s)}, AppID: 0x1000 + uint16(s),
		PDU: PDU{
			GocbRef: fmt.Sprintf("IED%02dLD0/LLN0$GO$gcb1", s), DatSet: fmt.Sprintf("IED%02dLD0/LLN0$ds1", s), GoID: "gcb1",
			TimeAllowedToLive: 2000, T: time.Unix(1700000000, 0), StNum: stNum, SqNum: sqNum, ConfRev: 1,
			NumDatSetEntries: uint32(len(values)), AllData: values,
		},
	}
	raw, err := Encode(f)
	if err != nil {
		b.Fatal(err)
	}
	return raw
}

func BenchmarkDecode(b *testing.B) {
	for _, points := range []int{1, 16} {
		b.Run(fmt.Sprintf("points=%d", points), func(b *testing.B) {
			raw := benchFrame(b, 0, points, 1, 0)
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				f, err := Decode(raw)
				if err != nil {
					b.Fatal(err)
				}
				if len(f.PDU.AllData) != 3*points {
					b.Fatalf("%d members, want %d", len(f.PDU.AllData), 3*points)
				}
			}
		})
	}
}

// sqNumBase is the first sqNum of the subscriber benchmark: every value
// from it to 0x7fffffff encodes as a 4-octet INTEGER, so the next sqNum
// is written into the encoded frame in place.
const sqNumBase = 0x01000000

// sqNumOffset returns the offset of the 4 value octets of sqNum in raw.
func sqNumOffset(b testing.TB, raw []byte) int {
	b.Helper()
	tlv := []byte{tagSqNum, 4, 0x01, 0, 0, 0}
	i := bytes.Index(raw, tlv)
	if i < 0 || bytes.Contains(raw[i+1:], tlv) {
		b.Fatal("sqNum not found once in the frame")
	}
	return i + 2
}

// BenchmarkSubscriber measures the receive path (decode, filter,
// supervision) for several streams interleaved, each sending the
// retransmissions of one state, as between state changes. One op is one
// frame.
func BenchmarkSubscriber(b *testing.B) {
	for _, streams := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("streams=%d", streams), func(b *testing.B) {
			frames := make([][]byte, streams)
			offs := make([]int, streams)
			for s := range frames {
				frames[s] = benchFrame(b, s, 4, 1, sqNumBase)
				offs[s] = sqNumOffset(b, frames[s])
			}
			s, err := NewSubscriber(NewHub().NewPort(MAC{}), SubscriberConfig{})
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = s.Close() }()
			sub, err := s.Subscribe(SubscriptionConfig{OnlyStateChanges: true})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				k := i % streams
				binary.BigEndian.PutUint32(frames[k][offs[k]:], sqNumBase+uint32(i/streams))
				s.HandleFrame(frames[k])
			}
			b.StopTimer()
			var messages, other uint64
			for _, sup := range sub.Supervision() {
				messages += sup.Messages
				other += sup.Duplicates + sup.OutOfOrder + sup.SqNumGaps + sup.Invalid + sup.DroppedEvents
			}
			if messages != uint64(b.N) || other != 0 {
				b.Fatalf("%d messages and %d others for %d frames", messages, other, b.N)
			}
			// At 1 000 frames per second per stream (a state change
			// burst at a MinTime of 1 ms): the share of one core.
			perFrame := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(perFrame*1000*float64(streams)/1e9*100, "%core@1kHz")
		})
	}
}
