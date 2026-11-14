// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

// detect reads at most maxDetectBytes of the file: the root element is
// enough, a file larger than the limit is detected from its beginning.
func TestDetect_ReadsBoundedPrefix(t *testing.T) {
	old := maxDetectBytes
	maxDetectBytes = 4096
	defer func() { maxDetectBytes = old }()

	p := writeTemp(t, "big.scd", `<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B" release="4">`+
		strings.Repeat("<!-- padding -->", 64<<10))
	data, err := readDetect(p)
	if err != nil || len(data) != 4096 {
		t.Fatalf("read %d bytes, %v; want 4096", len(data), err)
	}
	out, err := runCmd(t, "detect", "--quiet", p)
	if err != nil || strings.TrimSpace(out) != "2007B4" {
		t.Fatalf("detect: %q, %v", out, err)
	}
	if _, err := readDetect(p + ".missing"); err == nil {
		t.Error("missing file: no error")
	}
}
