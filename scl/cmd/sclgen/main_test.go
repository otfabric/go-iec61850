// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// specRootDir is the repository's XSD bundles, relative to this package.
const specRootDir = "../../specs"

// resetFlags restores every flag of the command tree to its default and
// clears its Changed state, so the required-flag check runs afresh.
func resetFlags(t *testing.T) {
	t.Helper()
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if err := f.Value.Set(f.DefValue); err != nil {
				t.Fatalf("reset --%s: %v", f.Name, err)
			}
			f.Changed = false
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
}

// runArgs drives rootCmd through run() with args and returns the exit code
// and stderr.
func runArgs(t *testing.T, ctx context.Context, args ...string) (int, string) {
	t.Helper()
	resetFlags(t)
	var stderr bytes.Buffer
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(args)
	code := run(ctx, &stderr)
	return code, stderr.String()
}

func TestRun_ExitCodes(t *testing.T) {
	if _, err := os.Stat(filepath.Join(specRootDir, allVersions[0].Dir, "SCL.xsd")); err != nil {
		t.Skipf("XSD bundles not available: %v", err)
	}
	upToDate := t.TempDir()
	if code, stderr := runArgs(t, context.Background(), "generate", "--spec-root", specRootDir, "--out", upToDate, "--versions", "v17"); code != ExitOK {
		t.Fatalf("generate: exit %d, stderr %q", code, stderr)
	}

	brokenRoot := t.TempDir()
	brokenBundle := filepath.Join(brokenRoot, allVersions[0].Dir)
	if err := os.MkdirAll(brokenBundle, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenBundle, "SCL.xsd"), []byte("<not xml"), 0o600); err != nil {
		t.Fatal(err)
	}

	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{"ok: check up to date", []string{"check", "--spec-root", specRootDir, "--out", upToDate, "--versions", "v17"}, ExitOK, ""},
		{"ok: version", []string{"version"}, ExitOK, ""},
		{"findings: check out of date", []string{"check", "--spec-root", specRootDir, "--out", t.TempDir(), "--versions", "v17"}, ExitFindings, "out of date: 1.7"},
		{"usage: missing --spec-root", []string{"generate", "--out", t.TempDir()}, ExitUsage, `required flag(s) "spec-root" not set`},
		{"usage: missing --out", []string{"check", "--spec-root", specRootDir}, ExitUsage, `required flag(s) "out" not set`},
		{"usage: unknown flag", []string{"generate", "--nope"}, ExitUsage, "unknown flag"},
		{"usage: single-dash long flag", []string{"generate", "-spec-root", specRootDir, "-out", t.TempDir()}, ExitUsage, "sclgen: "},
		{"usage: unknown command", []string{"nope"}, ExitUsage, "unknown command"},
		{"usage: no edition matches", []string{"generate", "--spec-root", specRootDir, "--out", t.TempDir(), "--versions", "v99"}, ExitUsage, "no edition matches"},
		{"invalid input: broken bundle", []string{"generate", "--spec-root", brokenRoot, "--out", t.TempDir(), "--versions", "v17"}, ExitInvalidInput, "parse 1.7"},
		{"invalid input: broken bundle (check)", []string{"check", "--spec-root", brokenRoot, "--out", t.TempDir(), "--versions", "v17"}, ExitInvalidInput, "parse 1.7"},
		{"internal: output not writable", []string{"generate", "--spec-root", specRootDir, "--out", notADir, "--versions", "v17"}, ExitInternal, "emit 1.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stderr := runArgs(t, context.Background(), tt.args...)
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

// A usage error is reported on stderr, not swallowed by SilenceErrors.
func TestRun_UsageErrorIsPrinted(t *testing.T) {
	code, stderr := runArgs(t, context.Background(), "generate")
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.HasPrefix(stderr, "sclgen: ") || !strings.Contains(stderr, "spec-root") {
		t.Errorf("stderr = %q, want the missing --spec-root error", stderr)
	}
}

func TestRun_Interrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, stderr := runArgs(t, ctx, "version")
	if code != ExitInterrupted {
		t.Errorf("exit code = %d, want %d", code, ExitInterrupted)
	}
	if !strings.Contains(stderr, "interrupted") {
		t.Errorf("stderr = %q, want \"interrupted\"", stderr)
	}
}

// The examples in the help text parse with pflag (a single-dash long flag
// such as -spec-root does not).
func TestHelpExamples_Parse(t *testing.T) {
	n := 0
	for _, c := range []*cobra.Command{rootCmd, generateCmd, checkCmd} {
		for _, line := range strings.Split(c.Long, "\n") {
			if !strings.HasPrefix(line, "  sclgen ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			n++
			t.Run(strings.Join(fields, " "), func(t *testing.T) {
				resetFlags(t)
				sub, rest, err := rootCmd.Find(fields[1:])
				if err != nil || sub == rootCmd {
					t.Fatalf("example names no subcommand: %v", err)
				}
				if err := sub.ParseFlags(rest); err != nil {
					t.Fatalf("flags do not parse: %v", err)
				}
				if err := sub.ValidateRequiredFlags(); err != nil {
					t.Fatalf("required flags: %v", err)
				}
				if specRoot == "" || outDir == "" {
					t.Errorf("spec-root %q, out %q: want both set", specRoot, outDir)
				}
			})
		}
	}
	resetFlags(t)
	if n != 4 {
		t.Errorf("found %d examples, want 4", n)
	}
}

func TestExitCode_Classification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"findings", exitErr(ExitFindings, "out of date"), ExitFindings},
		{"usage", exitErr(ExitUsage, "no edition"), ExitUsage},
		{"invalid input", exitErr(ExitInvalidInput, "parse: %w", errors.New("bad xsd")), ExitInvalidInput},
		{"wrapped invalid input", fmt.Errorf("ctx: %w", exitErr(ExitInvalidInput, "parse")), ExitInvalidInput},
		{"internal", exitErr(ExitInternal, "emit"), ExitInternal},
		{"cancelled", context.Canceled, ExitInterrupted},
		{"wrapped cancelled", fmt.Errorf("generate: %w", context.Canceled), ExitInterrupted},
		{"cobra usage", errors.New(`required flag(s) "out" not set`), ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
	inner := errors.New("bad xsd")
	if err := exitErr(ExitInvalidInput, "parse: %w", inner); !errors.Is(err, inner) {
		t.Error("exitError does not unwrap to its cause")
	}
}

func TestExitCodes_OrganisationTable(t *testing.T) {
	for name, v := range map[string][2]int{
		"ExitOK": {ExitOK, 0}, "ExitFindings": {ExitFindings, 1}, "ExitUsage": {ExitUsage, 2},
		"ExitInvalidInput": {ExitInvalidInput, 3}, "ExitInternal": {ExitInternal, 10}, "ExitInterrupted": {ExitInterrupted, 130},
	} {
		if v[0] != v[1] {
			t.Errorf("%s = %d, want %d", name, v[0], v[1])
		}
	}
}
