// Command sclparse inspects, validates, and summarizes IEC 61850 SCL files.
package main

import (
	"errors"
	"fmt"
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
	Version:       version,
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
	rootCmd.SetVersionTemplate(versionTemplate("sclparse"))
	rootCmd.CompletionOptions.HiddenDefaultCmd = false
}

func main() {
	err := rootCmd.Execute()
	if err == nil {
		os.Exit(exitOK)
	}
	var ee *exitError
	if errors.As(err, &ee) {
		fmt.Fprintln(os.Stderr, "sclparse: "+ee.msg)
		os.Exit(ee.code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(exitUsage)
}

func sclFileCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"scd", "cid", "icd", "ssd", "iid", "sed", "xml"}, cobra.ShellCompDirectiveFilterFileExt
}
