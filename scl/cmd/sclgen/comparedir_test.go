// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compareDir fails on a file that differs, a file the generator produces
// that is not checked in, and a checked-in file the generator does not
// produce.
func TestCompareDir(t *testing.T) {
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gen, checked := t.TempDir(), t.TempDir()
	write(gen, "types.go", "a")
	write(checked, "types.go", "a")
	if err := os.Mkdir(filepath.Join(checked, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := compareDir(gen, checked); err != nil {
		t.Fatalf("equal: %v", err)
	}

	write(checked, "stale.go", "old")
	if err := compareDir(gen, checked); err == nil || !strings.Contains(err.Error(), "stale.go: not produced") {
		t.Errorf("extra checked-in file: %v", err)
	}
	if err := os.Remove(filepath.Join(checked, "stale.go")); err != nil {
		t.Fatal(err)
	}

	write(checked, "types.go", "b")
	if err := compareDir(gen, checked); err == nil || !strings.Contains(err.Error(), "content differs") {
		t.Errorf("different content: %v", err)
	}
	write(gen, "enums.go", "e")
	if err := compareDir(gen, checked); err == nil || !strings.Contains(err.Error(), "not found in checked-in output") {
		t.Errorf("missing file: %v", err)
	}
	if err := compareDir(gen, filepath.Join(checked, "none")); err == nil {
		t.Error("missing checked-in directory: no error")
	}
}
