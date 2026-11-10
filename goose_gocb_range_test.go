// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-mms"
)

// Regression: decodeGoCB used to truncate out-of-range GoCB attributes
// read from a peer (ConfRev 2^32 became 0, VID 0x10001 became 1, ...).
// IEC 61850-7-2 types them INT32U / INT8U / INT16U, so a wider value is
// a protocol error.
func TestDecodeGoCBRanges(t *testing.T) {
	gocb := func(confRev, prio, vid, appID, minT, maxT uint64) *mms.Value {
		return mms.NewStructure([]*mms.Value{
			mms.NewBoolean(true), mms.NewVisibleString("id"), mms.NewVisibleString("ds"), mms.NewUnsigned(confRev), mms.NewBoolean(false),
			mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewUnsigned(prio), mms.NewUnsigned(vid), mms.NewUnsigned(appID)}),
			mms.NewUnsigned(minT), mms.NewUnsigned(maxT),
		})
	}
	cb, err := decodeGoCB("r", gocb(math.MaxUint32, 7, 0x0fff, math.MaxUint16, math.MaxUint32, math.MaxUint32))
	if err != nil {
		t.Fatal(err)
	}
	if cb.ConfRev != math.MaxUint32 || cb.DstAddress.Priority != 7 || cb.DstAddress.VID != 0x0fff ||
		cb.DstAddress.AppID != math.MaxUint16 || cb.MinTime != math.MaxUint32 || cb.MaxTime != math.MaxUint32 || !cb.HasTimes {
		t.Fatalf("in-range maxima: %+v", cb)
	}
	for name, v := range map[string]*mms.Value{
		"ConfRev":  gocb(math.MaxUint32+1, 4, 0, 1, 10, 1000),
		"PRIORITY": gocb(1, math.MaxUint8+1, 0, 1, 10, 1000),
		"VID":      gocb(1, 4, math.MaxUint16+1, 1, 10, 1000),
		// 8-1 Table 12: PRIORITY 0..7, VID 0..4095 (GSE-14).
		"PRIORITY 8": gocb(1, 8, 0, 1, 10, 1000),
		"VID 4096":   gocb(1, 4, 0x1000, 1, 10, 1000),
		"APPID":      gocb(1, 4, 0, math.MaxUint16+1, 10, 1000),
		"MinTime":    gocb(1, 4, 0, 1, math.MaxUint32+1, 1000),
		"MaxTime":    gocb(1, 4, 0, 1, 10, math.MaxUint32+1),
	} {
		if cb, err := decodeGoCB("r", v); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s out of range: %+v, %v; want ErrProtocol", name, cb, err)
		}
	}
}

// Regression: a DatSet change increments ConfRev in the store; from
// 2^32-1 that reaches 2^32, which the GOOSE publisher used to send as
// confRev 0. Enabling must now fail instead of publishing a wrapped
// configuration revision.
func TestGOOSEServerConfRevOverflow(t *testing.T) {
	ctx := context.Background()
	f := newGOOSEFixture(t, GOOSEOptions{StartDisabled: true})
	confRevKey := gcbAllRef + "$ConfRev"
	f.srv.store.Set(confRevKey, mms.NewUnsigned(math.MaxUint32))
	ds := "InteropIEDInteropLD/LLN0$dsGooseBool"
	if err := f.client.SetGoCB(ctx, "InteropIEDInteropLD", "LLN0$GO$gcbAll", GoCBUpdate{DatSet: &ds}); err != nil {
		t.Fatal(err)
	}
	if cr, _ := f.srv.store.Get(confRevKey).Uint64(); cr != math.MaxUint32+1 {
		t.Fatalf("ConfRev after DatSet change = %d", cr)
	}
	err := f.engine.SetEnabled(gcbAllRef, true)
	var dae *mms.DataAccessError
	if !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectValueInvalid {
		t.Fatalf("enable with ConfRev 2^32: %v, want object-value-invalid", err)
	}
	if on, _ := f.srv.store.Get(gcbAllRef + "$GoEna").Bool(); on {
		t.Fatal("GoEna set after refused enable")
	}
	// A client reading this GoCB reports the out-of-range ConfRev
	// instead of truncating it.
	if cb, err := f.client.GetGoCB(ctx, "InteropIEDInteropLD", "LLN0$GO$gcbAll"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("GetGoCB: %+v %v, want ErrProtocol", cb, err)
	}
}
