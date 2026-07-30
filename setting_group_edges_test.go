// SPDX-License-Identifier: MIT

package iec61850

import "testing"

func TestGetEditSettingGroup_MissingLD(t *testing.T) {
	e := &SettingGroupEngine{sgcbs: map[string]*sgcbRuntime{}}
	if got := e.GetEditSettingGroup("missing"); got != 0 {
		t.Fatalf("got %d", got)
	}
}
