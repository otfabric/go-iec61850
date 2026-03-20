package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var listIEDsCmd = &cobra.Command{
	Use:   "list-ieds <file>",
	Short: "List all IEDs with access-point and logical-device counts",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListIEDs,
}

var listIEDsJSON bool

func init() {
	listIEDsCmd.Flags().BoolVar(&listIEDsJSON, "json", false, "output as JSON")
}

type iedInfo struct {
	Name         string `json:"name"`
	Desc         string `json:"desc,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Type         string `json:"type,omitempty"`
	APs          int    `json:"accessPoints"`
	LDs          int    `json:"logicalDevices"`
	LNs          int    `json:"logicalNodes"`
}

func runListIEDs(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	var rows []iedInfo
	for _, ied := range doc.IEDs {
		info := iedInfo{
			Name: ied.Name, Desc: ied.Desc,
			Manufacturer: ied.Manufacturer, Type: ied.Type,
			APs: len(ied.AccessPoints),
		}
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			info.LDs += len(ap.Server.LDevices)
			for _, ld := range ap.Server.LDevices {
				info.LNs += len(ld.LNs)
				if ld.LN0 != nil {
					info.LNs++
				}
			}
		}
		rows = append(rows, info)
	}

	if listIEDsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tMANUFACTURER\tTYPE\tAPs\tLDs\tLNs\tDESCRIPTION")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			r.Name, r.Manufacturer, r.Type, r.APs, r.LDs, r.LNs, r.Desc)
	}
	return tw.Flush()
}
