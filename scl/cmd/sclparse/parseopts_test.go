// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const bareSCLFile = `<?xml version="1.0" encoding="UTF-8"?>
<SCL>
  <Header id="bare"/>
  <IED name="Demo">
    <AccessPoint name="AP1">
      <Server>
        <Authentication/>
        <LDevice inst="LD0">
          <LN0 lnClass="LLN0" inst="" lnType="LLN0_T"/>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0_T" lnClass="LLN0"/>
  </DataTypeTemplates>
</SCL>`

func TestAssumeNamespaceFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bare.icd")
	if err := os.WriteFile(path, []byte(bareSCLFile), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, sub := range [][]string{
		{"validate"}, {"summary"}, {"dump-json"}, {"inspect"},
		{"list-ieds"}, {"list-lns"}, {"list-datasets"}, {"list-reports"},
		{"list-goose"}, {"list-smv"}, {"list-connected-ap"}, {"list-types"},
	} {
		t.Run(sub[0], func(t *testing.T) {
			code, _ := runWithArgs(t, append(sub, path)...)
			if code != ExitInvalidInput {
				t.Errorf("without --assume-namespace: exit %d, want %d", code, ExitInvalidInput)
			}
			code, out := runWithArgs(t, append(append(sub, "--assume-namespace"), path)...)
			if code != ExitOK {
				t.Errorf("with --assume-namespace: exit %d, want %d (%s)", code, ExitOK, out)
			}
		})
	}

	out := captureStdout(t, func() {
		if err := executeCmd("validate", "--json", "--assume-namespace", path); err != nil {
			t.Errorf("validate: %v", err)
		}
	})
	if !strings.Contains(out, "namespace-assumed") {
		t.Errorf("validate output lacks the namespace-assumed warning: %s", out)
	}
}
