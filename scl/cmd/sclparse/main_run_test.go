// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runWithArgs drives the run() seam used by main with the given args after
// resetting flag state via executeCmd's reset logic.
func runWithArgs(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var code int
	var stderr bytes.Buffer
	_ = captureStdout(t, func() {
		// executeCmd resets all flag globals; run it with "version" first so
		// the reset happens, then run the real args through run().
		_ = executeCmd("version")
		rootCmd.SetArgs(args)
		code = run(context.Background(), &stderr)
	})
	return code, stderr.String()
}

func TestRun_ExitCodes(t *testing.T) {
	missing := "/nonexistent/sclparse/does-not-exist.scd"
	notSCL := filepath.Join(t.TempDir(), "other.xml")
	if err := os.WriteFile(notSCL, []byte(`<?xml version="1.0"?><Other/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	unwritable := filepath.Join(t.TempDir(), "no-such-dir", "out.json")
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{"ok", []string{"detect", "--quiet", fixturePath("scl/testdata/simple.scd")}, ExitOK, ""},
		{"findings", []string{"validate", fixturePath("scl/testdata/broken.scd")}, ExitFindings, "validation error(s)"},
		{"usage: argument count", []string{"detect"}, ExitUsage, "accepts 1 arg"},
		{"usage: unknown command", []string{"no-such-command"}, ExitUsage, "unknown command"},
		{"usage: unknown flag", []string{"summary", "--no-such-flag", missing}, ExitUsage, "unknown flag"},
		{"invalid input: missing file", []string{"summary", missing}, ExitInvalidInput, "sclparse: "},
		{"invalid input: not SCL", []string{"detect", notSCL}, ExitInvalidInput, "sclparse: detect"},
		{"internal: output not writable", []string{"dump-json", "--output", unwritable, fixturePath("scl/testdata/simple.scd")}, ExitInternal, "sclparse: create"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stderr := runWithArgs(t, tt.args...)
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

func TestRun_Interrupted(t *testing.T) {
	_ = executeCmd("version") // reset the flag globals
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rootCmd.SetArgs([]string{"detect", "--quiet", fixturePath("scl/testdata/simple.scd")})
	var stderr bytes.Buffer
	var code int
	_ = captureStdout(t, func() { code = run(ctx, &stderr) })
	if code != ExitInterrupted {
		t.Errorf("exit code = %d, want %d", code, ExitInterrupted)
	}
	if !strings.Contains(stderr.String(), "interrupted") {
		t.Errorf("stderr = %q, want \"interrupted\"", stderr.String())
	}
}

func TestExitCode_Classification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"findings", exitErr(ExitFindings, "1 validation error(s)"), ExitFindings},
		{"invalid input", exitErr(ExitInvalidInput, "parse"), ExitInvalidInput},
		{"wrapped invalid input", fmt.Errorf("ctx: %w", exitErr(ExitInvalidInput, "parse")), ExitInvalidInput},
		{"internal", exitErr(ExitInternal, "write"), ExitInternal},
		{"output error", outputErr(errors.New("broken pipe")), ExitInternal},
		{"cancelled", context.Canceled, ExitInterrupted},
		{"wrapped cancelled", fmt.Errorf("run: %w", context.Canceled), ExitInterrupted},
		{"cobra usage", errors.New(`unknown flag: --nope`), ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
	if outputErr(nil) != nil {
		t.Error("outputErr(nil) != nil")
	}
}

func TestExitCodes_OrganisationTable(t *testing.T) {
	want := map[string][2]int{
		"ExitOK": {ExitOK, 0}, "ExitFindings": {ExitFindings, 1}, "ExitUsage": {ExitUsage, 2},
		"ExitInvalidInput": {ExitInvalidInput, 3}, "ExitInternal": {ExitInternal, 10}, "ExitInterrupted": {ExitInterrupted, 130},
	}
	for name, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s = %d, want %d", name, v[0], v[1])
		}
	}
}

func TestRun_UsageErrorHasNoPrefix(t *testing.T) {
	_, stderr := runWithArgs(t, "detect")
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
