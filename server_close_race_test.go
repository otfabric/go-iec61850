// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

// TestServerClose_ConcurrentWithRequests: Close runs while open
// associations select setting groups and write settings and the
// application calls SetValue (Close does not end associations). Run with
// -race: before v1.19.1 Close cleared the journal and setting group
// engine fields that those requests read. Afterwards the accessors report
// the engines as gone and the requests still get an answer.
func TestServerClose_ConcurrentWithRequests(t *testing.T) {
	srv := sgFixtureServer(t, SettingGroupHandler{}, func(s *Server) { s.EnableReports() })
	ctx := context.Background()
	c := dialLoopback(t, ctx, srv)
	// Each loop counts its rounds; Close comes after both have run a few,
	// and the loops stop after a few more rounds after Close.
	stop := make(chan struct{})
	var clientRounds, appRounds atomic.Int64
	var wg sync.WaitGroup
	wg.Go(func() {
		for g := uint8(1); ; g = g%3 + 1 {
			select {
			case <-stop:
				return
			default:
			}
			_ = c.SelectActiveSG(ctx, sgFixtureLD, g)
			_ = c.SelectEditSG(ctx, sgFixtureLD, g)
			_ = c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(float64(g)))
			_ = c.ConfirmEditSG(ctx, sgFixtureLD)
			clientRounds.Add(1)
		}
	})
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			srv.SetValue(ctx, keyStrSG, mms.NewFloat(float64(i)))
			appRounds.Add(1)
		}
	})
	// waitRounds returns once both loops have run three rounds more.
	waitRounds := func() {
		t.Helper()
		c, a := clientRounds.Load()+3, appRounds.Load()+3
		deadline := time.Now().Add(10 * time.Second)
		for clientRounds.Load() < c || appRounds.Load() < a {
			if time.Now().After(deadline) {
				t.Fatalf("rounds: client %d, application %d, want %d and %d", clientRounds.Load(), appRounds.Load(), c, a)
			}
			runtime.Gosched()
		}
	}
	waitRounds()
	srv.Close()
	srv.Close()
	waitRounds()
	close(stop)
	wg.Wait()
	if srv.SettingGroupEngine() != nil || srv.JournalEngine() != nil {
		t.Fatal("engines reported after Close")
	}
	if err := srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, 1); err == nil {
		t.Fatal("ChangeActiveSettingGroup after Close succeeded")
	}
}
