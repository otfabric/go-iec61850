// SPDX-License-Identifier: MIT

// Command sclparse inspects, validates, and summarizes IEC 61850 SCL files.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	tag       = ""
	commit    = ""
	buildDate = ""
)

// Exit codes of sclparse: the organisation exit-code table
// (otfabric/engineering standards/api-design.md); see docs/exitcodes.md.
const (
	// ExitOK: success.
	ExitOK = 0
	// ExitFindings: validate completed and found validation errors.
	ExitFindings = 1
	// ExitUsage: usage error (unknown command or flag, wrong arguments).
	ExitUsage = 2
	// ExitInvalidInput: the SCL file cannot be read or parsed.
	ExitInvalidInput = 3
	// ExitInternal: the output cannot be written.
	ExitInternal = 10
	// ExitInterrupted: interrupted by SIGINT or SIGTERM.
	ExitInterrupted = 130
)

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func exitErr(code int, format string, args ...any) *exitError {
	return &exitError{code: code, msg: fmt.Sprintf(format, args...)}
}

var rootCmd = &cobra.Command{
	Use:   "sclparse",
	Short: "Inspect, validate, and summarize IEC 61850 SCL files",
	Long: `sclparse provides subcommands for working with IEC 61850 SCL configuration files.

It can detect schema versions, print element summaries, run semantic
validation, and emit the normalized data model as JSON.`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	rootCmd.AddCommand(detectCmd)
	rootCmd.AddCommand(summaryCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(dumpJSONCmd)
	rootCmd.AddCommand(listIEDsCmd)
	rootCmd.AddCommand(listLNsCmd)
	rootCmd.AddCommand(listDataSetsCmd)
	rootCmd.AddCommand(listReportsCmd)
	rootCmd.AddCommand(listGooseCmd)
	rootCmd.AddCommand(listSMVCmd)
	rootCmd.AddCommand(listConnectedAPCmd)
	rootCmd.AddCommand(listTypesCmd)
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.CompletionOptions.HiddenDefaultCmd = false
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes rootCmd under ctx and maps its result to a process exit code,
// writing any error message to stderr. When ctx is cancelled (SIGINT or
// SIGTERM) before the command returns, run returns ExitInterrupted at once.
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
		_, _ = fmt.Fprintln(stderr, "sclparse: interrupted")
	default:
		if ee, ok := errors.AsType[*exitError](err); ok {
			_, _ = fmt.Fprintln(stderr, "sclparse: "+ee.msg)
		} else {
			_, _ = fmt.Fprintln(stderr, err)
		}
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

// outputErr classifies a failure to write the command's output.
func outputErr(err error) error {
	if err == nil {
		return nil
	}
	return exitErr(ExitInternal, "write output: %v", err)
}

func sclFileCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"scd", "cid", "icd", "ssd", "iid", "sed", "xml"}, cobra.ShellCompDirectiveFilterFileExt
}
