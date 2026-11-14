// SPDX-License-Identifier: MIT

package scl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

const limitsHead = `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"><Header id="x"/>`

// allocated returns the bytes allocated while f runs.
func allocated(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// A document of MaxBytes is parsed; one of MaxBytes+1 is refused with
// ErrLimitExceeded by every entry point, never read in part.
func TestParse_MaxBytesBoundary(t *testing.T) {
	doc := limitsHead + `<IED name="I1"/></SCL>`
	n := int64(len(doc))
	path := filepath.Join(t.TempDir(), "doc.scd")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := map[string]func(ParseOptions) (*Result, error){
		"ParseBytes":       func(o ParseOptions) (*Result, error) { return ParseBytes([]byte(doc), o) },
		"ParseWithOptions": func(o ParseOptions) (*Result, error) { return ParseWithOptions(strings.NewReader(doc), o) },
		"ParseFileOpts":    func(o ParseOptions) (*Result, error) { return ParseFileOpts(path, o) },
	}
	for name, parse := range entries {
		for _, tc := range []struct {
			max  int64
			want bool // refused
		}{
			{0, false}, {-1, false}, {n + 1, false}, {n, false}, {n - 1, true}, {1, true},
		} {
			res, err := parse(ParseOptions{MaxBytes: tc.max})
			switch {
			case tc.want && !errors.Is(err, ErrLimitExceeded):
				t.Errorf("%s MaxBytes %d (document %d bytes): err = %v, want ErrLimitExceeded", name, tc.max, n, err)
			case tc.want && res != nil:
				t.Errorf("%s MaxBytes %d: a result next to the error", name, tc.max)
			case !tc.want && (err != nil || len(res.Document.IEDs) != 1):
				t.Errorf("%s MaxBytes %d (document %d bytes): %v", name, tc.max, n, err)
			}
		}
	}
}

// endless yields a document that never ends.
type endless struct{ head bool }

func (e *endless) Read(p []byte) (int, error) {
	if !e.head {
		e.head = true
		return copy(p, limitsHead), nil
	}
	for i := range p {
		p[i] = "<IED/>"[i%6]
	}
	return len(p), nil
}

// A reader is read up to MaxBytes+1 bytes and no further.
func TestParseWithOptions_ReaderStopsAtMaxBytes(t *testing.T) {
	const max = 1 << 20
	var err error
	got := allocated(func() { _, err = ParseWithOptions(&endless{}, ParseOptions{MaxBytes: max}) })
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("err = %v, want ErrLimitExceeded", err)
	}
	if got > 8*max {
		t.Errorf("allocated %d bytes for a limit of %d", got, max)
	}
}

// A file larger than MaxBytes is refused before it is read.
func TestParseFileOpts_LargeFileRefusedBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.scd")
	big := limitsHead + strings.Repeat("<IED/>", 8<<20/6) + "</SCL>"
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	var err error
	got := allocated(func() { _, err = ParseFileOpts(path, ParseOptions{MaxBytes: 1 << 20}) })
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("err = %v, want ErrLimitExceeded", err)
	}
	if got > 64<<10 {
		t.Errorf("allocated %d bytes to refuse a file of %d bytes", got, len(big))
	}
}

// DetectFile reads a file up to its root element.
func TestDetectFile_ReadsUpToTheRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.scd")
	big := limitsHead + strings.Repeat("<IED/>", 8<<20/6) + "</SCL>"
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	var vi VersionInfo
	var err error
	got := allocated(func() { vi, err = DetectFile(path) })
	if err != nil || vi.Schema != Version2007B4 {
		t.Fatalf("DetectFile = %v, %v", vi.Schema, err)
	}
	if got > 256<<10 {
		t.Errorf("allocated %d bytes to detect a file of %d bytes", got, len(big))
	}
	for name, p := range map[string]string{"missing file": filepath.Join(t.TempDir(), "missing.scd"), "directory": t.TempDir()} {
		if _, err := DetectFile(p); err == nil || !strings.HasPrefix(err.Error(), "scl: read ") {
			t.Errorf("%s: %v", name, err)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty.scd")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DetectFile(empty); err == nil || err.Error() != "scl: no SCL root element found" {
		t.Errorf("empty file: %v", err)
	}
}

// nested returns a document whose deepest element is at depth depth,
// below a Private element (skipped content) or as nested SDI elements
// (bound content).
func nested(kind string, depth int) string {
	switch kind {
	case "private":
		// SCL 1, IED 2, Private 3, x 4..depth
		n := depth - 3
		return limitsHead + `<IED name="I"><Private type="p">` + strings.Repeat("<x>", n) + strings.Repeat("</x>", n) + `</Private></IED></SCL>`
	default:
		// SCL 1, IED 2, AccessPoint 3, Server 4, LDevice 5, LN0 6, DOI 7, SDI 8..depth
		n := depth - 7
		return limitsHead + `<IED name="I"><AccessPoint name="AP"><Server><LDevice inst="LD0"><LN0 lnClass="LLN0" inst="" lnType="T"><DOI name="D">` +
			strings.Repeat(`<SDI name="s">`, n) + strings.Repeat(`</SDI>`, n) + `</DOI></LN0></LDevice></Server></AccessPoint></IED></SCL>`
	}
}

// Elements at MaxDepth are parsed; one level more is refused, for bound
// and for skipped content.
func TestParse_MaxDepthBoundary(t *testing.T) {
	for _, kind := range []string{"private", "sdi"} {
		for _, tc := range []struct {
			max, depth int
			want       bool // refused
		}{
			{16, 15, false}, {16, 16, false}, {16, 17, true},
			{0, DefaultMaxDepth - 1, false}, {0, DefaultMaxDepth, false}, {0, DefaultMaxDepth + 1, true},
			{-1, DefaultMaxDepth + 1, true},
		} {
			_, err := ParseBytes([]byte(nested(kind, tc.depth)), ParseOptions{MaxDepth: tc.max})
			if got := errors.Is(err, ErrLimitExceeded); got != tc.want {
				t.Errorf("%s depth %d, MaxDepth %d: err = %v, want refused %v", kind, tc.depth, tc.max, err, tc.want)
			}
		}
	}
}

// A deep nesting of skipped elements stops at the depth limit, with an
// allocation far below the size of the document.
func TestParse_DeepSkippedContentAllocation(t *testing.T) {
	doc := []byte(nested("private", 300_000))
	var err error
	got := allocated(func() { _, err = ParseBytes(doc, ParseOptions{}) })
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("err = %v, want ErrLimitExceeded", err)
	}
	if got > uint64(len(doc)) {
		t.Errorf("allocated %d bytes for a document of %d bytes", got, len(doc))
	}
}

// lnsWithoutType returns a document with an IED of name nameLen bytes
// and lns logical nodes whose LNodeType does not exist.
func lnsWithoutType(nameLen, lns int) []byte {
	var b strings.Builder
	b.WriteString(limitsHead + `<IED name="` + strings.Repeat("A", nameLen) + `"><AccessPoint name="AP"><Server><LDevice inst="LD0"><LN0 lnClass="LLN0" inst="" lnType="none"/>`)
	for i := range lns {
		fmt.Fprintf(&b, `<LN lnClass="GGIO" inst="%d" lnType="none"/>`, i)
	}
	b.WriteString(`</LDevice></Server></AccessPoint></IED></SCL>`)
	return []byte(b.String())
}

// With MaxDiagnostics, parsing and validation stop adding diagnostics
// once the list is full, and quoted names are cut: the allocation stays
// a small multiple of the document.
func TestParse_DiagnosticsBoundedByInput(t *testing.T) {
	doc := lnsWithoutType(100_000, 1000)
	for _, max := range []int{10, 0} {
		var res *Result
		var err error
		got := allocated(func() { res, err = ParseBytes(doc, ParseOptions{ValidateSemantic: true, MaxDiagnostics: max}) })
		if err != nil {
			t.Fatal(err)
		}
		if max > 0 && len(res.Diagnostics) != max {
			t.Errorf("MaxDiagnostics %d: %d diagnostics", max, len(res.Diagnostics))
		}
		if max == 0 && len(res.Diagnostics) < 1001 {
			t.Errorf("MaxDiagnostics 0: %d diagnostics, want every one", len(res.Diagnostics))
		}
		for _, d := range res.Diagnostics {
			if len(d.Path) > 4*DefaultMaxDiagnosticName || len(d.Message) > 4*DefaultMaxDiagnosticName {
				t.Fatalf("diagnostic of %d+%d bytes: %.80s", len(d.Path), len(d.Message), d.Path)
			}
		}
		if budget := uint64(64 * len(doc)); got > budget {
			t.Errorf("MaxDiagnostics %d: allocated %d bytes for a document of %d bytes, budget %d", max, got, len(doc), budget)
		}
	}
}

// MaxDiagnostics keeps the first diagnostics, in the order they are
// found, and Strict counts the errors among them: 0, 1, max-1, max and
// max+1 diagnostics found.
func TestParse_MaxDiagnosticsBoundary(t *testing.T) {
	const max = 4
	for _, lns := range []int{0, 1, max - 2, max - 1, max, max + 1} {
		doc := lnsWithoutType(4, lns) // LN0 and lns LNs without a type
		all, err := ParseBytes(doc, ParseOptions{ValidateSemantic: true})
		if err != nil {
			t.Fatal(err)
		}
		res, err := ParseBytes(doc, ParseOptions{ValidateSemantic: true, MaxDiagnostics: max})
		if err != nil {
			t.Fatal(err)
		}
		want := all.Diagnostics
		if len(want) > max {
			want = want[:max]
		}
		if fmt.Sprint(res.Diagnostics) != fmt.Sprint(want) {
			t.Errorf("%d LNs: %v, want %v", lns, res.Diagnostics, want)
		}
		_, err = ParseBytes(doc, ParseOptions{ValidateSemantic: true, MaxDiagnostics: max, Strict: true})
		if wantErr := fmt.Sprintf("scl: strict mode: %d error(s)", len(want)); err == nil || err.Error() != wantErr {
			t.Errorf("%d LNs, Strict: %v, want %q", lns, err, wantErr)
		}
	}
}

// A name quoted in a diagnostic is kept up to MaxDiagnosticName bytes
// and cut beyond it.
func TestParse_DiagnosticNameBoundary(t *testing.T) {
	for _, tc := range []struct {
		cap, nameLen int
		cut          bool
	}{
		{0, DefaultMaxDiagnosticName - 1, false},
		{0, DefaultMaxDiagnosticName, false},
		{0, DefaultMaxDiagnosticName + 1, true},
		{8, 8, false},
		{8, 9, true},
		{1, 2, true},
	} {
		res, err := ParseBytes(lnsWithoutType(tc.nameLen, 0), ParseOptions{ValidateSemantic: true, MaxDiagnosticName: tc.cap})
		if err != nil {
			t.Fatal(err)
		}
		cap := tc.cap
		if cap == 0 {
			cap = DefaultMaxDiagnosticName
		}
		want := strings.Repeat("A", min(tc.nameLen, cap))
		if tc.cut {
			want += "…"
		}
		var found bool
		for _, d := range res.Diagnostics {
			if d.Code == "missing-lnodetype" {
				found = true
				if !strings.HasPrefix(d.Path, "IED["+want+"]/") {
					t.Errorf("name %d bytes, cap %d: path %.40s…, want the name as %d bytes", tc.nameLen, tc.cap, d.Path, len(want))
				}
			}
		}
		if !found {
			t.Errorf("name %d bytes: no missing-lnodetype diagnostic in %v", tc.nameLen, res.Diagnostics)
		}
	}
}

// capName cuts at a character boundary.
func TestCapName(t *testing.T) {
	for _, tc := range []struct {
		in   string
		max  int
		want string
	}{
		{"", 3, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc…"},
		{"aé", 2, "a…"}, // é is two bytes: the cut moves before it
		{"aé", 3, "aé"},
		{"ééé", 4, "éé…"},
		{"é", 1, "…"},
	} {
		got := capName(tc.in, tc.max)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("capName(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

// An invalid count attribute is quoted cut, in the path and in the
// message, and the parse error keeps its reason.
func TestParse_InvalidCountQuotedCut(t *testing.T) {
	long := strings.Repeat("9x", 1000)
	doc := limitsHead + `<DataTypeTemplates><DOType id="` + strings.Repeat("T", 1000) + `" cdc="X"><DA name="a" fc="ST" bType="BOOLEAN" count="` + long + `"/></DOType>` +
		`<DAType id="S"><BDA name="b" bType="BOOLEAN" count="` + long + `"/></DAType></DataTypeTemplates></SCL>`
	for _, root := range []string{
		`version="2007" revision="B" release="4"`,
		`version="2007" revision="C" release="5"`,
		`version="2007" revision="B"`,
	} {
		res, err := ParseBytes([]byte(strings.Replace(doc, `version="2007" revision="B" release="4"`, root, 1)), ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Diagnostics) != 2 {
			t.Fatalf("%s: diagnostics %v", root, res.Diagnostics)
		}
		for _, d := range res.Diagnostics {
			if d.Code != "invalid-count" || len(d.Path) > 2*DefaultMaxDiagnosticName+32 || len(d.Message) > 3*DefaultMaxDiagnosticName ||
				!strings.Contains(d.Message, "invalid syntax") {
				t.Errorf("%s: %s (%d bytes): %.120s", root, d.Code, len(d.Path)+len(d.Message), d.Message)
			}
		}
	}
}

// A document declared in an encoding label the parse accepts has its
// Header found: semantic validation does not report it missing.
func TestParse_EncodingLabelsHeader(t *testing.T) {
	body := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"><Header id="t"/></SCL>`
	for _, enc := range []string{"utf8", "UTF8", "UTF-8", "us-ascii", "US-ASCII", "ASCII"} {
		res, err := ParseBytes([]byte(`<?xml version="1.0" encoding="`+enc+`"?>`+body), ParseOptions{ValidateSemantic: true, Strict: true})
		if err != nil {
			t.Errorf("encoding %q: %v (%v)", enc, err, res.Diagnostics)
			continue
		}
		if !res.Document.Metadata.HasHeader {
			t.Errorf("encoding %q: HasHeader false", enc)
		}
	}
}

// fanout returns a document model whose single LN0 expands its DO
// through a chain of depth DATypes, each with k BDAs of the next type:
// 2 + k + k² + … + k^depth nodes and k^depth rows.
func fanout(depth, k int) (*SCL, int) {
	s := &SCL{
		IEDs: []IED{{Name: "I", AccessPoints: []AccessPoint{{Name: "AP", Server: &Server{LDevices: []LDevice{{
			Inst: "LD0", LN0: &LN{LNClass: "LLN0", LNType: "T0"},
		}}}}}}},
	}
	t := &s.DataTypeTemplates
	t.LNodeTypes = []LNodeType{{ID: "T0", LNClass: "LLN0", DOs: []DO{{Name: "D", Type: "DOT"}}}}
	t.DOTypes = []DOType{{ID: "DOT", CDC: "X", DAs: []DA{{Name: "a", FC: "ST", BType: "Struct", Type: "S0"}}}}
	nodes, level := 2, 1
	for i := range depth {
		dat := DAType{ID: fmt.Sprintf("S%d", i)}
		for j := range k {
			if i == depth-1 {
				dat.BDAs = append(dat.BDAs, BDA{Name: fmt.Sprintf("b%d", j), BType: "BOOLEAN"})
			} else {
				dat.BDAs = append(dat.BDAs, BDA{Name: fmt.Sprintf("b%d", j), BType: "Struct", Type: fmt.Sprintf("S%d", i+1)})
			}
		}
		t.DATypes = append(t.DATypes, dat)
		level *= k
		nodes += level
	}
	return s, nodes
}

// The expansion produces MaxExpandedNodes nodes and stops at the next
// one with a diagnostic and an "expansion-limit" row.
func TestFlatten_MaxExpandedNodesBoundary(t *testing.T) {
	s, n := fanout(6, 3)
	leaves := 729
	for _, tc := range []struct {
		max     int
		stopped bool
	}{
		{0, false}, {-1, false}, {n + 1, false}, {n, false}, {n - 1, true}, {2, true}, {1, true},
	} {
		rows, diags := FlattenWithOptions(s, FlattenOptions{MaxExpandedNodes: tc.max})
		if !tc.stopped {
			if len(diags) != 0 || len(rows) != leaves || rows[len(rows)-1].Status != "" {
				t.Errorf("limit %d (%d nodes): %d rows, diagnostics %v", tc.max, n, len(rows), diags)
			}
			continue
		}
		if len(diags) != 1 || diags[0].Code != "expansion-limit" || diags[0].Severity != DiagError ||
			diags[0].Path != "IED[I]/LD[LD0]/LN[LLN0]" || !strings.Contains(diags[0].Message, ErrLimitExceeded.Error()) {
			t.Errorf("limit %d: diagnostics %v", tc.max, diags)
		}
		if len(rows) == 0 || len(rows) > tc.max+1 || rows[len(rows)-1].Status != "expansion-limit" {
			t.Errorf("limit %d: %d rows, last %+v", tc.max, len(rows), rows[len(rows)-1])
		}
	}
	if got := Flatten(s); len(got) != leaves {
		t.Errorf("Flatten: %d rows, want %d", len(got), leaves)
	}
}

// Every kind of row counts as a node: an unresolved LNodeType, DOType or
// DAType, a type cycle; the expansion stops on each kind of node.
func TestFlatten_LimitOnEveryNodeKind(t *testing.T) {
	s := &SCL{IEDs: []IED{{Name: "I", AccessPoints: []AccessPoint{{Name: "AP", Server: &Server{LDevices: []LDevice{{
		Inst: "LD0", LN0: &LN{LNClass: "LLN0", LNType: "T0"},
		LNs: []LN{{LNClass: "GGIO", Inst: "1", LNType: "missing"}, {LNClass: "GGIO", Inst: "2", LNType: "T0"}},
	}}}}}}}}
	t0 := &s.DataTypeTemplates
	t0.LNodeTypes = []LNodeType{{ID: "T0", LNClass: "LLN0", DOs: []DO{{Name: "D", Type: "DOT"}, {Name: "M", Type: "missing"}}}}
	t0.DOTypes = []DOType{{ID: "DOT", CDC: "X",
		DAs:  []DA{{Name: "a", FC: "ST", BType: "Struct", Type: "S"}, {Name: "u", FC: "ST", BType: "Struct", Type: "missing"}},
		SDOs: []SDO{{Name: "self", Type: "DOT"}, {Name: "gone", Type: "missing"}, {Name: "sub", Type: "SUB"}},
	}, {ID: "SUB", CDC: "Y", DAs: []DA{{Name: "v", FC: "MX", BType: "INT32"}}}}
	t0.DATypes = []DAType{{ID: "S", BDAs: []BDA{{Name: "loop", BType: "Struct", Type: "S"}, {Name: "x", BType: "Struct", Type: "missing"}, {Name: "y", BType: "BOOLEAN"}}}}

	full, diags := FlattenWithOptions(s, FlattenOptions{})
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	if fmt.Sprint(full) != fmt.Sprint(Flatten(s)) {
		t.Fatal("Flatten and FlattenWithOptions differ")
	}
	// Find the node count: the smallest limit without a diagnostic.
	n := 1
	for ; ; n++ {
		if _, d := FlattenWithOptions(s, FlattenOptions{MaxExpandedNodes: n}); len(d) == 0 {
			break
		}
	}
	for limit := 1; limit < n; limit++ {
		rows, d := FlattenWithOptions(s, FlattenOptions{MaxExpandedNodes: limit})
		if len(d) != 1 || rows[len(rows)-1].Status != "expansion-limit" {
			t.Fatalf("limit %d: %v", limit, d)
		}
		// The rows before the stop are the rows of the full expansion.
		if got, want := fmt.Sprint(rows[:len(rows)-1]), fmt.Sprint(full[:len(rows)-1]); got != want {
			t.Errorf("limit %d: rows %s, want %s", limit, got, want)
		}
	}
}

// An exponential expansion stops at the limit with an allocation
// proportional to the limit.
func TestFlatten_ExpansionAllocation(t *testing.T) {
	s, n := fanout(20, 2)
	const limit = 10_000
	var rows []FlatRow
	var diags []Diagnostic
	got := allocated(func() { rows, diags = FlattenWithOptions(s, FlattenOptions{MaxExpandedNodes: limit}) })
	if len(diags) != 1 || len(rows) > limit+1 {
		t.Fatalf("%d nodes, limit %d: %d rows, diagnostics %v", n, limit, len(rows), diags)
	}
	if budget := uint64(limit * 1024); got > budget {
		t.Errorf("allocated %d bytes, budget %d", got, budget)
	}
}

var _ io.Reader = (*endless)(nil)

// A document with findings in every validation pass: for every
// MaxDiagnostics, the diagnostics are the first ones of the full list,
// and a negative MaxDiagnostics keeps them all.
func TestParse_MaxDiagnosticsEveryPass(t *testing.T) {
	doc := []byte(limitsHead + `
<Substation name="S"><LNode iedName="NOIED" ldInst="LD0" lnClass="XCBR" lnInst="1"/>
 <VoltageLevel name="V"><LNode iedName="I" ldInst="NOLD" lnClass="XCBR" lnInst="1"/>
  <Bay name="B"><LNode iedName="NOIED" lnClass="XCBR" lnInst="2"/><LNode iedName="None" lnClass="XCBR"/></Bay></VoltageLevel></Substation>
<Communication><SubNetwork name="N">
 <ConnectedAP iedName="NOIED" apName="AP"/>
 <ConnectedAP iedName="I" apName="NOAP"><GSE ldInst="NOLD" cbName="g"/><SMV ldInst="NOLD" cbName="s"/></ConnectedAP></SubNetwork></Communication>
<IED name="I"><AccessPoint name="AP"><Server><LDevice inst="LD0"><LN0 lnClass="LLN0" inst="" lnType="none">
 <ReportControl name="r" datSet="NODS"/><GSEControl name="g" appID="a" datSet="NODS"/><SampledValueControl name="s" smvID="v" datSet="NODS"/></LN0>
 <LN lnClass="GGIO" inst="1" lnType="T0"/></LDevice><LDevice inst="LD0"/></Server></AccessPoint><AccessPoint name="AP"/></IED>
<IED name="I"/>
<DataTypeTemplates>
 <LNodeType id="T0" lnClass="LLN0"><DO name="D" type="NODOT"/></LNodeType><LNodeType id="T0" lnClass="LLN0"/>
 <DOType id="DOT" cdc="X"><DA name="a" fc="ST" bType="Struct" type="NODAT"/><DA name="e" fc="ST" bType="Enum" type="NOENUM"/><SDO name="s" type="NODOT"/></DOType><DOType id="DOT" cdc="X"/>
 <DAType id="DAT"><BDA name="b" bType="Struct" type="NODAT"/></DAType><DAType id="DAT"/>
 <EnumType id="E"/><EnumType id="E"/>
</DataTypeTemplates></SCL>`)
	all, err := ParseBytes(doc, ParseOptions{ValidateSemantic: true, Kind: KindSSD})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, d := range all.Diagnostics {
		codes[d.Code] = true
	}
	for _, c := range []string{"duplicate-id", "duplicate-ied", "missing-dotype", "missing-datype", "missing-enumtype", "duplicate-access-point",
		"duplicate-ld", "missing-lnodetype", "missing-dataset", "missing-connected-ap", "missing-ld", "unresolved-topology-lnode", "missing-attribute"} {
		if !codes[c] {
			t.Errorf("no %s diagnostic in %v", c, all.Diagnostics)
		}
	}
	if neg, err := ParseBytes(doc, ParseOptions{ValidateSemantic: true, Kind: KindSSD, MaxDiagnostics: -1}); err != nil || len(neg.Diagnostics) != len(all.Diagnostics) {
		t.Errorf("MaxDiagnostics -1: %d diagnostics, want %d (%v)", len(neg.Diagnostics), len(all.Diagnostics), err)
	}
	for max := 1; max <= len(all.Diagnostics)+1; max++ {
		res, err := ParseBytes(doc, ParseOptions{ValidateSemantic: true, Kind: KindSSD, MaxDiagnostics: max})
		if err != nil {
			t.Fatal(err)
		}
		want := all.Diagnostics[:min(max, len(all.Diagnostics))]
		if fmt.Sprint(res.Diagnostics) != fmt.Sprint(want) {
			t.Fatalf("MaxDiagnostics %d: %v, want %v", max, res.Diagnostics, want)
		}
	}
	if fmt.Sprint(Validate(all.Document)) == "" || len(ValidateKind(all.Document, KindSSD)) != 0 {
		t.Error("Validate and ValidateKind without a sink")
	}
}
