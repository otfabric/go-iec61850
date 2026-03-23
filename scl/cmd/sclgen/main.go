// Command sclgen generates internal Go types from IEC 61850 SCL XSD schemas.
// See SCL.md for the full specification.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
  sclgen generate -spec-root ./scl/specs -out ./scl/internal/raw
  sclgen check    -spec-root ./scl/specs -out ./scl/internal/raw`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Go types from XSD schemas",
	Long: `Generate versioned Go types from XSD schema bundles.

Example:
  sclgen generate -spec-root ./scl/specs -out ./scl/internal/raw`,
	Run: runGenerate,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify generated output is up to date",
	Long: `Generate to a temp directory and compare against checked-in output.
Exits non-zero if any file differs.

Example:
  sclgen check -spec-root ./scl/specs -out ./scl/internal/raw`,
	Run: runCheck,
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runGenerate(_ *cobra.Command, _ []string) {
	selected := selectVersions(versions)
	if len(selected) == 0 {
		log.Fatal("no versions selected")
	}

	for _, vs := range selected {
		bundleDir := filepath.Join(specRoot, vs.Dir)
		pkgDir := filepath.Join(outDir, vs.Version)

		if verbose {
			log.Printf("Processing %s from %s", vs.Label, vs.Dir)
		}

		if _, err := os.Stat(filepath.Join(bundleDir, "SCL.xsd")); os.IsNotExist(err) {
			log.Printf("WARNING: bundle %s not found, skipping", vs.Dir)
			continue
		}

		schema, err := genir.ParseBundle(bundleDir, vs.Version)
		if err != nil {
			log.Fatalf("parse %s: %v", vs.Label, err)
		}

		if verbose {
			log.Printf("  Parsed: %d simpleTypes, %d complexTypes, %d elements, %d attrGroups",
				len(schema.SimpleTypes), len(schema.ComplexTypes),
				len(schema.Elements), len(schema.AttributeGroups))
		}

		resolved, err := genir.Resolve(schema)
		if err != nil {
			log.Fatalf("resolve %s: %v", vs.Label, err)
		}

		emitter := genir.NewEmitter(resolved, pkgDir, vs.Version)
		if err := emitter.Emit(); err != nil {
			log.Fatalf("emit %s: %v", vs.Label, err)
		}

		if verbose {
			log.Printf("  Generated %s", pkgDir)
		}
	}

	log.Println("sclgen: done")
}

func runCheck(_ *cobra.Command, _ []string) {
	selected := selectVersions(versions)
	if len(selected) == 0 {
		log.Fatal("no versions selected")
	}

	exitCode := 0
	for _, vs := range selected {
		bundleDir := filepath.Join(specRoot, vs.Dir)
		pkgDir := filepath.Join(outDir, vs.Version)

		if verbose {
			log.Printf("Checking %s from %s", vs.Label, vs.Dir)
		}

		if _, err := os.Stat(filepath.Join(bundleDir, "SCL.xsd")); os.IsNotExist(err) {
			log.Printf("WARNING: bundle %s not found, skipping", vs.Dir)
			continue
		}

		schema, err := genir.ParseBundle(bundleDir, vs.Version)
		if err != nil {
			log.Fatalf("parse %s: %v", vs.Label, err)
		}

		resolved, err := genir.Resolve(schema)
		if err != nil {
			log.Fatalf("resolve %s: %v", vs.Label, err)
		}

		tmpDir, err := os.MkdirTemp("", "sclgen-check-*")
		if err != nil {
			log.Fatalf("create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		emitter := genir.NewEmitter(resolved, tmpDir, vs.Version)
		if err := emitter.Emit(); err != nil {
			log.Fatalf("emit %s: %v", vs.Label, err)
		}

		if err := compareDir(tmpDir, pkgDir); err != nil {
			log.Printf("CHECK FAILED for %s: %v", vs.Label, err)
			exitCode = 1
		} else if verbose {
			log.Printf("  CHECK OK: %s", vs.Label)
		}
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
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
	entries, err := os.ReadDir(generated)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		genData, err := os.ReadFile(filepath.Join(generated, e.Name()))
		if err != nil {
			return err
		}
		checkedData, err := os.ReadFile(filepath.Join(checked, e.Name()))
		if err != nil {
			return fmt.Errorf("file %s: not found in checked-in output: %w", e.Name(), err)
		}
		if string(genData) != string(checkedData) {
			return fmt.Errorf("file %s: content differs", e.Name())
		}
	}
	return nil
}
