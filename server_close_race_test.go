// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"sync"
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
	stop := make(chan struct{})
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
		}
	})
	time.Sleep(20 * time.Millisecond)
	srv.Close()
	srv.Close()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
	if srv.SettingGroupEngine() != nil || srv.JournalEngine() != nil {
		t.Fatal("engines reported after Close")
	}
	if err := srv.ChangeActiveSettingGroup(ctx, sgFixtureLD, 1); err == nil {
		t.Fatal("ChangeActiveSettingGroup after Close succeeded")
	}
}
