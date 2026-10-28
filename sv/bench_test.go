// SPDX-License-Identifier: MIT

package sv

import (
	"fmt"
	"testing"
	"time"
)

// discard is a transport that drops every frame.
type discard struct{ Transport }

func (discard) Send([]byte) error { return nil }
func (discard) HardwareAddr() MAC { return testSrc }

func benchFrame(b testing.TB, noASDU int) []byte {
	f := &Frame{Dst: testDst, Src: testSrc, AppID: 0x4000}
	for i := 0; i < noASDU; i++ {
		f.PDU.ASDUs = append(f.PDU.ASDUs, ASDU{SvID: "MU01", DatSet: "IED1MU/LLN0$PhsMeas1", SmpCnt: uint16(i), ConfRev: 1, SmpSynch: 1, Sample: leSample(int32(i))})
	}
	raw, err := Encode(f)
	if err != nil {
		b.Fatal(err)
	}
	return raw
}

func BenchmarkDecodeInto(b *testing.B) {
	for _, n := range []int{1, 8} {
		b.Run(fmt.Sprintf("noASDU=%d", n), func(b *testing.B) {
			raw := benchFrame(b, n)
			var f Frame
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				if err := DecodeInto(&f, raw, DefaultLimits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAppendFrame(b *testing.B) {
	f, _ := Decode(benchFrame(b, 1))
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f.PDU.ASDUs[0].SmpCnt = uint16(i)
		buf, _ = AppendFrame(buf[:0], f)
	}
}

// BenchmarkPublisherFrame measures one paced frame: sample copy,
// encode and send, without the timer.
func BenchmarkPublisherFrame(b *testing.B) {
	p, err := NewPublisher(discard{}, pubConfig())
	if err != nil {
		b.Fatal(err)
	}
	t0 := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := p.sendFrame(t0, uint64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSubscriber measures the receive path for several 4 kHz
// streams interleaved, as on a process bus. One op is one frame.
func BenchmarkSubscriber(b *testing.B) {
	for _, streams := range []int{1, 8, 32} {
		b.Run(fmt.Sprintf("streams=%d", streams), func(b *testing.B) {
			frames := make([][][]byte, streams)
			for s := range frames {
				for c := 0; c < 4000; c++ {
					f := &Frame{Dst: testDst, Src: MAC{2, 0, 0, 0, byte(s >> 8), byte(s)}, AppID: 0x4000 + uint16(s)}
					f.PDU.ASDUs = []ASDU{{SvID: fmt.Sprintf("MU%02d", s), SmpCnt: uint16(c), ConfRev: 1, SmpSynch: 1, Sample: leSample(int32(c))}}
					raw, _ := Encode(f)
					frames[s] = append(frames[s], raw)
				}
			}
			s, _ := NewSubscriber(nil, SubscriberConfig{})
			var sum int32
			_, _ = s.Subscribe(SubscriptionConfig{Layout: LELayout, SmpCntWrap: 4000, OnASDU: func(smp *Sample) {
				sum += Int32At(smp.ASDU.Sample, 0)
			}})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c := i / streams
				s.HandleFrame(frames[i%streams][c%4000])
			}
			b.StopTimer()
			// 4 kHz per stream: report the share of one core used at line rate.
			perFrame := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(perFrame*4000*float64(streams)/1e9*100, "%core@4kHz")
		})
	}
}

// TestSubscriberZeroAlloc pins the per-frame allocation budget of the
// steady-state receive path to zero.
func TestSubscriberZeroAlloc(t *testing.T) {
	frames := make([][]byte, 100)
	for i := range frames {
		f := &Frame{Dst: testDst, Src: testSrc, AppID: 0x4000}
		// Two interleaved streams: the decoder must reuse interned svIDs.
		f.PDU.ASDUs = []ASDU{{SvID: fmt.Sprintf("MU%d", i%2), DatSet: "ds", SmpCnt: uint16(i / 2), ConfRev: 1, SmpSynch: 1, Sample: leSample(int32(i))}}
		frames[i], _ = Encode(f)
	}
	s, _ := NewSubscriber(nil, SubscriberConfig{})
	_, _ = s.Subscribe(SubscriptionConfig{Layout: LELayout, SmpCntWrap: 50, OnASDU: func(*Sample) {}})
	i := 1
	s.HandleFrame(frames[0])
	s.HandleFrame(frames[1])
	allocs := testing.AllocsPerRun(1000, func() {
		i++
		s.HandleFrame(frames[i%100])
	})
	if allocs != 0 {
		t.Errorf("HandleFrame allocates %.1f per frame", allocs)
	}
	p, _ := NewPublisher(discard{}, pubConfig())
	t0 := time.Now()
	k := uint64(0)
	if a := testing.AllocsPerRun(1000, func() { k++; _ = p.sendFrame(t0, k) }); a != 0 {
		t.Errorf("publisher allocates %.1f per frame", a)
	}
}
