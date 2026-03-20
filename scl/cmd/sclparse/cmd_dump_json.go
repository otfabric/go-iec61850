package main

import (
	"encoding/json"
	"os"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var dumpJSONCmd = &cobra.Command{
	Use:   "dump-json <file>",
	Short: "Emit normalized model as JSON",
	Long: `Emit the normalized SCL model as JSON.

Example:
  sclparse dump-json --pretty station.scd
  sclparse dump-json --output model.json station.scd`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: sclFileCompletion,
	RunE:              runDumpJSON,
}

var (
	dumpJSONPretty      bool
	dumpJSONOutput      string
	dumpJSONIncludeMeta bool
)

func init() {
	dumpJSONCmd.Flags().BoolVar(&dumpJSONPretty, "pretty", false, "pretty-print with indentation")
	dumpJSONCmd.Flags().StringVarP(&dumpJSONOutput, "output", "o", "", "write to file instead of stdout")
	dumpJSONCmd.Flags().BoolVar(&dumpJSONIncludeMeta, "include-metadata", false, "include version and kind metadata wrapper")
}

func runDumpJSON(_ *cobra.Command, args []string) error {
	path := args[0]

	result, err := scl.ParseFileOpts(path, scl.ParseOptions{})
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	var payload any
	if dumpJSONIncludeMeta {
		payload = struct {
			Schema scl.SchemaVersion `json:"schema"`
			Kind   scl.DocumentKind  `json:"kind"`
			Model  *scl.SCL          `json:"model"`
		}{
			Schema: result.Version.Schema,
			Kind:   result.Kind,
			Model:  result.Document,
		}
	} else {
		payload = result.Document
	}

	w := os.Stdout
	if dumpJSONOutput != "" {
		f, err := os.Create(dumpJSONOutput)
		if err != nil {
			return exitErr(exitParseFail, "create %s: %v", dumpJSONOutput, err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}

	enc := json.NewEncoder(w)
	if dumpJSONPretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(payload); err != nil {
		return exitErr(exitParseFail, "encode JSON: %v", err)
	}
	return nil
}
