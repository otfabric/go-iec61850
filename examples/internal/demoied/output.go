// SPDX-License-Identifier: MIT

package demoied

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// Output is an io.Writer for an example's output that tests can read
// while the example still writes to it.
type Output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

// String returns everything written so far.
func (o *Output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// WaitFor waits until the output contains s.
func (o *Output) WaitFor(t testing.TB, s string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !strings.Contains(o.String(), s) {
		if time.Now().After(deadline) {
			t.Fatalf("output has no %q after %v:\n%s", s, timeout, o.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
