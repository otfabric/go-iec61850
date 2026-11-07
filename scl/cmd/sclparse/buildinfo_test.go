// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const sha = "1802d0f9afe8642d1e278120b76fcdfac967cb9e"

var goPlatform = runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH

func TestBuildInfoText(t *testing.T) {
	cases := []struct {
		name string
		info buildInfo
		want string
	}{
		{"release", resolveBuildInfo("1.9.0", "v1.9.0", sha, "2026-11-05T15:41:49Z", nil),
			"sclparse 1.9.0 (tag v1.9.0, commit " + sha + ", built 2026-11-05T15:41:49Z, " + goPlatform + ")"},
		{"leading v stripped", resolveBuildInfo("v1.9.0", "v1.9.0", sha, "2026-11-05T15:41:49Z", nil),
			"sclparse 1.9.0 (tag v1.9.0, commit " + sha + ", built 2026-11-05T15:41:49Z, " + goPlatform + ")"},
		{"no tag", resolveBuildInfo("1.9.0-3-gabc", "", sha, "2026-11-05T15:41:49Z", nil),
			"sclparse 1.9.0-3-gabc (commit " + sha + ", built 2026-11-05T15:41:49Z, " + goPlatform + ")"},
		{"nothing injected", resolveBuildInfo("dev", "", "unknown", "unknown", nil),
			"sclparse dev (commit unknown, built unknown, " + goPlatform + ")"},
		{"empty injected", resolveBuildInfo("", "", "", "", nil),
			"sclparse dev (commit unknown, built unknown, " + goPlatform + ")"},
	}
	for _, c := range cases {
		if got := c.info.Text(); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestBuildInfoFromGo(t *testing.T) {
	dirty := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "true"}}}
	if got, want := resolveBuildInfo("dev", "", "unknown", "unknown", dirty).Text(),
		"sclparse dev (commit "+sha+"-dirty, built unknown, "+goPlatform+")"; got != want {
		t.Errorf("go build:\n got %s\nwant %s", got, want)
	}
	dirtyPseudo := &debug.BuildInfo{Main: debug.Module{Version: "v0.4.1-0.20261107101010-1802d0f9afe8+dirty"}}
	if got := resolveBuildInfo("dev", "", "", "", dirtyPseudo).Version; got != "dev" {
		t.Errorf("+dirty pseudo-version: %q, want dev", got)
	}
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v1.9.0"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "false"}}}
	info := resolveBuildInfo("dev", "", "unknown", "unknown", installed)
	if info.Version != "1.9.0" || info.Commit != sha {
		t.Errorf("go install @v1.9.0: %+v", info)
	}
	// Injected values win over the Go build information.
	if info := resolveBuildInfo("1.10.0", "v1.10.0", "abc", "2026-11-05T15:41:49Z", installed); info.Version != "1.10.0" || info.Commit != "abc" {
		t.Errorf("injected: %+v", info)
	}
}

func TestBuildInfoJSON(t *testing.T) {
	got := resolveBuildInfo("1.9.0", "v1.9.0", sha, "2026-11-05T15:41:49Z", nil).JSON()
	want := `{"name":"sclparse","version":"1.9.0","tag":"v1.9.0","commit":"` + sha + `","built":"2026-11-05T15:41:49Z","go":"` +
		runtime.Version() + `","platform":"` + runtime.GOOS + "/" + runtime.GOARCH + `"}`
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	got = resolveBuildInfo("", "", "", "", nil).JSON()
	if !strings.HasPrefix(got, `{"name":"sclparse","version":"dev","tag":"","commit":"unknown","built":"unknown","go":"`) {
		t.Errorf("dev: %s", got)
	}
}

// freshCompletionCmd removes cobra's default completion command, which
// captures the output writer when it is created, so that the next Execute
// creates it again with the current one.
func freshCompletionCmd() {
	if c, _, err := rootCmd.Find([]string{"completion"}); err == nil && c.Name() == "completion" {
		rootCmd.RemoveCommand(c)
	}
}

// complete runs cobra's hidden __complete command and returns its output.
func complete(t *testing.T, args ...string) string {
	t.Helper()
	_ = captureStdout(t, func() { _ = executeCmd("version") }) // reset the flag globals
	freshCompletionCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs(append([]string{"__complete"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	return out.String()
}

func TestCompletion(t *testing.T) {
	cases := []struct {
		args  []string
		wants []string
	}{
		{[]string{"completion", ""}, []string{"bash", "zsh", "fish", "powershell"}},
		{[]string{""}, []string{"summary", "validate", "dump-json", "list-ieds", "version", "completion"}},
		{[]string{"validate", ""}, []string{"scd", "cid", "icd", "ssd", "iid", "sed", "xml", ":8"}}, // FilterFileExt
		{[]string{"dump-json", "--output", ""}, []string{"json", ":8"}},
		{[]string{"validate", "--max-errors", ""}, []string{":4"}}, // NoFileComp
		{[]string{"version", "--j"}, []string{"--json"}},
	}
	for _, c := range cases {
		out := complete(t, c.args...)
		for _, w := range c.wants {
			if !strings.Contains(out, w) {
				t.Errorf("__complete %q: missing %q in\n%s", c.args, w, out)
			}
		}
	}
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		_ = captureStdout(t, func() { _ = executeCmd("version") })
		freshCompletionCmd()
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs([]string{"completion", sh})
		if err := rootCmd.Execute(); err != nil || out.Len() < 500 {
			t.Errorf("completion %s: %v, %d bytes", sh, err, out.Len())
		}
	}
}

// TestEveryFlagCompletes fails when a flag that takes a value has neither a
// completion function nor a file or directory annotation.
func TestEveryFlagCompletes(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.NoOptDefVal != "" || f.Name == "help" {
				return // a boolean
			}
			_, hasFunc := c.GetFlagCompletionFunc(f.Name)
			_, hasExt := f.Annotations[cobra.BashCompFilenameExt]
			_, hasDir := f.Annotations[cobra.BashCompSubdirsInDir]
			if !hasFunc && !hasExt && !hasDir {
				t.Errorf("%s --%s has no completion", c.CommandPath(), f.Name)
			}
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
}
