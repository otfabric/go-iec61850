// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/examples/internal/demoied"
)

func TestRun(t *testing.T) {
	_, addr := demoied.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	var out demoied.Output
	if err := run(ctx, addr, "", "demo.icd", dir, &out); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out.String())
	got, err := os.ReadFile(filepath.Join(dir, "demo.icd"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, demoied.ICD) {
		t.Errorf("downloaded %d bytes, want the %d of demo.icd", len(got), len(demoied.ICD))
	}
	if fi, err := os.Stat(filepath.Join(dir, "demo.icd")); err != nil {
		t.Error(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("download mode %v, want 0600", fi.Mode().Perm())
	}
	// A missing -out directory is an error, not a write elsewhere.
	if err := run(ctx, addr, "", "demo.icd", filepath.Join(dir, "missing"), &out); err == nil {
		t.Error("missing -out directory accepted")
	}
	// A second download must not overwrite.
	if err := run(ctx, addr, "", "demo.icd", dir, &out); err == nil {
		t.Error("overwrote an existing file")
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"a/b/c.txt": "c.txt", `..\x:y`: "x_y", "..": "download", "COMTRADE/r1.cfg": "r1.cfg"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
