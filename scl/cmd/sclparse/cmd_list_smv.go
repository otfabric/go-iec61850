package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var listSMVCmd = &cobra.Command{
	Use:   "list-smv <file>",
	Short: "List all Sampled Values control blocks",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListSMV,
}

var listSMVJSON bool

func init() {
	listSMVCmd.Flags().BoolVar(&listSMVJSON, "json", false, "output as JSON")
}

func runListSMV(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	rows := scl.ExportSMVControls(doc)

	if listSMVJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IED\tAP\tLD\tNAME\tSMV-ID\tDATSET\tSMPRATE\tNOFASDU\tMCAST\tCONFREV")
	for _, r := range rows {
		mcast := "N"
		if r.Multicast {
			mcast = "Y"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%d\n",
			r.IED, r.AccessPoint, r.LD, r.Name, r.SmvID, r.DatSet,
			r.SmpRate, r.NofASDU, mcast, r.ConfRev)
	}
	return tw.Flush()
}
