// SPDX-License-Identifier: MIT

// Command sclgen generates internal Go types from IEC 61850 SCL XSD schemas.
// See SCL.md for the full specification.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/otfabric/go-iec61850/scl/internal/genir"
	"github.com/spf13/cobra"
)

type versionSpec struct {
	Dir     string
	Version string
	Label   string
}

var allVersions = []versionSpec{
	{"IEC_61850-6.2003.SCL.1.7.full", "v17", "1.7"},
	{"IEC_61850-6.2009.SCL.2007B.full", "v2007b", "2007B"},
	{"IEC_61850-6.2018.SCL.2007B4.full", "v2007b4", "2007B4"},
	{"IEC_61850-6.2025.SCL.2007C5.full", "v2007c5", "2007C5"},
}

var (
	version   = "dev"
	tag       = ""
	commit    = ""
	buildDate = ""
)

var (
	specRoot string
	outDir   string
	versions string
	verbose  bool
)

var rootCmd = &cobra.Command{
	Use:   "sclgen",
	Short: "Generate Go types from IEC 61850 SCL XSD schemas",
	Long: `sclgen generates internal Go types from IEC 61850 SCL XSD schema bundles.

Example:
  sclgen generate --spec-root ./scl/specs --out ./scl/internal/raw
  sclgen check    --spec-root ./scl/specs --out ./scl/internal/raw`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Go types from XSD schemas",
	Long: `Generate versioned Go types from XSD schema bundles.

Example:
  sclgen generate --spec-root ./scl/specs --out ./scl/internal/raw`,
	RunE: runGenerate,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify generated output is up to date",
	Long: `Generate to a temp directory and compare against checked-in output.
Exits with code 1 if any file differs.

Example:
  sclgen check --spec-root ./scl/specs --out ./scl/internal/raw`,
	RunE: runCheck,
}

func init() {
	rootCmd.CompletionOptions.HiddenDefaultCmd = false

	for _, cmd := range []*cobra.Command{generateCmd, checkCmd} {
		cmd.Flags().StringVar(&specRoot, "spec-root", "", "root directory containing XSD bundles (required)")
		cmd.Flags().StringVar(&outDir, "out", "", "output root directory for generated packages (required)")
		cmd.Flags().StringVar(&versions, "versions", "all", "comma-separated version list or 'all'")
		cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
		_ = cmd.MarkFlagRequired("spec-root")
		_ = cmd.MarkFlagRequired("out")
		_ = cmd.MarkFlagDirname("spec-root")
		_ = cmd.MarkFlagDirname("out")
	}

	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(versionCmd)
}

// Exit codes of sclgen: the organisation exit-code table
// (otfabric/engineering standards/api-design.md); see docs/exitcodes.md.
const (
	// ExitOK: success.
	ExitOK = 0
	// ExitFindings: check completed and found out-of-date generated files.
	ExitFindings = 1
	// ExitUsage: usage error (unknown command or flag, missing required
	// flag, no edition matches --versions).
	ExitUsage = 2
	// ExitInvalidInput: a schema bundle cannot be parsed or resolved.
	ExitInvalidInput = 3
	// ExitInternal: the output cannot be written, or another internal failure.
	ExitInternal = 10
	// ExitInterrupted: interrupted by SIGINT or SIGTERM.
	ExitInterrupted = 130
)

// exitError carries the exit code of a command failure.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func exitErr(code int, format string, args ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, args...)}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes rootCmd under ctx, writes any error to stderr and returns the
// exit code. When ctx is cancelled (SIGINT or SIGTERM) before the command
// returns, run returns ExitInterrupted at once.
func run(ctx context.Context, stderr io.Writer) int {
	err := ctx.Err()
	if err == nil {
		done := make(chan error, 1)
		go func() { done <- rootCmd.ExecuteContext(ctx) }()
		select {
		case err = <-done:
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	code := exitCode(err)
	switch {
	case err == nil:
	case code == ExitInterrupted:
		_, _ = fmt.Fprintln(stderr, "sclgen: interrupted")
	default:
		_, _ = fmt.Fprintln(stderr, "sclgen: "+err.Error())
	}
	return code
}

// exitCode classifies an error returned by rootCmd. The commands return
// *exitError with their code; cancellation is an interruption; anything else
// comes from cobra's command, flag and argument parsing and is a usage error.
func exitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	if errors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	return ExitUsage
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	selected := selectVersions(versions)
	if len(selected) == 0 {
		return exitErr(ExitUsage, "no edition matches --versions %q", versions)
	}

	for _, vs := range selected {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		bundleDir := filepath.Join(specRoot, vs.Dir)
		pkgDir := filepath.Join(outDir, vs.Version)

		if verbose {
			log.Printf("Processing %s from %s", vs.Label, vs.Dir)
		}

		if _, err := os.Stat(filepath.Join(bundleDir, "SCL.xsd")); os.IsNotExist(err) {
			log.Printf("WARNING: bundle %s not found, skipping", vs.Dir)
			continue
		}

		resolved, err := loadBundle(bundleDir, vs)
		if err != nil {
			return err
		}

		emitter := genir.NewEmitter(resolved, pkgDir, vs.Version)
		if err := emitter.Emit(); err != nil {
			return exitErr(ExitInternal, "emit %s: %w", vs.Label, err)
		}

		if verbose {
			log.Printf("  Generated %s", pkgDir)
		}
	}

	log.Println("sclgen: done")
	return nil
}

// loadBundle parses and resolves one XSD bundle; a failure is invalid input.
func loadBundle(bundleDir string, vs versionSpec) (*genir.Resolved, error) {
	schema, err := genir.ParseBundle(bundleDir, vs.Version)
	if err != nil {
		return nil, exitErr(ExitInvalidInput, "parse %s: %w", vs.Label, err)
	}
	if verbose {
		log.Printf("  Parsed: %d simpleTypes, %d complexTypes, %d elements, %d attrGroups",
			len(schema.SimpleTypes), len(schema.ComplexTypes),
			len(schema.Elements), len(schema.AttributeGroups))
	}
	resolved, err := genir.Resolve(schema)
	if err != nil {
		return nil, exitErr(ExitInvalidInput, "resolve %s: %w", vs.Label, err)
	}
	return resolved, nil
}

func runCheck(cmd *cobra.Command, _ []string) error {
	selected := selectVersions(versions)
	if len(selected) == 0 {
		return exitErr(ExitUsage, "no edition matches --versions %q", versions)
	}

	var stale []string
	for _, vs := range selected {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		bundleDir := filepath.Join(specRoot, vs.Dir)
		pkgDir := filepath.Join(outDir, vs.Version)

		if verbose {
			log.Printf("Checking %s from %s", vs.Label, vs.Dir)
		}

		if _, err := os.Stat(filepath.Join(bundleDir, "SCL.xsd")); os.IsNotExist(err) {
			log.Printf("WARNING: bundle %s not found, skipping", vs.Dir)
			continue
		}

		resolved, err := loadBundle(bundleDir, vs)
		if err != nil {
			return err
		}

		if err := checkEdition(resolved, vs, pkgDir); err != nil {
			if _, ok := errors.AsType[*exitError](err); ok {
				return err
			}
			log.Printf("CHECK FAILED for %s: %v", vs.Label, err)
			stale = append(stale, vs.Label)
		} else if verbose {
			log.Printf("  CHECK OK: %s", vs.Label)
		}
	}

	if len(stale) > 0 {
		return exitErr(ExitFindings, "generated files out of date: %s", strings.Join(stale, ", "))
	}
	return nil
}

// checkEdition generates one edition to a temporary directory and compares it
// with pkgDir. A difference is returned as a plain error; a failure to
// generate is an *exitError.
func checkEdition(resolved *genir.Resolved, vs versionSpec, pkgDir string) error {
	tmpDir, err := os.MkdirTemp("", "sclgen-check-*")
	if err != nil {
		return exitErr(ExitInternal, "create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	emitter := genir.NewEmitter(resolved, tmpDir, vs.Version)
	if err := emitter.Emit(); err != nil {
		return exitErr(ExitInternal, "emit %s: %w", vs.Label, err)
	}
	return compareDir(tmpDir, pkgDir)
}

func selectVersions(spec string) []versionSpec {
	if spec == "all" {
		return allVersions
	}
	parts := strings.Split(spec, ",")
	wanted := make(map[string]bool)
	for _, p := range parts {
		wanted[strings.TrimSpace(p)] = true
	}
	var result []versionSpec
	for _, vs := range allVersions {
		if wanted[vs.Version] {
			result = append(result, vs)
		}
	}
	return result
}

func compareDir(generated, checked string) error {
	genRoot, err := os.OpenRoot(generated)
	if err != nil {
		return err
	}
	defer func() { _ = genRoot.Close() }()
	checkedRoot, err := os.OpenRoot(checked)
	if err != nil {
		return fmt.Errorf("checked-in output: %w", err)
	}
	defer func() { _ = checkedRoot.Close() }()

	entries, err := os.ReadDir(generated)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		genData, err := genRoot.ReadFile(e.Name())
		if err != nil {
			return err
		}
		checkedData, err := checkedRoot.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("file %s: not found in checked-in output: %w", e.Name(), err)
		}
		if string(genData) != string(checkedData) {
			return fmt.Errorf("file %s: content differs", e.Name())
		}
	}
	return nil
}
