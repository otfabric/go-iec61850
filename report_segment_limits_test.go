// SPDX-License-Identifier: MIT

package iec61850

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-mms"
)

// segmentOf returns segment sub of report seq of a data set of width
// members; the segment carries member sub with a visible string of
// payload octets.
func segmentOf(seq, sub uint32, more bool, width, payload int) *ReportIndication {
	inc := make([]bool, width)
	inc[int(sub)%width] = true
	return &ReportIndication{
		RptID: "rseg", OptFlds: OptFldSeqNum | OptFldSegmentation, SeqNum: seq, SubSeqNum: sub, MoreSegments: more,
		Inclusion: inc,
		Values:    []*Value{NewValue(mms.NewVisibleString(strings.Repeat("v", payload)))},
	}
}

// buffered returns the segments a subscription buffers, under its lock.
func buffered(s *ReportSubscription) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.segBuf)
}

func segmentSubscription(maxBytes int, timeout time.Duration) *ReportSubscription {
	return &ReportSubscription{client: &Client{logger: discardLogger()}, segMaxBytes: maxBytes, segTimeout: timeout}
}

// The segments of one partial report are buffered up to MaxSegmentBytes:
// a report whose segments fit is assembled; one more byte drops the
// partial report, releases its segments and counts it. The next report
// is assembled again.
func TestReportSegments_ByteBudget(t *testing.T) {
	const width, payload = 16, 100
	size := segmentSize(segmentOf(1, 0, true, width, payload))
	cases := []struct {
		name     string
		segments int
		budget   int
		dropped  bool
	}{
		{"1 segment, budget size-1", 1, size - 1, true},
		{"2 segments, budget 2*size-1", 2, 2*size - 1, true},
		{"2 segments, budget 2*size", 2, 2 * size, false},
		{"8 segments, budget 8*size", 8, 8 * size, false},
		{"8 segments, budget 8*size-1", 8, 8*size - 1, true},
		{"8 segments, budget 7*size", 8, 7 * size, true},
		{"8 segments, budget 8*size+1", 8, 8*size + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := segmentSubscription(tc.budget, 0)
			var assembled *ReportIndication
			for i := range tc.segments {
				// A one-segment report has MoreSegments on its only
				// segment here, and is completed by an empty last one.
				more := i < tc.segments-1 || tc.segments == 1
				if got := s.handleSegment(segmentOf(7, uint32(i), more, width, payload)); got != nil {
					assembled = got
				}
			}
			if tc.segments == 1 && !tc.dropped {
				t.Fatal("bad case")
			}
			if got := s.Stats().PartialReportsDropped; tc.dropped != (got == 1) || got > 1 {
				t.Fatalf("PartialReportsDropped = %d, want dropped %v", got, tc.dropped)
			}
			if tc.dropped {
				if assembled != nil {
					t.Error("a dropped partial report was delivered")
				}
				if s.segBuf != nil || s.segActive || s.segBytes != 0 || s.segTimer != nil {
					t.Errorf("after the drop: %d segments, %d bytes buffered, active %v, timer %v; want released",
						len(s.segBuf), s.segBytes, s.segActive, s.segTimer != nil)
				}
			} else if assembled == nil || len(assembled.Values) != tc.segments {
				t.Fatalf("assembled %+v, want %d members", assembled, tc.segments)
			}
			// The next report is assembled within the budget.
			if got := s.handleSegment(segmentOf(8, 0, false, width, payload)); got == nil {
				t.Error("an unsegmented report after the drop was not delivered")
			}
		})
	}
}

// With MaxSegmentBytes zero the default applies, and the estimate counts
// the inclusion bit string of every segment and its values.
func TestReportSegments_DefaultsAndEstimate(t *testing.T) {
	s := segmentSubscription(0, 0)
	if b, d := s.segmentLimits(); b != DefaultMaxSegmentBytes || d != DefaultSegmentTimeout {
		t.Errorf("segmentLimits() = %d, %v; want the defaults", b, d)
	}
	s = segmentSubscription(-1, -1)
	if b, d := s.segmentLimits(); b != DefaultMaxSegmentBytes || d != DefaultSegmentTimeout {
		t.Errorf("negative limits: segmentLimits() = %d, %v; want the defaults", b, d)
	}
	narrow := segmentSize(segmentOf(1, 0, true, 1, 0))
	wide := segmentSize(segmentOf(1, 0, true, 64001, 0))
	if wide-narrow != 64000 {
		t.Errorf("estimate grows by %d for 64000 more inclusion bits, want 64000", wide-narrow)
	}
	nested := &ReportIndication{Values: []*Value{NewValue(mms.NewStructure([]*mms.Value{
		mms.NewOctetString(make([]byte, 1000)),
		mms.NewArray([]*mms.Value{mms.NewBitStringWithLength(make([]byte, 125), 1000), mms.NewMmsString("äää")}),
		mms.NewBoolean(true),
	}))}}
	if got := segmentSize(nested); got < segmentOverhead+1000+125+6 {
		t.Errorf("estimate of a nested value %d does not hold its content", got)
	}

	// A partial report whose segments stay below the default budget.
	for i := range uint32(4) {
		s.handleSegment(segmentOf(3, i, true, 8, 1000))
	}
	if len(s.segBuf) != 4 || s.Stats().PartialReportsDropped != 0 {
		t.Fatalf("%d segments buffered, %d dropped; want 4, 0", len(s.segBuf), s.Stats().PartialReportsDropped)
	}
	if got := s.handleSegment(segmentOf(3, 4, false, 8, 1000)); got == nil || len(got.Values) != 5 {
		t.Fatalf("assembled %+v, want 5 members", got)
	}
}

// A partial report still incomplete SegmentTimeout after its first segment
// is dropped, its segments released and counted, without a further
// segment. A report completed before the limit is assembled. The times
// are the fake time of the synctest bubble.
func TestReportSegments_ReassemblyTimeLimit(t *testing.T) {
	for _, limit := range []time.Duration{0, time.Second} {
		want := limit
		if want == 0 {
			want = DefaultSegmentTimeout
		}
		t.Run(want.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := segmentSubscription(0, limit)

				// Completed just before the limit.
				s.handleSegment(segmentOf(1, 0, true, 4, 10))
				time.Sleep(want - time.Millisecond) // fake time
				synctest.Wait()
				if got := s.handleSegment(segmentOf(1, 1, false, 4, 10)); got == nil || len(got.Values) != 2 {
					t.Fatalf("report completed before the limit: assembled %+v, want 2 members", got)
				}
				time.Sleep(time.Hour) // fake time: the timer of that report does nothing
				synctest.Wait()
				if got := s.Stats().PartialReportsDropped; got != 0 {
					t.Fatalf("PartialReportsDropped = %d after a completed report, want 0", got)
				}

				// Incomplete at the limit.
				s.handleSegment(segmentOf(2, 0, true, 4, 10))
				s.handleSegment(segmentOf(2, 1, true, 4, 10))
				time.Sleep(want - time.Millisecond) // fake time
				synctest.Wait()
				if n := buffered(s); n != 2 || s.Stats().PartialReportsDropped != 0 {
					t.Fatalf("before the limit: %d segments buffered, %d dropped; want 2, 0", n, s.Stats().PartialReportsDropped)
				}
				time.Sleep(time.Millisecond) // fake time: the limit
				synctest.Wait()
				s.mu.Lock()
				buffered, active, bytes := len(s.segBuf), s.segActive, s.segBytes
				s.mu.Unlock()
				if buffered != 0 || active || bytes != 0 {
					t.Errorf("at the limit: %d segments, %d bytes buffered, active %v; want released", buffered, bytes, active)
				}
				if got := s.Stats().PartialReportsDropped; got != 1 {
					t.Errorf("PartialReportsDropped = %d at the limit, want 1", got)
				}
			})
		})
	}
}

// A partial report interrupted by another report, or by more than 65536
// segments, is counted as dropped; Close releases the buffered segments
// and stops the time limit.
func TestReportSegments_OtherDropsAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sub := localSubscription(t, "rseg", 4, OverflowDropNewest)
		sub.handleSegment(segmentOf(1, 0, true, 4, 10))
		sub.handleSegment(segmentOf(2, 0, true, 4, 10)) // a new report starts
		if got := sub.Stats().PartialReportsDropped; got != 1 {
			t.Errorf("PartialReportsDropped = %d after a new report started, want 1", got)
		}
		sub.handleSegment(segmentOf(3, 1, true, 4, 10)) // a segment of another report
		if got := sub.Stats().PartialReportsDropped; got != 2 {
			t.Errorf("PartialReportsDropped = %d after a segment of another report, want 2", got)
		}
		if err := sub.Close(); err != nil {
			t.Fatal(err)
		}
		if sub.segBuf != nil || sub.segTimer != nil {
			t.Error("Close kept the buffered segments or their timer")
		}
		if got := sub.handleSegment(segmentOf(4, 0, false, 4, 10)); got != nil {
			t.Error("a segment after Close was handled")
		}
		time.Sleep(2 * DefaultSegmentTimeout) // fake time: no timer runs after Close
		synctest.Wait()
		if got := sub.Stats().PartialReportsDropped; got != 2 {
			t.Errorf("PartialReportsDropped = %d after Close, want 2", got)
		}
	})
}

// ReportsDropped counts what the overflow policies drop: the incoming
// report with OverflowDropNewest and OverflowCallback, the oldest queued
// one with OverflowDropOldest. With a queue of one, three reports leave
// one queued and two dropped. CallbacksRecovered counts the OnOverflow
// calls that do not return normally.
func TestReportSubscription_StatsCountDrops(t *testing.T) {
	cases := []struct {
		name     string
		policy   OverflowPolicy
		callback bool
		queued   uint32
	}{
		{"OverflowDropNewest", OverflowDropNewest, false, 1},
		{"OverflowDropOldest", OverflowDropOldest, false, 3},
		{"OverflowCallback without callback", OverflowCallback, false, 1},
		{"OverflowCallback", OverflowCallback, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := localSubscription(t, "rdrop", 1, tc.policy)
			var overflowed []uint32
			if tc.callback {
				sub.onOverflow = func(ri *ReportIndication) { overflowed = append(overflowed, ri.SeqNum) }
			}
			for seq := uint32(1); seq <= 3; seq++ {
				sub.deliver(&ReportIndication{RptID: "rdrop", SeqNum: seq}, discardLogger())
			}
			st := sub.Stats()
			if st.ReportsDropped != 2 || st.PartialReportsDropped != 0 || st.CallbacksRecovered != 0 {
				t.Errorf("Stats() = %+v, want 2 reports dropped", st)
			}
			if ri := <-sub.Reports(); ri.SeqNum != tc.queued {
				t.Errorf("queued report %d, want %d", ri.SeqNum, tc.queued)
			}
			if tc.callback && (len(overflowed) != 2 || overflowed[0] != 2 || overflowed[1] != 3) {
				t.Errorf("OnOverflow got %v, want [2 3]", overflowed)
			}
		})
	}
	t.Run("OverflowBlock", func(t *testing.T) {
		sub := localSubscription(t, "rdrop", 1, OverflowBlock)
		sub.deliver(&ReportIndication{RptID: "rdrop", SeqNum: 1}, discardLogger())
		<-sub.Reports()
		sub.deliver(&ReportIndication{RptID: "rdrop", SeqNum: 2}, discardLogger())
		if st := sub.Stats(); st != (ReportSubscriptionStats{}) {
			t.Errorf("Stats() = %+v, want none dropped", st)
		}
	})
	t.Run("callback does not return normally", func(t *testing.T) {
		sub := localSubscription(t, "rdrop", 1, OverflowCallback)
		sub.onOverflow = func(*ReportIndication) { panic("callback") }
		for seq := uint32(1); seq <= 3; seq++ {
			sub.deliver(&ReportIndication{RptID: "rdrop", SeqNum: seq}, discardLogger())
		}
		if st := sub.Stats(); st.ReportsDropped != 2 || st.CallbacksRecovered != 2 {
			t.Errorf("Stats() = %+v, want 2 dropped and 2 recovered", st)
		}
		<-sub.Reports()
		sub.deliver(&ReportIndication{RptID: "rdrop", SeqNum: 4}, discardLogger())
		if ri := <-sub.Reports(); ri.SeqNum != 4 {
			t.Errorf("after the recovered callbacks received %d, want 4", ri.SeqNum)
		}
	})
}
