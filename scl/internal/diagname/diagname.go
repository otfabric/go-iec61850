// SPDX-License-Identifier: MIT

// Package diagname cuts the document names that SCL diagnostics quote,
// so that the size of a diagnostic does not grow with the length of a
// name in the document.
package diagname

import "unicode/utf8"

// Max is the longest name, in bytes, a diagnostic quotes by default
// (scl.DefaultMaxDiagnosticName).
const Max = 256

// Cap returns n cut to max bytes at a character boundary, ending with
// "…" when it was cut.
func Cap(n string, max int) string {
	if len(n) <= max {
		return n
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(n[cut]) {
		cut--
	}
	return n[:cut] + "…"
}

// Q is Cap with the default length [Max].
func Q(n string) string { return Cap(n, Max) }
