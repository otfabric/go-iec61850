// SPDX-License-Identifier: MIT

package iec61850

import (
	"slices"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/mapping"
)

// SRV-06: control blocks are not data objects (IEC 61850-7-2
// GetLogicalNodeDirectory per ACSI class; IEC 61850-8-1 Figure 3).
func TestSpec_7_2_GetLogicalNodeDirectory_DataOnly(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	ctx := waitCtx(t)
	dos, err := c.ListDataObjects(ctx, ctlLD, "LLN0")
	if err != nil {
		t.Fatal(err)
	}
	children, err := c.ListChildren(ctx, Ref{LD: ctlLD, LN: "LLN0"})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(dos))
	for _, d := range dos {
		names = append(names, d.Name)
	}
	for _, ch := range children {
		names = append(names, ch.Name)
	}
	if !slices.Contains(names, "Mod") {
		t.Errorf("LLN0 data objects %v lack Mod", names)
	}
	for _, cb := range []string{"brcb01", "urcb01", "gcbAll", "gcbBool"} {
		if slices.Contains(names, cb) {
			t.Errorf("control block %s listed as a data object: %v", cb, names)
		}
	}
	// The control block is still browsable below its name.
	if kids, err := c.ListChildren(ctx, Ref{LD: ctlLD, LN: "LLN0", Path: []string{"brcb01"}}); err != nil || len(kids) == 0 {
		t.Errorf("ListChildren(brcb01) = %v, %v", kids, err)
	}

	for item, want := range map[string]bool{
		"LLN0$SP$SGCB$ActSG": true, "LLN0$BR$brcb01$RptID": true, "LLN0$GO$gcb01": true,
		"LLN0$MS$msv": true, "LLN0$US$usv": true, "LLN0$LG$log": true, "LLN0$GS$gs": true,
		"GGIO1$SP$SGCB$x": true, "LLN0$SP$SetInt1": false, "LLN0$ST$Mod$stVal": false, "LLN0$SV$x$subVal": false,
	} {
		pv, _ := mapping.ParseItemID(item)
		got := mapping.IsControlBlockItem(pv)
		if item == "GGIO1$SP$SGCB$x" {
			want = false // the SGCB is in LLN0 only
		}
		if got != want {
			t.Errorf("IsControlBlockItem(%s) = %v, want %v", item, got, want)
		}
	}
}

// SRV-07: the FC components of a logical node in the order of IEC
// 61850-8-1 Figure 4.
func TestSpec_8_1_Figure4_FCOrder(t *testing.T) {
	c := controlPair(t, ControlHandler{})
	ctx := waitCtx(t)
	rank := func(fc string) int {
		for i, f := range []string{"MX", "ST", "CO", "CF", "DC", "SP", "SG", "RP", "LG", "BR", "GO", "GS", "SV", "SE", "MS", "US", "EX", "SR", "OR", "BL"} {
			if f == fc {
				return i
			}
		}
		return 99
	}
	for _, ln := range []string{"LLN0", "GGIO1"} {
		attrs, err := c.mmsClient.GetVariableAccessAttributes(ctx, mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: ctlLD, ItemID: mms.ItemID(ln)})
		if err != nil {
			t.Fatal(err)
		}
		var fcs []string
		for _, e := range attrs.TypeSpec.Elements {
			fcs = append(fcs, e.Name)
		}
		if !slices.IsSortedFunc(fcs, func(a, b string) int { return rank(a) - rank(b) }) || len(fcs) < 2 {
			t.Errorf("%s FC components %v, not in Figure 4 order", ln, fcs)
		}
	}
}
