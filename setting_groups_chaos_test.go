// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"math/rand"
	"sync"
	"testing"

	"github.com/otfabric/go-mms"
)

// Chaos and race tests for the setting group value banks (IEC 61850-7-2
// §16). Run with -race.

// TestChaos_SettingGroupEditSelectRace: clients select, edit, confirm
// and cancel setting groups at once while the application seeds values
// and reports run. The edit reservation keeps each edit to its owner
// (7-2 16.3.3.1): a confirmed edit commits the owner's last SE value.
// Afterwards every bank holds a value someone wrote, and FC=SG holds the
// active group's bank.
func TestChaos_SettingGroupEditSelectRace(t *testing.T) {
	checkGoroutines(t, func() {
		type commit struct {
			group uint8
			value float64
		}
		var (
			mu      sync.Mutex
			commits = map[commit]bool{}
			allowed = map[float64]bool{600: true, 800: true, 500: true}
			srv     *Server
		)
		srv = sgFixtureServer(t, SettingGroupHandler{
			// Runs before the engine copies the edit buffer.
			OnConfirmEdit: func(_ context.Context, _ string, g uint8) error {
				f, _ := srv.ValueStore().Get(keyStrSE).Float64()
				mu.Lock()
				commits[commit{g, f}] = true
				mu.Unlock()
				return nil
			},
		}, func(s *Server) { s.EnableReports() })
		e := srv.SettingGroupEngine()
		ctx := context.Background()

		allow := func(v float64) {
			mu.Lock()
			allowed[v] = true
			mu.Unlock()
		}

		const clients, rounds = 6, 60
		var wg sync.WaitGroup
		start := make(chan struct{})
		conns := make([]*Client, clients)
		for id := range clients {
			c := dialLoopback(t, ctx, srv)
			conns[id] = c
			wg.Add(1)
			go func() {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(id) + 1))
				<-start
				for round := range rounds {
					g := uint8(rng.Intn(3) + 1)
					switch rng.Intn(4) {
					case 0:
						if err := c.SelectActiveSG(ctx, sgFixtureLD, g); err != nil {
							t.Errorf("client %d SelectActiveSG: %v", id, err)
						}
					case 1:
						if err := c.CancelEditSG(ctx, sgFixtureLD); err != nil && dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
							t.Errorf("client %d CancelEditSG: %v", id, err)
						}
					default:
						v := float64(id*1000 + round)
						if err := c.SelectEditSG(ctx, sgFixtureLD, g); err != nil {
							if dataAccessCode(err) != mms.DataAccessErrorTemporarilyUnavailable {
								t.Errorf("client %d SelectEditSG: %v", id, err)
							}
							continue
						}
						allow(v)
						if err := c.SetEditSGValue(ctx, refStrVal, mms.NewFloat(v)); err != nil {
							t.Errorf("client %d SetEditSGValue while holding the edit: %v", id, err)
							continue
						}
						if err := c.ConfirmEditSG(ctx, sgFixtureLD); err != nil {
							t.Errorf("client %d ConfirmEditSG while holding the edit: %v", id, err)
							continue
						}
						mu.Lock()
						ok := commits[commit{g, float64(float32(v))}]
						mu.Unlock()
						if !ok {
							t.Errorf("client %d: confirm of group %d did not commit its value %v", id, g, v)
						}
					}
				}
			}()
		}

		// The application seeds groups and reads them meanwhile.
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(99))
			<-start
			for i := range rounds {
				g := uint8(rng.Intn(3) + 1)
				v := float64(-1 - i)
				allow(v)
				if err := e.SetSettingGroupValue(ctx, keyStrSG, g, mms.NewFloat(v)); err != nil {
					t.Errorf("SetSettingGroupValue: %v", err)
				}
				if _, err := e.SettingGroupValue(keyStrSE, g); err != nil {
					t.Errorf("SettingGroupValue: %v", err)
				}
			}
		}()
		close(start)
		wg.Wait()
		for _, c := range conns {
			dropAssociation(c)
		}

		for g := uint8(1); g <= 3; g++ {
			v := groupFloat(t, srv, keyStrSG, g)
			if !allowed[float64(float32(v))] && !allowed[v] {
				t.Errorf("group %d holds %v, which nobody wrote", g, v)
			}
		}
		act := e.GetActiveSettingGroup(sgFixtureLD)
		live, _ := srv.ValueStore().Get(keyStrSG).Float64()
		if want := groupFloat(t, srv, keyStrSG, act); live != want {
			t.Errorf("FC=SG %v, active group %d bank %v", live, act, want)
		}
		srv.Close()
	})
}
