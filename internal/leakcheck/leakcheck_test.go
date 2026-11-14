// SPDX-License-Identifier: MIT

package leakcheck

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLeaked_FindsGoroutineStartedAfterSnapshot: a goroutine started after
// the snapshot is reported with its state and stack while it runs, and not
// once it has ended.
func TestLeaked_FindsGoroutineStartedAfterSnapshot(t *testing.T) {
	base := Take()
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		parkedForLeakcheckTest(release)
	}()
	// The goroutine is reported from its start; its state is read until
	// it has reached the channel receive.
	var leaked []Goroutine
	for deadline := time.Now().Add(Wait); ; {
		leaked = base.Leaked(0)
		if len(leaked) != 1 {
			t.Fatalf("Leaked = %d goroutines, want 1:\n%s", len(leaked), Report(leaked))
		}
		if leaked[0].State == "chan receive" || time.Now().After(deadline) {
			break
		}
		runtime.Gosched()
	}
	if leaked[0].State != "chan receive" || !strings.Contains(leaked[0].Stack, "parkedForLeakcheckTest") {
		t.Fatalf("Leaked = state %q, stack:\n%s", leaked[0].State, leaked[0].Stack)
	}
	if r := Report(leaked); !strings.Contains(r, "1 goroutine(s)") || !strings.Contains(r, "parkedForLeakcheckTest") {
		t.Fatalf("Report = %q", r)
	}
	close(release)
	<-done
	if leaked := base.Leaked(Wait); len(leaked) != 0 {
		t.Fatalf("after the goroutine ended: %s", Report(leaked))
	}
}

// TestLeaked_WaitsForEndingGoroutine: a goroutine that ends while Leaked
// waits is not reported.
func TestLeaked_WaitsForEndingGoroutine(t *testing.T) {
	base := Take()
	release := make(chan struct{})
	startParked(release)
	// Release the goroutine from a timer so that it ends while Leaked
	// retries; the result does not depend on when.
	time.AfterFunc(5*time.Millisecond, func() { close(release) })
	if leaked := base.Leaked(Wait); len(leaked) != 0 {
		t.Fatalf("Leaked: %s", Report(leaked))
	}
}

// TestLeaked_BaselineNotReported: goroutines of the snapshot are not
// reported, nor is the caller.
func TestLeaked_BaselineNotReported(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	startParked(release)
	base := Take()
	if leaked := base.Leaked(0); len(leaked) != 0 {
		t.Fatalf("Leaked: %s", Report(leaked))
	}
}

type fakeTB struct {
	cleanups []func()
	errs     []string
	failed   bool
}

func (f *fakeTB) Helper()                   {}
func (f *fakeTB) Cleanup(fn func())         { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) Failed() bool              { return f.failed }
func (f *fakeTB) Errorf(s string, a ...any) { f.errs = append(f.errs, fmt.Sprintf(s, a...)) }
func (f *fakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

// TestCheck: the cleanup fails the test when a goroutine is left, passes
// when none is, and adds nothing to a test that already failed.
func TestCheck(t *testing.T) {
	t.Run("leak", func(t *testing.T) {
		f := &fakeTB{}
		check(f, 0)
		release := make(chan struct{})
		startParked(release)
		f.runCleanups()
		close(release)
		if len(f.errs) != 1 || !strings.Contains(f.errs[0], "parkedForLeakcheckTest") {
			t.Fatalf("errors = %q, want one naming the goroutine", f.errs)
		}
	})
	t.Run("clean", func(t *testing.T) {
		f := &fakeTB{}
		Check(f)
		f.runCleanups()
		if len(f.errs) != 0 {
			t.Fatalf("errors = %q", f.errs)
		}
	})
	t.Run("already failed", func(t *testing.T) {
		f := &fakeTB{failed: true}
		check(f, 0)
		release := make(chan struct{})
		startParked(release)
		f.runCleanups()
		close(release)
		if len(f.errs) != 0 {
			t.Fatalf("errors = %q", f.errs)
		}
	})
}

type fakeM struct {
	code int
	run  func()
}

func (m fakeM) Run() int {
	if m.run != nil {
		m.run()
	}
	return m.code
}

// TestMain_ExitCodes: Main keeps a failing code, passes a clean run and
// fails a passing run that leaves a goroutine, naming it.
func TestMain_ExitCodes(t *testing.T) {
	var out bytes.Buffer
	stderr = &out
	defer func() { stderr = nil }()

	if code := Main(fakeM{code: 0}); code != 0 {
		t.Fatalf("clean run: code %d, output %q", code, out.String())
	}

	release := make(chan struct{})
	defer close(release)
	m := fakeM{code: 0, run: func() { startParked(release) }}
	if code := run(m, 0); code != 1 || !strings.Contains(out.String(), "parkedForLeakcheckTest") {
		t.Fatalf("leaking run: code %d, output %q", code, out.String())
	}

	out.Reset()
	if code := run(fakeM{code: 3, run: m.run}, 0); code != 3 || out.Len() != 0 {
		t.Fatalf("failing run: code %d, output %q", code, out.String())
	}
}

// TestCurrent_GrowsBuffer: a dump larger than the first buffer is read in
// full.
func TestCurrent_GrowsBuffer(t *testing.T) {
	old := bufSize
	bufSize = 16
	defer func() { bufSize = old }()
	all := current()
	if len(all) == 0 || !strings.Contains(all[0].Stack, "TestCurrent_GrowsBuffer") {
		t.Fatalf("current() = %d goroutines, first:\n%v", len(all), all)
	}
}

func TestParse(t *testing.T) {
	dump := "goroutine 7 [running]:\nmain.f()\n\n" +
		"goroutine 12 gp=0x1 m=nil [select, 2 minutes]:\nmain.g()\n\n" +
		"goroutine x [running]:\n\n" +
		"not a goroutine\n\n" +
		"goroutine 13 no state\nmain.h()\n"
	got := parse([]byte(dump))
	want := []struct {
		id    uint64
		state string
	}{{7, "running"}, {12, "select, 2 minutes"}, {13, ""}}
	if len(got) != len(want) {
		t.Fatalf("parse = %+v", got)
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].State != w.state {
			t.Errorf("goroutine %d = %d %q, want %d %q", i, got[i].ID, got[i].State, w.id, w.state)
		}
	}
}

func TestIgnored(t *testing.T) {
	for _, tc := range []struct {
		stack string
		want  bool
	}{
		{"goroutine 5 [select]:\nos/signal.loop()\n", true},
		{"goroutine 5 [select]:\ninternal/fuzz.(*worker).coordinate()\n", true},
		{"goroutine 5 [select]:\ntesting.runFuzzing()\n", true},
		{"goroutine 5 [select]:\ngithub.com/otfabric/x.f()\n", false},
	} {
		if got := ignored(Goroutine{Stack: tc.stack}); got != tc.want {
			t.Errorf("ignored(%q) = %v, want %v", tc.stack, got, tc.want)
		}
	}
}

// parkedForLeakcheckTest blocks until release is closed; its name marks
// the goroutine in stack dumps.
func parkedForLeakcheckTest(release <-chan struct{}) {
	<-release
}

// startParked starts a goroutine in parkedForLeakcheckTest and returns
// once it runs.
func startParked(release <-chan struct{}) {
	var started sync.WaitGroup
	started.Add(1)
	go func() {
		started.Done()
		parkedForLeakcheckTest(release)
	}()
	started.Wait()
}

// TestWaitBlocked: true once a goroutine is blocked in the named
// function, false when none is.
func TestWaitBlocked(t *testing.T) {
	release := make(chan struct{})
	startParked(release)
	if WaitBlocked("leakcheck.startParked.func1", time.Millisecond) {
		t.Fatal("WaitBlocked = true for a function that is not the innermost one")
	}
	if !WaitBlocked("leakcheck.parkedForLeakcheckTest", Wait) {
		t.Fatal("WaitBlocked = false for a goroutine blocked in parkedForLeakcheckTest")
	}
	close(release)
	if WaitBlocked("leakcheck.noSuchFunction", time.Millisecond) {
		t.Fatal("WaitBlocked = true for a function no goroutine is in")
	}
}

func TestBlocked(t *testing.T) {
	for state, want := range map[string]bool{
		"": false, "running": false, "runnable": false, "syscall": false,
		"chan receive": true, "select": true, "select, 2 minutes": true, "IO wait": true, "sync.Mutex.Lock": true,
	} {
		if got := blocked(state); got != want {
			t.Errorf("blocked(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestInnermost(t *testing.T) {
	stack := "goroutine 9 [select]:\nruntime.gopark(0x1)\n\t/x/proc.go:1\nruntime.selectgo(0x2)\n\t/x/select.go:2\n" +
		"example.com/p.(*T).Wait(0x3)\n\t/p/t.go:3\nexample.com/p.Run()\n\t/p/r.go:4"
	if got := innermost(stack); got != "example.com/p.(*T).Wait(0x3)" {
		t.Errorf("innermost = %q", got)
	}
	if got := innermost("goroutine 9 [running]:\nruntime.goexit()\n"); got != "" {
		t.Errorf("innermost of a runtime-only stack = %q", got)
	}
}
