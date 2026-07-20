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

var listReportsCmd = &cobra.Command{
	Use:   "list-reports <file>",
	Short: "List all report control blocks",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListReports,
}

var listReportsJSON bool

func init() {
	listReportsCmd.Flags().BoolVar(&listReportsJSON, "json", false, "output as JSON")
}

func runListReports(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	rows := scl.ExportReports(doc)

	if listReportsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IED\tAP\tLD\tLN\tNAME\tRPT-ID\tDATSET\tBUF\tCONFREV")
	for _, r := range rows {
		bufStr := "N"
		if r.Buffered {
			bufStr = "Y"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			r.IED, r.AccessPoint, r.LD, r.LN, r.Name, r.RptID, r.DatSet, bufStr, r.ConfRev)
	}
	return tw.Flush()
}
