//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

// readDsInteropNVL reads LLN0$dsInterop of the server at h by its list name
// (MMS Read with a variableListName) and returns the access results.
func readDsInteropNVL(t *testing.T, h *iedServerHandle, withSpec bool) ([]mms.AccessResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := h.dial(t, ctx)
	defer c.Abort(context.Background())

	name := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: mms.DomainID(h.iedName + "InteropLD"),
		ItemID: "LLN0$dsInterop",
	}
	return c.MMS().ReadNamedVariableList(ctx, name, mms.ReadNamedVariableListOptions{SpecificationWithResult: withSpec})
}

// checkDsInteropValues asserts the two members of dsInterop: GGIO1.SPS1.stVal
// (boolean) and LLN0.Mod.stVal (integer), as in values.json.
func checkDsInteropValues(t *testing.T, res []mms.AccessResult) {
	t.Helper()
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	for i, r := range res {
		if r.Value == nil {
			t.Fatalf("result[%d]: data access error %v", i, r.ErrorCode)
		}
	}
	if b, ok := res[0].Value.Bool(); !ok || b != fixVal.SPS1StVal {
		t.Errorf("result[0] (SPS1.stVal) = %v (%v), want boolean %v", res[0].Value, ok, fixVal.SPS1StVal)
	}
	if iv, ok := res[1].Value.Int64(); !ok || iv != fixVal.ModStVal {
		t.Errorf("result[1] (Mod.stVal) = %v (%v), want integer %d", res[1].Value, ok, fixVal.ModStVal)
	}
}

// TestLibIECClient_ReadNamedVariableList reads the data set dsInterop of the
// peer stack's IED server by its list name, with specificationWithResult
// false and true.
func TestLibIECClient_ReadNamedVariableList(t *testing.T) {
	h := startIEDServer(t)
	for _, withSpec := range []bool{false, true} {
		t.Run(specName(withSpec), func(t *testing.T) {
			res, err := readDsInteropNVL(t, h, withSpec)
			if err != nil {
				t.Fatalf("ReadNamedVariableList: %v", err)
			}
			checkDsInteropValues(t, res)
		})
	}
}

// TestBeanClient_ReadNamedVariableList is TestLibIECClient_ReadNamedVariableList
// against the iec61850bean IED server. That server answers only
// specificationWithResult TRUE (iec61850-interop capabilities.json,
// registered limitation of iec61850bean GetDataSetValues); the FALSE case
// expects the refusal and fails once the limitation is gone, so it is noticed.
func TestBeanClient_ReadNamedVariableList(t *testing.T) {
	h := startIEC61850BeanServer(t)
	t.Run(specName(false), func(t *testing.T) {
		res, err := readDsInteropNVL(t, h, false)
		if err == nil {
			t.Errorf("iec61850bean answered specificationWithResult FALSE (%d results): "+
				"the registered limitation (capabilities.json, GetDataSetValues needs specificationWithResult TRUE) is gone; "+
				"update the limitation and this test", len(res))
			return
		}
		t.Skipf("iec61850bean limitation (capabilities.json): GetDataSetValues needs specificationWithResult TRUE; FALSE refused: %v", err)
	})
	t.Run(specName(true), func(t *testing.T) {
		res, err := readDsInteropNVL(t, h, true)
		if err != nil {
			t.Fatalf("ReadNamedVariableList: %v", err)
		}
		checkDsInteropValues(t, res)
	})
}

func specName(withSpec bool) string {
	if withSpec {
		return "specificationWithResult=true"
	}
	return "specificationWithResult=false"
}
