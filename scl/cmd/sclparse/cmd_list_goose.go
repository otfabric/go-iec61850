// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var listGooseCmd = &cobra.Command{
	Use:   "list-goose <file>",
	Short: "List all GOOSE (GSE) control blocks",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListGoose,
}

var listGooseJSON bool

func init() {
	listGooseCmd.Flags().BoolVar(&listGooseJSON, "json", false, "output as JSON")
}

func runListGoose(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	rows := scl.ExportGSEControls(doc)

	if listGooseJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IED\tAP\tLD\tNAME\tAPP-ID\tTYPE\tDATSET\tCONFREV")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			r.IED, r.AccessPoint, r.LD, r.Name, r.AppID, r.Type, r.DatSet, r.ConfRev)
	}
	return tw.Flush()
}
