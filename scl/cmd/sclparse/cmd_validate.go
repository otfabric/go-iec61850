package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate <file>",
	Short: "Run semantic validation and report diagnostics",
	Long: `Run semantic validation and present diagnostics.

Exit code 3 indicates validation errors are present.

Example:
  sclparse validate station.scd`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: sclFileCompletion,
	RunE:              runValidate,
}

var (
	validateJSON             bool
	validateStrict           bool
	validateMaxErrors        int
	validateWarningsAsErrors bool
	validateNoWarnings       bool
)

func init() {
	validateCmd.Flags().BoolVar(&validateJSON, "json", false, "output as JSON")
	validateCmd.Flags().BoolVar(&validateStrict, "strict", false, "treat warnings as errors")
	validateCmd.Flags().IntVar(&validateMaxErrors, "max-errors", 0, "stop after N errors (0=unlimited)")
	validateCmd.Flags().BoolVar(&validateWarningsAsErrors, "warnings-as-errors", false, "promote warnings to errors")
	validateCmd.Flags().BoolVar(&validateNoWarnings, "no-warnings", false, "suppress warning output")
}

func runValidate(_ *cobra.Command, args []string) error {
	path := args[0]

	result, err := scl.ParseFileWithOptions(path, scl.ParseOptions{
		ValidateSemantic: true,
		Strict:           validateStrict,
	})
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	type diagOut struct {
		Severity string `json:"severity"`
		Code     string `json:"code,omitempty"`
		Path     string `json:"path"`
		Message  string `json:"message"`
	}

	var diags []diagOut
	errorCount := 0
	warningCount := 0

	for _, d := range result.Diagnostics {
		sev := string(d.Severity)
		if validateWarningsAsErrors && d.Severity == scl.DiagWarning {
			sev = string(scl.DiagError)
		}
		if validateNoWarnings && d.Severity == scl.DiagWarning {
			continue
		}

		diags = append(diags, diagOut{
			Severity: sev,
			Code:     d.Code,
			Path:     d.Path,
			Message:  d.Message,
		})

		if sev == string(scl.DiagError) {
			errorCount++
		} else {
			warningCount++
		}

		if validateMaxErrors > 0 && errorCount >= validateMaxErrors {
			break
		}
	}

	if validateJSON {
		out := struct {
			File     string    `json:"file"`
			Errors   int       `json:"errors"`
			Warnings int       `json:"warnings"`
			Findings []diagOut `json:"findings"`
		}{
			File:     filepath.Base(path),
			Errors:   errorCount,
			Warnings: warningCount,
			Findings: diags,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	} else {
		if len(diags) == 0 {
			fmt.Printf("%s: no issues found\n", filepath.Base(path))
		} else {
			for _, d := range diags {
				fmt.Printf("%-7s  %-40s  %s\n", d.Severity, d.Path, d.Message)
			}
			fmt.Printf("\n%d error(s), %d warning(s)\n", errorCount, warningCount)
		}
	}

	if errorCount > 0 {
		return exitErr(exitValidation, "%d validation error(s)", errorCount)
	}
	return nil
}
