//go:build interop

// SPDX-License-Identifier: MIT

// Package interop contains interoperability tests for go-iec61850.
//
// Tests start mms-interop adapter containers (or local binaries), wait for the
// JSON readiness event, exercise the go-iec61850 API, and assert results.
// They own the full lifecycle: start, wait, run, teardown.
//
// Run with:
//
//	go test -tags=interop ./interop/...
//
// Environment variables (all optional):
//
//	LIBIEC61850_IMAGE         Docker image with libiec61850 adapters (default: mms-interop-libiec61850:local)
//	IEC61850BEAN_IMAGE        Docker image with iec61850bean adapters (default: mms-interop-iec61850bean:local)
//	IEC61850_SERVER_BINARY    Path to libiec61850-ied-server binary (skips Docker)
//	IEC61850_CLIENT_BINARY    Path to libiec61850-ied-client binary (skips Docker)
//	IEC61850_REPORTER_BINARY  Path to libiec61850-ied-reporter binary (skips Docker)
//	IEC61850_FIXTURE_DIR      Directory with interop.icd and values.json (default: testdata)
package interop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/scl"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultLibIECImage = "mms-interop-libiec61850:local"
	defaultBeanImage   = "mms-interop-iec61850bean:local"
	defaultFixtureDir  = "testdata"
	iedReadyTimeout    = 60 * time.Second
	iedClientTimeout   = 60 * time.Second
	iedReporterTimeout = 60 * time.Second
)

// ---------------------------------------------------------------------------
// Shared iec61850bean server (started once in TestMain, shared across tests)
// ---------------------------------------------------------------------------

// sharedBeanHandle is set once in TestMain and reused by all TestBeanClient_*
// tests. Starting the iec61850bean container (JVM) takes ~60 s, so we avoid
// restarting it for every test function.
var sharedBeanHandle *iedServerHandle

// ---------------------------------------------------------------------------
// Fixture values (loaded once from values.json)
// ---------------------------------------------------------------------------

type fixtureValues struct {
	ModStVal       int64
	ModCtlModel    int64
	ModD           string
	BehStVal       int64
	SPS1StVal      bool
	SPS1D          string
	SPCSO1StVal    bool
	SPCSO1CtlModel int64
	SPCSO2StVal    bool
	SPCSO2CtlModel int64
	SPCSO3StVal    bool
	SPCSO3CtlModel int64
	TotWMagF       float64
}

var fixVal *fixtureValues

func TestMain(m *testing.M) {
	dir := getEnvOr("IEC61850_FIXTURE_DIR", defaultFixtureDir)
	var err error
	fixVal, err = loadFixtureValues(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadFixtureValues: %v\n", err)
		os.Exit(1)
	}

	// Pre-start the iec61850bean IED server once and share it across all
	// TestBeanClient_* tests. The JVM takes ~60 s to start, so sharing
	// avoids a 60 s per-test overhead that causes the overall test run to
	// exceed the default timeout.
	var beanErr error
	sharedBeanHandle, beanErr = startSharedBeanServer()
	if beanErr != nil {
		fmt.Fprintf(os.Stderr, "[interop] shared bean server not started: %v (falling back to per-test start)\n", beanErr)
	} else {
		fmt.Fprintf(os.Stderr, "[interop] shared bean server ready at %s\n", sharedBeanHandle.addr)
	}

	code := m.Run()

	// Stop the shared bean container after all tests finish.
	if sharedBeanHandle != nil && sharedBeanHandle.stopFn != nil {
		sharedBeanHandle.stopFn()
	}
	os.Exit(code)
}

func loadFixtureValues(fixtureDir string) (*fixtureValues, error) {
	path := filepath.Join(fixtureDir, "values.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var raw struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}

	getFloat := func(key string) (float64, error) {
		v, ok := raw.Values[key]
		if !ok {
			return 0, fmt.Errorf("key %q not found in values.json", key)
		}
		var f float64
		return f, json.Unmarshal(v, &f)
	}
	getString := func(key string) (string, error) {
		v, ok := raw.Values[key]
		if !ok {
			return "", fmt.Errorf("key %q not found in values.json", key)
		}
		var s string
		return s, json.Unmarshal(v, &s)
	}
	getBool := func(key string) (bool, error) {
		v, ok := raw.Values[key]
		if !ok {
			return false, fmt.Errorf("key %q not found in values.json", key)
		}
		var b bool
		return b, json.Unmarshal(v, &b)
	}

	fv := &fixtureValues{}
	var f float64

	if f, err = getFloat("InteropLD/LLN0.Mod.stVal"); err != nil {
		return nil, err
	}
	fv.ModStVal = int64(f)

	if f, err = getFloat("InteropLD/LLN0.Mod.ctlModel"); err != nil {
		return nil, err
	}
	fv.ModCtlModel = int64(f)

	if fv.ModD, err = getString("InteropLD/LLN0.Mod.d"); err != nil {
		return nil, err
	}

	if f, err = getFloat("InteropLD/LLN0.Beh.stVal"); err != nil {
		return nil, err
	}
	fv.BehStVal = int64(f)

	if fv.SPS1StVal, err = getBool("InteropLD/GGIO1.SPS1.stVal"); err != nil {
		return nil, err
	}

	if fv.SPS1D, err = getString("InteropLD/GGIO1.SPS1.d"); err != nil {
		return nil, err
	}

	if fv.SPCSO1StVal, err = getBool("InteropLD/GGIO1.SPCSO1.stVal"); err != nil {
		return nil, err
	}

	if f, err = getFloat("InteropLD/GGIO1.SPCSO1.ctlModel"); err != nil {
		return nil, err
	}
	fv.SPCSO1CtlModel = int64(f)

	if fv.SPCSO2StVal, err = getBool("InteropLD/GGIO1.SPCSO2.stVal"); err != nil {
		return nil, err
	}

	if f, err = getFloat("InteropLD/GGIO1.SPCSO2.ctlModel"); err != nil {
		return nil, err
	}
	fv.SPCSO2CtlModel = int64(f)

	if fv.SPCSO3StVal, err = getBool("InteropLD/GGIO1.SPCSO3.stVal"); err != nil {
		return nil, err
	}

	if f, err = getFloat("InteropLD/GGIO1.SPCSO3.ctlModel"); err != nil {
		return nil, err
	}
	fv.SPCSO3CtlModel = int64(f)

	if fv.TotWMagF, err = getFloat("InteropLD/MMXU1.TotW.mag.f"); err != nil {
		return nil, err
	}

	return fv, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func icdPath(t *testing.T) string {
	t.Helper()
	dir := getEnvOr("IEC61850_FIXTURE_DIR", defaultFixtureDir)
	abs, err := filepath.Abs(filepath.Join(dir, "interop.icd"))
	if err != nil {
		t.Fatalf("icdPath: %v", err)
	}
	return abs
}

func iedFreePort(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("IEC61850_INTEROP_PORT"); p != "" {
		return p
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("iedFreePort: %v", err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return fmt.Sprintf("%d", p)
}

func dialIED(t *testing.T, ctx context.Context, addr string) *iec61850.Client {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("iec61850.Dial %s: %v", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dialIEDWithIEDName dials with the IED name set so the client strips the
// IED prefix from MMS domain names (e.g. "InteropIEDInteropLD" → "InteropLD").
func dialIEDWithIEDName(t *testing.T, ctx context.Context, addr, iedName string) *iec61850.Client {
	t.Helper()
	c, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{IEDName: iedName})
	if err != nil {
		t.Fatalf("iec61850.Dial %s: %v", addr, err)
	}
	return c
}

// ---------------------------------------------------------------------------
// JSON Lines types
// ---------------------------------------------------------------------------

type iedReadyEvent struct {
	Event   string `json:"event"`
	Address string `json:"address"`
	Fixture string `json:"fixture"`
	Adapter string `json:"adapter"`
	Version string `json:"version"`
	IEDName string `json:"ied_name"`
}

type iedAdapterReady struct {
	addr    string
	fixture string
	adapter string
	version string
	iedName string
}

// validateIEDAdapterMeta asserts that the ready event carries the expected
// fixture and adapter identifiers and a non-empty version string. In CI (env CI
// set) a "dev" version is rejected so accidental use of a local image is caught.
func validateIEDAdapterMeta(t *testing.T, m iedAdapterReady, wantFixture, wantAdapter string) {
	t.Helper()
	if m.fixture != wantFixture {
		t.Errorf("adapter fixture: got %q, want %q", m.fixture, wantFixture)
	}
	if m.adapter != wantAdapter {
		t.Errorf("adapter name: got %q, want %q", m.adapter, wantAdapter)
	}
	if m.version == "" {
		t.Error("adapter version is empty")
	}
	if os.Getenv("CI") != "" && m.version == "dev" {
		t.Errorf("adapter version is %q in CI; pin a released image digest", m.version)
	}
}

type iedClientResult struct {
	Operation string          `json:"operation"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Target    string          `json:"target,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Names     []string        `json:"names,omitempty"`
	Values    json.RawMessage `json:"values,omitempty"`
}

type iedReporterResult struct {
	Operation string          `json:"operation"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Target    string          `json:"target,omitempty"`
	RptID     string          `json:"rptID,omitempty"`
	RptEna    bool            `json:"rptEna,omitempty"`
	SeqNum    uint32          `json:"seqNum,omitempty"`
	Inclusion []bool          `json:"inclusion,omitempty"`
	Values    json.RawMessage `json:"values,omitempty"`
	Reasons   []string        `json:"reasons,omitempty"`
}

type iedControllerResult struct {
	Operation string          `json:"operation"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Target    string          `json:"target,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	CtlVal    *bool           `json:"ctlval,omitempty"`
}

// ---------------------------------------------------------------------------
// Collection helpers
// ---------------------------------------------------------------------------

func collectIEDResults(t *testing.T, ch <-chan iedClientResult) []iedClientResult {
	t.Helper()
	var out []iedClientResult
	for r := range ch {
		t.Logf("ied-client: op=%q ok=%v target=%q", r.Operation, r.OK, r.Target)
		out = append(out, r)
	}
	return out
}

func findIEDResult(results []iedClientResult, op, target string) (iedClientResult, bool) {
	for _, r := range results {
		if r.Operation == op && (target == "" || r.Target == target) {
			return r, true
		}
	}
	return iedClientResult{}, false
}

func findIEDOp(results []iedClientResult, op string) (iedClientResult, bool) {
	return findIEDResult(results, op, "")
}

func collectReporterResults(t *testing.T, ch <-chan iedReporterResult) []iedReporterResult {
	t.Helper()
	var out []iedReporterResult
	for r := range ch {
		t.Logf("reporter: op=%q ok=%v target=%q", r.Operation, r.OK, r.Target)
		out = append(out, r)
	}
	return out
}

func findReporterOp(results []iedReporterResult, op string) (iedReporterResult, bool) {
	for _, r := range results {
		if r.Operation == op {
			return r, true
		}
	}
	return iedReporterResult{}, false
}

// ---------------------------------------------------------------------------
// libiec61850 IED server (client-direction tests)
// ---------------------------------------------------------------------------

type iedServerHandle struct {
	addr    string
	iedName string
	stopFn  func() // optional cleanup, used by shared instances
}

// dial opens a go-iec61850 connection to the server, passing the IED name
// so that MMS domain names are automatically stripped.
// Under QEMU emulation the TCP listener may not be immediately ready after the
// "ready" event is emitted; retry with a short back-off for up to 5 seconds.
func (h *iedServerHandle) dial(t *testing.T, ctx context.Context) *iec61850.Client {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := iec61850.Dial(ctx, h.addr, iec61850.DialOptions{IEDName: h.iedName})
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("iec61850.Dial %s: %v", h.addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dockerContainerName returns a unique Docker container name for the given test.
// Container names must match [a-zA-Z0-9_-] so test path slashes are replaced.
func dockerContainerName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("interop-%d-%s", os.Getpid(), t.Name())
	return strings.NewReplacer("/", "_", " ", "_", "(", "", ")", "").Replace(name)
}

// dockerStop sends docker stop to the named container; errors are ignored
// because the container may have already exited.
func dockerStop(name string) {
	exec.Command("docker", "stop", "-t", "2", name).Run() //nolint:errcheck
}

func startIEDServer(t *testing.T) *iedServerHandle {
	t.Helper()

	port := iedFreePort(t)
	// cmdCtx lives for the whole test — cancelled by t.Cleanup.
	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	// startCtx only guards the ready-event wait.
	startCtx, startCancel := context.WithTimeout(context.Background(), iedReadyTimeout)
	defer startCancel()

	var (
		cmd           *exec.Cmd
		containerName string
	)
	if binary := os.Getenv("IEC61850_SERVER_BINARY"); binary != "" {
		cmd = exec.CommandContext(cmdCtx, binary, "--port", port)
	} else {
		containerName = dockerContainerName(t)
		image := getEnvOr("LIBIEC61850_IMAGE", defaultLibIECImage)
		cmd = exec.CommandContext(cmdCtx, "docker", "run", "--rm",
			"--name", containerName,
			"-p", port+":"+port,
			"--entrypoint", "libiec61850-ied-server",
			image,
			"--port", port,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmdCancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cmdCancel()
		t.Fatalf("start IED server: %v", err)
	}

	stop := func() {
		if containerName != "" {
			dockerStop(containerName)
		}
		cmdCancel()
		_ = cmd.Wait()
	}

	ready := make(chan iedAdapterReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var ev iedReadyEvent
			if json.Unmarshal([]byte(scanner.Text()), &ev) == nil && ev.Event == "ready" {
				ready <- iedAdapterReady{
					addr:    fmt.Sprintf("127.0.0.1:%s", port),
					fixture: ev.Fixture,
					adapter: ev.Adapter,
					version: ev.Version,
					iedName: ev.IEDName,
				}
				break
			}
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		close(ready)
	}()

	select {
	case m, ok := <-ready:
		if !ok {
			stop()
			t.Fatal("IED server exited before emitting readiness event")
		}
		validateIEDAdapterMeta(t, m, "iec61850-v1", "libiec61850")
		t.Cleanup(stop)
		return &iedServerHandle{addr: m.addr, iedName: m.iedName}
	case <-startCtx.Done():
		stop()
		t.Fatal("timed out waiting for IED server readiness")
		return nil
	}
}

// ---------------------------------------------------------------------------
// libiec61850 IED client (server-direction tests)
// ---------------------------------------------------------------------------

func startIEDClientAdapter(t *testing.T, port int) <-chan iedClientResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), iedClientTimeout)

	var cmd *exec.Cmd
	if binary := os.Getenv("IEC61850_CLIENT_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
		)
	} else {
		image := getEnvOr("LIBIEC61850_IMAGE", defaultLibIECImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "libiec61850-ied-client",
			image,
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start IED client: %v", err)
	}

	ch := make(chan iedClientResult, 32)
	go func() {
		defer close(ch)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r iedClientResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable IED client line: %q: %v", line, err)
				continue
			}
			ch <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return ch
}

// ---------------------------------------------------------------------------
// iec61850bean adapters (Phase 2B)
// ---------------------------------------------------------------------------

// startSharedBeanServer starts a single iec61850bean container that is
// shared across all TestBeanClient_* tests. Returns (nil, err) on failure,
// which is non-fatal: each test will then fall back to
// startIEC61850BeanServer. The returned handle has a stopFn that must be
// called by TestMain after m.Run().
func startSharedBeanServer() (*iedServerHandle, error) {
	port := strconv.Itoa(findFreePort())
	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	startCtx, startCancel := context.WithTimeout(context.Background(), iedReadyTimeout)
	defer startCancel()

	var (
		cmd           *exec.Cmd
		containerName string
	)
	if binary := os.Getenv("IEC61850BEAN_SERVER_BINARY"); binary != "" {
		cmd = exec.CommandContext(cmdCtx, binary, "--port", port)
	} else {
		containerName = "mms-interop-bean-shared"
		// Ensure any leftover container from a previous run is removed.
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		image := getEnvOr("IEC61850BEAN_IMAGE", defaultBeanImage)
		cmd = exec.CommandContext(cmdCtx, "docker", "run", "--rm",
			"--name", containerName,
			"-p", port+":"+port,
			"--entrypoint", "iec61850bean-ied-server",
			image,
			"--port", port,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmdCancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cmdCancel()
		return nil, fmt.Errorf("start shared bean server: %w", err)
	}

	stop := func() {
		if containerName != "" {
			dockerStop(containerName)
		}
		cmdCancel()
		_ = cmd.Wait()
	}

	ready := make(chan iedAdapterReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var ev iedReadyEvent
			if json.Unmarshal([]byte(scanner.Text()), &ev) == nil && ev.Event == "ready" {
				ready <- iedAdapterReady{
					addr:    fmt.Sprintf("127.0.0.1:%s", port),
					fixture: ev.Fixture,
					adapter: ev.Adapter,
					version: ev.Version,
					iedName: ev.IEDName,
				}
				break
			}
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		close(ready)
	}()

	select {
	case m, ok := <-ready:
		if !ok {
			stop()
			return nil, fmt.Errorf("bean server exited before readiness event")
		}
		return &iedServerHandle{addr: m.addr, iedName: m.iedName, stopFn: stop}, nil
	case <-startCtx.Done():
		stop()
		return nil, fmt.Errorf("timed out waiting for bean server readiness")
	}
}

// findFreePort returns a free TCP port for local use.
func findFreePort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startIEC61850BeanServer(t *testing.T) *iedServerHandle {
	t.Helper()

	// If the shared server was pre-started in TestMain, reuse it.
	// No per-test cleanup is registered because the shared server lives
	// for the entire test binary run and is stopped in TestMain.
	if sharedBeanHandle != nil {
		return sharedBeanHandle
	}

	// Fallback: start a dedicated container for this test (e.g. when
	// running a single test in isolation without TestMain pre-start).
	port := iedFreePort(t)
	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	startCtx, startCancel := context.WithTimeout(context.Background(), iedReadyTimeout)
	defer startCancel()

	var (
		cmd           *exec.Cmd
		containerName string
	)
	if binary := os.Getenv("IEC61850BEAN_SERVER_BINARY"); binary != "" {
		cmd = exec.CommandContext(cmdCtx, binary, "--port", port)
	} else {
		containerName = dockerContainerName(t)
		image := getEnvOr("IEC61850BEAN_IMAGE", defaultBeanImage)
		cmd = exec.CommandContext(cmdCtx, "docker", "run", "--rm",
			"--name", containerName,
			"-p", port+":"+port,
			"--entrypoint", "iec61850bean-ied-server",
			image,
			"--port", port,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmdCancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cmdCancel()
		t.Fatalf("start iec61850bean IED server: %v", err)
	}

	stop := func() {
		if containerName != "" {
			dockerStop(containerName)
		}
		cmdCancel()
		_ = cmd.Wait()
	}

	ready := make(chan iedAdapterReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var ev iedReadyEvent
			if json.Unmarshal([]byte(scanner.Text()), &ev) == nil && ev.Event == "ready" {
				ready <- iedAdapterReady{
					addr:    fmt.Sprintf("127.0.0.1:%s", port),
					fixture: ev.Fixture,
					adapter: ev.Adapter,
					version: ev.Version,
					iedName: ev.IEDName,
				}
				break
			}
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		close(ready)
	}()

	select {
	case m, ok := <-ready:
		if !ok {
			stop()
			t.Fatal("iec61850bean IED server exited before emitting readiness event")
		}
		validateIEDAdapterMeta(t, m, "iec61850-v1", "iec61850bean")
		t.Cleanup(stop)
		return &iedServerHandle{addr: m.addr, iedName: m.iedName}
	case <-startCtx.Done():
		stop()
		t.Fatal("timed out waiting for iec61850bean IED server readiness")
		return nil
	}
}

func startIEC61850BeanClientAdapter(t *testing.T, port int) <-chan iedClientResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), iedClientTimeout)

	var cmd *exec.Cmd
	if binary := os.Getenv("IEC61850BEAN_CLIENT_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
		)
	} else {
		image := getEnvOr("IEC61850BEAN_IMAGE", defaultBeanImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "iec61850bean-ied-client",
			image,
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start iec61850bean IED client: %v", err)
	}

	ch := make(chan iedClientResult, 32)
	go func() {
		defer close(ch)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r iedClientResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable iec61850bean client line: %q: %v", line, err)
				continue
			}
			ch <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return ch
}

// ---------------------------------------------------------------------------
// iec61850bean IED reporter (Phase 2D server direction)
// ---------------------------------------------------------------------------

func startIEC61850BeanReporterAdapter(t *testing.T, port int, sps1Initial bool) <-chan iedReporterResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), iedReporterTimeout)

	initial := "0"
	if sps1Initial {
		initial = "1"
	}

	var cmd *exec.Cmd
	if binary := os.Getenv("IEC61850BEAN_REPORTER_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
			"--sps1-initial", initial,
		)
	} else {
		image := getEnvOr("IEC61850BEAN_IMAGE", defaultBeanImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "iec61850bean-ied-reporter",
			image,
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
			"--sps1-initial", initial,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start iec61850bean reporter: %v", err)
	}

	ch := make(chan iedReporterResult, 32)
	go func() {
		defer close(ch)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r iedReporterResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable bean reporter line: %q: %v", line, err)
				continue
			}
			ch <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return ch
}

// ---------------------------------------------------------------------------
// go-iec61850 server (server-direction tests)
// ---------------------------------------------------------------------------

type goIEDServer struct {
	port int
	srv  *iec61850.Server
}

func startGoIEDServer(t *testing.T) *goIEDServer {
	t.Helper()

	sclData, err := scl.ParseFile(icdPath(t))
	if err != nil {
		t.Fatalf("scl.ParseFile: %v", err)
	}

	model, err := iec61850.NewServerModelFromSCL(sclData, "InteropIED", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := iec61850.NewServer(model, iec61850.ServerOptions{
		Identity: &iec61850.ServerIdentity{
			Vendor:   "OTFabric",
			Model:    "go-iec61850-interop",
			Revision: "1.0",
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	setGoIEDInitialValues(t, srv)

	ln, err := iso.Listen("0.0.0.0:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	port := ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})

	return &goIEDServer{port: port, srv: srv}
}

func startGoIEDServerWithReports(t *testing.T) *goIEDServer {
	t.Helper()

	sclData, err := scl.ParseFile(icdPath(t))
	if err != nil {
		t.Fatalf("scl.ParseFile: %v", err)
	}

	model, err := iec61850.NewServerModelFromSCL(sclData, "InteropIED", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := iec61850.NewServer(model, iec61850.ServerOptions{
		Identity: &iec61850.ServerIdentity{
			Vendor:   "OTFabric",
			Model:    "go-iec61850-interop",
			Revision: "1.0",
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	setGoIEDInitialValues(t, srv)
	srv.EnableReports()

	ln, err := iso.Listen("0.0.0.0:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	port := ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})

	return &goIEDServer{port: port, srv: srv}
}

func setGoIEDInitialValues(t *testing.T, srv *iec61850.Server) {
	t.Helper()

	vs := srv.ValueStore()
	set := func(key string, val *mms.Value) { vs.Set(key, val) }

	set("InteropLD/LLN0$ST$Mod$stVal", mms.NewInteger(fixVal.ModStVal))
	set("InteropLD/LLN0$CF$Mod$ctlModel", mms.NewInteger(fixVal.ModCtlModel))
	set("InteropLD/LLN0$DC$Mod$d", mms.NewVisibleString(fixVal.ModD))
	set("InteropLD/LLN0$ST$Beh$stVal", mms.NewInteger(fixVal.BehStVal))
	set("InteropLD/GGIO1$ST$SPS1$stVal", mms.NewBoolean(fixVal.SPS1StVal))
	set("InteropLD/GGIO1$DC$SPS1$d", mms.NewVisibleString(fixVal.SPS1D))
	set("InteropLD/GGIO1$ST$SPCSO1$stVal", mms.NewBoolean(fixVal.SPCSO1StVal))
	set("InteropLD/GGIO1$CF$SPCSO1$ctlModel", mms.NewInteger(fixVal.SPCSO1CtlModel))
	set("InteropLD/GGIO1$ST$SPCSO2$stVal", mms.NewBoolean(fixVal.SPCSO2StVal))
	set("InteropLD/GGIO1$CF$SPCSO2$ctlModel", mms.NewInteger(fixVal.SPCSO2CtlModel))
	set("InteropLD/GGIO1$ST$SPCSO3$stVal", mms.NewBoolean(fixVal.SPCSO3StVal))
	set("InteropLD/GGIO1$CF$SPCSO3$ctlModel", mms.NewInteger(fixVal.SPCSO3CtlModel))
	set("InteropLD/MMXU1$MX$TotW$mag$f", mms.NewFloat(fixVal.TotWMagF))
}

// ---------------------------------------------------------------------------
// libiec61850 IED reporter (Phase 2C server direction)
// ---------------------------------------------------------------------------

func startIEDReporterAdapter(t *testing.T, port int, sps1Initial bool) <-chan iedReporterResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), iedReporterTimeout)

	initial := "0"
	if sps1Initial {
		initial = "1"
	}

	var cmd *exec.Cmd
	if binary := os.Getenv("IEC61850_REPORTER_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
			"--sps1-initial", initial,
		)
	} else {
		image := getEnvOr("LIBIEC61850_IMAGE", defaultLibIECImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "libiec61850-ied-reporter",
			image,
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
			"--sps1-initial", initial,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start IED reporter: %v", err)
	}

	ch := make(chan iedReporterResult, 32)
	go func() {
		defer close(ch)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r iedReporterResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable reporter line: %q: %v", line, err)
				continue
			}
			ch <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return ch
}

// ---------------------------------------------------------------------------
// Controller adapters (Phase 2E)
// ---------------------------------------------------------------------------

// startIEDControllerAdapter runs the libiec61850-ied-controller against a
// go-iec61850 server and returns a channel of controller results.
// doName selects the target data object (e.g. "SPCSO1" for direct,
// "SPCSO2" for SBO normal); an empty string defaults to "SPCSO1".
func startIEDControllerAdapter(t *testing.T, port int, ctlVal bool, doName string) <-chan iedControllerResult {
	t.Helper()

	if doName == "" {
		doName = "SPCSO1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), iedClientTimeout)

	ctlStr := "1"
	if !ctlVal {
		ctlStr = "0"
	}

	var cmd *exec.Cmd
	if binary := os.Getenv("IEC61850_CONTROLLER_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
			"--ctlval", ctlStr,
			"--do", doName,
		)
	} else {
		image := getEnvOr("LIBIEC61850_IMAGE", defaultLibIECImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "libiec61850-ied-controller",
			image,
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
			"--ctlval", ctlStr,
			"--do", doName,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start IED controller: %v", err)
	}

	ch := make(chan iedControllerResult, 16)
	go func() {
		defer close(ch)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r iedControllerResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable controller line: %q: %v", line, err)
				continue
			}
			ch <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return ch
}

// startIEC61850BeanControllerAdapter runs the iec61850bean-ied-controller
// against a go-iec61850 server and returns a channel of controller results.
// doName selects the target data object (e.g. "SPCSO1" for direct,
// "SPCSO2" for SBO normal); an empty string defaults to "SPCSO1".
func startIEC61850BeanControllerAdapter(t *testing.T, port int, ctlVal bool, doName string) <-chan iedControllerResult {
	t.Helper()

	if doName == "" {
		doName = "SPCSO1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), iedClientTimeout)

	ctlStr := "1"
	if !ctlVal {
		ctlStr = "0"
	}

	var cmd *exec.Cmd
	if binary := os.Getenv("IEC61850BEAN_CONTROLLER_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
			"--ctlval", ctlStr,
			"--do", doName,
		)
	} else {
		image := getEnvOr("IEC61850BEAN_IMAGE", defaultBeanImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "iec61850bean-ied-controller",
			image,
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
			"--ctlval", ctlStr,
			"--do", doName,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start bean controller: %v", err)
	}

	ch := make(chan iedControllerResult, 16)
	go func() {
		defer close(ch)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r iedControllerResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable bean controller line: %q: %v", line, err)
				continue
			}
			ch <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return ch
}

// collectControllerResults drains a controller results channel.
func collectControllerResults(t *testing.T, ch <-chan iedControllerResult) []iedControllerResult {
	t.Helper()
	var out []iedControllerResult
	for r := range ch {
		t.Logf("controller: op=%q ok=%v target=%q", r.Operation, r.OK, r.Target)
		out = append(out, r)
	}
	return out
}

// findControllerOp returns the first result with the given operation.
func findControllerOp(results []iedControllerResult, op string) (iedControllerResult, bool) {
	for _, r := range results {
		if r.Operation == op {
			return r, true
		}
	}
	return iedControllerResult{}, false
}

// startGoIEDServerWithControls starts a go-iec61850 IED server with:
//   - GGIO1.SPCSO1 as direct-with-normal-security (ctlModel=1)
//   - GGIO1.SPCSO2 as sbo-with-normal-security (ctlModel=2)
//   - GGIO1.SPCSO3 as sbo-with-enhanced-security (ctlModel=4)
//
// All OnOperate handlers update stVal in the value store on operate.
func startGoIEDServerWithControls(t *testing.T) *goIEDServer {
	t.Helper()

	sclData, err := scl.ParseFile(icdPath(t))
	if err != nil {
		t.Fatalf("scl.ParseFile: %v", err)
	}

	model, err := iec61850.NewServerModelFromSCL(sclData, "InteropIED", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := iec61850.NewServer(model, iec61850.ServerOptions{
		Identity: &iec61850.ServerIdentity{
			Vendor:   "OTFabric",
			Model:    "go-iec61850-interop",
			Revision: "1.0",
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	setGoIEDInitialValues(t, srv)

	vs := srv.ValueStore()
	makeOperateHandler := func(stValKey string) iec61850.ControlHandler {
		return iec61850.ControlHandler{
			OnOperate: func(_ context.Context, req iec61850.ControlRequest) error {
				if req.CtlVal == nil {
					return fmt.Errorf("nil CtlVal in operate request")
				}
				boolVal, ok := req.CtlVal.Bool()
				if !ok {
					return fmt.Errorf("ctlVal is not a boolean")
				}
				vs.Set(stValKey, mms.NewBoolean(boolVal))
				return nil
			},
		}
	}

	// SPCSO1 — direct-with-normal-security
	if err := srv.RegisterControl("InteropLD", "GGIO1.SPCSO1",
		iec61850.CtlModelDirectNormal,
		makeOperateHandler("InteropLD/GGIO1$ST$SPCSO1$stVal")); err != nil {
		t.Fatalf("RegisterControl SPCSO1: %v", err)
	}

	// SPCSO2 — sbo-with-normal-security
	if err := srv.RegisterControl("InteropLD", "GGIO1.SPCSO2",
		iec61850.CtlModelSBONormal,
		makeOperateHandler("InteropLD/GGIO1$ST$SPCSO2$stVal")); err != nil {
		t.Fatalf("RegisterControl SPCSO2: %v", err)
	}

	// SPCSO3 — sbo-with-enhanced-security (SBOw)
	if err := srv.RegisterControl("InteropLD", "GGIO1.SPCSO3",
		iec61850.CtlModelSBOEnhanced,
		makeOperateHandler("InteropLD/GGIO1$ST$SPCSO3$stVal")); err != nil {
		t.Fatalf("RegisterControl SPCSO3: %v", err)
	}

	ln, err := iso.Listen("0.0.0.0:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	port := ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})

	return &goIEDServer{port: port, srv: srv}
}
