// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// operWithCtlNum builds an Oper (or Cancel, without Test/Check) structure
// carrying ctlNum as an MMS unsigned value.
func operWithCtlNum(ctlNum uint64, cancel bool) *mms.Value {
	m := []*mms.Value{
		mms.NewBoolean(true),
		mms.NewUTCTime(zeroTime),
		mms.NewStructure([]*mms.Value{mms.NewInteger(int64(OrCatRemoteControl)), mms.NewOctetString(nil)}),
		mms.NewUnsigned(ctlNum),
		mms.NewUTCTime(zeroTime),
	}
	if !cancel {
		m = append(m, mms.NewBoolean(false), mms.NewBitStringWithLength([]byte{0}, 2))
	}
	return mms.NewStructure(m)
}

// TestDecodeCtlNumBounds: ctlNum is INT8U. Before the fix, 256 decoded
// as 0 and 257 as 1, so an out-of-range Oper could match the ctlNum of
// an unrelated Select.
func TestDecodeCtlNumBounds(t *testing.T) {
	for _, op := range []string{"Oper", "SBOw", "Cancel"} {
		cancel := op == "Cancel"
		for _, n := range []uint64{0, 1, 254, 255} {
			req, err := decodeControlRequest("LD/LN.DO", op, operWithCtlNum(n, cancel))
			if err != nil || uint64(req.CtlNum) != n {
				t.Errorf("%s ctlNum %d: got %d, %v", op, n, req.CtlNum, err)
			}
		}
		for _, n := range []uint64{256, 257, math.MaxUint32} {
			if _, err := decodeControlRequest("LD/LN.DO", op, operWithCtlNum(n, cancel)); err == nil || !strings.Contains(err.Error(), "ctlNum") {
				t.Errorf("%s ctlNum %d: err = %v, want out-of-range error", op, n, err)
			}
		}
	}
	// A ctlNum member of another type keeps the previous lenient decode.
	v := operWithCtlNum(0, false)
	m, _ := v.Structure()
	m[3] = mms.NewBoolean(true)
	if req, err := decodeControlRequest("LD/LN.DO", "Oper", mms.NewStructure(m)); err != nil || req.CtlNum != 0 {
		t.Errorf("non-unsigned ctlNum: %d, %v", req.CtlNum, err)
	}
}

// TestNextCtlNumWraps checks the documented 8-bit wrap of the client
// counter at its boundary.
func TestNextCtlNumWraps(t *testing.T) {
	ctlNumCounter.Store(254)
	if got := nextCtlNum(); got != 255 {
		t.Errorf("after 254: %d, want 255", got)
	}
	if got := nextCtlNum(); got != 0 {
		t.Errorf("after 255: %d, want 0", got)
	}
	ctlNumCounter.Store(math.MaxUint32)
	if got := nextCtlNum(); got != 0 {
		t.Errorf("counter wrap: %d, want 0", got)
	}
}

func sgcbValue(numOfSGs, actSG, editSG uint64, resvTms *uint64) *mms.Value {
	m := []*mms.Value{
		mms.NewUnsigned(numOfSGs), mms.NewUnsigned(actSG), mms.NewUnsigned(editSG),
		mms.NewBoolean(false), mms.NewUTCTime(time.Time{}),
	}
	if resvTms != nil {
		m = append(m, mms.NewUnsigned(*resvTms))
	}
	return mms.NewStructure(m)
}

// TestDecodeSGCBBounds: NumOfSG/ActSG/EditSG are INT8U and ResvTms
// INT16U. Before the fix a server reporting 256 setting groups decoded
// as 0 and ActSG 257 as group 1.
func TestDecodeSGCBBounds(t *testing.T) {
	maxResv := uint64(math.MaxUint16)
	info, err := decodeSGCB(sgcbValue(255, 255, 255, &maxResv))
	if err != nil {
		t.Fatal(err)
	}
	if info.NumOfSGs != 255 || info.ActSG != 255 || info.EditSG != 255 || info.ResvTms != math.MaxUint16 {
		t.Errorf("max values: %+v", info)
	}
	over := uint64(math.MaxUint16 + 1)
	for name, v := range map[string]*mms.Value{
		"NumOfSGs": sgcbValue(256, 1, 0, nil),
		"ActSG":    sgcbValue(4, 257, 0, nil),
		"EditSG":   sgcbValue(4, 1, 260, nil),
		"ResvTms":  sgcbValue(4, 1, 0, &over),
	} {
		if _, err := decodeSGCB(v); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s out of range: err = %v", name, err)
		}
	}
}

// TestSGCBWriteBounds: before the fix a client writing ActSG=257 to a
// 4-group SGCB activated group 1 (and 256 hit the "0" check only by
// accident); EditSG=258 opened group 2 for editing.
func TestSGCBWriteBounds(t *testing.T) {
	srv := newTestSGServer(t)
	srv.EnableSettingGroups(SettingGroupHandler{})
	e := srv.SettingGroupEngine()
	ctx := context.Background()

	for _, sub := range []string{"ActSG", "EditSG"} {
		for _, n := range []uint64{256, 257, 258, math.MaxUint32} {
			if err := e.HandleSGCBWrite(ctx, "LD1", sub, mms.NewUnsigned(n)); err == nil {
				t.Errorf("%s=%d accepted", sub, n)
			}
		}
	}
	if got := e.GetActiveSettingGroup("LD1"); got != 1 {
		t.Errorf("ActSG changed to %d by out-of-range writes", got)
	}
	if err := e.HandleSGCBWrite(ctx, "LD1", "ActSG", mms.NewUnsigned(4)); err != nil {
		t.Errorf("ActSG=4: %v", err)
	}
	if err := e.HandleSGCBWrite(ctx, "LD1", "EditSG", mms.NewUnsigned(2)); err != nil {
		t.Errorf("EditSG=2: %v", err)
	}
}

// TestQualityWithValidityBounds: before the fix WithValidity(-1) returned
// 0xFFFF, setting every detail-quality flag.
func TestQualityWithValidityBounds(t *testing.T) {
	q := QualityOverflow
	for _, v := range []Validity{ValidityGood, ValidityReserved, ValidityInvalid, ValidityQuestionable} {
		got := q.WithValidity(v)
		if got.Validity() != v || got&^validityMask != QualityOverflow {
			t.Errorf("WithValidity(%d) = %#x", v, uint16(got))
		}
	}
	for _, v := range []Validity{-1, 4, 7, math.MaxInt32} {
		got := Quality(0).WithValidity(v)
		if got&^validityMask != 0 {
			t.Errorf("WithValidity(%d) = %#x sets detail flags", v, uint16(got))
		}
	}
}

// TestEncodeTimeQualityAccuracyBounds checks TimeAccuracy at the edges of
// its 5-bit field; the encoding is unchanged by the rewrite.
func TestEncodeTimeQualityAccuracyBounds(t *testing.T) {
	for acc, want := range map[int]uint8{0: 0, 1: 1, 24: 24, 30: 30, 31: 31, 32: 0, 33: 1, -1: 31, 255: 31, 256: 0} {
		if got := encodeTimeQuality(TimeQuality{TimeAccuracy: acc}); got != want {
			t.Errorf("TimeAccuracy %d: %#x, want %#x", acc, got, want)
		}
	}
	if got := encodeTimeQuality(TimeQuality{LeapSecondKnown: true, ClockFailure: true, ClockNotSynchronized: true, TimeAccuracy: -1}); got != 0xff {
		t.Errorf("all bits: %#x", got)
	}
}

// TestHandleSegmentBound: SubSeqNum is INT16U, so a report has at most
// 65536 segments. Before the fix a peer that never cleared MoreSegments
// grew the reassembly buffer without bound.
func TestHandleSegmentBound(t *testing.T) {
	s := &ReportSubscription{client: &Client{logger: discardLogger()}}
	seg := func(sub uint32, more bool) *ReportIndication {
		return &ReportIndication{RptID: "r", SeqNum: 9, SubSeqNum: sub, MoreSegments: more}
	}
	for i := range uint32(math.MaxUint16 + 1) {
		if got := s.handleSegment(seg(i, true)); got != nil {
			t.Fatalf("segment %d completed the report", i)
		}
	}
	if len(s.segBuf) != math.MaxUint16+1 || !s.segActive {
		t.Fatalf("buffer %d active %v after 65536 segments", len(s.segBuf), s.segActive)
	}
	// Segment 65537 overflows SubSeqNum: the partial report is dropped.
	if got := s.handleSegment(seg(math.MaxUint16+1, true)); got != nil {
		t.Fatal("overflowing segment completed the report")
	}
	if len(s.segBuf) != 0 || s.segActive {
		t.Fatalf("buffer %d active %v after overflow, want reset", len(s.segBuf), s.segActive)
	}
	// A fresh two-segment report still reassembles.
	if s.handleSegment(seg(0, true)) != nil {
		t.Fatal("first segment completed the report")
	}
	if got := s.handleSegment(seg(1, false)); got == nil {
		t.Fatal("two-segment report not reassembled")
	}
}

// TestReportEngineStopCancelsSends: background report sends (and their
// pre-send pauses) end when the engine stops, and every send is bounded
// by unsolicitedSendTimeout. Before the fix they used
// context.Background() with no deadline, so a stuck peer kept the
// goroutine, and Stop with it, forever.
func TestReportEngineStopCancelsSends(t *testing.T) {
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	re := srv.EnableReports()

	sendCtx, cancel := re.sendContext()
	dl, ok := sendCtx.Deadline()
	if !ok || time.Until(dl) > unsolicitedSendTimeout {
		t.Errorf("send deadline %v, %v", dl, ok)
	}

	paused := make(chan bool, 1)
	re.wg.Go(func() { paused <- re.pauseUntil(time.Hour, nil) })
	replayed := make(chan struct{})
	var sc mms.ServerConn
	rt := &rcbRuntime{rcbItemID: "LLN0$BR$brcb01", buffered: true}
	stop, wake := make(chan struct{}), make(chan struct{}, 1)
	wake <- struct{}{}
	re.wg.Add(1)
	go func() {
		re.senderLoop(rt, &sc, stop, wake)
		close(replayed)
	}()

	start := time.Now()
	srv.Close() // stops the report engine
	if d := time.Since(start); d > time.Second {
		t.Errorf("Stop took %v", d)
	}
	if <-paused {
		t.Error("pause completed instead of observing Stop")
	}
	<-replayed
	if sendCtx.Err() == nil {
		t.Error("send context still live after Stop")
	}
	cancel()

	// An engine not built by EnableReports still bounds its sends.
	var bare ReportEngine
	ctx, c2 := bare.sendContext()
	defer c2()
	if _, ok := ctx.Deadline(); !ok {
		t.Error("bare engine send has no deadline")
	}
	if !bare.pauseUntil(time.Millisecond, nil) {
		t.Error("bare engine pause")
	}
}
