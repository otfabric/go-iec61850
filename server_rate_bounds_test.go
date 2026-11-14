// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/sv"
)

// TestMinIntgPdMillis: ServerOptions.MinIntegrityPeriod in whole
// milliseconds: zero is the default, negative is no minimum, sub-ms
// periods round up and huge ones saturate at INT32U.
func TestMinIntgPdMillis(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want uint32
	}{
		{0, uint32(DefaultMinIntegrityPeriod / time.Millisecond)},
		{-1, 0},
		{time.Microsecond, 1},
		{time.Millisecond, 1},
		{1500 * time.Microsecond, 2},
		{250 * time.Millisecond, 250},
		{math.MaxUint32 * time.Millisecond, math.MaxUint32},
		{math.MaxInt64, math.MaxUint32},
	} {
		if got := minIntgPdMillis(c.d); got != c.want {
			t.Errorf("minIntgPdMillis(%v) = %d, want %d", c.d, got, c.want)
		}
	}
}

func newBoundsSpecServer(t *testing.T, opts ServerOptions) *Server {
	t.Helper()
	srv, err := NewServer(specModel(), opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.EnableReports()
	t.Cleanup(srv.Close)
	return srv
}

// TestRCBIntgPdMinimum: a client write of a non-zero IntgPd below
// ServerOptions.MinIntegrityPeriod to a BRCB or URCB is refused with
// object-value-invalid and leaves the stored value unchanged; 0 (no
// integrity period) and values at or above the minimum are accepted.
func TestRCBIntgPdMinimum(t *testing.T) {
	ctx := context.Background()
	srv := newBoundsSpecServer(t, ServerOptions{})
	c := dialLoopback(t, ctx, srv)
	for _, rcb := range []string{specBRCB, specURCB} {
		key := "LD1/" + rcb + "$IntgPd"
		before := srv.store.Get(key)
		for _, v := range []uint64{1, 99} {
			if code := dataAccessCode(rawWrite(c, rcb+"$IntgPd", mms.NewUnsigned(v))); code != mms.DataAccessErrorObjectValueInvalid {
				t.Errorf("%s IntgPd %d: code %v, want object-value-invalid", rcb, v, code)
			}
			if !srv.store.Get(key).Equal(before) {
				t.Errorf("%s IntgPd %d stored", rcb, v)
			}
		}
		for _, v := range []uint64{100, 500, 0} {
			if err := rawWrite(c, rcb+"$IntgPd", mms.NewUnsigned(v)); err != nil {
				t.Errorf("%s IntgPd %d: %v", rcb, v, err)
			}
			if got, _ := srv.store.Get(key).Uint64(); got != v {
				t.Errorf("%s IntgPd = %d, want %d", rcb, got, v)
			}
		}
	}
}

// TestRCBIntgPdMinimumOptions: a configured minimum replaces the
// default, and a negative one removes it.
func TestRCBIntgPdMinimumOptions(t *testing.T) {
	ctx := context.Background()
	srv := newBoundsSpecServer(t, ServerOptions{MinIntegrityPeriod: time.Second})
	c := dialLoopback(t, ctx, srv)
	if code := dataAccessCode(rawWrite(c, specURCB+"$IntgPd", mms.NewUnsigned(999))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("IntgPd 999 with a 1 s minimum: code %v", code)
	}
	if err := rawWrite(c, specURCB+"$IntgPd", mms.NewUnsigned(1000)); err != nil {
		t.Errorf("IntgPd 1000 with a 1 s minimum: %v", err)
	}

	srv = newBoundsSpecServer(t, ServerOptions{MinIntegrityPeriod: -1})
	c = dialLoopback(t, ctx, srv)
	if err := rawWrite(c, specBRCB+"$IntgPd", mms.NewUnsigned(1)); err != nil {
		t.Errorf("IntgPd 1 without a minimum: %v", err)
	}
}

// TestRCBIntgPdModelNotChecked: an IntgPd below the minimum configured
// in the model is served as is.
func TestRCBIntgPdModelNotChecked(t *testing.T) {
	m := specModel()
	m.LogicalDevices[0].LogicalNodes[0].Reports[0].IntgPd = 10
	srv, err := NewServer(m, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	re := srv.EnableReports()
	rt := re.rcbs["LD1/"+specBRCB]
	if rt == nil {
		t.Fatalf("RCB %s not registered", specBRCB)
	}
	if rt.intgPd != 10 {
		t.Errorf("model IntgPd = %d, want 10", rt.intgPd)
	}
}

// TestLCBIntgPdMinimum: the same minimum applies to LCB IntgPd writes;
// an IntgPd below it in the model is not checked.
func TestLCBIntgPdMinimum(t *testing.T) {
	ctx := context.Background()
	m := lcbModel()
	m.LogicalDevices[0].LogicalNodes[0].Logs[0].IntgPd = 10
	srv, je := newLCBServer(t, m)
	if rt := je.lcbs["LD1/"+specLCB]; rt == nil || rt.intgPd != 10 {
		t.Fatalf("model IntgPd not served: %+v", rt)
	}
	c := dialLoopback(t, ctx, srv)
	if err := rawWrite(c, specLCB+"$LogEna", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	key := "LD1/" + specLCB + "$IntgPd"
	if code := dataAccessCode(rawWrite(c, specLCB+"$IntgPd", mms.NewUnsigned(1))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("LCB IntgPd 1: code %v, want object-value-invalid", code)
	}
	if got, _ := srv.store.Get(key).Uint64(); got != 10 {
		t.Errorf("LCB IntgPd after refused write = %d, want 10", got)
	}
	for _, v := range []uint64{100, 0} {
		if err := rawWrite(c, specLCB+"$IntgPd", mms.NewUnsigned(v)); err != nil {
			t.Errorf("LCB IntgPd %d: %v", v, err)
		}
	}
}

// TestSVServerMaxSamplesPerSecond: enabling an MSVCB whose SmpRate and
// SmpMod select more than SVOptions.MaxSamplesPerSecond (default 96000
// samples/s) is refused with object-value-invalid and starts no
// publisher. The SmpRate and SmpMod writes themselves are accepted, so
// a client can change both in either order.
func TestSVServerMaxSamplesPerSecond(t *testing.T) {
	ctx := context.Background()
	f := newSVFixture(t, SVOptions{StartDisabled: true})
	write := func(field string, v *mms.Value) error {
		_, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MUIEDMU01", ItemID: mms.ItemID("LLN0$MS$MSVCB01$" + field), Value: v})
		return err
	}
	enable := func(on bool) error { return write("SvEna", mms.NewBoolean(on)) }

	// 65535 samples per 50 Hz period.
	if err := write("SmpRate", mms.NewUnsigned(math.MaxUint16)); err != nil {
		t.Fatalf("SmpRate 65535: %v", err)
	}
	if code := dataAccessCode(enable(true)); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("SvEna at 3276750 samples/s: code %v, want object-value-invalid", code)
	}
	if p, _ := f.engine.Publisher(msvcb1Ref); p != nil {
		t.Error("publisher started")
	}
	if on, _ := f.srv.store.Get(msvcb1Ref + "$SvEna").Bool(); on {
		t.Error("SvEna TRUE after refused enable")
	}
	err := f.engine.SetEnabled(msvcb1Ref, true)
	var dae *mms.DataAccessError
	if !errors.Is(err, ErrSV) || !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("SetEnabled: %v, want ErrSV and object-value-invalid", err)
	}

	for _, c := range []struct {
		rate uint64
		mod  sv.SmpMod
		ok   bool
	}{
		{1920, sv.SmpPerPeriod, true}, // 96000 samples/s
		{1921, sv.SmpPerPeriod, false},
		{math.MaxUint16, sv.SmpPerSec, true},
		{1, sv.SecPerSmp, true},
	} {
		if err := write("SmpMod", mms.NewInteger(int64(c.mod))); err != nil {
			t.Fatalf("SmpMod %d: %v", c.mod, err)
		}
		if err := write("SmpRate", mms.NewUnsigned(c.rate)); err != nil {
			t.Fatalf("SmpRate %d: %v", c.rate, err)
		}
		err := enable(true)
		if c.ok != (err == nil) {
			t.Errorf("SmpRate %d SmpMod %d: enable %v, want ok=%v", c.rate, c.mod, err, c.ok)
		}
		if err := enable(false); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSVServerMaxSamplesPerSecondOptions: a configured maximum below an
// MSVCB's SCL rate still serves that rate (MSVCB02 is 256 samples per
// period, 12800 samples/s); a negative maximum removes the bound.
func TestSVServerMaxSamplesPerSecondOptions(t *testing.T) {
	f := newSVFixture(t, SVOptions{MaxSamplesPerSecond: 5000})
	if p, err := f.engine.Publisher(msvcb2Ref); err != nil || p == nil {
		t.Fatalf("MSVCB02 at its SCL rate: %v %v", p, err)
	}
	rt1, err := f.engine.lookup(msvcb1Ref) // SCL 80 per period, 4000 samples/s
	if err != nil {
		t.Fatal(err)
	}
	if rt1.maxRate() != 5000 || !rt1.rateAllowed(100, int64(sv.SmpPerPeriod)) || rt1.rateAllowed(101, int64(sv.SmpPerPeriod)) {
		t.Errorf("max 5000: maxRate %v", rt1.maxRate())
	}
	rt2, _ := f.engine.lookup(msvcb2Ref)
	if rt2.maxRate() != 12800 || rt2.rateAllowed(257, int64(sv.SmpPerPeriod)) {
		t.Errorf("MSVCB02 maxRate %v, want its SCL rate 12800", rt2.maxRate())
	}

	g := newSVFixture(t, SVOptions{StartDisabled: true, MaxSamplesPerSecond: -1})
	rt, _ := g.engine.lookup(msvcb1Ref)
	if !math.IsInf(rt.maxRate(), 1) || !rt.rateAllowed(math.MaxUint16, int64(sv.SmpPerPeriod)) {
		t.Errorf("no bound: maxRate %v", rt.maxRate())
	}
}

// TestSVServerConfRevBound: ConfRev is INT32U. At 4294967295 an MsvID or
// DatSet change, which increments it, is refused with
// object-value-invalid and changes nothing, so the MSVCB stays
// enablable; writing the stored value again is accepted.
func TestSVServerConfRevBound(t *testing.T) {
	ctx := context.Background()
	f := newSVFixture(t, SVOptions{StartDisabled: true})
	f.srv.store.Set(msvcb1Ref+"$ConfRev", mms.NewUnsigned(math.MaxUint32))
	write := func(field string, v *mms.Value) error {
		_, err := f.client.MMS().Write(ctx, mms.WriteRequest{DomainID: "MUIEDMU01", ItemID: mms.ItemID("LLN0$MS$MSVCB01$" + field), Value: v})
		return err
	}
	id := f.srv.store.Get(msvcb1Ref + "$MsvID")
	if code := dataAccessCode(write("MsvID", mms.NewVisibleString("other"))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("MsvID change: code %v, want object-value-invalid", code)
	}
	if !f.srv.store.Get(msvcb1Ref + "$MsvID").Equal(id) {
		t.Error("MsvID changed by a refused write")
	}
	if err := write("MsvID", id); err != nil {
		t.Errorf("unchanged MsvID: %v", err)
	}
	// DatSet by its bare name differs from the stored qualified name
	// only in form: no change, accepted.
	if err := write("DatSet", mms.NewVisibleString("PhsMeas1")); err != nil {
		t.Errorf("unchanged DatSet: %v", err)
	}
	// A DatSet change: point the stored value elsewhere first.
	f.srv.store.Set(msvcb1Ref+"$DatSet", mms.NewVisibleString("MUIEDMU01/LLN0$Other"))
	if code := dataAccessCode(write("DatSet", mms.NewVisibleString("PhsMeas1"))); code != mms.DataAccessErrorObjectValueInvalid {
		t.Errorf("DatSet change: code %v, want object-value-invalid", code)
	}
	if cr, _ := f.srv.store.Get(msvcb1Ref + "$ConfRev").Uint64(); cr != math.MaxUint32 {
		t.Errorf("ConfRev = %d", cr)
	}
	f.srv.store.Set(msvcb1Ref+"$DatSet", mms.NewVisibleString("MUIEDMU01/LLN0$PhsMeas1"))
	if err := f.engine.SetEnabled(msvcb1Ref, true); err != nil {
		t.Errorf("enable at ConfRev 4294967295: %v", err)
	}
}
