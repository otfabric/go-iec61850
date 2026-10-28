// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout while fn runs and returns what was
// written. The pipe is drained concurrently so large outputs cannot block.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	os.Stdout = old
	out := <-done
	_ = r.Close()
	return out
}

// runCmd executes a sub-command and returns its stdout and error.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = executeCmd(args...) })
	return out, err
}

// richSCL exercises the branches the stock fixtures miss: vendor namespaces,
// typed and untyped Private elements at every level, a client-only access
// point (no Server), DataSets/ReportControls, and ConnectedAP GSE/SMV entries
// pointing at a non-existent LDevice (validation warnings).
const richSCL = `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" xmlns:acme="http://acme.example/scl" version="2007" revision="B">
  <Header id="Rich"/>
  <Communication>
    <SubNetwork name="SN1">
      <ConnectedAP iedName="IED1" apName="S1">
        <GSE ldInst="NOLD" cbName="gcb1"/>
        <SMV ldInst="NOLD" cbName="sv1"/>
      </ConnectedAP>
    </SubNetwork>
  </Communication>
  <IED name="IED1" manufacturer="Acme">
    <Private type="acme:ied">x</Private>
    <AccessPoint name="S1">
      <Private>untyped</Private>
      <Server>
        <LDevice inst="LD1">
          <Private type="acme:ld"/>
          <LN0 lnClass="LLN0" inst="" lnType="LLN0_T">
            <Private type="acme:ld"/>
            <DataSet name="ds1">
              <FCDA ldInst="LD1" prefix="P" lnClass="GGIO" lnInst="1" doName="Ind" fc="ST"/>
            </DataSet>
            <ReportControl name="rcb1" datSet="ds1" confRev="1" buffered="true"/>
          </LN0>
          <LN prefix="P" lnClass="GGIO" inst="1" lnType="GGIO_T" desc="general io">
            <Private type="acme:ln"/>
          </LN>
        </LDevice>
      </Server>
    </AccessPoint>
    <AccessPoint name="Client"/>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0_T" lnClass="LLN0"/>
    <LNodeType id="GGIO_T" lnClass="GGIO"/>
  </DataTypeTemplates>
</SCL>`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func wantExitCode(t *testing.T, err error, code int) {
	t.Helper()
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *exitError, got %T (%v)", err, err)
	}
	if ee.code != code {
		t.Errorf("exit code = %d, want %d (%s)", ee.code, code, ee.msg)
	}
}

func TestAllCommands_MissingFileIsParseFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.scd")
	for _, cmd := range []string{
		"detect", "summary", "validate", "dump-json", "list-ieds", "list-lns",
		"list-datasets", "list-reports", "list-goose", "list-smv",
		"list-connected-ap", "list-types", "inspect",
	} {
		t.Run(cmd, func(t *testing.T) {
			_, err := runCmd(t, cmd, missing)
			wantExitCode(t, err, exitParseFail)
		})
	}
}

func TestDetect_NotSCL(t *testing.T) {
	p := writeTemp(t, "x.xml", `<?xml version="1.0"?><Other/>`)
	_, err := runCmd(t, "detect", p)
	wantExitCode(t, err, exitParseFail)
}

func TestDetect_UnknownVersionTextAndQuiet(t *testing.T) {
	p := writeTemp(t, "future.scd",
		`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" xmlns:acme="http://acme.example/scl" version="2099" revision="Z"/>`)

	out, err := runCmd(t, "detect", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Schema:     unknown",
		"Declared:   version=2099 revision=Z release=",
		"  - unsupported tuple",
		"Vendor NS:  [http://acme.example/scl]",
		"Kind:       scd",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detect output missing %q:\n%s", want, out)
		}
	}

	out, err = runCmd(t, "detect", "--quiet", p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "unknown" {
		t.Errorf("quiet output = %q, want unknown", out)
	}
}

func TestListLNs_TableAndJSON(t *testing.T) {
	p := writeTemp(t, "rich.scd", richSCL)

	out, err := runCmd(t, "list-lns", p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d lines:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "IED") || !strings.Contains(lines[0], "LNTYPE") {
		t.Errorf("bad header: %q", lines[0])
	}
	if !strings.Contains(lines[1], "LLN0") || !strings.Contains(lines[2], "PGGIO1") ||
		!strings.Contains(lines[2], "general io") {
		t.Errorf("unexpected rows:\n%s", out)
	}

	out, err = runCmd(t, "list-lns", "--json", p)
	if err != nil {
		t.Fatal(err)
	}
	var rows []lnInfo
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	want := []lnInfo{
		{IED: "IED1", AP: "S1", LD: "LD1", LN: "LLN0", LNClass: "LLN0", LNType: "LLN0_T"},
		{IED: "IED1", AP: "S1", LD: "LD1", LN: "PGGIO1", LNClass: "GGIO", LNType: "GGIO_T", Desc: "general io"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

func TestInspect_PrivatesAndVendorNamespaces(t *testing.T) {
	p := writeTemp(t, "rich.cid", richSCL)

	out, err := runCmd(t, "inspect", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Kind (ext):      cid",
		"Vendor namespaces:\n  http://acme.example/scl",
		"Private elements: 5",
		"(untyped)",
		"acme:ld",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("inspect output missing %q:\n%s", want, out)
		}
	}

	out, err = runCmd(t, "inspect", "--json", p)
	if err != nil {
		t.Fatal(err)
	}
	var res inspectResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	wantTypes := map[string]int{"acme:ied": 1, "(untyped)": 1, "acme:ld": 2, "acme:ln": 1}
	if res.PrivateCount != 5 {
		t.Errorf("PrivateCount = %d, want 5", res.PrivateCount)
	}
	for k, v := range wantTypes {
		if res.PrivateTypes[k] != v {
			t.Errorf("PrivateTypes[%q] = %d, want %d (all=%v)", k, res.PrivateTypes[k], v, res.PrivateTypes)
		}
	}
}

func TestInspect_DetectOKButParseFails(t *testing.T) {
	// The root element is well-formed enough for detection but the
	// document as a whole is truncated.
	p := writeTemp(t, "trunc.scd",
		`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B"><IED name="x">`)
	_, err := runCmd(t, "inspect", p)
	wantExitCode(t, err, exitParseFail)
}

func TestValidate_WarningsFlags(t *testing.T) {
	p := writeTemp(t, "rich.scd", richSCL)

	t.Run("table shows warnings", func(t *testing.T) {
		out, err := runCmd(t, "validate", p)
		if err != nil {
			t.Fatalf("warnings alone must not fail: %v", err)
		}
		if !strings.Contains(out, "warning") || !strings.Contains(out, "GSE[gcb1]") ||
			!strings.Contains(out, "0 error(s), 2 warning(s)") {
			t.Errorf("unexpected output:\n%s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		out, err := runCmd(t, "validate", "--json", p)
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			File     string `json:"file"`
			Errors   int    `json:"errors"`
			Warnings int    `json:"warnings"`
			Findings []struct {
				Severity string `json:"severity"`
				Code     string `json:"code"`
			} `json:"findings"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		if res.File != "rich.scd" || res.Errors != 0 || res.Warnings != 2 || len(res.Findings) != 2 {
			t.Errorf("unexpected result: %+v", res)
		}
		for _, f := range res.Findings {
			if f.Severity != "warning" || f.Code != "missing-ld" {
				t.Errorf("finding = %+v", f)
			}
		}
	})

	t.Run("no-warnings", func(t *testing.T) {
		out, err := runCmd(t, "validate", "--no-warnings", p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "rich.scd: no issues found") {
			t.Errorf("unexpected output:\n%s", out)
		}
	})

	t.Run("warnings-as-errors", func(t *testing.T) {
		out, err := runCmd(t, "validate", "--warnings-as-errors", p)
		wantExitCode(t, err, exitValidation)
		if !strings.Contains(out, "2 error(s), 0 warning(s)") {
			t.Errorf("unexpected output:\n%s", out)
		}
	})

	t.Run("max-errors", func(t *testing.T) {
		out, err := runCmd(t, "validate", "--warnings-as-errors", "--max-errors", "1", p)
		wantExitCode(t, err, exitValidation)
		if !strings.Contains(out, "1 error(s), 0 warning(s)") {
			t.Errorf("max-errors not honoured:\n%s", out)
		}
	})
}

func TestListCommands_JSONOutputs(t *testing.T) {
	p := writeTemp(t, "rich.scd", richSCL)
	tests := []struct {
		cmd  string
		want string
	}{
		{"list-datasets", `"ds1"`},
		{"list-reports", `"rcb1"`},
		{"list-connected-ap", `"SN1"`},
		{"list-types", `"GGIO_T"`},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			out, err := runCmd(t, tt.cmd, "--json", p)
			if err != nil {
				t.Fatal(err)
			}
			var rows []map[string]any
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, out)
			}
			if len(rows) == 0 || !strings.Contains(out, tt.want) {
				t.Errorf("output missing %s:\n%s", tt.want, out)
			}
		})
	}
}

func TestListIEDs_ClientOnlyAccessPoint(t *testing.T) {
	p := writeTemp(t, "rich.scd", richSCL)
	out, err := runCmd(t, "list-ieds", "--json", p)
	if err != nil {
		t.Fatal(err)
	}
	var rows []iedInfo
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].APs != 2 || rows[0].LDs != 1 || rows[0].LNs != 2 {
		t.Errorf("rows = %+v, want 1 IED with 2 APs, 1 LD, 2 LNs", rows)
	}
}

func TestSummary_PrettyAndPrivates(t *testing.T) {
	p := writeTemp(t, "rich.scd", richSCL)

	out, err := runCmd(t, "summary", "--json", "--pretty", p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n  \"file\": \"rich.scd\"") {
		t.Errorf("expected indented JSON:\n%s", out)
	}

	out, err = runCmd(t, "summary", p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Private elems:") {
		t.Errorf("summary missing private count:\n%s", out)
	}
}

func TestDumpJSON_OutputCreateFails(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no-such-dir", "out.json")
	_, err := runCmd(t, "dump-json", "--output", bad, fixturePath("scl/testdata/simple.scd"))
	wantExitCode(t, err, exitParseFail)
}
