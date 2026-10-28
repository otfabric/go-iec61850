// SPDX-License-Identifier: MIT

package servermodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// nestedModel builds a hand-written model with sub-data-objects and
// structured data attributes so the recursive config/MMS printers are
// exercised beyond what the SCL-derived fixtures reach.
func nestedModel() *Model {
	return &Model{LogicalDevices: []LogicalDevice{{
		Name: "IEDLD1",
		LogicalNodes: []LogicalNode{{
			Name:    "MMXU1",
			LNClass: "MMXU",
			DataObjects: []DataObject{{
				Name: "PhV",
				CDC:  "WYE",
				Children: []DataObject{{
					Name: "phsA",
					CDC:  "CMV",
					Attributes: []DataAttribute{{
						Name: "cVal",
						FC:   "MX",
						Children: []DataAttribute{
							{Name: "mag", BType: "Struct", Children: []DataAttribute{
								{Name: "f", BType: "FLOAT32", InitialValue: "1.5"},
							}},
							{Name: "ang", FC: "CF", BType: "FLOAT32"},
						},
					}},
				}},
				Attributes: []DataAttribute{{Name: "d", FC: "DC", BType: "VisString255"}},
			}},
			DataSets: []DataSetDef{{Name: "dsMX", Members: []DataSetMemberDef{
				{LDInst: "LD1", LNName: "MMXU1", DOPath: "PhV.phsA", FC: "MX"},
			}}},
			Reports: []ReportDef{
				{Name: "urcb01", RptID: "u1", DatSet: "dsMX", ConfRev: 1},
				{Name: "brcb01", DatSet: "dsMX", ConfRev: 2, Buffered: true, BufTime: 50, IntgPd: 1000},
			},
		}},
	}}}
}

func TestGenerateMMS_NestedObjectsAndAttributes(t *testing.T) {
	var buf bytes.Buffer
	if err := GenerateMMS(&buf, nestedModel()); err != nil {
		t.Fatalf("GenerateMMS: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"domain: IEDLD1\n",
		// FC inherited from the parent structured attribute (MX) at two levels.
		"  var: IEDLD1/MMXU1$MX$PhV$phsA$cVal$mag$f  [MX] FLOAT32\n",
		// A child with its own FC keeps it.
		"  var: IEDLD1/MMXU1$CF$PhV$phsA$cVal$ang  [CF] FLOAT32\n",
		"  var: IEDLD1/MMXU1$DC$PhV$d  [DC] VisString255\n",
		"  nvl: IEDLD1/MMXU1$dsMX\n",
		"  urcb: IEDLD1/MMXU1$RP$urcb01  store=IEDLD1/MMXU1$RP$urcb01  datSet=dsMX\n",
		"    sub: MMXU1$RP$urcb01$Resv  store=IEDLD1/MMXU1$RP$urcb01$Resv\n",
		"  brcb: IEDLD1/MMXU1$BR$brcb01  store=IEDLD1/MMXU1$BR$brcb01  datSet=dsMX\n",
		"    sub: MMXU1$BR$brcb01$TimeOfEntry  store=IEDLD1/MMXU1$BR$brcb01$TimeOfEntry\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	// Structured attributes themselves must not be emitted as leaf vars.
	if strings.Contains(out, "$cVal  [") || strings.Contains(out, "$mag  [") {
		t.Errorf("structured attribute printed as leaf:\n%s", out)
	}
	if strings.Contains(out, "brcb01$Resv") || strings.Contains(out, "urcb01$PurgeBuf") {
		t.Errorf("buffered/unbuffered sub-fields mixed up:\n%s", out)
	}
}

// failAfterWriter accepts n writes and then fails every subsequent write.
type failAfterWriter struct {
	n      int
	writes int
}

var errWriteFailed = errors.New("write failed")

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.writes >= w.n {
		return 0, errWriteFailed
	}
	w.writes++
	return len(p), nil
}

func TestGenerateMMS_PropagatesWriterErrors(t *testing.T) {
	m := nestedModel()
	var full bytes.Buffer
	if err := GenerateMMS(&full, m); err != nil {
		t.Fatal(err)
	}
	total := strings.Count(full.String(), "\n")
	if total < 5 {
		t.Fatalf("unexpectedly short output (%d lines)", total)
	}
	// Each Fprintf emits exactly one line, so failing at every line index
	// walks every error-return branch in GenerateMMS/printDO/printDA/printRCB.
	for i := 0; i < total; i++ {
		w := &failAfterWriter{n: i}
		if err := GenerateMMS(w, m); !errors.Is(err, errWriteFailed) {
			t.Fatalf("fail at write %d: err = %v, want errWriteFailed", i, err)
		}
	}
	if err := GenerateMMS(&failAfterWriter{n: total}, m); err != nil {
		t.Fatalf("writer with enough capacity: %v", err)
	}
}

func TestGenerateConfig_NestedStructure(t *testing.T) {
	var buf bytes.Buffer
	if err := GenerateConfig(&buf, nestedModel()); err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	var cfg ServerConfig
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.LogicalDevices) != 1 || len(cfg.LogicalDevices[0].LogicalNodes) != 1 {
		t.Fatalf("unexpected shape: %+v", cfg)
	}
	ln := cfg.LogicalDevices[0].LogicalNodes[0]
	if len(ln.Objects) != 1 {
		t.Fatalf("objects = %+v", ln.Objects)
	}
	phv := ln.Objects[0]
	if phv.CDC != "WYE" || len(phv.Children) != 1 || len(phv.Attributes) != 1 {
		t.Fatalf("PhV = %+v", phv)
	}
	phsA := phv.Children[0]
	if phsA.Name != "phsA" || phsA.CDC != "CMV" || len(phsA.Attributes) != 1 {
		t.Fatalf("phsA = %+v", phsA)
	}
	cVal := phsA.Attributes[0]
	if cVal.Name != "cVal" || len(cVal.Children) != 2 {
		t.Fatalf("cVal = %+v", cVal)
	}
	mag := cVal.Children[0]
	if len(mag.Children) != 1 || mag.Children[0].Name != "f" || mag.Children[0].InitialValue != "1.5" {
		t.Errorf("mag = %+v", mag)
	}
	if len(ln.Reports) != 2 || !ln.Reports[1].Buffered || ln.Reports[1].BufTime != 50 || ln.Reports[1].IntgPd != 1000 {
		t.Errorf("reports = %+v", ln.Reports)
	}
	if len(ln.DataSets) != 1 || ln.DataSets[0].Members[0].DOPath != "PhV.phsA" {
		t.Errorf("datasets = %+v", ln.DataSets)
	}
}
