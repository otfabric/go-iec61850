// SPDX-License-Identifier: MIT

// Command sclparse inspects, validates, and summarizes IEC 61850 SCL files.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	tag       = ""
	commit    = ""
	buildDate = ""
)

const (
	exitOK         = 0
	exitUsage      = 1
	exitParseFail  = 2
	exitValidation = 3
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
	os.Exit(run(os.Stderr))
}

// run executes rootCmd and maps its result to a process exit code, writing
// any error message to stderr.
func run(stderr io.Writer) int {
	err := rootCmd.Execute()
	if err == nil {
		return exitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		_, _ = fmt.Fprintln(stderr, "sclparse: "+ee.msg)
		return ee.code
	}
	_, _ = fmt.Fprintln(stderr, err)
	return exitUsage
}

func sclFileCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"scd", "cid", "icd", "ssd", "iid", "sed", "xml"}, cobra.ShellCompDirectiveFilterFileExt
}
