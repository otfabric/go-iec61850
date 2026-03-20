package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var listTypesCmd = &cobra.Command{
	Use:   "list-types <file>",
	Short: "List all data type templates (LNodeType, DOType, DAType, EnumType)",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListTypes,
}

var listTypesJSON bool

func init() {
	listTypesCmd.Flags().BoolVar(&listTypesJSON, "json", false, "output as JSON")
}

type typeRow struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	LNClass string `json:"lnClass,omitempty"`
	CDC     string `json:"cdc,omitempty"`
	Members int    `json:"members"`
	Desc    string `json:"desc,omitempty"`
}

func runListTypes(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	var rows []typeRow
	for _, t := range doc.DataTypeTemplates.LNodeTypes {
		rows = append(rows, typeRow{
			Kind: "LNodeType", ID: t.ID, LNClass: t.LNClass,
			Members: len(t.DOs), Desc: t.Desc,
		})
	}
	for _, t := range doc.DataTypeTemplates.DOTypes {
		rows = append(rows, typeRow{
			Kind: "DOType", ID: t.ID, CDC: t.CDC,
			Members: len(t.DAs) + len(t.SDOs), Desc: t.Desc,
		})
	}
	for _, t := range doc.DataTypeTemplates.DATypes {
		rows = append(rows, typeRow{
			Kind: "DAType", ID: t.ID,
			Members: len(t.BDAs), Desc: t.Desc,
		})
	}
	for _, t := range doc.DataTypeTemplates.EnumTypes {
		rows = append(rows, typeRow{
			Kind: "EnumType", ID: t.ID,
			Members: len(t.Vals), Desc: t.Desc,
		})
	}

	if listTypesJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KIND\tID\tLNCLASS\tCDC\tMEMBERS\tDESCRIPTION")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
			r.Kind, r.ID, r.LNClass, r.CDC, r.Members, r.Desc)
	}
	return tw.Flush()
}
