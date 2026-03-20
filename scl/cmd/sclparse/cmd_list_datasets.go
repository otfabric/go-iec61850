package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var listDataSetsCmd = &cobra.Command{
	Use:   "list-datasets <file>",
	Short: "List all data sets with member counts",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListDataSets,
}

var listDataSetsJSON bool

func init() {
	listDataSetsCmd.Flags().BoolVar(&listDataSetsJSON, "json", false, "output as JSON")
}

func runListDataSets(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	rows := scl.ExportDataSets(doc)

	if listDataSetsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IED\tAP\tLD\tLN\tDATASET\tMEMBERS\tDESCRIPTION")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			r.IED, r.AccessPoint, r.LD, r.LN, r.DataSet, r.MemberCount, r.Desc)
	}
	return tw.Flush()
}
