// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/sv"
)

// TestSpec_6_Table30_UnicastSMVSkipped: multicast="false" is a USVCB
// (6 Table 30, 7-2 19.3), which is not implemented; it is not served as
// an MSVCB (SV-03).
func TestSpec_6_Table30_UnicastSMVSkipped(t *testing.T) {
	raw, err := os.ReadFile("sv/testdata/mu-9-2le.icd")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(string(raw), `nofASDU="8" multicast="true"`, `nofASDU="8" multicast="false"`, 1)
	doc, err := scl.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "MUIED", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range model.Warnings {
		if strings.Contains(w, `"MSVCB02"`) && strings.Contains(w, "USVCB") {
			found = true
		}
	}
	if !found {
		t.Errorf("no USVCB warning in %v", model.Warnings)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	e, err := srv.EnableSV(SVOptions{Transport: sv.NewHub().NewPort(sv.MAC{}), StartDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range e.ControlBlocks() {
		if ref == msvcb2Ref {
			t.Errorf("unicast block served as MSVCB: %v", e.ControlBlocks())
		}
	}
	if v := srv.store.Get(msvcb2Ref + "$SvEna"); v != nil {
		t.Errorf("MSVCB02 attributes registered: %v", v)
	}
}

// TestSpec_9_2_Table14_ReservedSmpSynch: smpSynch 3, 4 and 255 are
// "Reserved values – Do not use" (SV-06).
func TestSpec_9_2_Table14_ReservedSmpSynch(t *testing.T) {
	port := sv.NewHub().NewPort(sv.MAC{})
	base := sv.PublisherConfig{Dst: sv.MAC{1, 0x0c, 0xcd, 4, 0, 1}, AppID: 0x4000, SvID: "MU", SmpRate: 80, Layout: sv.LELayout}
	for _, s := range []uint8{3, 4, 255} {
		c := base
		c.SmpSynch = s
		if _, err := sv.NewPublisher(port, c); !errors.Is(err, sv.ErrInvalidConfig) {
			t.Errorf("smpSynch %d: %v", s, err)
		}
	}
	c := base
	c.SmpSynch = 2
	p, err := sv.NewPublisher(port, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	p.SetSmpSynch(3)
	if got := p.State().SmpSynch; got != 2 {
		t.Errorf("reserved SetSmpSynch applied: %d", got)
	}
	p.SetSmpSynch(5)
	if got := p.State().SmpSynch; got != 5 {
		t.Errorf("SetSmpSynch(5): %d", got)
	}
	// The server engine refuses a reserved SVOptions.SmpSynch.
	f := newSVFixture(t, SVOptions{StartDisabled: true})
	f.engine.Stop()
	f.srv.svEngine = nil
	if _, err := f.srv.EnableSV(SVOptions{Transport: sv.NewHub().NewPort(sv.MAC{}), SmpSynch: 4}); !errors.Is(err, ErrSV) {
		t.Errorf("EnableSV with smpSynch 4: %v", err)
	}
}

// TestSpec_9_2_Table9_DatSetQualified: a bare dataset name written to
// the MSVCB is stored as the IEC reference sent in the ASDU datSet
// (9-2 Table 9) and counts as a ConfRev change (7-2 19.2.1.6) (SV-04).
func TestSpec_9_2_Table9_DatSetQualified(t *testing.T) {
	f := newSVFixture(t, SVOptions{StartDisabled: true})
	f.srv.store.Set(msvcb1Ref+"$DatSet", mms.NewVisibleString("MUIEDMU01/LLN0$other"))
	cr, _ := f.srv.store.Get(msvcb1Ref + "$ConfRev").Uint64()
	if _, err := f.engine.handleWrite(msvcb1Ref+"$DatSet", mms.NewVisibleString("PhsMeas1")); err != nil {
		t.Fatal(err)
	}
	if ds, _ := f.srv.store.Get(msvcb1Ref + "$DatSet").VisibleString(); ds != "MUIEDMU01/LLN0$PhsMeas1" {
		t.Errorf("DatSet %q", ds)
	}
	if got, _ := f.srv.store.Get(msvcb1Ref + "$ConfRev").Uint64(); got != cr+1 {
		t.Errorf("ConfRev %d, want %d", got, cr+1)
	}
}
