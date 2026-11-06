// SPDX-License-Identifier: MIT

package servermodel

import (
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// twoLDSCL is testSCL with a second logical device LD2, whose data set
// dsCross has a member in LD1 (FCDA ldInst) and one in its own LD.
func twoLDSCL(ldName2 string) *scl.SCL {
	s := testSCL()
	srv := s.IEDs[0].AccessPoints[0].Server
	ld2 := scl.LDevice{
		Inst:   "LD2",
		LdName: ldName2,
		LN0: &scl.LN{
			LNClass: "LLN0",
			LNType:  "LNT_LLN0",
			DataSets: []scl.DataSet{{
				Name: "dsCross",
				FCDAs: []scl.FCDA{
					{LDInst: "LD1", LNClass: "GGIO", LNInst: "1", DOName: "Ind1", DAName: "stVal", FC: "ST"},
					{LDInst: "LD2", LNClass: "LLN0", DOName: "Mod", DAName: "stVal", FC: "ST"},
					{LNClass: "LLN0", DOName: "Health", DAName: "stVal", FC: "ST"},
				},
			}},
		},
	}
	srv.LDevices = append(srv.LDevices, ld2)
	return s
}

func ldNames(m *Model) []string {
	var out []string
	for _, ld := range m.LogicalDevices {
		out = append(out, ld.Name+"("+ld.Inst+")")
	}
	return out
}

func crossMembers(t *testing.T, m *Model) []string {
	t.Helper()
	for _, ld := range m.LogicalDevices {
		for _, ln := range ld.LogicalNodes {
			for _, ds := range ln.DataSets {
				if ds.Name == "dsCross" {
					var out []string
					for _, mem := range ds.Members {
						out = append(out, mem.LDInst)
					}
					return out
				}
			}
		}
	}
	t.Fatal("dsCross not found")
	return nil
}

// IEC 61850-8-1: an LD is the MMS domain LDevice@ldName, or the IED name
// followed by LDevice@inst; data set members follow.
func TestFromSCL_StandardDomainNames(t *testing.T) {
	m, err := FromSCL(twoLDSCL(""), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ldNames(m), " "); got != "IED1LD1(LD1) IED1LD2(LD2)" {
		t.Errorf("domains %s", got)
	}
	if got := strings.Join(crossMembers(t, m), ","); got != "IED1LD1,IED1LD2," {
		t.Errorf("dsCross member LDs %q, want IED1LD1,IED1LD2, (the last one in its own LD)", got)
	}
	if got := m.LDInst("IED1LD2"); got != "LD2" {
		t.Errorf("LDInst(IED1LD2) = %q", got)
	}
	if got := m.LDInst("LD2"); got != "" {
		t.Errorf("LDInst(LD2) = %q, want none", got)
	}
	// A member in another LD resolves under the domain name.
	if _, err := m.ResolveMember("IED1LD2", DataSetMemberDef{LDInst: "IED1LD1", LNName: "GGIO1", DOPath: "Ind1.stVal", FC: "ST"}); err != nil {
		t.Errorf("ResolveMember across LDs: %v", err)
	}
}

func TestFromSCL_LdNameHonoured(t *testing.T) {
	m, err := FromSCL(twoLDSCL("Bay2"), "IED1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ldNames(m), " "); got != "IED1LD1(LD1) Bay2(LD2)" {
		t.Errorf("domains %s", got)
	}
	if got := strings.Join(crossMembers(t, m), ","); got != "IED1LD1,Bay2," {
		t.Errorf("dsCross member LDs %q", got)
	}
}

func TestFromSCL_DuplicateDomainNamesRejected(t *testing.T) {
	// LD2's ldName is the standard name of LD1.
	_, err := FromSCL(twoLDSCL("IED1LD1"), "IED1", "")
	if err == nil || !strings.Contains(err.Error(), "IED1LD1") {
		t.Fatalf("err = %v, want a duplicate domain name error", err)
	}
	// The instance names do not collide.
	if _, err := FromSCLWithOptions(twoLDSCL("IED1LD1"), "IED1", "", Options{InstanceDomainNames: true}); err != nil {
		t.Errorf("InstanceDomainNames: %v", err)
	}
}

func TestFromSCL_InstanceDomainNames(t *testing.T) {
	m, err := FromSCLWithOptions(twoLDSCL("Bay2"), "IED1", "", Options{InstanceDomainNames: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ldNames(m), " "); got != "LD1(LD1) LD2(LD2)" {
		t.Errorf("domains %s", got)
	}
	if got := strings.Join(crossMembers(t, m), ","); got != "LD1,LD2," {
		t.Errorf("dsCross member LDs %q", got)
	}
}
