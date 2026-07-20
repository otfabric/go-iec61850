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

var listConnectedAPCmd = &cobra.Command{
	Use:   "list-connected-ap <file>",
	Short: "List all connected access points",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListConnectedAP,
}

var listConnectedAPJSON bool

func init() {
	listConnectedAPCmd.Flags().BoolVar(&listConnectedAPJSON, "json", false, "output as JSON")
}

func runListConnectedAP(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	rows := scl.ExportConnectedAPs(doc)

	if listConnectedAPJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SUBNETWORK\tIED\tAP\tGSE\tSMV")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
			r.SubNetwork, r.IEDName, r.APName, r.GSECount, r.SMVCount)
	}
	return tw.Flush()
}
