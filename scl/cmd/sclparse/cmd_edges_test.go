// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestListSMV_TableAndJSON(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="SMVTest"/>
  <IED name="IED1">
    <AccessPoint name="S1">
      <Server>
        <LDevice inst="LD1">
          <LN0 lnClass="LLN0" inst="" lnType="T">
            <SampledValueControl name="smv01" smvID="SV1" datSet="ds1" confRev="1" smpRate="4800" nofASDU="1" multicast="true"/>
            <SampledValueControl name="smv02" smvID="SV2" confRev="2" smpRate="4000" nofASDU="2" multicast="false"/>
          </LN0>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="T" lnClass="LLN0"/>
  </DataTypeTemplates>
</SCL>`
	path := filepath.Join(t.TempDir(), "smv.scd")
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("table", func(t *testing.T) {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := executeCmd("list-smv", path)

		_ = w.Close()
		os.Stdout = old
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		out := string(buf[:n])
		if !strings.Contains(out, "smv01") || !strings.Contains(out, "SV1") {
			t.Errorf("table missing smv01: %s", out)
		}
		if !strings.Contains(out, "Y") || !strings.Contains(out, "N") {
			t.Errorf("expected multicast Y/N markers: %s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := executeCmd("list-smv", "--json", path)

		_ = w.Close()
		os.Stdout = old
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var rows []map[string]any
		if err := json.NewDecoder(r).Decode(&rows); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("got %d rows, want 2", len(rows))
		}
		if rows[0]["name"] != "smv01" {
			t.Errorf("name = %v", rows[0]["name"])
		}
	})
}

func TestListSMV_ParseFail(t *testing.T) {
	err := executeCmd("list-smv", filepath.Join(t.TempDir(), "missing.scd"))
	if err == nil {
		t.Fatal("expected parse failure")
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected exitError, got %T: %v", err, err)
	}
	if ee.code != exitParseFail {
		t.Errorf("code = %d, want %d", ee.code, exitParseFail)
	}
	if ee.Error() == "" {
		t.Error("Error() empty")
	}
}

func TestVersionString(t *testing.T) {
	origV, origTag, origCommit, origDate := version, tag, commit, buildDate
	t.Cleanup(func() {
		version, tag, commit, buildDate = origV, origTag, origCommit, origDate
	})

	version, tag, commit, buildDate = "1.2.3", "", "", ""
	if got := versionString("sclparse"); got != "sclparse 1.2.3" {
		t.Errorf("basic = %q", got)
	}

	tag, commit, buildDate = "v1.2.3", "abcdef0123456789", "2026-01-01"
	got := versionString("sclparse")
	if !strings.Contains(got, "(v1.2.3)") {
		t.Errorf("missing tag: %q", got)
	}
	if !strings.Contains(got, "commit abcdef01") {
		t.Errorf("missing short commit: %q", got)
	}
	if !strings.Contains(got, "built 2026-01-01") {
		t.Errorf("missing build date: %q", got)
	}

	commit = "abcd"
	got = versionString("tool")
	if !strings.Contains(got, "commit abcd") {
		t.Errorf("short commit unchanged: %q", got)
	}
}

func TestExitError_Error(t *testing.T) {
	ee := &exitError{code: 2, msg: "boom"}
	if ee.Error() != "boom" {
		t.Errorf("Error() = %q", ee.Error())
	}
}

func TestSCLFileCompletion(t *testing.T) {
	exts, dir := sclFileCompletion(nil, nil, "")
	if dir != cobra.ShellCompDirectiveFilterFileExt {
		t.Fatalf("directive = %v", dir)
	}
	if len(exts) < 3 {
		t.Fatalf("exts = %v", exts)
	}
}
