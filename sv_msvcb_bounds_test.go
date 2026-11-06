// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-mms"
)

// TestDecodeMSVCBRanges: MSVCB attributes are typed by IEC 61850-7-2
// (ConfRev INT32U, SmpRate INT16U, SmpMod ENUMERATED, DstAddress
// PRIORITY INT8U, VID and APPID INT16U, noASDU INT16U). A response
// with a value outside its type used to be truncated silently (ConfRev
// 2^32 read as 0, SmpRate 70000 as 4464, SmpMod -1 as 65535); it is now
// a protocol error. The type maxima themselves are accepted.
func TestDecodeMSVCBRanges(t *testing.T) {
	opt := mms.NewBitStringWithLength([]byte{0x20}, 5)
	addr := func(p, vid, app uint64) *mms.Value {
		return mms.NewStructure([]*mms.Value{mms.NewOctetString(make([]byte, 6)), mms.NewUnsigned(p), mms.NewUnsigned(vid), mms.NewUnsigned(app)})
	}
	mk := func(confRev, smpRate uint64, smpMod *mms.Value, dst *mms.Value, noASDU uint64) *mms.Value {
		e := []*mms.Value{mms.NewBoolean(true), mms.NewVisibleString("id"), mms.NewVisibleString("ds"), mms.NewUnsigned(confRev), mms.NewUnsigned(smpRate), opt}
		if smpMod != nil {
			e = append(e, smpMod)
		}
		return mms.NewStructure(append(e, dst, mms.NewUnsigned(noASDU)))
	}
	cb, err := decodeMSVCB("r", mk(math.MaxUint32, math.MaxUint16, mms.NewUnsigned(math.MaxUint16), addr(math.MaxUint8, math.MaxUint16, math.MaxUint16), math.MaxUint16))
	if err != nil || cb.ConfRev != math.MaxUint32 || cb.SmpRate != math.MaxUint16 || cb.SmpMod != math.MaxUint16 ||
		cb.DstAddress.Priority != math.MaxUint8 || cb.DstAddress.VID != math.MaxUint16 || cb.DstAddress.AppID != math.MaxUint16 || cb.NoASDU != math.MaxUint16 {
		t.Fatalf("maxima %+v %v", cb, err)
	}
	good := addr(4, 0, 0x4000)
	bad := map[string]*mms.Value{
		"ConfRev":      mk(1<<32, 80, nil, good, 1),
		"SmpRate":      mk(1, 70000, nil, good, 1),
		"SmpMod -1":    mk(1, 80, mms.NewInteger(-1), good, 1),
		"SmpMod 2^16":  mk(1, 80, mms.NewUnsigned(1<<16), good, 1),
		"SmpMod int":   mk(1, 80, mms.NewInteger(1<<16), good, 1),
		"PRIORITY":     mk(1, 80, nil, addr(256, 0, 0x4000), 1),
		"VID":          mk(1, 80, nil, addr(4, 1<<16, 0x4000), 1),
		"APPID":        mk(1, 80, nil, addr(4, 0, 1<<16), 1),
		"noASDU":       mk(1, 80, nil, good, 1<<16),
		"noASDU 2^32+": mk(1, 80, nil, good, 1<<32+1),
	}
	for name, v := range bad {
		if cb, err := decodeMSVCB("r", v); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: %+v %v", name, cb, err)
		}
	}
}

// TestSVServerSmpRateWidth: SmpRate is INT16U. A client write of 70000
// used to be stored and later published as smpRate 4464 (70000 mod
// 2^16); it is now refused with object-value-invalid. A ConfRev or
// noASDU outside its type that reaches the store some other way stops
// the SvCB from being enabled instead of being truncated.
func TestSVServerSmpRateWidth(t *testing.T) {
	ctx := context.Background()
	f := newSVFixture(t, SVOptions{StartDisabled: true})
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MUIEDMU01", ItemID: "LLN0$MS$MSVCB01$SmpRate", Value: mms.NewUnsigned(70000)}); err == nil {
		t.Error("SmpRate 70000 accepted")
	}
	if v, _ := f.srv.store.Get(msvcb1Ref + "$SmpRate").Uint64(); v == 70000 {
		t.Error("SmpRate 70000 stored")
	}
	if _, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MUIEDMU01", ItemID: "LLN0$MS$MSVCB01$SmpRate", Value: mms.NewUnsigned(math.MaxUint16)}); err != nil {
		t.Errorf("SmpRate 65535: %v", err)
	}
	for _, c := range []struct {
		field string
		v     *mms.Value
	}{
		{"ConfRev", mms.NewUnsigned(1 << 32)},
		{"noASDU", mms.NewUnsigned(1 << 16)},
		{"SmpRate", mms.NewUnsigned(1 << 16)},
		{"SmpMod", mms.NewInteger(-1)},
	} {
		key := msvcb2Ref + "$" + c.field
		old := f.srv.store.Get(key)
		f.srv.store.Set(key, c.v)
		if err := f.engine.SetEnabled(msvcb2Ref, true); !errors.Is(err, ErrSV) {
			t.Errorf("%s %v: %v", c.field, c.v, err)
		}
		if p, _ := f.engine.Publisher(msvcb2Ref); p != nil {
			t.Errorf("%s: publisher started", c.field)
		}
		f.srv.store.Set(key, old)
	}
	if err := f.engine.SetEnabled(msvcb2Ref, true); err != nil {
		t.Errorf("restored values: %v", err)
	}
}
