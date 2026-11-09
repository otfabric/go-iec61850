// SPDX-License-Identifier: MIT

package scl_test

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

const bareSCL = `<?xml version="1.0" encoding="UTF-8"?>
<SCL%s>
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

func countCode(r *scl.Result, code string) int {
	n := 0
	for _, d := range r.Diagnostics {
		if d.Code == code {
			n++
		}
	}
	return n
}

func TestAssumeSCLNamespace(t *testing.T) {
	bare := []byte(strings.Replace(bareSCL, "%s", "", 1))

	if _, err := scl.ParseBytes(bare, scl.ParseOptions{}); err == nil {
		t.Fatal("SCL root without namespace parsed by default; the schema requires the namespace")
	}

	r, err := scl.ParseBytes(bare, scl.ParseOptions{AssumeSCLNamespace: true})
	if err != nil {
		t.Fatalf("ParseBytes with AssumeSCLNamespace: %v", err)
	}
	if r.Version.Schema != scl.Version17 {
		t.Errorf("schema = %q, want %q", r.Version.Schema, scl.Version17)
	}
	if got := countCode(r, "namespace-assumed"); got != 1 {
		t.Errorf("namespace-assumed diagnostics = %d, want 1 (%v)", got, r.Diagnostics)
	}
	if r.Document.Metadata.OriginalNamespace != "" {
		t.Errorf("OriginalNamespace = %q, want empty", r.Document.Metadata.OriginalNamespace)
	}
	if !r.Document.Metadata.HasHeader {
		t.Error("HasHeader = false")
	}
	if len(r.Document.IEDs) != 1 || r.Document.IEDs[0].Name != "Demo" {
		t.Fatalf("IEDs = %+v, want one IED Demo", r.Document.IEDs)
	}
	if len(r.Document.DataTypeTemplates.LNodeTypes) != 1 {
		t.Errorf("LNodeTypes = %d, want 1", len(r.Document.DataTypeTemplates.LNodeTypes))
	}
}

func TestAssumeSCLNamespaceVersioned(t *testing.T) {
	data := []byte(strings.Replace(bareSCL, "%s", ` version="2007" revision="B" release="4"`, 1))
	r, err := scl.ParseBytes(data, scl.ParseOptions{AssumeSCLNamespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version.Schema != scl.Version2007B4 {
		t.Errorf("schema = %q, want %q", r.Version.Schema, scl.Version2007B4)
	}
	if len(r.Document.IEDs) != 1 {
		t.Errorf("IEDs = %d, want 1", len(r.Document.IEDs))
	}
}

func TestAssumeSCLNamespaceOtherNamespaceRefused(t *testing.T) {
	data := []byte(strings.Replace(bareSCL, "%s", ` xmlns="urn:example:not-scl"`, 1))
	if _, err := scl.ParseBytes(data, scl.ParseOptions{AssumeSCLNamespace: true}); err == nil {
		t.Fatal("SCL root in a foreign namespace parsed")
	}
}

func TestAssumeSCLNamespaceNoEffectWithNamespace(t *testing.T) {
	data := []byte(strings.Replace(bareSCL, "%s", ` xmlns="http://www.iec.ch/61850/2003/SCL"`, 1))
	r, err := scl.ParseBytes(data, scl.ParseOptions{AssumeSCLNamespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := countCode(r, "namespace-assumed"); got != 0 {
		t.Errorf("namespace-assumed diagnostics = %d, want 0", got)
	}
	if r.Document.Metadata.OriginalNamespace != "http://www.iec.ch/61850/2003/SCL" {
		t.Errorf("OriginalNamespace = %q", r.Document.Metadata.OriginalNamespace)
	}
}
