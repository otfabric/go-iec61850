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

var listLNsCmd = &cobra.Command{
	Use:   "list-lns <file>",
	Short: "List all logical nodes with their IED/LD location",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runListLNs,
}

var listLNsJSON bool

func init() {
	listLNsCmd.Flags().BoolVar(&listLNsJSON, "json", false, "output as JSON")
}

type lnInfo struct {
	IED     string `json:"ied"`
	AP      string `json:"ap"`
	LD      string `json:"ld"`
	LN      string `json:"ln"`
	LNClass string `json:"lnClass"`
	LNType  string `json:"lnType"`
	Desc    string `json:"desc,omitempty"`
}

func runListLNs(_ *cobra.Command, args []string) error {
	doc, err := scl.ParseFile(args[0])
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	var rows []lnInfo
	for _, ied := range doc.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				addLN := func(ln *scl.LN) {
					rows = append(rows, lnInfo{
						IED: ied.Name, AP: ap.Name, LD: ld.Inst,
						LN:      ln.Prefix + ln.LNClass + ln.Inst,
						LNClass: ln.LNClass, LNType: ln.LNType,
						Desc: ln.Desc,
					})
				}
				if ld.LN0 != nil {
					addLN(ld.LN0)
				}
				for i := range ld.LNs {
					addLN(&ld.LNs[i])
				}
			}
		}
	}

	if listLNsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IED\tAP\tLD\tLN\tCLASS\tLNTYPE\tDESCRIPTION")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.IED, r.AP, r.LD, r.LN, r.LNClass, r.LNType, r.Desc)
	}
	return tw.Flush()
}
