// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// The SettingGroupHandler callbacks run under a recover, like
// ControlHandler callbacks: a callback that ends abnormally refuses the
// write with object-access-denied, is logged, changes nothing, and the
// server answers the next request of the same association.
func TestSettingGroupHandler_CallbackIsRecovered(t *testing.T) {
	const reason = "application fault"
	for _, tc := range []struct {
		name    string
		handler SettingGroupHandler
		// act issues the service that reaches the callback.
		act func(ctx context.Context, c *Client) error
	}{
		{
			name: "OnActiveSGChanged",
			handler: SettingGroupHandler{OnActiveSGChanged: func(_ context.Context, _ string, sg uint8) error {
				if sg == 3 {
					panic(reason)
				}
				return nil
			}},
			act: func(ctx context.Context, c *Client) error { return c.SelectActiveSG(ctx, "LD1", 3) },
		},
		{
			name: "OnEditSGSelected",
			handler: SettingGroupHandler{OnEditSGSelected: func(_ context.Context, _ string, sg uint8) error {
				if sg == 3 {
					panic(reason)
				}
				return nil
			}},
			act: func(ctx context.Context, c *Client) error { return c.SelectEditSG(ctx, "LD1", 3) },
		},
		{
			name:    "OnConfirmEdit",
			handler: SettingGroupHandler{OnConfirmEdit: func(context.Context, string, uint8) error { panic(reason) }},
			act: func(ctx context.Context, c *Client) error {
				if err := c.SelectEditSG(ctx, "LD1", 2); err != nil {
					return err
				}
				return c.ConfirmEditSG(ctx, "LD1")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logged bytes.Buffer
			model := &servermodel.Model{LogicalDevices: []servermodel.LogicalDevice{{
				Name: "LD1",
				LogicalNodes: []servermodel.LogicalNode{{
					Name: "LLN0", LNClass: "LLN0",
					DataObjects: []servermodel.DataObject{{
						Name: "Set1",
						Attributes: []servermodel.DataAttribute{
							{Name: "setVal", FC: "SE", BType: "INT32"},
							{Name: "setVal", FC: "SG", BType: "INT32"},
						},
					}},
					SettingGroup: &servermodel.SettingGroupDef{NumOfSGs: 3, ActSG: 1},
				}},
			}}}
			srv, err := NewServer(model, ServerOptions{Logger: slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError}))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(srv.Close)
			srv.EnableSettingGroups(tc.handler)
			c := controlClient(t, srv)
			ctx := waitCtx(t)

			err = tc.act(ctx, c)
			if got := dataAccessCode(err); got != mms.DataAccessErrorObjectAccessDenied {
				t.Fatalf("service = %v (%v), want object-access-denied", err, got)
			}

			// The same association is still served, and the state is the
			// one before the refused service.
			info, err := c.GetSettingGroupInfo(ctx, "LD1")
			if err != nil {
				t.Fatalf("GetSettingGroupInfo after the refused service: %v", err)
			}
			if info.ActSG != 1 {
				t.Errorf("ActSG = %d after the refused service, want 1", info.ActSG)
			}
			if tc.name != "OnConfirmEdit" && info.EditSG != 0 {
				t.Errorf("EditSG = %d after the refused service, want 0", info.EditSG)
			}
			// A service the callback accepts works afterwards.
			if tc.name != "OnConfirmEdit" {
				if err := c.SelectActiveSG(ctx, "LD1", 2); err != nil {
					t.Errorf("SelectActiveSG 2 afterwards: %v", err)
				}
			}
			srv.Close() // the log is complete
			if out := logged.String(); !strings.Contains(out, tc.name) || !strings.Contains(out, reason) {
				t.Errorf("log = %q, want the callback name %q and the recovered value", out, tc.name)
			}
		})
	}
}
