// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"
	"testing"
)

// runWithArgs drives the run() seam used by main with the given args after
// resetting flag state via executeCmd's reset logic.
func runWithArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var code int
	var stderr bytes.Buffer
	out := captureStdout(t, func() {
		// executeCmd resets all flag globals; run it with "version" first so
		// the reset happens, then run the real args through run().
		_ = executeCmd("version")
		rootCmd.SetArgs(args)
		code = run(&stderr)
	})
	return code, out, stderr.String()
}

func TestRun_ExitCodes(t *testing.T) {
	missing := "/nonexistent/sclparse/does-not-exist.scd"
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{"ok", []string{"detect", "--quiet", fixturePath("scl/testdata/simple.scd")}, exitOK, ""},
		{"parse fail", []string{"summary", missing}, exitParseFail, "sclparse: "},
		{"validation fail", []string{"validate", fixturePath("scl/testdata/broken.scd")}, exitValidation, "validation error(s)"},
		{"usage", []string{"detect"}, exitUsage, "accepts 1 arg"},
		{"unknown command", []string{"no-such-command"}, exitUsage, "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runWithArgs(t, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr=%q)", code, tt.wantCode, stderr)
			}
			if tt.wantStderr == "" && stderr != "" {
				t.Errorf("unexpected stderr: %q", stderr)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr, tt.wantStderr)
			}
		})
	}
}

func TestRun_UsageErrorHasNoPrefix(t *testing.T) {
	_, _, stderr := runWithArgs(t, "detect")
	if strings.HasPrefix(stderr, "sclparse: ") {
		t.Errorf("cobra usage errors must not carry the exitError prefix: %q", stderr)
	}
}

func TestVersionCommand_PrintsVersionString(t *testing.T) {
	out := captureStdout(t, func() {
		if err := executeCmd("version"); err != nil {
			t.Fatalf("version: %v", err)
		}
	})
	if strings.TrimSpace(out) != versionString("sclparse") {
		t.Errorf("version output = %q, want %q", out, versionString("sclparse"))
	}
}
