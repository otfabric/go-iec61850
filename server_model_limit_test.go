// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/scl"
)

// The type expansion of a server model stops at
// ServerModelOptions.MaxExpandedNodes with an error that wraps both
// limit sentinels; within the limit the model is built.
func TestNewServerModelFromSCL_MaxExpandedNodes(t *testing.T) {
	s, err := scl.Parse(strings.NewReader(`<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4"><Header id="x"/>
<IED name="I"><AccessPoint name="AP"><Server><LDevice inst="LD0"><LN0 lnClass="LLN0" inst="" lnType="T0"/></LDevice></Server></AccessPoint></IED>
<DataTypeTemplates><LNodeType id="T0" lnClass="LLN0"><DO name="Mod" type="M"/></LNodeType>
<DOType id="M" cdc="ENC"><DA name="stVal" fc="ST" bType="INT32"/><DA name="q" fc="ST" bType="Quality"/></DOType></DataTypeTemplates></SCL>`))
	if err != nil {
		t.Fatal(err)
	}
	const nodes = 3 // Mod, stVal, q
	for _, tc := range []struct {
		max  int
		fail bool
	}{
		{0, false}, {nodes, false}, {nodes - 1, true},
	} {
		m, err := NewServerModelFromSCLWithOptions(s, "I", "", ServerModelOptions{MaxExpandedNodes: tc.max})
		if tc.fail {
			if m != nil || !errors.Is(err, ErrLimitExceeded) || !errors.Is(err, scl.ErrLimitExceeded) || !strings.HasPrefix(err.Error(), "servermodel: ") {
				t.Errorf("limit %d: %v, want both limit sentinels", tc.max, err)
			}
			continue
		}
		if err != nil || len(m.LogicalDevices) != 1 {
			t.Errorf("limit %d: %v", tc.max, err)
		}
	}
	if _, err := NewServerModelFromSCL(s, "I", ""); err != nil {
		t.Errorf("NewServerModelFromSCL: %v", err)
	}
	if _, err := NewServerModelFromSCL(s, "missing", ""); err == nil || errors.Is(err, ErrLimitExceeded) {
		t.Errorf("unknown IED: %v", err)
	}
}
