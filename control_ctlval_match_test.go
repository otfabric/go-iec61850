// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// ctlValCase is one ctlVal type the server model supports: the SCL basic
// type of the Oper/SBOw/Cancel ctlVal, the selected value and another
// value of the same type and the same size.
type ctlValCase struct {
	name     string
	bda      string // the ctlVal BDA of the control structures
	selected *mms.Value
	other    *mms.Value
}

func ctlValCases() []ctlValCase {
	return []ctlValCase{
		{"BOOLEAN", `bType="BOOLEAN"`, mms.NewBoolean(true), mms.NewBoolean(false)},
		{"INT8", `bType="INT8"`, mms.NewInteger(1), mms.NewInteger(-1)},
		{"INT32U", `bType="INT32U"`, mms.NewUnsigned(7), mms.NewUnsigned(8)},
		{"Enum", `bType="Enum" type="CtlModels"`, mms.NewInteger(1), mms.NewInteger(2)},
		{"FLOAT32", `bType="FLOAT32"`, mms.NewFloat(1.5), mms.NewFloat(2.5)},
		// Tcmd, a CODED ENUM in BIT STRING(2): lower 01, higher 10.
		{"Tcmd", `bType="Tcmd"`, mms.NewBitStringWithLength([]byte{0x40}, 2), mms.NewBitStringWithLength([]byte{0x80}, 2)},
		{"Octet64", `bType="Octet64"`, mms.NewOctetString([]byte{1, 2, 3, 4}), mms.NewOctetString([]byte{1, 2, 3, 5})},
		{"VisString64", `bType="VisString64"`, mms.NewVisibleString("open"), mms.NewVisibleString("shut")},
		{"AnalogueValue", `bType="Struct" type="AnalogueValue"`,
			mms.NewStructure([]*mms.Value{mms.NewFloat(10)}), mms.NewStructure([]*mms.Value{mms.NewFloat(11)})},
		// A structure whose members are a bit string and an octet string.
		{"Struct", `bType="Struct" type="CtlValPair"`,
			mms.NewStructure([]*mms.Value{mms.NewBitStringWithLength([]byte{0x40}, 2), mms.NewOctetString([]byte{9, 9})}),
			mms.NewStructure([]*mms.Value{mms.NewBitStringWithLength([]byte{0x40}, 2), mms.NewOctetString([]byte{9, 8})})},
	}
}

// ctlValServer serves the interop GOOSE model with one control with SBO
// with enhanced security per case (GGIO1.Ctl0, Ctl1, ...), whose
// OnOperate records the operated ctlVal.
func ctlValServer(t *testing.T, cases []ctlValCase, operated func(i int, v *mms.Value)) *Server {
	t.Helper()
	raw, err := os.ReadFile("goose/testdata/interop-goose.cid")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	replace := func(old, new string) {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("fixture: %q not found", old)
		}
		s = strings.Replace(s, old, new, 1)
	}
	var dos, doTypes, daTypes strings.Builder
	for i, c := range cases {
		fmt.Fprintf(&dos, "\n      <DO name=\"Ctl%d\" type=\"Ctl%d_Type\"/>", i, i)
		fmt.Fprintf(&doTypes, `<DOType id="Ctl%[1]d_Type" cdc="SPC">
      <DA name="q"        fc="ST" bType="Quality"/>
      <DA name="t"        fc="ST" bType="Timestamp"/>
      <DA name="ctlModel" fc="CF" bType="Enum" type="CtlModels"/>
      <DA name="SBOw"     fc="CO" bType="Struct" type="OperCtl%[1]d"/>
      <DA name="Oper"     fc="CO" bType="Struct" type="OperCtl%[1]d"/>
      <DA name="Cancel"   fc="CO" bType="Struct" type="CancelCtl%[1]d"/>
    </DOType>
    `, i)
		fmt.Fprintf(&daTypes, `<DAType id="OperCtl%[1]d">
      <BDA name="ctlVal" %[2]s/>
      <BDA name="origin" bType="Struct" type="Originator_Type"/>
      <BDA name="ctlNum" bType="INT8U"/>
      <BDA name="T"      bType="Timestamp"/>
      <BDA name="Test"   bType="BOOLEAN"/>
      <BDA name="Check"  bType="Check"/>
    </DAType>
    <DAType id="CancelCtl%[1]d">
      <BDA name="ctlVal" %[2]s/>
      <BDA name="origin" bType="Struct" type="Originator_Type"/>
      <BDA name="ctlNum" bType="INT8U"/>
      <BDA name="T"      bType="Timestamp"/>
      <BDA name="Test"   bType="BOOLEAN"/>
    </DAType>
    `, i, c.bda)
	}
	replace(`<DO name="SPCSO3" type="SPC_Type"/>`, `<DO name="SPCSO3" type="SPC_Type"/>`+dos.String())
	replace(`<DOType id="MV_Type"`, doTypes.String()+`<DOType id="MV_Type"`)
	replace(`<DAType id="Originator_Type"`, daTypes.String()+`<DAType id="CtlValPair">
      <BDA name="cmd" bType="Tcmd"/>
      <BDA name="id"  bType="Octet64"/>
    </DAType>
    <DAType id="Originator_Type"`)
	doc, err := scl.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	for i := range cases {
		mustRegister(t, srv, fmt.Sprintf("GGIO1.Ctl%d", i), CtlModelSBOEnhanced, ControlHandler{
			OnOperate: func(_ context.Context, req ControlRequest) error {
				operated(i, req.CtlVal)
				return nil
			},
		})
	}
	return srv
}

// IEC 61850-7-2 20.5.2, Table 47 (inconsistent-parameters): the ctlVal of
// an Operate or a Cancel is compared with the ctlVal of the
// SelectWithValue by content, for every ctlVal type the model supports.
// A value of the same type and size with another content is refused with
// AddCause inconsistent-parameters and is not operated; the selected
// value is operated.
func TestSpec_7_2_Table47_CtlValComparedByContent(t *testing.T) {
	cases := ctlValCases()
	var mu sync.Mutex
	got := make(map[int][]*mms.Value)
	srv := ctlValServer(t, cases, func(i int, v *mms.Value) {
		mu.Lock()
		defer mu.Unlock()
		got[i] = append(got[i], v.Clone())
	})
	c := controlClient(t, srv)
	ctx := waitCtx(t)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := mustParseRef(t, fmt.Sprintf("%s/GGIO1.Ctl%d", ctlLD, i))
			n := uint8(10 + i)
			sel := OperateParams{CtlVal: tc.selected, CtlNum: n}

			// Operate with another value: refused, the selection released.
			if err := c.SelectWithValue(ctx, ref, sel); err != nil {
				t.Fatalf("SelectWithValue: %v", err)
			}
			refusal(t, "Oper with another ctlVal", c.Operate(ctx, ref, OperateParams{CtlVal: tc.other, CtlNum: n}),
				AddCauseInconsistentParameters, mms.DataAccessErrorObjectAccessDenied)

			// Cancel with another value: refused, the selection kept.
			if err := c.SelectWithValue(ctx, ref, sel); err != nil {
				t.Fatalf("SelectWithValue after the refused Operate: %v", err)
			}
			refusal(t, "Cancel with another ctlVal", c.Cancel(ctx, ref, CancelParams{CtlVal: tc.other, CtlNum: n}),
				AddCauseInconsistentParameters, mms.DataAccessErrorObjectAccessDenied)

			// The selected value is operated.
			if term, err := c.OperateWait(ctx, ref, sel); err != nil || !term.Positive() {
				t.Fatalf("OperateWait with the selected ctlVal = %+v, %v", term, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(got[i]) != 1 || !got[i][0].Equal(tc.selected) {
				t.Errorf("operated ctlVal = %v, want only the selected %v", got[i], tc.selected)
			}
		})
	}
}

// mmsValuesEqual compares by type and content: two values of one type and
// one size that differ in content are different, whatever the type.
func TestMmsValuesEqual_ByContent(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name       string
		a, same, b *mms.Value
	}{
		{"boolean", mms.NewBoolean(true), mms.NewBoolean(true), mms.NewBoolean(false)},
		{"integer", mms.NewInteger(-3), mms.NewInteger(-3), mms.NewInteger(3)},
		{"unsigned", mms.NewUnsigned(3), mms.NewUnsigned(3), mms.NewUnsigned(4)},
		{"float", mms.NewFloat(1.25), mms.NewFloat(1.25), mms.NewFloat(1.5)},
		{"bit string", mms.NewBitStringWithLength([]byte{0x40}, 2), mms.NewBitStringWithLength([]byte{0x40}, 2), mms.NewBitStringWithLength([]byte{0x80}, 2)},
		{"octet string", mms.NewOctetString([]byte{1, 2}), mms.NewOctetString([]byte{1, 2}), mms.NewOctetString([]byte{1, 3})},
		{"visible string", mms.NewVisibleString("ab"), mms.NewVisibleString("ab"), mms.NewVisibleString("ac")},
		{"mms string", mms.NewMmsString("ab"), mms.NewMmsString("ab"), mms.NewMmsString("ac")},
		{"utc time", mms.NewUTCTimeWithQuality(at, 0), mms.NewUTCTimeWithQuality(at, 0), mms.NewUTCTimeWithQuality(at.Add(time.Millisecond), 0)},
		{"structure", mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{1})}), mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{1})}),
			mms.NewStructure([]*mms.Value{mms.NewOctetString([]byte{2})})},
		{"array", mms.NewArray([]*mms.Value{mms.NewBitStringWithLength([]byte{0x80}, 1)}), mms.NewArray([]*mms.Value{mms.NewBitStringWithLength([]byte{0x80}, 1)}),
			mms.NewArray([]*mms.Value{mms.NewBitStringWithLength([]byte{0x00}, 1)})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !mmsValuesEqual(tt.a, tt.same) {
				t.Errorf("%v and %v: want equal", tt.a, tt.same)
			}
			if mmsValuesEqual(tt.a, tt.b) {
				t.Errorf("%v and %v (other content): want different", tt.a, tt.b)
			}
		})
	}
	// The type is part of the comparison.
	if mmsValuesEqual(mms.NewInteger(5), mms.NewUnsigned(5)) {
		t.Error("INTEGER 5 and UNSIGNED 5: want different")
	}
	if mmsValuesEqual(mms.NewBitStringWithLength([]byte{0x80}, 2), mms.NewBitStringWithLength([]byte{0x80}, 3)) {
		t.Error("bit strings of 2 and 3 bits: want different")
	}
}
