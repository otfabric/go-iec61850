// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/scl"
)

// TestChaosHostileClientsAgainstSCLModel runs several clients that write
// random values of random types to random variables of a model built from
// SCL (read-only FCs, control structures with out-of-range ctlNum, RCB and
// GoCB attributes, wrong types, oversized strings), while the application
// updates other status values with SetValue. Afterwards the server must
// still serve every LN$FC to a fresh client, and the status attribute the
// hostile clients kept overwriting must hold the value the application
// set. The seeds are fixed so a failure replays.
func TestChaosHostileClientsAgainstSCLModel(t *testing.T) {
	doc, err := scl.ParseFile("interop/testdata/interop.icd")
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewServerModelFromSCL(doc, "InteropIED", "AP1")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	const dom = "InteropIEDInteropLD"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	names, err := dialModel(t, srv).GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeDomain, DomainID: dom,
	})
	if err != nil || len(names) == 0 {
		t.Fatalf("names: %v (%d)", err, len(names))
	}

	values := func(rng *rand.Rand) *mms.Value {
		switch rng.Intn(9) {
		case 0:
			return mms.NewBoolean(rng.Intn(2) == 1)
		case 1:
			return mms.NewInteger(rng.Int63() - rng.Int63())
		case 2:
			return mms.NewUnsigned(rng.Uint64())
		case 3:
			return mms.NewVisibleString(strings.Repeat("x", rng.Intn(400)))
		case 4:
			return mms.NewBitStringWithLength([]byte{byte(rng.Intn(256)), byte(rng.Intn(256))}, 1+rng.Intn(16))
		case 5:
			return mms.NewUTCTime(time.Unix(rng.Int63n(1<<32), 0))
		case 6:
			return mms.NewFloat(rng.Float64())
		case 7: // an Oper-like structure with an out-of-range ctlNum
			return mms.NewStructure([]*mms.Value{
				mms.NewBoolean(true),
				mms.NewStructure([]*mms.Value{mms.NewInteger(3), mms.NewOctetString([]byte("chaos"))}),
				mms.NewUnsigned(256 + uint64(rng.Intn(1000))),
				mms.NewUTCTime(time.Now()),
				mms.NewBoolean(false),
				mms.NewBitStringWithLength([]byte{0}, 2),
			})
		default:
			return mms.NewStructure([]*mms.Value{mms.NewInteger(1), mms.NewVisibleString("")})
		}
	}

	const target = "GGIO1$ST$SPS1$stVal"
	const want = true // set by the application before the hostile writes
	srv.SetValue(ctx, dom+"/"+target, mms.NewBoolean(want))
	var wg sync.WaitGroup
	for w := range 4 {
		c := dialModel(t, srv)
		rng := rand.New(rand.NewSource(int64(w + 1)))
		wg.Go(func() {
			for i := range 300 {
				item, v := names[rng.Intn(len(names))], values(rng)
				if i%5 == 0 {
					item, v = target, mms.NewBoolean(!want)
				}
				_, _ = c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: mms.ItemID(item), Value: v})
			}
		})
	}
	wg.Go(func() { // the application keeps changing other status values
		for i := range 300 {
			srv.SetValue(ctx, dom+"/GGIO1$ST$SPS1$q", mms.NewBitStringWithLength([]byte{byte(i), 0}, 13))
			srv.SetValue(ctx, dom+"/LLN0$ST$Mod$stVal", mms.NewInteger(int64(i%5)))
		}
	})
	wg.Wait()

	if ctx.Err() != nil {
		t.Fatalf("hostile clients did not finish: %v", ctx.Err())
	}
	c := dialModel(t, srv)
	res, err := c.ReadMultiple(ctx, []mms.ObjectName{{Scope: mms.ObjectScopeDomain, Domain: dom, ItemID: target}})
	if err != nil || len(res) != 1 || res[0].Value == nil {
		t.Fatalf("read %s: %v %+v", target, err, res)
	}
	if got, _ := res[0].Value.Bool(); got != want {
		t.Errorf("%s = %v, want %v: a client overwrote a status value", target, got, want)
	}
	for _, n := range names {
		if strings.Count(n, "$") != 1 {
			continue
		}
		res, err := c.ReadMultiple(ctx, []mms.ObjectName{{Scope: mms.ObjectScopeDomain, Domain: dom, ItemID: mms.ItemID(n)}})
		if err != nil {
			t.Fatalf("%s after chaos: %v", n, err)
		}
		if len(res) != 1 || res[0].Value == nil {
			t.Errorf("%s after chaos: no value", n)
		}
	}
}
