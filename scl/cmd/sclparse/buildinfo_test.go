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
		{"no tag", resolveBuildInfo("1.9.0", "", sha, "2026-11-05T15:41:49Z", nil),
			"sclparse 1.9.0 (commit " + sha + ", built 2026-11-05T15:41:49Z, " + goPlatform + ")"},
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

func TestBuildInfoLibraries(t *testing.T) {
	b := resolveBuildInfo("1.9.0", "v1.9.0", sha, "2026-11-05T15:41:49Z", nil)
	pre := "sclparse 1.9.0 (tag v1.9.0, commit " + sha + ", built 2026-11-05T15:41:49Z, "
	jpre := `{"name":"sclparse","version":"1.9.0","tag":"v1.9.0","commit":"` + sha + `","built":"2026-11-05T15:41:49Z","libraries":`
	jpost := `,"go":"` + runtime.Version() + `","platform":"` + runtime.GOOS + "/" + runtime.GOARCH + `"}`
	cases := []struct {
		libs       []library
		text, json string
	}{
		{nil, pre + goPlatform + ")", jpre + "[]" + jpost},
		{[]library{{"go-mms", "1.2.0"}}, pre + "go-mms 1.2.0, " + goPlatform + ")",
			jpre + `[{"name":"go-mms","version":"1.2.0"}]` + jpost},
		{[]library{{"go-asn1", "0.3.1"}, {"go-mms", "1.2.0-0.20261105154149-1802d0f9afe8"}},
			pre + "go-asn1 0.3.1, go-mms 1.2.0-0.20261105154149-1802d0f9afe8, " + goPlatform + ")",
			jpre + `[{"name":"go-asn1","version":"0.3.1"},{"name":"go-mms","version":"1.2.0-0.20261105154149-1802d0f9afe8"}]` + jpost},
	}
	for _, c := range cases {
		b.Libraries = c.libs
		if got := b.Text(); got != c.text {
			t.Errorf("text:\n got %s\nwant %s", got, c.text)
		}
		if got := b.JSON(); got != c.json {
			t.Errorf("json:\n got %s\nwant %s", got, c.json)
		}
	}
}

func TestLibrariesFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/otfabric/go-iec61850/scl/cmd/sclparse"},
		Deps: []*debug.Module{
			{Path: "github.com/spf13/cobra", Version: "v1.10.2"},
			{Path: "github.com/otfabric/go-zeta", Version: "v0.2.0"},
			{Path: "github.com/otfabric/go-iec61850", Version: "v1.9.0"},
			{Path: "github.com/otfabric/go-alpha", Version: "v1.0.0", Replace: &debug.Module{Path: "../go-alpha", Version: "v1.1.0"}},
		},
	}
	got := libraries(bi)
	want := []library{{"go-alpha", "1.1.0"}, {"go-zeta", "0.2.0"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("libraries = %v, want %v", got, want)
	}
	if got := libraries(nil); got == nil || len(got) != 0 {
		t.Errorf("libraries(nil) = %#v, want empty", got)
	}
}

func TestBuildInfoFromGo(t *testing.T) {
	dirty := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "true"}}}
	if got, want := resolveBuildInfo("dev", "", "unknown", "unknown", dirty).Text(),
		"sclparse dev (commit "+sha+"-dirty, built unknown, "+goPlatform+")"; got != want {
		t.Errorf("go build:\n got %s\nwant %s", got, want)
	}
	for _, v := range []string{
		"v1.4.1-0.20261107101010-1802d0f9afe8+dirty", // a modified checkout
		"v1.4.1-0.20261107101010-1802d0f9afe8",       // a clean checkout off a tag
		"v0.0.0-20261107101010-1802d0f9afe8",         // no tag at all
		"v1.4.0+dirty",                               // a modified checkout on a tag
	} {
		bi := &debug.BuildInfo{Main: debug.Module{Version: v}}
		if got := resolveBuildInfo("dev", "", "", "", bi).Version; got != "dev" {
			t.Errorf("%s: version %q, want dev", v, got)
		}
	}
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v1.9.0"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "false"}}}
	info := resolveBuildInfo("dev", "", "unknown", "unknown", installed)
	if info.Version != "1.9.0" || info.Commit != sha {
		t.Errorf("go install @v1.9.0: %+v", info)
	}
	// Injected values win over the Go build information.
	if info := resolveBuildInfo("9.9.9", "v9.9.9", "abc", "2026-11-05T15:41:49Z", installed); info.Version != "9.9.9" || info.Commit != "abc" {
		t.Errorf("injected: %+v", info)
	}
}

func TestBuildInfoJSON(t *testing.T) {
	got := resolveBuildInfo("", "", "", "", nil).JSON()
	if !strings.HasPrefix(got, `{"name":"sclparse","version":"dev","tag":"","commit":"unknown","built":"unknown","libraries":[],"go":"`) {
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
